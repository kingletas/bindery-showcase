package gate

import (
	"fmt"
	"path"
	"strings"
)

// ReasonPrefix starts the line that excuses a changed test.
const ReasonPrefix = "Test changed:"

// IsTestFile reports whether p, a slash-separated path from the repository
// root, is a test file: a name ending _test.go or _test.py, starting test_,
// or holding .test. or .spec. before its extension, or any file below a
// directory named test, tests or __tests__.
func IsTestFile(p string) bool {
	dir, base := path.Split(p)
	if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_test.py") || strings.HasPrefix(base, "test_") {
		return true
	}
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	for _, d := range strings.Split(strings.TrimSuffix(dir, "/"), "/") {
		if d == "test" || d == "tests" || d == "__tests__" {
			return true
		}
	}
	return false
}

// HasReason reports whether the message carries a line that starts with
// "Test changed:" followed by a reason. A comment line, which starts with #,
// never counts.
func HasReason(message string) bool {
	for _, line := range strings.Split(message, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), ReasonPrefix); ok && strings.TrimSpace(rest) != "" {
			return true
		}
	}
	return false
}

// testChangedRule returns the sentence that refuses the commit, or "" when it
// may go ahead. A commit that modifies, deletes or renames away an existing
// test file needs a reason in its message; a new test file needs none.
func testChangedRule(changes []Change, message string, haveMessage bool) string {
	var touched []string
	for _, c := range changes {
		switch c.Status {
		case 'M', 'T', 'D':
			if IsTestFile(c.Path) {
				touched = append(touched, c.Path)
			}
		case 'R':
			if IsTestFile(c.OldPath) {
				touched = append(touched, c.OldPath)
			}
		}
	}
	if len(touched) == 0 || (haveMessage && HasReason(message)) {
		return ""
	}
	subject := fmt.Sprintf("The existing test %q is changed", touched[0])
	if len(touched) > 1 {
		subject = fmt.Sprintf("The existing tests %q and %d more are changed", touched[0], len(touched)-1)
	}
	line := ReasonPrefix + " <why>"
	if !haveMessage {
		return fmt.Sprintf("%s, and no commit message was given to carry a %q line.", subject, line)
	}
	return fmt.Sprintf("%s, and the commit message has no %q line.", subject, line)
}
