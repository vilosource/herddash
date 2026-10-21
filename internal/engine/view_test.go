package engine

import (
	"encoding/hex"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
)

// watcher collects every publication of one subscription, in order.
type watcher struct {
	t      *testing.T
	mu     sync.Mutex
	pubs   []Publication
	closed bool
	notify chan struct{}
}

func watch(t *testing.T, s *ViewSubscription) *watcher {
	w := &watcher{t: t, notify: make(chan struct{}, 1)}
	go func() {
		for p := range s.C {
			w.mu.Lock()
			w.pubs = append(w.pubs, p)
			w.mu.Unlock()
			w.poke()
		}
		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()
		w.poke()
	}()
	return w
}

func (w *watcher) poke() {
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

func (w *watcher) all() []Publication {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.pubs)
}

// await waits until ok holds for the publications so far, and returns them.
func (w *watcher) await(what string, ok func([]Publication) bool) []Publication {
	w.t.Helper()
	deadline := time.After(wait)
	for {
		ps := w.all()
		if ok(ps) {
			return ps
		}
		select {
		case <-w.notify:
		case <-deadline:
			w.t.Fatalf("waiting for %s; publications %s", what, describe(ps))
		}
	}
}

// awaitAfter waits until a publication after the first n satisfies ok, and
// returns the publications after the first n up to and including it.
func (w *watcher) awaitAfter(what string, n int, ok func(Publication) bool) []Publication {
	w.t.Helper()
	var end int
	w.await(what, func(ps []Publication) bool {
		for i := n; i < len(ps); i++ {
			if ok(ps[i]) {
				end = i + 1
				return true
			}
		}
		return false
	})
	return w.all()[n:end]
}

func describe(ps []Publication) string {
	s := ""
	for _, p := range ps {
		s += "\n  " + strconv.FormatUint(p.View.Seq, 10) + ":"
		for _, c := range p.Changes {
			s += " " + string(c.Kind)
		}
	}
	return s
}

func kinds(p Publication) []ChangeKind {
	out := []ChangeKind{}
	for _, c := range p.Changes {
		out = append(out, c.Kind)
	}
	return out
}

func hasKind(k ChangeKind) func(Publication) bool {
	return func(p Publication) bool { return slices.Contains(kinds(p), k) }
}

func keysOf(vs []core.AgentView) []string {
	out := []string{}
	for _, v := range vs {
		out = append(out, v.Key.PaneID)
	}
	return out
}

// readConcurrently reads the View from another goroutine until the test
// ends, checking that Seq never goes back and every View is in Board order.
func readConcurrently(t *testing.T, e *Engine) {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var last uint64
		for {
			select {
			case <-stop:
				return
			default:
			}
			v := e.View()
			if v.Seq < last {
				t.Errorf("view seq went back from %d to %d", last, v.Seq)
				return
			}
			last = v.Seq
			if !slices.IsSortedFunc(v.Agents, func(a, b core.AgentView) int {
				switch {
				case a.SortKey.Less(b.SortKey):
					return -1
				case b.SortKey.Less(a.SortKey):
					return 1
				}
				return 0
			}) {
				t.Errorf("view %d not in Board order", v.Seq)
				return
			}
			time.Sleep(50 * time.Microsecond)
		}
	}()
	t.Cleanup(func() { close(stop); <-done })
}

// startViewed starts an engine like startEngineWith, subscribed and read
// concurrently from before it runs.
func startViewed(t *testing.T, fake *fakeClient, opts func(*Options, *recorder)) (*harness, *watcher) {
	t.Helper()
	var w *watcher
	h := startEngineBefore(t, fake, opts, func(e *Engine) {
		s, _ := e.Subscribe(1024)
		w = watch(t, s)
		readConcurrently(t, e)
	})
	return h, w
}

// startedViewed runs a viewed engine on twoAgents until both Agents are in
// the View and it has settled, and returns how many publications that took.
func startedViewed(t *testing.T) (*harness, *watcher, int) {
	t.Helper()
	h, w := startViewed(t, newFakeClient(t, twoAgents()), nil)
	w.await("two agents", func(ps []Publication) bool { return len(ps) > 0 && len(ps[len(ps)-1].View.Agents) == 2 })
	h.fake.awaitSubscribe("w1:p1")
	h.fake.awaitSubscribe("w1:p2")
	h.settle()
	return h, w, len(w.all())
}

func checkSeqs(t *testing.T, ps []Publication) {
	t.Helper()
	for i, p := range ps {
		if p.View.Seq != uint64(i+1) {
			t.Fatalf("publication %d has seq %d, want %d", i, p.View.Seq, i+1)
		}
	}
}

func TestViewFirstStart(t *testing.T) {
	h, w := startViewed(t, newFakeClient(t, twoAgents()), func(o *Options, _ *recorder) { o.Home = "/src" })
	ps := w.await("two agents", func(ps []Publication) bool { return len(ps) >= 2 })
	want0 := core.ConnectionTruth{HerdrVersion: "0.9.5", HerdrMinimum: "0.9.5", Gate: core.GateOK, ResyncGuaranteed: true}
	if !slices.Equal(kinds(ps[0]), []ChangeKind{ChangeConnection}) || ps[0].Changes[0].Connection != want0 {
		t.Errorf("first publication %+v, want the gate decision", ps[0].Changes)
	}
	if len(ps[0].View.Agents) != 0 || ps[0].View.Connection != want0 {
		t.Errorf("first view %+v", ps[0].View)
	}
	if got := kinds(ps[1]); !slices.Equal(got, []ChangeKind{ChangeConnection, ChangeAgent, ChangeAgent}) {
		t.Fatalf("second publication kinds %v, want connection then two agents", got)
	}
	c := ps[1].Changes[0].Connection
	if !c.Connected || c.LastSnapshotAt != clockAt(2) || c.Gate != core.GateOK {
		t.Errorf("connection %+v, want connected at the snapshot", c)
	}
	v := ps[1].View
	if got := keysOf(v.Agents); !slices.Equal(got, []string{"w1:p1", "w1:p2"}) {
		t.Errorf("agents %v", got)
	}
	if v.Agents[0].Directory != "~/w1:p1" || v.Agents[0].Heading != "claude" || v.Agents[0].Class != core.ClassWorking {
		t.Errorf("agent view %+v", v.Agents[0])
	}
	if ps[1].Changes[1].Agent != v.Agents[0] || ps[1].Changes[2].Agent != v.Agents[1] {
		t.Error("agent changes do not carry the views published")
	}
	h.settle()
	all := w.all()
	checkSeqs(t, all)
	if got := h.engine.View(); got.Seq != uint64(len(all)) || !reflect.DeepEqual(got, all[len(all)-1].View) {
		t.Errorf("View() seq %d, want the latest publication %d", got.Seq, len(all))
	}
}

func TestViewTransition(t *testing.T) {
	h, w, n := startedViewed(t)
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	ps := w.awaitAfter("transition", n, hasKind(ChangeTransition))
	if len(ps) != 1 || !slices.Equal(kinds(ps[0]), []ChangeKind{ChangeTransition}) {
		t.Fatalf("publications %s, want one with one transition", describe(ps))
	}
	c := ps[0].Changes[0]
	if c.Transition.New() != core.StatusBlocked || c.Transition.Key().PaneID != "w1:p1" {
		t.Errorf("transition %+v", c.Transition)
	}
	if c.Agent.Class != core.ClassNeedsYou || c.Agent.Key.PaneID != "w1:p1" {
		t.Errorf("view %+v, want needs_you", c.Agent)
	}
	v := ps[0].View
	if v.Agents[0] != c.Agent {
		t.Errorf("view agents %v, want the blocked Agent first and as the change carries it", keysOf(v.Agents))
	}
	checkSeqs(t, w.all())
}

func TestViewRemoval(t *testing.T) {
	h, w, n := startedViewed(t)
	s := twoAgents()
	s.Panes = s.Panes[1:]
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
	ps := w.awaitAfter("removal", n, hasKind(ChangeRemoved))
	p := ps[len(ps)-1]
	if !slices.Equal(kinds(p), []ChangeKind{ChangeRemoved}) {
		t.Fatalf("kinds %v", kinds(p))
	}
	c := p.Changes[0]
	if c.Key.PaneID != "w1:p1" || c.Agent.Key != c.Key || c.Agent.Status != core.StatusWorking {
		t.Errorf("removed change %+v", c)
	}
	if got := keysOf(p.View.Agents); !slices.Equal(got, []string{"w1:p2"}) {
		t.Errorf("agents %v, want w1:p1 gone", got)
	}
}

func TestViewResync(t *testing.T) {
	h, w, n := startedViewed(t)
	h.fake.statusSub("w1:p1").health(&herdr.Health{Kind: herdr.HealthEventsLost, Code: "events_lost"})
	ps := w.awaitAfter("resync", n, hasKind(ChangeResync))
	p := ps[len(ps)-1]
	if !slices.Equal(kinds(p), []ChangeKind{ChangeResync}) {
		t.Fatalf("kinds %v", kinds(p))
	}
	if c := p.Changes[0]; c.Reason != "events_lost" || c.Detail != "events_lost" {
		t.Errorf("resync %+v", c)
	}
	// Its snapshot follows.
	w.awaitAfter("the resync's snapshot", n+len(ps), hasKind(ChangeConnection))
	h.settle()
	checkSeqs(t, w.all())
}

func TestViewLost(t *testing.T) {
	h, w, n := startedViewed(t)
	h.fake.lifecycleSub().health(&herdr.Health{Kind: herdr.HealthClosed})
	ps := w.awaitAfter("disconnect", n, func(p Publication) bool {
		return hasKind(ChangeConnection)(p) && !p.View.Connection.Connected
	})
	p := ps[len(ps)-1]
	if !slices.Equal(kinds(p), []ChangeKind{ChangeConnection}) || p.Changes[0].Connection.Connected {
		t.Fatalf("publication %+v", p.Changes)
	}
	if got := keysOf(p.View.Agents); !slices.Equal(got, []string{"w1:p1", "w1:p2"}) {
		t.Errorf("agents %v, want both kept", got)
	}
	if p.View.Connection.LastSnapshotAt == 0 {
		t.Error("last_snapshot_at lost")
	}
	// It reconnects.
	w.awaitAfter("reconnect", n+len(ps), func(p Publication) bool { return p.View.Connection.Connected })
	h.settle()
	checkSeqs(t, w.all())
}

func TestViewReconcileSnapshot(t *testing.T) {
	setReconcileInterval(t, 400*time.Millisecond)
	h, w, n := startedViewed(t)
	before := h.engine.View()
	ps := w.awaitAfter("reconcile", n, hasKind(ChangeConnection))
	p := ps[len(ps)-1]
	if len(ps) != 1 || !slices.Equal(kinds(p), []ChangeKind{ChangeConnection}) {
		t.Fatalf("publications %s, want one connection change", describe(ps))
	}
	c := p.Changes[0].Connection
	if !c.Connected || c.LastSnapshotAt <= before.Connection.LastSnapshotAt {
		t.Errorf("connection %+v, want a later last_snapshot_at than %d", c, before.Connection.LastSnapshotAt)
	}
	if !reflect.DeepEqual(p.View.Agents, before.Agents) {
		t.Error("agents changed")
	}
}

func TestViewRefused(t *testing.T) {
	fake := newFakeClient(t, twoAgents())
	fake.setVersions("0.8.2")
	h, w := startViewed(t, fake, nil)
	ps := w.await("refusal", func(ps []Publication) bool { return len(ps) >= 1 })
	h.fake.awaitPings(3)
	if ps = w.all(); len(ps) != 1 {
		t.Fatalf("publications %s, want one: unchanged rechecks change nothing", describe(ps))
	}
	v := h.engine.View()
	want := core.ConnectionTruth{HerdrVersion: "0.8.2", HerdrMinimum: "0.9.5", Gate: core.GateRefused}
	if v.Seq != 1 || v.Connection != want || v.Agents == nil || len(v.Agents) != 0 {
		t.Errorf("view %+v, want refused with no agents", v)
	}
}

func TestViewRefusedAfterConnected(t *testing.T) {
	setRecheckInterval(t, time.Hour)
	h, w, n := startedViewed(t)
	h.fake.setVersions("0.8.2")
	h.fake.lifecycleSub().health(&herdr.Health{Kind: herdr.HealthClosed})
	ps := w.awaitAfter("refusal", n, func(p Publication) bool { return p.View.Connection.Gate == core.GateRefused })
	p := ps[len(ps)-1]
	if got := kinds(p); !slices.Equal(got, []ChangeKind{ChangeConnection, ChangeRemoved, ChangeRemoved}) {
		t.Fatalf("kinds %v, want the gate then both removals", got)
	}
	v := p.View
	if v.Connection.Connected || v.Connection.HerdrVersion != "0.8.2" || len(v.Agents) != 0 || len(v.Headers) != 0 {
		t.Errorf("view %+v, want refused with no agents", v)
	}
}

func TestViewImmutable(t *testing.T) {
	h, w, n := startedViewed(t)
	held := h.engine.View()
	agents := slices.Clone(held.Agents)
	headers := slices.Clone(held.Headers)
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	h.fake.statusSub("w1:p2").event(core.StatusEvent{PaneID: "w1:p2", Status: core.StatusWorking, AgentProgram: "claude"})
	s := twoAgents()
	s.Panes = s.Panes[1:]
	h.fake.setSnapshot(s)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
	w.awaitAfter("removal", n, hasKind(ChangeRemoved))
	if h.engine.View().Seq <= held.Seq {
		t.Fatal("no later publication")
	}
	if !reflect.DeepEqual(held.Agents, agents) || !reflect.DeepEqual(held.Headers, headers) {
		t.Errorf("held view changed:\n got %+v\nwant %+v", held.Agents, agents)
	}
}

func TestViewRunID(t *testing.T) {
	a := New(Options{Host: "mac", Server: "default"})
	b := New(Options{Host: "mac", Server: "default"})
	va, vb := a.View(), b.View()
	for _, v := range []View{va, vb} {
		if raw, err := hex.DecodeString(v.RunID); err != nil || len(raw) != 16 {
			t.Errorf("run id %q, want 16 bytes in hex", v.RunID)
		}
		if v.Seq != 0 || v.Agents == nil || len(v.Agents) != 0 || v.Connection.HerdrMinimum != core.HerdrMinimum {
			t.Errorf("view before any publication %+v", v)
		}
	}
	if va.RunID == vb.RunID {
		t.Error("two engines share a run id")
	}
	_, w1 := startViewed(t, newFakeClient(t, twoAgents()), nil)
	_, w2 := startViewed(t, newFakeClient(t, twoAgents()), nil)
	p1 := w1.await("first", func(ps []Publication) bool { return len(ps) > 0 })[0]
	p2 := w2.await("first", func(ps []Publication) bool { return len(ps) > 0 })[0]
	if p1.View.Seq != 1 || p2.View.Seq != 1 || p1.View.RunID == p2.View.RunID {
		t.Errorf("first publications seq %d (%s), %d (%s)", p1.View.Seq, p1.View.RunID, p2.View.Seq, p2.View.RunID)
	}
}

func TestViewSlowSubscriber(t *testing.T) {
	h, w, n := startedViewed(t)
	slow, unsubscribe := h.engine.Subscribe(1)
	sub := h.fake.statusSub("w1:p1")
	for _, st := range []core.Status{core.StatusBlocked, core.StatusWorking, core.StatusBlocked} {
		sub.event(core.StatusEvent{PaneID: "w1:p1", Status: st, AgentProgram: "claude"})
	}
	w.await("three transitions", func(ps []Publication) bool { return len(ps) >= n+3 })
	got := 0
	timeout := time.After(wait)
	for open := true; open; {
		select {
		case _, open = <-slow.C:
			if open {
				got++
			}
		case <-timeout:
			t.Fatal("slow subscriber's channel not closed")
		}
	}
	if got != 1 {
		t.Errorf("slow subscriber got %d publications, want the one its buffer held", got)
	}
	unsubscribe() // after the engine dropped it: no panic
	// Others still receive.
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"})
	w.await("a fourth transition", func(ps []Publication) bool { return len(ps) >= n+4 })
}

func TestViewUnsubscribe(t *testing.T) {
	h, w, n := startedViewed(t)
	s, unsubscribe := h.engine.Subscribe(4)
	unsubscribe()
	unsubscribe()
	if _, open := <-s.C; open {
		t.Fatal("channel open after unsubscribing")
	}
	h.engine.pub.mu.Lock()
	_, kept := h.engine.pub.subs[s]
	h.engine.pub.mu.Unlock()
	if kept {
		t.Error("still in the subscriber set after unsubscribing")
	}
	// Publishing goes on for the others.
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	w.await("a transition", func(ps []Publication) bool { return len(ps) >= n+1 })
}

func TestViewSubscribeChurn(t *testing.T) {
	h, w, n := startedViewed(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s, unsubscribe := h.engine.Subscribe(1)
				select {
				case <-s.C:
				default:
				}
				unsubscribe()
				unsubscribe()
			}
		}()
	}
	sub := h.fake.statusSub("w1:p1")
	statuses := []core.Status{core.StatusBlocked, core.StatusWorking}
	for i := range 20 {
		sub.event(core.StatusEvent{PaneID: "w1:p1", Status: statuses[i%2], AgentProgram: "claude"})
	}
	w.await("twenty transitions", func(ps []Publication) bool { return len(ps) >= n+20 })
	close(stop)
	wg.Wait()
	checkSeqs(t, w.all())
}

func TestViewShutdown(t *testing.T) {
	h, w, _ := startedViewed(t)
	h.stop()
	w.await("closed", func([]Publication) bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.closed
	})
	ps := w.all()
	last := ps[len(ps)-1]
	if !slices.Equal(kinds(last), []ChangeKind{ChangeConnection}) || last.Changes[0].Connection.Connected {
		t.Errorf("last publication %+v, want the connection lost", last.Changes)
	}
	if v := h.engine.View(); v.Connection.Connected || len(v.Agents) != 2 {
		t.Errorf("view after stop %+v", v)
	}
	checkSeqs(t, ps)
	s, unsubscribe := h.engine.Subscribe(4)
	select {
	case _, open := <-s.C:
		if open {
			t.Error("subscription after stop received a publication")
		}
	case <-time.After(wait):
		t.Fatal("subscription after stop not closed")
	}
	unsubscribe()
	unsubscribe()
}

func TestViewContextChange(t *testing.T) {
	setReconcileInterval(t, 400*time.Millisecond)
	h, w, n := startedViewed(t)
	s := twoAgents()
	s.Panes[0].Context.ProjectDir = "/src/elsewhere"
	s.Panes[0].Context.PaneTitle = "new title"
	h.fake.setSnapshot(s)
	ps := w.awaitAfter("context change", n, hasKind(ChangeAgent))
	p := ps[len(ps)-1]
	var got []core.AgentView
	for _, c := range p.Changes {
		if c.Kind == ChangeAgent {
			got = append(got, c.Agent)
		}
	}
	if len(got) != 1 || got[0].Key.PaneID != "w1:p1" || got[0].Directory != "/src/elsewhere" || got[0].PaneTitle != "new title" {
		t.Fatalf("agent changes %+v, want one for w1:p1 with the new context", got)
	}
	if v := h.engine.View(); v.Agents[0] != got[0] {
		t.Errorf("View() has %+v, want %+v", v.Agents[0], got[0])
	}
}

func TestViewProfileChange(t *testing.T) {
	fake := newFakeClient(t, oneAgent("claude", core.StatusWorking))
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	h, w := startViewed(t, fake, func(o *Options, _ *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
	})
	w.await("the agent", func(ps []Publication) bool { return len(ps) > 0 && len(ps[len(ps)-1].View.Agents) == 1 })
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	n := len(w.all())
	env.set(p1PID, claudeVar, "/h/.claude-cldp")
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	ps := w.awaitAfter("profile change", n, hasKind(ChangeAgent))
	var c Change
	for _, ch := range ps[len(ps)-1].Changes {
		if ch.Kind == ChangeAgent {
			c = ch
		}
	}
	if c.Agent.Profile != "cldp" || c.Agent.Heading != "cldp" {
		t.Fatalf("agent change %+v, want Profile cldp", c.Agent)
	}
	if v := h.engine.View(); v.Agents[0] != c.Agent {
		t.Errorf("View() has %+v, want %+v", v.Agents[0], c.Agent)
	}
}

func TestViewSorted(t *testing.T) {
	pane := func(id string, st core.Status, label string, pos int) core.PaneObservation {
		p := agentPane(id, st)
		p.Context.WorkspaceLabel, p.Context.WorkspacePosition = label, pos
		return p
	}
	snap := core.Snapshot{HerdrVersion: "0.9.5", Panes: []core.PaneObservation{
		pane("w2:p1", core.StatusWorking, "two", 2),
		pane("w1:p1", core.StatusWorking, "one", 1),
		pane("w2:p2", core.StatusBlocked, "two", 2),
		pane("w1:p2", core.StatusUnknown, "one", 1),
		pane("w1:p3", core.StatusDone, "one", 1),
	}}
	_, w := startViewed(t, newFakeClient(t, snap), nil)
	ps := w.await("five agents", func(ps []Publication) bool {
		return len(ps) > 0 && len(ps[len(ps)-1].View.Agents) == 5
	})
	v := ps[len(ps)-1].View
	if got, want := keysOf(v.Agents), []string{"w2:p2", "w1:p1", "w1:p3", "w1:p2", "w2:p1"}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	sorted := slices.Clone(v.Agents)
	core.SortViews(sorted)
	if !reflect.DeepEqual(sorted, v.Agents) {
		t.Error("not in SortViews order")
	}
	if !slices.Equal(v.Headers, core.WorkspaceHeaders(v.Agents)) || !slices.Equal(v.Headers, []int{1, 4}) {
		t.Errorf("headers %v, want [1 4]", v.Headers)
	}
}
