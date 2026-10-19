package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
)

// wait bounds every wait in these tests; quiet is how long "nothing more
// happens" is watched for.
const (
	wait  = 5 * time.Second
	quiet = 100 * time.Millisecond
)

// fakeSub is a scripted subscription. The test pushes deliveries; Close ends
// it and closes the delivery channel, as herdr.Subscription does. A leaky one
// records Close but goes on delivering until end, which stands in for a
// message already on its way when the subscription was closed.
type fakeSub[T any] struct {
	in        chan herdr.Delivery[T]
	out       chan herdr.Delivery[T]
	closed    chan struct{} // ends delivery
	once      sync.Once
	leaky     bool
	closeSeen atomic.Bool
	delivered atomic.Int32 // deliveries the reader has taken
}

func newFakeSub[T any](leaky bool) *fakeSub[T] {
	s := &fakeSub[T]{in: make(chan herdr.Delivery[T], 16), out: make(chan herdr.Delivery[T]), closed: make(chan struct{}), leaky: leaky}
	go func() {
		defer close(s.out)
		for {
			select {
			case d := <-s.in:
				select {
				case s.out <- d:
					s.delivered.Add(1)
				case <-s.closed:
					return
				}
				if d.Health != nil && !s.leaky {
					return
				}
			case <-s.closed:
				return
			}
		}
	}()
	return s
}

func (s *fakeSub[T]) Deliveries() <-chan herdr.Delivery[T] { return s.out }

func (s *fakeSub[T]) Close() error {
	s.closeSeen.Store(true)
	if !s.leaky {
		s.end()
	}
	return nil
}

// end stops delivery, for a leaky subscription too.
func (s *fakeSub[T]) end() { s.once.Do(func() { close(s.closed) }) }

func (s *fakeSub[T]) isClosed() bool { return s.closeSeen.Load() }

// awaitDelivered waits until the reader has taken n deliveries in all.
func (s *fakeSub[T]) awaitDelivered(t *testing.T, n int32) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for s.delivered.Load() < n {
		if time.Now().After(deadline) {
			t.Fatal("delivery not taken")
		}
		time.Sleep(time.Millisecond)
	}
}

func (s *fakeSub[T]) event(ev T) { s.in <- herdr.Delivery[T]{Event: ev} }

func (s *fakeSub[T]) health(h *herdr.Health) { s.in <- herdr.Delivery[T]{Health: h} }

// fakeClient is a scripted herdr. It records every call in order. Snapshot
// returns the current snapshot, or, while gated, blocks until the test
// releases it.
type fakeClient struct {
	t *testing.T

	mu        sync.Mutex
	calls     []string
	snap      core.Snapshot
	snapErr   error
	pingErr   error
	lifeErr   error
	rejected  map[string]bool
	gated     bool
	lifecycle *fakeSub[core.LifecycleEvent]
	status    map[string][]*fakeSub[core.StatusEvent]
	// fails makes the next n calls of a kind ("ping", "lifecycle",
	// "snapshot") fail with errBoom.
	fails map[string]int
	// leaky makes every subscription from now on leaky.
	leaky bool
	// hold, when set, holds the next snapshot read, whatever its context,
	// until the channel closes; it then returns the snapshot of that moment.
	hold      chan struct{}
	pingTimes []time.Time
	// versions scripts what each ping answers, in order; the last one stays.
	// Empty means "0.9.5".
	versions []string
	ends     []func() // every subscription's end
	// holdSub holds the next status subscribe for a pane, whatever its
	// context, until its channel closes.
	holdSub map[string]chan struct{}
	// subTimes is when each status subscribe for a pane was called.
	subTimes map[string][]time.Time

	// procs is each pane's foreground process; a pane with none answers
	// herdr.ErrNoForegroundProcess.
	procs map[string]core.ForegroundProcess

	release   chan struct{}
	snapCalls chan struct{}
	awaited   map[string]int
	snapshots atomic.Int32
	snapTimes []time.Time
}

func newFakeClient(t *testing.T, snap core.Snapshot) *fakeClient {
	return &fakeClient{
		t:         t,
		snap:      snap,
		rejected:  map[string]bool{},
		status:    map[string][]*fakeSub[core.StatusEvent]{},
		fails:     map[string]int{},
		release:   make(chan struct{}),
		snapCalls: make(chan struct{}, 1024),
		awaited:   map[string]int{},
		subTimes:  map[string][]time.Time{},
		procs:     map[string]core.ForegroundProcess{},
	}
}

func (f *fakeClient) ForegroundProcess(ctx context.Context, paneID string) (core.ForegroundProcess, error) {
	f.record("process " + paneID)
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.procs[paneID]
	if !ok {
		return core.ForegroundProcess{}, fmt.Errorf("herdr: pane.process_info %s: %w", paneID, herdr.ErrNoForegroundProcess)
	}
	return fp, nil
}

// setProc makes pid the foreground process of paneID.
func (f *fakeClient) setProc(paneID string, pid int) {
	f.mu.Lock()
	f.procs[paneID] = core.ForegroundProcess{PID: pid, Name: "claude"}
	f.mu.Unlock()
}

// processCalls counts the ForegroundProcess calls so far.
func (f *fakeClient) processCalls() int {
	n := 0
	for _, c := range f.callList() {
		if strings.HasPrefix(c, "process ") {
			n++
		}
	}
	return n
}

// fakeEnviron is a scripted process environment table, standing in for
// procenv so no engine test reads a real process.
type fakeEnviron struct {
	mu   sync.Mutex
	vars map[int]map[string]string
	errs map[int]error
	// block, while set, holds every read until it closes.
	block chan struct{}
}

func newFakeEnviron(t *testing.T) *fakeEnviron {
	f := &fakeEnviron{vars: map[int]map[string]string{}, errs: map[int]error{}}
	t.Cleanup(func() { f.unblock() })
	return f
}

func (f *fakeEnviron) lookup(pid int, name string) (string, bool, error) {
	f.mu.Lock()
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[pid]; err != nil {
		return "", false, err
	}
	v, ok := f.vars[pid][name]
	return v, ok, nil
}

// set sets name in pid's environment; unset removes it.
func (f *fakeEnviron) set(pid int, name, value string) {
	f.mu.Lock()
	if f.vars[pid] == nil {
		f.vars[pid] = map[string]string{}
	}
	f.vars[pid][name] = value
	f.mu.Unlock()
}

func (f *fakeEnviron) fail(pid int, err error) {
	f.mu.Lock()
	f.errs[pid] = err
	f.mu.Unlock()
}

// hold makes every read from now on block until unblock.
func (f *fakeEnviron) hold() {
	f.mu.Lock()
	f.block = make(chan struct{})
	f.mu.Unlock()
}

func (f *fakeEnviron) unblock() {
	f.mu.Lock()
	if f.block != nil {
		close(f.block)
		f.block = nil
	}
	f.mu.Unlock()
}

func (f *fakeClient) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

// failing consumes one scripted failure of kind; f.mu must be held.
func (f *fakeClient) failing(kind string) bool {
	if f.fails[kind] > 0 {
		f.fails[kind]--
		return true
	}
	return false
}

func (f *fakeClient) Ping(ctx context.Context) (string, error) {
	f.record("ping")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingTimes = append(f.pingTimes, time.Now())
	if f.failing("ping") {
		return "", errBoom
	}
	v := "0.9.5"
	if len(f.versions) > 0 {
		v = f.versions[0]
		if len(f.versions) > 1 {
			f.versions = f.versions[1:]
		}
	}
	return v, f.pingErr
}

// setVersions scripts the versions the next pings answer, in order; the last
// one is answered from then on.
func (f *fakeClient) setVersions(vs ...string) {
	f.mu.Lock()
	f.versions = slices.Clone(vs)
	f.mu.Unlock()
}

func (f *fakeClient) Snapshot(ctx context.Context) (core.Snapshot, error) {
	f.record("snapshot")
	f.snapshots.Add(1)
	f.mu.Lock()
	f.snapTimes = append(f.snapTimes, time.Now())
	f.mu.Unlock()
	select {
	case f.snapCalls <- struct{}{}:
	default:
	}
	f.mu.Lock()
	gated, hold := f.gated, f.hold
	f.hold = nil
	f.mu.Unlock()
	if hold != nil {
		<-hold
	} else if gated {
		select {
		case <-f.release:
		case <-ctx.Done():
			return core.Snapshot{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing("snapshot") {
		return core.Snapshot{}, errBoom
	}
	return f.snap, f.snapErr
}

func (f *fakeClient) SubscribeLifecycle(ctx context.Context) (Subscription[core.LifecycleEvent], error) {
	f.record("lifecycle")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lifeErr != nil {
		return nil, f.lifeErr
	}
	if f.failing("lifecycle") {
		return nil, errBoom
	}
	f.lifecycle = newFakeSub[core.LifecycleEvent](f.leaky)
	f.ends = append(f.ends, f.lifecycle.end)
	return f.lifecycle, nil
}

func (f *fakeClient) SubscribeStatus(ctx context.Context, paneID string) (Subscription[core.StatusEvent], error) {
	f.record("status " + paneID)
	f.mu.Lock()
	f.subTimes[paneID] = append(f.subTimes[paneID], time.Now())
	hold := f.holdSub[paneID]
	delete(f.holdSub, paneID)
	f.mu.Unlock()
	if hold != nil {
		<-hold
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejected[paneID] {
		return nil, fmt.Errorf("herdr: subscribe: %w", &herdr.Health{Kind: herdr.HealthRejected, Code: "pane_not_found", Message: "pane " + paneID + " not found"})
	}
	s := newFakeSub[core.StatusEvent](f.leaky)
	f.ends = append(f.ends, s.end)
	f.status[paneID] = append(f.status[paneID], s)
	return s, nil
}

// setSnapshot replaces what the next snapshot read returns.
func (f *fakeClient) setSnapshot(s core.Snapshot) {
	f.mu.Lock()
	f.snap = s
	f.mu.Unlock()
}

// endAll stops delivery on every subscription ever made, leaky ones too.
func (f *fakeClient) endAll() {
	f.mu.Lock()
	ends := slices.Clone(f.ends)
	f.mu.Unlock()
	for _, end := range ends {
		end()
	}
}

// fail makes the next n calls of kind fail.
func (f *fakeClient) fail(kind string, n int) {
	f.mu.Lock()
	f.fails[kind] = n
	f.mu.Unlock()
}

// holdNext holds the next snapshot read until the returned channel closes.
func (f *fakeClient) holdNext() chan struct{} {
	c := make(chan struct{})
	f.mu.Lock()
	f.hold = c
	f.mu.Unlock()
	return c
}

func (f *fakeClient) pings() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.pingTimes)
}

// awaitPings waits until herdr has been pinged n times in all.
func (f *fakeClient) awaitPings(n int) {
	f.t.Helper()
	deadline := time.Now().Add(wait)
	for len(f.pings()) < n {
		if time.Now().After(deadline) {
			f.t.Fatalf("%d pings, want %d", len(f.pings()), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *fakeClient) setGated(g bool) {
	f.mu.Lock()
	f.gated = g
	f.mu.Unlock()
}

func (f *fakeClient) reject(paneID string) {
	f.mu.Lock()
	f.rejected[paneID] = true
	f.mu.Unlock()
}

func (f *fakeClient) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// subscribeTimes returns when each status subscribe for paneID was called.
func (f *fakeClient) subscribeTimes(paneID string) []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.subTimes[paneID])
}

// holdSubscribes holds the next status subscribe of each pane until the
// returned channel closes.
func (f *fakeClient) holdSubscribes(paneIDs ...string) chan struct{} {
	c := make(chan struct{})
	f.mu.Lock()
	if f.holdSub == nil {
		f.holdSub = map[string]chan struct{}{}
	}
	for _, id := range paneIDs {
		f.holdSub[id] = c
	}
	f.mu.Unlock()
	return c
}

// awaitStatusSubs waits until paneID has had n successful status
// subscriptions in all, and returns the latest.
func (f *fakeClient) awaitStatusSubs(paneID string, n int) *fakeSub[core.StatusEvent] {
	f.t.Helper()
	deadline := time.Now().Add(wait)
	for {
		f.mu.Lock()
		subs := f.status[paneID]
		f.mu.Unlock()
		if len(subs) >= n {
			return subs[len(subs)-1]
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("%d status subscriptions for %s, want %d", len(subs), paneID, n)
		}
		time.Sleep(time.Millisecond)
	}
}

// statusSub returns the latest status subscription for a pane.
func (f *fakeClient) statusSub(paneID string) *fakeSub[core.StatusEvent] {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	subs := f.status[paneID]
	if len(subs) == 0 {
		f.t.Fatalf("no status subscription for %s", paneID)
	}
	return subs[len(subs)-1]
}

func (f *fakeClient) lifecycleSub() *fakeSub[core.LifecycleEvent] {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lifecycle
}

// awaitSnapshotCall waits for the next snapshot call to begin.
func (f *fakeClient) awaitSnapshotCall() {
	f.t.Helper()
	select {
	case <-f.snapCalls:
	case <-time.After(wait):
		f.t.Fatal("no snapshot read began")
	}
}

// noSnapshotCall asserts that no snapshot read begins for a while.
func (f *fakeClient) noSnapshotCall() {
	f.t.Helper()
	select {
	case <-f.snapCalls:
		f.t.Fatal("an unexpected snapshot read began")
	case <-time.After(quiet):
	}
}

// drainSnapshotCalls forgets snapshot calls already begun.
func (f *fakeClient) drainSnapshotCalls() {
	for {
		select {
		case <-f.snapCalls:
		default:
			return
		}
	}
}

// awaitSubscribe waits for one more status subscribe call for paneID than
// it has waited for before: each call to it consumes one.
func (f *fakeClient) awaitSubscribe(paneID string) {
	f.t.Helper()
	deadline := time.Now().Add(wait)
	for {
		f.mu.Lock()
		n := 0
		for _, c := range f.calls {
			if c == "status "+paneID {
				n++
			}
		}
		if n > f.awaited[paneID] {
			f.awaited[paneID]++
			f.mu.Unlock()
			return
		}
		f.mu.Unlock()
		if time.Now().After(deadline) {
			f.t.Fatalf("no status subscribe for %s", paneID)
		}
		time.Sleep(time.Millisecond)
	}
}

// recorder is a Sink that keeps one line per report.
type recorder struct {
	mu          sync.Mutex
	lines       []string
	times       []int64  // observed_at of each line
	epochs      []uint64 // the epoch of each line's key, or of a reconnect
	transitions []core.Transition
	resyncTimes []time.Time
	// gates holds one "gate <gate> <version>" per Gate report. With
	// gateLines set, each is also one of the lines; tests from before the
	// gate leave it unset.
	gates     []string
	gateLines bool
	// profileLines adds each Agent's or Transition's Profile to its line;
	// tests from before Profiles leave it unset. Profile changes are always
	// lines.
	profileLines bool
	notify       chan struct{}
}

func newRecorder() *recorder { return &recorder{notify: make(chan struct{}, 256)} }

func (r *recorder) add(line string, observedAt int64, epoch uint64) {
	r.mu.Lock()
	r.lines = append(r.lines, line)
	r.times = append(r.times, observedAt)
	r.epochs = append(r.epochs, epoch)
	r.mu.Unlock()
	r.notify <- struct{}{}
}

func (r *recorder) withProfile(line, profile string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.profileLines {
		return line + " profile=" + profile
	}
	return line
}

func (r *recorder) Agent(a Agent, observedAt int64) {
	r.add(r.withProfile(fmt.Sprintf("agent %s %s %s", a.Key.PaneID, a.AgentProgram, a.Status), a.Profile), observedAt, a.Key.Epoch)
}

func (r *recorder) Profile(a Agent, previous string, observedAt int64) {
	r.add(fmt.Sprintf("profile %s %s previous %s", a.Key.PaneID, a.Profile, previous), observedAt, a.Key.Epoch)
}

func (r *recorder) Reconnected(version string, epoch uint64, kept bool, observedAt int64) {
	change := "incremented"
	if kept {
		change = "kept"
	}
	r.add(fmt.Sprintf("reconnected %s epoch %d %s", version, epoch, change), observedAt, epoch)
}

func (r *recorder) Resynced(cause string, observedAt int64) {
	r.mu.Lock()
	r.resyncTimes = append(r.resyncTimes, time.Now())
	r.mu.Unlock()
	r.add("resync "+cause, observedAt, 0)
}

func (r *recorder) Gate(gate core.Gate, version string, observedAt int64) {
	line := fmt.Sprintf("gate %s %s", gate, version)
	r.mu.Lock()
	r.gates = append(r.gates, line)
	lines := r.gateLines
	r.mu.Unlock()
	if lines {
		r.add(line, observedAt, 0)
		return
	}
	r.notify <- struct{}{}
}

// gateReports returns every Gate report so far.
func (r *recorder) gateReports() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.gates)
}

// resyncs returns when each resync began, by the wall clock.
func (r *recorder) resyncs() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.resyncTimes)
}

func (r *recorder) lineEpochs() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.epochs)
}

func (r *recorder) Transition(t core.Transition) {
	prev, _ := t.Previous()
	r.mu.Lock()
	r.transitions = append(r.transitions, t)
	r.mu.Unlock()
	line := fmt.Sprintf("transition %s %s %s->%s %s", t.Key().PaneID, t.AgentProgram(), prev, t.New(), t.Provenance())
	r.add(r.withProfile(line, t.Profile()), t.ObservedAt(), t.Key().Epoch)
}

func (r *recorder) Removed(a Agent, observedAt int64) {
	r.add(r.withProfile(fmt.Sprintf("removed %s", a.Key.PaneID), a.Profile), observedAt, a.Key.Epoch)
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lines)
}

func (r *recorder) observedTimes() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.times)
}

func (r *recorder) lastTransition() core.Transition {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transitions[len(r.transitions)-1]
}

// count returns how many lines equal line.
func (r *recorder) count(line string) int {
	n := 0
	for _, l := range r.all() {
		if l == line {
			n++
		}
	}
	return n
}

// awaitCount waits until n lines equal line.
func (r *recorder) awaitCount(t *testing.T, line string, n int) {
	t.Helper()
	deadline := time.After(wait)
	for r.count(line) < n {
		select {
		case <-r.notify:
		case <-deadline:
			t.Fatalf("%d lines %q, want %d; lines\n%s", r.count(line), line, n, strings.Join(r.all(), "\n"))
		}
	}
}

// expect waits until the recorder holds exactly want, in order.
func (r *recorder) expect(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.After(wait)
	for {
		got := r.all()
		if slices.Equal(got, want) {
			return
		}
		if len(got) > len(want) {
			t.Fatalf("lines\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
		}
		select {
		case <-r.notify:
		case <-deadline:
			t.Fatalf("lines\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
		}
	}
}

// clockAt is the test clock's nth reading, in Unix milliseconds.
func clockAt(n int) int64 {
	return time.Date(2026, 10, 16, 12, 0, n, 0, time.UTC).UnixMilli()
}

// testClock returns a clock that advances one second per reading, from a fixed
// start.
func testClock() func() time.Time {
	var n atomic.Int64
	start := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return start.Add(time.Duration(n.Add(1)) * time.Second) }
}

// harness runs an engine against a fake client.
type harness struct {
	t      *testing.T
	fake   *fakeClient
	rec    *recorder
	engine *Engine
	cancel context.CancelFunc
	// done closes when Run returns.
	done chan struct{}
}

func startEngine(t *testing.T, fake *fakeClient) *harness {
	t.Helper()
	return startEngineWith(t, fake, nil)
}

// startEngineWith starts an engine whose Options and recorder opts adjusts
// first; opts may be nil.
func startEngineWith(t *testing.T, fake *fakeClient, opts func(*Options, *recorder)) *harness {
	t.Helper()
	rec := newRecorder()
	o := Options{Host: "mac", Server: "default", Client: fake, Sink: rec, Clock: testClock()}
	if opts != nil {
		opts(&o, rec)
	}
	e := New(o)
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, fake: fake, rec: rec, engine: e, cancel: cancel, done: make(chan struct{})}
	go func() {
		e.Run(ctx)
		close(h.done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(wait):
			t.Error("engine did not stop")
		}
	})
	return h
}

// stop cancels the engine and waits for Run to return.
func (h *harness) stop() {
	h.t.Helper()
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(wait):
		h.t.Fatal("engine did not stop")
	}
}

// running fails the test if Run has returned.
func (h *harness) running() {
	h.t.Helper()
	select {
	case <-h.done:
		h.t.Fatal("engine stopped")
	default:
	}
}

// settle waits until no snapshot read has begun for a while, every one has
// finished, and the engine has handled everything sent to it.
func (h *harness) settle() {
	h.t.Helper()
	for {
		before := h.fake.snapshots.Load()
		time.Sleep(quiet)
		if _, err := h.engine.Agents(context.Background()); err != nil {
			h.t.Fatal(err)
		}
		if h.fake.snapshots.Load() == before {
			h.fake.drainSnapshotCalls()
			return
		}
	}
}

func (h *harness) agents() []Agent {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	as, err := h.engine.Agents(ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return as
}

// lifecycle sends a lifecycle event once the lifecycle subscription exists.
func (h *harness) lifecycle(ev core.LifecycleEvent) {
	h.t.Helper()
	deadline := time.Now().Add(wait)
	for h.fake.lifecycleSub() == nil {
		if time.Now().After(deadline) {
			h.t.Fatal("no lifecycle subscription")
		}
		time.Sleep(time.Millisecond)
	}
	h.fake.lifecycleSub().event(ev)
}

var errBoom = errors.New("boom")

// TestMain removes the snapshot floor and shortens the retry, reconnect and
// resync delays, so tests run fast; tests of any set their own value before starting
// an engine.
func TestMain(m *testing.M) {
	snapshotMinInterval = 0
	snapshotRetryDelay = 10 * time.Millisecond
	reconnectInitialDelay = 5 * time.Millisecond
	reconnectMaxDelay = 50 * time.Millisecond
	resyncMinInterval = 10 * time.Millisecond
	refusedRecheckInterval = 10 * time.Millisecond
	profileTimeout = 200 * time.Millisecond
	os.Exit(m.Run())
}
