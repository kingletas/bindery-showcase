package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain keeps the throwaway repositories independent of whatever git
// environment the tests were started from, including a commit hook's own.
func TestMain(m *testing.M) {
	scrubGitEnv()
	os.Exit(m.Run())
}

// scrubGitEnv removes every GIT_ variable, then switches off the user and
// system git configuration, so a test repository sees only its own.
func scrubGitEnv() {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "GIT_") {
			os.Unsetenv(name)
		}
	}
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// gitIn runs git in dir as an invented author and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-C", dir,
		"-c", "user.name=Sample Author",
		"-c", "user.email=author@example.test",
		"-c", "commit.gpgsign=false",
	}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// newRepo makes an empty repository with one commit, so later changes have a
// parent to be measured against.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "README.md", "sample\n")
	gitIn(t, dir, "add", "--", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "Start the sample")
	return dir
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

// commitFile writes, stages and commits one file.
func commitFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	write(t, dir, rel, content)
	gitIn(t, dir, "add", "--", rel)
	gitIn(t, dir, "commit", "-q", "-m", "Add "+rel)
}

// stage writes and stages one file without committing it.
func stage(t *testing.T, dir, rel, content string) {
	t.Helper()
	write(t, dir, rel, content)
	gitIn(t, dir, "add", "--", rel)
}

// staged returns the staged change of dir, failing the test on error.
func staged(t *testing.T, dir string) []Change {
	t.Helper()
	changes, err := Staged(dir)
	if err != nil {
		t.Fatal(err)
	}
	return changes
}

// checkerFor returns the Runner that checks dir's staged content, and closes
// it when the test ends.
func checkerFor(t *testing.T, dir string) Runner {
	t.Helper()
	c := NewChecker(dir)
	t.Cleanup(func() { c.Close() })
	return c.Run
}

// parse reads a table written inline in a test.
func parse(t *testing.T, text string) Table {
	t.Helper()
	table, err := ParseTable(strings.NewReader(text))
	if err != nil {
		t.Fatalf("the test table does not parse: %v\n%s", err, text)
	}
	return table
}

// findingNames lists the names in findings, in order.
func findingNames(findings []Finding) []string {
	var names []string
	for _, f := range findings {
		names = append(names, f.Name)
	}
	return names
}

// recorder is a Runner that records each command and answers from a map keyed
// by the command's first word; a command not in the map passes.
type recorder struct {
	calls  []string
	status map[string]error
}

func (r *recorder) run(argv []string) (string, error) {
	r.calls = append(r.calls, strings.Join(argv, " "))
	return "", r.status[argv[0]]
}
