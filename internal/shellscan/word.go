package shellscan

import (
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// maxBraces bounds how many brace groups one word may hold before the scanner
// declines to expand it, since each group multiplies the number of fields.
const maxBraces = 6

// args reads a word as command arguments and records the commands that run
// inside its substitutions.
func (s *scanner) args(w *syntax.Word) []Arg {
	inner := s.nested(w)
	out := s.fields(w)
	if len(inner) > 0 {
		if len(out) == 0 {
			out = []Arg{{}}
		}
		out[0].Inner = inner
	}
	return out
}

// fields expands a word the way a shell would, as far as that can be done
// without running anything. A word that depends on run-time state comes back
// as one Arg that is not Known.
func (s *scanner) fields(w *syntax.Word) []Arg {
	if !s.static(w) {
		return []Arg{{MaySplit: maySplit(w)}}
	}
	texts, err := expand.Fields(s.expandConfig(), w)
	if err != nil {
		return []Arg{{MaySplit: maySplit(w)}}
	}
	out := make([]Arg, len(texts))
	for i, t := range texts {
		out[i] = Arg{Text: t, Known: true}
	}
	return out
}

// document reads the body of a here-document.
func (s *scanner) document(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", true
	}
	if !s.static(w) {
		return "", false
	}
	text, err := expand.Document(s.expandConfig(), w)
	if err != nil {
		return "", false
	}
	return text, true
}

// expandConfig lets only $HOME and ~ resolve. An unset name is an error, so a
// word that mentions one is never read as an empty string.
func (s *scanner) expandConfig() *expand.Config {
	return &expand.Config{
		NoUnset: true,
		Env: expand.FuncEnviron(func(name string) string {
			if name == "HOME" {
				return s.opt.Home
			}
			return ""
		}),
	}
}

// static reports whether a word's value is fixed by its text. Only a plain
// $HOME (when a home is configured), a leading ~ or ~/, quoting and bounded
// brace lists qualify. Everything else is decided by the running shell.
func (s *scanner) static(w *syntax.Word) bool {
	ok := true
	braces := 0
	syntax.Walk(w, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.ParamExp:
			if !(isPlainHome(x) && s.opt.Home != "") {
				ok = false
			}
			return false
		case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.ArithmExp, *syntax.ExtGlob:
			ok = false
			return false
		case *syntax.Lit:
			braces += strings.Count(x.Value, "{")
			if strings.Contains(x.Value, "{") && strings.Contains(x.Value, "..") {
				ok = false
			}
		}
		return ok
	})
	if braces > maxBraces {
		ok = false
	}
	if ok && len(w.Parts) > 0 {
		if lit, isLit := w.Parts[0].(*syntax.Lit); isLit && strings.HasPrefix(lit.Value, "~") {
			rest := strings.TrimPrefix(lit.Value, "~")
			if (rest != "" && !strings.HasPrefix(rest, "/")) || s.opt.Home == "" {
				ok = false
			}
		}
	}
	return ok
}

// maySplit reports whether a word can turn into several words at run time:
// it holds an unquoted expansion or brace list, or a quoted "$@" or "${a[@]}".
func maySplit(w *syntax.Word) bool {
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst, *syntax.ExtGlob:
			return true
		case *syntax.Lit:
			if strings.Contains(p.Value, "{") {
				return true
			}
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				if pe, ok := inner.(*syntax.ParamExp); ok && expandsToAllArguments(pe) {
					return true
				}
			}
		}
	}
	return false
}

// expandsToAllArguments reports whether a parameter expansion is $@ or ${a[@]},
// which produce one word per element even inside double quotes.
func expandsToAllArguments(p *syntax.ParamExp) bool {
	if p.Param != nil && p.Param.Value == "@" {
		return true
	}
	index, ok := p.Index.(*syntax.Word)
	if !ok {
		return false
	}
	lit, ok := firstLit(index)
	return ok && lit == "@"
}

func firstLit(w *syntax.Word) (string, bool) {
	if len(w.Parts) != 1 {
		return "", false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return "", false
	}
	return lit.Value, true
}

func isPlainHome(p *syntax.ParamExp) bool {
	return p.Param != nil && p.Param.Value == "HOME" && p.Flags == nil &&
		!p.Excl && !p.Length && !p.Width && !p.IsSet && p.NestedParam == nil &&
		p.Index == nil && len(p.Modifiers) == 0 && p.Slice == nil &&
		p.Repl == nil && p.Names == 0 && p.Exp == nil
}
