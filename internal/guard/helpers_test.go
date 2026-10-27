package guard

import (
	"strings"
	"testing"

	"github.com/kingletas/bindery/internal/shellscan"
)

// testConfig is an invented machine: a home directory, one workspace root
// beneath it, and the default branch and secrets lists.
func testConfig() Config {
	return Config{
		Home:              "/home/sample",
		Cwd:               "/home/sample/work/app",
		WorkspaceRoots:    []string{"/home/sample/work/app"},
		ProtectedBranches: DefaultProtectedBranches,
		SecretPatterns:    DefaultSecretPatterns,
	}
}

// shellInput builds the Input a guard sees for a Bash call.
func shellInput(t *testing.T, cfg Config, command string) Input {
	t.Helper()
	scan, err := shellscan.Parse(command, shellscan.Options{Home: cfg.Home})
	if err != nil {
		t.Fatalf("the test command %q does not parse: %v", command, err)
	}
	call := Call{ToolName: ShellTool, ToolInput: ToolInput{Command: command}}
	return Input{Call: call, Config: cfg, Scan: scan}
}

// assertSilent fails when the guard refuses any of the commands.
func assertSilent(t *testing.T, g Guard, cfg Config, commands ...string) {
	t.Helper()
	for _, command := range commands {
		if reason := g.Check(shellInput(t, cfg, command)); reason != "" {
			t.Errorf("%s refused %q, want silence: %s", g.Name(), command, reason)
		}
	}
}

// assertRefuses fails when the guard stays silent on any of the commands, or
// gives a reason that is not one plain sentence.
func assertRefuses(t *testing.T, g Guard, cfg Config, commands ...string) {
	t.Helper()
	for _, command := range commands {
		reason := g.Check(shellInput(t, cfg, command))
		if reason == "" {
			t.Errorf("%s was silent on %q, want a refusal", g.Name(), command)
			continue
		}
		assertPlainSentence(t, reason)
	}
}

func assertPlainSentence(t *testing.T, reason string) {
	t.Helper()
	if strings.ContainsAny(reason, "\n\r") || !strings.HasSuffix(reason, ".") {
		t.Errorf("reason %q is not a single sentence ending in a full stop", reason)
	}
}
