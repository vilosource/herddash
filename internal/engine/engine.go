package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
	"github.com/vilosource/herddash/internal/procenv"
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
	// AllowUnsupportedHerdr is FR-16's dogfooding override: a herdr older than
	// core.HerdrMinimum is observed, with a warning (advisory), instead of
	// refused.
	AllowUnsupportedHerdr bool
	// Profiles is the Profile Map: the Profile rule for each Agent Program
	// that has one (FR-10). A program with none is its own Profile.
	Profiles map[core.AgentProgram]core.ProfileRule
	// Environ reads one variable from a process's environment, as
	// procenv.Lookup does, which is the default. Tests inject a fake.
	Environ EnvironFunc
}

// EnvironFunc is the type of Options.Environ.
type EnvironFunc = func(pid int, name string) (value string, set bool, err error)

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
//
// Resync (AD-11): a health signal on a status subscription, a failed status
// subscribe, or a snapshot showing a tracked Agent with no live or pending
// subscription means the engine can no longer trust it saw every event. It
// resyncs: it reports Resynced to the Sink, subscribes again every tracked pane
// without a subscription, and once those attempts have settled reads a
// snapshot that started after them; applying that snapshot ends the resync.
// One resync runs at a time, a trigger during one causes one more after it, and
// resyncs start at least resyncMinInterval apart. apply subscribes only the
// panes it starts tracking (and every pane after a reconnect), so a pane herdr
// keeps rejecting is subscribed again only by a resync, once per interval.
// While connected and ready, a snapshot is also read every reconcileInterval.
//
// Version gate (FR-16, AD-11): every start decides the gate on the version
// ping returns, before the lifecycle subscription. A refused herdr is observed
// by nothing: no subscription, no snapshot, every tracked Agent removed. The
// engine pings it again every refusedRecheckInterval, forever, outside the
// reconnect backoff. An advisory herdr is observed as a supported one is, with
// a warning at each start.
//
// Profile (FR-10, FR-11, AD-14): an Agent is reported first only once its
// Profile is resolved. For an Agent Program with a rule, its pane's foreground
// process is read from herdr and that process's environment through Environ,
// in a goroutine bounded by profileTimeout; a Status change meanwhile updates
// the Status silently, so the first report carries the latest one. Any
// failure gives core.ProfileUnresolved. A program with no rule is its own
// Profile, read from nothing. The Profile is resolved again when herdr detects
// an agent in a tracked pane, or when an Agent's Agent Program changes; a
// different result is reported to the Sink as a Profile change.
type Engine struct {
	host, server string
	client       Client
	sink         Sink
	clock        func() time.Time
	log          *slog.Logger
	// allowUnsupported is Options.AllowUnsupportedHerdr; read-only after New.
	allowUnsupported bool
	// profiles and environ are Options.Profiles and Options.Environ;
	// read-only after New.
	profiles map[core.AgentProgram]core.ProfileRule
	environ  EnvironFunc

	inbox chan any
	// stopped closes when Run returns.
	stopped chan struct{}
	// runCtx and wg are set by Run before any goroutine starts.
	runCtx context.Context
	wg     sync.WaitGroup

	// Everything below belongs to the Run goroutine alone.
	herdrVersion string
	// gateReported is set once a gate outcome has been reported; gate and
	// gateVersion are the last one, so an unchanged repeat is not reported.
	gateReported bool
	gate         core.Gate
	gateVersion  string
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

	// resyncing is set from a resync's start until the snapshot that ends it
	// is applied. resyncPending holds the panes whose resubscribe it still
	// waits for; resyncReadFrom is the first read that may end it, 0 while any
	// is pending.
	resyncing      bool
	resyncPending  map[string]bool
	resyncReadFrom uint64
	// resyncAgain records a trigger during the resync's read, so one more
	// resync follows it; resyncScheduled is set while a resync waits for the
	// minimum interval. resyncNext is the cause either one will report.
	resyncAgain     bool
	resyncScheduled bool
	resyncNext      string
	// resyncLastStart is when the latest resync began, by the wall clock.
	resyncLastStart time.Time
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
	// subFailed is set when a subscribe attempt made outside a resync failed.
	// The resync that failure triggers skips the pane once, clearing it, so
	// the pane is subscribed again only by the resync after that.
	subFailed bool
	// cause is why the pane last lost or failed its subscription: the cause a
	// resync for it reports.
	cause string
	// profile is the Agent's Profile, valid once resolved is set. resolving
	// tags the Profile resolution in flight, 0 when there is none; a result
	// carrying any other tag is stale and ignored.
	profile   string
	resolved  bool
	resolving uint64
}

// Inbox messages.
type (
	// connectMsg is the outcome of one attempt to reach herdr: ping, the
	// version gate and, unless refused, the lifecycle subscription.
	connectMsg struct {
		conn    uint64
		version string
		gate    core.Gate
		// resyncGuaranteed is false below 0.9.0 or for an unparsable version.
		resyncGuaranteed bool
		sub              Subscription[core.LifecycleEvent]
		err              error
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
	// resyncDueMsg says a scheduled resync may start.
	resyncDueMsg struct {
		conn uint64
	}
	// reconcileDueMsg says reconcileInterval has elapsed again.
	reconcileDueMsg struct {
		conn uint64
	}
	agentsQuery struct {
		reply chan []Agent
	}
	// profileMsg is the outcome of one Profile resolution for a pane. matched
	// is the variable's value when it matched a Profile Map entry, the only
	// environment value ever logged; err is why resolution failed.
	profileMsg struct {
		paneID  string
		gen     uint64
		profile string
		matched string
		err     error
	}
)

// New returns an Engine. Run starts it.
func New(o Options) *Engine {
	e := &Engine{
		host:   o.Host,
		server: o.Server,
		client: o.Client,
		sink:   o.Sink,
		clock:  o.Clock,
		log:    o.Logger,

		allowUnsupported: o.AllowUnsupportedHerdr,
		profiles:         maps.Clone(o.Profiles),
		environ:          o.Environ,
		inbox:            make(chan any),
		stopped:          make(chan struct{}),
		panes:            map[string]*pane{},
		epoch:            firstEpoch,
	}
	if e.clock == nil {
		e.clock = time.Now
	}
	if e.log == nil {
		e.log = slog.New(slog.DiscardHandler)
	}
	if e.environ == nil {
		e.environ = procenv.Lookup
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
// for herdr's version, the version gate on it, then, unless the gate refused,
// the lifecycle subscription. Its outcome arrives as a connectMsg; the first
// snapshot follows only once it has succeeded.
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
		} else if m.gate, m.resyncGuaranteed = core.DecideGate(m.version, e.allowUnsupported); m.gate != core.GateRefused {
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
	changed := e.reportGate(m.gate, m.version)
	e.herdrVersion = m.version
	switch m.gate {
	case core.GateRefused:
		e.refused(changed)
		return
	case core.GateAdvisory:
		msg := "herdr's version is below the supported minimum or could not be read; observing it because allow_unsupported_herdr is set"
		if !m.resyncGuaranteed {
			msg += "; the resync guarantee does not hold below " + core.HerdrResyncMinimum
		}
		e.log.Warn(msg, "herdr_version", m.version, "herdr_minimum", core.HerdrMinimum, "resync_guaranteed", m.resyncGuaranteed)
	}
	e.connected = true
	e.lifecycle = m.sub
	e.log.Info("herdr reached", "herdr_version", m.version, "gate", m.gate)
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

// reportGate reports a connect's gate outcome to the Sink when it or the herdr
// version differs from the last one reported, or none has been, and says
// whether it did.
func (e *Engine) reportGate(gate core.Gate, version string) bool {
	if e.gateReported && gate == e.gate && version == e.gateVersion {
		return false
	}
	e.gateReported, e.gate, e.gateVersion = true, gate, version
	e.sink.Gate(gate, version, e.now())
	return true
}

// refused handles a start the version gate refused: nothing of this herdr is
// observed, every tracked Agent is removed, and herdr is pinged again after
// refusedRecheckInterval. A refused herdr was reachable, so it clears the
// reconnect backoff rather than counting into it.
// changed is whether this outcome was just reported, so it is logged as a
// warning once and at debug on each recheck.
func (e *Engine) refused(changed bool) {
	e.failures = 0
	e.disconnect()
	ids := make([]string, 0, len(e.panes))
	for id := range e.panes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e.remove(id, "herdr refused")
	}
	if changed {
		e.log.Warn("herdr's version is below the supported minimum or could not be read; refusing to observe it",
			"herdr_version", e.herdrVersion, "herdr_minimum", core.HerdrMinimum, "recheck_in", refusedRecheckInterval.String())
	} else {
		e.log.Debug("herdr still refused", "herdr_version", e.herdrVersion, "herdr_minimum", core.HerdrMinimum)
	}
	e.awaitConnect(refusedRecheckInterval)
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
	// A reconnect resubscribes, reads and replaces state itself: any resync in
	// progress or scheduled is abandoned.
	e.resyncing = false
	e.resyncPending = nil
	e.resyncReadFrom = 0
	e.resyncAgain = false
	e.resyncScheduled = false
	e.resyncNext = ""
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
	case resyncDueMsg:
		if m.conn == e.conn && e.connected && e.ready && e.resyncScheduled {
			e.resyncScheduled = false
			e.startResync(e.resyncNext)
		}
	case reconcileDueMsg:
		if m.conn == e.conn && e.connected && e.ready {
			e.log.Debug("reconcile")
			e.requestSnapshot()
		}
	case agentsQuery:
		m.reply <- e.agents()
	case profileMsg:
		e.onProfile(m)
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
		e.resyncSettle()
	case core.LifecycleClosed, core.LifecycleExited:
		e.remove(ev.PaneID, string(ev.Kind))
		e.resyncSettle()
	case core.LifecycleAgentDetected:
		if p, tracked := e.panes[ev.PaneID]; tracked {
			// Perhaps another program in the same pane, or the same one
			// started again under another Profile: resolve it again, and read
			// the pane again.
			if !e.setProgram(p, ev.AgentProgram) && p.known {
				e.resolve(p)
			}
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
		p.cause = string(h.Kind)
		e.requestResync(p.cause) // which subscribes again and reads
		return
	}
	ev := m.d.Event
	if e.inFlight {
		p.liveDuring = e.snapSeq
	}
	if !p.known {
		// The Status first, so a report the program change causes carries it.
		p.status = ev.Status
		e.setProgram(p, ev.AgentProgram)
		if !p.known {
			e.report(p)
		}
		return
	}
	e.setProgram(p, ev.AgentProgram)
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
	pending := e.resyncPending[p.id]
	delete(e.resyncPending, p.id)
	if m.err != nil {
		// Only this pane fails; perhaps it closed since it was named. A
		// resync's snapshot decides whether it is still there.
		e.log.Warn("status subscribe failed", "pane", p.id, "error", m.err)
		p.subFailed = !pending // a resync's own failure is not skipped again
		p.cause = subscribeCause(m.err)
		if !p.known {
			delete(e.panes, p.id)
			if e.inFlight {
				e.removedDuring[p.id] = true // the read in flight may predate this
			}
		}
		if pending {
			// Part of a resync already: its snapshot follows.
			e.resyncSettle()
		} else {
			e.requestResync(p.cause)
		}
		return
	}
	p.subFailed = false
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
	if pending {
		e.resyncSettle() // the resync's read is the one that follows
	} else {
		e.requestSnapshot()
	}
}

// subscribeCause names a failed status subscribe as a resync cause: the
// health signal's kind, or closed when the attempt failed before herdr
// answered (a timeout).
func subscribeCause(err error) string {
	var h *herdr.Health
	if errors.As(err, &h) {
		return string(h.Kind)
	}
	return string(herdr.HealthClosed)
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
	first := !e.ready
	if first {
		e.ready = true
		e.readyAt = time.Now()
		if e.everReady {
			e.decideEpoch(m.snap)
		}
		e.everReady = true
		e.startReconcile()
	}
	// The read that ends a resync is the first one started after its
	// subscribe attempts settled.
	ending := e.resyncing && e.resyncReadFrom != 0 && m.seq >= e.resyncReadFrom
	if ending {
		e.resyncing = false
		e.resyncReadFrom = 0
		e.resyncPending = nil
	}
	unsubscribed := e.apply(m.seq, m.snap, first)
	e.removedDuring = nil
	if e.again {
		e.again = false
		e.requestSnapshot()
	}
	if ending {
		e.log.Info("resync done")
		if e.resyncAgain {
			e.resyncAgain = false
			e.requestResync(e.resyncNext)
		}
	}
	if unsubscribed != "" && !e.resyncing {
		e.requestResync(unsubscribed)
	}
	e.resyncSettle() // in case the snapshot removed the last pane it waited for
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
// which Agents exist and their Status. It subscribes the panes it starts
// tracking, and, when resubscribe is set (the first snapshot of a connection),
// every pane without a subscription. Otherwise a tracked Agent it finds with
// no live or pending subscription is left to a resync: apply returns that
// pane's cause, or "" when there is none.
func (e *Engine) apply(seq uint64, s core.Snapshot, resubscribe bool) string {
	unsubscribed := ""
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
		if tracked && p.terminalID != "" && obs.TerminalID != p.terminalID {
			// The same pane id on another terminal is another Agent: never
			// join the two. An unreported pane is replaced too, so a
			// resolution in flight, read from the old terminal, is dropped.
			e.remove(obs.PaneID, "terminal replaced")
			tracked = false
		}
		fresh := false
		if !tracked {
			if e.removedDuring[obs.PaneID] {
				continue // removed while this read ran; the next read decides
			}
			p = e.track(obs.PaneID)
			p.removableFrom = seq + 1
			fresh = true
		}
		if p.context.PaneTitle != obs.Context.PaneTitle {
			e.log.Debug("pane title", "pane", p.id, "pane_title", obs.Context.PaneTitle)
		}
		p.context = obs.Context
		p.terminalID = obs.TerminalID
		wasKnown := p.known
		if !wasKnown && p.liveDuring != seq {
			// The Status first, so a report the program change causes
			// carries it; unless a live event arrived while this read ran,
			// which it may predate.
			p.status = a.Status
		}
		e.setProgram(p, a.AgentProgram)
		switch {
		case !wasKnown:
			if !p.known {
				e.report(p)
			}
		case p.liveDuring == seq:
			// A live event arrived while this read ran, and the read may
			// predate it. The read that follows settles the Status.
		case a.Status != p.status:
			e.transition(p, a.Status, core.ProvenanceSnapshot, a.StateChangeSeq)
		}
		switch {
		case p.sub != nil || p.subscribing:
		case fresh || resubscribe:
			e.subscribe(p)
		case unsubscribed == "":
			unsubscribed = p.cause
			if unsubscribed == "" {
				unsubscribed = string(herdr.HealthClosed)
			}
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
	return unsubscribed
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

// startReconcile starts the current connection's reconcile timer: every
// reconcileInterval it asks for a snapshot, through the snapshot guard, until
// the connection ends.
func (e *Engine) startReconcile() {
	conn, ctx := e.conn, e.connCtx
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(reconcileInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if !e.send(reconcileDueMsg{conn: conn}) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// requestResync asks for a resync for cause. Before a running resync's read
// has started, the trigger joins it: any pane it left without a subscription
// is subscribed as part of it. After that, it causes exactly one more resync
// once the running one ends. With none running, a resync starts now, or when
// resyncMinInterval since the last one began has elapsed. Requests coalesce.
//
// Until the connection's first snapshot is applied it does nothing: that
// snapshot subscribes every pane, after the epoch is decided.
func (e *Engine) requestResync(cause string) {
	if !e.connected || !e.ready {
		return
	}
	switch {
	case e.resyncing:
		if e.resyncReadFrom == 0 || e.snapSeq < e.resyncReadFrom {
			if e.resyncSubscribe() > 0 {
				e.resyncReadFrom = 0 // wait for these too
			}
			return
		}
		if !e.resyncAgain {
			e.resyncAgain = true
			e.resyncNext = cause
		}
	case e.resyncScheduled:
	default:
		if wait := resyncMinInterval - time.Since(e.resyncLastStart); wait > 0 {
			e.scheduleResync(wait, cause)
			return
		}
		e.startResync(cause)
	}
}

// scheduleResync starts a resync for cause after delay.
func (e *Engine) scheduleResync(delay time.Duration, cause string) {
	e.resyncScheduled = true
	e.resyncNext = cause
	conn, ctx := e.conn, e.connCtx
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
			e.send(resyncDueMsg{conn: conn})
		case <-ctx.Done():
		}
	}()
}

// startResync begins a resync: it is reported once, every tracked pane
// without a subscription is subscribed again, and once those attempts settle a
// snapshot is read.
func (e *Engine) startResync(cause string) {
	e.resyncing = true
	e.resyncLastStart = time.Now()
	e.resyncReadFrom = 0
	e.resyncPending = map[string]bool{}
	e.resyncNext = ""
	e.log.Info("resync", "cause", cause)
	e.sink.Resynced(cause, e.now())
	e.resyncSubscribe()
	e.resyncSettle()
}

// resyncSubscribe subscribes again, as part of the running resync, every
// tracked pane without a live or pending subscription, except one whose
// subscribe outside a resync just failed: that pane is skipped once, and the
// snapshot that ends the resync finds it and causes the next one, at least
// resyncMinInterval later. It returns how many it subscribed.
func (e *Engine) resyncSubscribe() int {
	ids := make([]string, 0, len(e.panes))
	for id := range e.panes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	n := 0
	for _, id := range ids {
		p := e.panes[id]
		if p.sub != nil || p.subscribing {
			continue
		}
		if p.subFailed {
			p.subFailed = false
			continue
		}
		e.subscribe(p)
		e.resyncPending[id] = true
		n++
	}
	return n
}

// resyncSettle starts the running resync's read once none of its subscribe
// attempts is pending: the first read that starts from now on ends it.
func (e *Engine) resyncSettle() {
	if !e.resyncing || len(e.resyncPending) > 0 || e.resyncReadFrom != 0 {
		return
	}
	e.resyncReadFrom = e.snapSeq + 1
	e.requestSnapshot()
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
	delete(e.resyncPending, id) // the caller settles the resync
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

// report reports a pane's Agent for the first time, once its Profile is
// resolved: at once when it is, otherwise when the resolution it starts, or
// the one in flight, ends. Until then the pane's Status is kept up to date
// without being reported.
func (e *Engine) report(p *pane) {
	switch {
	case p.resolved:
		p.known = true
		e.log.Info("agent seen", "pane", p.id, "agent_program", p.program, "profile", p.profile, "status", p.status)
		e.sink.Agent(e.agent(p), e.now())
	case p.resolving == 0:
		e.resolve(p) // which reports p when it ends
	}
}

// setProgram records the Agent Program herdr named for p, and says whether it
// changed. A change resolves the Profile again for an Agent already reported
// or being resolved.
func (e *Engine) setProgram(p *pane, program core.AgentProgram) bool {
	if program == "" || program == p.program {
		return false
	}
	p.program = program
	if p.known || p.resolving != 0 {
		e.resolve(p)
	}
	return true
}

// resolve starts resolving p's Profile, superseding any resolution in flight.
// A program with no rule is its own Profile, settled now without reading
// anything; otherwise the reads run in a goroutine and the result arrives as
// a profileMsg.
func (e *Engine) resolve(p *pane) {
	rule, ok := e.profiles[p.program]
	if !ok {
		p.resolving = 0
		e.settleProfile(p, profileMsg{paneID: p.id, profile: core.ResolveProfile(p.program, nil, "", false)})
		return
	}
	e.nextGen++
	p.resolving = e.nextGen
	id, gen, program := p.id, p.resolving, p.program
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		m := e.readProfile(id, program, rule)
		m.paneID, m.gen = id, gen
		e.send(m)
	}()
}

// readProfile reads the foreground process of a pane and the rule's variable
// from that process's environment, and applies the rule, within
// profileTimeout. Every failure gives core.ProfileUnresolved, with the reason.
func (e *Engine) readProfile(paneID string, program core.AgentProgram, rule core.ProfileRule) profileMsg {
	ctx, cancel := context.WithTimeout(e.runCtx, profileTimeout)
	defer cancel()
	// The environment read cannot be cancelled, so the reads run apart and
	// a read that outlives the timeout is abandoned; done is buffered so it
	// never blocks.
	done := make(chan profileMsg, 1)
	go func() {
		fp, err := e.client.ForegroundProcess(ctx, paneID)
		if err != nil {
			done <- profileMsg{err: fmt.Errorf("engine: foreground process: %w", err)}
			return
		}
		value, set, err := e.environ(fp.PID, rule.Variable)
		if err != nil {
			done <- profileMsg{err: fmt.Errorf("engine: environment of pid %d: %w", fp.PID, err)}
			return
		}
		m := profileMsg{profile: core.ResolveProfile(program, &rule, value, set)}
		if set && m.profile != core.ProfileUnresolved {
			m.matched = value
		}
		done <- m
	}()
	select {
	case m := <-done:
		if m.err != nil {
			m.profile = core.ProfileUnresolved
		}
		return m
	case <-ctx.Done():
		return profileMsg{profile: core.ProfileUnresolved,
			err: fmt.Errorf("engine: profile not resolved within %s: %w", profileTimeout, ctx.Err())}
	}
}

func (e *Engine) onProfile(m profileMsg) {
	p, ok := e.panes[m.paneID]
	if !ok || m.gen == 0 || p.resolving != m.gen {
		return // for a pane gone since, or a resolution superseded
	}
	p.resolving = 0
	e.settleProfile(p, m)
}

// settleProfile records a resolved Profile for p: an Agent not yet reported is
// reported now, and a reported one whose Profile differs is reported as a
// Profile change.
func (e *Engine) settleProfile(p *pane, m profileMsg) {
	attrs := []any{"pane", p.id, "agent_program", p.program, "profile", m.profile}
	if m.matched != "" {
		if rule, ok := e.profiles[p.program]; ok {
			attrs = append(attrs, "variable", rule.Variable, "value", m.matched)
		}
	}
	if m.err != nil {
		attrs = append(attrs, "error", m.err.Error())
	}
	e.log.Debug("profile resolved", attrs...)
	previous, had := p.profile, p.resolved
	p.profile, p.resolved = m.profile, true
	switch {
	case !p.known:
		e.report(p)
	case had && previous != m.profile:
		e.log.Info("profile changed", "pane", p.id, "agent_program", p.program, "profile", m.profile, "previous", previous)
		e.sink.Profile(e.agent(p), previous, e.now())
	}
}

func (e *Engine) transition(p *pane, next core.Status, prov core.Provenance, order *uint64) {
	in := core.TransitionInput{
		Key:          e.key(p.id),
		AgentProgram: p.program,
		Profile:      p.profile,
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
	return Agent{Key: e.key(p.id), AgentProgram: p.program, Profile: p.profile, Status: p.status, Context: p.context}
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
