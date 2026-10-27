package shellscan

import (
	"strings"
)

// wrapper describes a command that runs another command given on its own
// command line, so a guard has to look through it.
type wrapper struct {
	// short lists the single-letter options that take a value.
	short string
	// long lists the long options that take a value in the next word.
	long []string
	// lead counts the positional words before the command, such as the
	// duration of timeout.
	lead int
	// assigns says NAME=value words may come before the command (env).
	assigns bool
}

var wrappers = map[string]wrapper{
	"timeout": {short: "sk", long: []string{"signal", "kill-after"}, lead: 1},
	"nice":    {short: "n", long: []string{"adjustment"}},
	"ionice":  {short: "cn", long: []string{"class", "classdata"}},
	"env":     {short: "uCS", long: []string{"unset", "chdir", "split-string"}, assigns: true},
	"nohup":   {},
	"setsid":  {},
	"stdbuf":  {short: "ioe", long: []string{"input", "output", "error"}},
	"sudo": {
		short: "ughpCDRTrtU",
		long:  []string{"user", "group", "host", "prompt", "close-from", "chdir", "chroot", "command-timeout", "role", "type", "other-user"},
	},
	"doas":    {short: "uC"},
	"command": {},
	"builtin": {},
	"exec":    {short: "a"},
}

var xargsSpec = wrapper{
	short: "IdnPLsEa",
	long:  []string{"arg-file", "delimiter", "max-args", "max-procs", "max-lines", "max-chars", "process-slot-var"},
}

// split separates a wrapper's own options from the command it runs. ok is false
// when a word in the option area cannot be read, so the command's start is
// unknown.
func (w wrapper) split(args []Arg) (consumed, inner []Arg, ok bool) {
	lead := w.lead
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !a.Known {
			return nil, nil, false
		}
		t := a.Text
		switch {
		case t == "--":
			consumed = append(consumed, a)
			return consumed, args[i+1:], true
		case strings.HasPrefix(t, "--"):
			consumed = append(consumed, a)
			name := strings.TrimPrefix(t, "--")
			if !strings.Contains(name, "=") && contains(w.long, name) && i+1 < len(args) {
				i++
				consumed = append(consumed, args[i])
			}
		case strings.HasPrefix(t, "-") && len(t) > 1:
			consumed = append(consumed, a)
			for j := 1; j < len(t); j++ {
				if strings.IndexByte(w.short, t[j]) < 0 {
					continue
				}
				if j == len(t)-1 && i+1 < len(args) {
					i++
					consumed = append(consumed, args[i])
				}
				break
			}
		case w.assigns && isAssignment(t):
			consumed = append(consumed, a)
		case lead > 0:
			lead--
			consumed = append(consumed, a)
		default:
			return consumed, args[i:], true
		}
	}
	return consumed, nil, true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func isAssignment(t string) bool {
	eq := strings.IndexByte(t, '=')
	if eq <= 0 {
		return false
	}
	for i, r := range t[:eq] {
		letter := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// wrap looks through a wrapper to the command it runs.
func (s *scanner) wrap(name string, spec wrapper, rest []Arg, ctx context) {
	consumed, inner, ok := spec.split(rest)
	if !ok {
		s.unreadable(name + " is given options that are built at run time")
		return
	}
	switch name {
	case "command":
		if hasText(consumed, "-v") || hasText(consumed, "-V") {
			return
		}
	case "env":
		if text, found := optionValue(consumed, "-S", "--split-string"); found {
			s.parseString(text, ctx)
		}
	case "sudo":
		if hasText(consumed, "-s") || hasText(consumed, "-i") || hasText(consumed, "--shell") || hasText(consumed, "--login") {
			if len(inner) == 0 {
				return
			}
			script, unreadable := joinKnown(inner)
			if unreadable {
				s.unreadable("sudo is given a shell command built at run time")
				return
			}
			s.parseString(*script, ctx)
			return
		}
	}
	s.invoke(inner, ctx)
}

func hasText(args []Arg, text string) bool {
	for _, a := range args {
		if a.Known && a.Text == text {
			return true
		}
	}
	return false
}

// optionValue finds the value of an option among already-split words, in the
// forms -S value, -Svalue, --long value and --long=value.
func optionValue(consumed []Arg, short, long string) (string, bool) {
	for i, a := range consumed {
		switch {
		case a.Text == short || a.Text == long:
			if i+1 < len(consumed) {
				return consumed[i+1].Text, true
			}
		case strings.HasPrefix(a.Text, long+"="):
			return strings.TrimPrefix(a.Text, long+"="), true
		case strings.HasPrefix(a.Text, short) && len(a.Text) > len(short) && !strings.HasPrefix(a.Text, "--"):
			return a.Text[len(short):], true
		}
	}
	return "", false
}

// xargs reads the command xargs runs. Its operands come from standard input,
// so the command is recorded with one operand that is not Known, or with the
// words holding the replace string made unknown.
func (s *scanner) xargs(rest []Arg, ctx context) {
	consumed, inner, ok := xargsSpec.split(rest)
	if !ok {
		s.unreadable("xargs is given options that are built at run time")
		return
	}
	if len(inner) == 0 {
		return
	}
	replace, hasReplace := xargsReplace(consumed)
	cmd := make([]Arg, len(inner), len(inner)+1)
	copy(cmd, inner)
	if hasReplace {
		for i := range cmd {
			if cmd[i].Known && strings.Contains(cmd[i].Text, replace) {
				cmd[i] = Arg{Inner: cmd[i].Inner}
			}
		}
	} else {
		cmd = append(cmd, Arg{})
	}
	s.invoke(cmd, ctx)
}

func xargsReplace(consumed []Arg) (string, bool) {
	for i, a := range consumed {
		t := a.Text
		switch {
		case t == "-I" && i+1 < len(consumed):
			return consumed[i+1].Text, true
		case strings.HasPrefix(t, "-I") && len(t) > 2:
			return t[2:], true
		case t == "-i" || t == "--replace":
			return "{}", true
		case strings.HasPrefix(t, "--replace="):
			return strings.TrimPrefix(t, "--replace="), true
		case strings.HasPrefix(t, "-i") && len(t) > 2 && !strings.HasPrefix(t, "--"):
			return t[2:], true
		}
	}
	return "", false
}

// find reads the commands given to -exec, -execdir, -ok and -okdir. The file
// each one acts on is the placeholder {}, which is not Known.
func (s *scanner) find(rest []Arg, ctx context) {
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if !a.Known || !(a.Text == "-exec" || a.Text == "-execdir" || a.Text == "-ok" || a.Text == "-okdir") {
			continue
		}
		var cmd []Arg
		j := i + 1
		for ; j < len(rest); j++ {
			if rest[j].Known && (rest[j].Text == ";" || rest[j].Text == "+") {
				break
			}
			w := rest[j]
			if w.Known && strings.Contains(w.Text, "{}") {
				w = Arg{Inner: w.Inner}
			}
			cmd = append(cmd, w)
		}
		s.invoke(cmd, ctx)
		i = j
	}
}
