package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kingletas/bindery/internal/guard"
)

// demoConfig matches what the Makefile's demo target sets in the environment.
func demoConfig() guard.Config {
	return guard.ConfigFromEnv(func(k string) string {
		return map[string]string{
			"HOME":                  "/home/demo",
			guard.EnvWorkspaceRoots: "/srv/demo/app",
		}[k]
	}, "/srv/demo/app")
}

func runOn(t *testing.T, input string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, strings.NewReader(input), &out, &errb, demoConfig())
	return code, out.String(), errb.String()
}

func example(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestASilentCallExitsZeroAndPrintsNothing(t *testing.T) {
	code, stdout, stderr := runOn(t, example(t, "silent.json"))
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("code %d, stdout %q, stderr %q; want 0 and no output", code, stdout, stderr)
	}
}

func TestARefusedCallExitsTwoAndNamesTheGuardOnStderr(t *testing.T) {
	code, stdout, stderr := runOn(t, example(t, "refused.json"))
	if code != 2 || stdout != "" {
		t.Fatalf("code %d, stdout %q; want 2 and an empty stdout", code, stdout)
	}
	want := `bindery-guard: recursive-delete: Recursive delete of "/srv/demo/data" is refused, because it is outside the workspace root.` + "\n"
	if stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

func TestACallItCannotParseExitsTwoSayingItCouldNotReadIt(t *testing.T) {
	code, _, stderr := runOn(t, example(t, "unreadable.json"))
	if code != 2 || !strings.HasPrefix(stderr, "bindery-guard: reader: could not read the command") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}

func TestMalformedJSONExitsTwo(t *testing.T) {
	for _, input := range []string{"", "not json", "{", `{"tool_name":"Bash"}`, `[]`} {
		code, stdout, stderr := runOn(t, input)
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "bindery-guard: input: ") {
			t.Errorf("input %q: code %d, stdout %q, stderr %q", input, code, stdout, stderr)
		}
		if strings.Count(stderr, "\n") != 1 {
			t.Errorf("input %q: stderr should be one line, got %q", input, stderr)
		}
	}
}

func TestVersionAndUsage(t *testing.T) {
	if code, stdout, _ := runOn(t, "", "--version"); code != 0 || stdout != "bindery dev\n" {
		t.Errorf("--version: code %d, stdout %q", code, stdout)
	}
	if code, _, stderr := runOn(t, "", "extra"); code != 1 || !strings.Contains(stderr, "usage:") {
		t.Errorf("an argument: code %d, stderr %q", code, stderr)
	}
}
