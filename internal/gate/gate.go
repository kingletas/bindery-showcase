package gate

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Names of the two rules that are not lanes but can still refuse a commit.
const (
	UnclaimedName   = "unclaimed"
	TestChangedName = "test-changed"
)

// Finding is one reason to refuse a commit: the lane or rule that failed, and
// one sentence saying why.
type Finding struct {
	Name   string
	Reason string
}

// Runner runs a check given as an argument vector and returns what it printed
// and an error when it did not exit 0.
type Runner func(argv []string) (output string, err error)

// ExecRunner runs checks directly, without a shell, in dir.
func ExecRunner(dir string) Runner {
	return func(argv []string) (string, error) {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
}

// Evaluate judges a staged change and its commit message against the table.
// haveMessage is false when no message was given, which is how a run by hand
// differs from a commit-msg hook. Findings come back in a fixed order:
// unclaimed paths, the test rule, then the failing lanes in table order.
func Evaluate(changes []Change, message string, haveMessage bool, t Table, run Runner) []Finding {
	var findings []Finding

	matched := map[string]bool{}
	var unclaimed []string
	seen := map[string]bool{}
	for _, c := range changes {
		for _, p := range c.Paths() {
			if seen[p] {
				continue
			}
			seen[p] = true
			lanes := t.Route(p)
			if len(lanes) == 0 {
				unclaimed = append(unclaimed, p)
			}
			for _, lane := range lanes {
				matched[lane.Name] = true
			}
		}
	}
	if len(unclaimed) > 0 {
		findings = append(findings, Finding{UnclaimedName, unclaimedReason(unclaimed)})
	}
	if reason := testChangedRule(changes, message, haveMessage); reason != "" {
		findings = append(findings, Finding{TestChangedName, reason})
	}
	for _, lane := range t.Lanes {
		if !matched[lane.Name] {
			continue
		}
		if reason := runLane(lane, run); reason != "" {
			findings = append(findings, Finding{lane.Name, reason})
		}
	}
	return findings
}

func unclaimedReason(paths []string) string {
	const shown = 3
	list := make([]string, 0, shown)
	for _, p := range paths[:min(shown, len(paths))] {
		list = append(list, fmt.Sprintf("%q", p))
	}
	text := strings.Join(list, ", ")
	if len(paths) > shown {
		text += fmt.Sprintf(" and %d more", len(paths)-shown)
	}
	return fmt.Sprintf("No lane claims %s, so the commit is refused until the table covers it.", text)
}

// runLane runs a lane's check once and returns "" when it exits 0, or one
// sentence saying how it did not.
func runLane(lane Lane, run Runner) string {
	command := strings.Join(lane.Check, " ")
	output, err := run(lane.Check)
	if err == nil {
		return ""
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return fmt.Sprintf("The check %q could not run: %v.", command, err)
	}
	reason := fmt.Sprintf("The check %q exited with status %d", command, exit.ExitCode())
	if exit.ExitCode() < 0 {
		reason = fmt.Sprintf("The check %q was stopped before it exited", command)
	}
	if last := lastLine(output); last != "" {
		return reason + ", saying: " + last
	}
	return reason + "."
}

// lastLine returns the last non-blank line of s, cut to a length that fits on
// one terminal line, with a full stop at the end.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, lines[len(lines)-1]))
	if line == "" {
		return ""
	}
	const limit = 120
	if utf8.RuneCountInString(line) > limit {
		line = string([]rune(line)[:limit]) + "..."
	}
	return strings.TrimRight(line, ".") + "."
}
