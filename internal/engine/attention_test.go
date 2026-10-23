package engine

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/procenv"
	"github.com/vilosource/herddash/internal/store"
)

// stored builds a row a previous run recorded for pane id under epoch 3.
func stored(t *testing.T, id string, prev, next core.Status, at int64, prov core.Provenance) core.Transition {
	t.Helper()
	return storedIn(t, 3, "claude", id, prev, next, at, prov)
}

// storedIn is stored under another epoch and Agent Program.
func storedIn(t *testing.T, epoch uint64, program core.AgentProgram, id string, prev, next core.Status, at int64, prov core.Provenance) core.Transition {
	t.Helper()
	in := core.TransitionInput{Key: core.AgentKey{Host: "mac", Server: "default", Epoch: epoch, PaneID: id},
		AgentProgram: program, Profile: "claude", New: next, HerdrVersion: "0.9.5", ObservedAt: at, Provenance: prov}
	if prev != "" {
		in.Previous = &prev
	}
	tr, err := core.NewTransition(in)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// seed adds rows as if a previous run had appended them.
func (f *fakeStore) seed(ts ...core.Transition) {
	f.mu.Lock()
	f.rows = append(f.rows, ts...)
	f.mu.Unlock()
}

// restartOn starts a recording engine as a Daemon restart on the same herdr:
// epoch 3 saved, with snap's terminals, so the epoch is kept.
func restartOn(t *testing.T, snap core.Snapshot, st *fakeStore, opts func(*Options, *recorder)) *harness {
	t.Helper()
	fake := newFakeClient(t, snap)
	st.client = fake
	st.setState(store.ServerState{Epoch: 3, Terminals: terminalsOf(snap)})
	return startRecording(t, fake, st, opts)
}

func agentByPane(t *testing.T, as []Agent, id string) Agent {
	t.Helper()
	for _, a := range as {
		if a.Key.PaneID == id {
			return a
		}
	}
	t.Fatalf("no Agent in pane %s among %+v", id, as)
	return Agent{}
}

func TestWaitingRestartUnchanged(t *testing.T) {
	// The record says w1:p1 has been blocked since 100, seen live. The Daemon
	// restarts and the snapshot still shows it blocked: no row, and the
	// Waiting Time still runs from 100, exact.
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusWorking, 50, core.ProvenanceSnapshot),
		stored(t, "w1:p1", core.StatusWorking, core.StatusBlocked, 100, core.ProvenanceLive))
	snap := twoAgents()
	snap.Panes[0].Agent.Status = core.StatusBlocked
	h := restartOn(t, snap, st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude blocked", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()

	got := rows(st.rowList())
	if len(got) != 3 || got[2] != strings.Replace(firstP2, "e1", "e3", 1) {
		t.Fatalf("rows %v, want the two stored and only w1:p2's first sight", got)
	}
	as := h.agents()
	p1 := agentByPane(t, as, "w1:p1")
	if p1.Class != ClassNeedsYou || p1.WaitingSince != 100 || !p1.WaitingExact {
		t.Errorf("w1:p1 %s since %d exact %v, want needs_you since 100, exact", p1.Class, p1.WaitingSince, p1.WaitingExact)
	}
	// w1:p2 has no history: idle and never seen working counts as working,
	// since its first sight, a lower bound.
	p2 := agentByPane(t, as, "w1:p2")
	if p2.Class != ClassWorking || p2.WaitingSince != st.rowList()[2].ObservedAt() || p2.WaitingExact {
		t.Errorf("w1:p2 %s since %d exact %v, want working since its first sight, a lower bound", p2.Class, p2.WaitingSince, p2.WaitingExact)
	}

	// The fold carries on from the record: unblocked, the Agent is working
	// from the live event on.
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusWorking, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude blocked->working live", 1)
	tr := h.rec.lastTransition()
	h.rec.mu.Lock()
	a := h.rec.transitionAgents[len(h.rec.transitionAgents)-1]
	h.rec.mu.Unlock()
	if a.Class != ClassWorking || a.WaitingSince != tr.ObservedAt() || !a.WaitingExact {
		t.Errorf("after the event %s since %d exact %v, want working since %d, exact", a.Class, a.WaitingSince, a.WaitingExact, tr.ObservedAt())
	}
}

func TestWaitingRestartChanged(t *testing.T) {
	// Stored blocked; the snapshot after the restart shows working: one row
	// from blocked to working, and the Waiting Time is a lower bound.
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusBlocked, 100, core.ProvenanceLive))
	h := restartOn(t, twoAgents(), st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	all := st.rowList()
	got := rows(all)
	want := `w1:p1 e3 blocked->working snapshot seq=7 v="0.9.5" program="claude" profile="claude"`
	if len(got) != 3 || got[1] != want {
		t.Fatalf("rows %v, want the stored one, then %s", got, want)
	}
	p1 := agentByPane(t, h.agents(), "w1:p1")
	if p1.Class != ClassWorking || p1.WaitingSince != all[1].ObservedAt() || p1.WaitingExact {
		t.Errorf("w1:p1 %s since %d exact %v, want working since the new row, a lower bound", p1.Class, p1.WaitingSince, p1.WaitingExact)
	}
}

func TestWaitingRestartDoneToIdle(t *testing.T) {
	// done becoming idle across a restart is recorded, but stays in the
	// finished class, which began live at 100.
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p2", "", core.StatusWorking, 50, core.ProvenanceLive),
		stored(t, "w1:p2", core.StatusWorking, core.StatusDone, 100, core.ProvenanceLive))
	h := restartOn(t, twoAgents(), st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	got := rows(st.rowList())
	want := `w1:p2 e3 done->idle snapshot seq=7 v="0.9.5" program="claude" profile="claude"`
	if len(got) != 4 || got[3] != want {
		t.Fatalf("rows %v, want %s last", got, want)
	}
	p2 := agentByPane(t, h.agents(), "w1:p2")
	if p2.Class != ClassFinished || p2.WaitingSince != 100 || !p2.WaitingExact {
		t.Errorf("w1:p2 %s since %d exact %v, want finished since 100, exact", p2.Class, p2.WaitingSince, p2.WaitingExact)
	}
}

func TestWaitingHistoryFails(t *testing.T) {
	// A failed read is logged, and the first sight is recorded as new.
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusWorking, 100, core.ProvenanceLive))
	st.historyErr = errors.New("disk on fire")
	logs := &logSink{}
	h := restartOn(t, twoAgents(), st, func(o *Options, _ *recorder) {
		o.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	})
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	got := rows(st.rowList())
	if len(got) != 3 || got[1] != strings.Replace(firstP1, "e1", "e3", 1) {
		t.Fatalf("rows %v, want w1:p1's first sight with no previous Status", got)
	}
	failed := logs.entries(t, "ERROR", "history not read; recording the first sight as new")
	if len(failed) != 2 || failed[0]["error"] != "disk on fire" {
		t.Errorf("logged %v, want one error per first sight, with the reason", failed)
	}
}

func TestWaitingBackfillNotFolded(t *testing.T) {
	// Imported rows are epoch 0, so they never reach a live Agent's fold.
	st := newFakeStore(nil)
	bf, err := core.NewTransition(core.TransitionInput{Key: core.AgentKey{Host: "mac", Server: "default", Epoch: 0, PaneID: "w1:p1"},
		New: core.StatusWorking, ObservedAt: 100, Provenance: core.ProvenanceBackfill, ImportKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	st.seed(bf)
	h := restartOn(t, twoAgents(), st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	got := rows(st.rowList())
	if len(got) != 3 || got[1] != strings.Replace(firstP1, "e1", "e3", 1) {
		t.Fatalf("rows %v, want w1:p1's first sight as new", got)
	}
	if p1 := agentByPane(t, h.agents(), "w1:p1"); p1.WaitingSince == 100 {
		t.Errorf("w1:p1 waits since the backfilled row")
	}
}

func TestWaitingFreshEpochReadsNoHistory(t *testing.T) {
	// A fresh database starts a new epoch: there is nothing to read.
	_, st := startedRecording(t)
	st.mu.Lock()
	n := st.historyReads
	st.mu.Unlock()
	if n != 0 {
		t.Errorf("%d history reads, want none", n)
	}
}

func TestWaitingIncrementedEpochReadsNoHistory(t *testing.T) {
	// A restarted herdr increments the epoch, so its Agents are new.
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusWorking, 100, core.ProvenanceLive))
	fake := newFakeClient(t, twoAgents())
	st.client = fake
	st.setState(store.ServerState{Epoch: 3, Terminals: map[string]string{"w1:p1": "old-1"}})
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 4 incremented", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	st.mu.Lock()
	n := st.historyReads
	st.mu.Unlock()
	if n != 0 {
		t.Errorf("%d history reads, want none", n)
	}
}

func TestWaitingWithoutStore(t *testing.T) {
	// The fold works with no store: first sight from the snapshot, a lower
	// bound; a live change starts an exact Waiting Time.
	h := started(t)
	p1 := agentByPane(t, h.agents(), "w1:p1")
	times := h.rec.observedTimes()
	if p1.Class != ClassWorking || p1.WaitingSince != times[0] || p1.WaitingExact {
		t.Errorf("w1:p1 %s since %d exact %v, want working since its report at %d, a lower bound", p1.Class, p1.WaitingSince, p1.WaitingExact, times[0])
	}
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->blocked live", 1)
	tr := h.rec.lastTransition()
	p1 = agentByPane(t, h.agents(), "w1:p1")
	if p1.Class != ClassNeedsYou || p1.WaitingSince != tr.ObservedAt() || !p1.WaitingExact {
		t.Errorf("w1:p1 %s since %d exact %v, want needs_you since %d, exact", p1.Class, p1.WaitingSince, p1.WaitingExact, tr.ObservedAt())
	}
}

func historyReads(st *fakeStore) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.historyReads
}

// restartedMatching restarts on twoAgents with a record that matches it: both
// first sights are seeded from history and write nothing.
func restartedMatching(t *testing.T) (*harness, *fakeStore) {
	t.Helper()
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusWorking, 100, core.ProvenanceLive),
		stored(t, "w1:p2", "", core.StatusIdle, 100, core.ProvenanceSnapshot))
	h := restartOn(t, twoAgents(), st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	if n := len(st.rowList()); n != 2 || historyReads(st) != 2 {
		t.Fatalf("%d rows, %d history reads; want only the two stored, both read", n, historyReads(st))
	}
	return h, st
}

func TestWaitingRedetectedReadsNoHistory(t *testing.T) {
	// A new Agent in a pane id already in the record, mid-run: no history.
	h, st := restartedMatching(t)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentReleased, PaneID: "w1:p1", FinalStatus: core.StatusWorking})
	h.rec.awaitCount(t, "removed w1:p1", 1)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.rec.awaitCount(t, "agent w1:p1 claude working", 2)
	h.settle()
	got := rows(st.rowList())
	if len(got) != 3 || got[2] != strings.Replace(firstP1, "e1", "e3", 1) {
		t.Fatalf("rows %v, want a fresh first sight for w1:p1", got)
	}
	if n := historyReads(st); n != 2 {
		t.Errorf("%d history reads, want only the start's 2", n)
	}
}

func TestWaitingTerminalReplacedReadsNoHistory(t *testing.T) {
	h, st := restartedMatching(t)
	s := twoAgents()
	s.Panes[0].TerminalID = "other"
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged, PaneID: "w1:p1"})
	h.rec.awaitCount(t, "agent w1:p1 claude working", 2)
	h.settle()
	got := rows(st.rowList())
	if len(got) != 3 || got[2] != strings.Replace(firstP1, "e1", "e3", 1) {
		t.Fatalf("rows %v, want a fresh first sight for w1:p1", got)
	}
	if n := historyReads(st); n != 2 {
		t.Errorf("%d history reads, want only the start's 2", n)
	}
}

func TestWaitingKeptReconnectSeeds(t *testing.T) {
	// A fresh run's epoch 1 is kept by a reconnect: the panes its first
	// snapshot tracks are seeded from the record.
	h, st := startedRecording(t)
	if n := historyReads(st); n != 0 {
		t.Fatalf("%d history reads on a fresh epoch, want none", n)
	}
	st.seed(storedIn(t, 1, "claude", "w1:p4", "", core.StatusBlocked, 100, core.ProvenanceLive))
	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusBlocked))
	h.fake.setSnapshot(s)
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	h.rec.awaitCount(t, "agent w1:p4 claude blocked", 1)
	h.settle()
	if n := len(st.rowList()); n != 3 {
		t.Errorf("%d rows, want the two first sights and the seeded row only", n)
	}
	if n := historyReads(st); n != 1 {
		t.Errorf("%d history reads, want 1", n)
	}
	p4 := agentByPane(t, h.agents(), "w1:p4")
	if p4.Class != ClassNeedsYou || p4.WaitingSince != 100 || !p4.WaitingExact {
		t.Errorf("w1:p4 %s since %d exact %v, want needs_you since 100, exact", p4.Class, p4.WaitingSince, p4.WaitingExact)
	}
}

func TestWaitingProgramDiffers(t *testing.T) {
	// The same Status under another Agent Program is a change: recorded.
	st := newFakeStore(nil)
	st.seed(storedIn(t, 3, "codex", "w1:p1", "", core.StatusWorking, 100, core.ProvenanceLive))
	h := restartOn(t, twoAgents(), st, nil)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	got := rows(st.rowList())
	want := `w1:p1 e3 working->working snapshot seq=7 v="0.9.5" program="claude" profile="claude"`
	if len(got) != 3 || got[1] != want {
		t.Fatalf("rows %v, want %s second", got, want)
	}
}

func TestWaitingRestartChangedLive(t *testing.T) {
	// The first sight comes from a live status event while the Profile
	// resolution is held: the row is live, from the last stored Status.
	setReconcileInterval(t, time.Hour)
	setProfileTimeout(t, wait)
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusBlocked, 100, core.ProvenanceLive))
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	env.hold()
	snap := oneAgent("claude", core.StatusWorking)
	fake := newFakeClient(t, snap)
	fake.setProc("w1:p1", p1PID)
	st.client = fake
	st.setState(store.ServerState{Epoch: 3, Terminals: terminalsOf(snap)})
	h := startRecording(t, fake, st, func(o *Options, _ *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
	})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "claude"})
	h.fake.statusSub("w1:p1").awaitDelivered(t, 1)
	h.agents()
	env.unblock()
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude done")
	got := rows(st.awaitRows(t, 2))
	want := `w1:p1 e3 blocked->done live seq=- v="0.9.5" program="claude" profile="cldw"`
	if len(got) != 2 || got[1] != want {
		t.Fatalf("rows %v, want %s second", got, want)
	}
	if p1 := agentByPane(t, h.agents(), "w1:p1"); p1.Class != ClassFinished || !p1.WaitingExact {
		t.Errorf("w1:p1 %s exact %v, want finished, exact from the live row", p1.Class, p1.WaitingExact)
	}
}

// directEngine is an Engine to drive by hand, outside Run, on fake: its
// goroutines end when the test does.
func directEngine(t *testing.T, fake *fakeClient, st Recorder) *Engine {
	t.Helper()
	o := Options{Host: "mac", Server: "default", Client: fake, Sink: newRecorder(), Session: noSession,
		Children: func(int) ([]procenv.Process, error) { return nil, nil }}
	if st != nil {
		o.Store = st
	}
	e := New(o)
	ctx, cancel := context.WithCancel(context.Background())
	e.runCtx, e.connCtx = ctx, ctx
	t.Cleanup(func() { cancel(); e.wg.Wait() })
	return e
}

func TestWaitingMatchedFirstSightIsNoDisagreement(t *testing.T) {
	// A first sight that matched the record wrote nothing, so the snapshot
	// did not disagree; without a store, what was reported still counts.
	snap := twoAgents()
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusWorking, 100, core.ProvenanceLive),
		stored(t, "w1:p2", "", core.StatusIdle, 100, core.ProvenanceSnapshot))
	e := directEngine(t, newFakeClient(t, snap), st)
	e.epoch, e.historic = 3, true
	if _, changed := e.apply(1, snap, true, nil); changed {
		t.Error("changed, though every first sight matched the record")
	}
	if n := len(st.rowList()); n != 2 {
		t.Errorf("%d rows, want only the two stored", n)
	}
	e = directEngine(t, newFakeClient(t, snap), nil)
	if _, changed := e.apply(1, snap, true, nil); !changed {
		t.Error("not changed without a store, though two Agents were reported first")
	}
}

func TestWaitingHistoryCancelled(t *testing.T) {
	// The read fails because the run is ending: nothing is recorded, so the
	// next start does not take the Agent for new.
	st := newFakeStore(nil)
	st.historyErr = context.Canceled
	e := directEngine(t, newFakeClient(t, twoAgents()), st)
	e.epoch = 3
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.runCtx = ctx
	p := e.track("w1:p1")
	p.seedHistory, p.status, p.program, p.seenBy = true, core.StatusWorking, "claude", core.ProvenanceSnapshot
	if !e.firstSight(p, 100) {
		t.Error("first sight not reported as skipped")
	}
	if n := len(st.rowList()); n != 0 {
		t.Errorf("%d rows, want none", n)
	}
}

// paneRows is every row of pane id in st, in append order.
func paneRows(st *fakeStore, id string) []core.Transition {
	var out []core.Transition
	for _, tr := range st.rowList() {
		if tr.Key().PaneID == id {
			out = append(out, tr)
		}
	}
	return out
}

func TestWaitingNewAgentInReusedPaneAcrossRestart(t *testing.T) {
	// Agent A works and finishes live, then quits; B starts in the same pane
	// and terminal and is idle. B's first row has no previous Status, so after
	// a Daemon restart keeping the epoch B is Idle (working, never worked)
	// since that row, not Finished with A's wait, and nothing is written.
	fake := newFakeClient(t, twoAgents())
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, nil)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->done live", 1)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentReleased, PaneID: "w1:p1", FinalStatus: core.StatusDone})
	h.rec.awaitCount(t, "removed w1:p1", 1)
	snap := twoAgents()
	snap.Panes[0].Agent.Status = core.StatusIdle
	h.fake.setSnapshot(snap)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.rec.awaitCount(t, "agent w1:p1 claude idle", 1)
	h.settle()
	h.stop()

	p1 := paneRows(st, "w1:p1")
	if len(p1) != 3 {
		t.Fatalf("w1:p1 rows %v, want A's two and B's first", rows(p1))
	}
	b := p1[2]
	if _, ok := b.Previous(); ok || b.New() != core.StatusIdle {
		t.Fatalf("B's first row %s, want nil->idle", row(b))
	}
	before := len(st.rowList())

	fake2 := newFakeClient(t, snap)
	st.client = fake2
	h2 := startRecording(t, fake2, st, nil)
	h2.rec.expect(t, "reconnected 0.9.5 epoch 1 kept", "agent w1:p1 claude idle", "agent w1:p2 claude idle")
	h2.settle()
	if n := len(st.rowList()); n != before {
		t.Errorf("rows %v, want nothing written by the restart", rows(st.rowList()[before:]))
	}
	a := agentByPane(t, h2.agents(), "w1:p1")
	if a.Class != ClassWorking || a.WaitingSince != b.ObservedAt() {
		t.Errorf("w1:p1 %s since %d, want working (Idle) since B's first row at %d", a.Class, a.WaitingSince, b.ObservedAt())
	}
}

// terminalChangedRestart restarts on epoch 3 with w1:p1 shown in Status s on
// terminal "other", saved holding the terminals the previous run saw; the
// epoch is kept by the other panes.
func terminalChangedRestart(t *testing.T, st *fakeStore, s core.Status, saved map[string]string, opts func(*Options, *recorder)) *harness {
	t.Helper()
	snap := twoAgents()
	snap.Panes[0].Agent.Status = s
	snap.Panes[0].TerminalID = "other"
	fake := newFakeClient(t, snap)
	st.client = fake
	st.setState(store.ServerState{Epoch: 3, Terminals: saved})
	h := startRecording(t, fake, st, opts)
	h.rec.expect(t, "reconnected 0.9.5 epoch 3 kept", "agent w1:p1 claude "+string(s), "agent w1:p2 claude idle")
	h.settle()
	return h
}

func TestWaitingTerminalChangedWhileDown(t *testing.T) {
	// The pane id came back on another terminal while the Daemon was down,
	// showing the Status A last had: it is a new Agent. Its history is not
	// read, and its first sight is a row with no previous Status.
	st := newFakeStore(nil)
	st.seed(stored(t, "w1:p1", "", core.StatusWorking, 50, core.ProvenanceLive),
		stored(t, "w1:p1", core.StatusWorking, core.StatusDone, 100, core.ProvenanceLive))
	logs := &logSink{}
	h := terminalChangedRestart(t, st, core.StatusDone, terminalsOf(twoAgents()), func(o *Options, _ *recorder) {
		o.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	})
	got := rows(st.rowList())
	want := `w1:p1 e3 nil->done snapshot seq=7 v="0.9.5" program="claude" profile="claude"`
	if len(got) != 4 || got[2] != want {
		t.Fatalf("rows %v, want %s third", got, want)
	}
	if n := historyReads(st); n != 1 {
		t.Errorf("%d history reads, want only w1:p2's", n)
	}
	a := agentByPane(t, h.agents(), "w1:p1")
	if at := st.rowList()[2].ObservedAt(); a.Class != ClassFinished || a.WaitingSince != at {
		t.Errorf("w1:p1 %s since %d, want finished since its first sight at %d", a.Class, a.WaitingSince, at)
	}
	if es := logs.entries(t, "INFO", "terminal changed since the record; not seeding from it"); len(es) != 1 ||
		es[0]["recorded_terminal_id"] != "t-w1:p1" || es[0]["terminal_id"] != "other" {
		t.Errorf("logged %v, want one entry naming both terminals", es)
	}
}

func TestWaitingNoTerminalEvidenceSeeds(t *testing.T) {
	// No saved terminal for the pane, or an empty one, is no evidence of a
	// change: the pane is seeded from the record as before.
	for name, saved := range map[string]map[string]string{
		"absent": {"w1:p2": "t-w1:p2", "w1:p3": "t-w1:p3"},
		"empty":  {"w1:p1": "", "w1:p2": "t-w1:p2", "w1:p3": "t-w1:p3"},
	} {
		t.Run(name, func(t *testing.T) {
			st := newFakeStore(nil)
			st.seed(stored(t, "w1:p1", "", core.StatusWorking, 50, core.ProvenanceLive),
				stored(t, "w1:p1", core.StatusWorking, core.StatusDone, 100, core.ProvenanceLive))
			h := terminalChangedRestart(t, st, core.StatusDone, saved, nil)
			if n := historyReads(st); n != 2 {
				t.Errorf("%d history reads, want both panes'", n)
			}
			if got := rows(st.rowList()); len(got) != 3 {
				t.Errorf("rows %v, want the two stored and w1:p2's first sight", got)
			}
			if a := agentByPane(t, h.agents(), "w1:p1"); a.Class != ClassFinished || a.WaitingSince != 100 {
				t.Errorf("w1:p1 %s since %d, want finished since 100", a.Class, a.WaitingSince)
			}
		})
	}
}

func TestTransitionPreviousFromFold(t *testing.T) {
	// A Transition of an Agent whose Status is not known but whose fold is
	// takes the fold's Status as previous: only an Agent's first row has none.
	st := newFakeStore(nil)
	e := directEngine(t, newFakeClient(t, twoAgents()), st)
	p := e.track("w1:p1")
	p.fold = core.Fold([]core.Transition{stored(t, "w1:p1", "", core.StatusBlocked, 100, core.ProvenanceLive)})
	e.transitionAt(p, core.StatusWorking, core.ProvenanceLive, nil, core.ObservedByHerdr, 200)
	rs := st.rowList()
	if len(rs) != 1 {
		t.Fatalf("%d rows, want 1", len(rs))
	}
	if prev, ok := rs[0].Previous(); !ok || prev != core.StatusBlocked {
		t.Errorf("row %s, want previous blocked", row(rs[0]))
	}
	if !p.fold.EverWorking || p.fold.Since != 200 {
		t.Errorf("fold %+v, want it continued, working since 200", p.fold)
	}
}
