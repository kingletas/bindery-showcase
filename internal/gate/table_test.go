package gate

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const sampleTable = "# lane\tpattern\tcheck\n" +
	"go\t*.go\tmake check\n" +
	"go\tgo.mod\tmake check\n" +
	"docs\tdocs/\tgit diff --cached --check\n" +
	"docs\t*.md\tgit diff --cached --check\n" +
	"cli\tcmd/*/main.go\tgo build ./cmd/...\n"

func laneNames(lanes []Lane) []string {
	var names []string
	for _, l := range lanes {
		names = append(names, l.Name)
	}
	return names
}

func TestRouteSendsAPathToItsLanes(t *testing.T) {
	table := parse(t, sampleTable)
	cases := []struct {
		path string
		want []string
	}{
		{"main.go", []string{"go"}},
		{"internal/deep/inside/x.go", []string{"go"}},
		{"go.mod", []string{"go"}},
		{"README.md", []string{"docs"}},
		{"docs/guide.txt", []string{"docs"}},
		{"cmd/tool/main.go", []string{"go", "cli"}},
		{"cmd/tool/extra/main.go", []string{"go"}},
		{"data/notes.csv", nil},
		{"vendor-go.mod", nil},
	}
	for _, c := range cases {
		if got := laneNames(table.Route(c.path)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Route(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestRowsSharingALaneNameAddPatternsToOneLane(t *testing.T) {
	table := parse(t, sampleTable)
	if len(table.Lanes) != 3 {
		t.Fatalf("got %d lanes, want 3: %+v", len(table.Lanes), table.Lanes)
	}
	if got, want := table.Lanes[0].Patterns, []string{"*.go", "go.mod"}; !reflect.DeepEqual(got, want) {
		t.Errorf("go patterns = %v, want %v", got, want)
	}
	if got, want := table.Lanes[0].Check, []string{"make", "check"}; !reflect.DeepEqual(got, want) {
		t.Errorf("go check = %v, want %v", got, want)
	}
}

func TestParseTableSkipsCommentsAndBlankLines(t *testing.T) {
	table := parse(t, "\n# only a comment\n  # indented comment\ngo\t*.go\ttrue\r\n\n")
	if len(table.Lanes) != 1 || table.Lanes[0].Name != "go" || !reflect.DeepEqual(table.Lanes[0].Check, []string{"true"}) {
		t.Fatalf("got %+v", table.Lanes)
	}
}

// A table that cannot be read fully is refused whole: a lane that silently
// went missing would leave its paths unchecked.
func TestAMalformedTableIsRefused(t *testing.T) {
	cases := []struct {
		name, table, want string
	}{
		{"empty", "", "no lanes"},
		{"only comments", "# nothing here\n", "no lanes"},
		{"spaces instead of tabs", "go *.go make check\n", "line 1 has 1 tab-separated columns"},
		{"two columns", "go\t*.go\n", "line 1 has 2 tab-separated columns"},
		{"four columns", "go\t*.go\ttrue\textra\n", "line 1 has 4 tab-separated columns"},
		{"empty check", "go\t*.go\t\n", "no check command"},
		{"empty pattern", "go\t\ttrue\n", "non-empty path"},
		{"absolute pattern", "go\t/etc/*\ttrue\n", "below the repository root"},
		{"parent pattern", "go\t../*.go\ttrue\n", "below the repository root"},
		{"bad pattern", "go\t[a\ttrue\n", "malformed"},
		{"bad lane name", "Go Lang\t*.go\ttrue\n", "lane name"},
		{"a pipe", "go\t*.go\tmake check | cat\n", "shell syntax"},
		{"a quote", "go\t*.go\tgo test -run 'A'\n", "shell syntax"},
		{"a substitution", "go\t*.go\techo $HOME\n", "shell syntax"},
		{"a list", "go\t*.go\ttrue && false\n", "shell syntax"},
		{"an assignment", "go\t*.go\tCGO_ENABLED=0 go test\n", "starts with an assignment"},
		{"two checks for one lane", "go\t*.go\ttrue\ngo\tgo.mod\tfalse\n", "a lane has one check"},
		{"a bad row after good ones", "go\t*.go\ttrue\ndocs\t*.md\n", "line 2"},
	}
	for _, c := range cases {
		_, err := ParseTable(strings.NewReader(c.table))
		if err == nil {
			t.Errorf("%s: table was accepted, want a refusal mentioning %q", c.name, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
	}
}

func TestLoadTableNamesTheFileInItsError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lanes.tsv", "go\t*.go\n")
	_, err := LoadTable(filepath.Join(dir, "lanes.tsv"))
	if err == nil || !strings.Contains(err.Error(), "lanes.tsv: line 1") {
		t.Fatalf("error = %v, want it to name the file and the line", err)
	}
	if _, err := LoadTable(filepath.Join(dir, "missing.tsv")); err == nil {
		t.Fatal("a missing table was accepted")
	}
}

// The table shipped for this repository is small on purpose, and it must stay
// readable by the code that reads it.
func TestTheExampleTableParsesAndHasFourLanes(t *testing.T) {
	table, err := LoadTable(filepath.Join("..", "..", "lanes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go", "docs", "workflows", "examples"}
	if got := laneNames(table.Lanes); !reflect.DeepEqual(got, want) {
		t.Fatalf("lanes = %v, want %v", got, want)
	}
	routes := map[string]string{
		"internal/gate/gate.go":          "go",
		"README.md":                      "docs",
		".github/workflows/ci.yml":       "workflows",
		"examples/commit-msg":            "examples",
		"Makefile":                       "go",
		"lanes.tsv":                      "go",
		"internal/gate/gate_test.go":     "go",
		"cmd/bindery-gate/main.go":       "go",
		"packaging/release-notes.sh":     "workflows",
		"docs/from-nothing.md":           "docs",
		"examples/gate/lanes.tsv":        "examples",
		"internal/guard/helpers_test.go": "go",
	}
	for p, lane := range routes {
		if got := laneNames(table.Route(p)); !slices.Contains(got, lane) {
			t.Errorf("Route(%q) = %v, want it to include %q", p, got, lane)
		}
	}
}
