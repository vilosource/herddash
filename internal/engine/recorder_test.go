package engine

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/vilosource/herddash/internal/core"
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
	if w := st.writeList(); !slices.Equal(w[:3], []string{"marker daemon_start 3", "save 4", "row w1:p1 4"}) {
		t.Errorf("writes %v, want epoch 4 saved before its first row", w)
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
	// Each snapshot's state is saved, when it changed, before its rows.
	if w := st.writeList(); !slices.Equal(w, []string{
		"marker daemon_start 1", "save 1", "row w1:p1 1", "row w1:p2 1",
		"marker daemon_start 1", "row w1:p1 1", "row w1:p2 1",
		"marker daemon_start 1", "save 2", "row w1:p1 2", "row w1:p2 2",
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
	// Clock readings: the marker (1), the gate (2), the snapshot showing the
	// Agent (3), then the report.
	if tr.Provenance() != core.ProvenanceSnapshot || !ok || order != 7 || tr.ObservedAt() != clockAt(3) || reported <= tr.ObservedAt() {
		t.Errorf("first sight %s at %d (reported at %d), want snapshot seq 7 at %d", row(tr), tr.ObservedAt(), reported, clockAt(3))
	}
}
