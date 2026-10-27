package gate

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Change is one path in the staged change. Status is git's letter: A added, M
// modified, D deleted, R renamed, T type changed.
type Change struct {
	Status byte
	Path   string
	// OldPath is where a renamed file came from, and is empty otherwise.
	OldPath string
}

// Paths returns every path the change touches: both ends of a rename.
func (c Change) Paths() []string {
	if c.OldPath != "" {
		return []string{c.OldPath, c.Path}
	}
	return []string{c.Path}
}

// RepoRoot returns the top of the repository that holds dir.
func RepoRoot(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Staged lists the change git would commit from the index: the one in
// GIT_INDEX_FILE when git has set it, so a commit with a pathspec is judged on
// what it commits and not on everything staged.
func Staged(dir string) ([]Change, error) {
	out, err := git(dir, "diff", "--cached", "--name-status", "-z", "-M", "--no-ext-diff")
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out)
}

// parseNameStatus reads git's NUL-separated name-status output: a status word
// and a path, or a status word and two paths for a rename.
func parseNameStatus(out []byte) ([]Change, error) {
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return nil, nil
	}
	var changes []Change
	for i := 0; i < len(fields); {
		status := fields[i]
		if status == "" {
			return nil, fmt.Errorf("git printed an empty status in %q", out)
		}
		want := 2
		if status[0] == 'R' || status[0] == 'C' {
			want = 3
		}
		if i+want > len(fields) {
			return nil, fmt.Errorf("git printed a truncated change after %q", status)
		}
		c := Change{Status: status[0]}
		if want == 3 {
			c.OldPath, c.Path = fields[i+1], fields[i+2]
		} else {
			c.Path = fields[i+1]
		}
		changes = append(changes, c)
		i += want
	}
	return changes, nil
}

func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
