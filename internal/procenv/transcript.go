package procenv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrTranscript is wrapped by every TailTranscript and ClaudeTranscriptPath
// error: the transcript cannot be named or read. Its errors never name the
// path, which holds the session's working directory, nor any content.
var ErrTranscript = errors.New("procenv: claude transcript not usable")

// The transcript bounds (ADR-0009). The first read of a transcript takes at
// most its last transcriptFirstBytes; every later read at most
// transcriptReadBytes of what was appended since.
const (
	transcriptFirstBytes = 8 << 20
	transcriptReadBytes  = 1 << 20
)

// ClaudeTranscriptPath is the transcript of the Claude Code session sessionID
// whose working directory is cwd, under the configuration directory configDir
// (ADR-0009): <configDir>/projects/<cwd with every character but a letter or
// digit replaced by "-">/<sessionID>.jsonl, or with only "/" and "." replaced
// when only that path exists. configDir and cwd must be absolute, and sessionID
// one path element of letters, digits, "-" and "_", so the path never leaves
// the projects directory.
func ClaudeTranscriptPath(configDir, cwd, sessionID string) (string, error) {
	switch {
	case configDir == "" || !filepath.IsAbs(configDir):
		return "", fmt.Errorf("%w: configuration directory is not an absolute path", ErrTranscript)
	case cwd == "" || !strings.HasPrefix(cwd, "/"):
		return "", fmt.Errorf("%w: the session record holds no absolute working directory", ErrTranscript)
	case !safeID(sessionID):
		return "", fmt.Errorf("%w: the session record holds no usable session id", ErrTranscript)
	}
	// Every running session on this machine on 2026-10-21 (15, four config
	// directories) fits both rules below; none of their paths held a
	// character that tells them apart. So the broader rule, every character
	// but a letter or digit, is tried first and the narrower one, "/" and "."
	// only, when only its path exists.
	dir := filepath.Join(configDir, "projects")
	name := sessionID + ".jsonl"
	broad := filepath.Join(dir, strings.Map(slugRune, cwd), name)
	narrow := filepath.Join(dir, strings.NewReplacer("/", "-", ".", "-").Replace(cwd), name)
	if broad != narrow {
		if _, err := os.Lstat(broad); err != nil {
			if _, err := os.Lstat(narrow); err == nil {
				return narrow, nil
			}
		}
	}
	return broad, nil
}

// slugRune keeps a letter or digit and turns anything else into "-", as
// Claude Code names a project's directory.
func slugRune(r rune) rune {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return r
	}
	return '-'
}

// safeID reports whether id is 1 to 128 letters, digits, "-" or "_".
func safeID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Tail is where a transcript was last read to: kept in memory per Agent and
// passed back to TailTranscript for the next read. Its zero value has read
// nothing.
type Tail struct {
	path   string
	file   os.FileInfo
	offset int64
	// skip is set inside a line longer than a read: its bytes are dropped up
	// to the next newline.
	skip bool
}

// Offset is the byte offset the next read starts at.
func (t Tail) Offset() int64 { return t.offset }

// TailTranscript reads the complete lines appended to the transcript at path
// since t, calling line for each, without its newline, and returns where it
// read to. line's argument is valid only during the call: it must not be
// kept.
//
// The first read of a path, and a read of a file that shrank or was replaced
// (another inode), restarts from the bound: at most the last 8 MiB, from the
// first line starting in them; restarted is then true, and whatever was
// derived from earlier reads is to be dropped. Every other read takes at most
// 1 MiB of new bytes, and only complete lines: a line still being written is
// read whole next time. A line longer than a whole read is skipped. Only a
// regular file is read, never through a symbolic link, and a FIFO never
// blocks the read. Every error wraps ErrTranscript and names the kind of
// failure only; t is returned unchanged with it.
func TailTranscript(t Tail, path string, line func([]byte)) (next Tail, restarted bool, err error) {
	if path == "" || !filepath.IsAbs(path) {
		return t, false, fmt.Errorf("%w: not an absolute path", ErrTranscript)
	}
	f, fi, err := openRegular(path)
	if err != nil {
		return t, false, fmt.Errorf("%w: %w", ErrTranscript, err)
	}
	defer f.Close()
	size := fi.Size()
	next = t
	limit := int64(transcriptReadBytes)
	if t.path != path || t.file == nil || !os.SameFile(t.file, fi) || size < t.offset {
		restarted = true
		limit = transcriptFirstBytes
		next = Tail{path: path}
		if size > transcriptFirstBytes {
			// From the byte before the bound, dropped up to its first
			// newline: a line that starts exactly at the bound is kept.
			next.offset, next.skip = size-transcriptFirstBytes-1, true
			limit++
		}
	}
	next.file = fi
	n := min(size-next.offset, limit)
	if n <= 0 {
		return next, restarted, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(f, next.offset, n), buf); err != nil {
		return t, false, fmt.Errorf("%w: %w", ErrTranscript, unwrapPath(err))
	}
	used := 0
	if next.skip {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			next.offset += n
			return next, restarted, nil
		}
		used, next.skip = i+1, false
	}
	for {
		i := bytes.IndexByte(buf[used:], '\n')
		if i < 0 {
			break
		}
		line(bytes.TrimSuffix(buf[used:used+i], []byte{'\r'}))
		used += i + 1
	}
	if used == 0 && n == limit {
		// No newline in a whole read: a line longer than the bound, skipped.
		next.offset, next.skip = next.offset+n, true
		return next, restarted, nil
	}
	next.offset += int64(used)
	return next, restarted, nil
}
