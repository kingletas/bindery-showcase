package gate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAPathIsRoutedToItsLaneAndOnlyThatLaneRuns(t *testing.T) {
	table := parse(t, "go\t*.go\tgo vet ./...\ndocs\t*.md\tgit diff --cached --check\n")
	changes := []Change{{Status: 'M', Path: "internal/x/y.go"}}

	rec := &recorder{}
	if findings := Evaluate(changes, "Change y\n", true, table, rec.run); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
	if want := []string{"go vet ./..."}; !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("ran %v, want %v", rec.calls, want)
	}
}

func TestEachMatchedLaneRunsOnceHoweverManyPathsMatch(t *testing.T) {
	table := parse(t, "go\t*.go\tgo vet ./...\ngo\tgo.mod\tgo vet ./...\ndocs\t*.md\tdocs-check\n")
	changes := []Change{
		{Status: 'M', Path: "a.go"}, {Status: 'M', Path: "b.go"}, {Status: 'A', Path: "go.mod"},
		{Status: 'M', Path: "README.md"},
	}
	rec := &recorder{}
	Evaluate(changes, "Several\n", true, table, rec.run)
	if want := []string{"go vet ./...", "docs-check"}; !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("ran %v, want %v", rec.calls, want)
	}
}

func TestAPassingCommitIsSilent(t *testing.T) {
	dir := newRepo(t)
	stage(t, dir, "main.go", "package main\n")

	table := parse(t, "go\t*.go\ttrue\n")
	if findings := Evaluate(staged(t, dir), "Add main\n", true, table, ExecRunner(dir)); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestAFailingLaneRefusesTheCommitAndSaysHow(t *testing.T) {
	dir := newRepo(t)
	stage(t, dir, "main.go", "package main\n")
	stage(t, dir, "notes.md", "notes\n")

	table := parse(t, "go\t*.go\tfalse\ndocs\t*.md\ttrue\n")
	findings := Evaluate(staged(t, dir), "Add main\n", true, table, ExecRunner(dir))

	if len(findings) != 1 || findings[0].Name != "go" {
		t.Fatalf("findings = %+v, want exactly the go lane", findings)
	}
	if want := `The check "false" exited with status 1.`; findings[0].Reason != want {
		t.Fatalf("reason = %q, want %q", findings[0].Reason, want)
	}
}

func TestAFailingChecksLastOutputLineIsInTheReason(t *testing.T) {
	table := parse(t, "go\t*.go\tmake check\n")
	run := func([]string) (string, error) {
		return "first line\nFAIL: widget\x1b[0m  \n\n", exitError(t)
	}
	findings := Evaluate([]Change{{Status: 'M', Path: "a.go"}}, "x\n", true, table, run)
	if len(findings) != 1 || !strings.HasSuffix(findings[0].Reason, "saying: FAIL: widget [0m.") {
		t.Fatalf("findings = %+v, want the last line, cleaned, in the reason", findings)
	}
	assertOneSentence(t, findings[0].Reason)
}

func TestEveryFailingLaneIsReportedInTableOrder(t *testing.T) {
	table := parse(t, "docs\t*.md\tdocs-check\ngo\t*.go\tgo-check\n")
	changes := []Change{{Status: 'M', Path: "a.go"}, {Status: 'M', Path: "a.md"}}
	rec := &recorder{status: map[string]error{"docs-check": errors.New("x"), "go-check": errors.New("y")}}
	if got := findingNames(Evaluate(changes, "x\n", true, table, rec.run)); !reflect.DeepEqual(got, []string{"docs", "go"}) {
		t.Fatalf("findings = %v, want docs then go", got)
	}
}

func TestACheckThatIsNotInstalledFailsItsLane(t *testing.T) {
	dir := newRepo(t)
	stage(t, dir, "main.go", "package main\n")

	table := parse(t, "go\t*.go\tno-such-tool-anywhere --flag\n")
	findings := Evaluate(staged(t, dir), "Add main\n", true, table, ExecRunner(dir))
	if len(findings) != 1 || !strings.Contains(findings[0].Reason, `"no-such-tool-anywhere --flag" could not run`) {
		t.Fatalf("findings = %+v, want a lane failure saying the check could not run", findings)
	}
}

func TestChecksRunAtTheRepositoryRoot(t *testing.T) {
	dir := newRepo(t)
	stage(t, dir, "main.go", "package main\n")

	table := parse(t, "go\t*.go\ttest -f README.md\n")
	if findings := Evaluate(staged(t, dir), "x\n", true, table, ExecRunner(dir)); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: the check should see README.md at the root", findings)
	}
	missing := parse(t, "go\t*.go\ttest -f absent.txt\n")
	if findings := Evaluate(staged(t, dir), "x\n", true, missing, ExecRunner(dir)); len(findings) != 1 {
		t.Fatalf("findings = %+v, want the lane to fail on a file that is not there", findings)
	}
}

func TestAPathNoLaneClaimsIsRefusedAndNamed(t *testing.T) {
	dir := newRepo(t)
	stage(t, dir, "main.go", "package main\n")
	stage(t, dir, "data/rows.csv", "a,b\n")

	table := parse(t, "go\t*.go\ttrue\n")
	rec := &recorder{}
	findings := Evaluate(staged(t, dir), "Add rows\n", true, table, rec.run)

	if len(findings) != 1 || findings[0].Name != UnclaimedName {
		t.Fatalf("findings = %+v, want only the unclaimed rule", findings)
	}
	if !strings.Contains(findings[0].Reason, `"data/rows.csv"`) || strings.Contains(findings[0].Reason, "main.go") {
		t.Fatalf("reason = %q, want it to name the unclaimed path and not the claimed one", findings[0].Reason)
	}
	assertOneSentence(t, findings[0].Reason)
	if len(rec.calls) != 1 {
		t.Fatalf("ran %v: the claimed path's lane should still run", rec.calls)
	}
}

func TestUnclaimedPathsAreListedBrieflyWhenThereAreMany(t *testing.T) {
	table := parse(t, "go\t*.go\ttrue\n")
	var changes []Change
	for _, p := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		changes = append(changes, Change{Status: 'A', Path: p})
	}
	findings := Evaluate(changes, "x\n", true, table, (&recorder{}).run)
	if len(findings) != 1 || !strings.Contains(findings[0].Reason, `"a.txt", "b.txt", "c.txt" and 2 more`) {
		t.Fatalf("findings = %+v, want three paths and a count", findings)
	}
}

func TestEveryPathClaimedLeavesTheUnclaimedRuleSilent(t *testing.T) {
	table := parse(t, "go\t*.go\ttrue\ndocs\t*.md\ttrue\n")
	changes := []Change{{Status: 'A', Path: "a.go"}, {Status: 'D', Path: "old.md"}, {Status: 'R', OldPath: "x.md", Path: "y.md"}}
	if findings := Evaluate(changes, "x\n", true, table, (&recorder{}).run); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestARenameIsRoutedByBothItsPaths(t *testing.T) {
	table := parse(t, "go\t*.go\tgo-check\ndocs\t*.md\tdocs-check\n")
	rec := &recorder{}
	Evaluate([]Change{{Status: 'R', OldPath: "a.md", Path: "a.go"}}, "x\n", true, table, rec.run)
	if want := []string{"go-check", "docs-check"}; !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("ran %v, want both lanes in table order", rec.calls)
	}
}

func TestAnEmptyStagedChangeIsSilentAndRunsNothing(t *testing.T) {
	dir := newRepo(t)
	if changes := staged(t, dir); len(changes) != 0 {
		t.Fatalf("changes = %+v, want none", changes)
	}
	table := parse(t, "go\t*.go\ttrue\n")
	rec := &recorder{}
	if findings := Evaluate(nil, "", true, table, rec.run); len(findings) != 0 || len(rec.calls) != 0 {
		t.Fatalf("findings %+v, ran %v; want neither", findings, rec.calls)
	}
}

func TestStagedReportsAddedModifiedDeletedAndRenamedPaths(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "keep.txt", "one\n")
	commitFile(t, dir, "gone.txt", "two\n")
	commitFile(t, dir, "old-name.txt", "three three three three\n")
	write(t, dir, "keep.txt", "one changed\n")
	gitIn(t, dir, "add", "--", "keep.txt")
	gitIn(t, dir, "rm", "-q", "--", "gone.txt")
	gitIn(t, dir, "mv", "old-name.txt", "new-name.txt")
	stage(t, dir, "fresh.txt", "four\n")

	got := map[string]Change{}
	for _, c := range staged(t, dir) {
		got[c.Path] = c
	}
	want := map[string]Change{
		"keep.txt":     {Status: 'M', Path: "keep.txt"},
		"gone.txt":     {Status: 'D', Path: "gone.txt"},
		"new-name.txt": {Status: 'R', Path: "new-name.txt", OldPath: "old-name.txt"},
		"fresh.txt":    {Status: 'A', Path: "fresh.txt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("staged = %+v\nwant     %+v", got, want)
	}
}

func TestStagedReadsPathsWithSpacesAndNonASCIIIntact(t *testing.T) {
	dir := newRepo(t)
	stage(t, dir, "sample dir/café menu.txt", "x\n")
	changes := staged(t, dir)
	if len(changes) != 1 || changes[0].Path != "sample dir/café menu.txt" {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestStagedFailsOutsideARepository(t *testing.T) {
	if _, err := Staged(t.TempDir()); err == nil {
		t.Fatal("Staged succeeded outside a repository")
	}
	if _, err := RepoRoot(t.TempDir()); err == nil {
		t.Fatal("RepoRoot succeeded outside a repository")
	}
}

func TestParseNameStatusRefusesTruncatedOutput(t *testing.T) {
	for _, out := range []string{"R100\x00old\x00", "M\x00", "\x00a\x00"} {
		if _, err := parseNameStatus([]byte(out)); err == nil {
			t.Errorf("parseNameStatus(%q) succeeded, want an error", out)
		}
	}
}

// exitError makes the *exec.ExitError a failed check returns, from a real
// process, so the test does not depend on how the standard library builds one.
func exitError(t *testing.T) error {
	t.Helper()
	_, err := ExecRunner(t.TempDir())([]string{"false"})
	if err == nil {
		t.Fatal("false exited 0")
	}
	return err
}
