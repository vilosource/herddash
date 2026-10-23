package engine

import (
	"errors"
	"slices"
	"testing"

	"github.com/vilosource/herddash/internal/core"
)

// pingCount is how many times herdr has been pinged so far.
func pingCount(h *harness) int { return len(h.fake.pings()) }

// countCalls counts the calls named call so far.
func countCalls(h *harness, call string) int {
	n := 0
	for _, c := range h.fake.callList() {
		if c == call {
			n++
		}
	}
	return n
}

// disconnected is a publication that says herdr is not connected.
func disconnected(p Publication) bool {
	return hasKind(ChangeConnection)(p) && !p.View.Connection.Connected
}

// readAfter is a publication of a read that completed after last.
func readAfter(last int64) func(Publication) bool {
	return func(p Publication) bool {
		return hasKind(ChangeConnection)(p) && p.View.Connection.Connected && p.View.Connection.LastSnapshotAt > last
	}
}

// Snapshot reads that keep failing once ready are a lost connection: the
// bound-th failure publishes connected false with the Agents kept, the engine
// starts again, and once reads succeed it publishes connected true with a
// later last_snapshot_at and records the reconnect.
func TestViewSnapshotFailuresLoseConnection(t *testing.T) {
	h, w, n := startedViewed(t)
	before := h.engine.View().Connection
	if pingCount(h) != 1 {
		t.Fatalf("%d pings at start, want 1", pingCount(h))
	}
	reads := h.fake.snapshots.Load()
	h.fake.fail("snapshot", snapshotFailuresBeforeLost)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})

	ps := w.awaitAfter("disconnect", n, disconnected)
	p := ps[len(ps)-1]
	if !slices.Equal(kinds(p), []ChangeKind{ChangeConnection}) {
		t.Fatalf("publication %+v", p.Changes)
	}
	if got := keysOf(p.View.Agents); !slices.Equal(got, []string{"w1:p2", "w1:p1"}) {
		t.Errorf("agents %v, want both kept", got)
	}
	if got := p.View.Connection.LastSnapshotAt; got != before.LastSnapshotAt {
		t.Errorf("last_snapshot_at %d, want the last good read's %d", got, before.LastSnapshotAt)
	}
	if got := h.fake.snapshots.Load() - reads; got < int32(snapshotFailuresBeforeLost) {
		t.Errorf("%d reads before the loss, want at least %d", got, snapshotFailuresBeforeLost)
	}

	// It pings again and, its reads succeeding, is connected with a later read.
	h.fake.awaitPings(2)
	n += len(ps)
	ps = w.awaitAfter("reconnect", n, readAfter(before.LastSnapshotAt))
	h.settle()
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "reconnected 0.9.5 epoch 1 kept")
	if pingCount(h) != 2 {
		t.Errorf("%d pings, want 2", pingCount(h))
	}
	if !h.engine.View().Connection.Connected {
		t.Error("not connected after recovery")
	}
	checkSeqs(t, w.all())
}

// Failures that go on past the loss fail the starts that follow it: the
// engine stays disconnected, publishing no connected true, until a read
// succeeds, and then recovers with a later last_snapshot_at.
func TestViewSnapshotFailuresOutlastReconnect(t *testing.T) {
	h, w, n := startedViewed(t)
	before := h.engine.View().Connection
	h.fake.fail("snapshot", snapshotFailuresBeforeLost+2)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})

	ps := w.awaitAfter("disconnect", n, disconnected)
	n += len(ps)
	// The two starts after the loss each fail their read, then one succeeds.
	h.fake.awaitPings(4)
	ps = w.awaitAfter("recovery", n, func(p Publication) bool { return p.View.Connection.Connected })
	for i, p := range ps[:len(ps)-1] {
		if p.View.Connection.Connected {
			t.Errorf("publication %d after the disconnect published connected true before recovery", i)
		}
	}
	if c := ps[len(ps)-1].View.Connection; c.LastSnapshotAt <= before.LastSnapshotAt {
		t.Errorf("recovered with last_snapshot_at %d, want later than %d", c.LastSnapshotAt, before.LastSnapshotAt)
	}
	h.settle()
	if !h.engine.View().Connection.Connected {
		t.Error("not connected after recovery")
	}
	if pingCount(h) != 4 {
		t.Errorf("%d pings, want 4: the start, the failed restarts and the one that succeeded", pingCount(h))
	}
	checkSeqs(t, w.all())
}

// Fewer failures in a row than the bound are blips: the engine stays
// connected and does not ping again, and a success clears the count, so as
// many failing again later is still a blip.
func TestViewSnapshotFailuresBelowBound(t *testing.T) {
	h, w, n := startedViewed(t)
	start := n
	for round := range 2 {
		before := h.engine.View().Connection
		h.fake.fail("snapshot", snapshotFailuresBeforeLost-1)
		h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
		ps := w.awaitAfter("the read after the failures", n, readAfter(before.LastSnapshotAt))
		h.settle()
		n += len(ps)
		if !h.engine.View().Connection.Connected {
			t.Fatalf("round %d: not connected", round)
		}
	}
	for i, p := range w.all()[start:] {
		if disconnected(p) {
			t.Errorf("publication %d after the start published connected false", i)
		}
	}
	if pingCount(h) != 1 {
		t.Errorf("%d pings, want 1", pingCount(h))
	}
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle")
}

// An unexpected lifecycle line reads a snapshot and ends nothing: the
// lifecycle subscription stays open, nothing pings or subscribes again, and
// the engine stays connected. A later event on the same stream still counts.
func TestViewUnexpectedLifecycleLine(t *testing.T) {
	h, w, n := startedViewed(t)
	start := n
	sub := h.fake.lifecycleSub()
	calls := len(h.fake.callList())
	before := h.engine.View().Connection

	sub.unexpected(errors.New(`unexpected event: lifecycle event "pane_focused"`))
	h.fake.awaitSnapshotCall()
	ps := w.awaitAfter("the snapshot", n, readAfter(before.LastSnapshotAt))
	h.settle()
	n += len(ps)
	if sub.isClosed() || h.fake.lifecycleSub() != sub {
		t.Error("the lifecycle subscription was closed or replaced")
	}
	for _, c := range h.fake.callList()[calls:] {
		if c != "snapshot" {
			t.Errorf("call %q after the unexpected line, want only snapshots", c)
		}
	}
	if !h.engine.View().Connection.Connected {
		t.Error("not connected")
	}

	// A second one reads again, and a real event after it still arrives on
	// the same stream.
	before = h.engine.View().Connection
	sub.unexpected(errors.New("unexpected event: lifecycle line has no data"))
	h.fake.awaitSnapshotCall()
	w.awaitAfter("the second snapshot", n, readAfter(before.LastSnapshotAt))
	h.settle()
	s := twoAgents()
	s.Panes = append(s.Panes, agentPane("w1:p4", core.StatusWorking))
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.fake.awaitSubscribe("w1:p4")
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude working")
	h.settle()
	for i, p := range w.all()[start:] {
		if disconnected(p) {
			t.Errorf("publication %d after the start published connected false", i)
		}
	}
	if pingCount(h) != 1 {
		t.Errorf("%d pings, want 1", pingCount(h))
	}
	if got := countCalls(h, "lifecycle"); got != 1 {
		t.Errorf("%d lifecycle subscribes, want 1", got)
	}
	if sub.isClosed() {
		t.Error("the lifecycle subscription was closed")
	}
	checkSeqs(t, w.all())
}
