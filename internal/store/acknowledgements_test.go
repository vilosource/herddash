package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/vilosource/herddash/internal/core"
)

func mustAck(t *testing.T, key core.AgentKey, since int64, seen bool, at int64) core.Acknowledgement {
	t.Helper()
	a, err := core.NewAcknowledgement(core.AcknowledgementInput{Source: core.AckSourceBoard, Key: key, FinishedSince: since, Seen: seen, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// liveAt appends one live row for key moving from prev ("" for none, an
// Agent's first row) to next at ms.
func liveAt(t *testing.T, s *Store, key core.AgentKey, prev, next core.Status, at int64) {
	t.Helper()
	in := core.TransitionInput{Key: key, AgentProgram: "claude", Profile: "claude", New: next,
		HerdrVersion: "0.9.5", ObservedAt: at, Provenance: core.ProvenanceLive}
	if prev != "" {
		in.Previous = &prev
	}
	if err := s.Append(mustTransition(t, in)); err != nil {
		t.Fatal(err)
	}
}

func ackCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM acknowledgements`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// dumpTable is every row of table, every column, in rowid order, as text.
func dumpTable(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM ` + table + ` ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			if bs, ok := v.([]byte); ok {
				vals[i] = string(bs)
			}
		}
		fmt.Fprintln(&b, vals...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// schema4 creates a database at path migrated only to schema 4, as 0.7.x
// leaves it, holding Transitions, resync markers and server_state, and
// returns its open handle.
func schema4(t *testing.T, path string) *sql.DB {
	t.Helper()
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	old := &Store{db: raw}
	if err := old.prepare(context.Background(), path, ms[:4]); err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, raw); v != 4 {
		t.Fatalf("old database at %d, want 4", v)
	}
	liveAt(t, old, keyP1, "", core.StatusWorking, 10)
	liveAt(t, old, keyP1, core.StatusWorking, core.StatusDone, 20)
	if err := old.AppendResyncMarker(ResyncMarker{Host: "mac", Server: "default", Epoch: 1, ObservedAt: 5, Reason: ReasonDaemonStart}); err != nil {
		t.Fatal(err)
	}
	if err := old.SaveServerState("mac", "default", ServerState{Epoch: 1, Terminals: map[string]string{"w1:p1": "t1"}, Released: []string{"w1:p9"}}); err != nil {
		t.Fatal(err)
	}
	return raw
}

var recordTables = []string{"transitions", "resync_markers", "server_state"}

// TestUpgradeFromSchema4: a 0.7.x database with rows is migrated to the
// current schema with every row unchanged, and a binary that knows only
// schema 4 then refuses it with ErrSchemaNewer.
func TestUpgradeFromSchema4(t *testing.T) {
	path := dbPath(t)
	raw := schema4(t, path)
	before := map[string]string{}
	for _, table := range recordTables {
		before[table] = dumpTable(t, raw, table)
		if before[table] == "" {
			t.Fatalf("%s holds no row before the upgrade", table)
		}
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, s.db); v != schemaVersion {
		t.Errorf("user_version %d, want %d", v, schemaVersion)
	}
	for _, table := range recordTables {
		if got := dumpTable(t, s.db, table); got != before[table] {
			t.Errorf("%s changed by the upgrade:\n got %s\nwant %s", table, got, before[table])
		}
	}
	if n := ackCount(t, s.db); n != 0 {
		t.Errorf("%d acknowledgements after the upgrade, want none", n)
	}
	if err := s.AppendAcknowledgements(context.Background(), []core.Acknowledgement{mustAck(t, keyP1, 20, true, 30)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A binary that knows only schema 4: the same prepare, its list truncated.
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	raw, err = sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	err = (&Store{db: raw}).prepare(context.Background(), path, ms[:4])
	if !errors.Is(err, ErrSchemaNewer) || !strings.Contains(err.Error(), fmt.Sprintf("version %d", schemaVersion)) || !strings.Contains(err.Error(), "up to 4") {
		t.Errorf("schema-4 binary on a schema-%d database: %v, want ErrSchemaNewer naming %d and 4", schemaVersion, err, schemaVersion)
	}
}

// TestUpgradeFromSchema4RollsBack: 0005 runs in the same transaction as the
// steps after it, so a later step failing leaves the database at schema 4
// with no acknowledgements table and its rows unchanged.
func TestUpgradeFromSchema4RollsBack(t *testing.T) {
	path := dbPath(t)
	raw := schema4(t, path)
	defer raw.Close()
	before := map[string]string{}
	for _, table := range recordTables {
		before[table] = dumpTable(t, raw, table)
	}
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	bad := append(ms[:5:5], migration{version: 6, name: "0006_bad.sql", sql: "CREATE TABLE nope ("})
	err = (&Store{db: raw}).prepare(context.Background(), path, bad)
	if err == nil || !strings.Contains(err.Error(), "0006_bad.sql") {
		t.Fatalf("error %v, want the failing migration named", err)
	}
	if v := userVersion(t, raw); v != 4 {
		t.Errorf("user_version %d, want 4", v)
	}
	var n int
	if err := raw.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'acknowledgements%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d acknowledgements schema objects survived the rollback", n)
	}
	for _, table := range recordTables {
		if got := dumpTable(t, raw, table); got != before[table] {
			t.Errorf("%s changed by the failed upgrade", table)
		}
	}
}

func TestAcknowledgementChecks(t *testing.T) {
	s, _ := openTemp(t)
	insert := func(epoch, since, seen int) error {
		_, err := s.db.Exec(`INSERT INTO acknowledgements (host, server, epoch, pane_id, finished_since, seen, observed_at)
			VALUES ('h', 's', ?, 'w1:p1', ?, ?, 1)`, epoch, since, seen)
		return err
	}
	if err := insert(1, 1, 1); err != nil {
		t.Errorf("a valid row refused: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO acknowledgements (host, server, epoch, pane_id, finished_since, seen, observed_at, herdr_order)
		VALUES ('h', 's', 1, 'w1:p1', 1, 1, 1, -1)`); err == nil {
		t.Error("a negative herdr_order accepted")
	}
	if err := insert(1, 1, 0); err != nil {
		t.Errorf("a valid undo refused: %v", err)
	}
	for _, src := range []string{"board", "herdr_focus"} {
		if _, err := s.db.Exec(`INSERT INTO acknowledgements (host, server, epoch, pane_id, finished_since, seen, observed_at, source)
			VALUES ('h', 's', 1, 'w1:p1', 1, 1, 1, ?)`, src); err != nil {
			t.Errorf("source %q refused: %v", src, err)
		}
	}
	for _, src := range []any{"other", "", "Board", nil} {
		if _, err := s.db.Exec(`INSERT INTO acknowledgements (host, server, epoch, pane_id, finished_since, seen, observed_at, source)
			VALUES ('h', 's', 1, 'w1:p1', 1, 1, 1, ?)`, src); err == nil {
			t.Errorf("source %v accepted", src)
		}
	}
	for name, r := range map[string][3]int{
		"epoch 0":          {0, 1, 1},
		"finished_since 0": {1, 0, 1},
		"negative since":   {1, -1, 1},
		"seen 2":           {1, 1, 2},
	} {
		if err := insert(r[0], r[1], r[2]); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestAppendAcknowledgements(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.AppendAcknowledgements(ctx, nil); err != nil {
		t.Fatalf("no rows: %v", err)
	}
	if n := ackCount(t, s.db); n != 0 {
		t.Fatalf("%d rows after appending none", n)
	}
	other := core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p2"}
	if err := s.AppendAcknowledgements(ctx, []core.Acknowledgement{
		mustAck(t, keyP1, 100, true, 130),
		mustAck(t, other, 100, true, 110),
		mustAck(t, keyP1, 100, false, 120), // recorded later, observed earlier: a clock stepped back
		mustAck(t, keyP1, 50, true, 130),
	}); err != nil {
		t.Fatal(err)
	}
	var host, server, pane string
	var epoch, since, seen, at int64
	if err := s.db.QueryRow(`SELECT host, server, epoch, pane_id, finished_since, seen, observed_at FROM acknowledgements WHERE id = 1`).
		Scan(&host, &server, &epoch, &pane, &since, &seen, &at); err != nil {
		t.Fatal(err)
	}
	if host != "mac" || server != "default" || epoch != 1 || pane != "w1:p1" || since != 100 || seen != 1 || at != 130 {
		t.Errorf("row 1 is %s %s %d %s %d %d %d", host, server, epoch, pane, since, seen, at)
	}
	got, err := s.Acknowledgements(ctx, keyP1)
	if err != nil {
		t.Fatal(err)
	}
	var desc []string
	for _, a := range got {
		if a.Key() != keyP1 {
			t.Errorf("row of %+v read for %+v", a.Key(), keyP1)
		}
		desc = append(desc, fmt.Sprintf("%d:%v@%d", a.FinishedSince(), a.Seen(), a.ObservedAt()))
	}
	// Recording order is id alone: the undo, recorded after the Seen, stays
	// after it although its clock reads earlier, so it is the last word.
	if want := "100:true@130 100:false@120 50:true@130"; strings.Join(desc, " ") != want {
		t.Errorf("acknowledgements %v, want %s", desc, want)
	}
	if none, err := s.Acknowledgements(ctx, core.AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"}); err != nil || len(none) != 0 {
		t.Errorf("another epoch: %v, %v; want none", none, err)
	}
}

// TestAcknowledgementUndoSurvivesClockStepAcrossRestart records a Seen, then
// its Undo with a wall clock stepped back, in separate batches as the engine
// would, closes the store and reopens it: core.Seen over the rows read back
// must give the operator's last action, the Undo.
func TestAcknowledgementUndoSurvivesClockStepAcrossRestart(t *testing.T) {
	path := dbPath(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.AppendAcknowledgements(ctx, []core.Acknowledgement{mustAck(t, keyP1, 100, true, 5_000)}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAcknowledgements(ctx, []core.Acknowledgement{mustAck(t, keyP1, 100, false, 4_000)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	acks, err := s.Acknowledgements(ctx, keyP1)
	if err != nil {
		t.Fatal(err)
	}
	f := core.FoldResult{Known: true, Status: core.StatusDone, Class: core.ClassFinished, Since: 100}
	if core.Seen(f, acks) {
		t.Errorf("after a restart the undo recorded last, at an earlier clock, reads as seen")
	}
}

// TestAppendAcknowledgementsRollsBack forces the second of three inserts to
// fail, after the first was written: the batch errors and no row of it
// remains.
func TestAppendAcknowledgementsRollsBack(t *testing.T) {
	s, _ := openTemp(t)
	// It aborts the second insert only once the first row is in the table,
	// so the batch failing proves a row was written before the rollback.
	if _, err := s.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON acknowledgements
		WHEN NEW.finished_since = 200 AND (SELECT count(*) FROM acknowledgements WHERE finished_since = 100) = 1
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	err := s.AppendAcknowledgements(context.Background(), []core.Acknowledgement{
		mustAck(t, keyP1, 100, true, 110),
		mustAck(t, keyP1, 200, true, 210),
		mustAck(t, keyP1, 300, true, 310),
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %v, want the trigger's", err)
	}
	if n := ackCount(t, s.db); n != 0 {
		t.Errorf("%d acknowledgements after a failed batch, want none", n)
	}
	// A batch the trigger lets through is written whole.
	if err := s.AppendAcknowledgements(context.Background(), []core.Acknowledgement{mustAck(t, keyP1, 100, true, 110)}); err != nil {
		t.Fatal(err)
	}
	if n := ackCount(t, s.db); n != 1 {
		t.Errorf("%d acknowledgements, want 1", n)
	}
}

// TestAcknowledgementsSurviveReopen derives Seen from the record (History,
// core.Fold, Acknowledgements, core.Seen) before and after closing and
// reopening the store, for each case the story names.
func TestAcknowledgementsSurviveReopen(t *testing.T) {
	path := dbPath(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	k := func(pane string) core.AgentKey {
		return core.AgentKey{Host: "mac", Server: "default", Epoch: 3, PaneID: pane}
	}
	finish := func(key core.AgentKey) {
		liveAt(t, s, key, "", core.StatusWorking, 10)
		liveAt(t, s, key, core.StatusWorking, core.StatusDone, 100)
	}
	acks := func(as ...core.Acknowledgement) {
		if err := s.AppendAcknowledgements(ctx, as); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		key  core.AgentKey
		want bool
	}{
		{"no rows", k("none"), false},
		{"seen", k("seen"), true},
		{"seen then undone", k("undone"), false},
		{"undone then seen again", k("again"), true},
		{"seen, then done to idle", k("idle"), true},
		{"only an older finish", k("older"), false},
		{"a later Agent on the same key", k("later"), false},
		{"another key's rows", k("other"), false},
	}
	finish(k("none"))
	finish(k("seen"))
	acks(mustAck(t, k("seen"), 100, true, 110))
	finish(k("undone"))
	acks(mustAck(t, k("undone"), 100, true, 110))
	acks(mustAck(t, k("undone"), 100, false, 120))
	finish(k("again"))
	acks(mustAck(t, k("again"), 100, true, 110), mustAck(t, k("again"), 100, false, 120))
	acks(mustAck(t, k("again"), 100, true, 130))
	finish(k("idle"))
	acks(mustAck(t, k("idle"), 100, true, 110))
	liveAt(t, s, k("idle"), core.StatusDone, core.StatusIdle, 120)
	finish(k("older"))
	acks(mustAck(t, k("older"), 100, true, 110))
	liveAt(t, s, k("older"), core.StatusDone, core.StatusWorking, 200)
	liveAt(t, s, k("older"), core.StatusWorking, core.StatusDone, 300)
	finish(k("later"))
	acks(mustAck(t, k("later"), 100, true, 110))
	// A new Agent in the same pane: its first row has no previous Status, so
	// its fold starts over and its finish has another Since.
	liveAt(t, s, k("later"), "", core.StatusWorking, 200)
	liveAt(t, s, k("later"), core.StatusWorking, core.StatusDone, 300)
	finish(k("other"))
	acks(mustAck(t, k("someone-else"), 100, true, 110))

	derive := func(s *Store) map[string]bool {
		out := map[string]bool{}
		for _, c := range cases {
			ts, err := s.History(ctx, c.key)
			if err != nil {
				t.Fatal(err)
			}
			as, err := s.Acknowledgements(ctx, c.key)
			if err != nil {
				t.Fatal(err)
			}
			out[c.name] = core.Seen(core.Fold(ts), as)
		}
		return out
	}
	before := derive(s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after := derive(s)
	for _, c := range cases {
		if before[c.name] != c.want || after[c.name] != c.want {
			t.Errorf("%s: seen %v before the reopen and %v after, want %v", c.name, before[c.name], after[c.name], c.want)
		}
	}
}

// TestUpgradeFromSchema5: a database an Epic 6 -dev build left at schema 5,
// with acknowledgements, is migrated to 7 with every row unchanged and its
// acknowledgements read back with no herdr order and source board; a new
// row's order round trips; a binary that knows only schema 5 then refuses the
// database.
func TestUpgradeFromSchema5(t *testing.T) {
	path := dbPath(t)
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	old := &Store{db: raw}
	if err := old.prepare(context.Background(), path, ms[:5]); err != nil {
		t.Fatal(err)
	}
	liveAt(t, old, keyP1, "", core.StatusWorking, 10)
	liveAt(t, old, keyP1, core.StatusWorking, core.StatusDone, 20)
	// Schema 5's own INSERT, which names no herdr_order.
	if _, err := raw.Exec(`INSERT INTO acknowledgements (host, server, epoch, pane_id, finished_since, seen, observed_at)
		VALUES ('mac', 'default', 1, 'w1:p1', 20, 1, 30)`); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, table := range append(slices.Clone(recordTables), "acknowledgements") {
		before[table] = dumpTable(t, raw, table)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, s.db); v != schemaVersion {
		t.Errorf("user_version %d, want %d", v, schemaVersion)
	}
	for _, table := range recordTables {
		if got := dumpTable(t, s.db, table); got != before[table] {
			t.Errorf("%s changed by the upgrade:\n got %s\nwant %s", table, got, before[table])
		}
	}
	if got, want := dumpTable(t, s.db, "acknowledgements"), strings.ReplaceAll(before["acknowledgements"], "\n", " <nil> board\n"); got != want {
		t.Errorf("acknowledgements after the upgrade:\n got %s\nwant %s (each row with a NULL herdr_order and source board)", got, want)
	}
	order := uint64(42)
	a, err := core.NewAcknowledgement(core.AcknowledgementInput{Source: core.AckSourceBoard, Key: keyP1, FinishedSince: 20, Seen: true, ObservedAt: 40, HerdrOrder: &order})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAcknowledgements(context.Background(), []core.Acknowledgement{a}); err != nil {
		t.Fatal(err)
	}
	acks, err := s.Acknowledgements(context.Background(), keyP1)
	if err != nil {
		t.Fatal(err)
	}
	if len(acks) != 2 {
		t.Fatalf("%d acknowledgements, want 2", len(acks))
	}
	if _, ok := acks[0].HerdrOrder(); ok {
		t.Error("the schema-5 row reads back with a herdr order")
	}
	if src := acks[0].Source(); src != core.AckSourceBoard {
		t.Errorf("the schema-5 row reads back with source %q, want board", src)
	}
	if got, ok := acks[1].HerdrOrder(); !ok || got != 42 {
		t.Errorf("the new row's herdr order %d %v, want 42", got, ok)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err = sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	err = (&Store{db: raw}).prepare(context.Background(), path, ms[:5])
	if !errors.Is(err, ErrSchemaNewer) || !strings.Contains(err.Error(), fmt.Sprintf("version %d", schemaVersion)) || !strings.Contains(err.Error(), "up to 5") {
		t.Errorf("schema-5 binary on a schema-%d database: %v, want ErrSchemaNewer naming %d and 5", schemaVersion, err, schemaVersion)
	}
}

// rawAt opens a database at path migrated only to the first n migrations, as
// an older binary leaves it, and returns its handle.
func rawAt(t *testing.T, path string, n int) *sql.DB {
	t.Helper()
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	if err := (&Store{db: raw}).prepare(context.Background(), path, ms[:n]); err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, raw); v != n {
		t.Fatalf("old database at %d, want %d", v, n)
	}
	return raw
}

// TestUpgradeFromSchema6: the operator's database, which the installed -dev
// build (0.8.0-dev) left at schema 6 with acknowledgements, marks and undos,
// with and without herdr's order, is migrated to 7 with every row of every
// table unchanged but for the new source column, which reads board for each
// earlier row; a binary that knows only schema 6 then refuses the database.
func TestUpgradeFromSchema6(t *testing.T) {
	path := dbPath(t)
	raw := rawAt(t, path, 6)
	old := &Store{db: raw}
	liveAt(t, old, keyP1, "", core.StatusWorking, 10)
	liveAt(t, old, keyP1, core.StatusWorking, core.StatusDone, 20)
	if err := old.SaveServerState("mac", "default", ServerState{Epoch: 1, Terminals: map[string]string{"w1:p1": "t1"}}); err != nil {
		t.Fatal(err)
	}
	// Schema 6's own INSERT, which names no source.
	if _, err := raw.Exec(`INSERT INTO acknowledgements (host, server, epoch, pane_id, finished_since, seen, observed_at, herdr_order)
		VALUES ('mac', 'default', 1, 'w1:p1', 20, 1, 30, 7), ('mac', 'default', 1, 'w1:p1', 20, 0, 31, NULL),
		('mac', 'default', 1, 'w1:p1', 20, 1, 32, 7)`); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, table := range append(slices.Clone(recordTables), "acknowledgements") {
		before[table] = dumpTable(t, raw, table)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, s.db); v != 7 {
		t.Errorf("user_version %d, want 7", v)
	}
	for _, table := range recordTables {
		if got := dumpTable(t, s.db, table); got != before[table] {
			t.Errorf("%s changed by the upgrade:\n got %s\nwant %s", table, got, before[table])
		}
	}
	if got, want := dumpTable(t, s.db, "acknowledgements"), strings.ReplaceAll(before["acknowledgements"], "\n", " board\n"); got != want {
		t.Errorf("acknowledgements after the upgrade:\n got %s\nwant %s (each row with source board)", got, want)
	}
	acks, err := s.Acknowledgements(context.Background(), keyP1)
	if err != nil {
		t.Fatal(err)
	}
	var desc []string
	for _, a := range acks {
		o, _ := a.HerdrOrder()
		desc = append(desc, fmt.Sprintf("%d:%v@%d#%d/%s", a.FinishedSince(), a.Seen(), a.ObservedAt(), o, a.Source()))
	}
	if got, want := strings.Join(desc, " "), "20:true@30#7/board 20:false@31#0/board 20:true@32#7/board"; got != want {
		t.Errorf("acknowledgements read back %s, want %s", got, want)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err = sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	err = (&Store{db: raw}).prepare(context.Background(), path, ms[:6])
	if !errors.Is(err, ErrSchemaNewer) || !strings.Contains(err.Error(), "version 7") || !strings.Contains(err.Error(), "up to 6") {
		t.Errorf("schema-6 binary on a schema-7 database: %v, want ErrSchemaNewer naming 7 and 6", err)
	}
}

// TestUpgradeFromSchema6RollsBack: 0007 runs in the same transaction as any
// migration after it, so a failing one leaves the database at schema 6 with
// no source column and its rows unchanged.
func TestUpgradeFromSchema6RollsBack(t *testing.T) {
	path := dbPath(t)
	raw := rawAt(t, path, 6)
	if _, err := raw.Exec(`INSERT INTO acknowledgements (host, server, epoch, pane_id, finished_since, seen, observed_at)
		VALUES ('mac', 'default', 1, 'w1:p1', 20, 1, 30)`); err != nil {
		t.Fatal(err)
	}
	before := dumpTable(t, raw, "acknowledgements")
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	bad := append(ms[:7:7], migration{version: 8, name: "0008_bad.sql", sql: "CREATE TABLE nope ("})
	if err := (&Store{db: raw}).prepare(context.Background(), path, bad); err == nil {
		t.Fatal("a failing migration after 0007 was accepted")
	}
	if v := userVersion(t, raw); v != 6 {
		t.Errorf("user_version %d after the failed upgrade, want 6", v)
	}
	for _, c := range tableInfo(t, raw, "acknowledgements") {
		if c.name == "source" {
			t.Error("the source column survived the rolled-back upgrade")
		}
	}
	if got := dumpTable(t, raw, "acknowledgements"); got != before {
		t.Errorf("acknowledgements changed by the failed upgrade:\n got %s\nwant %s", got, before)
	}
	raw.Close()
}

func mustAckFrom(t *testing.T, src core.AckSource, since int64, at int64) core.Acknowledgement {
	t.Helper()
	a, err := core.NewAcknowledgement(core.AcknowledgementInput{Key: keyP1, FinishedSince: since, Seen: true, ObservedAt: at, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestAcknowledgementSources: a batch mixing board and herdr_focus rows is
// written in one transaction, so an abort forced on its second insert, after
// the first row was written, leaves none of it; without the abort every row
// reads back with its source, across a close and reopen.
func TestAcknowledgementSources(t *testing.T) {
	path := dbPath(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	batch := []core.Acknowledgement{
		mustAckFrom(t, core.AckSourceBoard, 100, 110),
		mustAckFrom(t, core.AckSourceHerdrFocus, 200, 210),
		mustAckFrom(t, core.AckSourceBoard, 300, 310),
	}
	if _, err := s.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON acknowledgements
		WHEN NEW.source = 'herdr_focus' AND (SELECT count(*) FROM acknowledgements WHERE source = 'board') = 1
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAcknowledgements(ctx, batch); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %v, want the trigger's", err)
	}
	if n := ackCount(t, s.db); n != 0 {
		t.Errorf("%d acknowledgements after a failed batch, want none", n)
	}
	if _, err := s.db.Exec(`DROP TRIGGER boom`); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAcknowledgements(ctx, batch); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		t.Helper()
		acks, err := s.Acknowledgements(ctx, keyP1)
		if err != nil {
			t.Fatal(err)
		}
		var desc []string
		for _, a := range acks {
			desc = append(desc, fmt.Sprintf("%d/%s", a.FinishedSince(), a.Source()))
		}
		return strings.Join(desc, " ")
	}
	const want = "100/board 200/herdr_focus 300/board"
	if got := read(); got != want {
		t.Errorf("before the reopen %s, want %s", got, want)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if got := read(); got != want {
		t.Errorf("after the reopen %s, want %s", got, want)
	}
}
