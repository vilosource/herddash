package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// logLine is one newline-terminated line of exactly n bytes.
func logLine(c byte, n int) []byte {
	b := bytes.Repeat([]byte{c}, n)
	b[n-1] = '\n'
	return b
}

func openTestLog(t *testing.T, path string, limit int64, keep int, errOut *syncBuffer) *rotatingLog {
	t.Helper()
	l, err := openRotatingLog(path, limit, keep, errOut)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func mustWrite(t *testing.T, l *rotatingLog, p []byte) {
	t.Helper()
	n, err := l.Write(p)
	if err != nil || n != len(p) {
		t.Fatalf("Write: %d, %v; want %d, nil", n, err, len(p))
	}
}

func readOrEmpty(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertMode(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("%s mode %#o, want 0600", filepath.Base(path), fi.Mode().Perm())
	}
}

func TestRotatingLogBelowLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	var errOut syncBuffer
	l := openTestLog(t, path, 100, 2, &errOut)
	for _, c := range []byte("abcd") {
		mustWrite(t, l, logLine(c, 25)) // 100 bytes in all: at the limit, not past it
	}
	if got := readOrEmpty(t, path); len(got) != 100 {
		t.Errorf("log holds %d bytes, want 100", len(got))
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("herddash.log.1 exists below the limit (%v)", err)
	}
	if errOut.String() != "" {
		t.Errorf("stderr %q", errOut.String())
	}
}

func TestRotatingLogCrossing(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	var errOut syncBuffer
	l := openTestLog(t, path, 100, 2, &errOut)
	mustWrite(t, l, logLine('a', 60))
	mustWrite(t, l, logLine('b', 41)) // 101: would pass the limit
	if g, w := readOrEmpty(t, path+".1"), string(logLine('a', 60)); g != w {
		t.Errorf("herddash.log.1 = %q, want %q", g, w)
	}
	if g, w := readOrEmpty(t, path), string(logLine('b', 41)); g != w {
		t.Errorf("herddash.log = %q, want %q", g, w)
	}
	assertMode(t, path)
	assertMode(t, path+".1")
}

func TestRotatingLogThirdRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	var errOut syncBuffer
	l := openTestLog(t, path, 100, 2, &errOut)
	for _, c := range []byte("abcd") {
		mustWrite(t, l, logLine(c, 80)) // each write after the first rotates
	}
	for name, want := range map[string]byte{".2": 'b', ".1": 'c', "": 'd'} {
		if g, w := readOrEmpty(t, path+name), string(logLine(want, 80)); g != w {
			t.Errorf("herddash.log%s = %q, want %q", name, g, w)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("herddash.log.3 exists (%v); only two earlier files are kept", err)
	}
	for _, name := range []string{"", ".1", ".2"} {
		assertMode(t, path+name)
	}
	if errOut.String() != "" {
		t.Errorf("stderr %q", errOut.String())
	}
}

func TestRotatingLogRestartNearLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	old := string(logLine('o', 95))
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	var errOut syncBuffer
	l := openTestLog(t, path, 100, 2, &errOut)
	mustWrite(t, l, logLine('x', 5)) // 100: still within
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("rotated at the limit, not past it (%v)", err)
	}
	mustWrite(t, l, logLine('y', 5)) // 105: crosses
	if g, w := readOrEmpty(t, path+".1"), old+string(logLine('x', 5)); g != w {
		t.Errorf("herddash.log.1 = %q, want %q", g, w)
	}
	if g, w := readOrEmpty(t, path), string(logLine('y', 5)); g != w {
		t.Errorf("herddash.log = %q, want %q", g, w)
	}
}

func TestRotatingLogLooseMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, logFileName)
	for _, name := range []string{"", ".1", ".2"} {
		if err := os.WriteFile(path+name, []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path+name, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var errOut syncBuffer
	l := openTestLog(t, path, 100, 2, &errOut)
	for _, name := range []string{"", ".1", ".2"} {
		assertMode(t, path+name)
	}
	// They stay owner-only through a rotation, and so does the new file.
	mustWrite(t, l, logLine('a', 100))
	for _, name := range []string{"", ".1", ".2"} {
		assertMode(t, path+name)
	}
	if errOut.String() != "" {
		t.Errorf("stderr %q", errOut.String())
	}
}

func TestRotatingLogHugeLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	var errOut syncBuffer
	l := openTestLog(t, path, 100, 2, &errOut)
	huge := logLine('h', 250)
	// Into an empty file it is written whole, without a rotation.
	mustWrite(t, l, huge)
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("an empty file was rotated (%v)", err)
	}
	// Into a non-empty one it rotates first, then goes whole into the new file.
	mustWrite(t, l, logLine('s', 10))
	mustWrite(t, l, huge)
	if g, w := readOrEmpty(t, path), string(huge); g != w {
		t.Errorf("herddash.log = %q, want the whole line", g)
	}
	if g, w := readOrEmpty(t, path+".1"), string(logLine('s', 10)); g != w {
		t.Errorf("herddash.log.1 = %q, want %q", g, w)
	}
	if g, w := readOrEmpty(t, path+".2"), string(huge); g != w {
		t.Errorf("herddash.log.2 = %q, want the first huge line", g)
	}
}

func TestRotatingLogRotationFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	if err := os.WriteFile(path+".1", []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// herddash.log.1 cannot be renamed onto a non-empty directory.
	if err := os.MkdirAll(filepath.Join(path+".2", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	var errOut syncBuffer
	l := openTestLog(t, path, 100, 2, &errOut)
	mustWrite(t, l, logLine('a', 80))
	mustWrite(t, l, logLine('b', 80)) // crosses; the rotation fails
	mustWrite(t, l, logLine('c', 15)) // 175, within another limit of 80: not tried again
	if g, w := readOrEmpty(t, path), string(logLine('a', 80))+string(logLine('b', 80))+string(logLine('c', 15)); g != w {
		t.Errorf("herddash.log = %q, want every line", g)
	}
	if g := strings.Count(errOut.String(), "herddash: rotate log:"); g != 1 {
		t.Errorf("stderr reports %d failures, want 1: %q", g, errOut.String())
	}
	if g := readOrEmpty(t, path+".1"); g != "one\n" {
		t.Errorf("herddash.log.1 = %q, want it untouched", g)
	}

	// Once the obstacle is gone, the next limit crossing (past 180) rotates.
	if err := os.RemoveAll(path + ".2"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, l, logLine('d', 80))
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal(err)
	}
	if g := readOrEmpty(t, path); g != string(logLine('d', 80)) {
		t.Errorf("herddash.log = %q after the retried rotation", g)
	}
	if g := readOrEmpty(t, path+".2"); g != "one\n" {
		t.Errorf("herddash.log.2 = %q, want the earlier .1", g)
	}
	if g := strings.Count(errOut.String(), "herddash: rotate log:"); g != 1 {
		t.Errorf("stderr reports %d failures, want still 1: %q", g, errOut.String())
	}
}

// TestRotatingLogReopenFails makes the open of the new herddash.log fail once,
// after both renames succeeded: the current file is moved back, so the line
// lands in herddash.log; the kept files have shifted (the old .2 replaced by
// the old .1, .1 absent), and the next crossing rotates normally.
func TestRotatingLogReopenFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	if err := os.WriteFile(path+".1", []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".2", []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errOut syncBuffer
	l := openTestLog(t, path, 100, 2, &errOut)
	fail := true
	l.open = func(name string) (*os.File, int64, error) {
		if fail {
			fail = false
			return nil, 0, errors.New("open refused")
		}
		return openLogFile(name)
	}
	mustWrite(t, l, logLine('a', 80))
	mustWrite(t, l, logLine('b', 80)) // crosses; the reopen fails
	if g, w := readOrEmpty(t, path), string(logLine('a', 80))+string(logLine('b', 80)); g != w {
		t.Errorf("herddash.log = %q, want both lines", g)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("herddash.log.1 exists after the undo (%v)", err)
	}
	if g := readOrEmpty(t, path+".2"); g != "one\n" {
		t.Errorf("herddash.log.2 = %q, want the old .1", g)
	}
	if g := strings.Count(errOut.String(), "herddash: rotate log:"); g != 1 || !strings.Contains(errOut.String(), "open refused") {
		t.Errorf("stderr %q, want one report of the open", errOut.String())
	}

	mustWrite(t, l, logLine('c', 30)) // 190, past another limit from 80: rotates
	if g, w := readOrEmpty(t, path+".1"), string(logLine('a', 80))+string(logLine('b', 80)); g != w {
		t.Errorf("herddash.log.1 = %q, want the lines written before", g)
	}
	if g, w := readOrEmpty(t, path), string(logLine('c', 30)); g != w {
		t.Errorf("herddash.log = %q, want %q", g, w)
	}
	if g := readOrEmpty(t, path+".2"); g != "one\n" {
		t.Errorf("herddash.log.2 = %q, want it kept (.1 was absent)", g)
	}
	if g := strings.Count(errOut.String(), "herddash: rotate log:"); g != 1 {
		t.Errorf("stderr reports %d failures, want still 1", g)
	}
}

func TestRotatingLogConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	var errOut syncBuffer
	const writers, lines, keep = 8, 50, 1000
	l := openTestLog(t, path, 512, keep, &errOut)
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range lines {
				if _, err := fmt.Fprintf(l, "{\"writer\":%d,\"line\":%03d,\"pad\":\"%s\"}\n", w, i, strings.Repeat("p", w*3)); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	l.Close()

	seen := map[string]bool{}
	files := 0
	for i := 0; i <= keep; i++ {
		name := path
		if i > 0 {
			name = rotatedName(path, i)
		}
		b, err := os.ReadFile(name)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files++
		if len(b) > 512 {
			t.Errorf("%s holds %d bytes, past the limit", filepath.Base(name), len(b))
		}
		if len(b) == 0 || b[len(b)-1] != '\n' {
			t.Errorf("%s does not end on a line boundary", filepath.Base(name))
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			var w, n int
			var pad string
			if _, err := fmt.Sscanf(line, "{\"writer\":%d,\"line\":%d,\"pad\":%q}", &w, &n, &pad); err != nil || pad != strings.Repeat("p", w*3) {
				t.Errorf("torn line %q in %s", line, filepath.Base(name))
				continue
			}
			seen[line] = true
		}
	}
	if len(seen) != writers*lines {
		t.Errorf("found %d distinct lines in %d files, want %d", len(seen), files, writers*lines)
	}
	if files < 2 {
		t.Errorf("only %d file(s): the test never rotated", files)
	}
	if errOut.String() != "" {
		t.Errorf("stderr %q", errOut.String())
	}
}
