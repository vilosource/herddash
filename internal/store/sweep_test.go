package store

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/vilosource/herddash/internal/core"
)

const day = int64(24 * 60 * 60 * 1000)

// sweepNow and sweepCutoff are the sweep's clock and a 90-day Retention.
const (
	sweepNow    = 1000 * day
	sweepCutoff = sweepNow - 90*day
)

func keyOf(pane string) core.AgentKey {
	return core.AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: pane}
}

// appendAt appends one live (or, for epoch 0, backfill) row for key moving
// from prev ("" for none) to next, daysAgo days before sweepNow.
func appendAt(t *testing.T, s *Store, key core.AgentKey, prev, next core.Status, daysAgo int64) {
	t.Helper()
	in := core.TransitionInput{Key: key, AgentProgram: "claude", Profile: "claude", New: next,
		HerdrVersion: "0.9.5", ObservedAt: sweepNow - daysAgo*day, Provenance: core.ProvenanceLive}
	if prev != "" {
		in.Previous = &prev
	}
	if key.Epoch == core.BackfillEpoch {
		in.Provenance = core.ProvenanceBackfill
		in.HerdrVersion = ""
		in.ImportKey = fmt.Sprintf("log:%s:%d:%s", key.PaneID, daysAgo, next)
	}
	if err := s.Append(mustTransition(t, in)); err != nil {
		t.Fatal(err)
	}
}

func appendMarkerAt(t *testing.T, s *Store, daysAgo int64) {
	t.Helper()
	if err := s.AppendResyncMarker(ResyncMarker{Host: "mac", Server: "default", Epoch: 2,
		ObservedAt: sweepNow - daysAgo*day, Reason: ReasonDaemonStart}); err != nil {
		t.Fatal(err)
	}
}

// daysOf lists key's remaining rows as "<status>@<days ago>", in History's
// order.
func daysOf(t *testing.T, s *Store, key core.AgentKey) []string {
	t.Helper()
	ts, err := s.History(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, tr := range ts {
		out = append(out, fmt.Sprintf("%s@%d", tr.New(), (sweepNow-tr.ObservedAt())/day))
	}
	return out
}

func sweep(t *testing.T, s *Store, present ...Present) SweepResult {
	t.Helper()
	r, err := s.Sweep(context.Background(), sweepCutoff, present)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func expectDays(t *testing.T, s *Store, key core.AgentKey, want ...string) {
	t.Helper()
	got := daysOf(t, s, key)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s rows %v, want %v", key.PaneID, got, want)
	}
}

func folded(t *testing.T, s *Store, key core.AgentKey) core.FoldResult {
	t.Helper()
	ts, err := s.History(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return core.Fold(ts)
}

func TestSweepAbsentAgent(t *testing.T) {
	s, _ := openTemp(t)
	gone := keyOf("w1:gone")
	appendAt(t, s, gone, "", core.StatusWorking, 101)
	appendAt(t, s, gone, core.StatusWorking, core.StatusDone, 100)
	r := sweep(t, s)
	expectDays(t, s, gone)
	if r.Transitions != 2 || r.ResyncMarkers != 0 {
		t.Errorf("result %+v, want 2 transitions", r)
	}
}

func TestSweepPresentKeepsClass(t *testing.T) {
	// Blocked since 120 days ago: the rows before the class began go, the
	// class start and everything after it stay. Its latest working row is
	// also older than the class start, and stays.
	s, _ := openTemp(t)
	k := keyOf("w1:p1")
	appendAt(t, s, k, "", core.StatusIdle, 200)
	appendAt(t, s, k, core.StatusIdle, core.StatusWorking, 150)
	appendAt(t, s, k, core.StatusWorking, core.StatusIdle, 140)
	appendAt(t, s, k, core.StatusIdle, core.StatusWorking, 130)
	appendAt(t, s, k, core.StatusWorking, core.StatusBlocked, 120)
	before := folded(t, s, k)
	if before.Class != core.ClassNeedsYou || before.Since != sweepNow-120*day {
		t.Fatalf("fold %+v, want needs_you since 120 days ago", before)
	}
	r := sweep(t, s, Present{Key: k, Since: before.Since})
	expectDays(t, s, k, "working@130", "blocked@120")
	if r.Transitions != 3 {
		t.Errorf("deleted %d, want 3", r.Transitions)
	}
	if after := folded(t, s, k); after.Class != before.Class || after.Since != before.Since || after.EverWorking != before.EverWorking {
		t.Errorf("fold after the sweep %+v, want %+v", after, before)
	}
}

func TestSweepKeepsLatestWorking(t *testing.T) {
	// Idle after working, the class (finished) began 150 days ago. Only the
	// last working row stays from before it, so idle is still finished.
	s, _ := openTemp(t)
	k := keyOf("w1:p1")
	appendAt(t, s, k, "", core.StatusWorking, 300)
	appendAt(t, s, k, core.StatusWorking, core.StatusDone, 250)
	appendAt(t, s, k, core.StatusDone, core.StatusWorking, 200)
	appendAt(t, s, k, core.StatusWorking, core.StatusIdle, 150)
	before := folded(t, s, k)
	if before.Class != core.ClassFinished || !before.EverWorking {
		t.Fatalf("fold %+v, want finished, ever working", before)
	}
	sweep(t, s, Present{Key: k, Since: before.Since})
	expectDays(t, s, k, "working@200", "idle@150")
	if after := folded(t, s, k); after.Class != before.Class || after.Since != before.Since || after.EverWorking != before.EverWorking {
		t.Errorf("fold after the sweep %+v, want %+v", after, before)
	}
}

func TestSweepDoneThenIdle(t *testing.T) {
	// done then idle keeps the class start at the done row; the working row
	// before it stays as the latest working one, the first idle row goes.
	s, _ := openTemp(t)
	k := keyOf("w1:p1")
	appendAt(t, s, k, "", core.StatusIdle, 220)
	appendAt(t, s, k, core.StatusIdle, core.StatusWorking, 200)
	appendAt(t, s, k, core.StatusWorking, core.StatusDone, 195)
	appendAt(t, s, k, core.StatusDone, core.StatusIdle, 10)
	before := folded(t, s, k)
	sweep(t, s, Present{Key: k, Since: before.Since})
	expectDays(t, s, k, "working@200", "done@195", "idle@10")
	if after := folded(t, s, k); after != before {
		t.Errorf("fold after the sweep %+v, want %+v", after, before)
	}
}

func TestSweepRecentRowsUntouched(t *testing.T) {
	s, _ := openTemp(t)
	gone := keyOf("w1:gone")
	appendAt(t, s, gone, "", core.StatusWorking, 89)
	appendAt(t, s, gone, core.StatusWorking, core.StatusDone, 1)
	if r := sweep(t, s); r.Transitions != 0 {
		t.Errorf("deleted %d recent rows", r.Transitions)
	}
	expectDays(t, s, gone, "working@89", "done@1")
}

func TestSweepMarkersByAge(t *testing.T) {
	s, _ := openTemp(t)
	appendMarkerAt(t, s, 100)
	appendMarkerAt(t, s, 10)
	r := sweep(t, s, Present{Key: keyOf("w1:p1"), Since: 0})
	if r.ResyncMarkers != 1 {
		t.Errorf("deleted %d markers, want 1", r.ResyncMarkers)
	}
	var n int
	var at int64
	if err := s.db.QueryRow(`SELECT count(*), max(observed_at) FROM resync_markers`).Scan(&n, &at); err != nil {
		t.Fatal(err)
	}
	if n != 1 || at != sweepNow-10*day {
		t.Errorf("%d markers left, latest at %d, want only the 10-day one", n, at)
	}
}

func TestSweepBackfillByAge(t *testing.T) {
	// Epoch 0 is never present, even when a Present names the same pane.
	s, _ := openTemp(t)
	bk := keyOf("w1:p1")
	bk.Epoch = core.BackfillEpoch
	appendAt(t, s, bk, "", core.StatusWorking, 120)
	appendAt(t, s, bk, "", core.StatusDone, 100)
	appendAt(t, s, bk, "", core.StatusDone, 30)
	r := sweep(t, s, Present{Key: keyOf("w1:p1"), Since: sweepNow - 200*day})
	expectDays(t, s, bk, "done@30")
	if r.Transitions != 2 {
		t.Errorf("deleted %d, want 2", r.Transitions)
	}
}

func TestSweepUnknownSinceKeepsAll(t *testing.T) {
	s, _ := openTemp(t)
	k := keyOf("w1:p1")
	appendAt(t, s, k, "", core.StatusIdle, 300)
	appendAt(t, s, k, core.StatusIdle, core.StatusDone, 200)
	if r := sweep(t, s, Present{Key: k, Since: 0}); r.Transitions != 0 {
		t.Errorf("deleted %d rows of an Agent with an unknown class start", r.Transitions)
	}
	expectDays(t, s, k, "idle@300", "done@200")
}

func TestSweepMatchesWholeKey(t *testing.T) {
	// Another epoch, Host or Server with the same pane id is not present.
	s, _ := openTemp(t)
	k := keyOf("w1:p1")
	other := []core.AgentKey{k, k, k}
	other[0].Epoch = 1
	other[1].Host = "linux"
	other[2].Server = "other"
	for _, o := range other {
		appendAt(t, s, o, "", core.StatusWorking, 200)
	}
	appendAt(t, s, k, "", core.StatusWorking, 200)
	sweep(t, s, Present{Key: k, Since: sweepNow - 200*day})
	for _, o := range other {
		expectDays(t, s, o)
	}
	expectDays(t, s, k, "working@200")
}

func TestSweepRollsBackOnError(t *testing.T) {
	// The transitions delete succeeds, then the markers delete fails: the
	// sweep errors and neither table changes.
	s, _ := openTemp(t)
	gone := keyOf("w1:gone")
	appendAt(t, s, gone, "", core.StatusWorking, 200)
	appendMarkerAt(t, s, 200)
	if _, err := s.db.Exec(`CREATE TRIGGER no_marker_delete BEFORE DELETE ON resync_markers
		BEGIN SELECT RAISE(ABORT, 'markers locked'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sweep(context.Background(), sweepCutoff, nil); err == nil {
		t.Fatal("sweep succeeded with the markers delete failing")
	}
	expectDays(t, s, gone, "working@200")
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM resync_markers`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d markers left, want the old one kept", n)
	}
}

func TestSweepTwoAgentsOnOneKey(t *testing.T) {
	// Agent A worked and finished; B began on the same key with a row with no
	// previous Status (AD-3) and never worked. A's working row is not B's, so
	// it goes: only B's class start remains.
	s, _ := openTemp(t)
	k := keyOf("w1:p1")
	appendAt(t, s, k, "", core.StatusWorking, 200)
	appendAt(t, s, k, core.StatusWorking, core.StatusDone, 199)
	appendAt(t, s, k, "", core.StatusIdle, 150)
	appendAt(t, s, k, core.StatusIdle, core.StatusBlocked, 120)
	before := folded(t, s, k)
	if before.Class != core.ClassNeedsYou || before.Since != sweepNow-120*day || before.EverWorking {
		t.Fatalf("fold %+v, want needs_you since 120 days ago, never working", before)
	}
	sweep(t, s, Present{Key: k, Since: before.Since})
	expectDays(t, s, k, "blocked@120")
	if after := folded(t, s, k); after != before {
		t.Errorf("fold after the sweep %+v, want %+v", after, before)
	}
}

func TestSweepTwoAgentsCurrentWorked(t *testing.T) {
	// As above, but B worked: B's latest working row stays, A's goes.
	s, _ := openTemp(t)
	k := keyOf("w1:p1")
	appendAt(t, s, k, "", core.StatusWorking, 200)
	appendAt(t, s, k, core.StatusWorking, core.StatusDone, 199)
	appendAt(t, s, k, "", core.StatusIdle, 150)
	appendAt(t, s, k, core.StatusIdle, core.StatusWorking, 140)
	appendAt(t, s, k, core.StatusWorking, core.StatusBlocked, 120)
	before := folded(t, s, k)
	if before.Class != core.ClassNeedsYou || !before.EverWorking {
		t.Fatalf("fold %+v, want needs_you, ever working", before)
	}
	sweep(t, s, Present{Key: k, Since: before.Since})
	expectDays(t, s, k, "working@140", "blocked@120")
	if after := folded(t, s, k); after != before {
		t.Errorf("fold after the sweep %+v, want %+v", after, before)
	}
}

func TestSweepBoundaryInHistoryOrder(t *testing.T) {
	// The boundary is the last row with no previous Status in History order
	// (observed_at, id), not by id: a row with no previous Status inserted
	// last but observed before B's first row does not end B.
	s, _ := openTemp(t)
	k := keyOf("w1:p1")
	appendAt(t, s, k, "", core.StatusIdle, 150)
	appendAt(t, s, k, core.StatusIdle, core.StatusWorking, 140)
	appendAt(t, s, k, core.StatusWorking, core.StatusBlocked, 120)
	appendAt(t, s, k, "", core.StatusDone, 160)
	before := folded(t, s, k)
	if before.Class != core.ClassNeedsYou || !before.EverWorking {
		t.Fatalf("fold %+v, want needs_you, ever working", before)
	}
	sweep(t, s, Present{Key: k, Since: before.Since})
	expectDays(t, s, k, "working@140", "blocked@120")
}

// ackAt records one acknowledgement of key's finish that began finishedAgo
// days before sweepNow, observed daysAgo days before it.
func ackAt(t *testing.T, s *Store, key core.AgentKey, finishedAgo int64, seen bool, daysAgo int64) {
	t.Helper()
	if err := s.AppendAcknowledgements(context.Background(), []core.Acknowledgement{
		mustAck(t, key, sweepNow-finishedAgo*day, seen, sweepNow-daysAgo*day),
	}); err != nil {
		t.Fatal(err)
	}
}

// acksOf lists key's remaining acknowledgements as "<finish days ago>:<seen>",
// in recording order.
func acksOf(t *testing.T, s *Store, key core.AgentKey) string {
	t.Helper()
	as, err := s.Acknowledgements(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, a := range as {
		out = append(out, fmt.Sprintf("%d:%v", (sweepNow-a.FinishedSince())/day, a.Seen()))
	}
	return fmt.Sprint(out)
}

// TestSweepAcknowledgements: past the cutoff, a present Agent keeps the
// acknowledgements of its current finish, and loses those of an older one; an
// absent Agent loses all of them; a recent row stays whatever it names.
func TestSweepAcknowledgements(t *testing.T) {
	s, _ := openTemp(t)
	here, gone := keyOf("w1:here"), keyOf("w1:gone")
	appendAt(t, s, here, "", core.StatusWorking, 300)
	appendAt(t, s, here, core.StatusWorking, core.StatusDone, 250) // an older finish
	appendAt(t, s, here, core.StatusDone, core.StatusWorking, 210)
	appendAt(t, s, here, core.StatusWorking, core.StatusDone, 200) // the current finish
	ackAt(t, s, here, 250, true, 249)
	ackAt(t, s, here, 200, true, 199)
	ackAt(t, s, here, 200, false, 150)
	ackAt(t, s, here, 200, true, 120)
	ackAt(t, s, gone, 300, true, 299)
	ackAt(t, s, gone, 300, false, 10) // recent: kept by age
	f := folded(t, s, here)
	if f.Since != sweepNow-200*day {
		t.Fatalf("present Since %d, want the current finish", f.Since)
	}
	if !core.Seen(f, mustAcks(t, s, here)) {
		t.Fatal("the current finish is not seen before the sweep")
	}

	r := sweep(t, s, Present{Key: here, Since: f.Since})
	if got, want := acksOf(t, s, here), "[200:true 200:false 200:true]"; got != want {
		t.Errorf("present acknowledgements %s, want %s", got, want)
	}
	if got, want := acksOf(t, s, gone), "[300:false]"; got != want {
		t.Errorf("absent acknowledgements %s, want %s", got, want)
	}
	if r.Acknowledgements != 2 {
		t.Errorf("result %+v, want 2 acknowledgements swept", r)
	}
	if !core.Seen(folded(t, s, here), mustAcks(t, s, here)) {
		t.Error("the acknowledgement in force did not survive the sweep")
	}
}

func mustAcks(t *testing.T, s *Store, key core.AgentKey) []core.Acknowledgement {
	t.Helper()
	as, err := s.Acknowledgements(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return as
}

// TestSweepAcknowledgementsUnknownSinceKeepsAll: a present Agent whose class
// start is unknown keeps every acknowledgement; the key is matched whole.
func TestSweepAcknowledgementsUnknownSinceKeepsAll(t *testing.T) {
	s, _ := openTemp(t)
	here := keyOf("w1:here")
	otherEpoch := core.AgentKey{Host: here.Host, Server: here.Server, Epoch: here.Epoch + 1, PaneID: here.PaneID}
	ackAt(t, s, here, 250, true, 249)
	ackAt(t, s, here, 200, true, 199)
	ackAt(t, s, otherEpoch, 200, true, 199)
	r := sweep(t, s, Present{Key: here, Since: 0})
	if got, want := acksOf(t, s, here), "[250:true 200:true]"; got != want {
		t.Errorf("acknowledgements %s, want %s", got, want)
	}
	if got := acksOf(t, s, otherEpoch); got != "[]" {
		t.Errorf("another epoch's acknowledgements %s, want swept", got)
	}
	if r.Acknowledgements != 1 {
		t.Errorf("result %+v, want 1 acknowledgement swept", r)
	}
}

// TestSweepAcknowledgementsRollsBack: the transitions and markers deletes
// run, then the acknowledgements delete fails: the sweep errors and no table
// changes.
func TestSweepAcknowledgementsRollsBack(t *testing.T) {
	s, _ := openTemp(t)
	gone := keyOf("w1:gone")
	appendAt(t, s, gone, "", core.StatusWorking, 200)
	appendAt(t, s, gone, core.StatusWorking, core.StatusDone, 199)
	appendMarkerAt(t, s, 200)
	ackAt(t, s, gone, 199, true, 198)
	// Witnesses that the earlier deletes ran inside the sweep: each one's
	// trigger counts it, and the rollback takes the counts back with it, so
	// the abort's condition checks them before it raises.
	if _, err := s.db.Exec(`CREATE TABLE deleted (what TEXT);
		CREATE TRIGGER t_deleted AFTER DELETE ON transitions BEGIN INSERT INTO deleted VALUES ('transition'); END;
		CREATE TRIGGER m_deleted AFTER DELETE ON resync_markers BEGIN INSERT INTO deleted VALUES ('marker'); END;
		CREATE TRIGGER no_ack_delete BEFORE DELETE ON acknowledgements
			WHEN (SELECT count(*) FROM deleted WHERE what = 'transition') = 2
			AND (SELECT count(*) FROM deleted WHERE what = 'marker') = 1
			BEGIN SELECT RAISE(ABORT, 'acknowledgements locked'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.Sweep(context.Background(), sweepCutoff, nil)
	if err == nil || !strings.Contains(err.Error(), "acknowledgements locked") {
		t.Fatalf("sweep error %v, want the acknowledgements delete's", err)
	}
	expectDays(t, s, gone, "working@200", "done@199")
	var markers, witnessed int
	if err := s.db.QueryRow(`SELECT count(*) FROM resync_markers`).Scan(&markers); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM deleted`).Scan(&witnessed); err != nil {
		t.Fatal(err)
	}
	if markers != 1 || witnessed != 0 {
		t.Errorf("%d markers, %d witnessed deletes left; want the marker kept and nothing witnessed", markers, witnessed)
	}
	if got, want := acksOf(t, s, gone), "[199:true]"; got != want {
		t.Errorf("acknowledgements %s, want %s", got, want)
	}
}
