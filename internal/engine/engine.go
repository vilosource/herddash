package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
)

// Options configures an Engine.
type Options struct {
	// Host and Server are this Daemon's half of every AgentKey.
	Host   string
	Server string
	Client Client
	Sink   Sink
	// Clock stamps observed_at. It defaults to time.Now.
	Clock func() time.Time
	// Logger defaults to discarding everything.
	Logger *slog.Logger
}

// Engine is the single-writer actor (AD-2). Only its Run goroutine holds the
// pane set, each Agent's Status and its tracked context. Subscription readers
// and herdr calls run in goroutines of their own and reach it only through its
// inbox, each message tagged so that one from a closed or superseded
// subscription is ignored.
//
// Ordering (AD-11): at start, the lifecycle subscription first, then a
// snapshot, then one status subscription per Agent pane. Every status
// subscription, once herdr confirms it, is followed by a snapshot that reads
// the pane's state, so nothing that changed between the snapshot and the
// subscription is missed. At most one snapshot read is in flight; anything that
// arrives while one is causes exactly one more read after it, and reads start
// at least snapshotMinInterval apart. Buffered events are never replayed onto a
// snapshot.
type Engine struct {
	host, server string
	client       Client
	sink         Sink
	clock        func() time.Time
	log          *slog.Logger

	inbox chan any
	// stopped closes when Run returns.
	stopped chan struct{}
	// runCtx and wg are set by Run before any goroutine starts.
	runCtx context.Context
	wg     sync.WaitGroup

	// Everything below belongs to the Run goroutine alone.
	herdrVersion string
	lifecycle    Subscription[core.LifecycleEvent]
	panes        map[string]*pane
	nextGen      uint64
	// snapSeq numbers snapshot reads; it is the number of the latest one started.
	snapSeq  uint64
	inFlight bool
	// again records that something arrived during the read in flight, so one
	// more read follows it.
	again bool
	// removedDuring holds the panes removed by an event during the read in
	// flight, which that read must not bring back.
	removedDuring map[string]bool
	// lastStart is when the latest read started, by the wall clock; scheduled
	// is set while a read waits for its time to start.
	lastStart time.Time
	scheduled bool
	// started is set once the first snapshot has been applied; until then a
	// failed read is a failed start.
	started bool
}

// pane is one tracked Agent pane.
type pane struct {
	id string
	// gen tags this pane's current subscription attempt and every message from it.
	gen uint64
	// sub is nil while subscribing and after a subscription failed or ended.
	sub         Subscription[core.StatusEvent]
	subscribing bool
	// removableFrom is the first snapshot read allowed to remove the pane for
	// being absent. A pane detected by an event is neither removable nor
	// reported by a read until one that started after its status subscription
	// was confirmed.
	removableFrom uint64
	// known is set once the Agent has been reported to the Sink; until then the
	// pane is awaiting the snapshot that reads it.
	known   bool
	program core.AgentProgram
	status  core.Status
	context core.PaneContext
	// liveDuring is the read that was in flight when the last live status event
	// arrived, or 0. That read may predate the event, so its Status is not
	// compared; the read after it settles the Status.
	liveDuring uint64
}

// Inbox messages.
type (
	lifecycleMsg struct {
		d herdr.Delivery[core.LifecycleEvent]
	}
	statusMsg struct {
		paneID string
		gen    uint64
		d      herdr.Delivery[core.StatusEvent]
	}
	subscribedMsg struct {
		paneID string
		gen    uint64
		sub    Subscription[core.StatusEvent]
		err    error
	}
	snapshotMsg struct {
		seq  uint64
		snap core.Snapshot
		err  error
	}
	// snapshotDueMsg says a scheduled read may start.
	snapshotDueMsg struct{}
	agentsQuery    struct {
		reply chan []Agent
	}
)

// New returns an Engine. Run starts it.
func New(o Options) *Engine {
	e := &Engine{
		host:    o.Host,
		server:  o.Server,
		client:  o.Client,
		sink:    o.Sink,
		clock:   o.Clock,
		log:     o.Logger,
		inbox:   make(chan any),
		stopped: make(chan struct{}),
		panes:   map[string]*pane{},
	}
	if e.clock == nil {
		e.clock = time.Now
	}
	if e.log == nil {
		e.log = slog.New(slog.DiscardHandler)
	}
	return e
}

// Run starts the engine and runs it until ctx ends, which returns nil, or
// until it cannot go on: a failed start (ping, the lifecycle subscription or the
// first snapshot) or a health signal on the lifecycle stream. A snapshot that
// fails later is logged and read again after snapshotRetryDelay. Reconnecting is not this engine's job yet. Run closes every
// subscription and waits for its goroutines before it returns. Call it once.
func (e *Engine) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	e.runCtx = runCtx
	defer func() {
		e.closeAll()
		cancel()
		e.wg.Wait()
		close(e.stopped)
	}()

	err := e.start(runCtx)
	if err == nil {
		err = e.loop(ctx)
	}
	if err != nil && ctx.Err() != nil {
		// Stopped while starting or running: that is a clean stop.
		return nil
	}
	return err
}

// start runs before the loop, when the engine holds no state yet: it reads
// herdr's version, subscribes to lifecycle events, and only then starts the
// first snapshot.
func (e *Engine) start(ctx context.Context) error {
	pctx, cancel := context.WithTimeout(ctx, pingTimeout)
	version, err := e.client.Ping(pctx)
	cancel()
	if err != nil {
		return fmt.Errorf("engine: ping herdr: %w", err)
	}
	e.herdrVersion = version
	e.log.Info("herdr reached", "herdr_version", version)

	sctx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	sub, err := e.client.SubscribeLifecycle(sctx)
	cancel()
	if err != nil {
		return fmt.Errorf("engine: subscribe to lifecycle events: %w", err)
	}
	e.lifecycle = sub
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for d := range sub.Deliveries() {
			if !e.send(lifecycleMsg{d: d}) {
				return
			}
		}
	}()

	e.requestSnapshot()
	return nil
}

func (e *Engine) loop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-e.inbox:
			if err := e.handle(m); err != nil {
				return err
			}
		}
	}
}

// send delivers m to the engine, or reports false once the engine has stopped.
func (e *Engine) send(m any) bool {
	select {
	case e.inbox <- m:
		return true
	case <-e.runCtx.Done():
		return false
	}
}

// ErrStopped is returned by Agents once Run has returned.
var ErrStopped = errors.New("engine: stopped")

// Agents returns every Agent the engine currently holds, by pane id. It asks
// the engine goroutine by message, so it fails only when ctx ends or the engine
// has stopped.
func (e *Engine) Agents(ctx context.Context) ([]Agent, error) {
	q := agentsQuery{reply: make(chan []Agent, 1)}
	select {
	case e.inbox <- q:
	case <-e.stopped:
		return nil, ErrStopped
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case as := <-q.reply:
		return as, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *Engine) handle(m any) error {
	switch m := m.(type) {
	case lifecycleMsg:
		return e.onLifecycle(m.d)
	case statusMsg:
		e.onStatus(m)
	case subscribedMsg:
		e.onSubscribed(m)
	case snapshotMsg:
		return e.onSnapshot(m)
	case snapshotDueMsg:
		e.scheduled = false
		e.startSnapshot()
	case agentsQuery:
		m.reply <- e.agents()
	}
	return nil
}

func (e *Engine) onLifecycle(d herdr.Delivery[core.LifecycleEvent]) error {
	if d.Health != nil {
		return fmt.Errorf("engine: lifecycle stream lost: %w", d.Health)
	}
	e.noteEvent()
	ev := d.Event
	switch ev.Kind {
	case core.LifecycleAgentReleased:
		// The release carries the Agent's last Status: record it first.
		if p, ok := e.panes[ev.PaneID]; ok && p.known && ev.FinalStatus.Valid() && ev.FinalStatus != p.status {
			e.transition(p, ev.FinalStatus, core.ProvenanceLive, nil)
		}
		e.remove(ev.PaneID, string(ev.Kind))
	case core.LifecycleClosed, core.LifecycleExited:
		e.remove(ev.PaneID, string(ev.Kind))
	case core.LifecycleAgentDetected:
		if _, tracked := e.panes[ev.PaneID]; tracked {
			// Perhaps another program in the same pane: read it again.
			e.requestSnapshot()
			return nil
		}
		// Subscribe first; the snapshot that reads the pane follows the
		// subscription's confirmation.
		p := e.track(ev.PaneID)
		p.removableFrom = math.MaxUint64
		e.subscribe(p)
	case core.LifecycleMoved, core.LifecycleContextChanged:
		e.requestSnapshot()
	}
	return nil
}

func (e *Engine) onStatus(m statusMsg) {
	p, ok := e.panes[m.paneID]
	if !ok || p.gen != m.gen {
		return // from a subscription already closed or superseded
	}
	e.noteEvent()
	if h := m.d.Health; h != nil {
		e.log.Warn("status subscription ended", "pane", p.id, "error", h)
		if p.sub != nil {
			p.sub.Close()
			p.sub = nil
		}
		e.requestSnapshot() // which subscribes again if the Agent is still there
		return
	}
	ev := m.d.Event
	if e.inFlight {
		p.liveDuring = e.snapSeq
	}
	if ev.AgentProgram != "" {
		p.program = ev.AgentProgram
	}
	if !p.known {
		p.status = ev.Status
		e.report(p)
		return
	}
	if ev.Status == p.status {
		return
	}
	e.transition(p, ev.Status, core.ProvenanceLive, nil)
}

func (e *Engine) onSubscribed(m subscribedMsg) {
	p, ok := e.panes[m.paneID]
	if !ok || p.gen != m.gen {
		if m.sub != nil {
			m.sub.Close()
		}
		return
	}
	p.subscribing = false
	if m.err != nil {
		// Only this pane fails; perhaps it closed since it was named. Read
		// again: the snapshot decides whether it is still there.
		e.log.Warn("status subscribe failed", "pane", p.id, "error", m.err)
		if !p.known {
			delete(e.panes, p.id)
			if e.inFlight {
				e.removedDuring[p.id] = true // the read in flight may predate this
			}
		}
		e.requestSnapshot()
		return
	}
	p.sub = m.sub
	if next := e.snapSeq + 1; p.removableFrom > next {
		p.removableFrom = next
	}
	id, gen, sub := p.id, p.gen, m.sub
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for d := range sub.Deliveries() {
			if !e.send(statusMsg{paneID: id, gen: gen, d: d}) {
				return
			}
		}
	}()
	e.requestSnapshot()
}

func (e *Engine) onSnapshot(m snapshotMsg) error {
	e.inFlight = false
	if m.err != nil {
		if !e.started {
			return fmt.Errorf("engine: snapshot: %w", m.err)
		}
		e.log.Warn("snapshot failed; reading again", "error", m.err, "retry_in", snapshotRetryDelay.String())
		e.removedDuring = nil
		e.again = false
		e.schedule(snapshotRetryDelay)
		return nil
	}
	e.started = true
	e.apply(m.seq, m.snap)
	e.removedDuring = nil
	if e.again {
		e.again = false
		e.requestSnapshot()
	}
	return nil
}

// apply makes the engine agree with snapshot seq: the snapshot is the truth for
// which Agents exist and their Status.
func (e *Engine) apply(seq uint64, s core.Snapshot) {
	present := map[string]bool{}
	for _, obs := range s.Panes {
		a := obs.Agent
		if a == nil {
			continue
		}
		present[obs.PaneID] = true
		p, tracked := e.panes[obs.PaneID]
		if tracked && !p.known && seq < p.removableFrom {
			continue // detected by an event; this read may predate its subscription
		}
		if !tracked {
			if e.removedDuring[obs.PaneID] {
				continue // removed while this read ran; the next read decides
			}
			p = e.track(obs.PaneID)
			p.removableFrom = seq + 1
		}
		if p.context.PaneTitle != obs.Context.PaneTitle {
			e.log.Debug("pane title", "pane", p.id, "pane_title", obs.Context.PaneTitle)
		}
		p.context = obs.Context
		if a.AgentProgram != "" {
			p.program = a.AgentProgram
		}
		switch {
		case !p.known:
			p.status = a.Status
			e.report(p)
		case p.liveDuring == seq:
			// A live event arrived while this read ran, and the read may
			// predate it. The read that follows settles the Status.
		case a.Status != p.status:
			e.transition(p, a.Status, core.ProvenanceSnapshot, a.StateChangeSeq)
		}
		if p.sub == nil && !p.subscribing {
			e.subscribe(p)
		}
	}
	var gone []string
	for id, p := range e.panes {
		if !present[id] && seq >= p.removableFrom {
			gone = append(gone, id)
		}
	}
	sort.Strings(gone)
	for _, id := range gone {
		e.remove(id, "absent from snapshot")
	}
}

// noteEvent records that an event arrived; one during a read causes one more.
func (e *Engine) noteEvent() {
	if e.inFlight {
		e.again = true
	}
}

// requestSnapshot asks for a read: now, or when the minimum interval since the
// last read's start has elapsed, or after the read in flight. Requests coalesce.
func (e *Engine) requestSnapshot() {
	switch {
	case e.inFlight:
		e.again = true
	case e.scheduled:
	default:
		if wait := snapshotMinInterval - time.Since(e.lastStart); wait > 0 {
			e.schedule(wait)
			return
		}
		e.startSnapshot()
	}
}

// schedule starts a read after delay, unless one is already scheduled.
func (e *Engine) schedule(delay time.Duration) {
	if e.scheduled {
		return
	}
	e.scheduled = true
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
			e.send(snapshotDueMsg{})
		case <-e.runCtx.Done():
		}
	}()
}

func (e *Engine) startSnapshot() {
	e.lastStart = time.Now()
	e.snapSeq++
	e.inFlight = true
	e.removedDuring = map[string]bool{}
	seq := e.snapSeq
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ctx, cancel := context.WithTimeout(e.runCtx, snapshotTimeout)
		defer cancel()
		snap, err := e.client.Snapshot(ctx)
		e.send(snapshotMsg{seq: seq, snap: snap, err: err})
	}()
}

// track starts tracking a pane, not yet reported and not yet subscribed.
func (e *Engine) track(id string) *pane {
	p := &pane{id: id}
	e.panes[id] = p
	return p
}

// subscribe starts a status subscription attempt for p, tagged with a new
// generation.
func (e *Engine) subscribe(p *pane) {
	e.nextGen++
	p.gen = e.nextGen
	p.subscribing = true
	p.sub = nil
	id, gen := p.id, p.gen
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ctx, cancel := context.WithTimeout(e.runCtx, subscribeTimeout)
		defer cancel()
		sub, err := e.client.SubscribeStatus(ctx, id)
		if err != nil {
			sub = nil
		}
		if !e.send(subscribedMsg{paneID: id, gen: gen, sub: sub, err: err}) && sub != nil {
			sub.Close()
		}
	}()
}

// remove stops tracking a pane, closes its subscription and reports it gone.
func (e *Engine) remove(id, reason string) {
	p, ok := e.panes[id]
	if !ok {
		return
	}
	delete(e.panes, id)
	if p.sub != nil {
		p.sub.Close()
	}
	if e.inFlight {
		e.removedDuring[id] = true
	}
	if p.known {
		e.log.Info("agent removed", "pane", id, "agent_program", p.program, "reason", reason)
		e.sink.Removed(e.agent(p), e.now())
	}
}

// report reports a pane's Agent for the first time.
func (e *Engine) report(p *pane) {
	p.known = true
	e.log.Info("agent seen", "pane", p.id, "agent_program", p.program, "status", p.status)
	e.sink.Agent(e.agent(p), e.now())
}

func (e *Engine) transition(p *pane, next core.Status, prov core.Provenance, order *uint64) {
	in := core.TransitionInput{
		Key:          e.key(p.id),
		AgentProgram: p.program,
		New:          next,
		HerdrOrder:   order,
		HerdrVersion: e.herdrVersion,
		ObservedAt:   e.now(),
		Provenance:   prov,
	}
	if p.status.Valid() {
		prev := p.status
		in.Previous = &prev
	}
	t, err := core.NewTransition(in)
	if err != nil {
		e.log.Warn("transition not recorded", "pane", p.id, "error", err)
		return
	}
	p.status = next
	e.sink.Transition(t)
}

func (e *Engine) key(paneID string) core.AgentKey {
	return core.AgentKey{Host: e.host, Server: e.server, Epoch: liveEpoch, PaneID: paneID}
}

func (e *Engine) agent(p *pane) Agent {
	return Agent{Key: e.key(p.id), AgentProgram: p.program, Status: p.status, Context: p.context}
}

func (e *Engine) agents() []Agent {
	var out []Agent
	for _, p := range e.panes {
		if p.known {
			out = append(out, e.agent(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key.PaneID < out[j].Key.PaneID })
	return out
}

func (e *Engine) now() int64 { return e.clock().UnixMilli() }

// closeAll closes every subscription the engine holds.
func (e *Engine) closeAll() {
	if e.lifecycle != nil {
		e.lifecycle.Close()
	}
	for _, p := range e.panes {
		if p.sub != nil {
			p.sub.Close()
		}
	}
}
