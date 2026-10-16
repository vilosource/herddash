package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
)

func seq(v uint64) *uint64 { return &v }

func agentPane(id string, status core.Status) core.PaneObservation {
	return core.PaneObservation{
		PaneID: id, TerminalID: "t-" + id, WorkspaceID: "w1", TabID: "w1:t1",
		Context: core.PaneContext{ProjectDir: "/src/" + id, WorkspaceLabel: "work", TabLabel: "main", WorkspacePosition: 1, PaneTitle: "title " + id},
		Agent:   &core.AgentObservation{AgentProgram: "claude", Status: status, StateChangeSeq: seq(7)},
	}
}

func shellPane(id string) core.PaneObservation {
	return core.PaneObservation{PaneID: id, TerminalID: "t-" + id, WorkspaceID: "w1", TabID: "w1:t1"}
}

// twoAgents is the matrix's start state: two Agent panes and one shell.
func twoAgents() core.Snapshot {
	return core.Snapshot{HerdrVersion: "0.9.5", Panes: []core.PaneObservation{
		agentPane("w1:p1", core.StatusWorking),
		agentPane("w1:p2", core.StatusIdle),
		shellPane("w1:p3"),
	}}
}

// started runs an engine on twoAgents and waits for it to settle.
func started(t *testing.T) *harness {
	t.Helper()
	h := startEngine(t, newFakeClient(t, twoAgents()))
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	return h
}

func TestStart(t *testing.T) {
	h := started(t)
	calls := h.fake.callList()
	if len(calls) < 5 || calls[0] != "ping" || calls[1] != "lifecycle" || calls[2] != "snapshot" {
		t.Fatalf("calls %v: want ping, lifecycle, then a snapshot first", calls)
	}
	subs := []string{}
	for _, c := range calls[3:] {
		if strings.HasPrefix(c, "status ") {
			subs = append(subs, c)
		}
	}
	slices.Sort(subs)
	if !slices.Equal(subs, []string{"status w1:p1", "status w1:p2"}) {
		t.Errorf("status subscriptions %v, want one per Agent pane and none for the shell", subs)
	}
	// Each subscription is followed by a read that finds nothing new.
	if calls[len(calls)-1] != "snapshot" {
		t.Errorf("calls %v: want a snapshot after the status subscriptions", calls)
	}
	as := h.agents()
	if len(as) != 2 {
		t.Fatalf("agents %+v", as)
	}
	want := core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"}
	if as[0].Key != want || as[0].Context.PaneTitle != "title w1:p1" || as[0].Context.ProjectDir != "/src/w1:p1" {
		t.Errorf("agent %+v", as[0])
	}
}

func TestLiveChange(t *testing.T) {
	h := started(t)
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->blocked live")

	tr := h.rec.lastTransition()
	if tr.Key() != (core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"}) {
		t.Errorf("key %+v", tr.Key())
	}
	if tr.HerdrVersion() != "0.9.5" || tr.Profile() != "" {
		t.Errorf("version %q profile %q", tr.HerdrVersion(), tr.Profile())
	}
	if _, ok := tr.HerdrOrder(); ok {
		t.Error("a live Transition carries a herdr order")
	}

	s := twoAgents()
	s.Panes = s.Panes[1:]
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->blocked live", "removed w1:p1")
	// One clock reading per report, in order: the two Agents, the
	// Transition, the removal.
	want := []int64{clockAt(1), clockAt(2), clockAt(3), clockAt(4)}
	if got := h.rec.observedTimes(); !slices.Equal(got, want) {
		t.Errorf("observed_at %v, want %v", got, want)
	}
}

func TestLiveChangeIsPrompt(t *testing.T) {
	h := started(t)
	begin := time.Now()
	h.fake.statusSub("w1:p2").event(core.StatusEvent{PaneID: "w1:p2", Status: core.StatusWorking, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p2 claude idle->working live")
	if d := time.Since(begin); d > time.Second {
		t.Errorf("live change reported after %v, want within one second", d)
	}
}

func TestRepeatIsSilent(t *testing.T) {
	h := started(t)
	sub := h.fake.statusSub("w1:p1")
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusWorking, AgentProgram: "claude"})
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->idle live")
}

func TestNewAgent(t *testing.T) {
	h := started(t)
	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusWorking))
	h.fake.setSnapshot(s)
	before := len(h.fake.callList())

	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude working")

	after := h.fake.callList()[before:]
	sub, snap := slices.Index(after, "status w1:p4"), slices.Index(after, "snapshot")
	if sub < 0 || snap < 0 || sub > snap {
		t.Errorf("calls after detection %v: want the status subscribe before the snapshot that reads it", after)
	}
}

func TestNewAgentLiveBeforeSnapshot(t *testing.T) {
	// A status event that beats the snapshot reading a new pane reports the
	// Agent from it; the snapshot then agrees and adds nothing.
	h := started(t)
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.fake.awaitSubscribe("w1:p4")
	h.fake.awaitSnapshotCall()
	h.fake.statusSub("w1:p4").event(core.StatusEvent{PaneID: "w1:p4", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude blocked")

	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusBlocked))
	h.fake.setSnapshot(s)
	h.fake.release <- struct{}{}
	h.fake.awaitSnapshotCall() // the event arrived during the read
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude blocked")
}

func TestCloseRace(t *testing.T) {
	h := started(t)
	h.fake.reject("w1:p4")
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.fake.awaitSubscribe("w1:p4")
	h.fake.awaitSnapshotCall() // the rejection reads again
	h.settle()

	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	for _, id := range []string{"w1:p1", "w1:p2"} {
		if h.fake.statusSub(id).isClosed() {
			t.Errorf("%s's subscription was closed by another pane's rejection", id)
		}
	}
	if as := h.agents(); len(as) != 2 {
		t.Errorf("agents %+v", as)
	}
	h.running()
}

func TestRejectedKnownPaneIsResubscribed(t *testing.T) {
	// A rejected subscribe for a pane the snapshot still shows fails only
	// that attempt: the next snapshot subscribes again.
	fake := newFakeClient(t, twoAgents())
	fake.reject("w1:p2")
	h := startEngine(t, fake)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p2")
	fake.mu.Lock()
	delete(fake.rejected, "w1:p2")
	fake.mu.Unlock()
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	h.fake.statusSub("w1:p2").event(core.StatusEvent{PaneID: "w1:p2", Status: core.StatusWorking, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p2 claude idle->working live")
}

func TestRemoval(t *testing.T) {
	tests := []struct {
		name    string
		trigger func(h *harness)
	}{
		{"pane closed", func(h *harness) {
			h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
		}},
		{"pane exited", func(h *harness) {
			h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleExited, PaneID: "w1:p1"})
		}},
		{"agent released", func(h *harness) {
			h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentReleased, PaneID: "w1:p1", AgentProgram: "claude", FinalStatus: core.StatusWorking})
		}},
		{"gone from snapshot", func(h *harness) {
			s := twoAgents()
			s.Panes = s.Panes[1:]
			h.fake.setSnapshot(s)
			h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
		}},
		{"agent gone from snapshot", func(h *harness) {
			s := twoAgents()
			s.Panes[0].Agent = nil
			h.fake.setSnapshot(s)
			h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := started(t)
			sub := h.fake.statusSub("w1:p1")
			// The removed pane is no longer in herdr, so later reads agree.
			s := twoAgents()
			s.Panes = s.Panes[1:]
			h.fake.setSnapshot(s)
			tt.trigger(h)
			h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "removed w1:p1")
			deadline := time.Now().Add(wait)
			for !sub.isClosed() {
				if time.Now().After(deadline) {
					t.Fatal("removed pane's subscription is still open")
				}
				time.Sleep(time.Millisecond)
			}
			if h.fake.statusSub("w1:p2").isClosed() {
				t.Error("another pane's subscription was closed")
			}
			if as := h.agents(); len(as) != 1 || as[0].Key.PaneID != "w1:p2" {
				t.Errorf("agents %+v", as)
			}
		})
	}
}

func TestRemovedDuringReadIsNotRevived(t *testing.T) {
	// A pane closed while a read is in flight is not brought back by that
	// read, which may predate the close.
	h := started(t)
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.fake.awaitSnapshotCall()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "removed w1:p1")
	h.fake.release <- struct{}{} // the stale read still shows w1:p1
	h.fake.awaitSnapshotCall()
	s := twoAgents()
	s.Panes = s.Panes[1:]
	h.fake.setSnapshot(s)
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "removed w1:p1")
}

func TestDisagreement(t *testing.T) {
	h := started(t)
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	s.Panes[0].Agent.StateChangeSeq = seq(12)
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->idle snapshot")
	tr := h.rec.lastTransition()
	if o, ok := tr.HerdrOrder(); !ok || o != 12 {
		t.Errorf("herdr order %d %v, want state_change_seq 12", o, ok)
	}
}

func TestEventDuringRead(t *testing.T) {
	h := started(t)
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.fake.awaitSnapshotCall()
	// Two events during the read: still exactly one more read.
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleCreated, PaneID: "w1:p9"})
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->blocked live")
	h.fake.noSnapshotCall() // no second read while one is in flight

	// The read in flight predates the event and still says working: it is
	// not compared, so no snapshot Transition flaps the Status back.
	h.fake.release <- struct{}{}
	h.fake.awaitSnapshotCall()
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusBlocked
	h.fake.setSnapshot(s)
	h.fake.release <- struct{}{}
	h.fake.noSnapshotCall()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->blocked live")
}

func TestContextChanged(t *testing.T) {
	h := started(t)
	s := twoAgents()
	s.Panes[0].Context.TabLabel = "renamed"
	s.Panes[0].Context.WorkspacePosition = 3
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged}) // tab.renamed
	h.fake.awaitSnapshotCall()
	h.settle()
	as := h.agents()
	if len(as) != 2 || as[0].Context.TabLabel != "renamed" || as[0].Context.WorkspacePosition != 3 {
		t.Errorf("agents %+v", as)
	}
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
}

func TestStatusSubscriptionEnded(t *testing.T) {
	// A status stream that ends fails only that pane: the engine reads again
	// and subscribes again.
	h := started(t)
	old := h.fake.statusSub("w1:p1")
	old.health(&herdr.Health{Kind: herdr.HealthClosed})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	if h.fake.statusSub("w1:p1") == old {
		t.Fatal("not subscribed again")
	}
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->done live")
}

func TestFailedStartIsRetried(t *testing.T) {
	// A failed start at any step is retried until it succeeds, and then the
	// engine starts normally: no reconnect line, epoch 1.
	for _, kind := range []string{"ping", "lifecycle", "snapshot"} {
		t.Run(kind, func(t *testing.T) {
			fake := newFakeClient(t, twoAgents())
			fake.fail(kind, 2)
			h := startEngine(t, fake)
			h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
			h.settle()
			if n := len(fake.pings()); n != 3 {
				t.Errorf("%d pings, want 3 (two failed starts, then one that succeeded)", n)
			}
			for _, ep := range h.rec.lineEpochs() {
				if ep != 1 {
					t.Errorf("epoch %d, want 1", ep)
				}
			}
		})
	}
}

func TestStopClosesEverything(t *testing.T) {
	h := started(t)
	h.stop()
	if !h.fake.lifecycleSub().isClosed() {
		t.Error("lifecycle subscription left open")
	}
	for _, id := range []string{"w1:p1", "w1:p2"} {
		if !h.fake.statusSub(id).isClosed() {
			t.Errorf("%s's subscription left open", id)
		}
	}
	if _, err := h.engine.Agents(context.Background()); !errors.Is(err, ErrStopped) {
		t.Errorf("Agents after stop = %v, want ErrStopped", err)
	}
}

func TestNonStandardStatus(t *testing.T) {
	// herdr's status set may grow. A Status core rejects records nothing, and
	// the next valid change's previous is the last Status reported.
	h := started(t)
	sub := h.fake.statusSub("w1:p1")
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: "brand_new", AgentProgram: "claude"})
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->blocked live")
}

func TestAgentReleasedFinalStatus(t *testing.T) {
	h := started(t)
	s := twoAgents()
	s.Panes = s.Panes[1:]
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentReleased, PaneID: "w1:p1", AgentProgram: "claude", FinalStatus: core.StatusIdle})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->idle live", "removed w1:p1")

	// A final Status equal to the current one adds no Transition.
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentReleased, PaneID: "w1:p2", AgentProgram: "claude", FinalStatus: core.StatusIdle})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->idle live", "removed w1:p1", "removed w1:p2")
}

// confirmSent gives the goroutine that just returned from a fake subscribe
// call time to hand its result to the engine, then waits for the engine to
// have handled it.
func confirmSent(h *harness) {
	time.Sleep(quiet)
	h.agents()
}

func TestRejectedDuringReadIsNotRevived(t *testing.T) {
	h := started(t)
	h.fake.setGated(true)
	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusWorking))
	h.fake.setSnapshot(s) // the read in flight still sees w1:p4
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.fake.awaitSnapshotCall()

	h.fake.reject("w1:p4")
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.fake.awaitSubscribe("w1:p4")
	confirmSent(h)

	h.fake.release <- struct{}{}
	h.fake.awaitSnapshotCall()
	h.fake.setSnapshot(twoAgents()) // closed by now
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	if n := strings.Count(strings.Join(h.fake.callList(), ","), "status w1:p4"); n != 1 {
		t.Errorf("w1:p4 subscribed %d times, want 1", n)
	}
}

func TestDetectedPaneNotReportedByOlderRead(t *testing.T) {
	h := started(t)
	h.fake.setGated(true)
	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusWorking))
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.fake.awaitSnapshotCall() // this read started before w1:p4's subscription

	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.fake.awaitSubscribe("w1:p4")
	confirmSent(h)

	h.fake.release <- struct{}{}
	h.fake.awaitSnapshotCall() // the read after the subscription
	h.agents()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")

	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude working")
}

func TestLaterSnapshotFailureIsRetried(t *testing.T) {
	h := started(t)
	h.fake.setGated(true)
	h.fake.mu.Lock()
	h.fake.snapErr = errBoom
	h.fake.mu.Unlock()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.fake.awaitSnapshotCall()
	h.fake.release <- struct{}{} // fails

	h.fake.awaitSnapshotCall() // the retry
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	h.fake.mu.Lock()
	h.fake.snapErr = nil
	h.fake.snap = s
	h.fake.mu.Unlock()
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p1 claude working->idle snapshot")
	h.running()
}

func TestSnapshotMinInterval(t *testing.T) {
	const interval = 300 * time.Millisecond
	snapshotMinInterval = interval
	t.Cleanup(func() { snapshotMinInterval = 0 }) // runs after the engine stops
	h := started(t)
	h.fake.mu.Lock()
	before := len(h.fake.snapTimes)
	h.fake.mu.Unlock()

	for range 10 {
		h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	}
	time.Sleep(3 * interval)

	h.fake.mu.Lock()
	times := slices.Clone(h.fake.snapTimes)
	h.fake.mu.Unlock()
	if n := len(times) - before; n < 1 || n > 2 {
		t.Errorf("%d reads after a burst of 10 context changes, want 1 or 2", n)
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < interval-20*time.Millisecond {
			t.Errorf("reads %d and %d started %v apart, want at least %v", i-1, i, gap, interval)
		}
	}
}

func TestPaneMoved(t *testing.T) {
	// Today a move is a snapshot: the old id is removed and the new id is a
	// new Agent with its own subscription.
	h := started(t)
	old := h.fake.statusSub("w1:p1")
	s := twoAgents()
	s.Panes[0].PaneID = "w2:p1"
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleMoved, PaneID: "w2:p1", PreviousPaneID: "w1:p1"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w2:p1 claude working", "removed w1:p1")
	h.fake.awaitSubscribe("w2:p1")
	waitClosed(t, old)
}

func TestAgentDetectedOnTrackedPane(t *testing.T) {
	h := started(t)
	before := h.fake.snapshots.Load()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.fake.awaitSnapshotCall()
	h.settle()
	if n := h.fake.snapshots.Load() - before; n != 1 {
		t.Errorf("%d reads followed, want 1", n)
	}
	if n := strings.Count(strings.Join(h.fake.callList(), ","), "status w1:p1"); n != 1 {
		t.Errorf("w1:p1 subscribed %d times, want 1", n)
	}
	if h.fake.statusSub("w1:p1").isClosed() {
		t.Error("existing subscription closed")
	}
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
}

func waitClosed[T any](t *testing.T, s *fakeSub[T]) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !s.isClosed() {
		if time.Now().After(deadline) {
			t.Fatal("subscription still open")
		}
		time.Sleep(time.Millisecond)
	}
}

// setReconnectDelays sets the backoff for one test and restores the TestMain
// values after its engine has stopped.
func setReconnectDelays(t *testing.T, initial, max time.Duration) {
	t.Helper()
	prevInitial, prevMax := reconnectInitialDelay, reconnectMaxDelay
	reconnectInitialDelay, reconnectMaxDelay = initial, max
	t.Cleanup(func() { reconnectInitialDelay, reconnectMaxDelay = prevInitial, prevMax })
}

// setStableAfter sets reconnectStableAfter for one test, restoring it after.
func setStableAfter(t *testing.T, d time.Duration) {
	t.Helper()
	prev := reconnectStableAfter
	reconnectStableAfter = d
	t.Cleanup(func() { reconnectStableAfter = prev })
}

// loseLifecycle ends the current lifecycle stream, as herdr going away does,
// and returns the subscription it ended.
func (h *harness) loseLifecycle() *fakeSub[core.LifecycleEvent] {
	h.t.Helper()
	old := h.fake.lifecycleSub()
	old.health(&herdr.Health{Kind: herdr.HealthClosed, Err: errors.New("EOF")})
	return old
}

// restarted is twoAgents as a new herdr instance shows it: the same pane ids,
// every terminal id new.
func restarted() core.Snapshot {
	s := twoAgents()
	for i := range s.Panes {
		s.Panes[i].TerminalID = "new-" + s.Panes[i].PaneID
	}
	return s
}

func TestReconnectDelay(t *testing.T) {
	setReconnectDelays(t, time.Second, 30*time.Second)
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := reconnectDelay(i + 1); got != w {
			t.Errorf("reconnectDelay(%d) = %v, want %v", i+1, got, w)
		}
	}
	if got := reconnectDelay(1000); got != 30*time.Second {
		t.Errorf("reconnectDelay(1000) = %v, want the cap", got)
	}
}

func TestAbsentAtStart(t *testing.T) {
	const initial = 20 * time.Millisecond
	setReconnectDelays(t, initial, time.Second)
	fake := newFakeClient(t, twoAgents())
	fake.fail("ping", 3)
	h := startEngine(t, fake)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	pings := fake.pings()
	if len(pings) != 4 {
		t.Fatalf("%d pings, want 4", len(pings))
	}
	for i := 1; i < len(pings); i++ {
		want := initial << (i - 1)
		if gap := pings[i].Sub(pings[i-1]); gap < want-5*time.Millisecond {
			t.Errorf("attempt %d followed %v after the last, want at least %v", i+1, gap, want)
		}
	}
}

func TestBackoffCap(t *testing.T) {
	const max = 40 * time.Millisecond
	setReconnectDelays(t, 2*time.Millisecond, max)
	fake := newFakeClient(t, twoAgents())
	fake.pingErr = errBoom
	startEngine(t, fake)
	fake.awaitPings(12) // uncapped, the last wait would be 2ms<<10, about 2s
	pings := fake.pings()
	for i := 1; i < len(pings); i++ {
		if gap := pings[i].Sub(pings[i-1]); gap > max+100*time.Millisecond {
			t.Errorf("attempt %d followed %v after the last, more than the %v cap", i+1, gap, max)
		}
	}
	if gap := pings[len(pings)-1].Sub(pings[len(pings)-2]); gap < max-5*time.Millisecond {
		t.Errorf("the last wait was %v, want the %v cap", gap, max)
	}
}

func TestStopWhileWaiting(t *testing.T) {
	setReconnectDelays(t, time.Hour, time.Hour)
	fake := newFakeClient(t, twoAgents())
	fake.pingErr = errBoom
	h := startEngine(t, fake)
	fake.awaitPings(1)
	time.Sleep(quiet) // into the wait
	begin := time.Now()
	h.stop()
	if d := time.Since(begin); d > time.Second {
		t.Errorf("Run took %v to return", d)
	}
}

func TestReconnectingKeepsAgents(t *testing.T) {
	setReconnectDelays(t, time.Hour, time.Hour)
	fake := newFakeClient(t, twoAgents())
	fake.leaky = true
	h := startEngine(t, fake)
	t.Cleanup(fake.endAll)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	fake.awaitSubscribe("w1:p1")
	fake.awaitSubscribe("w1:p2")
	h.settle()
	p1, p2 := h.fake.statusSub("w1:p1"), h.fake.statusSub("w1:p2")
	old := h.loseLifecycle()
	waitClosed(t, old)
	waitClosed(t, p1)
	waitClosed(t, p2)
	// A status event already on its way from a closed subscription is ignored
	// while reconnecting.
	p1.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "claude"})
	p1.awaitDelivered(t, 1)
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	as := h.agents()
	if len(as) != 2 || as[0].Status != core.StatusWorking || as[1].Status != core.StatusIdle {
		t.Fatalf("agents while reconnecting %+v, want both, as they were", as)
	}
	h.running()
}

func TestStreamLostSameInstance(t *testing.T) {
	h := started(t)
	p1 := h.fake.statusSub("w1:p1")
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept")
	waitClosed(t, p1)
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept")

	// The new subscription is live, under the kept epoch.
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept",
		"transition w1:p1 claude working->blocked live")
	if ep := h.rec.lastTransition().Key().Epoch; ep != 1 {
		t.Errorf("epoch %d, want 1", ep)
	}
	calls := h.fake.callList()
	first := slices.Index(calls, "ping")
	i := -1
	if first >= 0 {
		if j := slices.Index(calls[first+1:], "ping"); j >= 0 {
			i = first + 1 + j
		}
	}
	if i < 0 {
		t.Fatalf("calls %v: no second ping after the loss", calls)
	}
	if after := calls[i:]; len(after) < 3 || after[1] != "lifecycle" || after[2] != "snapshot" {
		t.Errorf("calls after the loss %v, want ping, lifecycle, then a snapshot", after)
	}
}

func TestChangedDuringOutage(t *testing.T) {
	h := started(t)
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	h.fake.setSnapshot(s)
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept",
		"transition w1:p1 claude working->idle snapshot")
	if ep := h.rec.lastTransition().Key().Epoch; ep != 1 {
		t.Errorf("epoch %d, want 1", ep)
	}
}

func TestClosedDuringOutage(t *testing.T) {
	h := started(t)
	s := twoAgents()
	s.Panes = slices.Delete(s.Panes, 1, 2) // w1:p2
	h.fake.setSnapshot(s)
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept",
		"removed w1:p2")
	h.settle()
	if eps := h.rec.lineEpochs(); eps[3] != 1 {
		t.Errorf("removal under epoch %d, want 1", eps[3])
	}
	if as := h.agents(); len(as) != 1 || as[0].Key.PaneID != "w1:p1" || as[0].Key.Epoch != 1 {
		t.Errorf("agents %+v, want w1:p1 under epoch 1", as)
	}
}

func TestRestarted(t *testing.T) {
	h := started(t)
	h.fake.setSnapshot(restarted())
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 2 incremented",
		"removed w1:p1", "removed w1:p2", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	if eps := h.rec.lineEpochs(); !slices.Equal(eps, []uint64{1, 1, 2, 1, 1, 2, 2}) {
		t.Errorf("line epochs %v", eps)
	}
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 2 incremented",
		"removed w1:p1", "removed w1:p2", "agent w1:p1 claude working", "agent w1:p2 claude idle",
		"transition w1:p1 claude working->done live")
	if ep := h.rec.lastTransition().Key().Epoch; ep != 2 {
		t.Errorf("epoch %d, want 2", ep)
	}
}

func TestFreshReissue(t *testing.T) {
	// A fresh server reissues w1:p1 to a different agent: the ids match, the
	// terminal ids do not, so the two are never joined.
	h := started(t)
	h.fake.setSnapshot(core.Snapshot{HerdrVersion: "0.9.5", Panes: []core.PaneObservation{
		{PaneID: "w1:p1", TerminalID: "fresh-1", WorkspaceID: "w1", TabID: "w1:t1",
			Agent: &core.AgentObservation{AgentProgram: "codex", Status: core.StatusBlocked}},
		{PaneID: "w1:p2", TerminalID: "fresh-2", WorkspaceID: "w1", TabID: "w1:t1"},
	}})
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 2 incremented",
		"removed w1:p1", "removed w1:p2", "agent w1:p1 codex blocked")
	h.settle()
	if as := h.agents(); len(as) != 1 || as[0].Key.Epoch != 2 || as[0].AgentProgram != "codex" {
		t.Errorf("agents %+v, want only codex in w1:p1 under epoch 2", as)
	}
	if strings.Contains(strings.Join(h.rec.all(), ","), "transition") {
		t.Error("the new agent was joined to the old one's history")
	}
}

func TestNothingTrackedIncrements(t *testing.T) {
	// With no tracked pane there is no evidence of the same instance.
	h := startEngine(t, newFakeClient(t, core.Snapshot{HerdrVersion: "0.9.5"}))
	h.settle()
	h.fake.setSnapshot(twoAgents())
	h.loseLifecycle()
	h.rec.expect(t, "reconnected 0.9.5 epoch 2 incremented", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	if eps := h.rec.lineEpochs(); !slices.Equal(eps, []uint64{2, 2, 2}) {
		t.Errorf("line epochs %v", eps)
	}
}

func TestReconnectAgainKeepsCounting(t *testing.T) {
	// Two restarts in a row: epoch 3; a following same-instance reconnect keeps it.
	h := started(t)
	lines := []string{"agent w1:p1 claude working", "agent w1:p2 claude idle"}
	restart := []string{"removed w1:p1", "removed w1:p2", "agent w1:p1 claude working", "agent w1:p2 claude idle"}

	h.fake.setSnapshot(restarted())
	h.loseLifecycle()
	lines = append(append(lines, "reconnected 0.9.5 epoch 2 incremented"), restart...)
	h.rec.expect(t, lines...)
	h.settle()

	h.fake.setSnapshot(twoAgents())
	h.loseLifecycle()
	lines = append(append(lines, "reconnected 0.9.5 epoch 3 incremented"), restart...)
	h.rec.expect(t, lines...)
	h.settle()

	h.loseLifecycle()
	lines = append(lines, "reconnected 0.9.5 epoch 3 kept")
	h.rec.expect(t, lines...)
}

func TestTerminalReplacedInKeptEpoch(t *testing.T) {
	// w1:p1 keeps its terminal id, so the epoch is kept; w1:p2 keeps its pane
	// id on a new terminal, so it is another Agent: removed, then new.
	h := started(t)
	old2 := h.fake.statusSub("w1:p2")
	s := twoAgents()
	s.Panes[1].TerminalID = "new-w1:p2"
	h.fake.setSnapshot(s)
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept",
		"removed w1:p2", "agent w1:p2 claude idle")
	if eps := h.rec.lineEpochs(); !slices.Equal(eps, []uint64{1, 1, 1, 1, 1}) {
		t.Errorf("line epochs %v", eps)
	}
	waitClosed(t, old2)
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	if h.fake.statusSub("w1:p2") == old2 {
		t.Error("the new Agent was not subscribed")
	}
	if strings.Contains(strings.Join(h.rec.all(), ","), "transition") {
		t.Error("the new terminal's Agent was joined to the old one")
	}
}

func TestShellKeepsEpoch(t *testing.T) {
	// Every Agent pane closed during the outage, but a shell pane kept its
	// terminal id: the same instance, so the epoch is kept.
	h := started(t)
	h.fake.setSnapshot(core.Snapshot{HerdrVersion: "0.9.5", Panes: []core.PaneObservation{shellPane("w1:p3")}})
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept",
		"removed w1:p1", "removed w1:p2")
	if eps := h.rec.lineEpochs(); !slices.Equal(eps, []uint64{1, 1, 1, 1, 1}) {
		t.Errorf("line epochs %v", eps)
	}
}

func TestEmptyTerminalIDNeverKeeps(t *testing.T) {
	blank := func() core.Snapshot {
		s := twoAgents()
		for i := range s.Panes {
			s.Panes[i].TerminalID = ""
		}
		return s
	}
	h := startEngine(t, newFakeClient(t, blank()))
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.settle()
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 2 incremented",
		"removed w1:p1", "removed w1:p2", "agent w1:p1 claude working", "agent w1:p2 claude idle")
}

func TestDetectedPaneAcrossReconnect(t *testing.T) {
	// w1:p4 is detected and its subscription is still pending when herdr goes
	// away; it was never reported. The reconnect snapshot shows it: it is
	// reported then, and subscribed.
	h := started(t)
	setReconnectDelays(t, 50*time.Millisecond, 50*time.Millisecond)
	hold := make(chan struct{})
	h.fake.mu.Lock()
	h.fake.holdSub = map[string]chan struct{}{"w1:p4": hold}
	h.fake.mu.Unlock()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.fake.awaitSubscribe("w1:p4")
	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusBlocked))
	h.fake.setSnapshot(s)
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept",
		"agent w1:p4 claude blocked")
	h.fake.awaitSubscribe("w1:p4")
	h.settle()
	sub := h.fake.statusSub("w1:p4")
	close(hold) // the pre-reconnect subscribe returns late, and is closed
	deadline := time.Now().Add(wait)
	for h.fake.statusSub("w1:p4") == sub {
		if time.Now().After(deadline) {
			t.Fatal("the held subscribe never returned")
		}
		time.Sleep(time.Millisecond)
	}
	waitClosed(t, h.fake.statusSub("w1:p4"))
	h.settle()
	if sub.isClosed() {
		t.Fatal("w1:p4's subscription after the reconnect is closed")
	}
	sub.event(core.StatusEvent{PaneID: "w1:p4", Status: core.StatusDone, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept",
		"agent w1:p4 claude blocked", "transition w1:p4 claude blocked->done live")
}

func TestLossesRightAfterStartBackOff(t *testing.T) {
	const initial = 20 * time.Millisecond
	setReconnectDelays(t, initial, time.Second)
	setStableAfter(t, time.Hour)
	h := started(t)
	for i := 1; i <= 3; i++ {
		h.loseLifecycle()
		want := i + 1
		deadline := time.Now().Add(wait)
		for strings.Count(strings.Join(h.rec.all(), "\n"), "reconnected") < i {
			if time.Now().After(deadline) {
				t.Fatalf("reconnect %d did not happen", i)
			}
			time.Sleep(time.Millisecond)
		}
		h.fake.awaitPings(want)
	}
	checkGrowingWaits(t, h.fake.pings(), initial)
}

func TestHealthBeforeFirstSnapshotBacksOff(t *testing.T) {
	const initial = 20 * time.Millisecond
	setReconnectDelays(t, initial, time.Second)
	fake := newFakeClient(t, twoAgents())
	fake.setGated(true) // no start ever reaches its first snapshot
	h := startEngine(t, fake)
	for i := 1; i <= 4; i++ {
		fake.awaitPings(i)
		fake.awaitSnapshotCall()
		h.loseLifecycle()
	}
	fake.awaitPings(5)
	checkGrowingWaits(t, fake.pings(), initial)
}

// checkGrowingWaits checks that each wait between pings is at least the
// doubling backoff from initial.
func checkGrowingWaits(t *testing.T, pings []time.Time, initial time.Duration) {
	t.Helper()
	for i := 1; i < len(pings); i++ {
		want := initial << (i - 1)
		if gap := pings[i].Sub(pings[i-1]); gap < want-5*time.Millisecond {
			t.Errorf("attempt %d followed %v after the last, want at least %v", i+1, gap, want)
		}
	}
}

func TestStaleMessages(t *testing.T) {
	// Messages from before a reconnect, on subscriptions and reads already
	// closed or abandoned, are ignored.
	fake := newFakeClient(t, twoAgents())
	fake.leaky = true
	h := startEngine(t, fake)
	t.Cleanup(fake.endAll) // before the engine is stopped, which waits for its readers
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	fake.awaitSubscribe("w1:p1")
	fake.awaitSubscribe("w1:p2")
	h.settle()
	oldStatus := fake.statusSub("w1:p1")

	// A read in flight when herdr goes away.
	hold := fake.holdNext()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	fake.awaitSnapshotCall()
	oldLife := h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept")
	fake.awaitSubscribe("w1:p1")
	fake.awaitSubscribe("w1:p2")
	h.settle()

	// The old read finishes, now reporting a change; the old status
	// subscription reports another; the old lifecycle stream reports a close.
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusBlocked
	fake.setSnapshot(s)
	close(hold)
	oldStatus.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "claude"})
	oldStatus.awaitDelivered(t, 1)
	oldLife.event(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p2"})
	oldLife.awaitDelivered(t, 3) // the context change, the health signal, then the close
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept")
	if as := h.agents(); len(as) != 2 || as[0].Status != core.StatusWorking {
		t.Errorf("agents %+v, want both unchanged", as)
	}
}
