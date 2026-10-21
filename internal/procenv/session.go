package procenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// ErrSession is wrapped by every ClaudeSession error: the record is missing,
// too large, not the shape observed, or names another process.
var ErrSession = errors.New("procenv: claude session record not usable")

// ErrSessionOtherProcess is wrapped, with ErrSession, by a ClaudeSession error
// for a record that names another process than the one asked for: the
// process asked for has gone and its pid was reused, a lasting failure.
var ErrSessionOtherProcess = errors.New("names another process")

// sessionMaxBytes bounds one session record read. The records observed on
// 2026-10-21 are a few hundred bytes.
const sessionMaxBytes = 64 << 10

// Session is the part of Claude Code's per-process session record herddash
// reads (ADR-0008): its process, its status and when that status was set.
// Every other field of the record is ignored and never decoded.
type Session struct {
	PID    int
	Status string
	// StatusUpdatedAt is when Status was set, in UTC Unix milliseconds; 0
	// when the record does not say.
	StatusUpdatedAt int64
}

// ClaudeSession reads Claude Code's session record for the process pid under
// the Claude Code configuration directory configDir: exactly the one file
// <configDir>/sessions/<pid>.json, never any other file in that directory and
// never a .key file. It decodes only pid, status and statusUpdatedAt, and
// requires pid to equal the process asked for. Only a regular file is read:
// a symbolic link, a FIFO or any other kind of file is refused, and is
// never opened where the platform can avoid it. A missing or unreadable file,
// one over 64 KiB, a body that does not decode or a status that is not a
// string, and a pid that differs are each an error wrapping ErrSession: never
// a guess. The errors name the path and the kind of failure, never the
// record's content.
func ClaudeSession(configDir string, pid int) (Session, error) {
	if pid <= 0 {
		return Session{}, fmt.Errorf("%w: pid %d: not a process id", ErrSession, pid)
	}
	if configDir == "" || !filepath.IsAbs(configDir) {
		return Session{}, fmt.Errorf("%w: configuration directory %q is not an absolute path", ErrSession, configDir)
	}
	path := filepath.Join(configDir, "sessions", strconv.Itoa(pid)+".json")
	// Lstat first: a symbolic link, a FIFO or a device is refused before it
	// is opened, so a read never follows a link or blocks on a FIFO.
	before, err := os.Lstat(path)
	if err != nil {
		return Session{}, fmt.Errorf("%w: %s: %w", ErrSession, path, unwrapPath(err))
	}
	if !before.Mode().IsRegular() {
		return Session{}, fmt.Errorf("%w: %s: not a regular file", ErrSession, path)
	}
	// Opened without following a link or blocking where the platform allows,
	// in case the path was replaced since; checked again once open.
	f, err := os.OpenFile(path, os.O_RDONLY|sessionOpenFlags, 0)
	if err != nil {
		return Session{}, fmt.Errorf("%w: %s: %w", ErrSession, path, unwrapPath(err))
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil {
		return Session{}, fmt.Errorf("%w: %s: %w", ErrSession, path, unwrapPath(err))
	} else if !fi.Mode().IsRegular() || !os.SameFile(before, fi) {
		return Session{}, fmt.Errorf("%w: %s: not a regular file", ErrSession, path)
	}
	data, err := io.ReadAll(io.LimitReader(f, sessionMaxBytes+1))
	if err != nil {
		return Session{}, fmt.Errorf("%w: %s: %w", ErrSession, path, unwrapPath(err))
	}
	if len(data) > sessionMaxBytes {
		return Session{}, fmt.Errorf("%w: %s: larger than %d bytes", ErrSession, path, sessionMaxBytes)
	}
	return parseSession(path, data, pid)
}

// parseSession decodes the three fields ClaudeSession reads from data. The
// decoding error itself is not wrapped: it may quote the record.
func parseSession(path string, data []byte, pid int) (Session, error) {
	var rec struct {
		PID             *int    `json:"pid"`
		Status          *string `json:"status"`
		StatusUpdatedAt *int64  `json:"statusUpdatedAt"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&rec); err != nil {
		return Session{}, fmt.Errorf("%w: %s: does not decode as the observed record", ErrSession, path)
	}
	switch {
	case rec.PID == nil:
		return Session{}, fmt.Errorf("%w: %s: no pid", ErrSession, path)
	case *rec.PID != pid:
		return Session{}, fmt.Errorf("%w: %s: %w", ErrSession, path, ErrSessionOtherProcess)
	case rec.Status == nil:
		return Session{}, fmt.Errorf("%w: %s: no status", ErrSession, path)
	}
	s := Session{PID: pid, Status: *rec.Status}
	if rec.StatusUpdatedAt != nil {
		s.StatusUpdatedAt = *rec.StatusUpdatedAt
	}
	return s, nil
}

// unwrapPath drops the *PathError around err, whose path the caller already
// names.
func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
