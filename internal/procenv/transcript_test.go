package procenv

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeTranscriptPath covers the matrix's "Slug" row: every "/" and "."
// of the working directory becomes "-", checked against the directory
// observed on 2026-10-21 (on paths where the broader rule, every character
// but a letter or digit, agrees; TestClaudeTranscriptPathRules covers where
// they differ); a session id or directory that could leave the
// projects directory is refused.
func TestClaudeTranscriptPath(t *testing.T) {
	for cwd, slug := range map[string]string{
		"/Users/jasonviloria/GitHub/herddash": "-Users-jasonviloria-GitHub-herddash",
		"/h/.config/my.proj":                  "-h--config-my-proj",
		"/":                                   "-",
	} {
		got, err := ClaudeTranscriptPath("/h/.claude-cldw", cwd, "0f0e-11_ab")
		if want := "/h/.claude-cldw/projects/" + slug + "/0f0e-11_ab.jsonl"; err != nil || got != want {
			t.Errorf("%s: %q, %v, want %q", cwd, got, err, want)
		}
	}
	for name, in := range map[string][3]string{
		"relative config": {"rel", "/w", "s"},
		"empty config":    {"", "/w", "s"},
		"relative cwd":    {"/c", "w", "s"},
		"empty cwd":       {"/c", "", "s"},
		"empty id":        {"/c", "/w", ""},
		"id with slash":   {"/c", "/w", "../../etc/passwd"},
		"id with dot":     {"/c", "/w", "a.b"},
		"id too long":     {"/c", "/w", strings.Repeat("a", 129)},
	} {
		if p, err := ClaudeTranscriptPath(in[0], in[1], in[2]); !errors.Is(err, ErrTranscript) {
			t.Errorf("%s: %q, error %v, want ErrTranscript", name, p, err)
		}
	}
}

// A path with a character only the broader rule replaces: the broader name
// is used unless only the narrower one exists.
func TestClaudeTranscriptPathRules(t *testing.T) {
	cfg := t.TempDir()
	const cwd, id = "/w/my_proj x", "s1"
	broad := filepath.Join(cfg, "projects", "-w-my-proj-x", id+".jsonl")
	narrow := filepath.Join(cfg, "projects", "-w-my_proj x", id+".jsonl")
	if got, _ := ClaudeTranscriptPath(cfg, cwd, id); got != broad {
		t.Errorf("neither exists: %q, want %q", got, broad)
	}
	if err := os.MkdirAll(filepath.Dir(narrow), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(narrow, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := ClaudeTranscriptPath(cfg, cwd, id); got != narrow {
		t.Errorf("only narrow exists: %q, want %q", got, narrow)
	}
	if err := os.MkdirAll(filepath.Dir(broad), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broad, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := ClaudeTranscriptPath(cfg, cwd, id); got != broad {
		t.Errorf("both exist: %q, want %q", got, broad)
	}
}

// tailAll runs TailTranscript once and returns the lines it gave.
func tailAll(t *testing.T, tl Tail, path string) (Tail, bool, []string, error) {
	t.Helper()
	var lines []string
	next, restarted, err := TailTranscript(tl, path, func(b []byte) { lines = append(lines, string(b)) })
	return next, restarted, lines, err
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// TestTailTranscriptLines: complete lines only, a line being written is read
// whole next time, and later reads take only what was appended.
func TestTailTranscriptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, "one\r\ntwo\nthr")
	tl, restarted, lines, err := tailAll(t, Tail{}, path)
	if err != nil || !restarted || strings.Join(lines, "|") != "one|two" || tl.Offset() != 9 {
		t.Fatalf("first read: %v %v %q offset %d", err, restarted, lines, tl.Offset())
	}
	tl, restarted, lines, err = tailAll(t, tl, path)
	if err != nil || restarted || len(lines) != 0 || tl.Offset() != 9 {
		t.Fatalf("nothing new: %v %v %q offset %d", err, restarted, lines, tl.Offset())
	}
	appendFile(t, path, "ee\nfour\n")
	tl, restarted, lines, err = tailAll(t, tl, path)
	if err != nil || restarted || strings.Join(lines, "|") != "three|four" {
		t.Fatalf("appended: %v %v %q", err, restarted, lines)
	}
}

// TestTailTranscriptReadBound: after the first read, at most 1 MiB of new
// bytes per read, so a burst is caught up over several reads.
func TestTailTranscriptReadBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, "start\n")
	tl, _, _, err := tailAll(t, Tail{}, path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 1023) + "\n"
	appendFile(t, path, strings.Repeat(line, 3*1024)) // 3 MiB
	reads := 0
	total := 0
	for total < 3*1024 {
		before := tl.Offset()
		var lines []string
		tl, _, lines, err = tailAll(t, tl, path)
		if err != nil {
			t.Fatal(err)
		}
		if tl.Offset()-before > transcriptReadBytes {
			t.Fatalf("read %d bytes, over the bound", tl.Offset()-before)
		}
		total += len(lines)
		if reads++; reads > 10 {
			t.Fatal("not caught up")
		}
	}
	if reads < 3 {
		t.Errorf("caught up 3 MiB in %d reads", reads)
	}
}

// TestTailTranscriptBigFile covers the matrix's "Big file first sight" row:
// a 50 MiB transcript is read from its last 8 MiB only, from the first full
// line in them.
func TestTailTranscriptBigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	var b bytes.Buffer
	for i := 0; b.Len() < 50<<20; i++ {
		fmt.Fprintf(&b, "line-%08d %s\n", i, strings.Repeat("y", 100))
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	size := int64(b.Len())
	var got int
	var first string
	tl, restarted, err := TailTranscript(Tail{}, path, func(l []byte) {
		if got == 0 {
			first = string(l)
		}
		got += len(l) + 1
	})
	if err != nil || !restarted {
		t.Fatalf("%v %v", err, restarted)
	}
	if got > transcriptFirstBytes || got < transcriptFirstBytes-200 {
		t.Errorf("read %d bytes of lines, want the last 8 MiB", got)
	}
	if !strings.HasPrefix(first, "line-") || len(first) != len("line-00000000 ")+100 {
		t.Errorf("first line %.30q is not a full line", first)
	}
	if tl.Offset() != size {
		t.Errorf("offset %d, want %d", tl.Offset(), size)
	}
}

// TestTailTranscriptLineAtBound: a line that starts exactly at the bound is
// read whole.
func TestTailTranscriptLineAtBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	head := strings.Repeat("h", 99) + "\n"
	tail := strings.Repeat("t", transcriptFirstBytes-1) + "\n"
	if err := os.WriteFile(path, []byte(head+tail), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, lines, err := tailAll(t, Tail{}, path)
	if err != nil || len(lines) != 1 || len(lines[0]) != transcriptFirstBytes-1 {
		t.Errorf("%v: %d lines", err, len(lines))
	}
}

// TestTailTranscriptRestart covers the matrix's "Truncated/rotated" row: a
// file that shrank, or another file at the path, is read again from the
// bound and reported restarted.
func TestTailTranscriptRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	appendFile(t, path, "a\nb\nc\n")
	tl, _, _, err := tailAll(t, Tail{}, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("d\n"), 0o600); err != nil { // shrinks
		t.Fatal(err)
	}
	tl, restarted, lines, err := tailAll(t, tl, path)
	if err != nil || !restarted || strings.Join(lines, "|") != "d" {
		t.Fatalf("shrunk: %v %v %q", err, restarted, lines)
	}
	other := filepath.Join(dir, "new.jsonl")
	if err := os.WriteFile(other, []byte("e\nf\ng\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil { // another inode, larger
		t.Fatal(err)
	}
	_, restarted, lines, err = tailAll(t, tl, path)
	if err != nil || !restarted || strings.Join(lines, "|") != "e|f|g" {
		t.Fatalf("replaced: %v %v %q", err, restarted, lines)
	}
	// Another path is another transcript: restarted too.
	p2 := filepath.Join(dir, "two.jsonl")
	appendFile(t, p2, "h\n")
	if _, restarted, lines, err = tailAll(t, tl, p2); err != nil || !restarted || strings.Join(lines, "|") != "h" {
		t.Fatalf("other path: %v %v %q", err, restarted, lines)
	}
}

// TestTailTranscriptLongLine: a line longer than a whole read is skipped,
// and the lines after it are read.
func TestTailTranscriptLongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, "a\n")
	tl, _, _, err := tailAll(t, Tail{}, path)
	if err != nil {
		t.Fatal(err)
	}
	appendFile(t, path, strings.Repeat("z", 3*transcriptReadBytes/2)+"\nb\n")
	var all []string
	for i := 0; i < 4; i++ {
		var lines []string
		tl, _, lines, err = tailAll(t, tl, path)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, lines...)
	}
	if strings.Join(all, "|") != "b" {
		t.Errorf("lines %.40q, want only b", all)
	}
}

// TestTailTranscriptUnusable: a missing file, a directory, a relative path;
// every error wraps ErrTranscript and names no path, and the Tail is kept.
func TestTailTranscriptUnusable(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "s3cr3t-project")
	if err := os.Mkdir(secret, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"missing":   filepath.Join(secret, "x.jsonl"),
		"directory": secret,
		"relative":  "s3cr3t-project/x.jsonl",
	} {
		tl := Tail{offset: 7}
		got, _, err := TailTranscript(tl, path, func([]byte) { t.Error("a line was read") })
		if !errors.Is(err, ErrTranscript) || strings.Contains(err.Error(), "s3cr3t") || got.Offset() != 7 {
			t.Errorf("%s: %v, offset %d", name, err, got.Offset())
		}
	}
	if _, _, err := TailTranscript(Tail{}, filepath.Join(secret, "x.jsonl"), func([]byte) {}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v, want fs.ErrNotExist", err)
	}
}
