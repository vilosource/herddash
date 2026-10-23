package store

import (
	"context"
	"database/sql"
	"maps"
	"net/url"
	"testing"

	"github.com/vilosource/herddash/internal/core"
)

func status(s core.Status) *core.Status { return &s }

func order(v uint64) *uint64 { return &v }

var keyP1 = core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"}

func mustTransition(t *testing.T, in core.TransitionInput) core.Transition {
	t.Helper()
	tr, err := core.NewTransition(in)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// storedRow is one transitions row, every column; rowOf reads back the row
// with the given id.
type storedRow struct {
	host, server, paneID, newStatus, provenance string
	epoch, observedAt, spurious                 int64
	program, profile, previous, version, ikey   sql.NullString
	seq                                         sql.NullInt64
}

func rowOf(t *testing.T, s *Store, id int64) storedRow {
	t.Helper()
	var r storedRow
	err := s.db.QueryRow(`SELECT host, server, epoch, pane_id, agent_program, profile, previous_status, new_status,
		state_change_seq, herdr_version, provenance, possibly_spurious, observed_at, import_key FROM transitions WHERE id = ?`, id).
		Scan(&r.host, &r.server, &r.epoch, &r.paneID, &r.program, &r.profile, &r.previous, &r.newStatus,
			&r.seq, &r.version, &r.provenance, &r.spurious, &r.observedAt, &r.ikey)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAppendLive(t *testing.T) {
	s, _ := openTemp(t)
	tr := mustTransition(t, core.TransitionInput{Key: keyP1, AgentProgram: "claude", Profile: "cldw",
		Previous: status(core.StatusWorking), New: core.StatusBlocked, HerdrVersion: "0.9.5",
		ObservedAt: 1000, Provenance: core.ProvenanceLive})
	if err := s.Append(tr); err != nil {
		t.Fatal(err)
	}
	r := rowOf(t, s, 1)
	if r.host != "mac" || r.server != "default" || r.epoch != 1 || r.paneID != "w1:p1" ||
		r.program.String != "claude" || r.profile.String != "cldw" ||
		r.previous.String != "working" || r.newStatus != "blocked" || r.seq.Valid ||
		r.version.String != "0.9.5" || r.provenance != "live" || r.spurious != 0 || r.observedAt != 1000 || r.ikey.Valid {
		t.Errorf("row %+v", r)
	}
}

func TestAppendSnapshotOrder(t *testing.T) {
	s, _ := openTemp(t)
	tr := mustTransition(t, core.TransitionInput{Key: keyP1, AgentProgram: "claude", Profile: "claude",
		Previous: status(core.StatusWorking), New: core.StatusIdle, HerdrOrder: order(7), HerdrVersion: "0.9.5",
		ObservedAt: 1000, Provenance: core.ProvenanceSnapshot})
	if err := s.Append(tr); err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, s, 1); !r.seq.Valid || r.seq.Int64 != 7 || r.provenance != "snapshot" {
		t.Errorf("row %+v, want snapshot with state_change_seq 7", r)
	}
}

func TestAppendFirstSightAndNulls(t *testing.T) {
	s, _ := openTemp(t)
	// First sight: no previous Status; nothing else known either.
	tr := mustTransition(t, core.TransitionInput{Key: keyP1, New: core.StatusWorking,
		ObservedAt: 1000, Provenance: core.ProvenanceSnapshot})
	if err := s.Append(tr); err != nil {
		t.Fatal(err)
	}
	r := rowOf(t, s, 1)
	if r.previous.Valid || r.program.Valid || r.profile.Valid || r.version.Valid || r.seq.Valid || r.ikey.Valid {
		t.Errorf("unknown values not NULL: %+v", r)
	}
	if r.newStatus != "working" || r.provenance != "snapshot" {
		t.Errorf("row %+v", r)
	}
}

func TestAppendBackfill(t *testing.T) {
	s, _ := openTemp(t)
	key := keyP1
	key.Epoch = core.BackfillEpoch
	tr := mustTransition(t, core.TransitionInput{Key: key, AgentProgram: "claude", Profile: "cldp",
		New: core.StatusDone, ObservedAt: 1000, Provenance: core.ProvenanceBackfill, ImportKey: "log:1"})
	if err := s.Append(tr); err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, s, 1); r.epoch != 0 || r.ikey.String != "log:1" || r.provenance != "backfill" {
		t.Errorf("row %+v", r)
	}
	// The unique import key: a second import of the same entry is refused.
	if err := s.Append(tr); err == nil {
		t.Error("a repeated import key was appended")
	}
}

// backfillAt is a backfill Transition for pane at ms with import key ikey.
func backfillAt(t *testing.T, pane string, at int64, ikey string, spurious bool) core.Transition {
	t.Helper()
	return mustTransition(t, core.TransitionInput{
		Key:          core.AgentKey{Host: "mac", Server: "default", Epoch: core.BackfillEpoch, PaneID: pane},
		AgentProgram: "claude", Profile: "work", New: core.StatusDone, ObservedAt: at,
		Provenance: core.ProvenanceBackfill, ImportKey: ikey, PossiblySpurious: spurious})
}

func TestAppendWritesPossiblySpurious(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.Append(backfillAt(t, "w1:p1", 1000, "k1", true)); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(backfillAt(t, "w1:p1", 2000, "k2", false)); err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, s, 1); r.spurious != 1 {
		t.Errorf("flagged row has possibly_spurious %d, want 1", r.spurious)
	}
	if r := rowOf(t, s, 2); r.spurious != 0 {
		t.Errorf("unflagged row has possibly_spurious %d, want 0", r.spurious)
	}
	got, err := s.History(context.Background(), core.AgentKey{Host: "mac", Server: "default", Epoch: 0, PaneID: "w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].PossiblySpurious() || got[1].PossiblySpurious() {
		t.Errorf("History does not read the flag back: %+v", got)
	}
}

func count(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM transitions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAppendBackfillIdempotent(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	first := []core.Transition{backfillAt(t, "w1:p1", 1000, "a", true), backfillAt(t, "w1:p2", 2000, "b", false)}
	if added, flagged, err := s.AppendBackfill(ctx, first); err != nil || added != 2 || flagged != 1 {
		t.Fatalf("first import added %d (%d flagged), %v; want 2 (1 flagged)", added, flagged, err)
	}
	if added, flagged, err := s.AppendBackfill(ctx, first); err != nil || added != 0 || flagged != 0 {
		t.Fatalf("re-import added %d (%d flagged), %v; want 0", added, flagged, err)
	}
	grown := append(first, backfillAt(t, "w1:p1", 3000, "c", false))
	if added, flagged, err := s.AppendBackfill(ctx, grown); err != nil || added != 1 || flagged != 0 {
		t.Fatalf("grown import added %d (%d flagged), %v; want 1 (0 flagged)", added, flagged, err)
	}
	if n := count(t, s); n != 3 {
		t.Errorf("%d rows, want 3", n)
	}
	if r := rowOf(t, s, 1); r.epoch != 0 || r.provenance != "backfill" || r.previous.Valid || r.seq.Valid ||
		r.version.Valid || r.spurious != 1 || r.ikey.String != "a" {
		t.Errorf("row %+v", r)
	}
}

func TestAppendBackfillRefusesOtherRows(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	key := core.AgentKey{Host: "mac", Server: "default", Epoch: core.BackfillEpoch, PaneID: "w1:p1"}
	for name, bad := range map[string]core.Transition{
		"live": mustTransition(t, core.TransitionInput{Key: keyP1, New: core.StatusDone, ObservedAt: 1000,
			Provenance: core.ProvenanceLive}),
		"previous": mustTransition(t, core.TransitionInput{Key: key, Previous: status(core.StatusWorking), New: core.StatusDone,
			ObservedAt: 1000, Provenance: core.ProvenanceBackfill, ImportKey: "p"}),
		"version": mustTransition(t, core.TransitionInput{Key: key, HerdrVersion: "0.9.2", New: core.StatusDone,
			ObservedAt: 1000, Provenance: core.ProvenanceBackfill, ImportKey: "v"}),
	} {
		ts := []core.Transition{backfillAt(t, "w1:p1", 1000, "ok", false), bad}
		if added, _, err := s.AppendBackfill(ctx, ts); err == nil || added != 0 {
			t.Errorf("%s: added %d, err %v; want an error", name, added, err)
		}
	}
	if n := count(t, s); n != 0 {
		t.Errorf("%d rows written, want none", n)
	}
}

func TestAppendBackfillRollsBack(t *testing.T) {
	s, _ := openTemp(t)
	// A test-only trigger fails the third insert, after two have succeeded
	// inside the transaction: the rollback leaves no row at all.
	if _, err := s.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON transitions WHEN NEW.pane_id = 'boom'
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	ts := []core.Transition{backfillAt(t, "w1:p1", 1000, "a", false), backfillAt(t, "w1:p2", 2000, "b", true),
		backfillAt(t, "boom", 3000, "c", false)}
	if added, flagged, err := s.AppendBackfill(context.Background(), ts); err == nil || added != 0 || flagged != 0 {
		t.Errorf("an import failing part-way returned %d (%d flagged), %v; want an error", added, flagged, err)
	}
	if n := count(t, s); n != 0 {
		t.Errorf("%d rows written, want none", n)
	}
}

func TestAppendResyncMarker(t *testing.T) {
	s, _ := openTemp(t)
	for _, m := range []ResyncMarker{
		{Host: "mac", Server: "default", Epoch: 3, ObservedAt: 500, Reason: ReasonDaemonStart},
		{Host: "mac", Server: "default", Epoch: 3, ObservedAt: 600, Reason: "error", Detail: "code x"},
	} {
		if err := s.AppendResyncMarker(m); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.db.Query(`SELECT host, server, epoch, observed_at, reason, detail FROM resync_markers ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []ResyncMarker
	for rows.Next() {
		var m ResyncMarker
		var detail sql.NullString
		if err := rows.Scan(&m.Host, &m.Server, &m.Epoch, &m.ObservedAt, &m.Reason, &detail); err != nil {
			t.Fatal(err)
		}
		if m.Reason == ReasonDaemonStart && detail.Valid {
			t.Error("an empty detail was stored, not NULL")
		}
		m.Detail = detail.String
		got = append(got, m)
	}
	want := []ResyncMarker{
		{Host: "mac", Server: "default", Epoch: 3, ObservedAt: 500, Reason: "daemon_start"},
		{Host: "mac", Server: "default", Epoch: 3, ObservedAt: 600, Reason: "error", Detail: "code x"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("markers %+v, want %+v", got, want)
	}
}

func TestHistoryOrderAndRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	other := keyP1
	other.PaneID = "w1:p2"
	otherEpoch := keyP1
	otherEpoch.Epoch = 2
	in := []core.TransitionInput{
		// observed_at 20, then 10, then 10 again: History gives 10 (lower
		// id), 10, 20.
		{Key: keyP1, AgentProgram: "claude", Profile: "cldw", New: core.StatusWorking, HerdrOrder: order(4),
			HerdrVersion: "0.9.5", ObservedAt: 20, Provenance: core.ProvenanceSnapshot},
		{Key: keyP1, AgentProgram: "claude", Profile: "cldw", Previous: status(core.StatusIdle), New: core.StatusBlocked,
			HerdrVersion: "0.9.5", ObservedAt: 10, Provenance: core.ProvenanceLive},
		{Key: other, New: core.StatusIdle, ObservedAt: 15, Provenance: core.ProvenanceLive},
		{Key: otherEpoch, New: core.StatusIdle, ObservedAt: 15, Provenance: core.ProvenanceLive},
		{Key: keyP1, Previous: status(core.StatusBlocked), New: core.StatusDone, ObservedAt: 10, Provenance: core.ProvenanceLive,
			ObservedBy: core.ObservedByClaudeSession},
	}
	var trs []core.Transition
	for _, i := range in {
		tr := mustTransition(t, i)
		trs = append(trs, tr)
		if err := s.Append(tr); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.History(context.Background(), keyP1)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.Transition{trs[1], trs[4], trs[0]}
	if len(got) != len(want) {
		t.Fatalf("%d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if !sameTransition(got[i], want[i]) {
			t.Errorf("row %d is %+v, want %+v", i, describe(got[i]), describe(want[i]))
		}
	}
	if h, err := s.History(context.Background(), core.AgentKey{Host: "mac", Server: "default", Epoch: 9, PaneID: "w1:p1"}); err != nil || len(h) != 0 {
		t.Errorf("history of an unknown Agent %v, %v; want none", h, err)
	}
}

type described struct {
	Key                                      core.AgentKey
	Program, Profile, Previous, New, Version string
	HasPrevious, HasOrder                    bool
	Order                                    uint64
	ObservedAt                               int64
	Provenance                               core.Provenance
	ImportKey                                string
	Spurious                                 bool
	ObservedBy                               core.Observer
}

func describe(t core.Transition) described {
	prev, hasPrev := t.Previous()
	o, hasOrder := t.HerdrOrder()
	return described{t.Key(), string(t.AgentProgram()), t.Profile(), string(prev), string(t.New()), t.HerdrVersion(),
		hasPrev, hasOrder, o, t.ObservedAt(), t.Provenance(), t.ImportKey(), t.PossiblySpurious(), t.ObservedBy()}
}

func sameTransition(a, b core.Transition) bool { return describe(a) == describe(b) }

func TestServerState(t *testing.T) {
	path := dbPath(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.LoadServerState("mac", "default"); err != nil || ok {
		t.Fatalf("fresh database: found %v, err %v; want nothing", ok, err)
	}
	st := ServerState{Epoch: 3, Terminals: map[string]string{"w1:p1": "t1", "w1:p3": "t3"}}
	if err := s.SaveServerState("mac", "default", st); err != nil {
		t.Fatal(err)
	}
	// Another Server on the same Host is kept apart.
	if err := s.SaveServerState("mac", "work", ServerState{Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	// Upserted: the latest save wins.
	st = ServerState{Epoch: 4, Terminals: map[string]string{"w1:p1": "t9"}}
	if err := s.SaveServerState("mac", "default", st); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.LoadServerState("mac", "default")
	if err != nil || !ok || got.Epoch != 4 || !maps.Equal(got.Terminals, st.Terminals) {
		t.Errorf("loaded %+v, %v, %v; want %+v", got, ok, err, st)
	}
	got, ok, err = s.LoadServerState("mac", "work")
	if err != nil || !ok || got.Epoch != 1 || got.Terminals == nil || len(got.Terminals) != 0 {
		t.Errorf("loaded %+v, %v, %v; want epoch 1 with no terminals", got, ok, err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM server_state").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("%d server_state rows, want 2", n)
	}
	if err := s.SaveServerState("mac", "default", ServerState{Epoch: 0}); err == nil {
		t.Error("epoch 0 saved as server state")
	}
}

// TestUpgradeFromSchema1 opens a database Story 2.1's binary left, at schema
// version 1 with a marker in it: migration 0002 is applied and the record is
// untouched.
func TestUpgradeFromSchema1(t *testing.T) {
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
	if err := old.prepare(context.Background(), path, ms[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO resync_markers (host, server, epoch, observed_at, reason) VALUES ('h', 's', 1, 1, 'daemon_start')`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v := userVersion(t, s.db); v != schemaVersion {
		t.Errorf("user_version %d, want %d", v, schemaVersion)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM resync_markers").Scan(&n); err != nil || n != 1 {
		t.Errorf("%d markers (%v), want the 1 written before", n, err)
	}
	if err := s.SaveServerState("h", "s", ServerState{Epoch: 1}); err != nil {
		t.Errorf("server_state after the upgrade: %v", err)
	}
}

// TestAppendObservedBy: a Transition the session record observed stores
// observed_by 'claude_session', one herdr observed NULL, and History returns
// both as they were.
func TestAppendObservedBy(t *testing.T) {
	s, _ := openTemp(t)
	for i, by := range []core.Observer{core.ObservedByHerdr, core.ObservedByClaudeSession} {
		tr := mustTransition(t, core.TransitionInput{Key: keyP1, AgentProgram: "claude", New: core.StatusBlocked,
			ObservedAt: int64(10 + i), Provenance: core.ProvenanceLive, ObservedBy: by})
		if err := s.Append(tr); err != nil {
			t.Fatal(err)
		}
	}
	var by []sql.NullString
	rows, err := s.db.Query("SELECT observed_by FROM transitions ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		by = append(by, v)
	}
	rows.Close()
	if len(by) != 2 || by[0].Valid || by[1].String != "claude_session" {
		t.Errorf("observed_by %v, want NULL then claude_session", by)
	}
	h, err := s.History(context.Background(), keyP1)
	if err != nil || len(h) != 2 || h[0].ObservedBy() != core.ObservedByHerdr || h[1].ObservedBy() != core.ObservedByClaudeSession {
		t.Fatalf("history %v, %v", h, err)
	}
	if _, err := s.db.Exec(`INSERT INTO transitions (host, server, epoch, pane_id, new_status, provenance, observed_at, observed_by)
		VALUES ('h', 's', 1, 'w1:p1', 'blocked', 'live', 1, 'guess')`); err == nil {
		t.Error("observed_by 'guess' accepted")
	}
}

// TestUpgradeFromSchema2 covers the matrix's "Migration" row: a database at
// schema version 2 holding Transitions is migrated to 3 by ADD COLUMN, its
// rows untouched and read as observed by herdr, the table not rebuilt.
func TestUpgradeFromSchema2(t *testing.T) {
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
	if err := old.prepare(context.Background(), path, ms[:2]); err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, raw); v != 2 {
		t.Fatalf("old database at %d, want 2", v)
	}
	if _, err := raw.Exec(`INSERT INTO transitions (host, server, epoch, pane_id, agent_program, new_status, provenance, observed_at)
		VALUES ('mac', 'default', 1, 'w1:p1', 'claude', 'working', 'live', 10),
		       ('mac', 'default', 1, 'w1:p1', 'claude', 'done', 'snapshot', 20)`); err != nil {
		t.Fatal(err)
	}
	var rootBefore int
	if err := raw.QueryRow("SELECT rootpage FROM sqlite_schema WHERE name = 'transitions'").Scan(&rootBefore); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v := userVersion(t, s.db); v != schemaVersion || schemaVersion != 3 {
		t.Errorf("user_version %d, want 3", v)
	}
	var rootAfter, nulls int
	if err := s.db.QueryRow("SELECT rootpage FROM sqlite_schema WHERE name = 'transitions'").Scan(&rootAfter); err != nil {
		t.Fatal(err)
	}
	if rootAfter != rootBefore {
		t.Errorf("transitions rebuilt: root page %d, was %d", rootAfter, rootBefore)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM transitions WHERE observed_by IS NULL").Scan(&nulls); err != nil || nulls != 2 {
		t.Errorf("%d rows with observed_by NULL (%v), want 2", nulls, err)
	}
	h, err := s.History(context.Background(), keyP1)
	if err != nil || len(h) != 2 || h[0].ObservedBy() != core.ObservedByHerdr || h[1].New() != core.StatusDone {
		t.Fatalf("history %v, %v", h, err)
	}
	tr := mustTransition(t, core.TransitionInput{Key: keyP1, AgentProgram: "claude", Previous: status(core.StatusDone),
		New: core.StatusBlocked, ObservedAt: 30, Provenance: core.ProvenanceLive, ObservedBy: core.ObservedByClaudeSession})
	if err := s.Append(tr); err != nil {
		t.Errorf("append after the upgrade: %v", err)
	}
}
