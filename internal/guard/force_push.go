package guard

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/kingletas/bindery-showcase/internal/shellscan"
)

// ForcePush refuses git push when it overwrites or deletes a protected branch:
// a --force style flag, a +refspec, or a delete. A forced push that does not
// name its branch is refused too, because the current branch is not visible
// from the command line.
type ForcePush struct{}

func (ForcePush) Name() string { return "force-push" }

func (ForcePush) Check(in Input) string {
	if in.Scan == nil {
		return ""
	}
	for _, inv := range in.Scan.Invocations {
		if !inv.Known || inv.Name != "git" {
			continue
		}
		args, ok := afterSubcommand(inv.Args, "push")
		if !ok {
			continue
		}
		if reason := checkPush(args, in.Config.ProtectedBranches); reason != "" {
			return reason
		}
	}
	return ""
}

// gitValueOptions are git's global options that take their value as the next word.
var gitValueOptions = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true, "--super-prefix": true, "--config-env": true,
}

// afterSubcommand returns the words after git's subcommand when it is want,
// skipping git's own global options. A word before the subcommand that is not
// known might be one of those options or the subcommand itself, so it is
// skipped and the next word is then accepted as the subcommand.
func afterSubcommand(args []shellscan.Arg, want string) ([]shellscan.Arg, bool) {
	sawUnknown := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case !a.Known:
			sawUnknown = true
		case gitValueOptions[a.Text]:
			i++
		case strings.HasPrefix(a.Text, "-"):
		default:
			return args[i+1:], a.Text == want || sawUnknown
		}
	}
	return nil, false
}

// pushValueOptions are git push options that take their value as the next word.
var pushValueOptions = map[string]bool{
	"-o": true, "--push-option": true, "--repo": true, "--receive-pack": true, "--exec": true,
}

func checkPush(args []shellscan.Arg, protected []string) string {
	var force, deleteFlag, every, tags, unknownWord bool
	var positional []shellscan.Arg
	optionsEnded := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		t := a.Text
		switch {
		case optionsEnded || (a.Known && !strings.HasPrefix(t, "-")):
			positional = append(positional, a)
		case !a.Known:
			// The word might be --force or --delete, so the push is judged as
			// forced: refused when it targets a protected branch or names none.
			// The word is not counted as a remote or a branch.
			unknownWord = true
			force = true
		case t == "--":
			optionsEnded = true
		case t == "--force" || strings.HasPrefix(t, "--force-with-lease") || t == "--force-if-includes":
			force = true
		case t == "--mirror":
			force, every = true, true
		case t == "--tags":
			tags = true
		case t == "--all" || t == "--branches":
			every = true
		case t == "--delete":
			deleteFlag = true
		case pushValueOptions[t]:
			i++
		case !strings.HasPrefix(t, "--"):
			force = force || strings.Contains(t, "f")
			deleteFlag = deleteFlag || strings.Contains(t, "d")
		}
	}

	specs := positional
	if len(specs) > 0 {
		specs = specs[1:] // the first word is the remote
	}
	if every && force {
		return "A force-push of every branch (--all or --mirror) is refused, because it would include protected branches."
	}
	if len(specs) == 0 {
		if force && !tags {
			if unknownWord {
				return "A push with a word that is only known at run time is refused, because the word may force the push and the guard cannot tell which branch it targets."
			}
			return "A force-push that does not name its branch is refused, because the guard cannot tell whether it targets a protected branch."
		}
		return ""
	}
	for _, spec := range specs {
		if reason := checkRefspec(spec, force, deleteFlag, protected); reason != "" {
			return reason
		}
	}
	return ""
}

func checkRefspec(spec shellscan.Arg, force, deleteFlag bool, protected []string) string {
	if !spec.Known {
		if force || deleteFlag {
			return "A force-push to a branch that is only known at run time is refused, because the guard cannot tell whether it is protected."
		}
		return ""
	}
	text := spec.Text
	plus := strings.HasPrefix(text, "+")
	text = strings.TrimPrefix(text, "+")
	src, dst, hasColon := strings.Cut(text, ":")
	if !hasColon {
		dst = src
	}
	deleting := deleteFlag || (hasColon && src == "")
	if !(force || plus || deleting) {
		return ""
	}
	branch := strings.TrimPrefix(dst, "refs/heads/")
	if branch == "" || branch == "HEAD" {
		return "A force-push that does not name its branch is refused, because the guard cannot tell whether it targets a protected branch."
	}
	if !matchesAny(protected, branch) {
		return ""
	}
	if deleting {
		return fmt.Sprintf("Deleting the protected branch %s on a remote is refused.", strconv.Quote(branch))
	}
	return fmt.Sprintf("Force-pushing to the protected branch %s is refused.", strconv.Quote(branch))
}

func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, err := path.Match(p, name); err == nil && ok {
			return true
		}
	}
	return false
}
