// Package shellscan reads a shell command line with a real parser and lists
// every command it would run, wherever the command sits in the line.
//
// The parser is mvdan.cc/sh/v3/syntax in its bash dialect. A line it cannot
// parse is an error, and a part it parses but cannot read statically (a command
// name built from a variable, a script text produced at run time) is recorded in
// Scan.Unreadable. Callers are expected to refuse on either.
package shellscan

import (
	"fmt"
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// maxDepth bounds how many times a command string may sit inside another one,
// as in bash -c 'bash -c ...'.
const maxDepth = 8

// Options configures a scan.
type Options struct {
	// Home is what $HOME and a leading ~ stand for. When it is empty both are
	// treated as values the scanner cannot read.
	Home string
}

// Arg is one word of a command as far as the scanner could read it.
type Arg struct {
	// Text is the word after quote removal and expansion. It is empty when
	// Known is false.
	Text string
	// Known is false when the word depends on something only the running
	// shell knows, such as a variable or a command substitution.
	Known bool
	// MaySplit is set on a word that is not Known and that the shell may split
	// into several words at run time: an unquoted variable or substitution, a
	// brace list, or "$@". Such a word can hide a flag and its operand together.
	// A quoted word stays one word.
	MaySplit bool
	// Inner lists the commands that run inside $(...), backquotes or <(...)
	// within this word.
	Inner []Invocation
}

// PipeTag places a command inside one stage of one pipeline.
type PipeTag struct {
	ID    int
	Stage int
}

// Invocation is one command the line would run.
type Invocation struct {
	// Name is the command's base name, so /usr/bin/rm and rm both read "rm".
	// It is empty when the name is not Known.
	Name  string
	Known bool
	Args  []Arg
	// Inputs are the words after a < redirection on the command.
	Inputs []Arg
	// Pipes lists the pipeline stages the command sits in, outermost first.
	Pipes []PipeTag
}

// Scan is everything found in one command line.
type Scan struct {
	Invocations []Invocation
	// Unreadable says, for each part the scanner could not read, why.
	Unreadable []string
}

// Parse reads a command line. It returns an error when the line is not valid
// bash, which includes syntax that only other shells accept.
func Parse(command string, opt Options) (scan *Scan, err error) {
	defer func() {
		if r := recover(); r != nil {
			scan, err = nil, fmt.Errorf("the parser failed on this line: %v", r)
		}
	}()
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, err
	}
	s := &scanner{opt: opt, scan: &Scan{}}
	s.node(file, nil)
	return s.scan, nil
}

type scanner struct {
	opt    Options
	scan   *Scan
	nextID int
	depth  int
}

// context is what a command inherits from the statement around it.
type context struct {
	tags []PipeTag
	// script is the text a shell would read from a here-document or
	// here-string, and scriptUnreadable says such a text exists but is not
	// static.
	script           *string
	scriptUnreadable bool
	inputs           []Arg
}

func (s *scanner) unreadable(why string) {
	s.scan.Unreadable = append(s.scan.Unreadable, why)
}

// node walks any syntax node, recording each command found under it.
func (s *scanner) node(n syntax.Node, tags []PipeTag) {
	syntax.Walk(n, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.Stmt:
			switch cmd := x.Cmd.(type) {
			case *syntax.CallExpr:
				s.call(x, cmd, tags)
				return false
			case *syntax.BinaryCmd:
				if cmd.Op == syntax.Pipe || cmd.Op == syntax.PipeAll {
					s.pipeline(x, tags)
					return false
				}
			}
			return true
		case *syntax.CmdSubst:
			s.stmts(x.Stmts)
			return false
		case *syntax.ProcSubst:
			s.stmts(x.Stmts)
			return false
		}
		return true
	})
}

func (s *scanner) stmts(list []*syntax.Stmt) {
	for _, st := range list {
		s.node(st, nil)
	}
}

// nested scans the substitutions inside n and returns the commands they hold.
func (s *scanner) nested(n syntax.Node) []Invocation {
	start := len(s.scan.Invocations)
	syntax.Walk(n, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.CmdSubst:
			s.stmts(x.Stmts)
			return false
		case *syntax.ProcSubst:
			s.stmts(x.Stmts)
			return false
		}
		return true
	})
	return append([]Invocation(nil), s.scan.Invocations[start:]...)
}

// pipeline scans each stage of a | b | c with its place in the pipeline.
func (s *scanner) pipeline(st *syntax.Stmt, tags []PipeTag) {
	id := s.nextID
	s.nextID++
	for i, stage := range pipeStages(st) {
		s.node(stage, withTag(tags, PipeTag{ID: id, Stage: i}))
	}
	for _, r := range st.Redirs {
		s.nested(r)
	}
}

func pipeStages(st *syntax.Stmt) []*syntax.Stmt {
	if be, ok := st.Cmd.(*syntax.BinaryCmd); ok && (be.Op == syntax.Pipe || be.Op == syntax.PipeAll) {
		return append(pipeStages(be.X), pipeStages(be.Y)...)
	}
	return []*syntax.Stmt{st}
}

func withTag(tags []PipeTag, t PipeTag) []PipeTag {
	out := make([]PipeTag, 0, len(tags)+1)
	out = append(out, tags...)
	return append(out, t)
}

func (s *scanner) call(st *syntax.Stmt, c *syntax.CallExpr, tags []PipeTag) {
	for _, a := range c.Assigns {
		s.nested(a)
	}
	var args []Arg
	for _, w := range c.Args {
		args = append(args, s.args(w)...)
	}
	ctx := context{tags: tags}
	for _, r := range st.Redirs {
		s.redirect(r, &ctx)
	}
	s.invoke(args, ctx)
}

// redirect records what a redirection feeds to the command: input words, and
// the script text of a here-document or here-string.
func (s *scanner) redirect(r *syntax.Redirect, ctx *context) {
	s.nested(r)
	switch r.Op {
	case syntax.RdrIn:
		ctx.inputs = append(ctx.inputs, s.fields(r.Word)...)
	case syntax.WordHdoc:
		fields := s.fields(r.Word)
		ctx.script, ctx.scriptUnreadable = joinKnown(fields)
	case syntax.Hdoc, syntax.DashHdoc:
		text, ok := s.document(r.Hdoc)
		if ok {
			ctx.script = &text
		} else {
			ctx.scriptUnreadable = true
		}
	}
}

func joinKnown(args []Arg) (text *string, unreadable bool) {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if !a.Known {
			return nil, true
		}
		parts = append(parts, a.Text)
	}
	joined := strings.Join(parts, " ")
	return &joined, false
}

// invoke records one command, then looks through wrappers and shells to the
// commands they run.
func (s *scanner) invoke(args []Arg, ctx context) {
	if len(args) == 0 {
		return
	}
	inv := Invocation{Args: args[1:], Inputs: ctx.inputs, Pipes: ctx.tags}
	if !args[0].Known {
		s.unreadable("a command name is built at run time")
		s.scan.Invocations = append(s.scan.Invocations, inv)
		return
	}
	inv.Known = true
	inv.Name = path.Base(args[0].Text)
	s.scan.Invocations = append(s.scan.Invocations, inv)

	rest := args[1:]
	switch {
	case IsShell(inv.Name):
		s.shell(rest, ctx)
	case inv.Name == "eval":
		s.eval(rest, ctx)
	case inv.Name == "find":
		s.find(rest, ctx)
	case inv.Name == "xargs":
		s.xargs(rest, ctx)
	case inv.Name == "alias":
		s.alias(rest, ctx)
	case inv.Name == "trap":
		s.trap(rest, ctx)
	default:
		if spec, ok := wrappers[inv.Name]; ok {
			s.wrap(inv.Name, spec, rest, ctx)
		} else if !isRunnable(inv.Name) && !wordsOnly[inv.Name] {
			s.runner(rest, ctx)
		}
	}
}

func (s *scanner) shell(rest []Arg, ctx context) {
	sa := ParseShellArgs(rest)
	if !sa.HasCommand {
		// Without -c yet, a word that may split could still bring -c and a
		// command string with it.
		for _, a := range rest {
			if !a.Known && a.MaySplit {
				s.unreadable("a shell is given words built at run time that may hold -c and a command")
				return
			}
		}
	}
	switch {
	case sa.HasCommand:
		s.commandString(sa.Command, ctx)
	case sa.ReadsStdin:
		switch {
		case ctx.script != nil:
			s.parseString(*ctx.script, ctx)
		case ctx.scriptUnreadable:
			s.unreadable("a shell reads a script whose text is built at run time")
		case inPipeTail(ctx.tags):
			s.unreadable("a shell reads its script from a pipe")
		}
	}
}

// inPipeTail reports whether the command receives another command's output.
func inPipeTail(tags []PipeTag) bool {
	for _, t := range tags {
		if t.Stage > 0 {
			return true
		}
	}
	return false
}

func (s *scanner) eval(rest []Arg, ctx context) {
	if len(rest) == 0 {
		return
	}
	text, unreadable := joinKnown(rest)
	if unreadable {
		s.unreadable("eval is given text built at run time")
		return
	}
	s.parseString(*text, ctx)
}

// commandString parses the string given to a shell's -c.
func (s *scanner) commandString(a Arg, ctx context) {
	if !a.Known {
		s.unreadable("a shell is given a command string built at run time")
		return
	}
	s.parseString(a.Text, ctx)
}

func (s *scanner) parseString(text string, ctx context) {
	if s.depth >= maxDepth {
		s.unreadable("command strings are nested too deeply")
		return
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "")
	if err != nil {
		s.unreadable("a command string inside the line does not parse as bash")
		return
	}
	s.depth++
	s.node(file, ctx.tags)
	s.depth--
}
