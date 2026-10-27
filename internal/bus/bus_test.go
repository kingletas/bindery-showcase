package bus

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// newBus opens a bus in a directory of the test's own, polling fast so that
// waits are quick.
func newBus(t *testing.T) *Bus {
	t.Helper()
	b, err := Open(filepath.Join(t.TempDir(), "bus"))
	if err != nil {
		t.Fatal(err)
	}
	b.poll = 2 * time.Millisecond
	return b
}

func mustTell(t *testing.T, b *Bus, from, to, text string) Message {
	t.Helper()
	m, err := b.Tell(from, to, text)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// drain returns every unread message of name, oldest first, without waiting.
func drain(t *testing.T, b *Bus, name string) []Message {
	t.Helper()
	var got []Message
	// The bound keeps a bus that never marks messages read from looping forever.
	for len(got) <= 1000 {
		m, err := b.Wait(name, 0)
		if errors.Is(err, ErrTimeout) {
			return got
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, m)
	}
	t.Fatalf("%s still had messages after %d were drained", name, len(got))
	return nil
}

// --- telling and waiting ---

func TestAMessageToldIsReturnedByTheReceiversWait(t *testing.T) {
	b := newBus(t)
	told := mustTell(t, b, "juniper", "cobalt", "the build is green")

	got, err := b.Wait("cobalt", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.From != "juniper" || got.To != "cobalt" || got.Text != "the build is green" || got.Seq != 1 {
		t.Fatalf("got %+v", got)
	}
	if !got.Time.Equal(told.Time) || got.Time.IsZero() {
		t.Fatalf("the message time is %v, want the time it was told, %v", got.Time, told.Time)
	}
}

func TestSequenceNumbersCountUpWithinEachInbox(t *testing.T) {
	b := newBus(t)
	for want := 1; want <= 3; want++ {
		if m := mustTell(t, b, "juniper", "cobalt", "again"); m.Seq != want {
			t.Fatalf("message %d got sequence %d", want, m.Seq)
		}
	}
	if m := mustTell(t, b, "juniper", "marlin", "first for another inbox"); m.Seq != 1 {
		t.Fatalf("a new inbox starts at 1, got %d", m.Seq)
	}
}

func TestWaitReturnsAMessageThatArrivesAfterItStarted(t *testing.T) {
	b := newBus(t)
	type result struct {
		m   Message
		err error
	}
	done := make(chan result, 1)
	go func() {
		m, err := b.Wait("cobalt", 5*time.Second)
		done <- result{m, err}
	}()

	// Nothing has been told, so the wait has to still be waiting.
	select {
	case r := <-done:
		t.Fatalf("the wait returned %+v before anything was told", r)
	case <-time.After(40 * time.Millisecond):
	}
	mustTell(t, b, "juniper", "cobalt", "late news")

	select {
	case r := <-done:
		if r.err != nil || r.m.Text != "late news" {
			t.Fatalf("got %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the wait did not return the message")
	}
}

func TestWaitTimesOutWhenNothingArrives(t *testing.T) {
	b := newBus(t)
	start := time.Now()
	_, err := b.Wait("cobalt", 60*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("the wait took %v, want about 60ms", elapsed)
	}
}

func TestAZeroTimeoutLooksOnceAndDoesNotWait(t *testing.T) {
	b := newBus(t)
	start := time.Now()
	if _, err := b.Wait("cobalt", 0); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("a zero timeout waited")
	}
	mustTell(t, b, "juniper", "cobalt", "here")
	if m, err := b.Wait("cobalt", 0); err != nil || m.Text != "here" {
		t.Fatalf("got %+v, %v; want the waiting message", m, err)
	}
}

func TestAMessageIsNeverReturnedTwice(t *testing.T) {
	b := newBus(t)
	mustTell(t, b, "juniper", "cobalt", "once only")
	if _, err := b.Wait("cobalt", time.Second); err != nil {
		t.Fatal(err)
	}
	if m, err := b.Wait("cobalt", 20*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("a second wait returned %+v, %v; want a timeout", m, err)
	}
}

func TestAMessageIsNotReturnedTwiceAcrossHandles(t *testing.T) {
	b := newBus(t)
	mustTell(t, b, "juniper", "cobalt", "once only")
	if _, err := b.Wait("cobalt", time.Second); err != nil {
		t.Fatal(err)
	}
	again, err := Open(b.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, again, "cobalt"); len(got) != 0 {
		t.Fatalf("a new handle on the same directory returned %+v", got)
	}
}

func TestMessagesComeBackOldestFirstOneAtATime(t *testing.T) {
	b := newBus(t)
	for _, text := range []string{"one", "two", "three"} {
		mustTell(t, b, "juniper", "cobalt", text)
	}
	var got []string
	for _, m := range drain(t, b, "cobalt") {
		got = append(got, m.Text)
	}
	if want := []string{"one", "two", "three"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Two sessions waiting on one name compete for its messages, and a message
// goes to exactly one of them.
func TestTwoWaitersOnOneInboxShareTheMessagesAndNeverBothGetOne(t *testing.T) {
	b := newBus(t)
	const waiters = 4
	var wg sync.WaitGroup
	got := make(chan Message, waiters)
	for range waiters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m, err := b.Wait("cobalt", 400*time.Millisecond); err == nil {
				got <- m
			}
		}()
	}
	mustTell(t, b, "juniper", "cobalt", "first")
	mustTell(t, b, "juniper", "cobalt", "second")
	wg.Wait()
	close(got)

	var texts []string
	for m := range got {
		texts = append(texts, m.Text)
	}
	sort.Strings(texts)
	if want := []string{"first", "second"}; !reflect.DeepEqual(texts, want) {
		t.Fatalf("the waiters received %v, want each of %v exactly once", texts, want)
	}
}

func TestATellRefusesAnEmptyOversizedOrNonUTF8Message(t *testing.T) {
	b := newBus(t)
	for name, text := range map[string]string{
		"empty":      "",
		"blank":      " \t\n",
		"oversized":  strings.Repeat("x", MaxText+1),
		"not UTF-8":  "caf\xe9",
		"zero bytes": strings.Repeat("\x00", 3) + "\xff",
	} {
		if _, err := b.Tell("juniper", "cobalt", text); err == nil {
			t.Errorf("%s: the message was accepted", name)
		}
	}
	if got := drain(t, b, "cobalt"); len(got) != 0 {
		t.Fatalf("a refused message was delivered: %+v", got)
	}
	// Silent path: the longest allowed message and punctuation both go through.
	longest := strings.Repeat("x", MaxText)
	mustTell(t, b, "juniper", "cobalt", longest)
	mustTell(t, b, "juniper", "cobalt", "line one\nline \"two\" é")
	got := drain(t, b, "cobalt")
	if len(got) != 2 || got[0].Text != longest || got[1].Text != "line one\nline \"two\" é" {
		t.Fatal("the longest message or the one with newlines and quotes did not come back whole")
	}
}

// --- the concurrency claim ---

// checkExactlyOnce fails unless got holds each of want once, with the sequence
// numbers 1..len(want) each once.
func checkExactlyOnce(t *testing.T, got []Message, want map[string]bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d", len(got), len(want))
	}
	seen := map[string]int{}
	seqs := map[int]int{}
	for _, m := range got {
		seen[m.Text]++
		seqs[m.Seq]++
	}
	for text := range want {
		if seen[text] != 1 {
			t.Errorf("message %q arrived %d times, want exactly once", text, seen[text])
		}
	}
	for seq := 1; seq <= len(want); seq++ {
		if seqs[seq] != 1 {
			t.Errorf("sequence number %d was used %d times, want exactly once", seq, seqs[seq])
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

func TestManySendersToOneInboxLoseNothingAndDoubleNothing(t *testing.T) {
	b := newBus(t)
	const senders, each = 10, 20
	want := map[string]bool{}
	var wg sync.WaitGroup
	for s := range senders {
		for i := range each {
			want[fmt.Sprintf("from sender %d, message %d", s, i)] = true
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each sender opens the bus for itself, as separate sessions would.
			mine, err := Open(b.dir)
			if err != nil {
				t.Error(err)
				return
			}
			for i := range each {
				if _, err := mine.Tell(fmt.Sprintf("sender-%d", s), "cobalt", fmt.Sprintf("from sender %d, message %d", s, i)); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	checkExactlyOnce(t, drain(t, b, "cobalt"), want)
}

// The receiver drains the inbox while the senders are still sending.
func TestAReceiverDrainingWhileSendersSendGetsEachMessageOnce(t *testing.T) {
	b := newBus(t)
	const senders, each = 8, 20
	total := senders * each
	want := map[string]bool{}
	var wg sync.WaitGroup
	for s := range senders {
		for i := range each {
			want[fmt.Sprintf("s%d m%d", s, i)] = true
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				if _, err := b.Tell(fmt.Sprintf("sender-%d", s), "cobalt", fmt.Sprintf("s%d m%d", s, i)); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}

	var got []Message
	for len(got) < total {
		m, err := b.Wait("cobalt", 10*time.Second)
		if err != nil {
			t.Fatalf("after %d of %d messages: %v", len(got), total, err)
		}
		got = append(got, m)
	}
	wg.Wait()
	checkExactlyOnce(t, got, want)
	if extra := drain(t, b, "cobalt"); len(extra) != 0 {
		t.Fatalf("%d messages arrived beyond the %d told", len(extra), total)
	}
}

// --- names ---

func TestABadNameIsRefusedAndNothingIsWritten(t *testing.T) {
	b := newBus(t)
	bad := []string{"", "..", ".", "../escape", "a/b", "/abs", `a\b`, "A", "Cobalt", ".hidden", "-flag", "a b", "a.b", "a\x00b", strings.Repeat("a", 33)}
	for _, name := range bad {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) accepted it", name)
		}
		if _, err := b.Tell(name, "cobalt", "hi"); err == nil {
			t.Errorf("Tell from %q was accepted", name)
		}
		if _, err := b.Tell("juniper", name, "hi"); err == nil {
			t.Errorf("Tell to %q was accepted", name)
		}
		if _, err := b.Wait(name, 0); err == nil || errors.Is(err, ErrTimeout) {
			t.Errorf("Wait on %q returned %v, want a name error", name, err)
		}
	}
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the bus directory holds %d entries after only refusals", len(entries))
	}
	parent, err := os.ReadDir(filepath.Dir(b.dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(parent) != 1 {
		t.Errorf("the directory around the bus holds %d entries, want only the bus: a name escaped", len(parent))
	}
}

func TestAGoodNameIsAccepted(t *testing.T) {
	for _, name := range []string{"a", "cobalt", "build-bot_2", "7", strings.Repeat("a", 32)} {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v", name, err)
		}
	}
}

// --- who ---

func TestWhoListsEveryNameWithAnInboxAndWhatItDid(t *testing.T) {
	b := newBus(t)
	clock := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	b.now = func() time.Time { clock = clock.Add(time.Minute); return clock }

	if rows, err := b.Who(); err != nil || len(rows) != 0 {
		t.Fatalf("an empty bus listed %+v, %v", rows, err)
	}

	mustTell(t, b, "juniper", "cobalt", "one")     // 03:01 juniper told
	mustTell(t, b, "juniper", "cobalt", "two")     // 03:02
	mustTell(t, b, "cobalt", "marlin", "three")    // 03:03 cobalt told
	if _, err := b.Wait("cobalt", 0); err != nil { // 03:04 cobalt waited, read one
		t.Fatal(err)
	}

	// A stray file, and a file whose name is not a valid one, are not names.
	for _, stray := range []string{"notes.txt", "Upper.jsonl", ".hidden.jsonl"} {
		if err := os.WriteFile(filepath.Join(b.dir, stray), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := b.Who()
	if err != nil {
		t.Fatal(err)
	}
	at := func(minute int) time.Time { return time.Date(2030, 1, 2, 3, minute, 0, 0, time.UTC) }
	want := []Presence{
		{Name: "cobalt", Unread: 1, LastTold: at(3), LastWaited: at(4)},
		{Name: "juniper", Unread: 0, LastTold: at(2)},
		{Name: "marlin", Unread: 1},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("who =\n%+v\nwant\n%+v", rows, want)
	}
}

func TestAWaitThatTimesOutStillShowsTheNameAsHavingWaited(t *testing.T) {
	b := newBus(t)
	if _, err := b.Wait("cobalt", 0); !errors.Is(err, ErrTimeout) {
		t.Fatal(err)
	}
	rows, err := b.Who()
	if err != nil || len(rows) != 1 || rows[0].Name != "cobalt" || rows[0].LastWaited.IsZero() {
		t.Fatalf("who = %+v, %v; want cobalt with a last-waited time", rows, err)
	}
}

// --- the directory and the files ---

func TestOpenMakesTheDirectoryPrivateAndReopensIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "made", "bus")
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, %v; want 0700", info.Mode().Perm(), err)
	}
	if _, err := Open(dir); err != nil {
		t.Fatalf("reopening a private directory failed: %v", err)
	}
}

func TestOpenRefusesADirectoryOthersCanReach(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "reachable by group or others") {
		t.Fatalf("err = %v, want a refusal naming the mode", err)
	}
}

func TestOpenRefusesAnEmptyPathAndAFile(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Error("an empty directory was accepted")
	}
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(file); err == nil {
		t.Error("a file was accepted as the bus directory")
	}
}

func TestInboxFilesAreOwnerOnlyAndHoldOneJSONLinePerMessage(t *testing.T) {
	b := newBus(t)
	mustTell(t, b, "juniper", "cobalt", "line one\nline two")
	mustTell(t, b, "juniper", "cobalt", "second")

	path := filepath.Join(b.dir, "cobalt.jsonl")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("inbox mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"); len(lines) != 2 {
		t.Fatalf("the inbox has %d lines for 2 messages: %q", len(lines), raw)
	}
}

func TestAnInboxLineThatIsNotAMessageRefusesTheInboxAndNamesTheLine(t *testing.T) {
	b := newBus(t)
	mustTell(t, b, "juniper", "cobalt", "fine")
	f, err := os.OpenFile(filepath.Join(b.dir, "cobalt.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{\"seq\": 2, \"te")
	f.Close()

	for name, call := range map[string]func() error{
		"wait": func() error { _, err := b.Wait("cobalt", 0); return err },
		"tell": func() error { _, err := b.Tell("juniper", "cobalt", "more"); return err },
	} {
		// Every command reads the whole inbox, so the good first line does not
		// let the bad second one through.
		if err := call(); err == nil || !strings.Contains(err.Error(), "cobalt.jsonl line 2 is not a message") {
			t.Errorf("%s: err = %v, want the inbox refused with its line number", name, err)
		}
	}
}

// breakInbox leaves the stump of an interrupted write on the end of name's inbox.
func breakInbox(t *testing.T, b *Bus, name string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(b.dir, name+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("{\"seq\": 2, \"te"); err != nil {
		t.Fatal(err)
	}
}

func TestWhoMarksABrokenInboxAndStillListsTheOthers(t *testing.T) {
	b := newBus(t)
	mustTell(t, b, "juniper", "cobalt", "fine")
	mustTell(t, b, "juniper", "marlin", "also fine")
	breakInbox(t, b, "cobalt")

	rows, err := b.Who()
	if err != nil {
		t.Fatalf("one broken inbox failed who for every name: %v", err)
	}
	got := map[string]Presence{}
	for _, r := range rows {
		got[r.Name] = r
	}
	if len(rows) != 3 {
		t.Fatalf("who listed %d names, want cobalt, juniper and marlin: %+v", len(rows), rows)
	}
	if got["cobalt"].Broken != "line 2 is not a message" {
		t.Errorf("cobalt: Broken = %q, want the line named", got["cobalt"].Broken)
	}
	if got["marlin"].Broken != "" || got["marlin"].Unread != 1 || got["juniper"].Broken != "" {
		t.Errorf("the healthy inboxes were affected: %+v", rows)
	}
}

func TestWhoMarksAnUnreadableStateFileAsBroken(t *testing.T) {
	b := newBus(t)
	mustTell(t, b, "juniper", "cobalt", "fine")
	if err := os.WriteFile(filepath.Join(b.dir, "cobalt.state"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := b.Who()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Name == "cobalt" && r.Broken != "cobalt.state is not readable" {
			t.Errorf("cobalt: Broken = %q, want the state file named", r.Broken)
		}
		if r.Name == "juniper" && r.Broken != "" {
			t.Errorf("juniper: Broken = %q, want none", r.Broken)
		}
	}
}

// --- a write that fails part way ---

// failingWrite writes the first keep bytes of the line and then fails the way a
// full disk does.
func failingWrite(keep int) func(*os.File, []byte) (int, error) {
	return func(f *os.File, line []byte) (int, error) {
		n := min(keep, len(line))
		if _, err := f.Write(line[:n]); err != nil {
			return 0, err
		}
		return n, syscall.ENOSPC
	}
}

func TestAWriteThatFailsPartWayLeavesTheInboxAsItWas(t *testing.T) {
	for name, keep := range map[string]int{"nothing written": 0, "half a line": 30, "all but the newline": 1 << 20} {
		t.Run(name, func(t *testing.T) {
			b := newBus(t)
			mustTell(t, b, "juniper", "cobalt", "first")
			path := filepath.Join(b.dir, "cobalt.jsonl")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			good := b.write
			b.write = failingWrite(keep)
			if _, err := b.Tell("juniper", "cobalt", "this one hits a full disk"); err == nil {
				t.Fatal("the failed write was reported as a success")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("the inbox changed after a failed write:\nbefore %q\nafter  %q", before, after)
			}

			// The inbox is still whole: it reads, takes the next message and who lists it.
			b.write = good
			if m := mustTell(t, b, "juniper", "cobalt", "second"); m.Seq != 2 {
				t.Fatalf("the next message got sequence %d, want 2", m.Seq)
			}
			var texts []string
			for _, m := range drain(t, b, "cobalt") {
				texts = append(texts, m.Text)
			}
			if want := []string{"first", "second"}; !reflect.DeepEqual(texts, want) {
				t.Fatalf("got %v, want %v", texts, want)
			}
			if rows, err := b.Who(); err != nil {
				t.Fatal(err)
			} else {
				for _, r := range rows {
					if r.Broken != "" {
						t.Errorf("%s is marked broken after a failed write: %s", r.Name, r.Broken)
					}
				}
			}
		})
	}
}

// --- a symbolic link at the bus directory ---

func TestOpenRefusesASymbolicLinkAtTheBusDirectory(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("err = %v, want the link refused", err)
	}

	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dangling); err == nil {
		t.Fatal("a dangling link was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "nowhere")); err == nil {
		t.Fatal("Open created the directory a link pointed at")
	}
}

// Only the bus directory itself must not be a link; a link further up the path
// is just a way of reaching it.
func TestOpenFollowsASymbolicLinkHigherUpThePath(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(link, "bus")); err != nil {
		t.Fatalf("a bus below a linked parent was refused: %v", err)
	}
}
