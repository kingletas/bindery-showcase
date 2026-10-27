package gate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Checker runs lane checks against the index that is being committed, not
// against the working tree. On the first check it writes the index into a
// fresh temporary directory with git checkout-index and runs every check there,
// so a broken file that is not staged cannot refuse a clean commit and a broken
// staged file cannot pass because its fix was left unstaged. Close removes the
// directory.
type Checker struct {
	root string
	dir  string
}

// NewChecker returns a Checker for the repository whose top is root. It writes
// nothing until the first check runs.
func NewChecker(root string) *Checker {
	return &Checker{root: root}
}

// Close removes the temporary directory, if one was made. It is safe to call
// more than once, and is meant to be deferred so that every exit path, a
// failing check and a panic included, removes the directory.
func (c *Checker) Close() error {
	if c.dir == "" {
		return nil
	}
	dir := c.dir
	c.dir = ""
	return os.RemoveAll(dir)
}

// Run is a Runner. Like git, it reads the index from GIT_INDEX_FILE when that
// is set, so a commit with a pathspec is checked as exactly what it commits.
func (c *Checker) Run(argv []string) (string, error) {
	if c.dir == "" {
		if err := c.snapshot(); err != nil {
			return "", fmt.Errorf("could not check out the staged files: %w", err)
		}
	}
	env, err := c.env()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = c.dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (c *Checker) snapshot() error {
	dir, err := os.MkdirTemp("", "bindery-gate-")
	if err != nil {
		return err
	}
	c.dir = dir
	// The prefix must end in a slash, or git treats it as part of a file name.
	_, err = git(c.root, "checkout-index", "--all", "--prefix="+dir+string(filepath.Separator))
	return err
}

// env is the environment a check runs in. The temporary directory is not a
// repository, so GIT_DIR points git commands in a check (git diff --cached
// --check, for one) at the real one, and a relative GIT_INDEX_FILE is made
// absolute because the check no longer runs where git set it.
func (c *Checker) env() ([]string, error) {
	gitDir, err := git(c.root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	env := []string{"GIT_DIR=" + strings.TrimRight(string(gitDir), "\n")}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		switch {
		case name == "GIT_DIR" || name == "GIT_WORK_TREE":
			continue
		case name == "GIT_INDEX_FILE" && !filepath.IsAbs(value):
			kv = name + "=" + filepath.Join(c.root, value)
		}
		env = append(env, kv)
	}
	return env, nil
}
