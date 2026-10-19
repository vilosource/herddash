package engine

import (
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
	"github.com/vilosource/herddash/internal/store"
)

const dayMs = int64(24 * time.Hour / time.Millisecond)

// retentionDays is the Retention every test here runs with.
const retentionDays = 90

// withRetention sets a 90-day Retention and collects the log into logs.
func withRetention(logs *logSink) func(*Options, *recorder) {
	return func(o *Options, _ *recorder) {
		o.RetentionDays = retentionDays
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
}

// setRetentionInterval sets retentionSweepInterval for one test, restoring it
// after.
func setRetentionInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := retentionSweepInterval
	retentionSweepInterval = d
	t.Cleanup(func() { retentionSweepInterval = prev })
}

// retentionTick delivers one Retention timer tick, as the timer would, and
// waits until the engine has handled it.
func (h *harness) retentionTick() {
	h.t.Helper()
	h.engine.inbox <- retentionDueMsg{}
	h.agents()
}

// startedRetaining is startedRecording with a 90-day Retention, the timer out
// of the way so the test delivers each tick itself.
func startedRetaining(t *testing.T) (*harness, *fakeStore, *logSink) {
	t.Helper()
	setRetentionInterval(t, time.Hour)
	setReconcileInterval(t, time.Hour)
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	logs := &logSink{}
	h := startRecording(t, fake, st, withRetention(logs))
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	return h, st, logs
}

// presentOf maps each present pane id to its Since.
func presentOf(c sweepCall) map[string]int64 {
	out := map[string]int64{}
	for _, p := range c.present {
		out[p.Key.PaneID] = p.Since
	}
	return out
}

func TestRetentionFirstSweepAtReady(t *testing.T) {
	h, st, logs := startedRetaining(t)
	sw := st.sweepList()
	if len(sw) != 1 {
		t.Fatalf("%d sweeps at start, want one when the connection became ready", len(sw))
	}
	// After the connect marker, before the Agents' subscriptions settled.
	w := st.writeList()
	sweepAt, connectAt, rowAt := slices.Index(w, "sweep"), slices.Index(w, "marker connect 1"), slices.Index(w, "row w1:p2 1")
	if sweepAt < 0 || connectAt < 0 || rowAt < 0 || sweepAt < connectAt || sweepAt < rowAt {
		t.Errorf("writes %v, want the sweep after the connect marker and the first snapshot's rows", w)
	}
	// The present set is both Agents under the current epoch, each since its
	// first sight; cutoff is 90 days before the clock.
	rows := st.rowList()
	got := presentOf(sw[0])
	if len(got) != 2 || got["w1:p1"] != rows[0].ObservedAt() || got["w1:p2"] != rows[1].ObservedAt() {
		t.Errorf("present %+v, want both Agents since their first sights", sw[0].present)
	}
	for _, p := range sw[0].present {
		if p.Key.Epoch != 1 || p.Key.Host != "mac" || p.Key.Server != "default" {
			t.Errorf("present key %+v, want mac/default epoch 1", p.Key)
		}
	}
	if c := sw[0].cutoff; c <= clockAt(0)-retentionDays*dayMs || c > clockAt(60)-retentionDays*dayMs {
		t.Errorf("cutoff %d, want 90 days before the clock (%d)", c, clockAt(0)-retentionDays*dayMs)
	}
	if len(logs.entries(t, "INFO", "retention sweep")) != 1 {
		t.Error("no sweep logged at info")
	}
	if e := logs.entries(t, "INFO", "retention enabled"); len(e) != 1 || e[0]["retention_days"] != float64(retentionDays) || e[0]["interval"] != "1h0m0s" {
		t.Errorf("logged %v, want Retention enabled once, with its days and interval", e)
	}

	// Each tick sweeps again.
	h.retentionTick()
	h.retentionTick()
	if n := len(st.sweepList()); n != 3 {
		t.Errorf("%d sweeps after two ticks, want 3", n)
	}
}

func TestRetentionTimer(t *testing.T) {
	// The timer itself ticks every interval.
	setRetentionInterval(t, 20*time.Millisecond)
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, withRetention(&logSink{}))
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	deadline := time.Now().Add(wait)
	for len(st.sweepList()) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%d sweeps, want the first and two ticks", len(st.sweepList()))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRetentionDisabled(t *testing.T) {
	// RetentionDays 0 never sweeps, at ready or on a tick.
	setRetentionInterval(t, 10*time.Millisecond)
	logs := &logSink{}
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, func(o *Options, _ *recorder) {
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	time.Sleep(50 * time.Millisecond)
	if len(logs.entries(t, "INFO", "retention disabled")) != 1 {
		t.Error("Retention disabled not logged once at info")
	}
	h.retentionTick()
	if n := len(st.sweepList()); n != 0 {
		t.Errorf("%d sweeps with Retention disabled", n)
	}
}

func TestRetentionNoStore(t *testing.T) {
	// Without a Store there is nothing to sweep; the engine runs on.
	setRetentionInterval(t, 10*time.Millisecond)
	h := startEngineWith(t, newFakeClient(t, twoAgents()), func(o *Options, _ *recorder) { o.RetentionDays = retentionDays })
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.retentionTick()
	if as := h.agents(); len(as) != 2 {
		t.Errorf("agents %+v", as)
	}
}

func TestRetentionSkippedDuringResync(t *testing.T) {
	h, st, logs := startedRetaining(t)
	h.fake.setGated(true)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.awaitCount(t, "resync events_lost", 1)
	h.fake.awaitSnapshotCall() // the resync's read, held
	h.retentionTick()
	if n := len(st.sweepList()); n != 1 {
		t.Fatalf("%d sweeps, want the tick during the resync skipped", n)
	}
	if len(logs.entries(t, "DEBUG", "retention sweep skipped")) != 1 {
		t.Error("skip not logged")
	}
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.settle()
	h.retentionTick()
	if n := len(st.sweepList()); n != 2 {
		t.Errorf("%d sweeps, want the next tick after the resync to sweep", n)
	}
}

func TestRetentionSkippedWhileResyncScheduled(t *testing.T) {
	h, st, _ := startedRetaining(t)
	prev := resyncMinInterval
	resyncMinInterval = time.Hour
	t.Cleanup(func() { resyncMinInterval = prev })
	// The first resync runs now; one triggered after it ends waits for the
	// minimum interval, scheduled.
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.awaitCount(t, "resync events_lost", 1)
	sub := h.fake.awaitStatusSubs("w1:p1", 2)
	h.settle()
	h.retentionTick()
	if n := len(st.sweepList()); n != 2 {
		t.Fatalf("%d sweeps, want the tick after the first resync to sweep", n)
	}
	sub.health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.settle()
	h.retentionTick()
	if n := len(st.sweepList()); n != 2 {
		t.Errorf("%d sweeps, want the tick skipped while a resync is scheduled", n)
	}
	if n := h.rec.count("resync events_lost"); n != 1 {
		t.Errorf("%d resyncs reported, want the second still scheduled", n)
	}
}

func TestRetentionSkippedWhileDisconnected(t *testing.T) {
	h, st, _ := startedRetaining(t)
	h.fake.fail("ping", 1<<30)
	h.loseLifecycle()
	h.fake.awaitPings(3) // reconnecting, and failing
	h.retentionTick()
	if n := len(st.sweepList()); n != 1 {
		t.Fatalf("%d sweeps, want the tick while reconnecting skipped", n)
	}
	// Once reconnected, the timer goes on; the reconnect itself does not
	// sweep.
	h.fake.fail("ping", 0)
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	h.settle()
	if n := len(st.sweepList()); n != 1 {
		t.Errorf("%d sweeps after the reconnect, want no extra one", n)
	}
	h.retentionTick()
	if n := len(st.sweepList()); n != 2 {
		t.Errorf("%d sweeps, want the tick after the reconnect to sweep", n)
	}
}

func TestRetentionSweepFails(t *testing.T) {
	setRetentionInterval(t, time.Hour)
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	st.sweepErr = errors.New("disk full")
	logs := &logSink{}
	h := startRecording(t, fake, st, withRetention(logs))
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	h.retentionTick()
	failed := logs.entries(t, "ERROR", "retention sweep failed")
	if len(failed) != 2 || failed[0]["error"] != "disk full" {
		t.Fatalf("logged %v, want both failed sweeps at error with the reason", failed)
	}
	// The engine runs on.
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->blocked live", 1)
}

func TestRetentionKeepsFoldAcrossRestart(t *testing.T) {
	// The record from a previous run under epoch 3: w1:p1 has been blocked
	// since 120 days ago (live), after older rows; w1:p2 is idle, its last
	// working row 200 days old; w1:gone is no longer there; backfilled rows
	// and markers are old. The Daemon restarts with both Agents unchanged.
	setRetentionInterval(t, time.Hour)
	now := clockAt(0)
	ago := func(days int64) int64 { return now - days*dayMs }
	st := newFakeStore(nil)
	st.seed(
		stored(t, "w1:p1", "", core.StatusIdle, ago(300), core.ProvenanceSnapshot),
		stored(t, "w1:p1", core.StatusIdle, core.StatusWorking, ago(130), core.ProvenanceLive),
		stored(t, "w1:p1", core.StatusWorking, core.StatusBlocked, ago(120), core.ProvenanceLive),
		stored(t, "w1:p2", "", core.StatusIdle, ago(260), core.ProvenanceSnapshot),
		stored(t, "w1:p2", core.StatusIdle, core.StatusWorking, ago(200), core.ProvenanceLive),
		stored(t, "w1:p2", core.StatusWorking, core.StatusIdle, ago(190), core.ProvenanceLive),
		stored(t, "w1:p2", core.StatusIdle, core.StatusIdle, ago(10), core.ProvenanceSnapshot),
		stored(t, "w1:gone", "", core.StatusWorking, ago(100), core.ProvenanceLive),
	)
	backfill, err := core.NewTransition(core.TransitionInput{
		Key:          core.AgentKey{Host: "mac", Server: "default", Epoch: core.BackfillEpoch, PaneID: "w1:p1"},
		AgentProgram: "claude", Profile: "claude", New: core.StatusDone, ObservedAt: ago(150),
		Provenance: core.ProvenanceBackfill, ImportKey: "log:1"})
	if err != nil {
		t.Fatal(err)
	}
	st.seed(backfill)
	st.mu.Lock()
	st.markers = append(st.markers,
		store.ResyncMarker{Host: "mac", Server: "default", Epoch: 3, ObservedAt: ago(100), Reason: store.ReasonDaemonStart},
		store.ResyncMarker{Host: "mac", Server: "default", Epoch: 3, ObservedAt: ago(10), Reason: store.ReasonDaemonStart})
	st.mu.Unlock()

	snap := twoAgents()
	snap.Panes[0].Agent.Status = core.StatusBlocked
	foldBefore := map[string]core.FoldResult{}
	for _, id := range []string{"w1:p1", "w1:p2"} {
		ts, _ := st.History(t.Context(), core.AgentKey{Host: "mac", Server: "default", Epoch: 3, PaneID: id})
		foldBefore[id] = core.Fold(ts)
	}
	h := restartOn(t, snap, st, withRetention(&logSink{}))
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude blocked", "agent w1:p2 claude idle")
	h.settle()
	if n := len(st.sweepList()); n != 1 {
		t.Fatalf("%d sweeps, want one at ready", n)
	}
	if got := presentOf(st.sweepList()[0]); got["w1:p1"] != ago(120) || got["w1:p2"] != ago(190) {
		t.Errorf("present %v, want w1:p1 since 120 days ago and w1:p2 since 190", got)
	}

	got := rows(st.rowList())
	want := []string{
		`w1:p1 e3 idle->working live seq=- v="0.9.5" program="claude" profile="claude"`,
		`w1:p1 e3 working->blocked live seq=- v="0.9.5" program="claude" profile="claude"`,
		`w1:p2 e3 idle->working live seq=- v="0.9.5" program="claude" profile="claude"`,
		`w1:p2 e3 working->idle live seq=- v="0.9.5" program="claude" profile="claude"`,
		`w1:p2 e3 idle->idle snapshot seq=- v="0.9.5" program="claude" profile="claude"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows after the sweep\n got %v\nwant %v", got, want)
	}
	if ms := st.markerList(); !slices.Equal(ms, []string{"daemon_start 3 -", "daemon_start 3 -", "connect 3 epoch kept"}) {
		t.Errorf("markers %v, want the 100-day one swept", ms)
	}
	for _, id := range []string{"w1:p1", "w1:p2"} {
		key := core.AgentKey{Host: "mac", Server: "default", Epoch: 3, PaneID: id}
		ts, _ := st.History(t.Context(), key)
		f := core.Fold(ts)
		b := foldBefore[id]
		if f.Class != b.Class || f.Since != b.Since || f.EverWorking != b.EverWorking {
			t.Errorf("%s fold after the sweep %+v, want %+v", id, f, b)
		}
		a := agentByPane(t, h.agents(), id)
		if a.Class != b.Class || a.WaitingSince != b.Since {
			t.Errorf("%s Agent %s since %d, want %s since %d", id, a.Class, a.WaitingSince, b.Class, b.Since)
		}
	}
	if b := foldBefore["w1:p1"]; b.Class != core.ClassNeedsYou || !b.EverWorking {
		t.Errorf("w1:p1 fold %+v, want needs_you", b)
	}
	if b := foldBefore["w1:p2"]; b.Class != core.ClassFinished || !b.EverWorking {
		t.Errorf("w1:p2 fold %+v, want finished (idle after working)", b)
	}
}

func TestRetentionPendingFirstSightIsPresent(t *testing.T) {
	// An Agent whose Profile is still resolving at the first sweep has not
	// been reported, but its first sight will read its history: its rows are
	// all kept (Since 0).
	setRetentionInterval(t, time.Hour)
	setProfileTimeout(t, wait)
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusWorking, clockAt(0)-200*dayMs, core.ProvenanceLive))
	snap := oneAgent("claude", core.StatusWorking)
	fake := newFakeClient(t, snap)
	st.client = fake
	st.setState(store.ServerState{Epoch: 3, Terminals: terminalsOf(snap)})
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	env.hold()
	h := startRecording(t, fake, st, func(o *Options, r *recorder) {
		withRetention(&logSink{})(o, r)
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
	})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	sw := st.sweepList()
	if len(sw) != 1 {
		t.Fatalf("%d sweeps, want one at ready", len(sw))
	}
	if got := presentOf(sw[0]); len(got) != 1 || got["w1:p1"] != 0 {
		t.Errorf("present %v, want w1:p1 with Since 0", got)
	}
	if n := len(st.rowList()); n != 1 {
		t.Errorf("%d rows, want the pending Agent's row kept", n)
	}
	env.unblock()
	h.rec.awaitCount(t, "agent w1:p1 claude working", 1)
	a := agentByPane(t, h.agents(), "w1:p1")
	if a.WaitingSince != clockAt(0)-200*dayMs || !a.WaitingExact {
		t.Errorf("Agent since %d exact %v, want the stored row's time, exact", a.WaitingSince, a.WaitingExact)
	}
}

// unrecordedRestart restarts on snap and st, with st failing as set, and
// returns the first sweep's present set once w1:p1 is reported blocked.
func unrecordedRestart(t *testing.T, st *fakeStore, snap core.Snapshot) map[string]int64 {
	t.Helper()
	setRetentionInterval(t, time.Hour)
	h := restartOn(t, snap, st, withRetention(&logSink{}))
	h.rec.awaitCount(t, "agent w1:p1 claude blocked", 1)
	h.settle()
	sw := st.sweepList()
	if len(sw) != 1 {
		t.Fatalf("%d sweeps, want one at ready", len(sw))
	}
	return presentOf(sw[0])
}

func TestRetentionHistoryReadFailedKeepsAll(t *testing.T) {
	// The first sight could not read the record, so its fold starts at the
	// first sight, after the stored class start: Since 0 keeps that row.
	st := newFakeStore(nil)
	old := stored(t, "w1:p1", core.StatusWorking, core.StatusBlocked, clockAt(0)-200*dayMs, core.ProvenanceLive)
	st.seed(old)
	st.historyErr = errors.New("disk read")
	snap := twoAgents()
	snap.Panes[0].Agent.Status = core.StatusBlocked
	got := unrecordedRestart(t, st, snap)
	if s, ok := got["w1:p1"]; !ok || s != 0 {
		t.Errorf("present %v, want w1:p1 with Since 0", got)
	}
	if rs := st.rowList(); len(rs) == 0 || rs[0].ObservedAt() != old.ObservedAt() {
		t.Errorf("rows %v, want the stored class start kept", rows(rs))
	}
}

func TestRetentionAppendFailedKeepsAll(t *testing.T) {
	// The record matched at the restart, then the live change could not be
	// written: the fold's Since is a row the record lacks, so Since 0.
	setRetentionInterval(t, time.Hour)
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", core.StatusIdle, core.StatusWorking, clockAt(0)-200*dayMs, core.ProvenanceLive))
	snap := twoAgents()
	h := restartOn(t, snap, st, withRetention(&logSink{}))
	h.rec.awaitCount(t, "agent w1:p1 claude working", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	if got := presentOf(st.sweepList()[0]); got["w1:p1"] != clockAt(0)-200*dayMs {
		t.Fatalf("present %v, want w1:p1 since the stored row while the record holds its fold", got)
	}
	st.mu.Lock()
	st.appendErr = errors.New("disk full")
	st.mu.Unlock()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->blocked live", 1)
	h.retentionTick()
	sw := st.sweepList()
	if got := presentOf(sw[len(sw)-1]); got["w1:p1"] != 0 || got["w1:p2"] == 0 {
		t.Errorf("present %v, want w1:p1 with Since 0 and w1:p2 unaffected", got)
	}
}
