package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
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
	if tr.HerdrVersion() != "0.9.5" || tr.Profile() != "claude" {
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
	// One clock reading per report, in order: the gate (not a line here), the
	// first snapshot's last_snapshot_at, the two Agents; then the reads that
	// follow the status subscriptions, each one reading; then the Transition
	// and the removal, one after the other.
	got := h.rec.observedTimes()
	if len(got) != 4 || got[0] != clockAt(3) || got[1] != clockAt(4) || got[2] <= got[1] || got[3] != got[2]+1000 {
		t.Errorf("observed_at %v, want %d, %d, then two consecutive readings", got, clockAt(3), clockAt(4))
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
	h.fake.awaitSnapshotCall() // the rejection resyncs, which reads again
	h.settle()

	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync rejected")
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
	// that attempt: a resync subscribes again.
	fake := newFakeClient(t, twoAgents())
	fake.reject("w1:p2")
	h := startEngine(t, fake)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync rejected")
	fake.mu.Lock()
	delete(fake.rejected, "w1:p2")
	fake.mu.Unlock()
	sub := h.fake.awaitStatusSubs("w1:p2", 1)
	h.settle()
	sub.event(core.StatusEvent{PaneID: "w1:p2", Status: core.StatusWorking, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p2 claude idle->working live", 1)
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
	// A status stream that ends fails only that pane: the engine resyncs,
	// which subscribes again and reads.
	h := started(t)
	old := h.fake.statusSub("w1:p1")
	old.health(&herdr.Health{Kind: herdr.HealthClosed})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	if h.fake.statusSub("w1:p1") == old {
		t.Fatal("not subscribed again")
	}
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync closed", "transition w1:p1 claude working->done live")
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
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync rejected")
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

// setResyncMinInterval sets resyncMinInterval for one test, restoring it after
// the engine has stopped.
func setResyncMinInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := resyncMinInterval
	resyncMinInterval = d
	t.Cleanup(func() { resyncMinInterval = prev })
}

// callsSince returns the fake's calls from index from on.
func callsSince(h *harness, from int) []string { return h.fake.callList()[from:] }

func TestResyncEventsLost(t *testing.T) {
	h := started(t)
	old := h.fake.statusSub("w1:p1")
	before := len(h.fake.callList())
	old.health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost", Message: "fell behind"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync events_lost")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync events_lost")
	after := callsSince(h, before)
	if !slices.Equal(after, []string{"status w1:p1", "snapshot"}) {
		t.Errorf("calls after the signal %v, want the resubscribe, then one snapshot", after)
	}
	if h.fake.statusSub("w1:p1") == old {
		t.Error("w1:p1 not subscribed again")
	}
	if h.fake.statusSub("w1:p2").isClosed() {
		t.Error("another pane's subscription was closed")
	}
}

func TestResyncDisagreement(t *testing.T) {
	h := started(t)
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusIdle
	s.Panes[0].Agent.StateChangeSeq = seq(20)
	h.fake.setSnapshot(s)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync events_lost",
		"transition w1:p1 claude working->idle snapshot")
	if o, ok := h.rec.lastTransition().HerdrOrder(); !ok || o != 20 {
		t.Errorf("herdr order %d %v, want 20", o, ok)
	}
}

func TestResyncManyAtOnce(t *testing.T) {
	three := twoAgents()
	three.Panes = append(three.Panes, agentPane("w1:p4", core.StatusBlocked))
	h := startEngine(t, newFakeClient(t, three))
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude blocked")
	for _, id := range []string{"w1:p1", "w1:p2", "w1:p4"} {
		h.fake.awaitSubscribe(id)
	}
	h.settle()
	subs := []*fakeSub[core.StatusEvent]{h.fake.statusSub("w1:p1"), h.fake.statusSub("w1:p2"), h.fake.statusSub("w1:p4")}
	// The resubscribes are held until every drop has arrived, as they would
	// be when one condition drops every connection at once.
	release := h.fake.holdSubscribes("w1:p1", "w1:p2", "w1:p4")
	before := h.fake.snapshots.Load()
	for _, sub := range subs {
		sub.health(&herdr.Health{Kind: herdr.HealthClosed, Err: errors.New("EOF")})
	}
	for _, id := range []string{"w1:p1", "w1:p2", "w1:p4"} {
		h.fake.awaitSubscribe(id)
	}
	close(release)
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude blocked", "resync closed")
	if n := h.fake.snapshots.Load() - before; n != 1 {
		t.Errorf("%d snapshot reads, want 1", n)
	}
	for _, id := range []string{"w1:p1", "w1:p2", "w1:p4"} {
		if n := len(h.fake.subscribeTimes(id)); n != 2 {
			t.Errorf("%s subscribed %d times, want 2", id, n)
		}
	}
}

func TestResyncUnknownErrorCode(t *testing.T) {
	h := started(t)
	h.fake.statusSub("w1:p2").health(&herdr.Health{Kind: herdr.HealthError, Code: "weird_new", Message: "?"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync error")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	h.running()
	if as := h.agents(); len(as) != 2 {
		t.Errorf("agents %+v", as)
	}
}

func TestResyncTriggerDuringResync(t *testing.T) {
	const interval = 150 * time.Millisecond
	setResyncMinInterval(t, interval)
	h := started(t)
	h.fake.setGated(true)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthClosed})
	h.rec.awaitCount(t, "resync closed", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSnapshotCall() // the resync's read, held

	// A second signal while the resync's read is in flight.
	h.fake.statusSub("w1:p2").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.agents()
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.rec.awaitCount(t, "resync events_lost", 1)
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	time.Sleep(2 * interval)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync closed", "resync events_lost")
	times := h.rec.resyncs()
	if gap := times[1].Sub(times[0]); gap < interval {
		t.Errorf("the second resync began %v after the first, want at least %v", gap, interval)
	}
}

func TestResyncMinInterval(t *testing.T) {
	const interval = 200 * time.Millisecond
	setResyncMinInterval(t, interval)
	h := started(t)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthClosed})
	h.fake.awaitSnapshotCall() // the first resync's read has started: the next signal cannot join it
	h.fake.statusSub("w1:p2").health(&herdr.Health{Kind: herdr.HealthClosed})
	h.rec.awaitCount(t, "resync closed", 2)
	h.settle()
	time.Sleep(interval)
	if n := h.rec.count("resync closed"); n != 2 {
		t.Errorf("%d resyncs, want 2", n)
	}
	times := h.rec.resyncs()
	if gap := times[1].Sub(times[0]); gap < interval {
		t.Errorf("the second resync began %v after the first, want at least %v", gap, interval)
	}
	h.fake.awaitStatusSubs("w1:p1", 2)
	h.fake.awaitStatusSubs("w1:p2", 2)
}

func TestResyncPersistentRejection(t *testing.T) {
	// herdr lists w1:p2 but rejects every subscribe to it: it is subscribed
	// again about once per minimum interval, never immediately. Once a
	// resync's snapshot no longer shows it, it is removed and resyncs stop.
	const interval = 100 * time.Millisecond
	setResyncMinInterval(t, interval)
	fake := newFakeClient(t, twoAgents())
	fake.reject("w1:p2")
	h := startEngine(t, fake)
	time.Sleep(10 * interval)
	times := fake.subscribeTimes("w1:p2")
	// At most one per interval, plus the first; at least a few, however slow
	// the runner.
	if len(times) < 2 || len(times) > 12 {
		t.Errorf("w1:p2 subscribed %d times in %v, want about one per %v", len(times), 10*interval, interval)
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < interval/2 {
			t.Errorf("subscribes %d and %d to w1:p2 %v apart, want about %v", i-1, i, gap, interval)
		}
	}
	if h.rec.count("resync rejected") < 2 {
		t.Errorf("lines %v, want a resync per attempt", h.rec.all())
	}
	if h.fake.statusSub("w1:p1").isClosed() {
		t.Error("w1:p1's subscription was closed")
	}

	// Pane gone.
	s := twoAgents()
	s.Panes = slices.Delete(s.Panes, 1, 2)
	h.fake.setSnapshot(s)
	h.rec.awaitCount(t, "removed w1:p2", 1)
	h.settle()
	resyncs, subs := h.rec.count("resync rejected"), len(fake.subscribeTimes("w1:p2"))
	time.Sleep(3 * interval)
	if n := h.rec.count("resync rejected"); n != resyncs {
		t.Errorf("%d resyncs after the pane was removed", n-resyncs)
	}
	if n := len(fake.subscribeTimes("w1:p2")); n != subs {
		t.Errorf("%d subscribes to w1:p2 after it was removed", n-subs)
	}
	if lines := h.rec.all(); lines[len(lines)-1] != "removed w1:p2" {
		t.Errorf("lines end %v, want the removal last", lines[max(0, len(lines)-3):])
	}
}

func TestResyncPaneClosedWhileResubscribing(t *testing.T) {
	// The pane a resync waits on closes: the resync reads at once and ends.
	h := started(t)
	hold := h.fake.holdSubscribes("w1:p1")
	t.Cleanup(func() { close(hold) })
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthClosed})
	h.rec.awaitCount(t, "resync closed", 1)
	h.fake.awaitSubscribe("w1:p1") // held
	s := twoAgents()
	s.Panes = s.Panes[1:]
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
	h.fake.awaitSnapshotCall() // the resync's read, without waiting for reconcile
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync closed", "removed w1:p1")
	// The resync ended: the next signal starts another rather than joining it.
	h.fake.statusSub("w1:p2").health(&herdr.Health{Kind: herdr.HealthClosed})
	h.rec.awaitCount(t, "resync closed", 2)
}

func TestResyncEventsMidRead(t *testing.T) {
	h := started(t)
	h.fake.setGated(true)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.rec.awaitCount(t, "resync events_lost", 1)
	sub := h.fake.awaitStatusSubs("w1:p1", 2)
	h.fake.awaitSnapshotCall() // the resync's read, held; it still says working

	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync events_lost",
		"transition w1:p1 claude working->blocked live")
	h.fake.noSnapshotCall()

	h.fake.release <- struct{}{} // the stale read is not compared
	h.fake.awaitSnapshotCall()   // another read follows
	s := twoAgents()
	s.Panes[0].Agent.Status = core.StatusBlocked
	h.fake.setSnapshot(s)
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync events_lost",
		"transition w1:p1 claude working->blocked live")
}

func TestReconcile(t *testing.T) {
	const interval = 300 * time.Millisecond
	prev := reconcileInterval
	reconcileInterval = interval
	t.Cleanup(func() { reconcileInterval = prev })
	h := started(t)
	s := twoAgents()
	s.Panes[1].Agent.Status = core.StatusDone
	h.fake.setSnapshot(s)
	h.fake.awaitSnapshotCall()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p2 claude idle->done snapshot")
	h.fake.awaitSnapshotCall() // and again, every interval
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "transition w1:p2 claude idle->done snapshot")

	// On the connection after a reconnect too.
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	s.Panes[0].Agent.Status = core.StatusIdle
	h.fake.setSnapshot(s)
	h.rec.awaitCount(t, "transition w1:p1 claude working->idle snapshot", 1)
}

func TestSubscribeCause(t *testing.T) {
	tests := []struct {
		err  error
		want cause
	}{
		{fmt.Errorf("herdr: subscribe: %w", &herdr.Health{Kind: herdr.HealthRejected, Code: "pane_not_found"}), cause{"rejected", "pane_not_found"}},
		{fmt.Errorf("herdr: subscribe: %w", &herdr.Health{Kind: herdr.HealthUndecodable}), cause{reason: "undecodable"}},
		{errBoom, cause{reason: "closed"}},
		{fmt.Errorf("herdr: subscribe: %w", context.DeadlineExceeded), cause{reason: "closed"}},
	}
	for _, tt := range tests {
		if got := subscribeCause(tt.err); got != tt.want {
			t.Errorf("subscribeCause(%v) = %+v, want %+v", tt.err, got, tt.want)
		}
	}
}

func TestHealthCause(t *testing.T) {
	// herdr's code is the detail only for the kinds that carry one.
	tests := []struct {
		h    herdr.Health
		want cause
	}{
		{herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"}, cause{"events_lost", "events_lost"}},
		{herdr.Health{Kind: herdr.HealthError, Code: "weird_new"}, cause{"error", "weird_new"}},
		{herdr.Health{Kind: herdr.HealthRejected, Code: "pane_not_found"}, cause{"rejected", "pane_not_found"}},
		{herdr.Health{Kind: herdr.HealthClosed, Code: "ignored"}, cause{reason: "closed"}},
		{herdr.Health{Kind: herdr.HealthUndecodable}, cause{reason: "undecodable"}},
	}
	for _, tt := range tests {
		if got := healthCause(&tt.h); got != tt.want {
			t.Errorf("healthCause(%+v) = %+v, want %+v", tt.h, got, tt.want)
		}
	}
}

func TestResyncTerminalReplaced(t *testing.T) {
	// A resync snapshot showing a pane on another terminal reports another
	// Agent, without deciding the epoch again.
	h := started(t)
	s := twoAgents()
	s.Panes[0].TerminalID = "new-w1:p1"
	h.fake.setSnapshot(s)
	h.fake.statusSub("w1:p2").health(&herdr.Health{Kind: herdr.HealthClosed})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync closed",
		"removed w1:p1", "agent w1:p1 claude working")
	if eps := h.rec.lineEpochs(); eps[len(eps)-1] != 1 {
		t.Errorf("epoch %d, want 1", eps[len(eps)-1])
	}
}

func TestReconnectMidResync(t *testing.T) {
	const interval = 500 * time.Millisecond
	setResyncMinInterval(t, interval)
	h := started(t)
	// A resync waiting on its resubscribe when herdr goes away.
	hold := h.fake.holdSubscribes("w1:p1")
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthClosed})
	h.rec.awaitCount(t, "resync closed", 1)
	h.fake.awaitSubscribe("w1:p1")
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync closed", "reconnected 0.9.5 epoch 1 kept")
	h.fake.awaitSubscribe("w1:p1") // the reconnect resubscribes
	h.fake.awaitSubscribe("w1:p2")
	close(hold) // the abandoned resync's subscribe returns late, and is ignored
	h.settle()
	time.Sleep(interval)
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "resync closed", "reconnected 0.9.5 epoch 1 kept")
	if as := h.agents(); len(as) != 2 {
		t.Errorf("agents %+v", as)
	}

	// A resync scheduled when herdr goes away is abandoned too.
	h.fake.statusSub("w1:p2").health(&herdr.Health{Kind: herdr.HealthClosed})
	h.rec.awaitCount(t, "resync closed", 2)
	sub := h.fake.awaitStatusSubs("w1:p2", 3)
	h.settle() // well inside the interval
	sub.health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	h.agents()
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 2)
	time.Sleep(2 * interval)
	if n := h.rec.count("resync events_lost"); n != 0 {
		t.Errorf("%d scheduled resyncs ran after the reconnect, want 0", n)
	}
}

// logSink collects an engine's JSON log, debug included.
type logSink struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// entries returns every log entry at level whose message contains msg.
func (l *logSink) entries(t *testing.T, level, msg string) []map[string]any {
	t.Helper()
	l.mu.Lock()
	text := l.b.String()
	l.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if m, _ := e["msg"].(string); e["level"] == level && strings.Contains(m, msg) {
			out = append(out, e)
		}
	}
	return out
}

// startGated starts an engine on twoAgents whose pings answer versions, with
// the dogfooding override set to allow, gate reports among the lines and the
// log collected.
func startGated(t *testing.T, allow bool, versions ...string) (*harness, *logSink) {
	t.Helper()
	fake := newFakeClient(t, twoAgents())
	fake.setVersions(versions...)
	logs := &logSink{}
	h := startEngineWith(t, fake, func(o *Options, r *recorder) {
		o.AllowUnsupportedHerdr = allow
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		r.gateLines = true
	})
	return h, logs
}

// setRecheckInterval sets refusedRecheckInterval for one test, restoring it after.
func setRecheckInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := refusedRecheckInterval
	refusedRecheckInterval = d
	t.Cleanup(func() { refusedRecheckInterval = prev })
}

// onlyPings fails the test unless every herdr call so far is a ping.
func onlyPings(t *testing.T, calls []string) {
	t.Helper()
	for _, c := range calls {
		if c != "ping" {
			t.Fatalf("calls %v: a refused herdr got more than pings", calls)
		}
	}
}

const (
	refusedMsg  = "refusing to observe it"
	advisoryMsg = "observing it because allow_unsupported_herdr is set"
	resyncMsg   = "the resync guarantee does not hold below 0.9.0"
)

func TestGateSupported(t *testing.T) {
	for _, v := range []string{"0.9.5", "0.10.0", "1.0.0", "v0.9.6", "0.9.5-rc1"} {
		t.Run(v, func(t *testing.T) {
			h, logs := startGated(t, false, v)
			h.rec.expect(t, "gate ok "+v, "agent w1:p1 claude working", "agent w1:p2 claude idle")
			sub := h.fake.awaitStatusSubs("w1:p1", 1)
			h.settle()
			sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
			h.rec.awaitCount(t, "transition w1:p1 claude working->blocked live", 1)
			if got := h.rec.lastTransition().HerdrVersion(); got != v {
				t.Errorf("Transition herdr version %q, want %q", got, v)
			}
			if n := len(logs.entries(t, "WARN", "supported minimum")); n != 0 {
				t.Errorf("%d version warnings for a supported herdr", n)
			}
		})
	}
}

func TestGateRefused(t *testing.T) {
	h, logs := startGated(t, false, "0.9.4")
	h.fake.awaitPings(3)
	h.rec.expect(t, "gate refused 0.9.4")
	onlyPings(t, h.fake.callList())
	if as := h.agents(); len(as) != 0 {
		t.Errorf("agents %+v from a refused herdr", as)
	}
	warns := logs.entries(t, "WARN", refusedMsg)
	if len(warns) != 1 {
		t.Fatalf("%d refusal warnings, want 1", len(warns))
	}
	if warns[0]["herdr_version"] != "0.9.4" || warns[0]["herdr_minimum"] != "0.9.5" {
		t.Errorf("refusal warning %v does not name 0.9.4 and 0.9.5", warns[0])
	}
}

func TestGateRefusedThenUpgraded(t *testing.T) {
	setReconnectDelays(t, time.Hour, time.Hour) // a backoff would never retry
	setRecheckInterval(t, 20*time.Millisecond)
	h, _ := startGated(t, false, "0.8.2", "0.8.2", "0.8.2", "0.9.5")
	h.rec.expect(t, "gate refused 0.8.2", "gate ok 0.9.5", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	calls := h.fake.callList()
	onlyPings(t, calls[:3])
	if calls[3] != "ping" || calls[4] != "lifecycle" || calls[5] != "snapshot" {
		t.Errorf("calls %v: want three refused pings, then a normal start", calls)
	}
	pings := h.fake.pings()
	for i := 1; i < 4; i++ {
		if gap := pings[i].Sub(pings[i-1]); gap < 15*time.Millisecond {
			t.Errorf("recheck %d followed %v after the last, want the 20ms interval", i, gap)
		}
	}
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->blocked live", 1)
}

func TestGateRefusedRechecks(t *testing.T) {
	setReconnectDelays(t, time.Hour, time.Hour)
	setRecheckInterval(t, 20*time.Millisecond)
	h, logs := startGated(t, false, "0.8.2")
	h.fake.awaitPings(8)
	h.stop()
	if got := h.rec.all(); !slices.Equal(got, []string{"gate refused 0.8.2"}) {
		t.Errorf("lines %v, want one gate line", got)
	}
	onlyPings(t, h.fake.callList())
	pings := h.fake.pings()
	for i := 1; i < len(pings); i++ {
		gap := pings[i].Sub(pings[i-1])
		if gap < 15*time.Millisecond || gap > 5*time.Second {
			t.Errorf("recheck %d followed %v after the last, want the 20ms interval", i, gap)
		}
	}
	if h.engine.failures != 0 {
		t.Errorf("failures %d, want refusals kept out of the backoff", h.engine.failures)
	}
	if n := len(logs.entries(t, "WARN", refusedMsg)); n != 1 {
		t.Errorf("%d refusal warnings, want 1", n)
	}
	if n := len(logs.entries(t, "DEBUG", "herdr still refused")); n < len(pings)-2 {
		t.Errorf("%d recheck debug entries for %d pings", n, len(pings))
	}
}

func TestGateAdvisory(t *testing.T) {
	h, logs := startGated(t, true, "0.8.2")
	h.rec.expect(t, "gate advisory 0.8.2", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->blocked live", 1)
	if got := h.rec.lastTransition().HerdrVersion(); got != "0.8.2" {
		t.Errorf("Transition herdr version %q, want 0.8.2", got)
	}
	warns := logs.entries(t, "WARN", advisoryMsg)
	if len(warns) != 1 {
		t.Fatalf("%d advisory warnings, want 1", len(warns))
	}
	if m := warns[0]["msg"].(string); !strings.Contains(m, resyncMsg) {
		t.Errorf("advisory warning %q does not say the resync guarantee does not hold", m)
	}
	if warns[0]["herdr_version"] != "0.8.2" || warns[0]["herdr_minimum"] != "0.9.5" {
		t.Errorf("advisory warning %v does not name 0.8.2 and 0.9.5", warns[0])
	}

	// One warning per connect; the unchanged outcome is not reported again.
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.8.2 epoch 1 kept", 1)
	h.settle()
	if n := len(logs.entries(t, "WARN", advisoryMsg)); n != 2 {
		t.Errorf("%d advisory warnings after a reconnect, want 2", n)
	}
	if got := h.rec.gateReports(); len(got) != 1 {
		t.Errorf("gate reports %v, want the unchanged repeat left out", got)
	}
}

func TestGateAdvisory09x(t *testing.T) {
	h, logs := startGated(t, true, "0.9.2")
	h.rec.expect(t, "gate advisory 0.9.2", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	warns := logs.entries(t, "WARN", advisoryMsg)
	if len(warns) != 1 {
		t.Fatalf("%d advisory warnings, want 1", len(warns))
	}
	if m := warns[0]["msg"].(string); strings.Contains(m, resyncMsg) {
		t.Errorf("advisory warning %q for 0.9.2 carries the resync clause", m)
	}
}

func TestGateUnparsable(t *testing.T) {
	for _, v := range []string{"", "dev"} {
		t.Run("refused "+v, func(t *testing.T) {
			h, _ := startGated(t, false, v)
			h.fake.awaitPings(2)
			h.rec.expect(t, "gate refused "+v)
			onlyPings(t, h.fake.callList())
		})
		t.Run("advisory "+v, func(t *testing.T) {
			h, logs := startGated(t, true, v)
			h.rec.expect(t, "gate advisory "+v, "agent w1:p1 claude working", "agent w1:p2 claude idle")
			warns := logs.entries(t, "WARN", advisoryMsg)
			if len(warns) != 1 || !strings.Contains(warns[0]["msg"].(string), resyncMsg) {
				t.Errorf("advisory warnings %v, want one saying resync is not guaranteed", warns)
			}
		})
	}
}

func TestGateDowngradedAcrossReconnect(t *testing.T) {
	h, logs := startGated(t, false, "0.9.5", "0.8.2")
	h.rec.expect(t, "gate ok 0.9.5", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	p1, p2 := h.fake.statusSub("w1:p1"), h.fake.statusSub("w1:p2")
	before := len(h.fake.callList())
	h.loseLifecycle()
	h.rec.expect(t, "gate ok 0.9.5", "agent w1:p1 claude working", "agent w1:p2 claude idle",
		"gate refused 0.8.2", "removed w1:p1", "removed w1:p2")
	waitClosed(t, p1)
	waitClosed(t, p2)
	h.fake.awaitPings(4)
	onlyPings(t, h.fake.callList()[before:])
	if as := h.agents(); len(as) != 0 {
		t.Errorf("agents %+v after the refusal", as)
	}
	if n := len(logs.entries(t, "WARN", refusedMsg)); n != 1 {
		t.Errorf("%d refusal warnings, want 1", n)
	}
	if n := len(logs.entries(t, "INFO", "agent removed")); n != 2 {
		t.Errorf("%d removal log entries, want 2", n)
	}
	for _, e := range logs.entries(t, "INFO", "agent removed") {
		if e["reason"] != "herdr refused" {
			t.Errorf("removal reason %v, want herdr refused", e["reason"])
		}
	}
}

func TestGateRefusedVersionChange(t *testing.T) {
	h, logs := startGated(t, false, "0.8.2", "0.8.2", "0.9.4")
	h.fake.awaitPings(5)
	h.rec.expect(t, "gate refused 0.8.2", "gate refused 0.9.4")
	onlyPings(t, h.fake.callList())
	if n := len(logs.entries(t, "WARN", refusedMsg)); n != 2 {
		t.Errorf("%d refusal warnings, want one per reported version", n)
	}
}

func TestGateOKVersionChange(t *testing.T) {
	h, _ := startGated(t, false, "0.9.5", "0.9.6")
	h.rec.expect(t, "gate ok 0.9.5", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	h.loseLifecycle()
	h.rec.expect(t, "gate ok 0.9.5", "agent w1:p1 claude working", "agent w1:p2 claude idle",
		"gate ok 0.9.6", "reconnected 0.9.6 epoch 1 kept")
}

// recoverAfterRefusal runs ok, then refused across a reconnect, then ok again,
// with snap the snapshot herdr shows once refused: the Agents are reported
// again under epoch, and their new status subscriptions are live.
func recoverAfterRefusal(t *testing.T, snap core.Snapshot, reconnected string, epoch uint64) {
	t.Helper()
	h, _ := startGated(t, false, "0.9.5", "0.8.2", "0.9.5")
	h.rec.expect(t, "gate ok 0.9.5", "agent w1:p1 claude working", "agent w1:p2 claude idle")
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	h.fake.setSnapshot(snap)
	h.loseLifecycle()
	h.rec.expect(t, "gate ok 0.9.5", "agent w1:p1 claude working", "agent w1:p2 claude idle",
		"gate refused 0.8.2", "removed w1:p1", "removed w1:p2",
		"gate ok 0.9.5", reconnected, "agent w1:p1 claude working", "agent w1:p2 claude idle")
	if eps := h.rec.lineEpochs(); eps[len(eps)-1] != epoch || eps[len(eps)-2] != epoch {
		t.Errorf("line epochs %v, want the Agents reported again under epoch %d", eps, epoch)
	}
	sub := h.fake.awaitStatusSubs("w1:p1", 2)
	h.fake.awaitStatusSubs("w1:p2", 2)
	h.settle()
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.rec.awaitCount(t, "transition w1:p1 claude working->blocked live", 1)
	if ep := h.rec.lastTransition().Key().Epoch; ep != epoch {
		t.Errorf("transition epoch %d, want %d", ep, epoch)
	}
}

func TestGateRecoveryAfterRefusal(t *testing.T) {
	recoverAfterRefusal(t, twoAgents(), "reconnected 0.9.5 epoch 1 kept", 1)
}

func TestGateRecoveryAfterRefusalRestarted(t *testing.T) {
	recoverAfterRefusal(t, restarted(), "reconnected 0.9.5 epoch 2 incremented", 2)
}

// Profile resolution (Story 1.9).

const (
	claudeVar = "CLAUDE_CONFIG_DIR"
	p1PID     = 101
)

func claudeRule() core.ProfileRule {
	return core.ProfileRule{Variable: claudeVar, Absent: "claude",
		Values: map[string]string{"/h/.claude-cldw": "cldw", "/h/.claude-cldp": "cldp"}}
}

// oneAgent is a snapshot of one Agent pane, w1:p1, running program.
func oneAgent(program core.AgentProgram, status core.Status) core.Snapshot {
	p := agentPane("w1:p1", status)
	p.Agent.AgentProgram = program
	return core.Snapshot{HerdrVersion: "0.9.5", Panes: []core.PaneObservation{p, shellPane("w1:p3")}}
}

// startProfiled starts an engine on snap with the claude rule, a fake
// environment, w1:p1's foreground process p1PID, Profiles on the lines and
// the log collected. prepare, when not nil, runs before the engine starts.
func startProfiled(t *testing.T, snap core.Snapshot, prepare func(*fakeClient, *fakeEnviron)) (*harness, *fakeEnviron, *logSink) {
	t.Helper()
	fake := newFakeClient(t, snap)
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	if prepare != nil {
		prepare(fake, env)
	}
	logs := &logSink{}
	h := startEngineWith(t, fake, func(o *Options, r *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		r.profileLines = true
	})
	return h, env, logs
}

// setProfileTimeout sets profileTimeout for one test, restoring it after.
func setProfileTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := profileTimeout
	profileTimeout = d
	t.Cleanup(func() { profileTimeout = prev })
}

func (l *logSink) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestProfileResolved(t *testing.T) {
	cases := []struct {
		name    string
		value   *string
		profile string
	}{
		{"mapped", new("/h/.claude-cldw"), "cldw"},
		{"trailing slash", new("/h/.claude-cldw/"), "cldw"},
		{"absent", nil, "claude"},
		{"unmatched", new("/h/.other"), core.ProfileUnresolved},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, _, logs := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
				if c.value != nil {
					env.set(p1PID, claudeVar, *c.value)
				}
				env.set(p1PID, "SECRET", "do-not-log")
			})
			h.rec.expect(t, "agent w1:p1 claude working profile="+c.profile)
			h.fake.awaitSubscribe("w1:p1")
			h.settle()
			h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
			h.rec.expect(t, "agent w1:p1 claude working profile="+c.profile, "transition w1:p1 claude working->blocked live profile="+c.profile)
			if as := h.agents(); len(as) != 1 || as[0].Profile != c.profile {
				t.Errorf("agents %+v", as)
			}

			entries := logs.entries(t, "DEBUG", "profile resolved")
			if len(entries) != 1 || entries[0]["profile"] != c.profile {
				t.Fatalf("profile resolved entries %v", entries)
			}
			text := logs.text()
			if strings.Contains(text, "do-not-log") {
				t.Error("the log holds an environment value of another variable")
			}
			switch c.profile {
			case "cldw":
				if entries[0]["value"] != *c.value || entries[0]["variable"] != claudeVar {
					t.Errorf("a matched value is logged with its variable: %v", entries[0])
				}
			case core.ProfileUnresolved:
				if strings.Contains(text, *c.value) {
					t.Errorf("the log holds the unmatched value %q:\n%s", *c.value, text)
				}
			}
		})
	}
}

func TestProfileFailures(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*fakeClient, *fakeEnviron)
		// transient failures are retried profileRetries times.
		transient bool
	}{
		{"timeout", func(_ *fakeClient, env *fakeEnviron) {
			env.set(p1PID, claudeVar, "/h/.claude-cldw")
			env.hold()
		}, true},
		{"no foreground process", func(f *fakeClient, _ *fakeEnviron) {
			f.mu.Lock()
			delete(f.procs, "w1:p1")
			f.mu.Unlock()
		}, true},
		{"environment unreadable", func(_ *fakeClient, env *fakeEnviron) {
			env.fail(p1PID, errors.New("operation not permitted"))
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			begin := time.Now()
			h, env, logs := startProfiled(t, oneAgent("claude", core.StatusWorking), c.prepare)
			h.rec.expect(t, "agent w1:p1 claude working profile=unresolved")
			if c.name == "timeout" {
				if d := time.Since(begin); d < profileTimeout {
					t.Errorf("reported after %v, before the timeout %v", d, profileTimeout)
				}
			}
			lookups := 1
			if c.transient {
				lookups += profileRetries
				awaitLog(t, logs, "WARN", "profile lookup failed", 1)
			}
			// Past the whole retry window, nothing more is tried.
			time.Sleep(4 * profileRetryWait(profileRetries))
			h.settle()
			// A hung read keeps the pane's slot (Story 5.18): its retries run
			// out of time waiting for it, reading nothing.
			calls := lookups
			if c.name == "timeout" {
				calls = 1
				if started, _ := env.reads(); started != 1 {
					t.Errorf("%d environment reads, want 1", started)
				}
			}
			if n := h.fake.processCalls(); n != calls {
				t.Errorf("%d foreground process reads, want %d", n, calls)
			}
			if n := len(logs.entries(t, "WARN", "profile lookup failed")); c.transient && n != 1 {
				t.Errorf("%d warnings on giving up, want 1", n)
			}
			if n := len(logs.entries(t, "INFO", "profile lookup failed; retrying")); c.transient && n != profileRetries {
				t.Errorf("%d retries logged, want %d", n, profileRetries)
			}
			entries := logs.entries(t, "DEBUG", "profile resolved")
			if len(entries) != lookups {
				t.Fatalf("%d debug entries, want %d: %v", len(entries), lookups, entries)
			}
			for _, e := range entries {
				if e["error"] == nil || e["value"] != nil {
					t.Errorf("want each debug entry naming the error and no value: %v", e)
				}
			}
			h.rec.expect(t, "agent w1:p1 claude working profile=unresolved")
		})
	}
}

func TestProfileNoRule(t *testing.T) {
	h, _, _ := startProfiled(t, oneAgent("codex", core.StatusWorking), nil)
	h.rec.expect(t, "agent w1:p1 codex working profile=codex")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	if n := h.fake.processCalls(); n != 0 {
		t.Errorf("%d process_info calls for a program with no rule", n)
	}
	if got := core.Heading(h.agents()[0].Profile, "codex"); got != "codex" {
		t.Errorf("heading %q", got)
	}
}

// TestProfileStatusDuringResolve: a live Status change while the Profile is
// being resolved updates the Status silently, so the first report carries it
// and no Transition is reported.
func TestProfileStatusDuringResolve(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, _ := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
		env.hold()
	})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.setSnapshot(oneAgent("claude", core.StatusBlocked))
	sub := h.fake.statusSub("w1:p1")
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	sub.awaitDelivered(t, 1)
	h.settle()
	if got := h.rec.all(); len(got) != 0 {
		t.Fatalf("reported before the Profile was resolved: %v", got)
	}
	env.unblock()
	h.rec.expect(t, "agent w1:p1 claude blocked profile=cldw")
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude blocked profile=cldw")
}

// startedProfiled runs an engine whose w1:p1 resolved to cldw, settled.
func startedProfiled(t *testing.T) (*harness, *fakeEnviron, *logSink) {
	t.Helper()
	h, env, logs := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
	})
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	return h, env, logs
}

// awaitResolved waits until n Profile resolutions have been logged, and the
// engine has finished handling the last.
func (h *harness) awaitResolved(logs *logSink, n int) {
	h.t.Helper()
	deadline := time.Now().Add(wait)
	for len(logs.entries(h.t, "DEBUG", "profile resolved")) < n {
		if time.Now().After(deadline) {
			h.t.Fatalf("fewer than %d Profile resolutions", n)
		}
		time.Sleep(time.Millisecond)
	}
	h.settle()
}

func TestProfileRedetected(t *testing.T) {
	h, env, _ := startedProfiled(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldp")
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "profile w1:p1 cldp previous cldw")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "profile w1:p1 cldp previous cldw",
		"transition w1:p1 claude working->idle live profile=cldp")
	if p := h.rec.lastTransition().Profile(); p != "cldp" {
		t.Errorf("transition profile %q", p)
	}
}

func TestProfileRedetectedSame(t *testing.T) {
	h, _, logs := startedProfiled(t)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.awaitResolved(logs, 2)
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw")
}

func TestProfileProgramChanged(t *testing.T) {
	h, _, _ := startedProfiled(t)
	h.fake.setSnapshot(oneAgent("codex", core.StatusWorking))
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "profile w1:p1 codex previous cldw")

	// A status event naming another program resolves again too.
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusWorking, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "profile w1:p1 codex previous cldw", "profile w1:p1 cldw previous codex")
}

// TestProfileStaleResult: a resolution superseded by another, or for a pane
// gone since, is ignored.
func TestProfileStaleResult(t *testing.T) {
	t.Run("superseded", func(t *testing.T) {
		setProfileTimeout(t, wait)
		h, env, _ := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
			env.set(p1PID, claudeVar, "/h/.claude-cldw")
			env.hold()
		})
		h.fake.awaitSubscribe("w1:p1")
		h.settle()
		// The program changes to one with no rule while the claude rule is
		// still being read: that settles it, and the claude result is stale.
		h.fake.setSnapshot(oneAgent("codex", core.StatusWorking))
		h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusWorking, AgentProgram: "codex"})
		h.rec.expect(t, "agent w1:p1 codex working profile=codex")
		env.unblock()
		h.settle()
		h.rec.expect(t, "agent w1:p1 codex working profile=codex")
	})
	t.Run("pane gone", func(t *testing.T) {
		setProfileTimeout(t, wait)
		h, env, _ := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
			env.set(p1PID, claudeVar, "/h/.claude-cldw")
			env.hold()
		})
		h.fake.awaitSubscribe("w1:p1")
		h.settle()
		h.fake.setSnapshot(core.Snapshot{HerdrVersion: "0.9.5", Panes: []core.PaneObservation{shellPane("w1:p3")}})
		h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
		h.settle()
		if as := h.agents(); len(as) != 0 {
			t.Fatalf("agents %+v", as)
		}
		env.unblock()
		h.settle()
		if got := h.rec.all(); len(got) != 0 {
			t.Fatalf("a stale result was reported: %v", got)
		}
	})
}

// TestProfileProgramChangedDuringResolve: a status event that changes an
// unreported pane's program to one with no rule settles its Profile at once;
// the one Agent line carries the event's Status, and no Transition follows.
func TestProfileProgramChangedDuringResolve(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, _ := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
		env.hold()
	})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.setSnapshot(oneAgent("codex", core.StatusBlocked))
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "codex"})
	h.rec.expect(t, "agent w1:p1 codex blocked profile=codex")
	env.unblock()
	h.settle()
	h.rec.expect(t, "agent w1:p1 codex blocked profile=codex")
}

// TestProfileTerminalReplacedDuringResolve: an unreported pane whose terminal
// is replaced while its Profile is resolved is another Agent; the resolution
// read from the old terminal's process is dropped.
func TestProfileTerminalReplacedDuringResolve(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, _ := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
		env.set(202, claudeVar, "/h/.claude-cldp")
		env.hold()
	})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	s := oneAgent("claude", core.StatusWorking)
	s.Panes[0].TerminalID = "t-new"
	h.fake.setSnapshot(s)
	h.fake.setProc("w1:p1", 202)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	deadline := time.Now().Add(wait)
	for h.fake.processCalls() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the new terminal's Profile was not resolved")
		}
		time.Sleep(time.Millisecond)
	}
	h.settle()
	env.unblock()
	h.rec.expect(t, "agent w1:p1 claude working profile=cldp")
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working profile=cldp")
}

// TestProfileSnapshotDuringResolve: a read in flight when a live event
// arrives does not overwrite that event's Status on a pane not yet reported.
func TestProfileSnapshotDuringResolve(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, _ := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
		env.hold()
	})
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.fake.awaitSnapshotCall() // held; it still says working
	sub := h.fake.statusSub("w1:p1")
	// The second event is taken only once the engine has taken the first.
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	sub.awaitDelivered(t, 2)
	h.agents()

	h.fake.release <- struct{}{} // the stale read is applied
	h.fake.awaitSnapshotCall()   // the read the event caused, held
	env.unblock()
	h.rec.expect(t, "agent w1:p1 claude blocked profile=cldw")

	h.fake.setSnapshot(oneAgent("claude", core.StatusBlocked))
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude blocked profile=cldw")
}

// TestProfileReconnectDuringRedetect: a reconnect that keeps the epoch while
// a reported Agent's Profile is resolved again leaves that resolution intact:
// the reconnect starts no lookup of its own for it (Story 5.4).
func TestProfileReconnectDuringRedetect(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, _ := startedProfiled(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldp")
	env.hold()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	deadline := time.Now().Add(wait)
	for h.fake.processCalls() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("not resolved again")
		}
		time.Sleep(time.Millisecond)
	}
	h.loseLifecycle()
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "reconnected 0.9.5 epoch 1 kept")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	if n := h.fake.processCalls(); n != 2 {
		t.Errorf("%d lookups, want 2: the reconnect superseded the lookup in flight", n)
	}
	env.unblock()
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "reconnected 0.9.5 epoch 1 kept", "profile w1:p1 cldp previous cldw")
	h.settle()
	for _, l := range h.rec.all() {
		if strings.Contains(l, core.ProfileUnresolved) {
			t.Errorf("line %q", l)
		}
	}
}
