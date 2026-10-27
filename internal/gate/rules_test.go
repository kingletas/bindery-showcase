package gate

import (
	"reflect"
	"strings"
	"testing"
)

func TestIsTestFile(t *testing.T) {
	tests := []string{
		"widget_test.go", "internal/a/b_test.go", "test_widget.py", "pkg/widget_test.py",
		"web/app.test.ts", "web/app.spec.js", "tests/fixture.json", "pkg/test/data.txt", "web/__tests__/a.js",
	}
	for _, p := range tests {
		if !IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = false, want true", p)
		}
	}
	others := []string{
		"widget.go", "README.md", "internal/gate/testing.go", "contest.go", "latest/notes.txt",
		"docs/test-plan.md", "attest_data.go", "cmd/tester/main.go",
	}
	for _, p := range others {
		if IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = true, want false", p)
		}
	}
}

func TestHasReason(t *testing.T) {
	yes := []string{
		"Subject\n\nTest changed: the old expectation was wrong\n",
		"Test changed: x",
		"Subject\r\n\r\nTest changed: because\r\n",
		"Subject\nTest changed:   spaced out\nmore",
	}
	for _, m := range yes {
		if !HasReason(m) {
			t.Errorf("HasReason(%q) = false, want true", m)
		}
	}
	no := []string{
		"",
		"Subject only\n",
		"Test changed:\n",
		"Test changed:   \n",
		"Subject\n\nThe Test changed: in the middle of a line\n",
		"# Test changed: a comment git strips\n",
		"  Test changed: indented\n",
		"test changed: wrong case\n",
	}
	for _, m := range no {
		if HasReason(m) {
			t.Errorf("HasReason(%q) = true, want false", m)
		}
	}
}

// The acceptance test: an edited test with no reason line is refused.
func TestAnEditedTestWithoutAReasonLineIsRefused(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "widget_test.go", "package widget\n")
	stage(t, dir, "widget_test.go", "package widget\n\n// expectation moved\n")

	table := parse(t, "go\t*.go\ttrue\nlanes\t*.md\ttrue\n")
	findings := Evaluate(staged(t, dir), "Loosen the widget test\n", true, table, (&recorder{}).run)

	if got, want := findingNames(findings), []string{TestChangedName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want only %v", findings, want)
	}
	reason := findings[0].Reason
	for _, want := range []string{`"widget_test.go"`, "Test changed: <why>"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q does not mention %q", reason, want)
		}
	}
	assertOneSentence(t, reason)
}

func TestTheSameEditWithAReasonLinePasses(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "widget_test.go", "package widget\n")
	stage(t, dir, "widget_test.go", "package widget\n\n// expectation moved\n")

	table := parse(t, "go\t*.go\ttrue\n")
	message := "Loosen the widget test\n\nTest changed: the old expectation counted the header row.\n"
	if findings := Evaluate(staged(t, dir), message, true, table, (&recorder{}).run); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestANewTestFilePassesWithoutAReasonLine(t *testing.T) {
	dir := newRepo(t)
	stage(t, dir, "widget_test.go", "package widget\n")
	stage(t, dir, "tests/fixture.json", "{}\n")

	table := parse(t, "go\t*.go\ttrue\nfixtures\ttests/\ttrue\n")
	if findings := Evaluate(staged(t, dir), "Add a widget test\n", true, table, (&recorder{}).run); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestADeletedTestFileIsRefusedWithoutAReasonLine(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "widget_test.go", "package widget\n")
	gitIn(t, dir, "rm", "-q", "--", "widget_test.go")

	table := parse(t, "go\t*.go\ttrue\n")
	changes := staged(t, dir)
	if len(changes) != 1 || changes[0].Status != 'D' {
		t.Fatalf("changes = %+v, want one deletion", changes)
	}
	findings := Evaluate(changes, "Drop the widget test\n", true, table, (&recorder{}).run)
	if got, want := findingNames(findings), []string{TestChangedName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want only %v", findings, want)
	}
	withReason := "Drop the widget test\n\nTest changed: the widget is gone.\n"
	if findings := Evaluate(changes, withReason, true, table, (&recorder{}).run); len(findings) != 0 {
		t.Fatalf("findings with a reason = %+v, want none", findings)
	}
}

func TestADeletedFileThatIsNotATestNeedsNoReasonLine(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "widget.go", "package widget\n")
	gitIn(t, dir, "rm", "-q", "--", "widget.go")

	table := parse(t, "go\t*.go\ttrue\n")
	if findings := Evaluate(staged(t, dir), "Drop the widget\n", true, table, (&recorder{}).run); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestARenamedTestFileNeedsAReasonLineBecauseItsOldPathIsGone(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "widget_test.go", "package widget\n\n// a body long enough for git to see a rename\n")
	gitIn(t, dir, "mv", "widget_test.go", "gadget_test.go")

	table := parse(t, "go\t*.go\ttrue\n")
	changes := staged(t, dir)
	if len(changes) != 1 || changes[0].Status != 'R' || changes[0].OldPath != "widget_test.go" || changes[0].Path != "gadget_test.go" {
		t.Fatalf("changes = %+v, want one rename", changes)
	}
	if got := findingNames(Evaluate(changes, "Rename the test\n", true, table, (&recorder{}).run)); !reflect.DeepEqual(got, []string{TestChangedName}) {
		t.Fatalf("findings = %v, want the test rule", got)
	}
	if findings := Evaluate(changes, "Rename the test\n\nTest changed: it follows the type.\n", true, table, (&recorder{}).run); len(findings) != 0 {
		t.Fatalf("findings with a reason = %+v, want none", findings)
	}
}

func TestARunByHandWithNoMessageSaysSo(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "widget_test.go", "package widget\n")
	stage(t, dir, "widget_test.go", "package widget\n\n// changed\n")

	table := parse(t, "go\t*.go\ttrue\n")
	findings := Evaluate(staged(t, dir), "", false, table, (&recorder{}).run)
	if len(findings) != 1 || !strings.Contains(findings[0].Reason, "no commit message was given") {
		t.Fatalf("findings = %+v, want one saying no message was given", findings)
	}
}

func TestSeveralEditedTestsAreCountedInOneFinding(t *testing.T) {
	dir := newRepo(t)
	commitFile(t, dir, "a_test.go", "package a\n")
	commitFile(t, dir, "b_test.go", "package b\n")
	stage(t, dir, "a_test.go", "package a\n// x\n")
	stage(t, dir, "b_test.go", "package b\n// x\n")

	table := parse(t, "go\t*.go\ttrue\n")
	findings := Evaluate(staged(t, dir), "Touch two tests\n", true, table, (&recorder{}).run)
	if len(findings) != 1 || !strings.Contains(findings[0].Reason, `"a_test.go" and 1 more`) {
		t.Fatalf("findings = %+v, want one that counts the second test", findings)
	}
}

func assertOneSentence(t *testing.T, reason string) {
	t.Helper()
	if strings.ContainsAny(reason, "\n\r") || !strings.HasSuffix(reason, ".") {
		t.Errorf("reason %q is not one line ending in a full stop", reason)
	}
}
