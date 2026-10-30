package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"sync"
)

// logMaxBytes and logKeep bound herddash.log (Story 7.2): when the next line
// would take it past logMaxBytes, it is rotated, and logKeep earlier files,
// herddash.log.1 (the newest) and herddash.log.2, are kept, so the log uses at
// most about (logKeep+1) x logMaxBytes. Both are listed under "Fixed
// timings" in docs/config.example.toml.
const (
	logMaxBytes = 10 << 20
	logKeep     = 2
)

// rotatingLog is the Daemon's log file, rotated by size. It is safe for
// concurrent use. It rotates only between writes, so a line (slog's JSON
// handler writes one record per Write) is never split across files, and a
// write larger than the limit goes whole into a fresh file. Every file it
// creates or keeps is owner-only (FR-17): an existing file with a looser mode
// is made 0600. A rotation that fails never loses the line: it is written to
// the file already open, the failure is reported to errOut, and the rotation
// is tried again when that file has grown by another limit. So while
// rotation keeps failing, each further limit of growth gives one more report,
// and the log grows past its usual bound of about (keep+1) x limit.
type rotatingLog struct {
	path   string
	limit  int64
	keep   int
	errOut io.Writer
	open   func(string) (*os.File, int64, error) // openLogFile; a test seam

	mu   sync.Mutex
	f    *os.File
	size int64 // bytes in f, counting those it held when opened
	next int64 // the size past which the next write rotates
}

// openRotatingLog opens path for appending, creating it owner-only, tightens
// it and any kept earlier files that are regular files to 0600, and counts
// its existing bytes toward limit, so a restart does not reset the size. A
// kept file that cannot be tightened is reported to errOut, not refused.
// keep is at least 1.
func openRotatingLog(path string, limit int64, keep int, errOut io.Writer) (*rotatingLog, error) {
	f, size, err := openLogFile(path)
	if err != nil {
		return nil, err
	}
	for i := 1; i <= keep; i++ {
		// Only a regular file is a kept log; anything else in its place is
		// left alone, and the rotation that meets it reports it.
		name := rotatedName(path, i)
		if fi, err := os.Lstat(name); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if err := os.Chmod(name, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(errOut, "herddash: log: %v\n", err)
		}
	}
	return &rotatingLog{path: path, limit: limit, keep: keep, errOut: errOut, open: openLogFile,
		f: f, size: size, next: limit}, nil
}

// openLogFile opens path O_APPEND, creating it 0600, makes it 0600 if it
// existed with a looser mode, and returns its current size.
func openLogFile(path string) (*os.File, int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, 0, err
	}
	// An existing file keeps its mode on open; make it owner-only (FR-17).
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, fi.Size(), nil
}

func rotatedName(path string, i int) string { return path + "." + strconv.Itoa(i) }

// Write appends p to the current file, rotating first when the file is not
// empty and p would take it past the limit.
func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > l.next {
		if err := l.rotate(); err != nil {
			fmt.Fprintf(l.errOut, "herddash: rotate log: %v\n", err)
			l.next = l.size + l.limit
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate shifts the kept files up by one, the oldest replaced, renames the
// current file to .1 and opens a new one. On an error before the new file is
// open, the file already open stays the one written to. If the renames
// succeeded but the open fails, the current file is renamed back from .1, so
// the open handle is herddash.log again; the shift of the kept files has
// already happened by then, which is fine: the old .2 is replaced and .1 is
// absent until the next rotation that succeeds.
func (l *rotatingLog) rotate() error {
	for i := l.keep - 1; i >= 1; i-- {
		if err := os.Rename(rotatedName(l.path, i), rotatedName(l.path, i+1)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	moved := true
	if err := os.Rename(l.path, rotatedName(l.path, 1)); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		moved = false
	}
	f, size, err := l.open(l.path)
	if err != nil {
		if !moved {
			return err
		}
		if uerr := os.Rename(rotatedName(l.path, 1), l.path); uerr != nil {
			return fmt.Errorf("%w; and moving it back: %w", err, uerr)
		}
		return err
	}
	if err := l.f.Close(); err != nil {
		fmt.Fprintf(l.errOut, "herddash: close rotated log: %v\n", err)
	}
	l.f, l.size, l.next = f, size, l.limit
	return nil
}

// Close closes the current file. A Write after Close returns the file's
// error.
func (l *rotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
