package engine

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/store"
)

// snapOf is a snapshot of panes, herdr 0.9.5.
func snapOf(ps ...core.PaneObservation) core.Snapshot {
	return core.Snapshot{HerdrVersion: "0.9.5", Panes: ps}
}

// agentCount is how many of s's panes hold an Agent.
func agentCount(s core.Snapshot) int {
	n := 0
	for _, p := range s.Panes {
		if p.Agent != nil {
			n++
		}
	}
	return n
}

// awaitAgents waits until the View holds n Agents, every one subscribed, and
// the engine has settled.
func (h *harness) awaitAgents(n int) {
	h.t.Helper()
	deadline := time.Now().Add(wait)
	for len(h.engine.View().Agents) != n {
		if time.Now().After(deadline) {
			h.t.Fatalf("view holds %d agents, want %d", len(h.engine.View().Agents), n)
		}
		time.Sleep(time.Millisecond)
	}
	for _, v := range h.engine.View().Agents {
		h.fake.awaitSubscribe(v.Key.PaneID)
	}
	h.settle()
}

// viewOf is pane id as the latest View shows it.
func (h *harness) viewOf(id string) core.AgentView {
	h.t.Helper()
	for _, v := range h.engine.View().Agents {
		if v.Key.PaneID == id {
			return v
		}
	}
	h.t.Fatalf("%s not in the view", id)
	return core.AgentView{}
}

// say sends a status event for pane id and waits until the View shows it.
func (h *harness) say(id string, s core.Status) core.AgentView {
	h.t.Helper()
	h.fake.statusSub(id).event(core.StatusEvent{PaneID: id, Status: s, AgentProgram: "claude"})
	deadline := time.Now().Add(wait)
	for {
		if v := h.viewOf(id); v.Status == s {
			return v
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("%s never shown %s", id, s)
		}
		time.Sleep(time.Millisecond)
	}
}

// ack sends one request for items, marks when seen is set, undoes otherwise.
func (h *harness) ack(seen bool, items ...AckItem) ([]core.AgentView, error) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	return h.engine.Acknowledge(ctx, AckRequest{Seen: seen, Items: items})
}

// itemOf names the finish v shows.
func itemOf(v core.AgentView) AckItem {
	return AckItem{Key: v.Key, WaitingSince: v.WaitingSince}
}

// startAck starts a viewed, recording engine on snap and waits for its
// Agents.
func startAck(t *testing.T, snap core.Snapshot) (*harness, *watcher, *fakeStore) {
	t.Helper()
	fake := newFakeClient(t, snap)
	st := newFakeStore(fake)
	h, w := startViewed(t, fake, func(o *Options, _ *recorder) { o.Store = st })
	h.awaitAgents(agentCount(snap))
	return h, w, st
}

// unchanged checks that nothing was published or recorded since seq and n
// acknowledgements, once the engine has handled everything sent to it.
func (h *harness) unchanged(st *fakeStore, seq uint64, n int) {
	h.t.Helper()
	h.agents()
	if got := h.engine.View().Seq; got != seq {
		h.t.Errorf("view seq %d, want %d: published", got, seq)
	}
	if got := len(st.ackList()); got != n {
		h.t.Errorf("%d acknowledgements recorded, want %d", got, n)
	}
}

func TestAckMark(t *testing.T) {
	h, w, st := startAck(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)))
	before := h.viewOf("w1:p1")
	if before.Class != core.ClassFinished || before.Seen || before.SortKey.ClassRank != 1 || before.WaitingSince <= 0 {
		t.Fatalf("w1:p1 before %+v, want Finished, not seen, rank 1", before)
	}
	n := len(w.all())
	got, err := h.ack(true, itemOf(before))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Seen || got[0].SortKey.ClassRank != 2 || got[0].Class != core.ClassFinished ||
		got[0].WaitingSince != before.WaitingSince {
		t.Fatalf("reply %+v, want w1:p1 seen, Finished, rank 2, the same waiting_since", got)
	}
	if v := h.viewOf("w1:p1"); v != got[0] {
		t.Errorf("view %+v, reply %+v: the publication precedes the reply", v, got[0])
	}
	acks := st.ackList()
	if len(acks) != 1 || acks[0].Key() != before.Key || acks[0].FinishedSince() != before.WaitingSince || !acks[0].Seen() {
		t.Fatalf("acknowledgements %+v, want one seen row for w1:p1's finish", acks)
	}
	h.agents()
	time.Sleep(quiet)
	ps := w.all()[n:]
	if len(ps) != 1 || len(ps[0].Changes) != 1 || ps[0].Changes[0].Kind != ChangeAgent || ps[0].Changes[0].Agent != got[0] {
		t.Fatalf("publications %s, want one with one agent change, the reply's view", describe(ps))
	}
	// Marking it again is a no-op: nothing written, nothing published.
	seq := h.engine.View().Seq
	again, err := h.ack(true, itemOf(before))
	if err != nil || len(again) != 1 || again[0] != got[0] {
		t.Fatalf("mark again: %+v, %v", again, err)
	}
	h.unchanged(st, seq, 1)
}

func TestAckRefused(t *testing.T) {
	// Epoch 3, kept, so an older epoch exists.
	st := newFakeStore(nil)
	snap := snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusBlocked),
		agentPane("w1:p3", core.StatusIdle), agentPane("w1:p4", core.StatusWorking))
	h := restartOn(t, snap, st, nil)
	h.awaitAgents(4)
	p1 := itemOf(h.viewOf("w1:p1"))
	older := p1
	older.Key.Epoch = 2
	unknown := p1
	unknown.Key.PaneID = "w1:p9"
	otherHost := p1
	otherHost.Key.Host = "linux"
	off := p1
	off.WaitingSince++
	cases := []struct {
		name string
		req  AckRequest
		want error
	}{
		{"one millisecond off", AckRequest{Seen: true, Items: []AckItem{off}}, ErrStaleView},
		{"unknown pane", AckRequest{Seen: true, Items: []AckItem{unknown}}, ErrUnknownAgent},
		{"older epoch", AckRequest{Seen: true, Items: []AckItem{older}}, ErrUnknownAgent},
		{"another host", AckRequest{Seen: true, Items: []AckItem{otherHost}}, ErrUnknownAgent},
		{"undo of an unknown pane", AckRequest{Items: []AckItem{unknown}}, ErrUnknownAgent},
		{"needs you", AckRequest{Seen: true, Items: []AckItem{itemOf(h.viewOf("w1:p2"))}}, ErrNotFinished},
		{"idle", AckRequest{Seen: true, Items: []AckItem{itemOf(h.viewOf("w1:p3"))}}, ErrNotFinished},
		{"working", AckRequest{Seen: true, Items: []AckItem{itemOf(h.viewOf("w1:p4"))}}, ErrNotFinished},
		{"no items", AckRequest{Seen: true}, ErrBadRequest},
		{"a key twice", AckRequest{Seen: true, Items: []AckItem{p1, p1}}, ErrBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seq := h.engine.View().Seq
			views, err := h.engine.Acknowledge(context.Background(), c.req)
			if !errors.Is(err, c.want) || views != nil {
				t.Errorf("got %v, %v; want %v", views, err, c.want)
			}
			h.unchanged(st, seq, 0)
		})
	}
	var se *StaleError
	if _, err := h.ack(true, off); !errors.As(err, &se) || !slices.Equal(se.Keys, []core.AgentKey{p1.Key}) {
		t.Errorf("stale error %v, want a *StaleError naming w1:p1", err)
	}
	if h.viewOf("w1:p1").Seen {
		t.Error("w1:p1 seen after refusals")
	}
	// The right finish is accepted.
	if _, err := h.ack(true, p1); err != nil {
		t.Fatal(err)
	}
}

func TestAckRefusedDuringProfileHold(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, st := startProfiledStore(t, oneAgent("claude", core.StatusDone), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
	})
	h.rec.expect(t, "agent w1:p1 claude done profile=cldw")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	item := itemOf(viewOfP1(t, h))
	env.hold()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.awaitProcessCalls(2)
	seq := h.engine.View().Seq
	if _, err := h.ack(true, item); !errors.Is(err, ErrStaleView) {
		t.Fatalf("mark during a hold: %v, want ErrStaleView", err)
	}
	h.unchanged(st, seq, 0)
	// Once the lookup settles, the same finish can be marked.
	env.unblock()
	h.settle()
	if v, err := h.ack(true, item); err != nil || !v[0].Seen {
		t.Fatalf("mark after the hold: %+v, %v", v, err)
	}
}

// An undo that would write while the pane's rows are held is refused as
// stale: the held view still shows the Agent seen, so a reply or publication
// could not show the undo. Once the hold ends the same undo goes through.
func TestAckUndoRefusedDuringProfileHold(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, st := startProfiledStore(t, oneAgent("claude", core.StatusDone), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
	})
	h.rec.expect(t, "agent w1:p1 claude done profile=cldw")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	item := itemOf(viewOfP1(t, h))
	if v, err := h.ack(true, item); err != nil || !v[0].Seen {
		t.Fatalf("mark: %+v, %v", v, err)
	}
	env.hold()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.awaitProcessCalls(2)
	seq := h.engine.View().Seq
	if _, err := h.ack(false, item); !errors.Is(err, ErrStaleView) {
		t.Fatalf("undo during a hold: %v, want ErrStaleView", err)
	}
	h.unchanged(st, seq, 1)
	if !viewOfP1(t, h).Seen {
		t.Error("the refused undo cleared seen")
	}
	env.unblock()
	h.settle()
	if v, err := h.ack(false, item); err != nil || v[0].Seen || v[0].SortKey.ClassRank != 1 {
		t.Fatalf("undo after the hold: %+v, %v", v, err)
	}
	if acks := st.ackList(); len(acks) != 2 || acks[1].Seen() {
		t.Errorf("acknowledgements %+v, want the mark, then its undo", acks)
	}
}

func TestAckRefusedBackground(t *testing.T) {
	fastBackground(t)
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	fake := newFakeClient(t, oneAgent("claude", core.StatusDone))
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	st := newFakeStore(fake)
	h, w := startViewed(t, fake, func(o *Options, _ *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
		o.Children = kids.read
		o.Store = st
	})
	v := awaitBackground(w, true)
	if _, err := h.ack(true, itemOf(v)); !errors.Is(err, ErrNotFinished) {
		t.Fatalf("mark of a Background Agent: %v, want ErrNotFinished", err)
	}
	if n := len(st.ackList()); n != 0 {
		t.Errorf("%d acknowledgements recorded, want none", n)
	}
}

func TestAckBulkAllOrNothing(t *testing.T) {
	h, _, st := startAck(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusDone),
		agentPane("w1:p3", core.StatusDone)))
	items := []AckItem{itemOf(h.viewOf("w1:p1")), itemOf(h.viewOf("w1:p2")), itemOf(h.viewOf("w1:p3"))}
	stale := slices.Clone(items)
	stale[1].WaitingSince--
	seq := h.engine.View().Seq
	var se *StaleError
	if _, err := h.ack(true, stale...); !errors.As(err, &se) || !slices.Equal(se.Keys, []core.AgentKey{items[1].Key}) ||
		!errors.Is(err, ErrStaleView) {
		t.Fatalf("bulk with a stale item: %v, want a stale error naming w1:p2 alone", err)
	}
	h.unchanged(st, seq, 0)

	views, err := h.ack(true, items[2], items[0], items[1])
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(views); !slices.Equal(got, []string{"w1:p3", "w1:p1", "w1:p2"}) {
		t.Errorf("reply %v, want the request's order", got)
	}
	for _, v := range views {
		if !v.Seen {
			t.Errorf("%s not seen", v.Key.PaneID)
		}
	}
	if n := len(st.ackList()); n != 3 {
		t.Errorf("%d acknowledgements, want 3", n)
	}
	// A bulk undo writes one row per Agent acknowledged now, in one call.
	if _, err := h.ack(false, items...); err != nil {
		t.Fatal(err)
	}
	acks := st.ackList()
	if len(acks) != 6 || acks[3].Seen() || acks[4].Seen() || acks[5].Seen() {
		t.Errorf("acknowledgements %+v, want three undone", acks)
	}
}

func TestAckLastsUntilTheFinishEnds(t *testing.T) {
	h, _, st := startAck(t, snapOf(agentPane("w1:p1", core.StatusWorking), agentPane("w1:p2", core.StatusIdle)))
	finished := h.say("w1:p1", core.StatusDone)
	markP1 := func() core.AgentView {
		t.Helper()
		v, err := h.ack(true, itemOf(h.viewOf("w1:p1")))
		if err != nil || !v[0].Seen {
			t.Fatalf("mark: %+v, %v", v, err)
		}
		return v[0]
	}
	markP1()
	// done to idle within the finish keeps it.
	if v := h.say("w1:p1", core.StatusIdle); !v.Seen || v.WaitingSince != finished.WaitingSince || v.SortKey.ClassRank != 2 {
		t.Errorf("after done to idle %+v, want still seen", v)
	}
	if v := h.say("w1:p1", core.StatusDone); !v.Seen {
		t.Errorf("after idle to done %+v, want still seen", v)
	}
	// A new turn ends it, with nothing written.
	if v := h.say("w1:p1", core.StatusWorking); v.Seen {
		t.Errorf("working %+v, want not seen", v)
	}
	// Finishing again is a new finish, not seen.
	if v := h.say("w1:p1", core.StatusDone); v.Seen || v.WaitingSince == finished.WaitingSince || v.SortKey.ClassRank != 1 {
		t.Errorf("finished again %+v, want Finished and not seen", v)
	}
	// Needs you ends it.
	markP1()
	if v := h.say("w1:p1", core.StatusBlocked); v.Seen {
		t.Errorf("blocked %+v, want not seen", v)
	}
	if v := h.say("w1:p1", core.StatusDone); v.Seen {
		t.Errorf("done after blocked %+v, want not seen", v)
	}
	if n := len(st.ackList()); n != 2 {
		t.Errorf("%d acknowledgements, want only the two marks", n)
	}

	// A new Agent in the pane (another terminal: its first row has no
	// previous Status) is not seen.
	markP1()
	s := snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle))
	s.Panes[0].TerminalID = "t-other"
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleMoved, PaneID: "w1:p1"})
	h.rec.awaitCount(t, "removed w1:p1", 1)
	h.fake.awaitStatusSubs("w1:p1", 2)
	h.settle()
	if v := h.viewOf("w1:p1"); v.Seen || v.Class != core.ClassFinished {
		t.Errorf("new Agent %+v, want Finished, not seen", v)
	}
	last := st.rowList()[len(st.rowList())-1]
	if _, ok := last.Previous(); ok || last.Key().PaneID != "w1:p1" {
		t.Errorf("last row %s, want the new Agent's first", row(last))
	}

	// An epoch increment ends it.
	markP1()
	h.fake.setSnapshot(restarted())
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 2 incremented", 1)
	h.awaitAgents(2)
	if v := h.viewOf("w1:p1"); v.Seen || v.Key.Epoch != 2 {
		t.Errorf("after the epoch increment %+v, want epoch 2, not seen", v)
	}
	if n := len(st.ackList()); n != 4 {
		t.Errorf("%d acknowledgements, want only the four marks", n)
	}
}

func TestAckUndo(t *testing.T) {
	h, w, st := startAck(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)))
	item := itemOf(h.viewOf("w1:p1"))
	if _, err := h.ack(true, item); err != nil {
		t.Fatal(err)
	}
	n := len(w.all())
	views, err := h.ack(false, item)
	if err != nil {
		t.Fatal(err)
	}
	if v := views[0]; v.Seen || v.Class != core.ClassFinished || v.SortKey.ClassRank != 1 {
		t.Errorf("after the undo %+v, want Finished, rank 1", v)
	}
	acks := st.ackList()
	if len(acks) != 2 || acks[1].Seen() || acks[1].FinishedSince() != item.WaitingSince {
		t.Errorf("acknowledgements %+v, want the mark, then its undo", acks)
	}
	w.awaitAfter("the undo", n, hasKind(ChangeAgent))
	// Undoing again, or an older finish, writes nothing.
	seq := h.engine.View().Seq
	if _, err := h.ack(false, item); err != nil {
		t.Fatal(err)
	}
	h.unchanged(st, seq, 2)

	// An undo after the Agent went working succeeds, writes and publishes
	// nothing.
	if _, err := h.ack(true, item); err != nil {
		t.Fatal(err)
	}
	h.say("w1:p1", core.StatusWorking)
	seq = h.engine.View().Seq
	views, err = h.ack(false, item)
	if err != nil || len(views) != 1 || views[0].Seen || views[0].Status != core.StatusWorking {
		t.Fatalf("undo after working: %+v, %v", views, err)
	}
	h.unchanged(st, seq, 3)
}

func TestAckRecordingOff(t *testing.T) {
	snap := oneAgent("claude", core.StatusDone)
	t.Run("no store", func(t *testing.T) {
		h := startEngine(t, newFakeClient(t, snap))
		h.awaitAgents(1)
		if _, err := h.ack(true, itemOf(h.viewOf("w1:p1"))); !errors.Is(err, ErrRecordingOff) {
			t.Errorf("got %v, want ErrRecordingOff", err)
		}
	})
	t.Run("state not loaded", func(t *testing.T) {
		fake := newFakeClient(t, snap)
		st := newFakeStore(fake)
		st.loadErr = errBoom
		h := startRecording(t, fake, st, nil)
		h.awaitAgents(1)
		if _, err := h.ack(true, itemOf(h.viewOf("w1:p1"))); !errors.Is(err, ErrRecordingOff) {
			t.Errorf("got %v, want ErrRecordingOff", err)
		}
		if n := len(st.ackList()); n != 0 {
			t.Errorf("%d acknowledgements, want none", n)
		}
	})
}

func TestAckStoreFails(t *testing.T) {
	h, w, st := startAck(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)))
	before := h.viewOf("w1:p1")
	st.mu.Lock()
	st.ackErr = errBoom
	st.mu.Unlock()
	seq := h.engine.View().Seq
	if views, err := h.ack(true, itemOf(before)); !errors.Is(err, errBoom) || views != nil {
		t.Fatalf("got %v, %v; want the store's error", views, err)
	}
	h.unchanged(st, seq, 0)
	if v := h.viewOf("w1:p1"); v != before {
		t.Errorf("view %+v, want unchanged %+v", v, before)
	}
	// The next publication is as if no request was made.
	n := len(w.all())
	h.say("w1:p1", core.StatusIdle)
	ps := w.awaitAfter("the transition", n, hasKind(ChangeTransition))
	for _, c := range ps[len(ps)-1].Changes {
		if c.Agent.Seen {
			t.Errorf("change %+v seen", c)
		}
	}
}

func TestAckInRunReconnect(t *testing.T) {
	h, _, st := startAck(t, snapOf(agentPane("w1:p1", core.StatusDone), agentPane("w1:p2", core.StatusIdle)))
	item := itemOf(h.viewOf("w1:p1"))
	if _, err := h.ack(true, item); err != nil {
		t.Fatal(err)
	}
	// A reconnect keeping the epoch, the Status unchanged: still seen.
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 1)
	h.fake.awaitStatusSubs("w1:p1", 2)
	h.settle()
	if v := h.viewOf("w1:p1"); !v.Seen || v.WaitingSince != item.WaitingSince {
		t.Errorf("after a kept reconnect %+v, want seen", v)
	}
	// A pane first tracked by a kept reconnect is seeded from the record,
	// its acknowledgement with it.
	st.seed(storedIn(t, 1, "claude", "w1:p4", "", core.StatusWorking, 100, core.ProvenanceLive),
		storedIn(t, 1, "claude", "w1:p4", core.StatusWorking, core.StatusDone, 200, core.ProvenanceLive))
	p4 := core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p4"}
	seedAck(t, st, p4, 200, true, 300)
	// The Status changed while away: not seen.
	s := snapOf(agentPane("w1:p1", core.StatusWorking), agentPane("w1:p2", core.StatusIdle), agentPane("w1:p4", core.StatusIdle))
	h.fake.setSnapshot(s)
	h.loseLifecycle()
	h.rec.awaitCount(t, "reconnected 0.9.5 epoch 1 kept", 2)
	h.awaitAgents(3)
	if v := h.viewOf("w1:p1"); v.Seen {
		t.Errorf("after a kept reconnect showing working %+v, want not seen", v)
	}
	if v := h.viewOf("w1:p4"); !v.Seen || v.WaitingSince != 200 {
		t.Errorf("w1:p4 %+v, want seen since 200 from the record (done to idle keeps it)", v)
	}
	if n := len(st.ackList()); n != 2 {
		t.Errorf("%d acknowledgements, want the mark and the seeded one", n)
	}
}

// seedAck adds an acknowledgement as if a previous run had recorded it.
func seedAck(t *testing.T, st *fakeStore, key core.AgentKey, since int64, seen bool, at int64) {
	t.Helper()
	a, err := core.NewAcknowledgement(core.AcknowledgementInput{Key: key, FinishedSince: since, Seen: seen, ObservedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.acks = append(st.acks, a)
	st.mu.Unlock()
}

func TestAckRestartFromRecord(t *testing.T) {
	// A previous run recorded w1:p1 finishing at 200 and its mark; the
	// Daemon restarts on the same herdr.
	seeded := func(t *testing.T) *fakeStore {
		st := newFakeStore(nil)
		st.seed(stored(t, "w1:p1", "", core.StatusWorking, 100, core.ProvenanceLive),
			stored(t, "w1:p1", core.StatusWorking, core.StatusDone, 200, core.ProvenanceLive))
		key := core.AgentKey{Host: "mac", Server: "default", Epoch: 3, PaneID: "w1:p1"}
		seedAck(t, st, key, 100, true, 150) // an older finish's
		seedAck(t, st, key, 200, true, 300)
		return st
	}
	for _, c := range []struct {
		name    string
		status  core.Status
		readErr error
		seen    bool
	}{
		{"unchanged", core.StatusDone, nil, true},
		{"done to idle", core.StatusIdle, nil, true},
		{"working", core.StatusWorking, nil, false},
		{"blocked", core.StatusBlocked, nil, false},
		{"read fails", core.StatusDone, errBoom, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := seeded(t)
			st.ackReadErr = c.readErr
			logs := &logSink{}
			h := restartOn(t, snapOf(agentPane("w1:p1", c.status), agentPane("w1:p2", core.StatusIdle)), st,
				func(o *Options, _ *recorder) {
					o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
				})
			h.awaitAgents(2)
			v := h.viewOf("w1:p1")
			if v.Seen != c.seen {
				t.Errorf("w1:p1 %+v, want seen %v", v, c.seen)
			}
			if c.seen && v.WaitingSince != 200 {
				t.Errorf("waiting_since %d, want 200", v.WaitingSince)
			}
			if c.readErr != nil && len(logs.entries(t, "ERROR", "acknowledgements not read; the agent is shown not seen")) != 1 {
				t.Error("the failed read was not logged")
			}
			if n := len(st.ackList()); n != 2 {
				t.Errorf("%d acknowledgements, want the two seeded: a restart writes none", n)
			}
		})
	}
}

// jumpClock is the test clock, advancing a second per reading, plus a jump
// the test sets.
type jumpClock struct {
	n    atomic.Int64
	jump atomic.Int64
}

func (c *jumpClock) now() time.Time {
	start := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	return start.Add(time.Duration(c.n.Add(1))*time.Second + time.Duration(c.jump.Load()))
}

// openStore opens a real store in a fresh data directory.
func openStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(dir, "herddash.db"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAckRestartRealStore(t *testing.T) {
	// The real store, closed and opened again between Daemon runs, with a
	// Retention sweep before the restart: a seen finish is seen again only
	// while the first snapshot shows its Status unchanged.
	setRetentionInterval(t, time.Hour)
	setReconcileInterval(t, time.Hour)
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	clock := &jumpClock{}
	logs := &logSink{}
	run := func(p1 core.Status) (*harness, *store.Store) {
		t.Helper()
		db := openStore(t, dir)
		fake := newFakeClient(t, snapOf(agentPane("w1:p1", p1), agentPane("w1:p2", core.StatusIdle)))
		h := startEngineWith(t, fake, func(o *Options, _ *recorder) {
			o.Store = db
			o.Clock = clock.now
			o.RetentionDays = 1
			o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		})
		h.awaitAgents(2)
		return h, db
	}
	stop := func(h *harness, db *store.Store) {
		t.Helper()
		h.stop()
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}

	h, db := run(core.StatusWorking)
	h.say("w1:p1", core.StatusDone)
	first := h.viewOf("w1:p1")
	if _, err := h.ack(true, itemOf(first)); err != nil {
		t.Fatal(err)
	}
	h.say("w1:p1", core.StatusWorking)
	h.say("w1:p1", core.StatusDone)
	finish := h.viewOf("w1:p1")
	if _, err := h.ack(true, itemOf(finish)); err != nil {
		t.Fatal(err)
	}
	// Three days on, with a one-day Retention, the sweep removes the older
	// finish's rows and acknowledgement, and keeps the current finish's.
	clock.jump.Store(int64(3 * 24 * time.Hour))
	h.retentionTick()
	sweeps := logs.entries(t, "INFO", "retention sweep")
	if len(sweeps) < 2 || sweeps[len(sweeps)-1]["acknowledgements"] != float64(1) {
		t.Fatalf("sweeps %v, want the last to have removed one acknowledgement", sweeps)
	}
	acks, err := db.Acknowledgements(t.Context(), finish.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(acks) != 1 || acks[0].FinishedSince() != finish.WaitingSince || !acks[0].Seen() {
		t.Fatalf("acknowledgements after the sweep %+v, want the current finish's alone", acks)
	}
	stop(h, db)

	for _, c := range []struct {
		status core.Status
		seen   bool
	}{
		{core.StatusDone, true},
		{core.StatusIdle, true}, // done to idle within the finish
		{core.StatusWorking, false},
		{core.StatusDone, false}, // a new finish
	} {
		h, db := run(c.status)
		v := h.viewOf("w1:p1")
		if v.Seen != c.seen || v.Key.Epoch != 1 {
			t.Errorf("restarted showing %s: %+v, want seen %v under epoch 1", c.status, v, c.seen)
		}
		if c.seen && v.WaitingSince != finish.WaitingSince {
			t.Errorf("restarted showing %s: waiting_since %d, want %d", c.status, v.WaitingSince, finish.WaitingSince)
		}
		stop(h, db)
	}
}

func TestAckRetentionLogsAcknowledgements(t *testing.T) {
	h, st, logs := startedRetaining(t)
	gone := core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p9"}
	old := clockAt(0) - 2*retentionDays*dayMs
	seedAck(t, st, gone, old, true, old)
	h.retentionTick()
	e := logs.entries(t, "INFO", "retention sweep")
	if len(e) != 2 || e[1]["acknowledgements"] != float64(1) || e[1]["transitions"] == nil || e[1]["resync_markers"] == nil {
		t.Errorf("sweep lines %v, want the second to report one acknowledgement beside transitions and markers", e)
	}
}

// TestAckConcurrent sends marks, undos and bulk requests from 8 goroutines
// while herdr changes the same Agents' Status. Run it with -race -count=50
// -cpu=1,4,8.
func TestAckConcurrent(t *testing.T) {
	ids := []string{"w1:p1", "w1:p2", "w1:p3", "w1:p4"}
	var ps []core.PaneObservation
	for _, id := range ids {
		ps = append(ps, agentPane(id, core.StatusDone))
	}
	h, _, st := startAck(t, snapOf(ps...))
	stop := make(chan struct{})
	herdrDone := make(chan struct{})
	go func() {
		defer close(herdrDone)
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
	typed := func(err error) bool {
		return err == nil || errors.Is(err, ErrStaleView) || errors.Is(err, ErrNotFinished)
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(uint64(g), 7))
			for range 25 {
				views := h.engine.View().Agents
				var items []AckItem
				switch r.IntN(3) {
				case 0: // one Agent
					items = []AckItem{itemOf(views[r.IntN(len(views))])}
				default: // several, perhaps stale
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
				if err == nil {
					accepted.Add(1)
				}
				if err == nil && len(got) != len(items) {
					t.Errorf("%d views for %d items", len(got), len(items))
				}
				if err != nil && got != nil {
					t.Errorf("views %v beside error %v", got, err)
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-herdrDone
	if accepted.Load() == 0 {
		t.Error("no request was accepted")
	}
	h.settle()
	for _, v := range h.engine.View().Agents {
		ts, _ := st.History(context.Background(), v.Key)
		acks, _ := st.Acknowledgements(context.Background(), v.Key)
		f := core.Fold(ts)
		if want := core.Seen(f, acks); v.Seen != want || v.WaitingSince != f.Since {
			t.Errorf("%s seen %v since %d; the record says seen %v since %d", v.Key.PaneID, v.Seen, v.WaitingSince, want, f.Since)
		}
	}
}

// paneAt is agentPane with herdr's state_change_seq set to order.
func paneAt(id string, status core.Status, order uint64) core.PaneObservation {
	p := agentPane(id, status)
	p.Agent.StateChangeSeq = seq(order)
	return p
}

// orderOf is an acknowledgement's herdr order, or -1 when it has none.
func orderOf(a core.Acknowledgement) int64 {
	if o, ok := a.HerdrOrder(); ok {
		return int64(o)
	}
	return -1
}

func TestAckInRunMissedTurn(t *testing.T) {
	// herdr's order for w1:p1's done is 7 when the operator marks it. A
	// reconcile read later shows done again, at 9: herdr's Status changed
	// and came back with no event reaching herddash, a turn not observed.
	setReconcileInterval(t, time.Hour)
	h, _, st := startAck(t, snapOf(paneAt("w1:p1", core.StatusDone, 7), paneAt("w1:p2", core.StatusIdle, 3)))
	item := itemOf(h.viewOf("w1:p1"))
	if _, err := h.ack(true, item); err != nil {
		t.Fatal(err)
	}
	if acks := st.ackList(); len(acks) != 1 || orderOf(acks[0]) != 7 {
		t.Fatalf("acknowledgements %v, want one carrying herdr's order 7", acks)
	}
	// The same order again: nothing happened.
	h.reconcileTick()
	h.fake.awaitSnapshotCall()
	h.settle()
	if v := h.viewOf("w1:p1"); !v.Seen {
		t.Fatalf("after a read at the same order %+v, want seen", v)
	}
	rows := len(st.rowList())
	h.fake.setSnapshot(snapOf(paneAt("w1:p1", core.StatusDone, 9), paneAt("w1:p2", core.StatusIdle, 3)))
	h.reconcileTick()
	h.fake.awaitSnapshotCall()
	h.settle()
	v := h.viewOf("w1:p1")
	if v.Seen || v.Class != core.ClassFinished || v.WaitingSince != item.WaitingSince {
		t.Errorf("after a turn not observed %+v, want Finished and not seen", v)
	}
	if n := len(st.ackList()); n != 1 {
		t.Errorf("%d acknowledgements, want the mark alone: ending one writes nothing", n)
	}
	if n := len(st.rowList()); n != rows {
		t.Errorf("%d rows, want %d: the Status did not change", n, rows)
	}
	// Marked again, the row carries the new order.
	if _, err := h.ack(true, item); err != nil {
		t.Fatal(err)
	}
	if acks := st.ackList(); len(acks) != 2 || orderOf(acks[1]) != 9 || !h.viewOf("w1:p1").Seen {
		t.Errorf("acknowledgements %v, want a second carrying 9, and seen", acks)
	}
	// A live event carries no order: a finish learnt live is marked with
	// none, and the next read's order is adopted, not compared.
	h.say("w1:p1", core.StatusWorking)
	live := h.say("w1:p1", core.StatusDone)
	if _, err := h.ack(true, itemOf(live)); err != nil {
		t.Fatal(err)
	}
	if acks := st.ackList(); len(acks) != 3 || orderOf(acks[2]) != -1 {
		t.Errorf("acknowledgements %v, want a third with no order", acks)
	}
	h.fake.setSnapshot(snapOf(paneAt("w1:p1", core.StatusDone, 11), paneAt("w1:p2", core.StatusIdle, 3)))
	h.reconcileTick()
	h.fake.awaitSnapshotCall()
	h.settle()
	if v := h.viewOf("w1:p1"); !v.Seen {
		t.Errorf("after the first read of a finish learnt live %+v, want still seen", v)
	}
	// From then on the order is known, and a rise ends the mark.
	h.fake.setSnapshot(snapOf(paneAt("w1:p1", core.StatusDone, 14), paneAt("w1:p2", core.StatusIdle, 3)))
	h.reconcileTick()
	h.fake.awaitSnapshotCall()
	h.settle()
	if v := h.viewOf("w1:p1"); v.Seen {
		t.Errorf("after a rise once the order was known %+v, want not seen", v)
	}
}

func TestAckRestartMissedTurn(t *testing.T) {
	// A previous run recorded w1:p1 finishing at 200 and the operator's mark.
	// The Daemon restarts on the same herdr, whose first snapshot shows done:
	// at the order the mark (or the last row) carried, the same finish; at a
	// higher one, a later finish with the same Status and fold start.
	key := core.AgentKey{Host: "mac", Server: "default", Epoch: 3, PaneID: "w1:p1"}
	snapRow := func(t *testing.T, at int64, order uint64) core.Transition {
		t.Helper()
		prev := core.StatusWorking
		tr, err := core.NewTransition(core.TransitionInput{Key: key, AgentProgram: "claude", Profile: "claude",
			Previous: &prev, New: core.StatusDone, HerdrVersion: "0.9.5", ObservedAt: at,
			Provenance: core.ProvenanceSnapshot, HerdrOrder: seq(order)})
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}
	for _, c := range []struct {
		name     string
		snapRow  bool    // the done row came from a snapshot, at order 5
		ackOrder *uint64 // the mark's order
		now      uint64
		seen     bool
	}{
		{"mark's order, unchanged", false, seq(5), 5, true},
		{"mark's order, a turn missed", false, seq(5), 9, false},
		{"row's order, unchanged", true, nil, 5, true},
		{"row's order, a turn missed", true, nil, 9, false},
		{"no order known", false, nil, 9, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := newFakeStore(nil)
			done := stored(t, "w1:p1", core.StatusWorking, core.StatusDone, 200, core.ProvenanceLive)
			if c.snapRow {
				done = snapRow(t, 200, 5)
			}
			st.seed(stored(t, "w1:p1", "", core.StatusWorking, 100, core.ProvenanceLive), done)
			a, err := core.NewAcknowledgement(core.AcknowledgementInput{Key: key, FinishedSince: 200, Seen: true, ObservedAt: 300, HerdrOrder: c.ackOrder})
			if err != nil {
				t.Fatal(err)
			}
			st.mu.Lock()
			st.acks = append(st.acks, a)
			st.mu.Unlock()
			h := restartOn(t, snapOf(paneAt("w1:p1", core.StatusDone, c.now), agentPane("w1:p2", core.StatusIdle)), st, nil)
			h.awaitAgents(2)
			if v := h.viewOf("w1:p1"); v.Seen != c.seen || v.Class != core.ClassFinished {
				t.Errorf("w1:p1 %+v, want Finished, seen %v", v, c.seen)
			}
			if n := len(st.ackList()); n != 1 {
				t.Errorf("%d acknowledgements, want the seeded one: a restart writes none", n)
			}
		})
	}
}

func TestAckRestartMissedTurnRealStore(t *testing.T) {
	// The finding's case end to end: herdr's order rides the mark into the
	// real record, and a Daemon down for a whole turn of w1:p1 finds it
	// done again at a higher order, so the new result is not shown seen.
	setReconcileInterval(t, time.Hour)
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	clock := &jumpClock{}
	run := func(order uint64) (*harness, *store.Store) {
		t.Helper()
		db := openStore(t, dir)
		fake := newFakeClient(t, snapOf(paneAt("w1:p1", core.StatusDone, order), paneAt("w1:p2", core.StatusIdle, 3)))
		h := startEngineWith(t, fake, func(o *Options, _ *recorder) {
			o.Store = db
			o.Clock = clock.now
		})
		h.awaitAgents(2)
		return h, db
	}
	stop := func(h *harness, db *store.Store) {
		t.Helper()
		h.stop()
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	h, db := run(7)
	if _, err := h.ack(true, itemOf(h.viewOf("w1:p1"))); err != nil {
		t.Fatal(err)
	}
	stop(h, db)
	h, db = run(7)
	if v := h.viewOf("w1:p1"); !v.Seen {
		t.Errorf("restarted at the mark's order %+v, want seen", v)
	}
	stop(h, db)
	h, db = run(12)
	if v := h.viewOf("w1:p1"); v.Seen || v.Class != core.ClassFinished {
		t.Errorf("restarted after a turn not observed %+v, want Finished and not seen", v)
	}
	stop(h, db)
}
