package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
	"github.com/vilosource/herddash/internal/herdr"
	"github.com/vilosource/herddash/internal/procenv"
	"github.com/vilosource/herddash/internal/store"
)

// The contract between the engine, the SSE handler and the Board (Story
// 5.11, Epic 3 retro D5 and D6): the real engine runs behind the real /events
// handler, and every event the stream carries is replayed through order.mjs's
// streamDecision in Node. After every publication the Board's model must equal
// the View the engine published, and no event may be rejected or seen as a
// skipped publication.

// scriptSub is a scripted herdr subscription: the test pushes deliveries;
// Close, or a health signal, ends it and closes the delivery channel, as
// herdr.Subscription does.
type scriptSub[T any] struct {
	in     chan herdr.Delivery[T]
	out    chan herdr.Delivery[T]
	closed chan struct{}
	once   sync.Once
}

func newScriptSub[T any]() *scriptSub[T] {
	s := &scriptSub[T]{in: make(chan herdr.Delivery[T], 16), out: make(chan herdr.Delivery[T]), closed: make(chan struct{})}
	go func() {
		defer close(s.out)
		for {
			select {
			case d := <-s.in:
				select {
				case s.out <- d:
				case <-s.closed:
					return
				}
				if d.Health != nil {
					return
				}
			case <-s.closed:
				return
			}
		}
	}()
	return s
}

func (s *scriptSub[T]) Deliveries() <-chan herdr.Delivery[T] { return s.out }

func (s *scriptSub[T]) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// scriptClient is a scripted herdr 0.9.5: Snapshot answers the snapshot the
// test last set, and every subscription is a scriptSub the test drives.
type scriptClient struct {
	mu        sync.Mutex
	snap      core.Snapshot
	lifecycle *scriptSub[core.LifecycleEvent]
	status    map[string]*scriptSub[core.StatusEvent]
}

func (c *scriptClient) Ping(context.Context) (string, error) { return "0.9.5", nil }

func (c *scriptClient) Snapshot(context.Context) (core.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap, nil
}

func (c *scriptClient) SubscribeLifecycle(context.Context) (engine.Subscription[core.LifecycleEvent], error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lifecycle = newScriptSub[core.LifecycleEvent]()
	return c.lifecycle, nil
}

func (c *scriptClient) SubscribeStatus(_ context.Context, paneID string) (engine.Subscription[core.StatusEvent], error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := newScriptSub[core.StatusEvent]()
	c.status[paneID] = s
	return s, nil
}

func (c *scriptClient) ForegroundProcess(_ context.Context, paneID string) (core.ForegroundProcess, error) {
	return core.ForegroundProcess{}, fmt.Errorf("pane %s: %w", paneID, herdr.ErrNoForegroundProcess)
}

func (c *scriptClient) setSnapshot(s core.Snapshot) {
	c.mu.Lock()
	c.snap = s
	c.mu.Unlock()
}

// lifecycleEvent sends ev on the current lifecycle subscription.
func (c *scriptClient) lifecycleEvent(ev core.LifecycleEvent) {
	c.mu.Lock()
	s := c.lifecycle
	c.mu.Unlock()
	s.in <- herdr.Delivery[core.LifecycleEvent]{Event: ev}
}

// loseLifecycle ends the current lifecycle subscription as a dropped
// connection does, which makes the engine reconnect.
func (c *scriptClient) loseLifecycle() {
	c.mu.Lock()
	s := c.lifecycle
	c.mu.Unlock()
	s.in <- herdr.Delivery[core.LifecycleEvent]{Health: &herdr.Health{Kind: herdr.HealthClosed, Err: errors.New("EOF")}}
}

// statusSub waits for paneID's status subscription.
func (c *scriptClient) statusSub(t *testing.T, paneID string) *scriptSub[core.StatusEvent] {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c.mu.Lock()
		s := c.status[paneID]
		c.mu.Unlock()
		if s != nil {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("no status subscription for %s", paneID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// nopSink is an engine.Sink that keeps nothing.
type nopSink struct{}

func (nopSink) Agent(engine.Agent, int64)                  {}
func (nopSink) Transition(engine.Transition, engine.Agent) {}
func (nopSink) Removed(engine.Agent, int64)                {}
func (nopSink) Reconnected(string, uint64, bool, int64)    {}
func (nopSink) Resynced(string, int64)                     {}
func (nopSink) Gate(core.Gate, string, int64)              {}
func (nopSink) Profile(engine.Agent, string, int64)        {}
func contractEnviron(int, string) (string, bool, error)    { return "", false, nil }
func contractChildren(int) ([]procenv.Process, error)      { return nil, nil }
func contractSession(string, int) (procenv.Session, error) {
	return procenv.Session{}, fmt.Errorf("%w: %w", procenv.ErrSession, fs.ErrNotExist)
}
func contractTranscript(t procenv.Tail, _ string, _ func([]byte)) (procenv.Tail, bool, error) {
	return t, false, nil
}

func contractPane(id, terminal string, status core.Status) core.PaneObservation {
	n := uint64(7)
	return core.PaneObservation{
		PaneID: id, TerminalID: terminal, WorkspaceID: "w1", TabID: "w1:t1",
		Context: core.PaneContext{ProjectDir: "/src/" + id, WorkspaceLabel: "work", TabLabel: "main", WorkspacePosition: 1, PaneTitle: "title " + id},
		Agent:   &core.AgentObservation{AgentProgram: "claude", Status: status, StateChangeSeq: &n},
	}
}

func shellPane(terminal string) core.PaneObservation {
	return core.PaneObservation{PaneID: "w1:p9", TerminalID: terminal, WorkspaceID: "w1", TabID: "w1:t1"}
}

func contractSnapshot(panes ...core.PaneObservation) core.Snapshot {
	return core.Snapshot{HerdrVersion: "0.9.5", Panes: panes}
}

// replayed is what testdata/replay-board.mjs prints.
type replayed struct {
	Seqs map[string]struct {
		Agents     []json.RawMessage `json:"agents"`
		Connection json.RawMessage   `json:"connection"`
		Recording  *bool             `json:"recording"`
	} `json:"seqs"`
	Warnings []string `json:"warnings"`
	Reopens  []string `json:"reopens"`
}

// generic decodes b into plain JSON values, so two encodings compare equal
// whatever their key order.
func generic(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return v
}

func keyString(k core.AgentKey) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s", k.Host, k.Server, k.Epoch, k.PaneID)
}

func TestEngineContract(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is not on PATH, and CI is set")
		}
		t.Skip("node is not on PATH")
	}

	// A real store: an acknowledgement is refused while nothing is recorded.
	st, err := store.Open(filepath.Join(t.TempDir(), "data", "herddash.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fc := &scriptClient{status: map[string]*scriptSub[core.StatusEvent]{}}
	fc.setSnapshot(contractSnapshot(contractPane("w1:p1", "t-1", core.StatusWorking), contractPane("w1:p2", "t-2", core.StatusIdle), shellPane("t-shell")))
	// The engine's clock is the wall clock plus an offset the test moves, so
	// a dwell in herdr's focus (ADR-0015) passes without waiting for it.
	var offset atomic.Int64
	clock := func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	e := engine.New(engine.Options{
		Host: "mac", Server: "default", Client: fc, Sink: nopSink{}, Store: st, Clock: clock,
		Environ: contractEnviron, Children: contractChildren, Session: contractSession, Transcript: contractTranscript})

	// Every publication, as the engine itself publishes it.
	own, unsubscribe := e.Subscribe(4096)
	t.Cleanup(unsubscribe)
	var mu sync.Mutex
	var views []engine.View
	go func() {
		for p := range own.C {
			mu.Lock()
			views = append(views, p.View)
			mu.Unlock()
		}
	}()

	h := New(Options{View: e.View, Subscribe: e.Subscribe, History: st.History, Acknowledge: e.Acknowledge, Acknowledgements: st.Acknowledgements})
	h.keepalive = 20 * time.Millisecond
	srv, _ := eventsServer(t, h)
	s := openStream(t, srv, nil)
	// Every event the stream carries. The state event comes after the
	// handler subscribed, so the engine starts only once it has.
	var events []sseEvent
	got := make(chan struct{}, 1)
	first := s.next(t)
	if first.event != eventState {
		t.Fatalf("first event %+v", first)
	}
	events = append(events, first)
	stopCollect := make(chan struct{})
	t.Cleanup(func() { close(stopCollect) })
	go func() {
		for {
			select {
			case ev := <-s.events:
				mu.Lock()
				events = append(events, ev)
				mu.Unlock()
				select {
				case got <- struct{}{}:
				default:
				}
			case <-stopCollect:
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("engine did not stop")
		}
	})

	// await waits for a publication after the n-th that satisfies ok, and
	// returns how many there are then.
	await := func(what string, n int, ok func(engine.View) bool) int {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			mu.Lock()
			vs := slices.Clone(views)
			mu.Unlock()
			for i := n; i < len(vs); i++ {
				if ok(vs[i]) {
					return len(vs)
				}
			}
			if time.Now().After(deadline) {
				if len(vs) == 0 {
					t.Fatalf("no publication at all, waiting for: %s", what)
				}
				last, _ := json.Marshal(vs[len(vs)-1])
				t.Fatalf("no publication: %s; the last of %d is %s", what, len(vs), last)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	agent := func(v engine.View, pane string) (core.AgentView, bool) {
		for _, a := range v.Agents {
			if a.Key.PaneID == pane {
				return a, true
			}
		}
		return core.AgentView{}, false
	}

	// Two Agents.
	n := await("two Agents", 0, func(v engine.View) bool { return v.Connection.Connected && len(v.Agents) == 2 })
	// A live Status change.
	fc.statusSub(t, "w1:p1").in <- herdr.Delivery[core.StatusEvent]{Event: core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"}}
	n = await("w1:p1 blocked", n, func(v engine.View) bool { a, ok := agent(v, "w1:p1"); return ok && a.Status == core.StatusBlocked })
	// An Agent removed: its pane closed.
	fc.setSnapshot(contractSnapshot(contractPane("w1:p2", "t-2", core.StatusIdle), shellPane("t-shell")))
	fc.lifecycleEvent(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
	n = await("w1:p1 removed", n, func(v engine.View) bool { _, ok := agent(v, "w1:p1"); return !ok && len(v.Agents) == 1 })
	// A new Agent detected.
	fc.setSnapshot(contractSnapshot(contractPane("w1:p2", "t-2", core.StatusIdle), contractPane("w1:p4", "t-4", core.StatusWorking), shellPane("t-shell")))
	fc.lifecycleEvent(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	n = await("w1:p4 added", n, func(v engine.View) bool { _, ok := agent(v, "w1:p4"); return ok })
	// A context change.
	moved := contractPane("w1:p2", "t-2", core.StatusIdle)
	moved.Context.ProjectDir, moved.Context.PaneTitle = "/src/elsewhere", "new title"
	fc.setSnapshot(contractSnapshot(moved, contractPane("w1:p4", "t-4", core.StatusWorking), shellPane("t-shell")))
	fc.lifecycleEvent(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	n = await("w1:p2's context", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p2")
		return ok && a.Directory == "/src/elsewhere" && a.PaneTitle == "new title"
	})
	// Acknowledgement (ADR-0014): w1:p4 finishes, is marked seen through the
	// write route as the Board marks it, is undone, then goes working. Each
	// is a publication the stream must carry.
	fc.statusSub(t, "w1:p4").in <- herdr.Delivery[core.StatusEvent]{Event: core.StatusEvent{PaneID: "w1:p4", Status: core.StatusDone, AgentProgram: "claude"}}
	n = await("w1:p4 done", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p4")
		return ok && a.Status == core.StatusDone && a.Class == core.ClassFinished && a.WaitingSince > 0
	})
	done4, _ := agent(e.View(), "w1:p4")
	write := func(route string) {
		t.Helper()
		body := fmt.Sprintf(`{"waiting_since":%d}`, done4.WaitingSince)
		deadline := time.Now().Add(15 * time.Second)
		for {
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/agents/mac/default/1/w1%3Ap4/"+route, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Origin", srv.URL)
			req.Header.Set("X-Herddash-Write", "1")
			req.Header.Set("Content-Type", "application/json")
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			// A Profile hold still in progress refuses it as stale: try again.
			if resp.StatusCode != http.StatusConflict || !strings.Contains(string(b), codeStaleView) || time.Now().After(deadline) {
				t.Fatalf("%s: status %d, body %s", route, resp.StatusCode, b)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	write(segmentSeen)
	n = await("w1:p4 seen", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p4")
		return ok && a.Seen && a.Class == core.ClassFinished && a.SortKey.ClassRank == 2
	})
	write(segmentUnseen)
	n = await("w1:p4 not seen", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p4")
		return ok && !a.Seen && a.Status == core.StatusDone && a.SortKey.ClassRank == 1
	})
	write(segmentSeen)
	n = await("w1:p4 seen again", n, func(v engine.View) bool { a, ok := agent(v, "w1:p4"); return ok && a.Seen })
	fc.statusSub(t, "w1:p4").in <- herdr.Delivery[core.StatusEvent]{Event: core.StatusEvent{PaneID: "w1:p4", Status: core.StatusWorking, AgentProgram: "claude"}}
	n = await("w1:p4 working", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p4")
		return ok && a.Status == core.StatusWorking && !a.Seen
	})
	// The record holds each write, in order, and the history route sends it.
	hist, err := srv.Client().Get(srv.URL + "/api/agents/mac/default/1/w1%3Ap4/history")
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := io.ReadAll(hist.Body)
	hist.Body.Close()
	var hgot struct {
		Acknowledgements []struct {
			FinishedSince int64 `json:"finished_since"`
			Seen          bool  `json:"seen"`
		} `json:"acknowledgements"`
	}
	if err := json.Unmarshal(hb, &hgot); err != nil || hist.StatusCode != http.StatusOK {
		t.Fatalf("history: status %d, body %s: %v", hist.StatusCode, hb, err)
	}
	if a := hgot.Acknowledgements; len(a) != 3 || !a[0].Seen || a[1].Seen || !a[2].Seen ||
		a[0].FinishedSince != done4.WaitingSince || a[1].FinishedSince != done4.WaitingSince || a[2].FinishedSince != done4.WaitingSince {
		t.Errorf("history acknowledgements %+v, want seen, undone, seen of %d", a, done4.WaitingSince)
	}

	// herdr's focus (ADR-0015): w1:p4 finishes again, the operator focuses
	// its pane in herdr, stays 4 s by the engine's clock and moves to w1:p2.
	// w1:p4 is in view while focused, then Seen and out of view, and w1:p2 in
	// view: agent events for both, which the replay must apply.
	fc.statusSub(t, "w1:p4").in <- herdr.Delivery[core.StatusEvent]{Event: core.StatusEvent{PaneID: "w1:p4", Status: core.StatusDone, AgentProgram: "claude"}}
	n = await("w1:p4 done again", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p4")
		return ok && a.Status == core.StatusDone && !a.Seen
	})
	fc.lifecycleEvent(core.LifecycleEvent{Kind: core.LifecycleFocused, PaneID: "w1:p4", WorkspaceID: "w1"})
	n = await("w1:p4 in view", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p4")
		b, _ := agent(v, "w1:p2")
		return ok && a.InView && !a.Seen && !b.InView
	})
	offset.Add(int64(4 * time.Second))
	fc.lifecycleEvent(core.LifecycleEvent{Kind: core.LifecycleFocused, PaneID: "w1:p2", WorkspaceID: "w1"})
	n = await("w1:p4 seen by leaving its pane", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p4")
		b, _ := agent(v, "w1:p2")
		return ok && a.Seen && !a.InView && b.InView
	})
	focus4, _ := agent(e.View(), "w1:p4")
	acks, err := st.Acknowledgements(context.Background(), focus4.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(acks) != 4 || acks[3].Source() != core.AckSourceHerdrFocus || acks[3].FinishedSince() != focus4.WaitingSince || !acks[3].Seen() {
		t.Errorf("acknowledgements %+v, want a fourth, herdr_focus, of the new finish", acks)
	}

	// herdr lost and back, the same instance (the shell kept its terminal),
	// with both Agents' terminals replaced: each is removed and re-added
	// under the same key.
	fc.setSnapshot(contractSnapshot(contractPane("w1:p2", "re-2", core.StatusIdle), contractPane("w1:p4", "re-4", core.StatusWorking), shellPane("t-shell")))
	fc.loseLifecycle()
	n = await("herdr lost", n, func(v engine.View) bool { return !v.Connection.Connected })
	n = await("herdr back", n, func(v engine.View) bool {
		a, ok := agent(v, "w1:p2")
		return v.Connection.Connected && len(v.Agents) == 2 && ok && a.Key.Epoch == 1 && a.Directory == "/src/w1:p2"
	})
	// herdr restarts with every terminal id new: the epoch increments, every
	// Agent is removed and reported again under it.
	fc.setSnapshot(contractSnapshot(contractPane("w1:p2", "new-2", core.StatusIdle), contractPane("w1:p4", "new-4", core.StatusWorking), shellPane("new-shell")))
	fc.loseLifecycle()
	await("epoch 2", n, func(v engine.View) bool {
		return v.Connection.Connected && len(v.Agents) == 2 && v.Agents[0].Key.Epoch == 2 && v.Agents[1].Key.Epoch == 2
	})

	// Wait until the stream has carried the engine's last publication in
	// full: a keepalive after an event with its sequence, sent by the same
	// loop, comes after every event of that publication.
	deadline := time.Now().Add(15 * time.Second)
	for {
		last := e.View().Seq
		id := strconv.FormatUint(last, 10)
		mu.Lock()
		seen, after := false, false
		for _, ev := range events {
			if ev.id == id {
				seen = true
			} else if seen && ev.event == eventKeepalive {
				after = true
			}
		}
		nViews := len(views)
		lastOwn := uint64(0)
		if nViews > 0 {
			lastOwn = views[nViews-1].Seq
		}
		mu.Unlock()
		if after && lastOwn == last && e.View().Seq == last {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stream never carried publication %d", last)
		}
		select {
		case <-got:
		case <-time.After(50 * time.Millisecond):
		}
	}
	s.cancel()
	mu.Lock()
	evs := slices.Clone(events)
	vs := slices.Clone(views)
	mu.Unlock()
	if len(vs) == 0 {
		t.Fatal("no publication")
	}

	// Replay in Node.
	type wireEvent struct {
		Name string `json:"name"`
		ID   string `json:"id"`
		Data string `json:"data"`
	}
	in := struct {
		Events []wireEvent `json:"events"`
	}{}
	for _, ev := range evs {
		if ev.unexpected != "" || ev.comment != "" || ev.event == "" {
			t.Fatalf("unexpected stream content %+v", ev)
		}
		in.Events = append(in.Events, wireEvent{Name: ev.event, ID: ev.id, Data: ev.data})
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join("testdata", "replay-board.mjs"), file)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("replay: %v\n%s", err, stderr.String())
	}
	var r replayed
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("replay output: %v\n%s", err, out)
	}
	if len(r.Warnings) > 0 || len(r.Reopens) > 0 {
		t.Fatalf("the Board rejected an event or saw a skipped publication: warnings %q, reopens %q", r.Warnings, r.Reopens)
	}

	// After every publication, the Board's model is the published View.
	for _, v := range vs {
		got, ok := r.Seqs[strconv.FormatUint(v.Seq, 10)]
		if !ok {
			t.Errorf("view %d: no event on the stream", v.Seq)
			continue
		}
		want := map[string]any{}
		for _, a := range v.Agents {
			ab, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			want[keyString(a.Key)] = generic(t, ab)
		}
		have := map[string]any{}
		for _, raw := range got.Agents {
			var k struct {
				Key core.AgentKey `json:"key"`
			}
			if err := json.Unmarshal(raw, &k); err != nil {
				t.Fatal(err)
			}
			have[keyString(k.Key)] = generic(t, raw)
		}
		if !reflect.DeepEqual(have, want) {
			t.Errorf("view %d: Board agents\n%v\nwant\n%v", v.Seq, have, want)
		}
		cb, err := json.Marshal(v.Connection)
		if err != nil {
			t.Fatal(err)
		}
		if c := generic(t, got.Connection); !reflect.DeepEqual(c, generic(t, cb)) {
			t.Errorf("view %d: Board connection %v, want %s", v.Seq, c, cb)
		}
		// The Board keeps the view's recording from its state event (ADR-0016).
		if got.Recording == nil || *got.Recording != v.Recording {
			t.Errorf("view %d: Board recording %v, want %v", v.Seq, got.Recording, v.Recording)
		}
		if !v.Recording {
			t.Errorf("view %d: recording false on a run with a loadable store", v.Seq)
		}
	}
	// Each acknowledgement change of w1:p4 reached the stream: the mark, the
	// undo and the second mark as an agent event carrying seen, the move to
	// working as the transition event of its publication.
	var p4 []core.AgentView
	var p4seqs []uint64
	for _, v := range vs {
		if a, ok := agent(v, "w1:p4"); ok && a.Key.Epoch == 1 {
			if len(p4) == 0 || p4[len(p4)-1].Seen != a.Seen || p4[len(p4)-1].Status != a.Status {
				p4, p4seqs = append(p4, a), append(p4seqs, v.Seq)
			}
		}
	}
	var changes []string
	for i, a := range p4 {
		changes = append(changes, fmt.Sprintf("%s/%v", a.Status, a.Seen))
		if i == 0 {
			continue
		}
		id := strconv.FormatUint(p4seqs[i], 10)
		// A publication that removes w1:p4 and adds it again (its terminal
		// replaced) carries the new Agent's first sight as an agent event.
		readded := false
		for _, ev := range evs {
			readded = readded || ev.id == id && ev.event == eventRemoved && strings.Contains(ev.data, `"pane_id":"w1:p4"`)
		}
		want := eventAgent
		if a.Status != p4[i-1].Status && !readded {
			want = eventTransition
		}
		found := false
		for _, ev := range evs {
			if ev.id != id || ev.event != want || !strings.Contains(ev.data, `"pane_id":"w1:p4"`) {
				continue
			}
			if strings.Contains(ev.data, fmt.Sprintf(`"seen":%v`, a.Seen)) {
				found = true
			}
		}
		if !found {
			t.Errorf("publication %d (w1:p4 %s seen %v): no %s event for it on the stream", p4seqs[i], a.Status, a.Seen, want)
		}
	}
	if g, w := strings.Join(changes, " "), "working/false done/false done/true done/false done/true working/false done/false done/true working/false"; g != w {
		t.Errorf("w1:p4's published changes %q, want %q", g, w)
	}

	// The focus move reached the stream as agent events carrying in_view:
	// w1:p4 in view, then w1:p4 seen and out of view beside w1:p2 in view.
	onStream := func(pane string, parts ...string) bool {
		for _, ev := range evs {
			if ev.event != eventAgent || !strings.Contains(ev.data, `"pane_id":"`+pane+`"`) {
				continue
			}
			all := true
			for _, part := range parts {
				all = all && strings.Contains(ev.data, part)
			}
			if all {
				return true
			}
		}
		return false
	}
	if !onStream("w1:p4", `"in_view":true`, `"seen":false`) {
		t.Error("no agent event with w1:p4 in view")
	}
	if !onStream("w1:p4", `"in_view":false`, `"seen":true`) {
		t.Error("no agent event with w1:p4 seen and out of view")
	}
	if !onStream("w1:p2", `"in_view":true`) {
		t.Error("no agent event with w1:p2 in view")
	}

	// The scenarios above publish removals, a re-add under a new epoch and
	// connection changes: the replay saw each kind.
	kinds := map[string]bool{}
	for _, ev := range evs {
		kinds[ev.event] = true
	}
	for _, k := range []string{eventState, eventAgent, eventRemoved, eventTransition, eventConnection, eventKeepalive} {
		if !kinds[k] {
			t.Errorf("no %s event on the stream", k)
		}
	}
}
