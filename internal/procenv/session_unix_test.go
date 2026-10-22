//go:build unix

package procenv

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// readWithin runs ClaudeSession(dir, 7) and fails the test if it does not
// return within a second: a reader that opened a FIFO would block forever.
func readWithin(t *testing.T, dir string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := ClaudeSession(dir, 7)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("ClaudeSession blocked")
		return nil
	}
}

// TestClaudeSessionFIFO: a FIFO at the record's path is refused without
// being opened, so the read never blocks waiting for a writer.
func TestClaudeSessionFIFO(t *testing.T) {
	dir := sessionDir(t, nil)
	if err := syscall.Mkfifo(filepath.Join(dir, "sessions", "7.json"), 0o600); err != nil {
		t.Skipf("no FIFO here: %v", err)
	}
	if err := readWithin(t, dir); !errors.Is(err, ErrSession) {
		t.Errorf("error %v, want ErrSession", err)
	}
}

// TestClaudeSessionSymlink: a symbolic link at the record's path is refused,
// whether it names a valid record or a FIFO.
func TestClaudeSessionSymlink(t *testing.T) {
	other := t.TempDir()
	target := filepath.Join(other, "real.json")
	if err := os.WriteFile(target, []byte(`{"pid":7,"status":"waiting"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(other, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("no FIFO here: %v", err)
	}
	for name, to := range map[string]string{"to a record": target, "to a FIFO": fifo} {
		dir := sessionDir(t, nil)
		if err := os.Symlink(to, filepath.Join(dir, "sessions", "7.json")); err != nil {
			t.Fatal(err)
		}
		if err := readWithin(t, dir); !errors.Is(err, ErrSession) {
			t.Errorf("%s: error %v, want ErrSession", name, err)
		}
	}
}

// TestTailTranscriptFIFOAndSymlink: a FIFO at the transcript's path is
// refused without blocking, and a symbolic link is never followed.
func TestTailTranscriptFIFOAndSymlink(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo.jsonl")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("no FIFO here: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := TailTranscript(Tail{}, fifo, func([]byte) {})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrTranscript) {
			t.Errorf("FIFO: %v, want ErrTranscript", err)
		}
	case <-time.After(time.Second):
		t.Fatal("TailTranscript blocked on a FIFO")
	}
	real := filepath.Join(dir, "real.jsonl")
	if err := os.WriteFile(real, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := TailTranscript(Tail{}, link, func([]byte) { t.Error("read through a link") }); !errors.Is(err, ErrTranscript) {
		t.Errorf("symlink: %v, want ErrTranscript", err)
	}
}
