package guard

import (
	"slices"
	"testing"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigDefaultsNameNoRealMachine(t *testing.T) {
	cfg := ConfigFromEnv(envFrom(map[string]string{"HOME": "/home/sample"}), "/work/app")
	if cfg.Home != "/home/sample" || cfg.Cwd != "/work/app" {
		t.Fatalf("home and cwd = %q, %q", cfg.Home, cfg.Cwd)
	}
	if !slices.Equal(cfg.WorkspaceRoots, []string{"/work/app"}) {
		t.Errorf("roots = %v, want the working directory", cfg.WorkspaceRoots)
	}
	if !slices.Equal(cfg.ProtectedBranches, DefaultProtectedBranches) {
		t.Errorf("branches = %v", cfg.ProtectedBranches)
	}
	if !slices.Equal(cfg.SecretPatterns, DefaultSecretPatterns) {
		t.Errorf("patterns = %v", cfg.SecretPatterns)
	}
}

func TestConfigReadsTheEnvironment(t *testing.T) {
	cfg := ConfigFromEnv(envFrom(map[string]string{
		"HOME":               "/home/sample",
		EnvWorkspaceRoots:    "/srv/a:/srv/b",
		EnvProtectedBranches: "trunk, release/*",
		EnvSecretPatterns:    "*.safe,tokens",
	}), "/work/app")
	if !slices.Equal(cfg.WorkspaceRoots, []string{"/srv/a", "/srv/b"}) {
		t.Errorf("roots = %v", cfg.WorkspaceRoots)
	}
	if !slices.Equal(cfg.ProtectedBranches, []string{"trunk", "release/*"}) {
		t.Errorf("branches = %v", cfg.ProtectedBranches)
	}
	if !slices.Equal(cfg.SecretPatterns, []string{"*.safe", "tokens"}) {
		t.Errorf("patterns = %v", cfg.SecretPatterns)
	}
}

func TestConfigWithNoRootAndNoCwdAllowsNoDelete(t *testing.T) {
	cfg := ConfigFromEnv(envFrom(nil), "")
	if len(cfg.cleanRoots()) != 0 {
		t.Fatalf("roots = %v, want none", cfg.cleanRoots())
	}
}

func TestRelativeRootsResolveAgainstTheWorkingDirectory(t *testing.T) {
	cfg := Config{Cwd: "/work/app", WorkspaceRoots: []string{"sub/dir", "/abs/", "../x"}}
	want := []string{"/work/app/sub/dir", "/abs", "/work/x"}
	if got := cfg.cleanRoots(); !slices.Equal(got, want) {
		t.Fatalf("roots = %v, want %v", got, want)
	}
}
