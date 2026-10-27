package shellscan

import "strings"

var shells = map[string]bool{
	"sh": true, "bash": true, "dash": true, "zsh": true, "ksh": true, "ash": true,
}

// IsShell reports whether a command name is a POSIX-style shell.
func IsShell(name string) bool { return shells[name] }

// IsStdinPath reports whether a script operand names standard input: a lone
// dash, or one of the paths the kernel exposes for descriptor 0.
func IsStdinPath(s string) bool {
	return s == "-" || s == "/dev/stdin" || s == "/dev/fd/0" || s == "/proc/self/fd/0"
}

// ShellArgs is what a shell's command line asks it to do.
type ShellArgs struct {
	// HasCommand is true when -c was given a string, and Command is that string.
	HasCommand bool
	Command    Arg
	// ReadsStdin is true when the shell takes its script from standard input:
	// no -c and no script file, or -s, or a script file that names stdin.
	ReadsStdin bool
}

// ParseShellArgs reads the arguments given to sh, bash and the like.
func ParseShellArgs(args []Arg) ShellArgs {
	var out ShellArgs
	var wantsCommand, readsStdin, hasScript bool

	// operand takes a word that is not an option: the string after -c if one
	// is still wanted, otherwise the script file.
	operand := func(a Arg) {
		if wantsCommand && !out.HasCommand {
			out.HasCommand, out.Command = true, a
			return
		}
		if !hasScript && a.Known && IsStdinPath(a.Text) {
			readsStdin = true
		}
		hasScript = true
	}

	optionsEnded := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		t := a.Text
		switch {
		case optionsEnded || !a.Known:
			// A word that cannot be read is taken as an operand, which is the
			// reading that claims the least about standard input.
			operand(a)
		case t == "--":
			optionsEnded = true
		case t == "-o" || t == "+o" || t == "-O" || t == "+O":
			i++
		case strings.HasPrefix(t, "--"):
			// Long options such as --norc take no value.
		case len(t) > 1 && t[0] == '-':
			wantsCommand = wantsCommand || strings.ContainsRune(t, 'c')
			readsStdin = readsStdin || strings.ContainsRune(t, 's')
		case len(t) > 1 && t[0] == '+':
			// +x style options switch things off and never mean -c or -s.
		default:
			operand(a)
		}
	}
	out.ReadsStdin = !out.HasCommand && !wantsCommand && (!hasScript || readsStdin)
	return out
}
