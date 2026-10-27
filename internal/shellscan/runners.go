package shellscan

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// scriptInterpreters are interpreters other than shells that run text they are given.
var scriptInterpreters = map[string]bool{
	"python": true, "python3": true, "perl": true, "ruby": true, "node": true,
}

// IsScriptInterpreter reports whether a command name is an interpreter such as
// python that is not a shell.
func IsScriptInterpreter(name string) bool { return scriptInterpreters[name] }

// guardedTools are the commands the guards read, apart from shells, the script
// interpreters and the wrappers, which are listed elsewhere. An unknown command
// that names one of these among its words is read as running it.
var guardedTools = setOf(
	"rm", "find", "git", "curl", "wget", "fetch", "eval", "source", ".", "xargs", "alias", "trap",
	// Commands that print or copy the files they are given.
	"cat", "tac", "nl", "less", "more", "head", "tail", "bat", "strings", "xxd", "od",
	"hexdump", "base64", "cp", "scp", "rsync", "grep", "egrep", "fgrep", "rg", "sed",
	"awk", "cut", "sort", "diff", "cmp", "jq", "tar", "zip",
)

// wordsOnly are commands that treat every word they are given as data, so a
// command name among them is not run. An unknown command is read as a possible
// runner, and these are exempt: echo rm -rf / stays silent.
var wordsOnly = setOf(
	"echo", "printf", "print", "pwd", "ls", "dir", "wc", "tr", "uniq", "date", "basename",
	"dirname", "which", "type", "whereis", "whoami", "id", "mkdir", "touch", "cd", "pushd",
	"popd", "test", "[", "true", "false", ":", "read", "export", "set", "unset", "declare",
	"local", "return", "exit", "sleep", "seq", "yes", "man", "help",
)

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// isRunnable reports whether a command name is one the scanner or a guard reads.
func isRunnable(name string) bool {
	if IsShell(name) || scriptInterpreters[name] || guardedTools[name] {
		return true
	}
	_, isWrapper := wrappers[name]
	return isWrapper
}

// runner reads the words of a command nobody knows as the possible start of
// another command. It looks for the first word that names a tool the guards
// read, and reads the rest of the line from there as the command that runs. A
// word that holds spaces or operators is read as a command string, the way
// su -c 'rm -rf x' carries one. A words-only command among the words ends the
// search, since what follows it is data.
func (s *scanner) runner(words []Arg, ctx context) {
	for i, a := range words {
		if !a.Known {
			continue
		}
		text := a.Text
		if strings.HasPrefix(text, "-") {
			// Only a value attached to an option can be a command string.
			_, value, ok := strings.Cut(text, "=")
			if !ok {
				continue
			}
			text = value
		}
		if strings.ContainsAny(text, " \t\n;&|") {
			s.guessString(text, ctx)
			continue
		}
		if strings.HasPrefix(a.Text, "-") {
			continue
		}
		name := path.Base(text)
		if wordsOnly[name] {
			return
		}
		if isRunnable(name) {
			s.invoke(words[i:], ctx)
			return
		}
	}
}

// guessString reads a word of an unknown command as a command string. A word
// that does not parse is ordinary text, so it is skipped rather than refused.
func (s *scanner) guessString(text string, ctx context) {
	if s.depth >= maxDepth {
		s.unreadable("command strings are nested too deeply")
		return
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "")
	if err != nil {
		return
	}
	s.depth++
	s.node(file, ctx.tags)
	s.depth--
}

// alias reads the text each alias stands for as a command string.
func (s *scanner) alias(words []Arg, ctx context) {
	for _, a := range words {
		if !a.Known {
			s.unreadable("alias is given text built at run time")
			continue
		}
		if _, body, ok := strings.Cut(a.Text, "="); ok && !strings.HasPrefix(a.Text, "-") {
			s.parseString(body, ctx)
		}
	}
}

// trap reads its action, the first operand, as a command string.
func (s *scanner) trap(words []Arg, ctx context) {
	if len(words) > 0 && words[0].Known && words[0].Text == "--" {
		words = words[1:]
	}
	if len(words) == 0 {
		return
	}
	action := words[0]
	if action.Known && (action.Text == "-" || action.Text == "-l" || action.Text == "-p") {
		return
	}
	s.commandString(action, ctx)
}
