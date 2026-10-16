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
// herdr being absent, gone or restarting is normal operation. A failed start
// (ping, the lifecycle subscription or the connection's first snapshot) or any
// health signal on the lifecycle stream puts the engine into reconnecting: it
// closes every status subscription, keeps the tracked Agents and their last
// Status, and runs the start again, forever, waiting reconnectDelay between
// attempts. The first snapshot after a reconnect decides the epoch (AD-3): kept
// when at least one tracked pane is present with the terminal_id it had,
// incremented otherwise.
//
// Ordering (AD-11): at each start, the lifecycle subscription first, then a
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
	// epoch is the server epoch every AgentKey carries (AD-3).
	epoch uint64
	// conn numbers the attempts to start; every message from a connection's
	// lifecycle stream, snapshot reads and timers carries it, so one from
	// before a reconnect is ignored. connCancel ends the current attempt's
	// calls.
	conn       uint64
	connCtx    context.Context
	connCancel context.CancelFunc
	// connected is set once the current attempt's lifecycle subscription is
	// confirmed; ready once its first snapshot is applied, until then a failed
	// read is a failed start. everReady is set by the first start that got so
	// far, after which every ready start is a reconnect.
	connected bool
	ready     bool
	everReady bool
	// failures counts consecutive failed starts and lost streams, for the
	// backoff. A connection that stays up for reconnectStableAfter clears it.
	failures int
	// readyAt is when the current connection became ready, by the wall clock.
	readyAt time.Time
	// terminals is every pane's terminal_id in the latest applied snapshot,
	// Agent or not: the evidence the epoch rule compares across a reconnect.
	terminals map[string]string
	panes     map[string]*pane
	nextGen   uint64
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
	// terminalID is the pane's terminal_id in the latest snapshot that showed
	// it. A snapshot showing the same pane id with another terminal_id shows
	// another Agent (AD-3).
	terminalID string
	// liveDuring is the read that was in flight when the last live status event
	// arrived, or 0. That read may predate the event, so its Status is not
	// compared; the read after it settles the Status.
	liveDuring uint64
}

// Inbox messages.
type (
	// connectMsg is the outcome of one attempt to reach herdr: ping and
	// the lifecycle subscription.
	connectMsg struct {
		conn    uint64
		version string
		sub     Subscription[core.LifecycleEvent]
		err     error
	}
	// connectDueMsg says the wait before the next attempt is over.
	connectDueMsg struct {
		conn uint64
	}
	lifecycleMsg struct {
		conn uint64
		d    herdr.Delivery[core.LifecycleEvent]
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
		conn uint64
		seq  uint64
		snap core.Snapshot
		err  error
	}
	// snapshotDueMsg says a scheduled read may start.
	snapshotDueMsg struct {
		conn uint64
	}
	agentsQuery struct {
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
		epoch:   firstEpoch,
	}
	if e.clock == nil {
		e.clock = time.Now
	}
	if e.log == nil {
		e.log = slog.New(slog.DiscardHandler)
	}
	return e
}

// Run starts the engine and runs it until ctx ends, and returns then. Only ctx
// ending stops it: herdr being absent, gone or restarting never does, it
// reconnects. A snapshot that fails once started is logged and read again
// after snapshotRetryDelay. Run closes every subscription and waits for its
// goroutines before it returns. Call it once.
func (e *Engine) Run(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	e.runCtx = runCtx
	defer func() {
		e.closeAll()
		cancel()
		e.wg.Wait()
		close(e.stopped)
	}()

	e.connect()
	e.loop(ctx)
}

// connect starts one attempt to reach herdr, in a goroutine of its own: ping
// for herdr's version, then the lifecycle subscription. Its outcome arrives as
// a connectMsg; the first snapshot follows only once it has succeeded.
func (e *Engine) connect() {
	e.conn++
	conn := e.conn
	ctx, cancel := context.WithCancel(e.runCtx)
	e.connCtx, e.connCancel = ctx, cancel
	e.log.Debug("reaching herdr", "attempt", e.failures+1)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		m := connectMsg{conn: conn}
		pctx, pcancel := context.WithTimeout(ctx, pingTimeout)
		m.version, m.err = e.client.Ping(pctx)
		pcancel()
		if m.err != nil {
			m.err = fmt.Errorf("engine: ping herdr: %w", m.err)
		} else {
			sctx, scancel := context.WithTimeout(ctx, subscribeTimeout)
			m.sub, m.err = e.client.SubscribeLifecycle(sctx)
			scancel()
			if m.err != nil {
				m.sub = nil
				m.err = fmt.Errorf("engine: subscribe to lifecycle events: %w", m.err)
			}
		}
		if !e.send(m) && m.sub != nil {
			m.sub.Close()
		}
	}()
}

func (e *Engine) onConnect(m connectMsg) {
	if m.conn != e.conn || e.connected {
		if m.sub != nil {
			m.sub.Close()
		}
		return
	}
	if m.err != nil {
		e.retry(m.err)
		return
	}
	e.connected = true
	e.herdrVersion = m.version
	e.lifecycle = m.sub
	e.log.Info("herdr reached", "herdr_version", m.version)
	conn, sub := m.conn, m.sub
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for d := range sub.Deliveries() {
			if !e.send(lifecycleMsg{conn: conn, d: d}) {
				return
			}
		}
	}()
	e.requestSnapshot()
}

// retry abandons a failed start and waits before the next, longer after each
// consecutive failure up to the cap.
func (e *Engine) retry(cause error) {
	e.failures++
	e.disconnect()
	wait := reconnectDelay(e.failures)
	e.log.Info("herdr not reachable; waiting before the next attempt", "attempt", e.failures, "cause", cause.Error(), "retry_in", wait.String())
	e.awaitConnect(wait)
}

// lost handles a health signal on the lifecycle stream once started: the
// engine can no longer trust it has seen every event, so it starts again. A
// loss counts into the backoff like a failed start, unless the connection had
// stayed up for reconnectStableAfter, so a herdr that drops right after each
// start is not hammered.
func (e *Engine) lost(h error) {
	if time.Since(e.readyAt) >= reconnectStableAfter {
		e.failures = 0
	}
	e.failures++
	e.disconnect()
	wait := reconnectDelay(e.failures)
	e.log.Info("lifecycle stream lost; reconnecting", "cause", h.Error(), "retry_in", wait.String())
	e.awaitConnect(wait)
}

// awaitConnect starts the next attempt after delay.
func (e *Engine) awaitConnect(delay time.Duration) {
	conn := e.conn
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
			e.send(connectDueMsg{conn: conn})
		case <-e.runCtx.Done():
		}
	}()
}

// disconnect puts the engine into reconnecting: it ends the current
// connection's calls and closes every subscription, but keeps every reported
// Agent and its last Status for the epoch rule to judge. Panes detected by an
// event but never reported are dropped; a later snapshot finds them again.
func (e *Engine) disconnect() {
	if e.connCancel != nil {
		e.connCancel()
	}
	if e.lifecycle != nil {
		e.lifecycle.Close()
		e.lifecycle = nil
	}
	e.connected = false
	e.ready = false
	for id, p := range e.panes {
		if p.sub != nil {
			p.sub.Close()
		}
		if !p.known {
			delete(e.panes, id)
			continue
		}
		p.sub = nil
		p.subscribing = false
		p.gen = 0 // no message carries generation 0, so every old one is ignored
		p.liveDuring = 0
	}
	e.inFlight = false
	e.again = false
	e.removedDuring = nil
	e.scheduled = false
}

func (e *Engine) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-e.inbox:
			e.handle(m)
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

func (e *Engine) handle(m any) {
	switch m := m.(type) {
	case connectMsg:
		e.onConnect(m)
	case connectDueMsg:
		if m.conn == e.conn && !e.connected {
			e.connect()
		}
	case lifecycleMsg:
		if m.conn == e.conn && e.connected {
			e.onLifecycle(m.d)
		}
	case statusMsg:
		e.onStatus(m)
	case subscribedMsg:
		e.onSubscribed(m)
	case snapshotMsg:
		if m.conn == e.conn && e.connected {
			e.onSnapshot(m)
		}
	case snapshotDueMsg:
		if m.conn == e.conn && e.connected && e.scheduled {
			e.scheduled = false
			e.startSnapshot()
		}
	case agentsQuery:
		m.reply <- e.agents()
	}
}

func (e *Engine) onLifecycle(d herdr.Delivery[core.LifecycleEvent]) {
	if d.Health != nil {
		err := fmt.Errorf("engine: lifecycle stream lost: %w", d.Health)
		if !e.ready {
			e.retry(err) // still starting: a failed start
		} else {
			e.lost(err)
		}
		return
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
			return
		}
		// Subscribe first; the snapshot that reads the pane follows the
		// subscription's confirmation.
		p := e.track(ev.PaneID)
		p.removableFrom = math.MaxUint64
		e.subscribe(p)
	case core.LifecycleMoved, core.LifecycleContextChanged:
		e.requestSnapshot()
	}
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

func (e *Engine) onSnapshot(m snapshotMsg) {
	e.inFlight = false
	if m.err != nil {
		if !e.ready {
			e.retry(fmt.Errorf("engine: snapshot: %w", m.err))
			return
		}
		e.log.Warn("snapshot failed; reading again", "error", m.err, "retry_in", snapshotRetryDelay.String())
		e.removedDuring = nil
		e.again = false
		e.schedule(snapshotRetryDelay)
		return
	}
	if !e.ready {
		e.ready = true
		e.readyAt = time.Now()
		if e.everReady {
			e.decideEpoch(m.snap)
		}
		e.everReady = true
	}
	e.apply(m.seq, m.snap)
	e.removedDuring = nil
	if e.again {
		e.again = false
		e.requestSnapshot()
	}
}

// decideEpoch applies the epoch rule (AD-3) to the first snapshot after a
// reconnect, before that snapshot is applied. The same herdr instance is shown
// by at least one pane, Agent or not, present with the non-empty terminal_id
// the last applied snapshot showed for it; then the
// epoch is kept, and applying the snapshot removes only the panes that closed
// meanwhile. Otherwise every tracked Agent is removed under the old epoch and
// the epoch increments, so the snapshot's Agents are new ones. A pane-id match
// alone never keeps the epoch.
func (e *Engine) decideEpoch(s core.Snapshot) {
	kept := false
	for _, obs := range s.Panes {
		if obs.TerminalID != "" && e.terminals[obs.PaneID] == obs.TerminalID {
			kept = true
			break
		}
	}
	next := e.epoch
	if !kept {
		next++
	}
	e.log.Info("reconnected", "herdr_version", e.herdrVersion, "epoch", next, "epoch_kept", kept)
	e.sink.Reconnected(e.herdrVersion, next, kept, e.now())
	if kept {
		return
	}
	ids := make([]string, 0, len(e.panes))
	for id := range e.panes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e.remove(id, "herdr restarted")
	}
	e.epoch = next
}

// apply makes the engine agree with snapshot seq: the snapshot is the truth for
// which Agents exist and their Status.
func (e *Engine) apply(seq uint64, s core.Snapshot) {
	e.terminals = make(map[string]string, len(s.Panes))
	for _, obs := range s.Panes {
		e.terminals[obs.PaneID] = obs.TerminalID
	}
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
		if tracked && p.known && p.terminalID != "" && obs.TerminalID != p.terminalID {
			// The same pane id on another terminal is another Agent: never
			// join the two.
			e.remove(obs.PaneID, "terminal replaced")
			tracked = false
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
		p.terminalID = obs.TerminalID
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
	conn := e.conn
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
			e.send(snapshotDueMsg{conn: conn})
		case <-e.runCtx.Done():
		}
	}()
}

func (e *Engine) startSnapshot() {
	e.lastStart = time.Now()
	e.snapSeq++
	e.inFlight = true
	e.removedDuring = map[string]bool{}
	seq, conn, connCtx := e.snapSeq, e.conn, e.connCtx
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ctx, cancel := context.WithTimeout(connCtx, snapshotTimeout)
		defer cancel()
		snap, err := e.client.Snapshot(ctx)
		e.send(snapshotMsg{conn: conn, seq: seq, snap: snap, err: err})
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
	id, gen, connCtx := p.id, p.gen, e.connCtx
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ctx, cancel := context.WithTimeout(connCtx, subscribeTimeout)
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
	return core.AgentKey{Host: e.host, Server: e.server, Epoch: e.epoch, PaneID: paneID}
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
