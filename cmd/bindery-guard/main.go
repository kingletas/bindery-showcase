// Command bindery-guard reads one tool call as JSON on standard input and runs
// every guard against it.
//
// Exit codes: 0 when every guard is silent (nothing is printed), and 2 for
// anything else: a guard or the reader refusing (one line naming it goes to
// standard error), or the runner being started wrongly (the usage goes to
// standard error). A pre-tool-use hook blocks only on 2, so no failure of the
// runner may exit with another non-zero code.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/kingletas/bindery-showcase/internal/guard"
	"github.com/kingletas/bindery-showcase/internal/version"
)

const (
	exitSilent = 0
	exitBlock  = 2
)

func main() {
	cwd, _ := os.Getwd()
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, guard.ConfigFromEnv(os.Getenv, cwd)))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, cfg guard.Config) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, version.String())
		return exitSilent
	}
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: bindery-guard < tool-call.json")
		return exitBlock
	}
	verdict := guard.Evaluate(stdin, cfg, guard.Default())
	if !verdict.Refused {
		return exitSilent
	}
	fmt.Fprintf(stderr, "bindery-guard: %s: %s\n", verdict.Guard, verdict.Reason)
	return exitBlock
}
