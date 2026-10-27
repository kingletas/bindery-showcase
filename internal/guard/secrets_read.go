package guard

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/kingletas/bindery/internal/shellscan"
)

// SecretsRead refuses a read of a file whose name matches a configured secrets
// pattern, such as .env files or anything under an ssh directory. It covers
// the Read tool and the common commands that print or copy a file. A path that
// is only known at run time is not checked, so this guard is a tripwire for
// the obvious read and not a boundary.
type SecretsRead struct{}

func (SecretsRead) Name() string { return "secrets-read" }

// readers are the commands whose file arguments are read or copied.
var readers = map[string]bool{
	"cat": true, "tac": true, "nl": true, "less": true, "more": true, "head": true,
	"tail": true, "bat": true, "strings": true, "xxd": true, "od": true,
	"hexdump": true, "base64": true, "cp": true, "scp": true, "rsync": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "sed": true,
	"awk": true, "cut": true, "sort": true, "diff": true, "cmp": true,
	"jq": true, "tar": true, "zip": true, "source": true, ".": true,
}

func (SecretsRead) Check(in Input) string {
	var candidates []string
	if in.Call.ToolName == ReadTool && in.Call.ToolInput.FilePath != "" {
		candidates = append(candidates, in.Call.ToolInput.FilePath)
	}
	if in.Scan != nil {
		for _, inv := range in.Scan.Invocations {
			if inv.Known && readers[inv.Name] {
				candidates = append(candidates, knownTexts(inv.Args)...)
			}
			candidates = append(candidates, knownTexts(inv.Inputs)...)
		}
	}
	for _, p := range candidates {
		if pattern, ok := secretPattern(p, in.Config.SecretPatterns); ok {
			return fmt.Sprintf("Reading %s is refused, because its name matches the secrets pattern %s.", strconv.Quote(p), strconv.Quote(pattern))
		}
	}
	return ""
}

func knownTexts(args []shellscan.Arg) []string {
	var out []string
	for _, a := range args {
		if a.Known {
			out = append(out, a.Text)
		}
	}
	return out
}

// secretPattern returns the first pattern that matches the file name or any
// directory name in p. Files that are templates, such as .env.example, match
// nothing.
func secretPattern(p string, patterns []string) (string, bool) {
	p = path.Clean(strings.TrimSpace(p))
	base := path.Base(p)
	for _, suffix := range templateSuffixes {
		if strings.HasSuffix(base, suffix) {
			return "", false
		}
	}
	for _, part := range strings.Split(p, "/") {
		for _, pattern := range patterns {
			if ok, err := path.Match(pattern, part); err == nil && ok {
				return pattern, true
			}
		}
	}
	return "", false
}
