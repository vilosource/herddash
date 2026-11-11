package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/vilosource/herddash/internal/core"
)

// usageRow is one usage row of key's Agent instance since, Profile profile,
// for Response id (its message id; request id "r"+id) at ms, with counts
// derived from n so sums are easy to check.
func usageRow(key core.AgentKey, since int64, profile, id string, at, n int64) UsageRow {
	return UsageRow{Key: key, AgentSince: since, Profile: profile, SessionID: "sess", ObservedAt: at + 1,
		Response: core.Response{MessageID: id, RequestID: "r" + id, Model: "claude-opus", Stopped: true, At: at,
			Usage: core.Usage{Input: n, Output: 2 * n, CacheWrite5m: 3 * n, CacheWrite1h: 4 * n, CacheRead: 5 * n}}}
}

func appendUsage(t *testing.T, s *Store, rows ...UsageRow) int64 {
	t.Helper()
	n, err := s.AppendUsage(context.Background(), rows)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func usageCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM usage`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// usageOwners is each row's message id and the pane and agent_since that
// recorded it, in id order.
func usageOwners(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`SELECT message_id, pane_id, agent_since, profile FROM usage ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, pane, profile string
		var since int64
		if err := rows.Scan(&id, &pane, &since, &profile); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s:%s@%d/%s", id, pane, since, profile))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, " ")
}

func TestAppendUsageEmpty(t *testing.T) {
	s, _ := openTemp(t)
	if n, err := s.AppendUsage(context.Background(), nil); err != nil || n != 0 {
		t.Errorf("AppendUsage(nil) = %d, %v; want 0, nil", n, err)
	}
	if n := usageCount(t, s.db); n != 0 {
		t.Errorf("%d rows, want none", n)
	}
}

// TestAppendUsageDuplicateBatch: the same batch twice is one row per
// Response; the second append inserts nothing.
func TestAppendUsageDuplicateBatch(t *testing.T) {
	s, _ := openTemp(t)
	batch := []UsageRow{usageRow(keyP1, 10, "work", "A", 100, 1), usageRow(keyP1, 10, "work", "B", 200, 2)}
	if n := appendUsage(t, s, batch...); n != 2 {
		t.Errorf("first append inserted %d, want 2", n)
	}
	before := dumpTable(t, s.db, "usage")
	if n := appendUsage(t, s, batch...); n != 0 {
		t.Errorf("second append inserted %d, want 0", n)
	}
	if got := dumpTable(t, s.db, "usage"); got != before {
		t.Errorf("usage changed by a duplicate batch:\n got %s\nwant %s", got, before)
	}
}

// TestAppendUsageOverlap: only the new keys of an overlapping batch are
// inserted; the rows already written are unchanged.
func TestAppendUsageOverlap(t *testing.T) {
	s, _ := openTemp(t)
	appendUsage(t, s, usageRow(keyP1, 10, "work", "A", 100, 1), usageRow(keyP1, 10, "work", "B", 200, 2))
	before := dumpTable(t, s.db, "usage")
	changedB := usageRow(keyP1, 10, "work", "B", 250, 9)
	if n := appendUsage(t, s, changedB, usageRow(keyP1, 10, "work", "C", 300, 3)); n != 1 {
		t.Errorf("overlapping append inserted %d, want 1", n)
	}
	if got := dumpTable(t, s.db, "usage"); !strings.HasPrefix(got, before) {
		t.Errorf("existing rows changed:\n got %s\nwant prefix %s", got, before)
	}
	if got, want := usageOwners(t, s.db), "A:w1:p1@10/work B:w1:p1@10/work C:w1:p1@10/work"; got != want {
		t.Errorf("rows %s, want %s", got, want)
	}
}

// TestAppendUsageFirstAgentKept: a Response already written under Agent A and
// appended again under Agent B (another pane, another Profile) stays A's.
func TestAppendUsageFirstAgentKept(t *testing.T) {
	s, _ := openTemp(t)
	other := core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w2:p9"}
	appendUsage(t, s, usageRow(keyP1, 10, "work", "A", 100, 1))
	if n := appendUsage(t, s, usageRow(other, 50, "personal", "A", 100, 1)); n != 0 {
		t.Errorf("inserted %d, want 0", n)
	}
	if got, want := usageOwners(t, s.db), "A:w1:p1@10/work"; got != want {
		t.Errorf("rows %s, want %s", got, want)
	}
	// The same message id under another request id is another Response.
	r := usageRow(other, 50, "personal", "A", 100, 1)
	r.RequestID = "rOther"
	if n := appendUsage(t, s, r); n != 1 {
		t.Errorf("another request id inserted %d, want 1", n)
	}
}

// TestAppendUsageRollsBack: a trigger aborts the third insert only once the
// first two rows are in the table, so the failure proves rows were written
// before the rollback; none of the batch is kept.
func TestAppendUsageRollsBack(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON usage
		WHEN NEW.message_id = 'C' AND (SELECT count(*) FROM usage WHERE message_id IN ('A', 'B')) = 2
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.AppendUsage(context.Background(), []UsageRow{
		usageRow(keyP1, 10, "work", "A", 100, 1),
		usageRow(keyP1, 10, "work", "B", 200, 2),
		usageRow(keyP1, 10, "work", "C", 300, 3),
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %v, want the trigger's", err)
	}
	if n := usageCount(t, s.db); n != 0 {
		t.Errorf("%d usage rows after a failed batch, want none", n)
	}
	if n := appendUsage(t, s, usageRow(keyP1, 10, "work", "A", 100, 1)); n != 1 {
		t.Errorf("a batch the trigger lets through inserted %d, want 1", n)
	}
}

// TestUsageChecks: the database refuses what the table's CHECKs forbid.
func TestUsageChecks(t *testing.T) {
	s, _ := openTemp(t)
	const ok = `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`
	if _, err := s.db.Exec(`INSERT INTO usage (host, server, epoch, pane_id, agent_since, profile, session_id, subagent_id,
		message_id, request_id, model, responded_at, input, output, cache_write_5m, cache_write_1h, cache_read, stopped,
		observed_at) VALUES (` + ok + `)`); err != nil {
		t.Fatalf("a valid row was refused: %v", err)
	}
	for _, tc := range []struct{ name, values string }{
		{"epoch 0", `'mac', 'default', 0, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`},
		{"agent_since 0", `'mac', 'default', 1, 'w1:p1', 0, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`},
		{"negative responded_at", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', -1, 1, 2, 3, 4, 5, 1, 101`},
		{"negative input", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, -1, 2, 3, 4, 5, 1, 101`},
		{"negative output", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, -2, 3, 4, 5, 1, 101`},
		{"negative 5m", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, -3, 4, 5, 1, 101`},
		{"negative 1h", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, -4, 5, 1, 101`},
		{"negative read", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, 4, -5, 1, 101`},
		{"stopped 2", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 2, 101`},
		{"empty message id", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', '', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`},
		{"empty request id", `'mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', '', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`},
		{"empty session id", `'mac', 'default', 1, 'w1:p1', 10, 'work', '', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`},
		{"empty pane", `'mac', 'default', 1, '', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`},
		{"empty host", `'', 'default', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`},
		{"empty server", `'mac', '', 1, 'w1:p1', 10, 'work', 'sess', '', 'm2', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101`},
	} {
		if _, err := s.db.Exec(`INSERT INTO usage (host, server, epoch, pane_id, agent_since, profile, session_id, subagent_id,
			message_id, request_id, model, responded_at, input, output, cache_write_5m, cache_write_1h, cache_read, stopped,
			observed_at) VALUES (` + tc.values + `)`); err == nil || !strings.Contains(err.Error(), "CHECK") {
			t.Errorf("%s: error %v, want a CHECK failure", tc.name, err)
		}
	}
	// subagent_id defaults to ''.
	if _, err := s.db.Exec(`INSERT INTO usage (host, server, epoch, pane_id, agent_since, profile, session_id,
		message_id, request_id, model, responded_at, input, output, cache_write_5m, cache_write_1h, cache_read, stopped,
		observed_at) VALUES ('mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', 'm3', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101)`); err != nil {
		t.Fatal(err)
	}
	var sub string
	if err := s.db.QueryRow(`SELECT subagent_id FROM usage WHERE message_id = 'm3'`).Scan(&sub); err != nil || sub != "" {
		t.Errorf("subagent_id %q, %v; want ''", sub, err)
	}
	// The unique key refuses a plain INSERT of a Response already held.
	if _, err := s.db.Exec(`INSERT INTO usage (host, server, epoch, pane_id, agent_since, profile, session_id,
		message_id, request_id, model, responded_at, input, output, cache_write_5m, cache_write_1h, cache_read, stopped,
		observed_at) VALUES ('mac', 'default', 1, 'w1:p1', 10, 'work', 'sess', 'm3', 'r', 'claude-opus', 100, 1, 2, 3, 4, 5, 1, 101)`); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("duplicate key: error %v, want a UNIQUE failure", err)
	}
}

// TestUsageIndexes: the three indexes ADR-0017 names exist.
func TestUsageIndexes(t *testing.T) {
	s, _ := openTemp(t)
	rows, err := s.db.Query(`SELECT name FROM sqlite_schema WHERE type = 'index' AND tbl_name = 'usage' AND sql IS NOT NULL ORDER BY name`)
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
	if got, want := strings.Join(names, ","), "usage_agent,usage_profile,usage_responded_at"; got != want {
		t.Errorf("indexes %s, want %s", got, want)
	}
}

// TestUsageTotals sums one Agent instance: another instance on the same key,
// and another key, are not counted; none is the zero value. The sums survive
// closing and reopening the store.
func TestUsageTotals(t *testing.T) {
	path := dbPath(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	other := core.AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"}
	appendUsage(t, s,
		usageRow(keyP1, 10, "work", "A", 100, 1),
		usageRow(keyP1, 10, "work", "B", 300, 2),
		usageRow(keyP1, 10, "work", "C", 200, 4),
		usageRow(keyP1, 20, "work", "D", 400, 8), // a later Agent on the key
		usageRow(other, 10, "work", "E", 500, 16),
	)
	want := UsageTotals{Input: 7, Output: 14, CacheWrite: 49, CacheRead: 35, Responses: 3, LastResponseAt: 300}
	check := func(s *Store, when string) {
		t.Helper()
		got, err := s.UsageTotals(context.Background(), keyP1, 10)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: totals %+v, want %+v", when, got, want)
		}
		none, err := s.UsageTotals(context.Background(), keyP1, 99)
		if err != nil {
			t.Fatal(err)
		}
		if none != (UsageTotals{}) {
			t.Errorf("%s: an instance with no rows: %+v, want the zero value", when, none)
		}
	}
	check(s, "before reopen")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check(s, "after reopen")
}

// TestUsageHourly buckets by hour and key, half-open [from, to), ordered by
// start and then key, for each grouping.
func TestUsageHourly(t *testing.T) {
	s, _ := openTemp(t)
	const h = int64(hourMs)
	base := 1000 * h
	other := core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w2:p2"}
	opus := func(r UsageRow) UsageRow { return r }
	sonnet := func(r UsageRow) UsageRow { r.Model = "claude-sonnet"; return r }
	appendUsage(t, s,
		opus(usageRow(keyP1, 10, "work", "early", base-1, 100)),   // before from
		opus(usageRow(keyP1, 10, "work", "A", base, 1)),           // hour 0
		opus(usageRow(keyP1, 30, "work", "F", base+9, 32)),        // hour 0, a later Agent on the key
		sonnet(usageRow(other, 20, "personal", "B", base+h-1, 2)), // hour 0
		opus(usageRow(other, 20, "personal", "C", base+5, 4)),     // hour 0
		opus(usageRow(keyP1, 10, "work", "D", base+h, 8)),         // hour 1
		sonnet(usageRow(keyP1, 10, "work", "E", base+2*h+7, 16)),  // hour 2
		opus(usageRow(keyP1, 10, "work", "late", base+3*h, 1000)), // at to: excluded
	)
	from, to := base, base+3*h
	get := func(by UsageBy) []UsageBucket {
		t.Helper()
		b, err := s.UsageHourly(context.Background(), from, to, by, 100)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	sums := func(b UsageBucket, n, responses int64) UsageBucket {
		b.Input, b.Output, b.CacheWrite, b.CacheRead, b.Responses = n, 2*n, 7*n, 5*n, responses
		return b
	}
	byProfile := []UsageBucket{
		sums(UsageBucket{Start: base, Profile: "personal"}, 6, 2),
		sums(UsageBucket{Start: base, Profile: "work"}, 33, 2),
		sums(UsageBucket{Start: base + h, Profile: "work"}, 8, 1),
		sums(UsageBucket{Start: base + 2*h, Profile: "work"}, 16, 1),
	}
	if got := get(UsageByProfile); !reflect.DeepEqual(got, byProfile) {
		t.Errorf("by profile\n got %+v\nwant %+v", got, byProfile)
	}
	byModel := []UsageBucket{
		sums(UsageBucket{Start: base, Model: "claude-opus"}, 37, 3),
		sums(UsageBucket{Start: base, Model: "claude-sonnet"}, 2, 1),
		sums(UsageBucket{Start: base + h, Model: "claude-opus"}, 8, 1),
		sums(UsageBucket{Start: base + 2*h, Model: "claude-sonnet"}, 16, 1),
	}
	if got := get(UsageByModel); !reflect.DeepEqual(got, byModel) {
		t.Errorf("by model\n got %+v\nwant %+v", got, byModel)
	}
	byAgent := []UsageBucket{
		sums(UsageBucket{Start: base, Agent: keyP1, AgentSince: 10}, 1, 1),
		sums(UsageBucket{Start: base, Agent: keyP1, AgentSince: 30}, 32, 1),
		sums(UsageBucket{Start: base, Agent: other, AgentSince: 20}, 6, 2),
		sums(UsageBucket{Start: base + h, Agent: keyP1, AgentSince: 10}, 8, 1),
		sums(UsageBucket{Start: base + 2*h, Agent: keyP1, AgentSince: 10}, 16, 1),
	}
	if got := get(UsageByAgent); !reflect.DeepEqual(got, byAgent) {
		t.Errorf("by agent\n got %+v\nwant %+v", got, byAgent)
	}

	// Exactly limit buckets is fine; one more than limit is refused whole.
	if b, err := s.UsageHourly(context.Background(), from, to, UsageByProfile, 4); err != nil || len(b) != 4 {
		t.Errorf("limit 4: %d buckets, %v; want 4, nil", len(b), err)
	}
	if b, err := s.UsageHourly(context.Background(), from, to, UsageByProfile, 3); !errors.Is(err, ErrTooManyBuckets) || b != nil {
		t.Errorf("limit 3: %v, %v; want nil, ErrTooManyBuckets", b, err)
	}
	if b, err := s.UsageHourly(context.Background(), to, to+h, UsageByModel, 1); err != nil || len(b) != 1 {
		t.Errorf("one bucket at limit 1: %v, %v", b, err)
	}
	if b, err := s.UsageHourly(context.Background(), base+4*h, base+5*h, UsageByModel, 1); err != nil || len(b) != 0 {
		t.Errorf("an empty range: %v, %v; want no buckets", b, err)
	}
	if _, err := s.UsageHourly(context.Background(), from, to, UsageBy("session"), 10); err == nil {
		t.Error("an unknown grouping was accepted")
	}
	if _, err := s.UsageHourly(context.Background(), from, to, UsageByModel, 0); err == nil {
		t.Error("limit 0 was accepted")
	}
}

// TestUpgradeFromSchema7: a schema-7 database with rows in every table opens
// at 8 with every earlier row unchanged and an empty usage table; a binary
// that knows only schema 7 then refuses it.
func TestUpgradeFromSchema7(t *testing.T) {
	path := dbPath(t)
	raw := rawAt(t, path, 7)
	old := &Store{db: raw}
	liveAt(t, old, keyP1, "", core.StatusWorking, 10)
	liveAt(t, old, keyP1, core.StatusWorking, core.StatusDone, 20)
	if err := old.SaveServerState("mac", "default", ServerState{Epoch: 1, Terminals: map[string]string{"w1:p1": "t1"}}); err != nil {
		t.Fatal(err)
	}
	if err := old.AppendAcknowledgements(context.Background(), []core.Acknowledgement{mustAck(t, keyP1, 20, true, 30)}); err != nil {
		t.Fatal(err)
	}
	tables := append(append([]string{}, recordTables...), "acknowledgements")
	before := map[string]string{}
	for _, table := range tables {
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
	for _, table := range tables {
		if got := dumpTable(t, s.db, table); got != before[table] {
			t.Errorf("%s changed by the upgrade:\n got %s\nwant %s", table, got, before[table])
		}
	}
	if n := usageCount(t, s.db); n != 0 {
		t.Errorf("%d usage rows after the upgrade, want none", n)
	}
	if n := appendUsage(t, s, usageRow(keyP1, 10, "work", "A", 100, 1)); n != 1 {
		t.Errorf("inserted %d after the upgrade, want 1", n)
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
	err = (&Store{db: raw}).prepare(context.Background(), path, ms[:7])
	if !errors.Is(err, ErrSchemaNewer) || !strings.Contains(err.Error(), fmt.Sprintf("version %d", schemaVersion)) || !strings.Contains(err.Error(), "up to 7") {
		t.Errorf("schema-7 binary on a schema-%d database: %v, want ErrSchemaNewer naming %d and 7", schemaVersion, err, schemaVersion)
	}
}

// TestUpgradeFromSchema7RollsBack: 0008 runs in the same transaction as any
// migration after it, so a failing one leaves the database at schema 7 with
// no usage table and its rows unchanged.
func TestUpgradeFromSchema7RollsBack(t *testing.T) {
	path := dbPath(t)
	raw := rawAt(t, path, 7)
	defer raw.Close()
	if err := (&Store{db: raw}).AppendAcknowledgements(context.Background(), []core.Acknowledgement{mustAck(t, keyP1, 20, true, 30)}); err != nil {
		t.Fatal(err)
	}
	liveAt(t, &Store{db: raw}, keyP1, "", core.StatusWorking, 10)
	before := dumpTable(t, raw, "acknowledgements")
	beforeRecord := map[string]string{}
	for _, table := range recordTables {
		beforeRecord[table] = dumpTable(t, raw, table)
	}
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	bad := append(ms[:8:8], migration{version: 9, name: "0009_bad.sql", sql: "CREATE TABLE nope ("})
	if err := (&Store{db: raw}).prepare(context.Background(), path, bad); err == nil {
		t.Fatal("a failing migration after 0008 was accepted")
	}
	if v := userVersion(t, raw); v != 7 {
		t.Errorf("user_version %d after the failed upgrade, want 7", v)
	}
	var n int
	if err := raw.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'usage%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d usage schema objects survived the rolled-back upgrade", n)
	}
	if got := dumpTable(t, raw, "acknowledgements"); got != before {
		t.Errorf("acknowledgements changed by the failed upgrade:\n got %s\nwant %s", got, before)
	}
	for _, table := range recordTables {
		if got := dumpTable(t, raw, table); got != beforeRecord[table] {
			t.Errorf("%s changed by the failed upgrade:\n got %s\nwant %s", table, got, beforeRecord[table])
		}
	}
}

// TestAppendUsageRoundTrip writes one row with every field distinct and reads
// every column back.
func TestAppendUsageRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	in := UsageRow{Key: keyP1, AgentSince: 11, Profile: "personal", SessionID: "sess-rt", ObservedAt: 9999,
		Response: core.Response{MessageID: "msg-rt", RequestID: "req-rt", Model: "claude-haiku", SubagentID: "sub1",
			Stopped: false, At: 5000,
			Usage: core.Usage{Input: 21, Output: 22, CacheWrite5m: 23, CacheWrite1h: 24, CacheRead: 25}}}
	if n := appendUsage(t, s, in); n != 1 {
		t.Fatalf("inserted %d, want 1", n)
	}
	var got UsageRow
	var epoch, stopped int64
	if err := s.db.QueryRow(`SELECT host, server, epoch, pane_id, agent_since, profile, session_id, subagent_id,
		message_id, request_id, model, responded_at, input, output, cache_write_5m, cache_write_1h, cache_read, stopped,
		observed_at FROM usage`).Scan(&got.Key.Host, &got.Key.Server, &epoch, &got.Key.PaneID, &got.AgentSince,
		&got.Profile, &got.SessionID, &got.SubagentID, &got.MessageID, &got.RequestID, &got.Model, &got.At,
		&got.Input, &got.Output, &got.CacheWrite5m, &got.CacheWrite1h, &got.CacheRead, &stopped, &got.ObservedAt); err != nil {
		t.Fatal(err)
	}
	got.Key.Epoch = uint64(epoch)
	if stopped != 0 {
		t.Errorf("stopped %d, want 0", stopped)
	}
	got.Stopped = stopped == 1
	if !reflect.DeepEqual(got, in) {
		t.Errorf("read back\n got %+v\nwant %+v", got, in)
	}
}
