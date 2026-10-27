package guard

import (
	"os"
	"path"
	"strings"
)

// Environment variables that configure the guards. Each is optional.
const (
	EnvWorkspaceRoots    = "BINDERY_WORKSPACE_ROOTS"
	EnvProtectedBranches = "BINDERY_PROTECTED_BRANCHES"
	EnvSecretPatterns    = "BINDERY_SECRET_PATTERNS"
)

// Config holds the values the guards compare a command against. Nothing in it
// names a real machine: every value is supplied by the caller or defaulted.
type Config struct {
	// Home is the home directory. It protects itself from recursive deletes
	// and is what $HOME and ~ stand for in a command.
	Home string
	// Cwd is the directory relative paths are resolved against when the tool
	// call does not carry its own.
	Cwd string
	// WorkspaceRoots are the directories a recursive delete may reach into.
	WorkspaceRoots []string
	// ProtectedBranches are branch names, or path.Match patterns, that must
	// not be force-pushed.
	ProtectedBranches []string
	// SecretPatterns are path.Match patterns matched against every directory
	// name and the file name of a path.
	SecretPatterns []string
}

// DefaultProtectedBranches are used when the environment does not set any.
var DefaultProtectedBranches = []string{"main", "master"}

// DefaultSecretPatterns are used when the environment does not set any.
var DefaultSecretPatterns = []string{
	".env", ".env.*", "*.pem", "*.key", "id_rsa", "id_ecdsa", "id_ed25519", ".ssh", ".gnupg",
}

// templateSuffixes mark a file as a template that holds no secret.
var templateSuffixes = []string{".example", ".sample", ".template", ".dist"}

// ConfigFromEnv builds a Config from environment variables. getenv is
// os.Getenv in the runner, and cwd is the process's working directory, which is
// the workspace root when none is configured.
func ConfigFromEnv(getenv func(string) string, cwd string) Config {
	cfg := Config{
		Home:              getenv("HOME"),
		Cwd:               cwd,
		ProtectedBranches: DefaultProtectedBranches,
		SecretPatterns:    DefaultSecretPatterns,
	}
	if roots := splitList(getenv(EnvWorkspaceRoots), string(os.PathListSeparator)); len(roots) > 0 {
		cfg.WorkspaceRoots = roots
	} else if cwd != "" {
		cfg.WorkspaceRoots = []string{cwd}
	}
	if branches := splitList(getenv(EnvProtectedBranches), ","); len(branches) > 0 {
		cfg.ProtectedBranches = branches
	}
	if patterns := splitList(getenv(EnvSecretPatterns), ","); len(patterns) > 0 {
		cfg.SecretPatterns = patterns
	}
	return cfg
}

func splitList(s, sep string) []string {
	var out []string
	for _, item := range strings.Split(s, sep) {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// cleanRoots returns the configured roots as clean absolute paths. A relative
// root is taken relative to Cwd, and one that cannot be made absolute is
// dropped, which leaves fewer places a delete is allowed.
func (c Config) cleanRoots() []string {
	var out []string
	for _, r := range c.WorkspaceRoots {
		if !path.IsAbs(r) {
			if c.Cwd == "" {
				continue
			}
			r = path.Join(c.Cwd, r)
		}
		out = append(out, path.Clean(r))
	}
	return out
}
