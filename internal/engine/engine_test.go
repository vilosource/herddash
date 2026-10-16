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
		PaneID: id, WorkspaceID: "w1", TabID: "w1:t1",
		Context: core.PaneContext{ProjectDir: "/src/" + id, WorkspaceLabel: "work", TabLabel: "main", WorkspacePosition: 1, PaneTitle: "title " + id},
		Agent:   &core.AgentObservation{AgentProgram: "claude", Status: status, StateChangeSeq: seq(7)},
	}
}

func shellPane(id string) core.PaneObservation {
	return core.PaneObservation{PaneID: id, WorkspaceID: "w1", TabID: "w1:t1"}
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
	select {
	case err := <-h.done:
		t.Fatalf("engine stopped: %v", err)
	default:
	}
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

func TestLifecycleLost(t *testing.T) {
	h := started(t)
	h.fake.lifecycleSub().health(&herdr.Health{Kind: herdr.HealthClosed, Err: errors.New("EOF")})
	err := h.result()
	var health *herdr.Health
	if err == nil || !errors.As(err, &health) || health.Kind != herdr.HealthClosed || !strings.Contains(err.Error(), "lifecycle") {
		t.Fatalf("Run = %v, want a lifecycle closed error", err)
	}
	for _, id := range []string{"w1:p1", "w1:p2"} {
		if !h.fake.statusSub(id).isClosed() {
			t.Errorf("%s's subscription left open", id)
		}
	}
}

func TestFailedStart(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fakeClient)
		want  string
	}{
		{"ping", func(f *fakeClient) { f.pingErr = errBoom }, "ping herdr"},
		{"lifecycle", func(f *fakeClient) { f.lifeErr = errBoom }, "subscribe to lifecycle events"},
		{"snapshot", func(f *fakeClient) { f.snapErr = errBoom }, "snapshot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeClient(t, twoAgents())
			tt.setup(fake)
			h := startEngine(t, fake)
			err := h.result()
			if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run = %v, want %q wrapping boom", err, tt.want)
			}
		})
	}
}

func TestStopClosesEverything(t *testing.T) {
	h := started(t)
	if err := h.stop(); err != nil {
		t.Fatalf("Run = %v, want nil on a clean stop", err)
	}
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
	select {
	case err := <-h.done:
		t.Fatalf("engine stopped: %v", err)
	default:
	}
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
