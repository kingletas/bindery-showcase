// Package guard decides whether one tool call may run. Each guard reads the
// call and either stays silent or gives one sentence saying why it refuses.
package guard

import (
	"fmt"
	"io"
	"strings"

	"github.com/kingletas/bindery/internal/shellscan"
)

// Input is what a guard is shown.
type Input struct {
	Call   Call
	Config Config
	// Scan holds every command found in a Bash call's command line. It is nil
	// for any other tool.
	Scan *shellscan.Scan
}

// Dir is the directory relative paths in the call are resolved against.
func (in Input) Dir() string {
	if in.Call.Cwd != "" {
		return in.Call.Cwd
	}
	return in.Config.Cwd
}

// Guard is one rule. Check returns "" to stay silent, or a one-sentence reason
// to refuse the call.
type Guard interface {
	Name() string
	Check(in Input) string
}

// Names of the checks that are not guards but can still refuse a call.
const (
	InputName  = "input"
	ReaderName = "reader"
)

// Verdict is the outcome of running every guard against one call.
type Verdict struct {
	Refused bool
	// Guard names who refused: a guard, "input" or "reader".
	Guard  string
	Reason string
}

// Default returns the generic guards in the order they run.
func Default() []Guard {
	return []Guard{
		RecursiveDelete{},
		ForcePush{},
		PipeToShell{},
		SecretsRead{},
	}
}

// Evaluate reads one tool call from r and runs every guard against it. The
// first refusal wins. A call that cannot be read, and a command line that
// cannot be parsed or fully read, are refused too.
func Evaluate(r io.Reader, cfg Config, guards []Guard) (verdict Verdict) {
	// A guard that panics has not said the call is safe, so the call is refused.
	defer func() {
		if rec := recover(); rec != nil {
			verdict = refuse(ReaderName, "a guard failed while reading the call (%v), so it is refused.", rec)
		}
	}()
	call, err := DecodeCall(r)
	if err != nil {
		return refuse(InputName, "%s, so it is refused.", err)
	}
	in := Input{Call: call, Config: cfg}
	if call.ToolName == ShellTool {
		scan, err := shellscan.Parse(call.ToolInput.Command, shellscan.Options{Home: cfg.Home})
		if err != nil {
			return refuse(ReaderName, "could not read the command because it does not parse as bash (%s), so it is refused.", err)
		}
		in.Scan = scan
	}
	for _, g := range guards {
		if reason := g.Check(in); reason != "" {
			return refuse(g.Name(), "%s", reason)
		}
	}
	if in.Scan != nil && len(in.Scan.Unreadable) > 0 {
		return refuse(ReaderName, "could not read the command because %s, so it is refused.", in.Scan.Unreadable[0])
	}
	return Verdict{}
}

func refuse(guard, format string, args ...any) Verdict {
	return Verdict{Refused: true, Guard: guard, Reason: oneLine(fmt.Sprintf(format, args...))}
}

// oneLine keeps a reason on a single line, so whatever reads stderr sees
// exactly one sentence per refusal.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
