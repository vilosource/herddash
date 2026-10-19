package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/store"
)

// importFixture is the importer's fixture: 21 lines trimmed from the real
// plugin log, Profiles anonymised. Twelve are done entries, all before
// 2026-10-19, and three are for pane w1:pE.
var importFixture = filepath.Join("..", "..", "internal", "importer", "testdata", "hook.log")

func rowCount(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM transitions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestImportFlags(t *testing.T) {
	hermeticEnv(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"no since", []string{importFixture}, "--herdr-0.9.2-since is required"},
		{"empty since", []string{importFixture, "--herdr-0.9.2-since", ""}, "is not a date"},
		{"bad since", []string{importFixture, "--herdr-0.9.2-since", "2026-10-32"}, "is not a date"},
		{"since is a time", []string{importFixture, "--herdr-0.9.2-since", "2026-10-19T00:00:00"}, "is not a date"},
		{"no file", []string{"--herdr-0.9.2-since", "2026-10-19"}, "takes one log file"},
		{"two files", []string{importFixture, importFixture, "--herdr-0.9.2-since", "2026-10-19"}, "takes one log file"},
		{"flags after --", []string{"--herdr-0.9.2-since", "2026-10-19", "--", importFixture, "--host", "mac"}, "takes one log file"},
		{"unknown flag", []string{importFixture, "--herdr-0.9.2-since", "2026-10-19", "--nope"}, "flag provided but not defined"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr syncBuffer
			args := append([]string{"import-plugin-log", "--data-dir", dataDir}, c.args...)
			if code := run(context.Background(), args, &stdout, &stderr); code != 2 {
				t.Fatalf("exit code %d, want 2; stderr %q", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), c.want) || stdout.String() != "" {
				t.Errorf("stdout %q, stderr %q; want stderr to say %q", stdout.String(), stderr.String(), c.want)
			}
		})
	}
	if _, err := os.Stat(dataDir); err == nil {
		t.Error("a usage error created the data directory")
	}
}

// TestImportDashDash takes every argument after "--" as the file, even one
// that looks like a flag.
func TestImportDashDash(t *testing.T) {
	hermeticEnv(t)
	dir := t.TempDir()
	b, err := os.ReadFile(importFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "-weird.log"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var stdout, stderr syncBuffer
	args := []string{"import-plugin-log", "--data-dir", filepath.Join(dir, "data"), "--retention-days", "36500",
		"--herdr-0.9.2-since", "2026-10-19", "--", "-weird.log"}
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "imported 21 of 21 entries") {
		t.Errorf("stdout %q", stdout.String())
	}
}

// TestImportBadFile refuses a log that is missing or not a regular file with
// exit 1, before the data directory is created.
func TestImportBadFile(t *testing.T) {
	hermeticEnv(t)
	dir := t.TempDir()
	for name, file := range map[string]string{"missing": filepath.Join(dir, "nope.log"), "directory": dir} {
		t.Run(name, func(t *testing.T) {
			dataDir := filepath.Join(t.TempDir(), "data")
			var stdout, stderr syncBuffer
			args := []string{"import-plugin-log", file, "--herdr-0.9.2-since", "2026-10-19", "--data-dir", dataDir}
			if code := run(context.Background(), args, &stdout, &stderr); code != 1 {
				t.Fatalf("exit code %d, want 1; stderr %q", code, stderr.String())
			}
			if got := stderr.String(); !strings.HasPrefix(got, "herddash: ") || !strings.Contains(got, file) {
				t.Errorf("stderr %q, want herddash: <err> naming %s", got, file)
			}
			if _, err := os.Stat(dataDir); err == nil {
				t.Error("the data directory was created")
			}
		})
	}
}

// TestImport imports the fixture twice, with the flags before and after the
// file: the first run adds every entry and flags every done entry, the second
// adds and flags nothing. The rows carry the Host and Server serve would use. Retention
// is at its maximum so the fixture never ages out of it.
func TestImport(t *testing.T) {
	hermeticEnv(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	for i, args := range [][]string{
		{"import-plugin-log", importFixture, "--herdr-0.9.2-since", "2026-10-19", "--host", "mac", "--data-dir", dataDir, "--retention-days", "36500"},
		{"import-plugin-log", "--data-dir", dataDir, "--retention-days=36500", "--host=mac", "--herdr-0.9.2-since=2026-10-19", importFixture},
	} {
		var stdout, stderr syncBuffer
		if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatalf("run %d: exit code %d, stderr %q", i, code, stderr.String())
		}
		want := "imported 21 of 21 entries (0 already present, 0 older than retention, 12 flagged possibly spurious, 0 unparsed)\n"
		if i == 1 {
			want = "imported 0 of 21 entries (21 already present, 0 older than retention, 0 flagged possibly spurious, 0 unparsed)\n"
		}
		if stdout.String() != want || stderr.String() != "" {
			t.Errorf("run %d: stdout %q, stderr %q; want stdout %q", i, stdout.String(), stderr.String(), want)
		}
	}
	path := filepath.Join(dataDir, "herddash.db")
	if n := rowCount(t, path); n != 21 {
		t.Errorf("%d rows, want 21", n)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// --host mac and no socket flag: Server is the default socket's, "default".
	h, err := st.History(context.Background(), core.AgentKey{Host: "mac", Server: "default", Epoch: 0, PaneID: "w1:pE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 5 {
		t.Fatalf("%d rows for mac/default w1:pE, want 5", len(h))
	}
	for _, tr := range h {
		if tr.Provenance() != core.ProvenanceBackfill || tr.PossiblySpurious() != (tr.New() == core.StatusDone) {
			t.Errorf("row %+v", tr)
		}
	}
}

// TestImportWhileServing runs import beside a running serve: it exits 1 at
// once naming the lock and writes nothing. Then, once serve has stopped, the
// import runs, and a serve started after it never makes an epoch-0 row a live
// Agent: the record's live history of w1:p1 holds none of the imported rows.
func TestImportWhileServing(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	path := filepath.Join(dataDir, "herddash.db")
	log := filepath.Join(t.TempDir(), "hook.log")
	b, err := os.ReadFile(importFixture)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture plus entries for the fake herdr's own panes.
	b = append(b, []byte("2026-10-19 09:00:00 posted blocked for w1:p1 -> 204\n"+
		`2026-10-19 09:01:00 no url configured; would have sent {"agent": "claude", "profile": "work", "status": "done", "pane_id": "w1:p2", "workspace_id": "w1", "title": null}`+"\n")...)
	if err := os.WriteFile(log, b, 0o600); err != nil {
		t.Fatal(err)
	}
	importArgs := []string{"import-plugin-log", log, "--herdr-0.9.2-since", "2026-10-19",
		"--socket", f.path, "--server", "test", "--host", "testhost", "--data-dir", dataDir, "--retention-days", "36500"}

	serveUntil := func(want string, during func()) string {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var stdout, stderr syncBuffer
		done := make(chan int, 1)
		go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
		waitFor(t, want, func() bool { return strings.Contains(stdout.String(), want) })
		during()
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("serve exit code %d, stderr %q", code, stderr.String())
		}
		return stdout.String()
	}

	var rowsWhileServing int
	serveUntil("agent      host=testhost epoch=1 pane=w1:p1", func() {
		var stdout, stderr syncBuffer
		if code := run(context.Background(), importArgs, &stdout, &stderr); code != 1 {
			t.Fatalf("import beside serve: exit code %d, want 1; stderr %q", code, stderr.String())
		}
		lock := path + ".lock"
		if got := stderr.String(); !strings.HasPrefix(got, "herddash: ") || !strings.Contains(got, lock) || stdout.String() != "" {
			t.Errorf("stdout %q, stderr %q; want herddash: <err> naming %s", stdout.String(), got, lock)
		}
	})
	rowsWhileServing = rowCount(t, path)

	var stdout, stderr syncBuffer
	if code := run(context.Background(), importArgs, &stdout, &stderr); code != 0 {
		t.Fatalf("import: exit code %d, stderr %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "imported 23 of 23 entries") {
		t.Errorf("import printed %q", stdout.String())
	}
	if n := rowCount(t, path); n != rowsWhileServing+23 {
		t.Errorf("%d rows after the import, want %d: the refused import wrote, or this one did not", n, rowsWhileServing+23)
	}

	out := serveUntil("agent      host=testhost epoch=1 pane=w1:p1", func() {})
	if strings.Contains(out, "epoch=0") {
		t.Errorf("serve after an import shows an epoch-0 Agent:\n%s", out)
	}

	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	live, err := st.History(context.Background(), core.AgentKey{Host: "testhost", Server: "test", Epoch: 1, PaneID: "w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(live) == 0 {
		t.Fatal("no live history for w1:p1")
	}
	for _, tr := range live {
		if tr.Provenance() == core.ProvenanceBackfill || tr.Key().Epoch == 0 {
			t.Errorf("live history of w1:p1 holds an imported row: %+v", tr)
		}
	}
	// The fold the engine seeds from that history never sees the imported
	// blocked entry: w1:p1 is still working, from its own first sight.
	if fold := core.Fold(live); fold.Class != core.ClassWorking {
		t.Errorf("live fold %+v, want working", fold)
	}
	backfill, err := st.History(context.Background(), core.AgentKey{Host: "testhost", Server: "test", Epoch: 0, PaneID: "w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(backfill) != 2 {
		t.Errorf("%d imported rows for w1:p1, want 2", len(backfill))
	}
}
