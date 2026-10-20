package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
)

// fakeEngine stands in for the engine: a fixed View, and subscriptions the
// test drives. Publications in pre are queued on each new subscription before
// Subscribe returns, as if published between subscribing and reading the
// View.
type fakeEngine struct {
	view engine.View
	pre  []engine.Publication
	// gone makes Subscribe hand out closed channels, as the engine does once
	// it has stopped.
	gone bool

	mu      sync.Mutex
	chans   []chan engine.Publication
	buffers []int
	unsubs  atomic.Int32
}

func (f *fakeEngine) View() engine.View { return f.view }

func (f *fakeEngine) Subscribe(buffer int) (*engine.ViewSubscription, func()) {
	c := make(chan engine.Publication, buffer+len(f.pre))
	for _, p := range f.pre {
		c <- p // never blocks: c has room for every one
	}
	if f.gone {
		close(c)
	}
	f.mu.Lock()
	f.chans = append(f.chans, c)
	f.buffers = append(f.buffers, buffer)
	f.mu.Unlock()
	var once sync.Once
	return &engine.ViewSubscription{C: c}, func() { once.Do(func() { f.unsubs.Add(1) }) }
}

// sub returns the n-th subscription's channel, waiting for it.
func (f *fakeEngine) sub(t *testing.T, n int) chan engine.Publication {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		if len(f.chans) > n {
			c := f.chans[n]
			f.mu.Unlock()
			return c
		}
		f.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no subscription %d", n)
		}
		time.Sleep(time.Millisecond)
	}
}

// subs returns how many subscriptions were taken.
func (f *fakeEngine) subs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.chans)
}

// send delivers p on c, failing the test if it is not taken within 5 s.
func send(t *testing.T, c chan engine.Publication, p engine.Publication) {
	t.Helper()
	select {
	case c <- p:
	case <-time.After(5 * time.Second):
		t.Fatal("publication not taken within 5 s")
	}
}

// eventsView is View Seq 7 with two Agents.
func eventsView() engine.View {
	v := testView()
	v.Agents = v.Agents[:2]
	v.Headers = core.WorkspaceHeaders(v.Agents)
	return v
}

// sseEvent is one parsed event, or a comment when comment is set, or a line
// the reader did not expect when unexpected is set.
type sseEvent struct {
	comment, retry, event, id, data string
	unexpected                      string
}

// stream is an open /events response read on its own goroutine.
type stream struct {
	resp   *http.Response
	events chan sseEvent
	ended  chan struct{} // closed at EOF or a read error
	cancel context.CancelFunc
}

// eventsServer serves h; returned counts handler returns.
func eventsServer(t *testing.T, h http.Handler) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var returned atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer returned.Add(1)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &returned
}

func openStream(t *testing.T, srv *httptest.Server, hdr map[string]string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	s := &stream{resp: resp, events: make(chan sseEvent, 100), ended: make(chan struct{}), cancel: cancel}
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		defer close(s.ended)
		r := bufio.NewReader(resp.Body)
		var e sseEvent
		emit := func(ev sseEvent) bool {
			select {
			case s.events <- ev:
				return true
			case <-done:
				return false
			}
		}
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSuffix(line, "\n")
			switch {
			case line == "":
				if !emit(e) {
					return
				}
				e = sseEvent{}
			case strings.HasPrefix(line, "retry: "):
				e.retry = line[len("retry: "):]
			case strings.HasPrefix(line, ": "):
				e.comment = line[2:]
			case strings.HasPrefix(line, "event: "):
				e.event = line[len("event: "):]
			case strings.HasPrefix(line, "id: "):
				e.id = line[len("id: "):]
			case strings.HasPrefix(line, "data: "):
				e.data = line[len("data: "):]
			default:
				if !emit(sseEvent{unexpected: line}) {
					return
				}
			}
		}
	}()
	return s
}

func (s *stream) next(t *testing.T) sseEvent {
	t.Helper()
	check := func(e sseEvent) sseEvent {
		t.Helper()
		if e.unexpected != "" {
			t.Fatalf("unexpected line %q", e.unexpected)
		}
		return e
	}
	select {
	case e := <-s.events:
		return check(e)
	case <-s.ended:
		select {
		case e := <-s.events:
			return check(e)
		default:
		}
		t.Fatal("stream ended; want an event")
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5 s")
	}
	return sseEvent{}
}

// waitEnded asserts the stream ends with no further event.
func (s *stream) waitEnded(t *testing.T, within time.Duration) {
	t.Helper()
	select {
	case <-s.ended:
	case <-time.After(within):
		t.Fatalf("stream still open after %v", within)
	}
	select {
	case e := <-s.events:
		t.Errorf("event %+v before the end, want none", e)
	default:
	}
}

func waitCount(t *testing.T, what string, n *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for n.Load() != want {
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d, want %d", what, n.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func newEvents(t *testing.T, f *fakeEngine) (*Handler, *httptest.Server, *atomic.Int32) {
	t.Helper()
	h := New(Options{View: f.View, Subscribe: f.Subscribe, History: noHistory})
	srv, returned := eventsServer(t, h)
	return h, srv, returned
}

// checkState asserts e is the opening state event for v: id v.Seq and data
// exactly the /api/state body on one line.
func checkState(t *testing.T, h http.Handler, e sseEvent, v engine.View) {
	t.Helper()
	if e.event != "state" || e.id != "7" || e.retry != "2000" {
		t.Fatalf("first event %q id %q retry %q, want state id 7 with retry 2000", e.event, e.id, e.retry)
	}
	w := do(h, http.MethodGet, "/api/state", "127.0.0.1:7318", nil)
	if want := strings.TrimSuffix(w.Body.String(), "\n"); e.data != want {
		t.Errorf("state data\n%s\nwant the /api/state body\n%s", e.data, want)
	}
	want, _ := json.Marshal(v)
	if e.data != string(want) {
		t.Errorf("state data\n%s\nwant\n%s", e.data, want)
	}
}

func TestEventsOpening(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h, srv, _ := newEvents(t, f)
	s := openStream(t, srv, map[string]string{"Origin": "http://evil.example"})
	if s.resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", s.resp.StatusCode)
	}
	for k, want := range map[string]string{
		"Content-Type":           "text/event-stream",
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"X-Accel-Buffering":      "no",
	} {
		if got := s.resp.Header.Get(k); got != want {
			t.Errorf("%s %q, want %q", k, got, want)
		}
	}
	for k := range s.resp.Header {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Errorf("CORS header %s sent", k)
		}
	}
	checkState(t, h, s.next(t), f.view)
	f.mu.Lock()
	buffers := f.buffers
	f.mu.Unlock()
	if len(buffers) != 1 || buffers[0] != 64 {
		t.Errorf("subscribed with buffers %v, want [64]", buffers)
	}
}

// TestEventsLastEventID sends Last-Event-ID: it is ignored, and the stream
// starts with state.
func TestEventsLastEventID(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h, srv, _ := newEvents(t, f)
	s := openStream(t, srv, map[string]string{"Last-Event-ID": "3"})
	checkState(t, h, s.next(t), f.view)
}

func agentPub(seq uint64, pane string) engine.Publication {
	v := eventsView()
	v.Seq = seq
	a := v.Agents[0]
	a.Key.PaneID = pane
	return engine.Publication{View: v, Changes: []engine.Change{{Kind: engine.ChangeAgent, Agent: a}}}
}

// TestEventsNoGapStale queues publications 6 and 7 (stale: the state covers
// them) and 8 (published right after subscribing) before the View is read,
// then publishes 9: the stream sends state, then 8, then 9.
func TestEventsNoGapStale(t *testing.T) {
	f := &fakeEngine{view: eventsView(), pre: []engine.Publication{agentPub(6, "stale6"), agentPub(7, "stale7"), agentPub(8, "p8")}}
	h, srv, _ := newEvents(t, f)
	s := openStream(t, srv, nil)
	checkState(t, h, s.next(t), f.view)
	send(t, f.sub(t, 0), agentPub(9, "p9"))
	for _, want := range []struct{ id, pane string }{{"8", "p8"}, {"9", "p9"}} {
		e := s.next(t)
		var a core.AgentView
		if err := json.Unmarshal([]byte(e.data), &struct {
			Key *core.AgentKey `json:"key"`
		}{&a.Key}); err != nil {
			t.Fatal(err)
		}
		if e.event != "agent" || e.id != want.id || a.Key.PaneID != want.pane {
			t.Errorf("event %q id %q pane %q, want agent id %s pane %s", e.event, e.id, a.Key.PaneID, want.id, want.pane)
		}
	}
}

func TestEventsKinds(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	_, srv, _ := newEvents(t, f)
	s := openStream(t, srv, nil)
	s.next(t)

	v := eventsView()
	v.Seq = 8
	key := core.AgentKey{Host: "h", Server: "s", Epoch: 1, PaneID: "w1:p1"}
	prev := core.StatusWorking
	tr, err := core.NewTransition(core.TransitionInput{
		Key: key, AgentProgram: "claude", Profile: "claude", Previous: &prev, New: core.StatusBlocked,
		ObservedAt: 5000, Provenance: core.ProvenanceLive,
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked := v.Agents[0] // needs_you
	if blocked.Key != key || blocked.Class != core.ClassNeedsYou {
		t.Fatalf("fixture agent %+v", blocked)
	}
	other := v.Agents[1]
	conn := core.ConnectionTruth{Connected: false, LastSnapshotAt: 4000, HerdrVersion: "0.9.5",
		HerdrMinimum: core.HerdrMinimum, Gate: core.GateOK, ResyncGuaranteed: true}
	send(t, f.sub(t, 0), engine.Publication{View: v, Changes: []engine.Change{
		{Kind: engine.ChangeAgent, Agent: other},
		{Kind: engine.ChangeRemoved, Key: other.Key, Agent: other},
		{Kind: engine.ChangeTransition, Transition: tr, Agent: blocked},
		{Kind: engine.ChangeResync, Reason: "events_lost"},
		{Kind: engine.ChangeConnection, Connection: conn},
	}})

	js := func(x any) string {
		b, err := json.Marshal(x)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	keyJSON := `{"host":"h","server":"s","epoch":1,"pane_id":"w1:p1"}`
	want := []sseEvent{
		{event: "agent", id: "8", data: js(other)},
		{event: "removed", id: "8", data: `{"key":` + js(other.Key) + `}`},
		{event: "transition", id: "8", data: `{"key":` + keyJSON + `,"transition":{"agent_program":"claude","profile":"claude",` +
			`"previous":"working","new":"blocked","observed_at":5000,"provenance":"live","possibly_spurious":false},"agent":` + js(blocked) + `}`},
		{event: "resync", id: "8", data: `{"reason":"events_lost"}`},
		{event: "connection", id: "8", data: js(conn)},
	}
	for i, w := range want {
		if got := s.next(t); got != w {
			t.Errorf("event %d\n%+v\nwant\n%+v", i, got, w)
		}
	}
}

// TestEventNames pins every engine change kind to its public event name.
func TestEventNames(t *testing.T) {
	for kind, want := range map[engine.ChangeKind]string{
		engine.ChangeAgent:      "agent",
		engine.ChangeRemoved:    "removed",
		engine.ChangeTransition: "transition",
		engine.ChangeResync:     "resync",
		engine.ChangeConnection: "connection",
	} {
		if name, _, ok := changeData(engine.Change{Kind: kind}); !ok || name != want {
			t.Errorf("kind %q: name %q (%v), want %q", kind, name, ok, want)
		}
	}
}

// TestWireTransitionFull checks the fields omitted when unknown are present
// when known, and a resync's detail when set.
func TestWireTransitionFull(t *testing.T) {
	prev := core.StatusIdle
	order := uint64(42)
	tr, err := core.NewTransition(core.TransitionInput{
		Key:          core.AgentKey{Host: "h", Server: "s", Epoch: core.BackfillEpoch, PaneID: "p"},
		AgentProgram: "codex", Profile: "work", Previous: &prev, New: core.StatusDone, HerdrOrder: &order,
		HerdrVersion: "0.9.5", ObservedAt: 9, Provenance: core.ProvenanceBackfill, ImportKey: "k", PossiblySpurious: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(newWireTransition(tr))
	want := `{"agent_program":"codex","profile":"work","previous":"idle","new":"done","herdr_order":42,` +
		`"herdr_version":"0.9.5","observed_at":9,"provenance":"backfill","possibly_spurious":true}`
	if string(b) != want {
		t.Errorf("transition\n%s\nwant\n%s", b, want)
	}
	_, data, ok := changeData(engine.Change{Kind: engine.ChangeResync, Reason: "connect", Detail: "epoch incremented"})
	b, _ = json.Marshal(data)
	if !ok || string(b) != `{"reason":"connect","detail":"epoch incremented"}` {
		t.Errorf("resync %s %v", b, ok)
	}
	if _, _, ok := changeData(engine.Change{Kind: "bogus"}); ok {
		t.Error("an unknown kind has data")
	}
}

// TestEventsOverflow closes the subscription as the engine does for a slow
// client: the response ends, the handler unsubscribes, and a new GET starts
// with state.
func TestEventsOverflow(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h, srv, returned := newEvents(t, f)
	s := openStream(t, srv, nil)
	s.next(t)
	close(f.sub(t, 0))
	s.waitEnded(t, 5*time.Second)
	waitCount(t, "handler returns", returned, 1)
	waitCount(t, "unsubscribes", &f.unsubs, 1)
	s2 := openStream(t, srv, nil)
	checkState(t, h, s2.next(t), f.view)
}

func TestEventsKeepalive(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h := New(Options{View: f.View, Subscribe: f.Subscribe, History: noHistory})
	h.keepalive = 20 * time.Millisecond
	srv, _ := eventsServer(t, h)
	s := openStream(t, srv, nil)
	s.next(t)
	if e := s.next(t); e != (sseEvent{comment: "keepalive"}) {
		t.Errorf("event %+v, want the keepalive comment", e)
	}
}

// TestEventsClientGone cancels the request: the handler returns and
// unsubscribes.
func TestEventsClientGone(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	_, srv, returned := newEvents(t, f)
	s := openStream(t, srv, nil)
	s.next(t)
	s.cancel()
	waitCount(t, "handler returns", returned, 1)
	waitCount(t, "unsubscribes", &f.unsubs, 1)
}

// TestEventsWriteFails ends the stream on a failed write: the client's
// connection is gone, and the next publication's write fails.
func TestEventsWriteFails(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h := New(Options{View: f.View, Subscribe: f.Subscribe, History: noHistory})
	w := &failWriter{header: http.Header{}}
	r := httptest.NewRequest(http.MethodGet, "/events", nil)
	r.Host = "localhost"
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, r)
	}()
	send(t, f.sub(t, 0), agentPub(8, "p8"))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler went on after a failed write")
	}
	if f.unsubs.Load() != 1 {
		t.Errorf("unsubscribes %d, want 1", f.unsubs.Load())
	}
}

// failWriter accepts the state event, then fails every write.
type failWriter struct {
	header http.Header
	writes int
}

func (w *failWriter) Header() http.Header { return w.header }
func (w *failWriter) WriteHeader(int)     {}
func (w *failWriter) Write(b []byte) (int, error) {
	w.writes++
	if w.writes > 1 {
		return 0, io.ErrClosedPipe
	}
	return len(b), nil
}
func (w *failWriter) Flush() {}

// TestEventsCloseStreams ends an open stream, and a later /events gets 503
// without subscribing.
func TestEventsCloseStreams(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h, srv, returned := newEvents(t, f)
	s := openStream(t, srv, nil)
	s.next(t)
	h.CloseStreams()
	h.CloseStreams() // idempotent
	s.waitEnded(t, time.Second)
	waitCount(t, "handler returns", returned, 1)
	waitCount(t, "unsubscribes", &f.unsubs, 1)

	checkError(t, do(h, http.MethodGet, "/events", "localhost", nil), http.StatusServiceUnavailable, "unavailable")
	if n := f.subs(); n != 1 {
		t.Errorf("%d subscriptions, want 1: a request after CloseStreams subscribed", n)
	}
}

// TestEventsEngineGone gets a closed subscription, as from a stopped engine:
// 503, and the subscription is ended.
func TestEventsEngineGone(t *testing.T) {
	f := &fakeEngine{view: eventsView(), gone: true}
	h := New(Options{View: f.View, Subscribe: f.Subscribe, History: noHistory})
	checkError(t, do(h, http.MethodGet, "/events", "localhost", nil), http.StatusServiceUnavailable, "unavailable")
	if f.unsubs.Load() != 1 {
		t.Errorf("unsubscribes %d, want 1", f.unsubs.Load())
	}
}

// TestEventsTwoStreams closes one stream's subscription: the other goes on
// receiving.
func TestEventsTwoStreams(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	_, srv, _ := newEvents(t, f)
	s1 := openStream(t, srv, nil)
	s1.next(t)
	s2 := openStream(t, srv, nil)
	s2.next(t)
	close(f.sub(t, 0))
	s1.waitEnded(t, 5*time.Second)
	send(t, f.sub(t, 1), agentPub(8, "p8"))
	if e := s2.next(t); e.event != "agent" || e.id != "8" {
		t.Errorf("second stream got %+v, want agent id 8", e)
	}
}

// TestEventsStalledClient connects a client that never reads: once the
// socket fills, a write times out, and the handler returns and unsubscribes.
func TestEventsStalledClient(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h := New(Options{View: f.View, Subscribe: f.Subscribe, History: noHistory})
	h.writeTimeout = 50 * time.Millisecond
	srv, returned := eventsServer(t, h)
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /events HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	c := f.sub(t, 0)
	big := agentPub(0, "big")
	big.Changes[0].Agent.ContextLine = strings.Repeat("x", 1<<20)
	deadline := time.Now().Add(10 * time.Second)
	for seq := uint64(8); returned.Load() == 0; seq++ {
		if time.Now().After(deadline) {
			t.Fatal("the handler is still writing to a client that never reads")
		}
		p := big
		p.View.Seq = seq
		select {
		case c <- p:
		case <-time.After(10 * time.Millisecond):
		}
	}
	waitCount(t, "unsubscribes", &f.unsubs, 1)
}

func TestEventsRules(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h := New(Options{View: f.View, Subscribe: f.Subscribe, History: noHistory})
	for _, m := range []string{http.MethodPost, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		w := do(h, m, "/events", "127.0.0.1:7318", map[string]string{"Origin": "http://evil.example"})
		if m == http.MethodHead {
			// A HEAD response has no body to decode.
			checkCommon(t, w)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("HEAD: status %d, want 405", w.Code)
			}
		} else {
			checkError(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
		}
		if got := w.Header().Get("Allow"); got != "GET" {
			t.Errorf("%s: Allow %q, want GET", m, got)
		}
	}
	for _, host := range []string{"evil.example", "", "127.0.0.1.evil.example"} {
		checkError(t, do(h, http.MethodGet, "/events", host, nil), http.StatusForbidden, "host_not_allowed")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.chans) != 0 {
		t.Errorf("%d subscriptions taken by refused requests", len(f.chans))
	}
}

// transitionBudget is how long a published Transition may take to reach an
// open /events client (Story 3.8, FR-4).
const transitionBudget = time.Second

// TestEventsPublicationToDeliveryBudget publishes Transitions, as the engine
// does after recording each, and checks every one reaches an open /events
// client, as a transition event carrying the Agent's new view, within one
// second of publication. The budget from recording to delivery, through the
// real engine and a fake herdr, is cmd/herddash's TestServeEvents.
func TestEventsPublicationToDeliveryBudget(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	_, srv, _ := newEvents(t, f)
	s := openStream(t, srv, nil)
	if e := s.next(t); e.event != "state" {
		t.Fatalf("first event %q, want state", e.event)
	}
	c := f.sub(t, 0)

	v := eventsView()
	const i = 1
	agent := v.Agents[i]
	steps := []core.Status{core.StatusWorking, core.StatusBlocked, core.StatusDone, core.StatusIdle, core.StatusBlocked}
	if agent.Status == steps[0] {
		t.Fatalf("fixture agent is already %s", agent.Status)
	}
	for n, st := range steps {
		prev := agent.Status
		tr, err := core.NewTransition(core.TransitionInput{
			Key: agent.Key, AgentProgram: agent.AgentProgram, Profile: agent.Profile, Previous: &prev, New: st,
			ObservedAt: int64(1000 + n), Provenance: core.ProvenanceLive,
		})
		if err != nil {
			t.Fatal(err)
		}
		agent.Status = st
		v.Seq++
		v.Agents = slices.Clone(v.Agents)
		v.Agents[i] = agent
		start := time.Now()
		send(t, c, engine.Publication{View: v, Changes: []engine.Change{{Kind: engine.ChangeTransition, Transition: tr, Agent: agent}}})
		e := s.next(t)
		if took := time.Since(start); took > transitionBudget {
			t.Fatalf("transition %d (%s to %s) delivered after %v, want within %v", n, prev, st, took, transitionBudget)
		}
		if e.event != "transition" || e.id != strconv.FormatUint(v.Seq, 10) {
			t.Fatalf("event %q id %q, want transition id %d", e.event, e.id, v.Seq)
		}
		var got struct {
			Key        core.AgentKey  `json:"key"`
			Transition wireTransition `json:"transition"`
			Agent      struct {
				Key    core.AgentKey `json:"key"`
				Status core.Status   `json:"status"`
			} `json:"agent"`
		}
		if err := json.Unmarshal([]byte(e.data), &got); err != nil {
			t.Fatal(err)
		}
		if got.Key != agent.Key || got.Agent.Key != agent.Key || got.Agent.Status != st ||
			got.Transition.New != st || got.Transition.Previous == nil || *got.Transition.Previous != prev {
			t.Errorf("transition %d: %+v, want %s to %s for %+v", n, got, prev, st, agent.Key)
		}
	}
}
