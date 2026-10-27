// Package bus is a message bus between named sessions on one machine. Each name
// has an inbox file of one JSON message per line and a small state file that
// remembers how far its owner has read. Every change to either takes an
// exclusive flock on the inbox, so concurrent senders and waiters never see
// half of each other's work. There is no server: the directory is the bus.
//
// flock is POSIX only, so the package builds and works on Linux and macOS.
package bus

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	inboxSuffix = ".jsonl"
	stateSuffix = ".state"

	// MaxText is the longest message text, in bytes. One message is one write,
	// and a bound keeps an inbox line from growing without limit.
	MaxText = 64 * 1024

	// DefaultPoll is how often a waiting session looks at its inbox again.
	DefaultPoll = 50 * time.Millisecond
)

// ErrTimeout is what Wait returns when no message arrived in time.
var ErrTimeout = errors.New("timed out waiting for a message")

// A name becomes a file name, so it is a short lower case word: no dot, no
// slash and no leading hyphen. Lower case only, so that two names never meet in
// one file on a case-insensitive file system.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Message is one line of an inbox.
type Message struct {
	Seq  int       `json:"seq"`
	Time time.Time `json:"time"`
	From string    `json:"from"`
	To   string    `json:"to"`
	Text string    `json:"text"`
}

// Presence is one row of Who.
type Presence struct {
	Name       string
	Unread     int
	LastTold   time.Time // zero when the name has never told
	LastWaited time.Time // zero when the name has never waited
	Broken     string    // why the inbox cannot be read, or "" when it can
}

// state is the part of a name's record that is not a message. ReadThrough is
// the sequence number of the last message a wait returned.
type state struct {
	ReadThrough int       `json:"read_through"`
	LastTold    time.Time `json:"last_told,omitzero"`
	LastWaited  time.Time `json:"last_waited,omitzero"`
}

// Bus is a handle on one bus directory.
type Bus struct {
	dir  string
	now  func() time.Time
	poll time.Duration
	// write puts one line on the end of an inbox. A test replaces it to make
	// the disk fill up part way through.
	write func(f *os.File, line []byte) (int, error)
}

// ValidateName refuses any name that could not safely be a file name in the bus
// directory, including "", "..", and anything with a slash.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("the name %q is not valid: use 1 to 32 lower case letters, digits, hyphens or underscores, starting with a letter or digit", name)
	}
	return nil
}

// Open returns the bus in dir, creating the directory (mode 0700) when it is
// missing. A directory that someone else owns, or that group or others can
// reach, is refused: anyone who could write there could send as any name.
func Open(dir string) (*Bus, error) {
	if dir == "" {
		return nil, errors.New("the bus directory is empty")
	}
	// A link at the bus directory is refused, not followed: the checks below
	// would be made on its target, and the link can be pointed elsewhere later.
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is a symbolic link", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return nil, fmt.Errorf("%s is not owned by the current user", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is reachable by group or others (mode %04o); use mode 0700", dir, info.Mode().Perm())
	}
	return &Bus{dir: dir, now: time.Now, poll: DefaultPoll, write: (*os.File).Write}, nil
}

// Tell appends one message from one name to another's inbox and returns it with
// its sequence number. The sender's name is recorded as having told.
func (b *Bus) Tell(from, to, text string) (Message, error) {
	if err := ValidateName(from); err != nil {
		return Message{}, err
	}
	if err := ValidateName(to); err != nil {
		return Message{}, err
	}
	switch {
	case strings.TrimSpace(text) == "":
		return Message{}, errors.New("the message is empty")
	case len(text) > MaxText:
		return Message{}, fmt.Errorf("the message is %d bytes, over the limit of %d", len(text), MaxText)
	case !utf8.ValidString(text):
		return Message{}, errors.New("the message is not valid UTF-8")
	}

	msg, err := b.append(from, to, text)
	if err != nil {
		return Message{}, err
	}
	// The sender's own inbox is locked after the receiver's is released, never
	// while it is held, so two names telling each other cannot deadlock.
	if err := b.update(from, func(s *state) { s.LastTold = msg.Time }); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func (b *Bus) append(from, to, text string) (Message, error) {
	in, err := b.lock(to)
	if err != nil {
		return Message{}, err
	}
	defer in.close()

	msgs, err := in.messages()
	if err != nil {
		return Message{}, err
	}
	msg := Message{Seq: 1, Time: b.now().UTC(), From: from, To: to, Text: text}
	if len(msgs) > 0 {
		msg.Seq = msgs[len(msgs)-1].Seq + 1
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return Message{}, err
	}
	return msg, b.appendLine(in, append(line, '\n'))
}

// appendLine writes one whole line on the end of the inbox. When the write
// fails or is cut short, as it is on a full disk, the inbox is cut back to the
// size it had, so the stump of a message never stays to refuse every later read.
func (b *Bus) appendLine(in *inbox, line []byte) error {
	info, err := in.f.Stat()
	if err != nil {
		return err
	}
	n, err := b.write(in.f, line)
	if err == nil && n == len(line) {
		return nil
	}
	if err == nil {
		err = io.ErrShortWrite
	}
	err = fmt.Errorf("could not append to the inbox of %s: %w", filepath.Base(in.f.Name()), err)
	if cutErr := in.f.Truncate(info.Size()); cutErr != nil {
		return fmt.Errorf("%w, and the inbox could not be cut back to %d bytes: %v", err, info.Size(), cutErr)
	}
	return err
}

// Wait returns the oldest message in name's inbox that no wait has returned,
// marking it read before it returns, and blocks until there is one. It looks
// again every poll interval. After timeout without a message it returns
// ErrTimeout; a timeout of zero looks once and does not wait.
//
// A message is marked read before the caller sees it, so a caller that dies in
// between loses that message rather than receiving it twice.
func (b *Bus) Wait(name string, timeout time.Duration) (Message, error) {
	if err := ValidateName(name); err != nil {
		return Message{}, err
	}
	deadline := time.Now().Add(timeout)
	for first := true; ; first = false {
		msg, found, err := b.take(name, first)
		if err != nil || found {
			return msg, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return Message{}, ErrTimeout
		}
		time.Sleep(min(b.poll, remaining))
	}
}

// take returns the first unread message, if any, and marks it read. On the
// first look it also records that name is waiting.
func (b *Bus) take(name string, first bool) (Message, bool, error) {
	in, err := b.lock(name)
	if err != nil {
		return Message{}, false, err
	}
	defer in.close()

	s, err := in.readState()
	if err != nil {
		return Message{}, false, err
	}
	msgs, err := in.messages()
	if err != nil {
		return Message{}, false, err
	}
	for _, m := range msgs {
		if m.Seq > s.ReadThrough {
			s.ReadThrough = m.Seq
			s.LastWaited = b.now().UTC()
			return m, true, in.writeState(s)
		}
	}
	if first {
		s.LastWaited = b.now().UTC()
		return Message{}, false, in.writeState(s)
	}
	return Message{}, false, nil
}

// Who lists every name that has an inbox, in name order, with its unread count
// and when it last told and last waited.
func (b *Bus) Who() ([]Presence, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return nil, err
	}
	var rows []Presence
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), inboxSuffix)
		if !ok || ValidateName(name) != nil {
			continue
		}
		row, err := b.presence(name)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (b *Bus) presence(name string) (Presence, error) {
	in, err := b.lock(name)
	if err != nil {
		return Presence{}, err
	}
	defer in.close()

	// A file the bus cannot read marks this one name as broken, so that it does
	// not hide every other name from the session asking who is there.
	var bad *corruptError
	s, err := in.readState()
	if errors.As(err, &bad) {
		return Presence{Name: name, Broken: bad.problem}, nil
	}
	if err != nil {
		return Presence{}, err
	}
	msgs, err := in.messages()
	if errors.As(err, &bad) {
		return Presence{Name: name, LastTold: s.LastTold, LastWaited: s.LastWaited, Broken: bad.problem}, nil
	}
	if err != nil {
		return Presence{}, err
	}
	row := Presence{Name: name, LastTold: s.LastTold, LastWaited: s.LastWaited}
	for _, m := range msgs {
		if m.Seq > s.ReadThrough {
			row.Unread++
		}
	}
	return row, nil
}

// update changes name's state under the lock of its inbox, making the inbox
// if the name has none yet.
func (b *Bus) update(name string, change func(*state)) error {
	in, err := b.lock(name)
	if err != nil {
		return err
	}
	defer in.close()
	s, err := in.readState()
	if err != nil {
		return err
	}
	change(&s)
	return in.writeState(s)
}

// inbox is an open, exclusively locked inbox file.
type inbox struct {
	f         *os.File
	statePath string
}

// lock opens name's inbox, creating it, and blocks until it holds the flock.
// The lock lasts until close, which every caller defers.
func (b *Bus) lock(name string) (*inbox, error) {
	f, err := os.OpenFile(filepath.Join(b.dir, name+inboxSuffix), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("could not lock the inbox of %s: %w", name, err)
	}
	return &inbox{f: f, statePath: filepath.Join(b.dir, name+stateSuffix)}, nil
}

// close releases the lock by closing the file, which drops it.
func (in *inbox) close() error {
	return in.f.Close()
}

// messages reads the whole inbox. A line that is not a message, including the
// stump of an interrupted write, is an error naming the line: the inbox is
// refused, not skipped, so nothing is silently dropped.
func (in *inbox) messages() ([]Message, error) {
	if _, err := in.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var msgs []Message
	r := bufio.NewReader(in.f)
	for n := 1; ; n++ {
		line, err := r.ReadBytes('\n')
		if err == io.EOF && len(line) == 0 {
			return msgs, nil
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
		var m Message
		if err != nil || json.Unmarshal(line, &m) != nil || m.Seq < 1 {
			problem := fmt.Sprintf("line %d is not a message", n)
			return nil, &corruptError{problem: problem, full: filepath.Base(in.f.Name()) + " " + problem}
		}
		msgs = append(msgs, m)
	}
}

func (in *inbox) readState() (state, error) {
	var s state
	raw, err := os.ReadFile(in.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		problem := filepath.Base(in.statePath) + " is not readable"
		return s, &corruptError{problem: problem, full: problem + ": " + err.Error()}
	}
	return s, nil
}

// corruptError is a file of the bus that holds something the bus did not write.
// Who reports it against the one name and goes on, where a command that works
// on that name stops with it.
type corruptError struct {
	problem string // short, for a row of who
	full    string // names the file, for an error
}

func (e *corruptError) Error() string { return e.full }

// writeState replaces the state file whole, through a temporary file and a
// rename, so a crash leaves the old state or the new one and never a mixture.
func (in *inbox) writeState(s state) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(in.statePath), "."+filepath.Base(in.statePath)+".tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, in.statePath)
}
