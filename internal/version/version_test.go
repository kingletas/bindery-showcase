package version

import "testing"

func TestStringNamesTheProgramAndTheBuild(t *testing.T) {
	got := String()
	want := "bindery " + Version
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if Version == "" {
		t.Fatal("Version is empty, so a build could not say which one it is")
	}
}
