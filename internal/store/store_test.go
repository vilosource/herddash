package store

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// openTemp opens a fresh database in a temporary directory and closes it when
// the test ends.
func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := dbPath(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenFresh(t *testing.T) {
	s, path := openTemp(t)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created: %v", err)
	}
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode %q, want wal", mode)
	}
	if v := userVersion(t, s.db); v != schemaVersion {
		t.Errorf("user_version %d, want %d", v, schemaVersion)
	}
	var tables []string
	rows, err := s.db.Query("SELECT name FROM sqlite_schema WHERE type = 'table' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	if got := strings.Join(tables, ","); got != "resync_markers,server_state,transitions" {
		t.Errorf("tables %s, want resync_markers,server_state,transitions", got)
	}
}

func TestReopenKeepsDataAndVersion(t *testing.T) {
	path := dbPath(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO resync_markers (host, server, epoch, observed_at, reason) VALUES ('h', 's', 1, 1, 'daemon_start')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if v := userVersion(t, s.db); v != schemaVersion {
		t.Errorf("user_version %d after reopen, want %d", v, schemaVersion)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM resync_markers").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d markers after reopen, want the 1 written before", n)
	}
}

func TestOpenTwiceInOneProcess(t *testing.T) {
	_, path := openTemp(t)
	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("second Open in the same process succeeded")
	}
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), path+".lock") {
		t.Errorf("error %q: want ErrLocked naming %s.lock", err, path)
	}
}

// TestHelperHoldLock is the child process of the lock tests: it opens the
// database named by HERDDASH_TEST_DB, says so on stdout, and holds it.
func TestHelperHoldLock(t *testing.T) {
	path := os.Getenv("HERDDASH_TEST_DB")
	if path == "" {
		t.Skip("run only as a lock test's child process")
	}
	s, err := Open(path)
	if err != nil {
		os.Stdout.WriteString("error: " + err.Error() + "\n")
		os.Exit(1)
	}
	defer s.Close()
	os.Stdout.WriteString("held\n")
	time.Sleep(30 * time.Second)
}

// holdInChild starts a child process holding the database at path and
// returns once it holds the lock.
func holdInChild(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^TestHelperHoldLock$")
	child.Env = append(os.Environ(), "HERDDASH_TEST_DB="+path)
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		child.Process.Kill()
		child.Wait()
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "held\n" {
		t.Fatalf("child did not take the lock: %q, %v", line, err)
	}
	return child
}

func TestOpenLockedByAnotherProcess(t *testing.T) {
	path := dbPath(t)
	holdInChild(t, path)
	start := time.Now()
	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("Open succeeded while another process holds the lock")
	}
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), path+".lock") {
		t.Errorf("error %q: want ErrLocked naming %s.lock", err, path)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Open took %v to fail; want at once", d)
	}
}

func TestOpenAfterHolderCrashed(t *testing.T) {
	path := dbPath(t)
	child := holdInChild(t, path)
	if err := child.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open after the holder was killed: %v", err)
	}
	defer s.Close()
	if v := userVersion(t, s.db); v != schemaVersion {
		t.Errorf("user_version %d, want %d", v, schemaVersion)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	path := dbPath(t)
	// A database from a future herddash, written without Open.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE future (x); PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("Open accepted schema version 99")
	}
	if !errors.Is(err, ErrSchemaNewer) || !strings.Contains(err.Error(), "99") || !strings.Contains(err.Error(), "up to "+strconv.Itoa(schemaVersion)) {
		t.Errorf("error %q: want ErrSchemaNewer naming 99 and %d", err, schemaVersion)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the refused database was changed")
	}
	if _, err := os.Stat(path + "-wal"); err == nil {
		t.Error("a WAL file was created for the refused database")
	}
	// The refusal released the lock.
	if lock, err := takeLock(path + ".lock"); err != nil {
		t.Errorf("lock still held after the refusal: %v", err)
	} else {
		lock.Close()
	}
}

func TestFileModes(t *testing.T) {
	for _, umask := range []int{0o077, 0o022} {
		t.Run(umaskName(umask), func(t *testing.T) {
			old := syscall.Umask(umask)
			defer syscall.Umask(old)
			dir := filepath.Join(t.TempDir(), "new", "data")
			path := filepath.Join(dir, "herddash.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := s.db.Exec("PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{path, path + "-wal", path + "-shm", path + ".lock"} {
				fi, err := os.Stat(f)
				if err != nil {
					t.Errorf("%s: %v", filepath.Base(f), err)
					continue
				}
				if fi.Mode().Perm() != 0o600 {
					t.Errorf("%s mode %#o, want 0600", filepath.Base(f), fi.Mode().Perm())
				}
			}
			fi, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o700 {
				t.Errorf("created parent mode %#o, want 0700", fi.Mode().Perm())
			}
		})
	}
}

func umaskName(u int) string {
	if u == 0o077 {
		return "umask077"
	}
	return "umask022"
}

func TestOpenTightensExistingFiles(t *testing.T) {
	path := dbPath(t)
	files := []string{path, path + ".lock", path + "-wal", path + "-shm"}
	for _, f := range files {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(f, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %#o, want 0600", filepath.Base(f), fi.Mode().Perm())
		}
	}
}

func TestOpenRefusesLooseParent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "herddash.db")
	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("Open accepted a 0755 parent directory")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name %s", err, dir)
	}
	for _, f := range []string{path, path + ".lock"} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s was created under a refused directory", filepath.Base(f))
		}
	}
}

func TestOpenPathWithURICharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a?b#c%d")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "herddash.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database not created at %s: %v", path, err)
	}
}

// column is one row of PRAGMA table_info.
type column struct {
	name    string
	typ     string
	notNull bool
	pk      bool
}

func tableInfo(t *testing.T, db *sql.DB, table string) []column {
	t.Helper()
	rows, err := db.Query("SELECT name, type, \"notnull\", pk FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []column
	for rows.Next() {
		var c column
		var pk int
		if err := rows.Scan(&c.name, &c.typ, &c.notNull, &pk); err != nil {
			t.Fatal(err)
		}
		c.pk = pk > 0
		cols = append(cols, c)
	}
	return cols
}

func TestColumns(t *testing.T) {
	s, _ := openTemp(t)
	want := map[string][]column{
		"transitions": {
			{"id", "INTEGER", false, true},
			{"host", "TEXT", true, false},
			{"server", "TEXT", true, false},
			{"epoch", "INTEGER", true, false},
			{"pane_id", "TEXT", true, false},
			{"agent_program", "TEXT", false, false},
			{"profile", "TEXT", false, false},
			{"previous_status", "TEXT", false, false},
			{"new_status", "TEXT", true, false},
			{"state_change_seq", "INTEGER", false, false},
			{"herdr_version", "TEXT", false, false},
			{"provenance", "TEXT", true, false},
			{"possibly_spurious", "INTEGER", true, false},
			{"observed_at", "INTEGER", true, false},
			{"import_key", "TEXT", false, false},
			{"observed_by", "TEXT", false, false},
		},
		"resync_markers": {
			{"id", "INTEGER", false, true},
			{"host", "TEXT", true, false},
			{"server", "TEXT", true, false},
			{"epoch", "INTEGER", true, false},
			{"observed_at", "INTEGER", true, false},
			{"reason", "TEXT", true, false},
			{"detail", "TEXT", false, false},
		},
		"server_state": {
			{"host", "TEXT", true, true},
			{"server", "TEXT", true, true},
			{"epoch", "INTEGER", true, false},
			{"terminals", "TEXT", true, false},
			{"released", "TEXT", true, false},
		},
	}
	for table, cols := range want {
		got := tableInfo(t, s.db, table)
		if len(got) != len(cols) {
			t.Errorf("%s has %d columns %v, want %d", table, len(got), got, len(cols))
			continue
		}
		for i := range cols {
			if got[i] != cols[i] {
				t.Errorf("%s column %d is %+v, want %+v", table, i, got[i], cols[i])
			}
		}
	}

	// Indexes: the unique import_key, and one index per table as listed.
	indexCols := func(index string) string {
		rows, err := s.db.Query("SELECT name FROM pragma_index_info(?) ORDER BY seqno", index)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			names = append(names, n)
		}
		return strings.Join(names, ",")
	}
	indexes := func(table string) map[string]string {
		rows, err := s.db.Query("SELECT name, \"unique\", origin FROM pragma_index_list(?)", table)
		if err != nil {
			t.Fatal(err)
		}
		type idx struct {
			name, origin string
			unique       bool
		}
		var list []idx
		for rows.Next() {
			var i idx
			if err := rows.Scan(&i.name, &i.unique, &i.origin); err != nil {
				t.Fatal(err)
			}
			list = append(list, i)
		}
		rows.Close()
		got := map[string]string{}
		for _, i := range list {
			kind := "index"
			if i.unique {
				kind = "unique"
			}
			got[indexCols(i.name)] = kind + "/" + i.origin
		}
		return got
	}
	wantIdx := map[string]map[string]string{
		"transitions": {
			"import_key": "unique/u",
			"host,server,epoch,pane_id,observed_at,id": "index/c",
			"observed_at": "index/c",
		},
		"resync_markers": {"observed_at": "index/c"},
		"server_state":   {"host,server": "unique/pk"},
	}
	for table, w := range wantIdx {
		got := indexes(table)
		if len(got) != len(w) {
			t.Errorf("%s indexes %v, want %v", table, got, w)
			continue
		}
		for cols, kind := range w {
			if got[cols] != kind {
				t.Errorf("%s index on (%s) is %q, want %q", table, cols, got[cols], kind)
			}
		}
	}
}

func TestChecks(t *testing.T) {
	s, _ := openTemp(t)
	type row struct {
		epoch      int
		provenance string
		spurious   int
		seq        any
		importKey  any
	}
	insert := func(r row) error {
		_, err := s.db.Exec(`INSERT INTO transitions
			(host, server, epoch, pane_id, new_status, provenance, possibly_spurious, observed_at, state_change_seq, import_key)
			VALUES ('h', 's', ?, 'w1:p1', 'working', ?, ?, 1, ?, ?)`, r.epoch, r.provenance, r.spurious, r.seq, r.importKey)
		return err
	}
	for _, r := range []row{
		{1, "live", 0, nil, nil},
		{1, "snapshot", 0, 3, nil},
		{1, "snapshot", 0, nil, nil},
		{0, "backfill", 1, nil, "k1"},
	} {
		if err := insert(r); err != nil {
			t.Errorf("%+v refused: %v", r, err)
		}
	}
	for name, r := range map[string]row{
		"unknown provenance":             {1, "guess", 0, nil, nil},
		"possibly_spurious 2":            {1, "live", 2, nil, nil},
		"repeated import_key":            {0, "backfill", 0, nil, "k1"},
		"live under epoch 0":             {0, "live", 0, nil, nil},
		"snapshot under epoch 0":         {0, "snapshot", 0, nil, nil},
		"backfill under epoch 1":         {1, "backfill", 0, nil, "k2"},
		"backfill without import_key":    {0, "backfill", 0, nil, nil},
		"live with import_key":           {1, "live", 0, nil, "k3"},
		"snapshot with import_key":       {1, "snapshot", 0, nil, "k4"},
		"live with state_change_seq":     {1, "live", 0, 3, nil},
		"backfill with state_change_seq": {0, "backfill", 0, 3, "k5"},
	} {
		if err := insert(r); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// Unknown values default to NULL, never a placeholder.
	var program, profile, previous, version sql.NullString
	var seq sql.NullInt64
	var spurious int
	if err := s.db.QueryRow(`SELECT agent_program, profile, previous_status, state_change_seq, herdr_version, possibly_spurious
		FROM transitions WHERE provenance = 'live'`).Scan(&program, &profile, &previous, &seq, &version, &spurious); err != nil {
		t.Fatal(err)
	}
	if program.Valid || profile.Valid || previous.Valid || seq.Valid || version.Valid || spurious != 0 {
		t.Errorf("omitted columns are not NULL and 0: %v %v %v %v %v %d", program, profile, previous, seq, version, spurious)
	}
}

func TestMigrationsNumbered(t *testing.T) {
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 {
		t.Fatal("no migrations embedded")
	}
	for i, m := range ms {
		if m.version != i+1 {
			t.Errorf("migration %d is %s (version %d), want version %d", i, m.name, m.version, i+1)
		}
	}
	var names []string
	for _, m := range ms {
		names = append(names, m.name)
	}
	if got, want := strings.Join(names, ","), "0001_initial.sql,0002_server_state.sql,0003_observed_by.sql,0004_server_state_released.sql"; got != want {
		t.Errorf("migrations %s, want %s", got, want)
	}
	if len(ms) != schemaVersion {
		t.Errorf("%d migrations, but the tests expect schema version %d", len(ms), schemaVersion)
	}
}

// schemaVersion is the user_version a fully migrated database has.
const schemaVersion = 4

// TestFailedMigrationRollsBack runs a migration list whose second step fails:
// nothing from the first step survives and user_version is unchanged.
func TestFailedMigrationRollsBack(t *testing.T) {
	path := dbPath(t)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	s := &Store{db: raw}
	bad := []migration{
		{version: 1, name: "0001_ok.sql", sql: "CREATE TABLE first (x)"},
		{version: 2, name: "0002_bad.sql", sql: "CREATE TABLE nope ("},
	}
	err = s.prepare(context.Background(), path, bad)
	if err == nil || !strings.Contains(err.Error(), "0002_bad.sql") {
		t.Fatalf("error %v, want the failing migration named", err)
	}
	if v := userVersion(t, raw); v != 0 {
		t.Errorf("user_version %d after a failed migration, want 0", v)
	}
	var n int
	if err := raw.QueryRow("SELECT count(*) FROM sqlite_schema").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d schema objects survived the rolled-back migration", n)
	}
}

// rawDB writes a database at path without Open, as something else would.
func rawDB(t *testing.T, path, script string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(script); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRefusesForeignDatabase(t *testing.T) {
	path := dbPath(t)
	rawDB(t, path, "CREATE TABLE theirs (x); INSERT INTO theirs VALUES (1)")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("Open accepted a foreign SQLite database")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name %s", err, path)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the foreign database was changed")
	}
	if _, err := os.Stat(path + "-wal"); err == nil {
		t.Error("the foreign database was switched to WAL")
	}
}

func TestOpenRefusesNegativeVersion(t *testing.T) {
	path := dbPath(t)
	rawDB(t, path, "PRAGMA user_version = -1")
	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("Open accepted schema version -1")
	}
	if !strings.Contains(err.Error(), "-1") || !strings.Contains(err.Error(), path) {
		t.Errorf("error %q: want the path and version -1 named", err)
	}
}

// TestDependencyPins holds ADR-0004's pairing: the driver and its libc are
// upgraded only together, at these exact versions. A test binary's build info
// lists no dependencies, so this reads the module's go.mod.
func TestDependencyPins(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"modernc.org/sqlite": "v1.60.1", "modernc.org/libc": "v1.77.1"}
	got := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == "require" {
			f = f[1:]
		}
		if len(f) == 0 {
			continue
		}
		if f[0] == "replace" || strings.Contains(line, "=>") {
			for p := range want {
				if strings.Contains(line, p) {
					t.Errorf("go.mod replaces %s: %q", p, strings.TrimSpace(line))
				}
			}
			continue
		}
		if _, ok := want[f[0]]; ok && len(f) >= 2 {
			got[f[0]] = f[1]
		}
	}
	for p, v := range want {
		if got[p] != v {
			t.Errorf("go.mod requires %s %q, want %s (ADR-0004)", p, got[p], v)
		}
	}
}

// dbPath returns a database path in a fresh owner-only directory under
// t.TempDir(), whose own mode follows the test process's umask.
func dbPath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "herddash.db")
}
