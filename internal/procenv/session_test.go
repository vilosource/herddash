package procenv

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionDir makes a Claude Code configuration directory in a temporary
// directory, its sessions directory holding files as given (name to content),
// and returns the configuration directory. No test reads a real one.
func sessionDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	if err := os.Mkdir(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(sessions, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestClaudeSessionFixture pins the record's shape as observed on
// 2026-10-21: only pid, status, statusUpdatedAt, sessionId and cwd are taken
// from it.
func TestClaudeSessionFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "claude-session-2026-10-21.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := sessionDir(t, map[string]string{"4242.json": string(body)})
	s, err := ClaudeSession(dir, 4242)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Session{PID: 4242, Status: "waiting", StatusUpdatedAt: 1792595595000,
		SessionID: "00000000-0000-4000-8000-000000000000", CWD: "/h/projects/dns"}); s != want {
		t.Errorf("session %+v, want %+v", s, want)
	}
}

// TestClaudeSessionOptionalFields: sessionId and cwd are optional, and one
// that is not a string is ignored rather than failing the record, whose
// status still counts.
func TestClaudeSessionOptionalFields(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		id, cwd string
	}{
		"absent":      {`{"pid":7,"status":"busy"}`, "", ""},
		"strings":     {`{"pid":7,"status":"busy","sessionId":"s-1","cwd":"/w"}`, "s-1", "/w"},
		"not strings": {`{"pid":7,"status":"busy","sessionId":5,"cwd":{"a":1}}`, "", ""},
		"null":        {`{"pid":7,"status":"busy","sessionId":null,"cwd":null}`, "", ""},
	} {
		s, err := ClaudeSession(sessionDir(t, map[string]string{"7.json": tc.body}), 7)
		if err != nil || s.Status != "busy" || s.SessionID != tc.id || s.CWD != tc.cwd {
			t.Errorf("%s: %+v, error %v", name, s, err)
		}
	}
}

func TestClaudeSessionValues(t *testing.T) {
	for _, status := range []string{"busy", "idle", "waiting", "shell"} {
		dir := sessionDir(t, map[string]string{"7.json": `{"pid":7,"status":"` + status + `"}`})
		s, err := ClaudeSession(dir, 7)
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if s.Status != status || s.StatusUpdatedAt != 0 {
			t.Errorf("%s: %+v", status, s)
		}
	}
}

// TestClaudeSessionUnusable covers the matrix's "Record missing / bad / pid
// mismatch" row at the reader: each is an error wrapping ErrSession, and no
// error quotes the record.
func TestClaudeSessionUnusable(t *testing.T) {
	const secret = "s3cr3t-cwd"
	for name, files := range map[string]map[string]string{
		"missing":           {},
		"not json":          {"7.json": `{"pid":7,"status":"waiting",` + secret},
		"pid mismatch":      {"7.json": `{"pid":8,"status":"waiting","cwd":"` + secret + `"}`},
		"no pid":            {"7.json": `{"status":"waiting"}`},
		"pid not a number":  {"7.json": `{"pid":"7","status":"waiting"}`},
		"no status":         {"7.json": `{"pid":7}`},
		"status not text":   {"7.json": `{"pid":7,"status":3}`},
		"time not a number": {"7.json": `{"pid":7,"status":"waiting","statusUpdatedAt":"` + secret + `"}`},
		"too large":         {"7.json": `{"pid":7,"status":"waiting","cwd":"` + strings.Repeat("x", sessionMaxBytes) + `"}`},
		"other pid's file":  {"8.json": `{"pid":8,"status":"waiting"}`},
	} {
		dir := sessionDir(t, files)
		s, err := ClaudeSession(dir, 7)
		if !errors.Is(err, ErrSession) {
			t.Errorf("%s: %+v, error %v, want ErrSession", name, s, err)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error quotes the record: %v", name, err)
		}
	}
	for name, dir := range map[string]string{"empty dir": "", "relative dir": "rel/.claude"} {
		if _, err := ClaudeSession(dir, 7); !errors.Is(err, ErrSession) {
			t.Errorf("%s: error %v, want ErrSession", name, err)
		}
	}
	if _, err := ClaudeSession(t.TempDir(), 0); !errors.Is(err, ErrSession) {
		t.Errorf("pid 0: error %v, want ErrSession", err)
	}
	dir := sessionDir(t, nil)
	if err := os.Mkdir(filepath.Join(dir, "sessions", "7.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaudeSession(dir, 7); !errors.Is(err, ErrSession) {
		t.Errorf("directory: error %v, want ErrSession", err)
	}
}

// TestClaudeSessionNeverKey covers the matrix's "Never .key" row: with the
// sessions directory holding <pid>.<hash>.key files that cannot be opened, and
// the directory itself not listable, the record still reads, so the reader
// opened the one path it names and nothing else; and with only a .key file
// for the pid, there is no record.
func TestClaudeSessionNeverKey(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens files whatever their mode")
	}
	dir := sessionDir(t, map[string]string{
		"7.json":         `{"pid":7,"status":"waiting","statusUpdatedAt":5}`,
		"7.abc123.key":   "secret",
		"7.json.abc.key": "secret",
		"9.def456.key":   "secret",
	})
	sessions := filepath.Join(dir, "sessions")
	for _, k := range []string{"7.abc123.key", "7.json.abc.key", "9.def456.key"} {
		if err := os.Chmod(filepath.Join(sessions, k), 0); err != nil {
			t.Fatal(err)
		}
	}
	// Search only: a listing of the directory fails.
	if err := os.Chmod(sessions, 0o100); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sessions, 0o700) })
	if _, err := os.ReadDir(sessions); err == nil {
		t.Fatal("the sessions directory is still listable")
	}
	s, err := ClaudeSession(dir, 7)
	if err != nil || s.Status != "waiting" || s.StatusUpdatedAt != 5 {
		t.Fatalf("session %+v, error %v", s, err)
	}
	if _, err := ClaudeSession(dir, 9); !errors.Is(err, ErrSession) {
		t.Errorf("pid 9 with only a .key file: error %v, want ErrSession", err)
	}
}

// TestClaudeSessionLastingKinds: the two failures the engine treats as
// lasting are told apart: a missing record wraps fs.ErrNotExist, one naming
// another process wraps ErrSessionOtherProcess; a body that does not decode
// wraps neither.
func TestClaudeSessionLastingKinds(t *testing.T) {
	if _, err := ClaudeSession(sessionDir(t, nil), 7); !errors.Is(err, fs.ErrNotExist) || !errors.Is(err, ErrSession) {
		t.Errorf("missing: error %v, want ErrSession and fs.ErrNotExist", err)
	}
	dir := sessionDir(t, map[string]string{"7.json": `{"pid":8,"status":"waiting"}`})
	if _, err := ClaudeSession(dir, 7); !errors.Is(err, ErrSessionOtherProcess) || !errors.Is(err, ErrSession) {
		t.Errorf("other pid: error %v, want ErrSession and ErrSessionOtherProcess", err)
	}
	dir = sessionDir(t, map[string]string{"7.json": `{"pid":7,`})
	if _, err := ClaudeSession(dir, 7); errors.Is(err, ErrSessionOtherProcess) || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("not json: error %v, want neither lasting kind", err)
	}
}
