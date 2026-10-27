package guard

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/kingletas/bindery/internal/shellscan"
)

// RecursiveDelete refuses rm -r and find -delete aimed at a protected path: the
// filesystem root, the home directory, a workspace root itself, or anything
// outside the configured workspace roots. A target that is only known at run
// time is refused too, since the guard cannot say what it would remove.
type RecursiveDelete struct{}

func (RecursiveDelete) Name() string { return "recursive-delete" }

func (RecursiveDelete) Check(in Input) string {
	if in.Scan == nil {
		return ""
	}
	d := deleteJudge{
		roots:  in.Config.cleanRoots(),
		home:   cleanAbs(in.Config.Home),
		cwd:    in.Dir(),
		movedD: changesDirectory(in.Scan),
	}
	for _, inv := range in.Scan.Invocations {
		if !inv.Known {
			continue
		}
		switch inv.Name {
		case "rm":
			targets, recursive := rmTargets(inv.Args)
			if !recursive {
				continue
			}
			for _, t := range targets {
				if reason := d.judge(t, false); reason != "" {
					return reason
				}
			}
		case "find":
			targets, deletes := findDeleteTargets(inv.Args)
			if !deletes {
				continue
			}
			for _, t := range targets {
				// find removes what matches under the start path, so the start
				// path itself may be a workspace root.
				if reason := d.judge(t, true); reason != "" {
					return reason
				}
			}
		}
	}
	return ""
}

// rmTargets splits rm's words into its operands and says whether a recursive
// flag is present among the words that can be read.
func rmTargets(args []shellscan.Arg) (targets []shellscan.Arg, recursive bool) {
	optionsEnded := false
	for _, a := range args {
		switch {
		case optionsEnded || !a.Known || !strings.HasPrefix(a.Text, "-") || a.Text == "-":
			targets = append(targets, a)
		case a.Text == "--":
			optionsEnded = true
		case a.Text == "--recursive":
			recursive = true
		case !strings.HasPrefix(a.Text, "--") && strings.ContainsAny(a.Text, "rR"):
			recursive = true
		}
	}
	return targets, recursive
}

// findDeleteTargets returns find's start paths and whether it was given -delete.
func findDeleteTargets(args []shellscan.Arg) (starts []shellscan.Arg, deletes bool) {
	expressionBegun := false
	for _, a := range args {
		if a.Known && a.Text == "-delete" {
			deletes = true
		}
		if expressionBegun {
			continue
		}
		if a.Known && (strings.HasPrefix(a.Text, "-") || a.Text == "!" || a.Text == "(") {
			expressionBegun = true
			continue
		}
		starts = append(starts, a)
	}
	if len(starts) == 0 {
		starts = []shellscan.Arg{{Text: ".", Known: true}}
	}
	return starts, deletes
}

// changesDirectory reports whether the line moves the shell around, after which
// a relative path no longer means what the call's directory says.
func changesDirectory(scan *shellscan.Scan) bool {
	for _, inv := range scan.Invocations {
		if inv.Known && (inv.Name == "cd" || inv.Name == "pushd" || inv.Name == "popd") {
			return true
		}
	}
	return false
}

type deleteJudge struct {
	roots  []string
	home   string
	cwd    string
	movedD bool
}

// judge returns a refusal reason for one delete target, or "" when it is safe.
// With inclusive set, a target equal to a workspace root is allowed, which is
// right for a glob or find that only touches what is inside the directory.
func (d deleteJudge) judge(target shellscan.Arg, inclusive bool) string {
	if !target.Known {
		return "Recursive delete of a target that is only known at run time is refused, because the guard cannot tell what it would remove."
	}
	text := target.Text
	dir, glob, ok := d.resolve(text)
	if !ok {
		return fmt.Sprintf("Recursive delete of %s is refused, because the directory a relative path starts from cannot be determined here.", strconv.Quote(text))
	}
	for _, root := range d.roots {
		if within(dir, root) && (inclusive || glob || dir != root) {
			return ""
		}
	}
	if d.isProtected(dir) {
		return fmt.Sprintf("Recursive delete of %s is refused, because it is the filesystem root, the home directory, or a directory that holds the workspace.", strconv.Quote(text))
	}
	return fmt.Sprintf("Recursive delete of %s is refused, because it is outside the workspace root.", strconv.Quote(text))
}

// resolve turns a delete target into the clean absolute directory it names. For
// a glob that is the directory the pattern starts in, and glob is true.
func (d deleteJudge) resolve(text string) (dir string, glob, ok bool) {
	if i := strings.IndexAny(text, "*?["); i >= 0 {
		glob = true
		prefix := text[:i]
		text = prefix[:strings.LastIndex(prefix, "/")+1]
		if text == "" {
			text = "."
		}
	}
	if path.IsAbs(text) {
		return path.Clean(text), glob, true
	}
	if d.cwd == "" || !path.IsAbs(d.cwd) || d.movedD {
		return "", glob, false
	}
	return path.Join(d.cwd, text), glob, true
}

func (d deleteJudge) isProtected(p string) bool {
	if p == "/" || (d.home != "" && within(d.home, p)) {
		return true
	}
	for _, root := range d.roots {
		if within(root, p) {
			return true
		}
	}
	return false
}

// within reports whether p is dir itself or lies beneath it.
func within(p, dir string) bool {
	if dir == "/" {
		return path.IsAbs(p)
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}

func cleanAbs(p string) string {
	if !path.IsAbs(p) {
		return ""
	}
	return path.Clean(p)
}
