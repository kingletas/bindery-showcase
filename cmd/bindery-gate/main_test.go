package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gateBinary is the built bindery-gate that the hook tests run through git.
var gateBinary string

// TestMain keeps the throwaway repositories independent of whatever git
// environment the tests were started from, then builds the binary once.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "GIT_") {
			os.Unsetenv(name)
		}
	}
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir, err := os.MkdirTemp("", "bindery-gate-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	gateBinary = filepath.Join(dir, "bindery-gate")
	if out, err := exec.Command("go", "build", "-o", gateBinary, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building bindery-gate: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, code := gitCode(t, dir, args...)
	if code != 0 {
		t.Fatalf("git %s exited %d:\n%s", strings.Join(args, " "), code, out)
	}
	return out
}

// gitCode runs git as an invented author with the gate available to hooks, and
// returns its combined output and exit code.
func gitCode(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	full := append([]string{
		"-C", dir,
		"-c", "user.name=Sample Author",
		"-c", "user.email=author@example.test",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "BINDERY_GATE="+gateBinary)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	default:
		t.Fatal(err)
		return "", -1
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// laneTable is the table the throwaway repositories use: a go lane that passes
// while ok.txt says ok, and a docs lane that always passes.
const laneTable = "go\t*.go\tgrep -qx ok ok.txt\n" +
	"go\tok.txt\tgrep -qx ok ok.txt\n" +
	"docs\t*.md\ttrue\n"

// newRepo makes a repository with one commit, the lanes table, an ok.txt that
// says ok, and one existing test file, plus the shipped hook if hook is set.
func newRepo(t *testing.T, hook bool) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "lanes.tsv", laneTable)
	write(t, dir, "ok.txt", "ok\n")
	write(t, dir, "widget_test.go", "package widget\n")
	write(t, dir, "README.md", "sample\n")
	gitIn(t, dir, "add", "--", "lanes.tsv", "ok.txt", "widget_test.go", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "Start the sample")
	if hook {
		installHook(t, dir)
	}
	return dir
}

// installHook copies the shipped example hook into the repository.
func installHook(t *testing.T, dir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "commit-msg"))
	if err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	if err := os.WriteFile(hook, raw, 0o755); err != nil {
		t.Fatal(err)
	}
}

func stage(t *testing.T, dir, rel, content string) {
	t.Helper()
	write(t, dir, rel, content)
	gitIn(t, dir, "add", "--", rel)
}

type result struct {
	code           int
	stdout, stderr string
}

// runGate runs the command in-process in dir with the given environment.
func runGate(t *testing.T, dir string, env map[string]string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, dir, func(k string) string { return env[k] }, &out, &errb)
	return result{code, out.String(), errb.String()}
}

func (r result) lines() []string {
	if r.stderr == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(r.stderr, "\n"), "\n")
}

// --- run by hand ---

func TestAPassingStagedChangeExitsZeroAndIsSilent(t *testing.T) {
	dir := newRepo(t, false)
	stage(t, dir, "notes.md", "notes\n")
	if r := runGate(t, dir, nil); r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("got %+v, want exit 0 and no output", r)
	}
}

func TestAFailingLaneExitsOneWithOneLineNamingIt(t *testing.T) {
	dir := newRepo(t, false)
	stage(t, dir, "ok.txt", "broken\n")
	r := runGate(t, dir, nil)
	want := `bindery-gate: go: The check "grep -qx ok ok.txt" exited with status 1.`
	if r.code != 1 || r.stdout != "" || len(r.lines()) != 1 || r.lines()[0] != want {
		t.Fatalf("got %+v, want exit 1 and the single line %q", r, want)
	}
}

func TestEveryFailingRuleGetsItsOwnLine(t *testing.T) {
	dir := newRepo(t, false)
	stage(t, dir, "ok.txt", "broken\n")
	stage(t, dir, "widget_test.go", "package widget\n// edit\n")
	stage(t, dir, "data.csv", "a,b\n")
	r := runGate(t, dir, nil, writeMessage(t, "Change things\n"))
	if r.code != 1 || len(r.lines()) != 3 {
		t.Fatalf("got %+v, want exit 1 and three lines", r)
	}
	for i, prefix := range []string{"bindery-gate: unclaimed: ", "bindery-gate: test-changed: ", "bindery-gate: go: "} {
		if !strings.HasPrefix(r.lines()[i], prefix) {
			t.Errorf("line %d = %q, want prefix %q", i, r.lines()[i], prefix)
		}
	}
}

func TestByHandWithNoMessageAnEditedTestIsRefusedAndTheMessageFilePasses(t *testing.T) {
	dir := newRepo(t, false)
	stage(t, dir, "widget_test.go", "package widget\n// edit\n")

	r := runGate(t, dir, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "no commit message was given") {
		t.Fatalf("got %+v, want a refusal saying no message was given", r)
	}
	msg := writeMessage(t, "Loosen it\n\nTest changed: the old expectation was wrong.\n")
	if r := runGate(t, dir, nil, msg); r.code != 0 || r.stderr != "" {
		t.Fatalf("got %+v, want a pass with the reason in the message file", r)
	}
}

func TestAPathNoLaneClaimsExitsOne(t *testing.T) {
	dir := newRepo(t, false)
	stage(t, dir, "data.csv", "a,b\n")
	r := runGate(t, dir, nil)
	if r.code != 1 || len(r.lines()) != 1 || !strings.HasPrefix(r.lines()[0], `bindery-gate: unclaimed: No lane claims "data.csv"`) {
		t.Fatalf("got %+v", r)
	}
}

func TestRunFromASubdirectoryJudgesTheWholeRepository(t *testing.T) {
	dir := newRepo(t, false)
	stage(t, dir, "ok.txt", "broken\n")
	sub := filepath.Join(dir, "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if r := runGate(t, sub, nil); r.code != 1 || !strings.HasPrefix(r.stderr, "bindery-gate: go: ") {
		t.Fatalf("got %+v, want the go lane to fail from a subdirectory", r)
	}
}

// --- failing closed ---

func TestAMalformedTableExitsTwoEvenWhenTheChangeWouldPass(t *testing.T) {
	dir := newRepo(t, false)
	write(t, dir, "lanes.tsv", "go\t*.go\n")
	stage(t, dir, "notes.md", "notes\n")
	r := runGate(t, dir, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "bindery-gate: table: ") || !strings.Contains(r.stderr, "lanes.tsv: line 1 has 2 tab-separated columns") {
		t.Fatalf("got %+v, want exit 2 naming the table and the line", r)
	}
	if len(r.lines()) != 1 {
		t.Fatalf("stderr = %q, want one line", r.stderr)
	}
}

func TestAMissingTableExitsTwo(t *testing.T) {
	dir := newRepo(t, false)
	stage(t, dir, "notes.md", "notes\n")
	r := runGate(t, dir, map[string]string{EnvTable: "no-such-table.tsv"})
	if r.code != 2 || !strings.HasPrefix(r.stderr, "bindery-gate: table: ") {
		t.Fatalf("got %+v, want exit 2 about the table", r)
	}
}

func TestOutsideARepositoryExitsTwo(t *testing.T) {
	r := runGate(t, t.TempDir(), nil)
	if r.code != 2 || !strings.HasPrefix(r.stderr, "bindery-gate: repository: ") || len(r.lines()) != 1 {
		t.Fatalf("got %+v", r)
	}
}

func TestAnUnreadableMessageFileExitsTwo(t *testing.T) {
	dir := newRepo(t, false)
	r := runGate(t, dir, nil, filepath.Join(dir, "no-such-message"))
	if r.code != 2 || !strings.HasPrefix(r.stderr, "bindery-gate: message: ") {
		t.Fatalf("got %+v", r)
	}
}

func TestMisuseExitsTwoWithTheUsage(t *testing.T) {
	dir := newRepo(t, false)
	for _, args := range [][]string{{"--help"}, {"-x"}, {"a", "b"}} {
		r := runGate(t, dir, nil, args...)
		if r.code != 2 || r.stderr != "usage: bindery-gate [commit-message-file]\n" {
			t.Errorf("args %v: got %+v", args, r)
		}
	}
}

func TestVersion(t *testing.T) {
	if r := runGate(t, t.TempDir(), nil, "--version"); r.code != 0 || r.stdout != "bindery dev\n" {
		t.Fatalf("got %+v", r)
	}
}

// --- configuration ---

func TestTheTableComesFromTheEnvironment(t *testing.T) {
	dir := newRepo(t, false)
	stage(t, dir, "data.csv", "a,b\n")
	write(t, dir, "elsewhere.tsv", "data\t*.csv\ttrue\n")

	if r := runGate(t, dir, nil); r.code != 1 {
		t.Fatalf("with the default table got %+v, want a refusal for the unclaimed csv", r)
	}
	if r := runGate(t, dir, map[string]string{EnvTable: "elsewhere.tsv"}); r.code != 0 || r.stderr != "" {
		t.Fatalf("with a relative table got %+v, want a pass", r)
	}
	abs := filepath.Join(dir, "elsewhere.tsv")
	if r := runGate(t, t.TempDir(), map[string]string{EnvTable: abs}); r.code != 2 {
		t.Fatalf("outside a repository got %+v, want exit 2", r)
	}
	if r := runGate(t, dir, map[string]string{EnvTable: abs}); r.code != 0 {
		t.Fatalf("with an absolute table got %+v, want a pass", r)
	}
}

// --- as a git commit-msg hook ---

func commit(t *testing.T, dir string, messages ...string) (string, int) {
	t.Helper()
	args := []string{"commit", "-q"}
	for _, m := range messages {
		args = append(args, "-m", m)
	}
	return gitCode(t, dir, args...)
}

func head(t *testing.T, dir string) string {
	return strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))
}

func TestHookRefusesAnEditedTestWithoutAReasonLine(t *testing.T) {
	dir := newRepo(t, true)
	before := head(t, dir)
	stage(t, dir, "widget_test.go", "package widget\n// edit\n")

	out, code := commit(t, dir, "Loosen the widget test")
	if code == 0 {
		t.Fatalf("the commit went through:\n%s", out)
	}
	if !strings.Contains(out, "bindery-gate: test-changed: The existing test \"widget_test.go\" is changed") {
		t.Fatalf("output = %q, want the gate's line", out)
	}
	if head(t, dir) != before {
		t.Fatal("HEAD moved although the commit was refused")
	}
}

func TestHookAcceptsTheSameEditWithAReasonLine(t *testing.T) {
	dir := newRepo(t, true)
	before := head(t, dir)
	stage(t, dir, "widget_test.go", "package widget\n// edit\n")

	out, code := commit(t, dir, "Loosen the widget test", "Test changed: the old expectation counted the header row.")
	if code != 0 || out != "" {
		t.Fatalf("exit %d, output %q; want a silent commit", code, out)
	}
	if head(t, dir) == before {
		t.Fatal("HEAD did not move")
	}
}

func TestHookIsSilentOnAPassingCommitAndAcceptsANewTestWithoutAReason(t *testing.T) {
	dir := newRepo(t, true)
	stage(t, dir, "gadget_test.go", "package gadget\n")
	stage(t, dir, "notes.md", "notes\n")
	if out, code := commit(t, dir, "Add a gadget test and notes"); code != 0 || out != "" {
		t.Fatalf("exit %d, output %q; want a silent commit", code, out)
	}
}

func TestHookRefusesADeletedTestWithoutAReasonLine(t *testing.T) {
	dir := newRepo(t, true)
	gitIn(t, dir, "rm", "-q", "--", "widget_test.go")
	out, code := commit(t, dir, "Drop the widget test")
	if code == 0 || !strings.Contains(out, "bindery-gate: test-changed: ") {
		t.Fatalf("exit %d, output %q; want a refusal by the test rule", code, out)
	}
	if out, code := commit(t, dir, "Drop the widget test", "Test changed: the widget is gone."); code != 0 || out != "" {
		t.Fatalf("with a reason: exit %d, output %q; want a silent commit", code, out)
	}
}

func TestHookBlocksACommitThatBreaksALane(t *testing.T) {
	dir := newRepo(t, true)
	before := head(t, dir)
	stage(t, dir, "ok.txt", "broken\n")
	out, code := commit(t, dir, "Mark it broken")
	if code == 0 || !strings.Contains(out, `bindery-gate: go: The check "grep -qx ok ok.txt" exited with status 1.`) {
		t.Fatalf("exit %d, output %q; want the go lane to block the commit", code, out)
	}
	if head(t, dir) != before {
		t.Fatal("HEAD moved although the commit was refused")
	}
}

func TestHookBlocksWhenTheTableIsMalformed(t *testing.T) {
	dir := newRepo(t, true)
	write(t, dir, "lanes.tsv", "docs *.md true\n")
	stage(t, dir, "notes.md", "notes\n")
	out, code := commit(t, dir, "Add notes")
	if code == 0 || !strings.Contains(out, "bindery-gate: table: ") {
		t.Fatalf("exit %d, output %q; want the commit blocked by the table", code, out)
	}
}

// A commit with a pathspec commits only those paths, so the gate must judge
// those and not everything that happens to be staged.
func TestHookJudgesOnlyWhatAPathspecCommitCommits(t *testing.T) {
	dir := newRepo(t, true)
	stage(t, dir, "widget_test.go", "package widget\n// edit with no reason yet\n")
	write(t, dir, "notes.md", "notes\n")
	gitIn(t, dir, "add", "--", "notes.md")

	if out, code := gitCode(t, dir, "commit", "-q", "-m", "Add notes", "--", "notes.md"); code != 0 || out != "" {
		t.Fatalf("exit %d, output %q; want the notes commit to pass without the staged test edit", code, out)
	}
	if out, code := commit(t, dir, "Now the test edit"); code == 0 || !strings.Contains(out, "test-changed") {
		t.Fatalf("exit %d, output %q; want the test edit refused on its own", code, out)
	}
}

func writeMessage(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "COMMIT_EDITMSG")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
