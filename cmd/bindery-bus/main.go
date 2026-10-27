// Command bindery-bus is a message bus between named sessions on one machine.
//
//	bindery-bus tell FROM TO TEXT...
//	bindery-bus wait [--timeout DURATION] NAME
//	bindery-bus who
//
// tell appends a message to TO's inbox. wait blocks until NAME has an unread
// message, prints it and marks it read. who lists the names that have an inbox,
// and marks a name whose files cannot be read as broken without hiding the rest.
//
// Exit codes: 0 when the command did its job (a wait that printed a message
// included), 1 when a wait timed out with nothing to print, and 2 when the
// command could not do its job, for example because a name is not valid, the
// bus directory cannot be used, or the arguments are wrong.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/kingletas/bindery/internal/bus"
	"github.com/kingletas/bindery/internal/version"
)

const (
	exitOK       = 0
	exitTimedOut = 1
	exitBroken   = 2

	// EnvDir names the bus directory.
	EnvDir = "BINDERY_BUS_DIR"

	defaultTimeout = 30 * time.Second

	usage = `usage: bindery-bus tell FROM TO TEXT...
       bindery-bus wait [--timeout DURATION] NAME
       bindery-bus who`
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, version.String())
		return exitOK
	}
	if len(args) == 0 {
		return misuse(stderr)
	}

	// The arguments are checked before the directory is touched, so a mistyped
	// command makes nothing.
	var call func(*bus.Bus) int
	switch args[0] {
	case "tell":
		if len(args) < 4 {
			return misuse(stderr)
		}
		from, to, text := args[1], args[2], strings.Join(args[3:], " ")
		call = func(b *bus.Bus) int { return tell(b, from, to, text, stdout, stderr) }
	case "wait":
		name, timeout, ok := parseWait(args[1:])
		if !ok {
			return misuse(stderr)
		}
		call = func(b *bus.Bus) int { return wait(b, name, timeout, stdout, stderr) }
	case "who":
		if len(args) != 1 {
			return misuse(stderr)
		}
		call = func(b *bus.Bus) int { return who(b, stdout, stderr) }
	default:
		return misuse(stderr)
	}

	b, err := bus.Open(busDir(getenv))
	if err != nil {
		return broken(stderr, "directory", err)
	}
	return call(b)
}

// busDir is the directory named by BINDERY_BUS_DIR, or a private one in the
// temporary directory that is named for the current user.
func busDir(getenv func(string) string) string {
	if dir := getenv(EnvDir); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("bindery-bus-%d", os.Getuid()))
}

// parseWait reads the arguments after "wait". The flag comes before the name.
func parseWait(args []string) (name string, timeout time.Duration, ok bool) {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.DurationVar(&timeout, "timeout", defaultTimeout, "")
	if fs.Parse(args) != nil || fs.NArg() != 1 || timeout < 0 {
		return "", 0, false
	}
	return fs.Arg(0), timeout, true
}

func tell(b *bus.Bus, from, to, text string, stdout, stderr io.Writer) int {
	msg, err := b.Tell(from, to, text)
	if err != nil {
		return broken(stderr, "tell", err)
	}
	fmt.Fprintf(stdout, "told %s: message %d\n", to, msg.Seq)
	return exitOK
}

func wait(b *bus.Bus, name string, timeout time.Duration, stdout, stderr io.Writer) int {
	msg, err := b.Wait(name, timeout)
	if errors.Is(err, bus.ErrTimeout) {
		fmt.Fprintf(stderr, "bindery-bus: wait: nothing arrived for %s within %s.\n", name, timeout)
		return exitTimedOut
	}
	if err != nil {
		return broken(stderr, "wait", err)
	}
	fmt.Fprintf(stdout, "#%d %s: %s\n", msg.Seq, msg.From, msg.Text)
	return exitOK
}

func who(b *bus.Bus, stdout, stderr io.Writer) int {
	rows, err := b.Who()
	if err != nil {
		return broken(stderr, "who", err)
	}
	if len(rows) == 0 {
		return exitOK
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tUNREAD\tLAST TOLD\tLAST WAITED")
	for _, r := range rows {
		if r.Broken != "" {
			fmt.Fprintf(w, "%s\tbroken: %s\n", r.Name, r.Broken)
			continue
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", r.Name, r.Unread, stamp(r.LastTold), stamp(r.LastWaited))
	}
	w.Flush()
	return exitOK
}

// stamp is a time to the second in UTC, or "-" for one that never happened.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func misuse(stderr io.Writer) int {
	fmt.Fprintln(stderr, usage)
	return exitBroken
}

func broken(stderr io.Writer, what string, err error) int {
	oneLine := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "bindery-bus: %s: %s\n", what, oneLine)
	return exitBroken
}
