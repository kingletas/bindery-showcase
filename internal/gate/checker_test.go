package gate

import (
	"os"
	"strings"
	"testing"
)

// scratch points TMPDIR at an empty directory of the test's own and returns it,
// so a test can see whether the Checker left anything behind.
func scratch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

// okRepo is a repository whose ok.txt says ok in the index and in the work tree.
func okRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	commitFile(t, dir, "ok.txt", "ok\n")
	return dir
}

const okTable = "go\t*.go\tgrep -qx ok ok.txt\ngo\tok.txt\tgrep -qx ok ok.txt\n"

// A broken file that is not staged says nothing about the commit.
func TestAnUnstagedBreakageDoesNotRefuseACleanStagedChange(t *testing.T) {
	dir := okRepo(t)
	write(t, dir, "ok.txt", "broken\n")
	stage(t, dir, "main.go", "package main\n")

	findings := Evaluate(staged(t, dir), "Add main\n", true, parse(t, okTable), checkerFor(t, dir))
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: ok.txt is only broken in the work tree", findings)
	}
}

// A staged break is refused even when its fix sits in the work tree.
func TestAStagedBreakageWithItsFixUnstagedIsRefused(t *testing.T) {
	dir := okRepo(t)
	stage(t, dir, "ok.txt", "broken\n")
	stage(t, dir, "main.go", "package main\n")
	write(t, dir, "ok.txt", "ok\n")

	findings := Evaluate(staged(t, dir), "Add main\n", true, parse(t, okTable), checkerFor(t, dir))
	if len(findings) != 1 || findings[0].Name != "go" {
		t.Fatalf("findings = %+v, want the go lane to fail on the staged content", findings)
	}
}

func TestAnUntrackedFileIsNotInTheCopyTheChecksRunIn(t *testing.T) {
	dir := okRepo(t)
	write(t, dir, "untracked.txt", "here\n")
	stage(t, dir, "main.go", "package main\n")

	table := parse(t, "go\t*.go\ttest -f untracked.txt\n")
	if findings := Evaluate(staged(t, dir), "x\n", true, table, checkerFor(t, dir)); len(findings) != 1 {
		t.Fatalf("findings = %+v, want the check to fail because the copy holds only staged files", findings)
	}
}

func TestAChecksGitCommandsSeeTheRealRepositoryAndItsIndex(t *testing.T) {
	dir := okRepo(t)
	table := parse(t, "text\t*.dat\tgit diff --cached --check\n")

	stage(t, dir, "clean.dat", "fine\n")
	if findings := Evaluate(staged(t, dir), "x\n", true, table, checkerFor(t, dir)); len(findings) != 0 {
		t.Fatalf("clean: findings = %+v, want none", findings)
	}
	stage(t, dir, "spaced.dat", "trailing space \n")
	findings := Evaluate(staged(t, dir), "x\n", true, table, checkerFor(t, dir))
	if len(findings) != 1 || findings[0].Name != "text" {
		t.Fatalf("with trailing whitespace: findings = %+v, want the text lane to fail", findings)
	}
}

func TestTheCopyIsRemovedByClose(t *testing.T) {
	dir := okRepo(t)
	tmp := scratch(t)
	stage(t, dir, "main.go", "package main\n")

	c := NewChecker(dir)
	if _, err := c.Run([]string{"true"}); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, tmp); len(got) != 1 || !strings.HasPrefix(got[0], "bindery-gate-") {
		t.Fatalf("while open the temp directory holds %v, want one bindery-gate copy", got)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, tmp); len(got) != 0 {
		t.Fatalf("after Close the temp directory holds %v, want nothing", got)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("a second Close failed: %v", err)
	}
}

func TestTheCopyIsRemovedAfterAFailingCheck(t *testing.T) {
	dir := okRepo(t)
	tmp := scratch(t)
	stage(t, dir, "main.go", "package main\n")

	func() {
		c := NewChecker(dir)
		defer c.Close()
		Evaluate(staged(t, dir), "x\n", true, parse(t, "go\t*.go\tfalse\n"), c.Run)
	}()
	if got := entries(t, tmp); len(got) != 0 {
		t.Fatalf("the temp directory holds %v after a failing check, want nothing", got)
	}
}

func TestTheCopyIsRemovedWhenTheCallerPanics(t *testing.T) {
	dir := okRepo(t)
	tmp := scratch(t)
	stage(t, dir, "main.go", "package main\n")

	func() {
		defer func() { recover() }()
		c := NewChecker(dir)
		defer c.Close()
		c.Run([]string{"true"})
		panic("a bug somewhere after the check")
	}()
	if got := entries(t, tmp); len(got) != 0 {
		t.Fatalf("the temp directory holds %v after a panic, want nothing", got)
	}
}

func TestNothingIsWrittenWhenNoLaneRuns(t *testing.T) {
	dir := okRepo(t)
	tmp := scratch(t)
	stage(t, dir, "data.csv", "a,b\n")

	c := NewChecker(dir)
	defer c.Close()
	Evaluate(staged(t, dir), "x\n", true, parse(t, okTable), c.Run)
	if got := entries(t, tmp); len(got) != 0 {
		t.Fatalf("the temp directory holds %v although no lane matched", got)
	}
}

func TestAFailureToCopyTheIndexFailsTheLane(t *testing.T) {
	// Not a repository, so checkout-index cannot run.
	c := NewChecker(t.TempDir())
	defer c.Close()
	findings := Evaluate([]Change{{Status: 'A', Path: "a.go"}}, "x\n", true, parse(t, okTable), c.Run)
	if len(findings) != 1 || !strings.Contains(findings[0].Reason, "could not check out the staged files") {
		t.Fatalf("findings = %+v, want a lane failure saying the copy failed", findings)
	}
}
