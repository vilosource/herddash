package engine

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
	"github.com/vilosource/herddash/internal/store"
)

// startRecording starts an engine on fake that records into st; opts, when
// not nil, adjusts its Options and recorder first.
func startRecording(t *testing.T, fake *fakeClient, st *fakeStore, opts func(*Options, *recorder)) *harness {
	t.Helper()
	return startEngineWith(t, fake, func(o *Options, r *recorder) {
		o.Store = st
		if opts != nil {
			opts(o, r)
		}
	})
}

// row describes a recorded Transition in one line.
func row(tr core.Transition) string {
	prev, ok := tr.Previous()
	if !ok {
		prev = "nil"
	}
	order := "-"
	if o, ok := tr.HerdrOrder(); ok {
		order = fmt.Sprint(o)
	}
	return fmt.Sprintf("%s e%d %s->%s %s seq=%s v=%q program=%q profile=%q",
		tr.Key().PaneID, tr.Key().Epoch, prev, tr.New(), tr.Provenance(), order, tr.HerdrVersion(), tr.AgentProgram(), tr.Profile())
}

func rows(trs []core.Transition) []string {
	out := make([]string, len(trs))
	for i, tr := range trs {
		out[i] = row(tr)
	}
	return out
}

func expectRows(t *testing.T, st *fakeStore, want ...string) {
	t.Helper()
	got := rows(st.awaitRows(t, len(want)))
	if !slices.Equal(got, want) {
		t.Fatalf("rows\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
}

const (
	firstP1 = `w1:p1 e1 nil->working snapshot seq=7 v="0.9.5" program="claude" profile="claude"`
	firstP2 = `w1:p2 e1 nil->idle snapshot seq=7 v="0.9.5" program="claude" profile="claude"`
)

func TestRecordLiveChange(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->blocked live")
	expectRows(t, st, firstP1, firstP2,
		`w1:p1 e1 working->blocked live seq=- v="0.9.5" program="claude" profile="claude"`)

	// The row is the Transition the Sink saw, and the first sights carry the
	// time the snapshot was applied, read before the Agent reports.
	got := st.rowList()
	if row(got[2]) != row(h.rec.lastTransition()) || got[2].ObservedAt() != h.rec.lastTransition().ObservedAt() {
		t.Errorf("row %s at %d, Sink saw %s at %d", row(got[2]), got[2].ObservedAt(), row(h.rec.lastTransition()), h.rec.lastTransition().ObservedAt())
	}
	times := h.rec.observedTimes()
	if got[0].ObservedAt() <= 0 || got[0].ObservedAt() >= times[0] || got[1].ObservedAt() >= times[1] {
		t.Errorf("first sights at %d, %d; Agent reports at %v", got[0].ObservedAt(), got[1].ObservedAt(), times)
	}
}

func TestRecordDaemonStart(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	st.awaitRows(t, 2)
	h.settle()
	w := st.writeList()
	if len(w) < 1 || w[0] != "marker daemon_start 1" || slices.Index(w[1:], "marker daemon_start 1") >= 0 {
		t.Errorf("writes %v, want one daemon_start marker under epoch 1, first", w)
	}
	st.mu.Lock()
	calls, m := st.callsAtMarker, st.markers[0]
	st.mu.Unlock()
	if calls != 0 {
		t.Errorf("the marker was written after %d herdr calls, want before any", calls)
	}
	if m.Host != "mac" || m.Server != "default" || m.ObservedAt <= 0 || m.Detail != "" {
		t.Errorf("marker %+v", m)
	}
}

func TestRecordSnapshotChange(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged, PaneID: "w1:p1"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->idle snapshot")
	expectRows(t, st, firstP1, firstP2,
		`w1:p1 e1 working->idle snapshot seq=7 v="0.9.5" program="claude" profile="claude"`)
}

func TestRecordFirstSightLive(t *testing.T) {
	// A status event that beats the snapshot reading a new pane reports the
	// Agent, so its first sight is recorded live, with no herdr order.
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.fake.awaitSubscribe("w1:p4")
	h.fake.awaitSnapshotCall()
	h.fake.statusSub("w1:p4").event(core.StatusEvent{PaneID: "w1:p4", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude blocked")
	expectRows(t, st, firstP1, firstP2, `w1:p4 e1 nil->blocked live seq=- v="0.9.5" program="claude" profile="claude"`)

	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusBlocked))
	h.fake.setSnapshot(s)
	h.fake.release <- struct{}{}
	h.fake.awaitSnapshotCall()
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.settle()
	if n := len(st.rowList()); n != 3 {
		t.Errorf("%d rows, want 3: the agreeing snapshot records nothing", n)
	}
}

func TestRecordNulls(t *testing.T) {
	// No Agent Program, so no Profile, and a herdr whose version could not be
	// read: the row leaves both empty, which the store writes as NULL.
	snap := core.Snapshot{Panes: []core.PaneObservation{agentPane("w1:p1", core.StatusWorking)}}
	snap.Panes[0].Agent.AgentProgram = ""
	fake := newFakeClient(t, snap)
	fake.setVersions("")
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, func(o *Options, _ *recorder) { o.AllowUnsupportedHerdr = true })
	h.rec.expect(t, "agent w1:p1  working")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle})
	expectRows(t, st,
		`w1:p1 e1 nil->working snapshot seq=7 v="" program="" profile=""`,
		`w1:p1 e1 working->idle live seq=- v="" program="" profile=""`)
}

// terminalsOf is every pane's terminal id in s.
func terminalsOf(s core.Snapshot) map[string]string {
	m := map[string]string{}
	for _, p := range s.Panes {
		m[p.PaneID] = p.TerminalID
	}
	return m
}

func TestRecordNothingPersisted(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	if as := h.agents(); as[0].Key.Epoch != 1 {
		t.Errorf("epoch %d, want 1", as[0].Key.Epoch)
	}
	saved, ok := st.savedState()
	if !ok || saved.Epoch != 1 || !maps.Equal(saved.Terminals, terminalsOf(twoAgents())) {
		t.Errorf("saved %+v, %v; want epoch 1 with every pane's terminal", saved, ok)
	}
	st.mu.Lock()
	saves := st.saves
	st.mu.Unlock()
	if saves != 1 {
		t.Errorf("%d saves, want 1: later snapshots changed nothing", saves)
	}
}

func TestRecordRestartSameHerdr(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	st.setState(store.ServerState{Epoch: 3, Terminals: terminalsOf(twoAgents())})
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	expectRows(t, st,
		strings.Replace(firstP1, "e1", "e3", 1), strings.Replace(firstP2, "e1", "e3", 1),
		`w1:p1 e3 working->blocked live seq=- v="0.9.5" program="claude" profile="claude"`)
	if w := st.writeList(); w[0] != "marker daemon_start 3" {
		t.Errorf("writes %v, want the marker under epoch 3 first", w)
	}
	st.mu.Lock()
	saves := st.saves
	st.mu.Unlock()
	if saves != 0 {
		t.Errorf("%d saves, want none: nothing changed", saves)
	}
}

func TestRecordRestartNewHerdr(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	st.setState(store.ServerState{Epoch: 3, Terminals: map[string]string{"w1:p1": "old-1", "w1:p2": "old-2", "w1:p3": "old-3"}})
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 4 incremented", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	expectRows(t, st, strings.Replace(firstP1, "e1", "e4", 1), strings.Replace(firstP2, "e1", "e4", 1))
	if w := st.writeList(); w[0] != "marker daemon_start 3" {
		t.Errorf("writes %v, want the marker under epoch 3, the one in force at start, first", w)
	}
	if w := st.writeList(); !slices.Equal(w[:4], []string{"marker daemon_start 3", "save 4", "marker connect 4", "row w1:p1 4"}) {
		t.Errorf("writes %v, want epoch 4 saved and its connect marker written before its first row", w)
	}
	saved, _ := st.savedState()
	if saved.Epoch != 4 || !maps.Equal(saved.Terminals, terminalsOf(twoAgents())) {
		t.Errorf("saved %+v, want epoch 4 with the new terminals", saved)
	}
}

func TestRecordStateAcrossRuns(t *testing.T) {
	// One store, three Daemon runs: the same herdr keeps the epoch, a
	// restarted one increments it from the persisted value.
	st := newFakeStore(nil)
	run := func(snap core.Snapshot, lines ...string) {
		t.Helper()
		fake := newFakeClient(t, snap)
		h := startRecording(t, fake, st, nil)
		h.rec.expect(t, lines...)
		h.settle()
		h.stop()
	}
	run(twoAgents(), "agent w1:p1 claude working", "agent w1:p2 claude idle")
	run(twoAgents(), "reconnected 0.9.5 epoch 1 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	run(restarted(), "reconnected 0.9.5 epoch 2 incremented", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	// Each snapshot's state is saved, when it changed, and its connect
	// marker written, before its rows.
	if w := st.writeList(); !slices.Equal(w, []string{
		"marker daemon_start 1", "save 1", "marker connect 1", "row w1:p1 1", "row w1:p2 1",
		"marker daemon_start 1", "marker connect 1", "row w1:p1 1", "row w1:p2 1",
		"marker daemon_start 1", "save 2", "marker connect 2", "row w1:p1 2", "row w1:p2 2",
	}) {
		t.Errorf("writes %v", w)
	}
}

func TestRecordProfileEdit(t *testing.T) {
	// Rows carry the Profile resolved at the time. A Profile Map edit and a
	// restart change only the rows after it.
	st := newFakeStore(nil)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	run := func(rule core.ProfileRule, profile string, lines ...string) {
		t.Helper()
		fake := newFakeClient(t, oneAgent("claude", core.StatusWorking))
		fake.setProc("w1:p1", p1PID)
		h := startRecording(t, fake, st, func(o *Options, r *recorder) {
			o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": rule}
			o.Environ = env.lookup
			r.profileLines = true
		})
		h.rec.expect(t, lines...)
		h.fake.awaitSubscribe("w1:p1")
		h.settle()
		h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
		h.rec.expect(t, append(lines, "transition w1:p1 claude working->blocked live profile="+profile)...)
		h.stop()
	}
	run(claudeRule(), "cldw", "agent w1:p1 claude working profile=cldw")
	edited := claudeRule()
	edited.Values = map[string]string{"/h/.claude-cldw": "cldp"}
	run(edited, "cldp", "reconnected 0.9.5 epoch 1 kept", "agent w1:p1 claude working profile=cldp")

	var profiles []string
	for _, tr := range st.rowList() {
		profiles = append(profiles, tr.Profile())
	}
	if !slices.Equal(profiles, []string{"cldw", "cldw", "cldp", "cldp"}) {
		t.Errorf("row Profiles %v, want the first run's cldw kept and the second run's cldp", profiles)
	}
}

func TestRecordWriteFails(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	boom := errors.New("disk full")
	st.appendErr, st.markerErr, st.saveErr = boom, boom, boom
	logs := &logSink{}
	h := startRecording(t, fake, st, func(o *Options, _ *recorder) {
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->blocked live")
	h.running()

	failed := logs.entries(t, "ERROR", "transition not recorded")
	if len(failed) != 3 {
		t.Fatalf("%d transition write errors logged, want 3 (two first sights, one change)", len(failed))
	}
	if failed[2]["pane"] != "w1:p1" || failed[2]["error"] != "disk full" {
		t.Errorf("logged %v, want the pane and the reason", failed[2])
	}
	for _, msg := range []string{"resync marker not recorded", "server state not saved"} {
		if len(logs.entries(t, "ERROR", msg)) == 0 {
			t.Errorf("no error logged for %q", msg)
		}
	}
	if as := h.agents(); len(as) != 2 || as[0].Key.Epoch != 1 {
		t.Errorf("agents %+v, want both under epoch 1", as)
	}
}

func TestNoStoreRecordsNothing(t *testing.T) {
	// Without a Store the engine behaves as in Epic 1: the clock readings are
	// the reports' alone (TestLiveChange checks them), and the first snapshot
	// decides no epoch.
	h := started(t)
	if h.engine.store != nil {
		t.Fatal("a store was set")
	}
	if slices.Contains(h.rec.all(), "reconnected 0.9.5 epoch 1 kept") {
		t.Error("a start without a store reported a reconnect")
	}
}

func TestRecordLoadFails(t *testing.T) {
	// The saved epoch cannot be read, so starting at epoch 1 and saving it
	// could key new rows like old ones: nothing is recorded this run, and the
	// Sink still prints.
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	st.loadErr = errors.New("database is corrupt")
	logs := &logSink{}
	h := startRecording(t, fake, st, func(o *Options, _ *recorder) {
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->blocked live")
	// A resync and a reconnect write no markers either.
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.awaitCount(t, "resync events_lost", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	h.settle()
	if w := st.writeList(); len(w) != 0 {
		t.Errorf("writes %v, want none", w)
	}
	if _, ok := st.savedState(); ok {
		t.Error("a state was saved")
	}
	e := logs.entries(t, "ERROR", "recording is off for this run")
	if len(e) != 1 || e[0]["error"] != "database is corrupt" {
		t.Errorf("error entries %v, want one naming the cause", e)
	}
}

func TestRecordSaveRetried(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	st.saveErr, st.failSaves = errors.New("disk full"), 1
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	st.mu.Lock()
	st.saveErr = nil
	st.mu.Unlock()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged, PaneID: "w1:p1"})
	h.fake.awaitSnapshotCall()
	h.settle()
	saved, ok := st.savedState()
	if !ok || saved.Epoch != 1 || !maps.Equal(saved.Terminals, terminalsOf(twoAgents())) {
		t.Errorf("saved %+v, %v; want epoch 1 and every terminal after the retry", saved, ok)
	}
}

func TestRecordFirstSightAfterHeldResolution(t *testing.T) {
	// The Profile resolution is held, so the Agent is reported long after the
	// snapshot showed it; the first-sight row still carries that snapshot's
	// provenance, state_change_seq and observation time.
	setProfileTimeout(t, wait)
	st := newFakeStore(nil)
	fake := newFakeClient(t, oneAgent("claude", core.StatusWorking))
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	env.hold()
	h := startRecording(t, fake, st, func(o *Options, r *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
		r.profileLines = true
	})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	if len(h.rec.all()) != 0 {
		t.Fatalf("reported before the Profile resolved: %v", h.rec.all())
	}
	env.unblock()
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw")
	rows := st.awaitRows(t, 1)
	reported := h.rec.observedTimes()[0]
	tr := rows[0]
	order, ok := tr.HerdrOrder()
	// Clock readings: the daemon_start marker (1), the gate (2), the connect
	// marker (3), the snapshot showing the Agent (4), then the report.
	if tr.Provenance() != core.ProvenanceSnapshot || !ok || order != 7 || tr.ObservedAt() != clockAt(4) || reported <= tr.ObservedAt() {
		t.Errorf("first sight %s at %d (reported at %d), want snapshot seq 7 at %d", row(tr), tr.ObservedAt(), reported, clockAt(4))
	}
}

// startMarkers are the markers every recorded start on a fresh store writes.
var startMarkers = []string{"daemon_start 1 -", "connect 1 -"}

// startedRecording starts an engine on twoAgents that records into a fresh
// fake store, and waits for it to settle.
func startedRecording(t *testing.T) (*harness, *fakeStore) {
	t.Helper()
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	if got := st.markerList(); !slices.Equal(got, startMarkers) {
		t.Fatalf("markers at start %v, want %v", got, startMarkers)
	}
	return h, st
}

func expectMarkers(t *testing.T, st *fakeStore, want ...string) {
	t.Helper()
	if got := st.awaitMarkers(t, len(want)); !slices.Equal(got, want) {
		t.Fatalf("markers\n got %v\nwant %v", got, want)
	}
}

// latestStatus is the New Status of the last row recorded for each pane.
func latestStatus(st *fakeStore) map[string]core.Status {
	out := map[string]core.Status{}
	for _, tr := range st.rowList() {
		out[tr.Key().PaneID] = tr.New()
	}
	return out
}

func TestMarkerEventsLost(t *testing.T) {
	// The marker comes before the resync snapshot's rows, and afterwards the
	// record agrees with that snapshot for every Agent.
	h, st := startedRecording(t)
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	s.Panes[1].Agent.Status = core.StatusDone
	h.fake.setSnapshot(s)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost", Message: "fell behind"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync events_lost",
		"transition w1:p1 claude working->idle snapshot", "transition w1:p2 claude idle->done snapshot")
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "events_lost 1 events_lost")...)
	w := st.writeList()
	if !slices.Equal(w[len(w)-3:], []string{"marker events_lost 1", "row w1:p1 1", "row w1:p2 1"}) {
		t.Errorf("writes %v, want the marker before the resync snapshot's rows", w)
	}
	// Consistency (FR-7): the latest row per Agent is the snapshot's Status.
	if got := latestStatus(st); got["w1:p1"] != core.StatusIdle || got["w1:p2"] != core.StatusDone {
		t.Errorf("latest rows %v, want w1:p1 idle and w1:p2 done as the snapshot showed", got)
	}
}

func TestMarkerUnknownErrorCode(t *testing.T) {
	h, st := startedRecording(t)
	h.fake.statusSub("w1:p2").health(&herdr.Health{Kind: herdr.HealthError, Code: "weird_new", Message: "?"})
	h.rec.awaitCount(t, "resync error", 1)
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "error 1 weird_new")...)
}

func TestMarkerClosed(t *testing.T) {
	h, st := startedRecording(t)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthClosed, Err: errors.New("EOF")})
	h.rec.awaitCount(t, "resync closed", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "closed 1 -")...)
	if n := len(st.rowList()); n != 2 {
		t.Errorf("%d rows, want only the two first sights: the snapshot agreed", n)
	}
}

func TestMarkerRejected(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	fake.reject("w1:p2")
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync rejected")
	fake.mu.Lock()
	delete(fake.rejected, "w1:p2")
	fake.mu.Unlock()
	h.fake.awaitStatusSubs("w1:p2", 1)
	// Once w1:p2 is subscribed, the resync that did it ends and no other
	// follows: every resync reported has completed.
	h.settle()
	h.settle()
	ms := st.markerList()
	if len(ms) < 3 || !slices.Equal(ms[:3], append(slices.Clone(startMarkers), "rejected 1 pane_not_found")) {
		t.Fatalf("markers %v, want the start's, then rejected with herdr's code", ms)
	}
	for _, m := range ms[3:] {
		if m != "rejected 1 pane_not_found" {
			t.Fatalf("markers %v, want every resync rejected", ms)
		}
	}
	if n := len(ms) - 2; n != h.rec.count("resync rejected") {
		t.Errorf("%d resync markers, %d resyncs reported", n, h.rec.count("resync rejected"))
	}
}

func TestMarkerStartedWithHerdrDown(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	fake.fail("ping", 2)
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	if n := len(fake.pings()); n != 3 {
		t.Errorf("%d pings, want 3", n)
	}
	expectMarkers(t, st, startMarkers...)
	expectRows(t, st, firstP1, firstP2)
	if w := st.writeList(); !slices.Equal(w, []string{"marker daemon_start 1", "save 1", "marker connect 1", "row w1:p1 1", "row w1:p2 1"}) {
		t.Errorf("writes %v", w)
	}
}

func TestMarkerReconnect(t *testing.T) {
	h, st := startedRecording(t)
	// The same herdr: the epoch is kept and nothing changed, so no row.
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "reconnect 1 epoch kept")...)
	if n := len(st.rowList()); n != 2 {
		t.Errorf("%d rows, want only the first sights", n)
	}
	if as := h.agents(); as[0].Key.Epoch != 1 {
		t.Errorf("epoch %d, want 1", as[0].Key.Epoch)
	}

	// A restarted herdr: the epoch increments, and the marker carries the new
	// one, before the new epoch's rows.
	h.fake.setSnapshot(restarted())
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 2 incremented", 1)
	st.awaitRows(t, 4)
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "reconnect 1 epoch kept", "reconnect 2 epoch incremented")...)
	w := st.writeList()
	if !slices.Equal(w[len(w)-4:], []string{"save 2", "marker reconnect 2", "row w1:p1 2", "row w1:p2 2"}) {
		t.Errorf("writes %v, want the epoch saved and marked before its rows", w)
	}
}

func TestMarkerRestartNewHerdr(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	st.setState(store.ServerState{Epoch: 3, Terminals: map[string]string{"w1:p1": "old-1"}})
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 4 incremented", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	expectMarkers(t, st, "daemon_start 3 -", "connect 4 epoch incremented")
}

func TestMarkerRestartSameHerdr(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	st.setState(store.ServerState{Epoch: 3, Terminals: terminalsOf(twoAgents())})
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	expectMarkers(t, st, "daemon_start 3 -", "connect 3 epoch kept")
}

// setReconcileInterval sets reconcileInterval for one test, restoring it after.
func setReconcileInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := reconcileInterval
	reconcileInterval = d
	t.Cleanup(func() { reconcileInterval = prev })
}

// reconcileTick delivers one reconcile timer tick to the engine, as the
// timer would. The Agents round trip orders the read of conn after the
// engine's last write of it.
func (h *harness) reconcileTick() {
	h.t.Helper()
	h.agents()
	h.engine.inbox <- reconcileDueMsg{conn: h.engine.conn}
}

// startedReconciling is startedRecording with the reconcile timer out of the
// way, so the test delivers each tick itself.
func startedReconciling(t *testing.T) (*harness, *fakeStore) {
	t.Helper()
	setReconcileInterval(t, time.Hour)
	return startedRecording(t)
}

func TestMarkerReconcileAgrees(t *testing.T) {
	h, st := startedReconciling(t)
	for range 2 {
		h.reconcileTick()
		h.fake.awaitSnapshotCall()
		h.settle()
	}
	if got := st.markerList(); !slices.Equal(got, startMarkers) {
		t.Errorf("markers %v, want none after the start's", got)
	}
}

func TestMarkerReconcileDisagrees(t *testing.T) {
	h, st := startedReconciling(t)
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	h.fake.setSnapshot(s)
	h.reconcileTick()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->idle snapshot")
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "reconcile 1 -")...)
	w := st.writeList()
	if !slices.Equal(w[len(w)-2:], []string{"row w1:p1 1", "marker reconcile 1"}) {
		t.Errorf("writes %v, want the snapshot row, then the marker", w)
	}
	// A later reconcile agrees.
	h.reconcileTick()
	h.fake.awaitSnapshotCall()
	h.settle()
	if got := st.markerList(); len(got) != 3 {
		t.Errorf("markers %v, want no more", got)
	}
}

func TestMarkerReconcileRemoval(t *testing.T) {
	// A reconcile read that only removes an Agent disagreed too.
	h, st := startedReconciling(t)
	s := twoAgents()
	s.Panes = s.Panes[1:]
	h.fake.setSnapshot(s)
	h.reconcileTick()
	h.rec.awaitCount(t, "removed w1:p1", 1)
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "reconcile 1 -")...)
}

func TestMarkerReconcileFirstSight(t *testing.T) {
	// A reconcile read whose only difference is a new Agent disagreed.
	h, st := startedReconciling(t)
	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusBlocked))
	h.fake.setSnapshot(s)
	h.reconcileTick()
	h.rec.awaitCount(t, "agent w1:p4 claude blocked", 1)
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "reconcile 1 -")...)
}

func TestMarkerReconcileProgramChanged(t *testing.T) {
	// A reconcile read that only corrects an Agent's program disagreed.
	h, st := startedReconciling(t)
	s := twoAgents()
	s.Panes[0].Agent.AgentProgram = "codex"
	h.fake.setSnapshot(s)
	h.reconcileTick()
	h.fake.awaitSnapshotCall()
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "reconcile 1 -")...)
}

func TestMarkerReconcileHeldProfile(t *testing.T) {
	// An Agent whose Profile resolution is held was seen by the first
	// snapshot; reconcile reads that show it again find no disagreement.
	setReconcileInterval(t, time.Hour)
	setProfileTimeout(t, wait)
	st := newFakeStore(nil)
	fake := newFakeClient(t, oneAgent("claude", core.StatusWorking))
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	env.hold()
	h := startRecording(t, fake, st, func(o *Options, r *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
		r.profileLines = true
	})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	for range 3 {
		h.reconcileTick()
		h.fake.awaitSnapshotCall()
		h.settle()
	}
	if len(h.rec.all()) != 0 {
		t.Fatalf("reported before the Profile resolved: %v", h.rec.all())
	}
	if got := st.markerList(); !slices.Equal(got, startMarkers) {
		t.Errorf("markers %v, want none after the start's", got)
	}
	env.unblock()
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw")
}

func TestMarkerReconcileDuringRead(t *testing.T) {
	// A tick while another read is in flight tags the read after it: the
	// read in flight writes no reconcile marker even though it disagrees,
	// and the tagged one writes one only because it disagrees.
	h, st := startedReconciling(t)
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged, PaneID: "w1:p1"})
	h.fake.awaitSnapshotCall() // held
	h.reconcileTick()
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	h.fake.setSnapshot(s)
	h.fake.release <- struct{}{}
	h.rec.awaitCount(t, "transition w1:p1 claude working->idle snapshot", 1)
	h.fake.awaitSnapshotCall() // the tagged read, held
	if got := st.markerList(); !slices.Equal(got, startMarkers) {
		t.Errorf("markers %v, want none for the untagged read", got)
	}
	s.Panes[1].Agent.Status = core.StatusDone
	h.fake.setSnapshot(s)
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.rec.awaitCount(t, "transition w1:p2 claude idle->done snapshot", 1)
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "reconcile 1 -")...)
	w := st.writeList()
	if !slices.Equal(w[len(w)-2:], []string{"row w1:p2 1", "marker reconcile 1"}) {
		t.Errorf("writes %v", w)
	}
}

func TestMarkerResyncAndReconcileOneRead(t *testing.T) {
	// One read both ends a resync and serves a reconcile: the resync marker,
	// the rows, then the reconcile marker.
	h, st := startedReconciling(t)
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged, PaneID: "w1:p1"})
	h.fake.awaitSnapshotCall() // read N, held
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.awaitCount(t, "resync events_lost", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.settle() // the resubscribe settles: the resync's read is N+1
	h.reconcileTick()
	h.fake.release <- struct{}{} // N agrees
	h.fake.awaitSnapshotCall()   // N+1, held
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	h.fake.setSnapshot(s)
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.rec.awaitCount(t, "transition w1:p1 claude working->idle snapshot", 1)
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "events_lost 1 events_lost", "reconcile 1 -")...)
	w := st.writeList()
	if !slices.Equal(w[len(w)-3:], []string{"marker events_lost 1", "row w1:p1 1", "marker reconcile 1"}) {
		t.Errorf("writes %v, want the resync marker, the row, then the reconcile marker", w)
	}
}

func TestMarkerDisconnectDropsReconcile(t *testing.T) {
	// A reconcile request outstanding when the lifecycle stream is lost is
	// abandoned: the reconnect to a restarted herdr, which disagrees with
	// everything, writes the reconnect marker alone.
	h, st := startedReconciling(t)
	hold := h.fake.holdNext()
	h.reconcileTick()
	h.fake.awaitSnapshotCall() // held
	h.fake.setSnapshot(restarted())
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 2 incremented", 1)
	close(hold) // the stale read is ignored
	st.awaitRows(t, 4)
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "reconnect 2 epoch incremented")...)
}

func TestMarkerOtherReadsWriteNone(t *testing.T) {
	// A read that is not a resync, a connection's first or a reconcile writes
	// no marker, even when it disagrees.
	h, st := startedRecording(t)
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged, PaneID: "w1:p1"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->idle snapshot", 1)
	h.settle()
	if got := st.markerList(); !slices.Equal(got, startMarkers) {
		t.Errorf("markers %v, want none after the start's", got)
	}
}

func TestMarkerReasonsAreHerdrs(t *testing.T) {
	for _, tt := range []struct {
		kind   herdr.HealthKind
		reason string
	}{
		{herdr.HealthEventsLost, store.ReasonEventsLost},
		{herdr.HealthError, store.ReasonError},
		{herdr.HealthRejected, store.ReasonRejected},
		{herdr.HealthClosed, store.ReasonClosed},
		{herdr.HealthUndecodable, store.ReasonUndecodable},
	} {
		if string(tt.kind) != tt.reason {
			t.Errorf("herdr's %q is recorded as %q", tt.kind, tt.reason)
		}
	}
}

func TestMarkerUndecodable(t *testing.T) {
	h, st := startedRecording(t)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthUndecodable, Err: errors.New("bad json")})
	h.rec.awaitCount(t, "resync undecodable", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "undecodable 1 -")...)
}

func TestMarkerJoinedCauses(t *testing.T) {
	// Triggers that join a running resync are each marked when it ends, once
	// per distinct cause; the Sink still sees one resync.
	three := twoAgents()
	three.Panes = append(three.Panes, agentPane("w1:p4", core.StatusBlocked))
	fake := newFakeClient(t, three)
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude blocked")
	for _, id := range []string{"w1:p1", "w1:p2", "w1:p4"} {
		h.fake.awaitSubscribe(id)
	}
	h.settle()
	release := h.fake.holdSubscribes("w1:p1")
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.awaitCount(t, "resync events_lost", 1)
	h.fake.awaitSubscribe("w1:p1") // held: the resync's read waits
	h.fake.statusSub("w1:p2").health(&herdr.Health{Kind: herdr.HealthError, Code: "weird_new"})
	h.fake.awaitSubscribe("w1:p2")
	h.fake.statusSub("w1:p4").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.fake.awaitSubscribe("w1:p4")
	close(release)
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "events_lost 1 events_lost", "error 1 weird_new")...)
	if got := st.markerList(); len(got) != 4 {
		t.Errorf("markers %v, want one per distinct cause", got)
	}
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude blocked", "resync events_lost")
}

func TestMarkerResyncCutShort(t *testing.T) {
	// A resync already reported but cut short by a lost lifecycle stream is
	// still marked, before the reconnect.
	h, st := startedRecording(t)
	release := h.fake.holdSubscribes("w1:p1")
	t.Cleanup(func() { close(release) })
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.awaitCount(t, "resync events_lost", 1)
	h.fake.awaitSubscribe("w1:p1") // held
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	h.settle()
	expectMarkers(t, st, append(slices.Clone(startMarkers), "events_lost 1 events_lost", "reconnect 1 epoch kept")...)
}

func TestMarkerWriteFails(t *testing.T) {
	logs := &logSink{}
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, func(o *Options, _ *recorder) {
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	st.mu.Lock()
	st.markerErr = errors.New("disk full")
	st.mu.Unlock()
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.awaitCount(t, "resync events_lost", 1)
	sub := h.fake.awaitStatusSubs("w1:p1", 2)
	h.settle()
	e := logs.entries(t, "ERROR", "resync marker not recorded")
	if len(e) != 1 || e[0]["reason"] != "events_lost" || e[0]["detail"] != "events_lost" || e[0]["epoch"] != float64(1) || e[0]["error"] != "disk full" {
		t.Errorf("error entries %v, want one naming the reason, detail, epoch and error", e)
	}
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->blocked live", 1)
}
