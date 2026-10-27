package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// busBinary is the built bindery-bus that the cross-process tests run.
var busBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bindery-bus-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	busBinary = filepath.Join(dir, "bindery-bus")
	if out, err := exec.Command("go", "build", "-o", busBinary, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building bindery-bus: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code           int
	stdout, stderr string
}

// busEnv returns a getenv that points the bus at dir.
func busEnv(dir string) func(string) string {
	return func(k string) string {
		if k == EnvDir {
			return dir
		}
		return ""
	}
}

func runBus(dir string, args ...string) result {
	var out, errb bytes.Buffer
	code := run(args, busEnv(dir), &out, &errb)
	return result{code, out.String(), errb.String()}
}

func newDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "bus")
}

// --- the three commands ---

func TestTellThenWaitPrintsTheMessageOnce(t *testing.T) {
	dir := newDir(t)
	if r := runBus(dir, "tell", "juniper", "cobalt", "the", "build", "is", "green"); r.code != 0 || r.stdout != "told cobalt: message 1\n" || r.stderr != "" {
		t.Fatalf("tell: got %+v", r)
	}
	r := runBus(dir, "wait", "--timeout", "5s", "cobalt")
	if r.code != 0 || r.stdout != "#1 juniper: the build is green\n" || r.stderr != "" {
		t.Fatalf("wait: got %+v", r)
	}
	again := runBus(dir, "wait", "--timeout", "0s", "cobalt")
	if again.code != 1 || again.stdout != "" {
		t.Fatalf("a second wait: got %+v, want a timeout and nothing printed", again)
	}
}

func TestAWaitThatTimesOutExitsOneAndSaysSo(t *testing.T) {
	start := time.Now()
	r := runBus(newDir(t), "wait", "--timeout", "50ms", "cobalt")
	want := "bindery-bus: wait: nothing arrived for cobalt within 50ms.\n"
	if r.code != 1 || r.stdout != "" || r.stderr != want {
		t.Fatalf("got %+v, want exit 1 and %q", r, want)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the wait ran far past its timeout")
	}
}

func TestWhoPrintsAHeaderAndOneRowPerName(t *testing.T) {
	dir := newDir(t)
	if r := runBus(dir, "who"); r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("an empty bus: got %+v, want silence", r)
	}
	runBus(dir, "tell", "juniper", "cobalt", "hello")
	runBus(dir, "wait", "--timeout", "0s", "cobalt")

	r := runBus(dir, "who")
	lines := strings.Split(strings.TrimSuffix(r.stdout, "\n"), "\n")
	if r.code != 0 || len(lines) != 3 {
		t.Fatalf("got %+v, want a header and two rows", r)
	}
	if !strings.HasPrefix(lines[0], "NAME") || !strings.Contains(lines[0], "LAST WAITED") {
		t.Errorf("header = %q", lines[0])
	}
	stampPattern := `\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ`
	if !regexp.MustCompile(`^cobalt\s+0\s+-\s+` + stampPattern + `$`).MatchString(lines[1]) {
		t.Errorf("cobalt row = %q, want no unread, never told, a last-waited time", lines[1])
	}
	if !regexp.MustCompile(`^juniper\s+0\s+` + stampPattern + `\s+-$`).MatchString(lines[2]) {
		t.Errorf("juniper row = %q, want a last-told time and never waited", lines[2])
	}
}

func TestVersion(t *testing.T) {
	if r := runBus(newDir(t), "--version"); r.code != 0 || r.stdout != "bindery dev\n" {
		t.Fatalf("got %+v", r)
	}
}

// --- failing closed ---

func TestABadNameExitsTwoNamesItAndMakesNoFile(t *testing.T) {
	dir := newDir(t)
	for _, args := range [][]string{
		{"tell", "..", "cobalt", "hi"},
		{"tell", "juniper", "../escape", "hi"},
		{"tell", "juniper", "a/b", "hi"},
		{"wait", "--timeout", "0s", ""},
		{"wait", "--timeout", "0s", "Upper"},
	} {
		r := runBus(dir, args...)
		if r.code != 2 || r.stdout != "" || !strings.HasPrefix(r.stderr, "bindery-bus: ") || !strings.Contains(r.stderr, "is not valid") {
			t.Errorf("%v: got %+v, want exit 2 with a name error", args, r)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the bus holds %v, %v after only refusals", entries, err)
	}
}

func TestMisuseExitsTwoWithTheUsageAndMakesNoDirectory(t *testing.T) {
	dir := newDir(t)
	for _, args := range [][]string{
		nil,
		{"send"},
		{"tell"},
		{"tell", "juniper"},
		{"tell", "juniper", "cobalt"},
		{"wait"},
		{"wait", "cobalt", "marlin"},
		{"wait", "--timeout", "-1s", "cobalt"},
		{"wait", "--timeout", "soon", "cobalt"},
		{"wait", "--nosuch", "cobalt"},
		{"wait", "cobalt", "--timeout", "1s"},
		{"who", "extra"},
		{"--help"},
	} {
		r := runBus(dir, args...)
		if r.code != 2 || r.stdout != "" || !strings.HasPrefix(r.stderr, "usage: bindery-bus tell") {
			t.Errorf("args %v: got %+v, want exit 2 and the usage", args, r)
		}
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("misuse made the bus directory (stat error %v)", err)
	}
}

func TestADirectoryOthersCanReachExitsTwo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := runBus(dir, "who")
	if r.code != 2 || !strings.HasPrefix(r.stderr, "bindery-bus: directory: ") {
		t.Fatalf("got %+v, want exit 2 about the directory", r)
	}
}

// --- the directory ---

func TestTheDefaultDirectoryIsPrivateAndNamedForTheUser(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	var out, errb bytes.Buffer
	if code := run([]string{"tell", "juniper", "cobalt", "hi"}, func(string) string { return "" }, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	dir := filepath.Join(tmp, "bindery-bus-"+strconv.Itoa(os.Getuid()))
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("default directory %s: %v, %v; want mode 0700", dir, info, err)
	}
}

// --- separate processes ---

func startProc(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(busBinary, args...)
	cmd.Env = append(os.Environ(), EnvDir+"="+dir)
	return cmd
}

// Many separate processes tell one inbox at once, as separate sessions would.
func TestManyProcessesTellingOneInboxLoseNothingAndDoubleNothing(t *testing.T) {
	dir := newDir(t)
	const procs, each = 8, 10
	want := map[string]bool{}
	var wg sync.WaitGroup
	for p := range procs {
		for i := range each {
			want[fmt.Sprintf("p%d-m%d", p, i)] = true
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				cmd := startProc(t, dir, "tell", fmt.Sprintf("proc-%d", p), "cobalt", fmt.Sprintf("p%d-m%d", p, i))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Errorf("tell from process %d: %v\n%s", p, err, out)
					return
				}
			}
		}()
	}
	wg.Wait()

	var texts []string
	var seqs []int
	line := regexp.MustCompile(`^#(\d+) proc-\d: (p\d+-m\d+)$`)
	for range procs * each {
		r := runBus(dir, "wait", "--timeout", "0s", "cobalt")
		m := line.FindStringSubmatch(strings.TrimSuffix(r.stdout, "\n"))
		if r.code != 0 || m == nil {
			t.Fatalf("got %+v, want one of the told messages", r)
		}
		n, _ := strconv.Atoi(m[1])
		seqs = append(seqs, n)
		texts = append(texts, m[2])
	}
	if r := runBus(dir, "wait", "--timeout", "0s", "cobalt"); r.code != 1 {
		t.Fatalf("a message beyond the %d told arrived: %+v", procs*each, r)
	}
	sort.Ints(seqs)
	for i, n := range seqs {
		if n != i+1 {
			t.Fatalf("sequence numbers are not 1..%d each once: %v", len(seqs), seqs)
		}
	}
	seen := map[string]int{}
	for _, text := range texts {
		seen[text]++
	}
	for text := range want {
		if seen[text] != 1 {
			t.Errorf("message %q arrived %d times, want once", text, seen[text])
		}
	}
}

// A wait that is already running in one process returns a message told by another.
func TestAWaitInOneProcessReturnsAMessageToldByAnother(t *testing.T) {
	dir := newDir(t)
	waiter := startProc(t, dir, "wait", "--timeout", "10s", "cobalt")
	var out bytes.Buffer
	waiter.Stdout = &out
	if err := waiter.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- waiter.Wait() }()

	// The wait records itself in who as soon as it starts looking.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(runBus(dir, "who").stdout, "cobalt") {
		if time.Now().After(deadline) {
			waiter.Process.Kill()
			t.Fatal("the waiting process never showed up in who")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if out, err := startProc(t, dir, "tell", "juniper", "cobalt", "ship it").CombinedOutput(); err != nil {
		t.Fatalf("tell: %v\n%s", err, out)
	}

	select {
	case err := <-done:
		if err != nil || out.String() != "#1 juniper: ship it\n" {
			t.Fatalf("the waiting process exited with %v and printed %q", err, out.String())
		}
	case <-time.After(5 * time.Second):
		waiter.Process.Kill()
		t.Fatal("the waiting process did not return the message")
	}
}
