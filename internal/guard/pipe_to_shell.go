package guard

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kingletas/bindery/internal/shellscan"
)

// PipeToShell refuses a download that is run as it arrives: curl or wget piped
// into a shell or another interpreter, or handed to one as a command string or
// a process substitution.
type PipeToShell struct{}

func (PipeToShell) Name() string { return "pipe-to-shell" }

var downloaders = map[string]bool{"curl": true, "wget": true, "fetch": true}

// scriptRunners also execute text given to them as an argument.
var scriptRunners = map[string]bool{"eval": true, "source": true, ".": true}

func (PipeToShell) Check(in Input) string {
	if in.Scan == nil {
		return ""
	}
	downloadStage := map[int]int{} // pipeline id -> earliest stage holding a download
	for _, inv := range in.Scan.Invocations {
		if !inv.Known || !downloaders[inv.Name] {
			continue
		}
		for _, tag := range inv.Pipes {
			if stage, seen := downloadStage[tag.ID]; !seen || tag.Stage < stage {
				downloadStage[tag.ID] = tag.Stage
			}
		}
	}
	for _, inv := range in.Scan.Invocations {
		if !inv.Known {
			continue
		}
		if readsStdinAsScript(inv) || hasUnknownOperand(inv) {
			for _, tag := range inv.Pipes {
				if stage, ok := downloadStage[tag.ID]; ok && stage < tag.Stage {
					return fmt.Sprintf("Piping a download into %s is refused, because it would run code that nobody has read.", strconv.Quote(inv.Name))
				}
			}
		}
		if runsItsArguments(inv) && argumentHoldsDownload(inv.Args) {
			return fmt.Sprintf("Running the output of a download through %s is refused, because it would run code that nobody has read.", strconv.Quote(inv.Name))
		}
	}
	return ""
}

// readsStdinAsScript reports whether the command would execute what is piped to it.
func readsStdinAsScript(inv shellscan.Invocation) bool {
	switch {
	case shellscan.IsShell(inv.Name):
		return shellscan.ParseShellArgs(inv.Args).ReadsStdin
	case shellscan.IsScriptInterpreter(inv.Name):
		// The interpreter reads stdin when it is given no script, or a script
		// that names stdin.
		for _, a := range inv.Args {
			switch {
			case !a.Known:
				return false
			case a.Text == "-c" || a.Text == "-e" || a.Text == "-m":
				return false
			case a.Text != "-" && strings.HasPrefix(a.Text, "-"):
			default:
				return shellscan.IsStdinPath(a.Text)
			}
		}
		return true
	}
	return false
}

// hasUnknownOperand reports whether a shell or interpreter was given a word
// only the running shell can resolve. It might name stdin, so downstream of a
// download it is treated as reading the download.
func hasUnknownOperand(inv shellscan.Invocation) bool {
	if !shellscan.IsShell(inv.Name) && !shellscan.IsScriptInterpreter(inv.Name) {
		return false
	}
	for _, a := range inv.Args {
		if !a.Known {
			return true
		}
	}
	return false
}

func runsItsArguments(inv shellscan.Invocation) bool {
	return shellscan.IsShell(inv.Name) || shellscan.IsScriptInterpreter(inv.Name) || scriptRunners[inv.Name]
}

func argumentHoldsDownload(args []shellscan.Arg) bool {
	for _, a := range args {
		for _, inner := range a.Inner {
			if inner.Known && downloaders[inner.Name] {
				return true
			}
		}
	}
	return false
}
