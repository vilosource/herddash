package engine

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
)

// startFocus starts a viewed, recording engine on snap with a manual clock,
// the reconcile timer out of the way, and waits for its Agents. opts, when
// not nil, adjusts its Options after those.
func startFocus(t *testing.T, snap core.Snapshot, opts func(*Options, *recorder)) (*harness, *watcher, *fakeStore, *manualClock) {
	t.Helper()
	setReconcileInterval(t, time.Hour)
	clock := newManualClock()
	fake := newFakeClient(t, snap)
	st := newFakeStore(fake)
	h, w := startViewed(t, fake, func(o *Options, r *recorder) {
		o.Store = st
		o.Clock = clock.now
		if opts != nil {
			opts(o, r)
		}
	})
	h.awaitAgents(agentCount(snap))
	return h, w, st, clock
}

// focusAcks is every acknowledgement st holds whose source is herdr's focus.
func focusAcks(st *fakeStore) []core.Acknowledgement {
	var out []core.Acknowledgement
	for _, a := range st.ackList() {
		if a.Source() == core.AckSourceHerdrFocus {
			out = append(out, a)
		}
	}
	return out
}

// reconcileRead delivers one reconcile tick and waits for its read.
func (h *harness) reconcileRead() {
	h.t.Helper()
	h.reconcileTick()
	h.fake.awaitSnapshotCall()
	h.settle()
}

// A focus event is not a sign that tracked state is stale (ADR-0015): it
// requests no snapshot read, between reads or during one, and makes no read
// in flight be followed by another.
func TestFocusReadsNoSnapshot(t *testing.T) {
	h, _, _ := startedViewed(t)
	reads := h.fake.snapshots.Load()

	// Between reads: a move, a repeat for the same pane, a move back.
	h.focus("w1:p2", "w1")
	h.focus("w1:p2", "w1")
	h.focus("w1:p1", "w1")
	h.fake.noSnapshotCall()
	h.settle()
	if got := h.fake.snapshots.Load(); got != reads {
		t.Fatalf("%d snapshot reads after focus events, want %d", got, reads)
	}

	// During a read: the read in flight is not followed by another.
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.fake.awaitSnapshotCall()
	h.focus("w1:p2", "w1")
	h.focus("w1:p3", "w1")
	h.fake.noSnapshotCall()
	h.fake.release <- struct{}{}
	h.fake.noSnapshotCall()
	h.fake.setGated(false)
	h.settle()
	if got := h.fake.snapshots.Load(); got != reads+1 {
		t.Errorf("%d snapshot reads, want %d: only the context change's", got, reads+1)
	}
}

// Visit, read, leave: the Box is Finished and in view while the operator is
// there, and Seen when they leave after 3 s; the publication for the leave
// carries agent changes for the pane left and the pane landed on only.
func TestFocusReadThenLeave(t *testing.T) {
	logs := &logSink{}
	h, w, st, clock := startFocus(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle),
		agentPane("w1:p3", core.StatusIdle)), func(o *Options, _ *recorder) {
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	before := h.viewOf("w1:p1")
	if before.InView || h.viewOf("w1:p2").InView || before.Class != core.ClassFinished || before.Seen {
		t.Fatalf("before any focus: %+v, want Finished, not seen, nothing in view", before)
	}
	h.focusOn("w1:p1")
	if v := h.viewOf("w1:p1"); !v.InView || v.Seen || v.SortKey != before.SortKey {
		t.Fatalf("focused: %+v, want in view, not seen, the same sort key", v)
	}
	if h.viewOf("w1:p2").InView {
		t.Error("w1:p2 in view while w1:p1 is focused")
	}
	clock.advance(seenDwell)
	h.agents()
	time.Sleep(quiet)
	n := len(w.all())
	h.focusOn("w1:p2")
	v := h.viewOf("w1:p1")
	if !v.Seen || v.InView || v.Class != core.ClassFinished || v.SortKey.ClassRank != 2 || v.WaitingSince != before.WaitingSince {
		t.Fatalf("after the leave: %+v, want seen, rank 2, not in view, the same finish", v)
	}
	if !h.viewOf("w1:p2").InView || h.viewOf("w1:p3").InView {
		t.Error("after the leave, want w1:p2 alone in view")
	}
	acks := st.ackList()
	if len(acks) != 1 || acks[0].Source() != core.AckSourceHerdrFocus || acks[0].Key() != before.Key ||
		acks[0].FinishedSince() != before.WaitingSince || !acks[0].Seen() || orderOf(acks[0]) != 7 {
		t.Fatalf("acknowledgements %+v, want one herdr_focus mark of w1:p1's finish carrying herdr's order 7", acks)
	}
	h.agents()
	time.Sleep(quiet)
	ps := w.all()[n:]
	if len(ps) != 1 {
		t.Fatalf("publications %s, want one for the leave", describe(ps))
	}
	var changed []string
	for _, c := range ps[0].Changes {
		if c.Kind != ChangeAgent {
			t.Errorf("change %s, want only agent changes", c.Kind)
		}
		changed = append(changed, c.Agent.Key.PaneID)
	}
	slices.Sort(changed)
	if !slices.Equal(changed, []string{"w1:p1", "w1:p2"}) {
		t.Errorf("agent changes for %v, want w1:p1 and w1:p2", changed)
	}
	e := logs.entries(t, "INFO", "acknowledgement")
	if len(e) != 1 || e[0]["source"] != string(core.AckSourceHerdrFocus) || e[0]["pane"] != "w1:p1" {
		t.Errorf("acknowledgement log %v, want one at info with source herdr_focus", e)
	}
	// Back and away again: the finish has its row, so nothing more.
	h.focusOn("w1:p1")
	clock.advance(10 * time.Second)
	h.focusOn("w1:p3")
	if n := len(st.ackList()); n != 1 {
		t.Errorf("%d acknowledgements after leaving a seen finish, want 1", n)
	}
}

// A dwell under seenDwell does not mark, and flicking through panes, a
// second on each, marks nothing.
func TestFocusShortDwell(t *testing.T) {
	h, _, st, clock := startFocus(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusDone),
		agentPane("w1:p3", core.StatusDone), agentPane("w1:p4", core.StatusDone)), nil)
	h.focusOn("w1:p1")
	clock.advance(seenDwell - time.Millisecond)
	h.focusOn("w1:p2")
	for _, id := range []string{"w1:p1", "w1:p2", "w1:p3", "w1:p4"} {
		clock.advance(time.Second)
		h.focusOn(id)
	}
	if n := len(st.ackList()); n != 0 {
		t.Errorf("%d acknowledgements, want none", n)
	}
	for _, v := range h.engine.View().Agents {
		if v.Seen {
			t.Errorf("%s seen", v.Key.PaneID)
		}
	}
}

// Options.SeenDwell, the browser check's seam (Story 6.8), replaces seenDwell
// for one engine: a leave at it marks, one just under it does not.
func TestFocusSeenDwellOption(t *testing.T) {
	const dwell = 400 * time.Millisecond
	h, _, st, clock := startFocus(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusDone),
		agentPane("w1:p3", core.StatusIdle)), func(o *Options, _ *recorder) { o.SeenDwell = dwell })
	h.focusOn("w1:p1")
	clock.advance(dwell - time.Millisecond)
	h.focusOn("w1:p3")
	if n := len(st.ackList()); n != 0 || h.viewOf("w1:p1").Seen {
		t.Fatalf("%d acknowledgements after a dwell just under Options.SeenDwell, want none", n)
	}
	h.focusOn("w1:p2")
	clock.advance(dwell)
	h.focusOn("w1:p3")
	if acks := focusAcks(st); len(acks) != 1 || acks[0].Key().PaneID != "w1:p2" || !h.viewOf("w1:p2").Seen {
		t.Fatalf("acknowledgements %+v after a dwell of Options.SeenDwell, want one herdr_focus mark of w1:p2", acks)
	}
	if h.viewOf("w1:p1").Seen {
		t.Error("w1:p1 seen")
	}
}

// The dwell counts time focused during the current finish only: focus that
// began while the Agent was Working restarts when it finishes.
func TestFocusDwellRestartsAtFinish(t *testing.T) {
	for _, c := range []struct {
		after time.Duration
		mark  bool
	}{{time.Second, false}, {seenDwell, true}} {
		t.Run(c.after.String(), func(t *testing.T) {
			h, _, st, clock := startFocus(t, snapOf(agentPane("w1:p1", core.StatusWorking), agentPane("w1:p2", core.StatusIdle)), nil)
			h.focusOn("w1:p1")
			clock.advance(10 * time.Second)
			done := h.say("w1:p1", core.StatusDone)
			clock.advance(c.after)
			h.focusOn("w1:p2")
			if v := h.viewOf("w1:p1"); v.Seen != c.mark {
				t.Errorf("left %s after the finish: %+v, want seen %v", c.after, v, c.mark)
			}
			if acks := focusAcks(st); c.mark && (len(acks) != 1 || acks[0].FinishedSince() != done.WaitingSince) || !c.mark && len(acks) != 0 {
				t.Errorf("acknowledgements %+v", acks)
			}
		})
	}
}

// done to idle within a finish moves neither the class nor its start, so it
// neither restarts the dwell nor marks by itself (herdr's done/idle is not
// used for Seen).
func TestFocusDoneToIdle(t *testing.T) {
	h, _, st, clock := startFocus(t, snapOf(agentPane("w1:p1", core.StatusWorking), agentPane("w1:p2", core.StatusWorking),
		agentPane("w1:p3", core.StatusIdle)), nil)
	h.say("w1:p1", core.StatusDone)
	h.say("w1:p2", core.StatusDone)
	// An unfocused Finished Agent going idle: nothing.
	h.focusOn("w1:p3")
	clock.advance(10 * time.Second)
	h.say("w1:p2", core.StatusIdle)
	if v := h.viewOf("w1:p2"); v.Seen || v.Class != core.ClassFinished {
		t.Errorf("w1:p2 after done to idle %+v, want Finished, not seen", v)
	}
	// The focused one going idle midway: the dwell goes on.
	h.focusOn("w1:p1")
	clock.advance(2 * time.Second)
	h.say("w1:p1", core.StatusIdle)
	clock.advance(seenDwell - 2*time.Second)
	h.focusOn("w1:p3")
	if v := h.viewOf("w1:p1"); !v.Seen {
		t.Errorf("w1:p1 left 3 s after focus, idle midway: %+v, want seen", v)
	}
	if acks := focusAcks(st); len(acks) != 1 || acks[0].Key().PaneID != "w1:p1" {
		t.Errorf("acknowledgements %+v, want w1:p1's alone", acks)
	}
}

// A finish into the focused pane while the operator is away stays Finished
// and in view, however long, until a leave; the dwell then counts from the
// finish.
func TestFocusFinishWhileAway(t *testing.T) {
	h, _, st, clock := startFocus(t, snapOf(agentPane("w1:p1", core.StatusWorking), agentPane("w1:p2", core.StatusIdle)), nil)
	h.focusOn("w1:p1")
	done := h.say("w1:p1", core.StatusDone)
	h.fake.setSnapshot(focused(snapOf(paneAt("w1:p1", core.StatusDone, 8), agentPane("w1:p2", core.StatusIdle)), "w1:p1"))
	for range 6 {
		clock.advance(10 * time.Minute)
		h.reconcileRead()
	}
	if v := h.viewOf("w1:p1"); v.Seen || !v.InView || v.Class != core.ClassFinished {
		t.Fatalf("an hour on, %+v, want Finished, in view, not seen", v)
	}
	if n := len(st.ackList()); n != 0 {
		t.Fatalf("%d acknowledgements while away, want none", n)
	}
	h.focusOn("w1:p2")
	if v := h.viewOf("w1:p1"); !v.Seen || v.WaitingSince != done.WaitingSince {
		t.Errorf("after the leave %+v, want seen", v)
	}
	if acks := focusAcks(st); len(acks) != 1 || orderOf(acks[0]) != 8 {
		t.Errorf("acknowledgements %+v, want one herdr_focus row with herdr's order 8", acks)
	}
}

// A detached herdr sends no focus events: its snapshots still name a
// focused pane, which is in view, and nothing is ever marked.
func TestFocusDetached(t *testing.T) {
	snap := focused(snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)), "w1:p1")
	h, _, st, clock := startFocus(t, snap, nil)
	if v := h.viewOf("w1:p1"); !v.InView {
		t.Fatalf("w1:p1 %+v, want in view from the first snapshot", v)
	}
	for range 5 {
		clock.advance(time.Hour)
		h.reconcileRead()
	}
	if v := h.viewOf("w1:p1"); !v.InView || v.Seen {
		t.Errorf("w1:p1 %+v, want in view, not seen", v)
	}
	if n := len(st.ackList()); n != 0 {
		t.Errorf("%d acknowledgements, want none", n)
	}
}

// Needs you, Idle, Working, Unknown and an already seen finish are never
// marked by a leave, however long the dwell.
func TestFocusNotMarked(t *testing.T) {
	for _, c := range []struct {
		name   string
		status core.Status
		board  bool // marked on the Board first
	}{
		{"needs you", core.StatusBlocked, false},
		{"idle", core.StatusIdle, false},
		{"working", core.StatusWorking, false},
		{"unknown", core.StatusUnknown, false},
		{"already seen", core.StatusDone, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, _, st, clock := startFocus(t, snapOf(agentPane("w1:p1", c.status), agentPane("w1:p2", core.StatusIdle)), nil)
			want := 0
			if c.board {
				if _, err := h.ack(true, itemOf(h.viewOf("w1:p1"))); err != nil {
					t.Fatal(err)
				}
				want = 1
			}
			h.focusOn("w1:p1")
			clock.advance(10 * time.Second)
			h.focusOn("w1:p2")
			if n := len(st.ackList()); n != want {
				t.Errorf("%d acknowledgements, want %d", n, want)
			}
			if len(focusAcks(st)) != 0 {
				t.Error("a herdr_focus row was written")
			}
		})
	}
}

// Background still takes precedence: a Background Box is not marked.
func TestFocusNotMarkedBackground(t *testing.T) {
	fastBackground(t)
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	setReconcileInterval(t, time.Hour)
	clock := newManualClock()
	fake := newFakeClient(t, oneAgent("claude", core.StatusDone))
	fake.setProc("w1:p1", p1PID)
	st := newFakeStore(fake)
	h, w := startViewed(t, fake, func(o *Options, _ *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
		o.Children = kids.read
		o.Store = st
		o.Clock = clock.now
	})
	awaitBackground(w, true)
	h.focusOn("w1:p1")
	clock.advance(10 * time.Second)
	h.focusOn("w1:p3")
	if n := len(st.ackList()); n != 0 {
		t.Errorf("%d acknowledgements for a Background Agent, want none", n)
	}
}

// Mark unseen after an automatic mark sticks: later leaves write nothing,
// within the run and after the Daemon starts again over the same record.
func TestFocusMarkUnseenSticks(t *testing.T) {
	setReconcileInterval(t, time.Hour)
	clock := newManualClock()
	st := newFakeStore(nil)
	run := func(snap core.Snapshot) *harness {
		t.Helper()
		fake := newFakeClient(t, snap)
		st.mu.Lock()
		st.client = fake
		st.mu.Unlock()
		h := startRecording(t, fake, st, func(o *Options, _ *recorder) { o.Clock = clock.now })
		h.awaitAgents(2)
		return h
	}
	h := run(snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)))
	h.focusOn("w1:p1")
	clock.advance(seenDwell)
	h.focusOn("w1:p2")
	finish := h.viewOf("w1:p1")
	if !finish.Seen {
		t.Fatalf("w1:p1 %+v, want seen by the leave", finish)
	}
	if _, err := h.ack(false, itemOf(finish)); err != nil {
		t.Fatal(err)
	}
	h.focusOn("w1:p1")
	clock.advance(10 * time.Second)
	h.focusOn("w1:p2")
	if v := h.viewOf("w1:p1"); v.Seen {
		t.Errorf("after Mark unseen and another leave %+v, want not seen", v)
	}
	if n := len(st.ackList()); n != 2 {
		t.Fatalf("%d acknowledgements, want the mark and the undo", n)
	}
	h.stop()

	h = run(focused(snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)), "w1:p1"))
	if v := h.viewOf("w1:p1"); v.Seen || !v.InView || v.WaitingSince != finish.WaitingSince {
		t.Fatalf("restarted %+v, want the same finish, not seen, in view", v)
	}
	clock.advance(10 * time.Second)
	h.focusOn("w1:p2")
	if v := h.viewOf("w1:p1"); v.Seen {
		t.Errorf("restarted and left %+v, want not seen", v)
	}
	if n := len(st.ackList()); n != 2 {
		t.Errorf("%d acknowledgements after a restart and a leave, want 2", n)
	}
}

// A restart whose read of the acknowledgements fails cannot know whether the
// finish has the operator's own row, so no leave marks it: a Mark unseen
// still wins. The Agent's next finish is judged as usual.
func TestFocusAckReadFails(t *testing.T) {
	setReconcileInterval(t, time.Hour)
	clock := newManualClock()
	st := newFakeStore(nil)
	run := func(snap core.Snapshot) *harness {
		t.Helper()
		fake := newFakeClient(t, snap)
		st.mu.Lock()
		st.client = fake
		st.mu.Unlock()
		h := startRecording(t, fake, st, func(o *Options, _ *recorder) { o.Clock = clock.now })
		h.awaitAgents(2)
		return h
	}
	h := run(snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)))
	finish := h.viewOf("w1:p1")
	if _, err := h.ack(true, itemOf(finish)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ack(false, itemOf(finish)); err != nil {
		t.Fatal(err)
	}
	h.stop()

	st.mu.Lock()
	st.ackReadErr = errBoom
	st.mu.Unlock()
	h = run(snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)))
	if v := h.viewOf("w1:p1"); v.Seen || v.WaitingSince != finish.WaitingSince {
		t.Fatalf("restarted %+v, want the same finish, not seen", v)
	}
	h.focusOn("w1:p1")
	clock.advance(10 * time.Second)
	h.focusOn("w1:p2")
	if v := h.viewOf("w1:p1"); v.Seen {
		t.Errorf("left after a failed read %+v, want not seen: the Mark unseen wins", v)
	}
	if acks := focusAcks(st); len(acks) != 0 {
		t.Fatalf("herdr_focus rows %+v after a failed read, want none", acks)
	}

	st.mu.Lock()
	st.ackReadErr = nil
	st.mu.Unlock()
	h.say("w1:p1", core.StatusWorking)
	clock.advance(time.Second)
	next := h.say("w1:p1", core.StatusDone)
	h.focusOn("w1:p1")
	clock.advance(seenDwell)
	h.focusOn("w1:p2")
	if v := h.viewOf("w1:p1"); !v.Seen {
		t.Errorf("the next finish left %+v, want seen", v)
	}
	if acks := focusAcks(st); len(acks) != 1 || acks[0].FinishedSince() != next.WaitingSince {
		t.Errorf("herdr_focus rows %+v, want one for the next finish", acks)
	}
}

// An automatic mark survives a restart; a turn herddash did not observe, in
// the run or across a restart, ends a mark and lets a leave mark the new
// finish afresh, the earlier rows not counting for it.
func TestFocusMarkAcrossRestartAndMissedTurn(t *testing.T) {
	setReconcileInterval(t, time.Hour)
	clock := newManualClock()
	run := func(st *fakeStore, snap core.Snapshot) *harness {
		t.Helper()
		fake := newFakeClient(t, snap)
		st.mu.Lock()
		st.client = fake
		st.mu.Unlock()
		h := startRecording(t, fake, st, func(o *Options, _ *recorder) { o.Clock = clock.now })
		h.awaitAgents(2)
		return h
	}
	leave := func(h *harness) {
		t.Helper()
		h.focusOn("w1:p1")
		clock.advance(seenDwell)
		h.focusOn("w1:p2")
	}

	t.Run("automatic", func(t *testing.T) {
		st := newFakeStore(nil)
		h := run(st, snapOf(paneAt("w1:p1", core.StatusDone, 7), paneAt("w1:p2", core.StatusIdle, 3)))
		leave(h)
		finish := h.viewOf("w1:p1")
		if !finish.Seen {
			t.Fatalf("%+v, want seen", finish)
		}
		h.stop()
		h = run(st, snapOf(paneAt("w1:p1", core.StatusDone, 7), paneAt("w1:p2", core.StatusIdle, 3)))
		if v := h.viewOf("w1:p1"); !v.Seen || v.WaitingSince != finish.WaitingSince {
			t.Fatalf("restarted %+v, want seen with the same waiting_since", v)
		}
		// In the run: a read shows a turn not observed.
		h.fake.setSnapshot(snapOf(paneAt("w1:p1", core.StatusDone, 9), paneAt("w1:p2", core.StatusIdle, 3)))
		h.reconcileRead()
		if v := h.viewOf("w1:p1"); v.Seen {
			t.Fatalf("after a turn not observed %+v, want not seen", v)
		}
		leave(h)
		acks := focusAcks(st)
		if len(acks) != 2 || orderOf(acks[1]) != 9 || !h.viewOf("w1:p1").Seen {
			t.Errorf("acknowledgements %+v, want a second herdr_focus row at order 9, and seen", acks)
		}
	})
	t.Run("board mark, turn missed while down", func(t *testing.T) {
		st := newFakeStore(nil)
		h := run(st, snapOf(paneAt("w1:p1", core.StatusDone, 7), paneAt("w1:p2", core.StatusIdle, 3)))
		if _, err := h.ack(true, itemOf(h.viewOf("w1:p1"))); err != nil {
			t.Fatal(err)
		}
		h.stop()
		h = run(st, focused(snapOf(paneAt("w1:p1", core.StatusDone, 9), paneAt("w1:p2", core.StatusIdle, 3)), "w1:p1"))
		if v := h.viewOf("w1:p1"); v.Seen {
			t.Fatalf("restarted after a turn not observed %+v, want not seen", v)
		}
		clock.advance(seenDwell)
		h.focusOn("w1:p2")
		acks := st.ackList()
		if len(acks) != 2 || acks[0].Source() != core.AckSourceBoard || acks[1].Source() != core.AckSourceHerdrFocus ||
			orderOf(acks[1]) != 9 || !h.viewOf("w1:p1").Seen {
			t.Errorf("acknowledgements %+v, want the board mark then a herdr_focus row at order 9, and seen", acks)
		}
	})
}

// A focus move missed while the lifecycle connection was down never marks:
// the first snapshot after the reconnect names the pane landed on, counted
// from then.
func TestFocusReconnectMissedMove(t *testing.T) {
	snap := snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusDone), agentPane("w1:p3", core.StatusIdle))
	h, _, st, clock := startFocus(t, snap, nil)
	h.focusOn("w1:p1")
	clock.advance(10 * time.Second)
	h.fake.setSnapshot(focused(snap, "w1:p2"))
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	for _, id := range []string{"w1:p1", "w1:p2", "w1:p3"} {
		h.fake.awaitStatusSubs(id, 2)
	}
	h.settle()
	if v := h.viewOf("w1:p1"); v.Seen || v.InView {
		t.Errorf("w1:p1 after the reconnect %+v, want neither seen nor in view", v)
	}
	if v := h.viewOf("w1:p2"); !v.InView || v.Seen {
		t.Errorf("w1:p2 after the reconnect %+v, want in view, not seen", v)
	}
	if n := len(st.ackList()); n != 0 {
		t.Fatalf("%d acknowledgements, want none", n)
	}
	clock.advance(seenDwell)
	h.focusOn("w1:p3")
	if v := h.viewOf("w1:p2"); !v.Seen {
		t.Errorf("w1:p2 left 3 s after the snapshot named it: %+v, want seen", v)
	}
	if acks := st.ackList(); len(acks) != 1 || acks[0].Key().PaneID != "w1:p2" {
		t.Errorf("acknowledgements %+v, want w1:p2's alone", acks)
	}
}

// While reconnecting no pane is in view, and a pane_focused that arrives on
// the new connection before its first snapshot is no leave of the pane
// focused before the loss: that focus was dropped, and the Agents are not
// confirmed until the snapshot.
func TestFocusEventBeforeFirstSnapshot(t *testing.T) {
	snap := snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusDone), agentPane("w1:p3", core.StatusIdle))
	h, _, st, clock := startFocus(t, snap, nil)
	h.focusOn("w1:p1")
	clock.advance(10 * time.Second)
	h.fake.setSnapshot(focused(snap, "w1:p1"))
	old := h.fake.lifecycleSub()
	hold := h.fake.holdNext()
	h.loseLifecycle()
	deadline := time.Now().Add(wait)
	for h.fake.lifecycleSub() == old {
		if time.Now().After(deadline) {
			t.Fatal("no new lifecycle subscription")
		}
		time.Sleep(time.Millisecond)
	}
	h.fake.awaitSnapshotCall()
	if v := h.viewOf("w1:p1"); v.InView {
		t.Errorf("w1:p1 while reconnecting %+v, want not in view", v)
	}
	h.focusOn("w1:p3")
	h.fake.setSnapshot(focused(snap, "w1:p3")) // as herdr shows it from now on
	close(hold)
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	h.settle()
	if v := h.viewOf("w1:p1"); v.Seen || v.InView {
		t.Errorf("w1:p1 %+v, want neither seen nor in view", v)
	}
	if v := h.viewOf("w1:p3"); !v.InView {
		t.Errorf("w1:p3 %+v, want in view", v)
	}
	if n := len(st.ackList()); n != 0 {
		t.Errorf("%d acknowledgements, want none", n)
	}
}

// The snapshot that ends a resync resets focus as a connection's first one
// does (ADR-0015): any focus held before it is dropped without a leave, and
// the dwell of the pane it names starts when it is applied, even when that
// pane was already focused.
func TestFocusResyncResets(t *testing.T) {
	snap := snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusDone), agentPane("w1:p3", core.StatusDone))
	resync := func(h *harness) {
		h.t.Helper()
		h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthClosed, Err: errors.New("EOF")})
		h.rec.awaitCount(h.t, "resync closed", 1)
		h.fake.awaitStatusSubs("w1:p1", 2)
		h.settle()
	}
	// The resync moves focus to another pane: no move is held, so the event
	// that follows it is not a leave of the pane focused before.
	t.Run("another pane", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		resync(h)
		if v := h.viewOf("w1:p3"); !v.InView {
			t.Errorf("w1:p3 after the resync %+v, want in view", v)
		}
		h.focusOn("w1:p3")
		if v := h.viewOf("w1:p2"); v.Seen || v.InView {
			t.Errorf("w1:p2 %+v, want neither seen nor in view: the resync dropped its focus", v)
		}
		if n := len(st.ackList()); n != 0 {
			t.Errorf("%d acknowledgements, want none", n)
		}
	})
	// The resync names the pane already focused: its dwell starts again.
	t.Run("same pane", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p2"))
		resync(h)
		clock.advance(time.Second)
		h.focusOn("w1:p3")
		if v := h.viewOf("w1:p2"); v.Seen {
			t.Errorf("w1:p2 left 1 s after the resync: %+v, want not seen", v)
		}
		if n := len(st.ackList()); n != 0 {
			t.Errorf("%d acknowledgements, want none", n)
		}
	})
}

// A snapshot whose focus differs from the engine's moves it without a leave
// (another client's view); a read that started before a pane_focused is
// older than it, and its focus is ignored.
func TestFocusFromSnapshot(t *testing.T) {
	snap := snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusDone), agentPane("w1:p3", core.StatusDone))
	t.Run("disagrees", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		h.reconcileRead()
		if v := h.viewOf("w1:p2"); v.Seen || v.InView {
			t.Errorf("w1:p2 %+v, want neither seen nor in view", v)
		}
		if v := h.viewOf("w1:p3"); !v.InView {
			t.Errorf("w1:p3 %+v, want in view", v)
		}
		if n := len(st.ackList()); n != 0 {
			t.Fatalf("%d acknowledgements, want none", n)
		}
		// The same focus again changes nothing; its dwell counts from the
		// first read naming it.
		clock.advance(2 * time.Second)
		h.reconcileRead()
		clock.advance(time.Second)
		h.focusOn("w1:p1")
		if v := h.viewOf("w1:p3"); !v.Seen {
			t.Errorf("w1:p3 left 3 s after a snapshot named it: %+v, want seen", v)
		}
	})
	// The read is newer than a pane_focused still on its way, the two
	// arriving on separate connections: the event that confirms the move
	// the snapshot showed is the leave, with the dwell held when the
	// snapshot was applied. Only the very next event can confirm it.
	t.Run("fresh read before its event", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		h.reconcileRead()
		if n := len(st.ackList()); n != 0 {
			t.Fatalf("%d acknowledgements before the event, want none", n)
		}
		h.focusOn("w1:p3")
		if v := h.viewOf("w1:p2"); !v.Seen || v.InView {
			t.Errorf("w1:p2 %+v, want seen, not in view: the event confirms the leave", v)
		}
		if acks := focusAcks(st); len(acks) != 1 || acks[0].Key().PaneID != "w1:p2" {
			t.Errorf("acknowledgements %+v, want one herdr_focus mark of w1:p2", acks)
		}
		if v := h.viewOf("w1:p3"); !v.InView || v.Seen {
			t.Errorf("w1:p3 %+v, want in view, not seen", v)
		}
	})
	// Any event naming another pane than the one the held move left is
	// that pane's leave: the events arrive in herdr's order, so focus left
	// it. The event for w1:p1 is also a leave of w1:p3, after no dwell.
	t.Run("another event first", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		h.reconcileRead()
		h.focusOn("w1:p1")
		h.focusOn("w1:p3")
		if v := h.viewOf("w1:p2"); !v.Seen {
			t.Errorf("w1:p2 %+v, want seen", v)
		}
		if v := h.viewOf("w1:p3"); v.Seen || !v.InView {
			t.Errorf("w1:p3 %+v, want in view, not seen", v)
		}
		if acks := focusAcks(st); len(acks) != 1 || acks[0].Key().PaneID != "w1:p2" {
			t.Errorf("acknowledgements %+v, want one herdr_focus mark of w1:p2", acks)
		}
	})
	// A second read agreeing with the first keeps the held move.
	t.Run("agreeing second read", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		h.reconcileRead()
		h.reconcileRead()
		h.focusOn("w1:p3")
		if acks := focusAcks(st); len(acks) != 1 || acks[0].Key().PaneID != "w1:p2" {
			t.Errorf("acknowledgements %+v, want one herdr_focus mark of w1:p2", acks)
		}
	})
	// A second read naming yet another pane moves the held move on; the
	// pane left first is still the one an event leaves.
	t.Run("second read further on", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		h.reconcileRead()
		h.fake.setSnapshot(focused(snap, "w1:p1"))
		h.reconcileRead()
		h.focusOn("w1:p3")
		h.focusOn("w1:p1")
		if acks := focusAcks(st); len(acks) != 1 || acks[0].Key().PaneID != "w1:p2" {
			t.Errorf("acknowledgements %+v, want one herdr_focus mark of w1:p2", acks)
		}
		if v := h.viewOf("w1:p1"); !v.InView {
			t.Errorf("w1:p1 %+v, want in view", v)
		}
	})
	// The pane moved to has its own dwell: its Agent starting a new turn
	// does not change the leave of the pane moved from.
	t.Run("restart of the pane moved to", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		h.reconcileRead()
		h.say("w1:p3", core.StatusWorking)
		h.focusOn("w1:p3")
		if acks := focusAcks(st); len(acks) != 1 || acks[0].Key().PaneID != "w1:p2" {
			t.Errorf("acknowledgements %+v, want one herdr_focus mark of w1:p2", acks)
		}
	})
	// The pane moved from starting a new finish after the read discards the
	// move: the dwell held was spent on the finish before.
	t.Run("restart of the pane moved from", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(10 * time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		h.reconcileRead()
		h.say("w1:p2", core.StatusWorking)
		h.say("w1:p2", core.StatusDone)
		h.focusOn("w1:p3")
		if n := len(focusAcks(st)); n != 0 {
			t.Errorf("%d herdr_focus rows, want none: w1:p2's finish began after the read", n)
		}
	})
	t.Run("short dwell before the read", func(t *testing.T) {
		h, _, st, clock := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		clock.advance(seenDwell - time.Second)
		h.fake.setSnapshot(focused(snap, "w1:p3"))
		h.reconcileRead()
		clock.advance(10 * time.Second)
		h.focusOn("w1:p3")
		if n := len(st.ackList()); n != 0 {
			t.Errorf("%d acknowledgements, want none: w1:p2 had %s when the read moved focus", n, seenDwell-time.Second)
		}
	})
	t.Run("stale read", func(t *testing.T) {
		h, _, st, _ := startFocus(t, snap, nil)
		h.focusOn("w1:p2")
		h.fake.setSnapshot(focused(snap, "w1:p2"))
		h.fake.setGated(true)
		h.reconcileTick()
		h.fake.awaitSnapshotCall()
		h.focusOn("w1:p3")
		h.fake.release <- struct{}{}
		h.fake.setGated(false)
		h.settle()
		if v := h.viewOf("w1:p3"); !v.InView {
			t.Errorf("w1:p3 %+v, want in view: the event is newer than the read", v)
		}
		if v := h.viewOf("w1:p2"); v.InView {
			t.Errorf("w1:p2 %+v, want not in view", v)
		}
		if n := len(st.ackList()); n != 0 {
			t.Errorf("%d acknowledgements, want none", n)
		}
	})
}

// The prompt races the leave: status and focus events come on different
// connections. Either order ends Working, not seen.
func TestFocusWorkingRacesLeave(t *testing.T) {
	for _, workingFirst := range []bool{true, false} {
		name := "leave first"
		if workingFirst {
			name = "working first"
		}
		t.Run(name, func(t *testing.T) {
			h, _, st, clock := startFocus(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)), nil)
			h.focusOn("w1:p1")
			clock.advance(5 * time.Second)
			if workingFirst {
				h.say("w1:p1", core.StatusWorking)
				h.focusOn("w1:p2")
			} else {
				h.focusOn("w1:p2")
				h.say("w1:p1", core.StatusWorking)
			}
			if v := h.viewOf("w1:p1"); v.Status != core.StatusWorking || v.Class != core.ClassWorking || v.Seen {
				t.Errorf("w1:p1 %+v, want Working, not seen", v)
			}
			want := 0
			if !workingFirst {
				want = 1 // of the finish the working then ended
			}
			if n := len(focusAcks(st)); n != want {
				t.Errorf("%d herdr_focus rows, want %d", n, want)
			}
		})
	}
}

// A leave during a Profile hold of the pane left marks nothing; a later
// leave of the same finish after 3 s more does.
func TestFocusDuringProfileHold(t *testing.T) {
	setProfileTimeout(t, wait)
	setReconcileInterval(t, time.Hour)
	clock := newManualClock()
	fake := newFakeClient(t, oneAgent("claude", core.StatusDone))
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, func(o *Options, r *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
		o.Clock = clock.now
		r.profileLines = true
	})
	h.rec.expect(t, "agent w1:p1 claude done profile=cldw")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.focusOn("w1:p1")
	clock.advance(10 * time.Second)
	env.hold()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.awaitProcessCalls(2)
	h.focusOn("w1:p3")
	if n := len(st.ackList()); n != 0 {
		t.Fatalf("%d acknowledgements during a hold, want none", n)
	}
	env.unblock()
	h.settle()
	h.focusOn("w1:p1")
	clock.advance(seenDwell)
	h.focusOn("w1:p3")
	if v := viewOfP1(t, h); !v.Seen {
		t.Errorf("w1:p1 left after the hold ended %+v, want seen", v)
	}
	if n := len(focusAcks(st)); n != 1 {
		t.Errorf("%d herdr_focus rows, want 1", n)
	}
}

// A failed write marks nothing and is logged: the view is as if no leave
// happened, apart from where focus is. A later leave of the same finish
// marks it.
func TestFocusStoreFails(t *testing.T) {
	logs := &logSink{}
	h, _, st, clock := startFocus(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)),
		func(o *Options, _ *recorder) {
			o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		})
	before := h.viewOf("w1:p1")
	st.mu.Lock()
	st.ackErr = errBoom
	st.mu.Unlock()
	h.focusOn("w1:p1")
	clock.advance(seenDwell)
	h.focusOn("w1:p2")
	after := h.viewOf("w1:p1")
	before.InView = false
	if after != before {
		t.Errorf("after a failed write %+v, want %+v", after, before)
	}
	if e := logs.entries(t, "ERROR", "acknowledgement not recorded"); len(e) != 1 || e[0]["source"] != string(core.AckSourceHerdrFocus) {
		t.Errorf("error log %v, want one naming source herdr_focus", e)
	}
	st.mu.Lock()
	st.ackErr = nil
	st.mu.Unlock()
	h.focusOn("w1:p1")
	clock.advance(seenDwell)
	h.focusOn("w1:p2")
	if v := h.viewOf("w1:p1"); !v.Seen {
		t.Errorf("after the store recovered %+v, want seen", v)
	}
	if n := len(focusAcks(st)); n != 1 {
		t.Errorf("%d herdr_focus rows, want 1", n)
	}
}

// concClock reads a millisecond later at every reading, plus whatever the
// test adds, so every finish has its own start.
type concClock struct {
	n    atomic.Int64
	jump atomic.Int64
}

func (c *concClock) now() time.Time {
	start := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	return start.Add(time.Duration(c.n.Add(1))*time.Millisecond + time.Duration(c.jump.Load()))
}

// TestFocusConcurrent sends Board marks, undos and bulk requests from 8
// goroutines while herdr changes the same Agents' Status and moves focus
// among their panes, and the clock jumps. Run it with -race -count=50
// -cpu=1,4,8.
func TestFocusConcurrent(t *testing.T) {
	setReconcileInterval(t, time.Hour)
	ids := []string{"w1:p1", "w1:p2", "w1:p3", "w1:p4"}
	var ps []core.PaneObservation
	for _, id := range ids {
		ps = append(ps, agentPane(id, core.StatusDone))
	}
	clock := &concClock{}
	fake := newFakeClient(t, snapOf(ps...))
	st := newFakeStore(fake)
	h, _ := startViewed(t, fake, func(o *Options, _ *recorder) {
		o.Store = st
		o.Clock = clock.now
	})
	h.awaitAgents(len(ids))
	life := h.fake.lifecycleSub()
	stop := make(chan struct{})
	var herdrWG sync.WaitGroup
	herdrWG.Add(2)
	go func() { // Status
		defer herdrWG.Done()
		r := rand.New(rand.NewPCG(1, 2))
		statuses := []core.Status{core.StatusDone, core.StatusIdle, core.StatusWorking, core.StatusDone, core.StatusBlocked}
		for {
			select {
			case <-stop:
				return
			default:
			}
			id := ids[r.IntN(len(ids))]
			h.fake.statusSub(id).event(core.StatusEvent{PaneID: id, Status: statuses[r.IntN(len(statuses))], AgentProgram: "claude"})
			time.Sleep(time.Duration(r.IntN(500)) * time.Microsecond)
		}
	}()
	go func() { // focus, and the clock
		defer herdrWG.Done()
		r := rand.New(rand.NewPCG(3, 4))
		for {
			select {
			case <-stop:
				return
			default:
			}
			clock.jump.Add(int64(time.Duration(r.IntN(5000)) * time.Millisecond))
			id := ids[r.IntN(len(ids))]
			life.event(core.LifecycleEvent{Kind: core.LifecycleFocused, PaneID: id, WorkspaceID: "w1"})
			time.Sleep(time.Duration(r.IntN(500)) * time.Microsecond)
		}
	}()
	typed := func(err error) bool {
		return err == nil || errors.Is(err, ErrStaleView) || errors.Is(err, ErrNotFinished)
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(uint64(g), 7))
			for range 25 {
				views := h.engine.View().Agents
				var items []AckItem
				switch r.IntN(3) {
				case 0:
					items = []AckItem{itemOf(views[r.IntN(len(views))])}
				default:
					for _, v := range views {
						if r.IntN(2) == 0 {
							items = append(items, itemOf(v))
						}
					}
				}
				if len(items) == 0 {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), wait)
				got, err := h.engine.Acknowledge(ctx, AckRequest{Seen: r.IntN(3) != 0, Items: items})
				cancel()
				if !typed(err) {
					t.Errorf("reply error %v, want a typed one", err)
					return
				}
				if err == nil && len(got) != len(items) {
					t.Errorf("%d views for %d items", len(got), len(items))
				}
				if err != nil && got != nil {
					t.Errorf("views %v beside error %v", got, err)
				}
				// Paced, so herdr's changes and focus moves interleave with
				// the requests, and some finishes are left to a leave.
				time.Sleep(time.Duration(r.IntN(4000)) * time.Microsecond)
			}
		}()
	}
	wg.Wait()
	close(stop)
	herdrWG.Wait()
	h.settle()
	for _, v := range h.engine.View().Agents {
		ts, _ := st.History(context.Background(), v.Key)
		acks, _ := st.Acknowledgements(context.Background(), v.Key)
		f := core.Fold(ts)
		if want := core.SeenAt(f, acks, ts[len(ts)-1], nil); v.Seen != want || v.WaitingSince != f.Since {
			t.Errorf("%s seen %v since %d; the record says seen %v since %d", v.Key.PaneID, v.Seen, v.WaitingSince, want, f.Since)
		}
	}
	type finish struct {
		key   core.AgentKey
		since int64
	}
	auto, board := map[finish]int{}, map[finish]bool{}
	for _, a := range st.ackList() {
		f := finish{a.Key(), a.FinishedSince()}
		switch a.Source() {
		case core.AckSourceHerdrFocus:
			if board[f] {
				t.Errorf("a herdr_focus row for %s since %d after a board row", f.key.PaneID, f.since)
			}
			auto[f]++
			if auto[f] > 1 {
				t.Errorf("%d herdr_focus rows for %s since %d", auto[f], f.key.PaneID, f.since)
			}
		case core.AckSourceBoard:
			board[f] = true
		}
	}
	if len(auto) == 0 {
		t.Error("no herdr_focus row was written: the run exercised no automatic mark")
	}
}
