// Command bindery-gate sends the paths a commit changes to lanes, runs each
// matched lane's check, and refuses the commit when a check fails, when no lane
// claims a path, or when an edited test has no "Test changed:" line.
//
// It runs as a git commit-msg hook, which hands it the message file's path, or
// by hand with no argument against what is staged.
//
// Exit codes: 0 when the commit may go ahead (nothing is printed), 1 when the
// gate refuses it (one line per failing lane or rule goes to standard error),
// and 2 when the gate could not do its job, for example because the table is
// malformed. Git blocks a commit on any non-zero hook exit, so a gate that
// fails to run never lets a commit through.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kingletas/bindery/internal/gate"
	"github.com/kingletas/bindery/internal/version"
)

const (
	exitPass    = 0
	exitRefused = 1
	exitBroken  = 2

	// EnvTable names the lanes table. A relative path is read from the
	// repository root.
	EnvTable     = "BINDERY_GATE_TABLE"
	defaultTable = "lanes.tsv"
)

func main() {
	dir, _ := os.Getwd()
	os.Exit(run(os.Args[1:], dir, os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, dir string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, version.String())
		return exitPass
	}
	if len(args) > 1 || (len(args) == 1 && len(args[0]) > 0 && args[0][0] == '-') {
		fmt.Fprintln(stderr, "usage: bindery-gate [commit-message-file]")
		return exitBroken
	}

	var message string
	haveMessage := len(args) == 1
	if haveMessage {
		raw, err := os.ReadFile(args[0])
		if err != nil {
			return broken(stderr, "message", err)
		}
		message = string(raw)
	}

	root, err := gate.RepoRoot(dir)
	if err != nil {
		return broken(stderr, "repository", err)
	}
	table, err := gate.LoadTable(tablePath(root, getenv(EnvTable)))
	if err != nil {
		return broken(stderr, "table", err)
	}
	changes, err := gate.Staged(root)
	if err != nil {
		return broken(stderr, "staged", err)
	}

	findings := gate.Evaluate(changes, message, haveMessage, table, gate.ExecRunner(root))
	for _, f := range findings {
		fmt.Fprintf(stderr, "bindery-gate: %s: %s\n", f.Name, f.Reason)
	}
	if len(findings) > 0 {
		return exitRefused
	}
	return exitPass
}

func tablePath(root, configured string) string {
	if configured == "" {
		configured = defaultTable
	}
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(root, configured)
}

func broken(stderr io.Writer, what string, err error) int {
	oneLine := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "bindery-gate: %s: %s\n", what, oneLine)
	return exitBroken
}
