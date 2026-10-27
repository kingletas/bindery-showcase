// Package gate sends the paths a commit changes to lanes, runs each matched
// lane's check once, and applies one rule across lanes: an edited or deleted
// test needs a reason in the commit message.
package gate

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

// Lane is one named check and the paths it covers.
type Lane struct {
	Name     string
	Patterns []string
	// Check is the command that must exit 0, as an argument vector. It is run
	// directly, never through a shell.
	Check []string
}

// Table is the ordered list of lanes. A path may belong to several lanes.
type Table struct {
	Lanes []Lane
}

// shellSyntax holds the characters a shell would read as syntax. A check that
// holds one is refused, because without a shell it would not do what it says.
const shellSyntax = "\"'`$|&;<>()\\*?{}"

var laneName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ParseTable reads a lanes table: one row per line, three columns separated by
// tabs (lane name, path pattern, check command). Blank lines and lines that
// start with # are skipped. Rows that share a lane name add patterns to that
// lane and must give it the same check. Anything else that is wrong refuses
// the whole table, so a typo cannot leave a path unchecked.
func ParseTable(r io.Reader) (Table, error) {
	var t Table
	index := map[string]int{}
	scanner := bufio.NewScanner(r)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimRight(scanner.Text(), "\r")
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			return Table{}, fmt.Errorf("line %d has %d tab-separated columns, want 3 (lane, pattern, check)", n, len(cols))
		}
		name, pattern, check := strings.TrimSpace(cols[0]), strings.TrimSpace(cols[1]), strings.TrimSpace(cols[2])
		argv, err := checkRow(name, pattern, check)
		if err != nil {
			return Table{}, fmt.Errorf("line %d: %w", n, err)
		}
		i, seen := index[name]
		if !seen {
			index[name] = len(t.Lanes)
			t.Lanes = append(t.Lanes, Lane{Name: name, Patterns: []string{pattern}, Check: argv})
			continue
		}
		if strings.Join(t.Lanes[i].Check, " ") != strings.Join(argv, " ") {
			return Table{}, fmt.Errorf("line %d: lane %q already has the check %q, and a lane has one check", n, name, strings.Join(t.Lanes[i].Check, " "))
		}
		t.Lanes[i].Patterns = append(t.Lanes[i].Patterns, pattern)
	}
	if err := scanner.Err(); err != nil {
		return Table{}, err
	}
	if len(t.Lanes) == 0 {
		return Table{}, fmt.Errorf("the table has no lanes")
	}
	return t, nil
}

// LoadTable reads the table in file.
func LoadTable(file string) (Table, error) {
	f, err := os.Open(file)
	if err != nil {
		return Table{}, err
	}
	defer f.Close()
	t, err := ParseTable(f)
	if err != nil {
		return Table{}, fmt.Errorf("%s: %w", file, err)
	}
	return t, nil
}

func checkRow(name, pattern, check string) ([]string, error) {
	if !laneName.MatchString(name) {
		return nil, fmt.Errorf("lane name %q must be lower case letters, digits and hyphens, starting with a letter", name)
	}
	if pattern == "" || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "..") {
		return nil, fmt.Errorf("pattern %q must be a non-empty path below the repository root", pattern)
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("pattern %q is malformed: %v", pattern, err)
	}
	if i := strings.IndexAny(check, shellSyntax); i >= 0 {
		return nil, fmt.Errorf("check %q holds %q, which is shell syntax, and checks are run without a shell", check, check[i:i+1])
	}
	argv := strings.Fields(check)
	if len(argv) == 0 {
		return nil, fmt.Errorf("lane %q has no check command", name)
	}
	if strings.Contains(argv[0], "=") {
		return nil, fmt.Errorf("check %q starts with an assignment, and checks are run without a shell", check)
	}
	return argv, nil
}

// Route returns the lanes that claim p, a slash-separated path from the
// repository root, in table order.
func (t Table) Route(p string) []Lane {
	var out []Lane
	for _, lane := range t.Lanes {
		for _, pattern := range lane.Patterns {
			if matches(pattern, p) {
				out = append(out, lane)
				break
			}
		}
	}
	return out
}

// matches applies one pattern to one path. A pattern ending in / claims
// everything below that directory. A pattern with no / is matched against the
// file name at any depth. Any other pattern is matched against the whole path,
// and * does not cross a /.
func matches(pattern, p string) bool {
	switch {
	case strings.HasSuffix(pattern, "/"):
		return strings.HasPrefix(p, pattern)
	case !strings.Contains(pattern, "/"):
		ok, _ := path.Match(pattern, path.Base(p))
		return ok
	default:
		ok, _ := path.Match(pattern, p)
		return ok
	}
}
