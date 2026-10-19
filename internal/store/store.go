package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	_ "modernc.org/sqlite" // the pure-Go SQLite driver (ADR-0004)
)

// ErrLocked is wrapped by Open's error when another opener holds the
// database's lock file: a second Daemon, an importer, or the same process
// opening the database twice.
var ErrLocked = errors.New("database is in use by another herddash")

// ErrSchemaNewer is wrapped by Open's error when the database's schema
// version is above the highest migration this binary carries.
var ErrSchemaNewer = errors.New("database schema is newer than this herddash")

//go:embed migrations/*.sql
var migrationFS embed.FS

// migration is one embedded, numbered schema step.
type migration struct {
	version int
	name    string
	sql     string
}

// migrations returns the embedded migrations in version order. Their numbers
// must run 1, 2, 3... with no gap or repeat; anything else is a build mistake.
func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if !ok || len(num) != 4 || err != nil {
			return nil, fmt.Errorf("migration %q is not named NNNN_name.sql", e.Name())
		}
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{version: v, name: e.Name(), sql: string(b)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration %q: want version %d", m.name, i+1)
		}
	}
	return ms, nil
}

// Store is herddash's database: one SQLite file in WAL mode, owned by the
// process that holds its lock file.
type Store struct {
	db   *sql.DB
	lock *os.File
}

// Open opens the database at path, creating it when missing, for this process
// alone. It takes an exclusive flock on "<path>.lock" (released by Close, or
// by the OS when the process dies), requires an owner-only parent directory
// (creating it 0700 when missing), makes the database and lock files 0600,
// refuses a schema newer than this binary before writing anything, turns on
// WAL, and applies every pending migration in one transaction.
func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	if err := ownerOnlyParent(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	lock, err := takeLock(abs + ".lock")
	if err != nil {
		return nil, err
	}
	s, err := open(abs)
	if err != nil {
		lock.Close() // closing the only descriptor releases the flock
		return nil, err
	}
	s.lock = lock
	return s, nil
}

// Close closes the database and then releases the lock.
func (s *Store) Close() error {
	err := s.db.Close()
	if lerr := s.lock.Close(); err == nil {
		err = lerr
	}
	return err
}

// ownerOnlyParent creates dir 0700 when missing and refuses it when it is not
// owner-only (another owner, or any group or other permission bit), which
// covers a --database outside the data directory (FR-17).
func ownerOnlyParent(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("database directory %s: %w", dir, err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("database directory %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("database directory %s: not a directory", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("database directory %s is owned by uid %d, not by this user (uid %d)", dir, st.Uid, os.Getuid())
	}
	// The same rule as the data directory: no access at all for group or
	// others, which in particular refuses one they can write to.
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("database directory %s has mode %#o; it must be accessible only by its owner (chmod 700)", dir, perm)
	}
	return nil
}

// takeLock opens or creates the lock file 0600 and takes an exclusive,
// non-blocking flock on it. flock belongs to the open file, so even a second
// Open in the same process is refused.
func takeLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("database lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s is held; is another herddash serve or import running?", ErrLocked, path)
		}
		return nil, fmt.Errorf("database lock %s: %w", path, err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, fmt.Errorf("database lock %s: %w", path, err)
	}
	return f, nil
}

// open opens the database file under the held lock, checks its version, turns
// on WAL and migrates it.
func open(path string) (*Store, error) {
	ms, err := migrations()
	if err != nil {
		return nil, err
	}
	// Create the file ourselves so its mode is 0600 whatever the umask;
	// SQLite gives the WAL and SHM files the database file's mode.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	err = f.Chmod(0o600)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("database %s: %w", path, err)
	}

	// A file: URI with the path escaped, so a '?' or '#' in the path is a
	// file name character rather than the start of driver parameters.
	dsn := (&url.URL{Scheme: "file", Path: path}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	// One connection: herddash is a single writer. (WAL is a property of the
	// database file, not of a connection.)
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.prepare(context.Background(), path, ms); err != nil {
		db.Close()
		return nil, err
	}
	// WAL and SHM files left by an earlier run keep their mode on open;
	// make them owner-only too (FR-17).
	for _, f := range []string{path + "-wal", path + "-shm"} {
		if err := os.Chmod(f, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			db.Close()
			return nil, fmt.Errorf("database %s: %w", f, err)
		}
	}
	return s, nil
}

// prepare refuses a newer schema, turns on WAL and applies pending migrations.
func (s *Store) prepare(ctx context.Context, path string, ms []migration) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("database %s: read schema version: %w", path, err)
	}
	highest := ms[len(ms)-1].version
	if current < 0 {
		return fmt.Errorf("database %s has schema version %d; it is not a herddash database", path, current)
	}
	if current > highest {
		return fmt.Errorf("%w: %s is at schema version %d, this herddash knows up to %d; upgrade herddash",
			ErrSchemaNewer, path, current, highest)
	}
	if current == 0 {
		// Version 0 is a new, empty file. One that already holds tables is
		// some other SQLite database, given by --database: leave it alone.
		var objects int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&objects); err != nil {
			return fmt.Errorf("database %s: read schema: %w", path, err)
		}
		if objects > 0 {
			return fmt.Errorf("database %s is a SQLite database herddash did not create (schema version 0 with %d schema objects); refusing to change it", path, objects)
		}
	}

	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
		return fmt.Errorf("database %s: turn on WAL: %w", path, err)
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("database %s: journal mode is %q after asking for WAL", path, mode)
	}

	if current == highest {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("database %s: migrate: %w", path, err)
	}
	defer tx.Rollback()
	for _, m := range ms[current:] {
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("database %s: migration %s: %w", path, m.name, err)
		}
	}
	// user_version is in the database header, so it commits with the rest.
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(highest)); err != nil {
		return fmt.Errorf("database %s: set schema version: %w", path, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("database %s: migrate: %w", path, err)
	}
	return nil
}
