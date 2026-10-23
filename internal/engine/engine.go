package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr"
	"github.com/vilosource/herddash/internal/procenv"
	"github.com/vilosource/herddash/internal/store"
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
	// Children reads the direct children of a process, as procenv.Children
	// does, which is the default. Tests inject a fake.
	Children ChildrenFunc
	// Session reads Claude Code's session record for one process under a
	// configuration directory, as procenv.ClaudeSession does, which is the
	// default. Tests inject a fake.
	Session SessionFunc
	// Transcript tails a Claude Code session transcript, as
	// procenv.TailTranscript does, which is the default. Tests may inject a
	// fake.
	Transcript TranscriptFunc
	// ClaudeRecaps is the operator's consent to Claude Code's session recap
	// being read from the transcript and shown (ADR-0010). False, the
	// recap record is not decoded and every view's recap is "".
	ClaudeRecaps bool
	// Store is where every Transition, every resync marker (Daemon start,
	// connect and reconnect, resync, disagreeing reconcile) and the engine's
	// epoch state are written (FR-6, FR-7, AD-3). Nil records nothing.
	Store Recorder
	// RetentionDays is Retention, in days (FR-9): with a Store, every
	// retentionSweepInterval the record is swept of rows older than it that no
	// present Agent needs. 0 never sweeps.
	RetentionDays int
	// Home is the Daemon user's home directory, for shortening each Agent's
	// project directory in its view; "" shortens nothing.
	Home string
}

// EnvironFunc is the type of Options.Environ.
type EnvironFunc = func(pid int, name string) (value string, set bool, err error)

// ChildrenFunc is the type of Options.Children.
type ChildrenFunc = func(pid int) ([]procenv.Process, error)

// SessionFunc is the type of Options.Session.
type SessionFunc = func(configDir string, pid int) (procenv.Session, error)

// TranscriptFunc is the type of Options.Transcript.
type TranscriptFunc = func(t procenv.Tail, path string, line func([]byte)) (procenv.Tail, bool, error)

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
// A failed read once ready is read again after snapshotRetryDelay, and
// snapshotFailuresBeforeLost failures in a row are a lost connection, handled
// as a lost lifecycle stream is, so a herdr that answers nothing is never
// shown as connected. A lifecycle line flagged unexpected (well-formed, but not
// an event the engine can act on) ends nothing: the engine reads a snapshot.
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
// an agent in a tracked pane, when an Agent's Agent Program changes, and for
// every Agent a reconnect that keeps the epoch finds again; a different result
// is reported to the Sink as a Profile change.
//
// A transient failure (the lookup ran out of profileTimeout, or herdr's
// foreground process call failed) is tried again up to profileRetries times,
// after profileRetryWait, by a timer goroutine that sends a message; a rule
// outcome or an unreadable environment never is. A transient failure for an
// Agent that already holds a Profile resolved for its current program keeps
// that Profile: a failure is no evidence it changed. While a reported Agent's
// Profile is resolved again for one of the triggers above, its new
// Transitions are held, not recorded or reported, until the first resolution
// after the trigger settles (at most profileTimeout), so each row carries the
// Profile resolved for its program. A held row keeps its Status, previous
// Status, provenance and observed_at; only its Profile is decided when it is
// released. Removal and the run's end release held rows first, with the
// Profile held then for a row of the program it was resolved for and
// core.ProfileUnresolved for any other.
//
// Background (FR-2, operator request 2026-10-21): the foreground process a
// Profile resolution read is remembered as the Agent's process. While an
// Agent's program has a background rule and its Status is done or idle
// (core.ChecksBackground), that process's children are read through Children
// backgroundCheckDelay after the Status changes and every backgroundInterval
// after, each read in a goroutine bounded by backgroundTimeout, and the
// verdict arrives as a message. Only a changed verdict changes the view. A
// failed read is not Background, and is logged at debug level once per pane.
// Nothing is recorded: the record, the class and Waiting Time are unchanged.
//
// Claude Code's session record (ADR-0008): for an Agent whose program reads
// one (core.ReadsSession) and whose process is known, the record
// <config dir>/sessions/<pid>.json is read through Session at once and every
// sessionInterval after, whatever the Status, each read in a goroutine
// bounded by sessionTimeout and skipped while the previous one runs. The
// config dir is the process's CLAUDE_CONFIG_DIR, read once per process
// through Environ, or the Daemon's ~/.claude. The pane keeps herdr's latest
// Status apart from its Status, which is core.EffectiveStatus of the two:
// blocked while the record says waiting. A change of the Status the record
// causes, either way, is a live Transition observed by claude_session, at the
// record's statusUpdatedAt when core.SessionObservedAt allows it. A herdr
// change while waiting holds changes herdr's Status only. A failed read is no
// record, logged at debug level once per pane. The watch stops when the
// Agent is removed, its program or process changes, or the run ends.
//
// Claude Code's transcript (ADR-0009): each successful session read that
// names a session id and working directory also tails that session's
// transcript through Transcript, in the same goroutine, timeout and busy
// flag, from where the last read ended (kept per pane). Only the markers
// core.DecodeTranscriptLine finds leave the read; the pane folds them into
// core.BackgroundTasks, which the view turns into background_tasks and
// Background. A transcript that cannot be named or read, or a session
// record that ends, drops what the transcript said, so the process check
// alone decides, logged at debug level once per pane. A new process, a
// program change, removal and the run's end drop it too.
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
	// children and session are Options.Children and Options.Session;
	// read-only after New.
	children   ChildrenFunc
	session    SessionFunc
	transcript TranscriptFunc
	// recaps is Options.ClaudeRecaps; read-only after New.
	recaps bool
	// store is Options.Store, nil when nothing is recorded.
	store Recorder
	// retentionDays is Options.RetentionDays; read-only after New.
	retentionDays int
	// home is Options.Home and runID this run's id; read-only after New.
	home  string
	runID string
	// pub is what readers see; safe from any goroutine.
	pub publisher

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
	// historic is set while the epoch was loaded from the store or kept by a
	// reconnect, so the record may already hold its Agents' Transitions.
	historic bool
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
	// snapFailures counts consecutive failed snapshot reads once ready; a
	// successful read or a disconnect clears it, and reaching
	// snapshotFailuresBeforeLost loses the connection.
	snapFailures int
	// unexpectedWarned is set once this connection logged an unexpected
	// lifecycle line at warn; later ones are logged at debug.
	unexpectedWarned bool
	// readyOnce is set once any connection of this run became ready, so a
	// later one is a reconnect in the record. Unlike everReady, a saved state
	// does not set it.
	readyOnce bool
	// failures counts consecutive failed starts and lost streams, for the
	// backoff. A connection that stays up for reconnectStableAfter clears it.
	failures int
	// readyAt is when the current connection became ready, by the wall clock.
	readyAt time.Time
	// terminals is every pane's terminal_id in the latest applied snapshot,
	// Agent or not: the evidence the epoch rule compares across a reconnect.
	terminals map[string]string
	// released holds the panes whose Agent herdr released and whose next
	// Agent's first row has not been recorded yet (Story 5.17). Such a pane
	// is never seeded from the record: the record ends in its released
	// Agent's rows. A mark only ever splits one Agent, never joins two. It is
	// dropped when the epoch increments or a snapshot no longer shows the
	// pane, and saved with the terminals.
	released map[string]bool
	// saved is set once the epoch and terminals have been saved or loaded;
	// savedEpoch, savedTerminals and savedReleased are what the store holds.
	saved          bool
	savedEpoch     uint64
	savedTerminals map[string]string
	savedReleased  map[string]bool
	panes          map[string]*pane
	nextGen        uint64
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
	// resyncCauses are the running resync's distinct causes, the one it
	// began with first and every trigger that joined it after; each is marked
	// in the record when the resync ends or is cut short.
	resyncCauses []cause
	// resyncAgain records a trigger during the resync's read, so one more
	// resync follows it; resyncScheduled is set while a resync waits for the
	// minimum interval. resyncNext holds the distinct causes either one will
	// carry, the first being the one the Sink is told.
	resyncAgain     bool
	resyncScheduled bool
	resyncNext      []cause
	// reconcileFrom is the first read that serves the reconcile timer's
	// latest request, 0 when none is outstanding. Applying it writes a
	// reconcile marker if it changed anything.
	reconcileFrom uint64
	// resyncLastStart is when the latest resync began, by the wall clock.
	resyncLastStart time.Time

	// truth is the ConnectionTruth now; changes are the current message's
	// changes so far, published when it has been handled; viewSeq is the
	// latest publication's Seq.
	truth   core.ConnectionTruth
	changes []Change
	viewSeq uint64
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
	// Until the Agent is reported, seenBy is how its Status was last learnt
	// (snapshot or live) and seenOrder the snapshot's state_change_seq: what
	// its first-sight Transition records, observed at seenAt (UTC Unix ms;
	// 0 without a Store, which reads no clock for it).
	seenBy    core.Provenance
	seenOrder *uint64
	seenAt    int64
	context   core.PaneContext
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
	cause cause
	// fold is the Agent's record folded (FR-2, FR-3): seeded at its first
	// sight, from the store's history when there is one, and extended by every
	// Transition since.
	fold core.FoldResult
	// seedHistory is set for a pane first tracked by the first snapshot of a
	// connection whose epoch was loaded or kept, unless the terminal_id held
	// for it before that snapshot differs from the one it shows, or its last
	// Agent was released (Engine.released): only its first sight reads the
	// record. A pane tracked later, after a terminal replacement, on a changed
	// terminal or after a release is a new Agent even when it reuses a pane
	// id, and its first row has no previous Status (AD-3).
	seedHistory bool
	// unrecorded is set once the record may not hold this Agent's fold: a
	// History read for its first sight or an Append of one of its Transitions
	// failed. Retention then keeps all its rows (Since 0).
	unrecorded bool
	// firstSkipped is set when the Agent's first sight deliberately recorded
	// nothing (it matched the record), so it was no disagreement.
	firstSkipped bool
	// view is the Agent's view last noted in a change, valid once noted is set.
	view  core.AgentView
	noted bool
	// profile is the Agent's Profile, valid once resolved is set. resolving
	// tags the Profile resolution in flight, 0 when there is none; a result
	// carrying any other tag is stale and ignored.
	profile   string
	resolved  bool
	resolving uint64
	// profileProgram is the Agent Program the Profile was resolved for.
	// profileTries counts the transient failures in a row since the last
	// trigger; retryGen tags the retry timer waiting for the pane, 0 when
	// none.
	profileProgram core.AgentProgram
	profileTries   int
	retryGen       uint64
	// holdRows is set from a trigger that resolves a reported Agent's Profile
	// again until that resolution settles; meanwhile its Transitions wait in
	// held, validated, their Profile decided when they are released. While
	// held, status already says the latest held row's Status, so Agents
	// shows it, but the published view keeps what was last released.
	holdRows bool
	held     []core.TransitionInput
	// pid is the Agent's process: the foreground process its latest Profile
	// resolution read, 0 when not known. It is cleared when the Agent Program
	// changes.
	pid int
	// background is the latest verdict on pid's children, shown by the view
	// while core.ChecksBackground holds. bgGen tags the background watch
	// running for the pane, 0 when none, and bgCtx and bgCancel are its
	// context; bgLogged is set once a failed read was logged.
	background bool
	bgGen      uint64
	// bgBusy is set while a read of the children runs, off the actor.
	bgBusy   *atomic.Bool
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgLogged bool
	// herdr is the latest valid Status herdr reported; status is the
	// effective one, core.EffectiveStatus of herdr and session (ADR-0008).
	herdr core.Status
	// session is the session record's latest status, "" when it is not
	// read, not readable, or not watched. sessGen tags the session watch
	// running for the pane, 0 when none, and sessCtx and sessCancel are its
	// context; sessBusy is set while a read runs, off the actor; sessDir is
	// the process's Claude Code configuration directory once read;
	// sessLogged is set once a failed read was logged.
	session    string
	sessGen    uint64
	sessBusy   *atomic.Bool
	sessCtx    context.Context
	sessCancel context.CancelFunc
	sessDir    string
	sessLogged bool
	// sessFails counts the session reads that have failed in a row; a
	// passing failure keeps the verdict until it reaches sessionFailLimit.
	sessFails int
	// preSession is the Status before the session record made the Agent
	// blocked, the fallback when waiting ends with no valid herdr Status.
	preSession core.Status
	// awaitSession is set while the Agent's first sight waits for its first
	// session read (a restart while its record said waiting); sessDeferred
	// once that wait has been decided, so it happens at most once.
	awaitSession bool
	sessDeferred bool
	// hist is the Agent's stored history, read once for its first sight when
	// histRead is set; histOK is history's second result.
	hist     []core.Transition
	histRead bool
	histOK   bool
	// lastAt is the observed_at of the Agent's latest Transition, recorded
	// or read from the record; 0 when there is none.
	lastAt int64
	// tail is where the session's transcript was last read to, and tasks
	// what it says (ADR-0009); both zero when it is not read. trLogged is
	// set once a failed transcript read was logged.
	tail     procenv.Tail
	tasks    core.BackgroundTasks
	trLogged bool
}

// cause is why a resync runs: the reason the Sink and the record show, and,
// for a health signal carrying one, herdr's error code as its detail.
type cause struct {
	reason string
	detail string
}

// healthCause is the cause a health signal reports: its kind, and for
// events_lost, error and rejected herdr's error code.
func healthCause(h *herdr.Health) cause {
	c := cause{reason: string(h.Kind)}
	switch h.Kind {
	case herdr.HealthEventsLost, herdr.HealthError, herdr.HealthRejected:
		c.detail = h.Code
	}
	return c
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
	// retentionDueMsg says retentionSweepInterval has elapsed again. The
	// timer outlives connections, so it carries no conn: a tick while not
	// connected and ready is skipped.
	retentionDueMsg struct{}

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
		// pid is the foreground process read, 0 when none was.
		pid int
		// transient is set when err may pass: the lookup ran out of time, or
		// herdr's foreground process call failed.
		transient bool
	}
	// profileRetryMsg says the wait before retrying a pane's Profile lookup
	// is over.
	profileRetryMsg struct {
		paneID string
		gen    uint64
	}
	// backgroundMsg is the outcome of one read of a pane's Agent process's
	// children: whether one is a background task, or why the read failed.
	backgroundMsg struct {
		paneID     string
		gen        uint64
		background bool
		err        error
	}
	// sessionMsg is the outcome of one read of a pane's Agent's session
	// record: its status and when that was set, the configuration directory
	// it was read under when that is known, or why the read failed.
	sessionMsg struct {
		paneID    string
		gen       uint64
		dir       string
		status    string
		updatedAt int64
		err       error
		// tail and tasks are the transcript's read position and what it
		// says after this read, valid when the record was read and trErr
		// is nil; trErr is why the transcript was not read.
		tail  procenv.Tail
		tasks core.BackgroundTasks
		trErr error
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
		children:         o.Children,
		session:          o.Session,
		transcript:       o.Transcript,
		recaps:           o.ClaudeRecaps,
		store:            o.Store,
		retentionDays:    o.RetentionDays,
		home:             o.Home,
		runID:            newRunID(),
		inbox:            make(chan any),
		stopped:          make(chan struct{}),
		panes:            map[string]*pane{},
		released:         map[string]bool{},
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
	if e.children == nil {
		e.children = procenv.Children
	}
	if e.session == nil {
		e.session = procenv.ClaudeSession
	}
	if e.transcript == nil {
		e.transcript = procenv.TailTranscript
	}
	e.truth = core.ConnectionTruth{HerdrMinimum: core.HerdrMinimum}
	e.pub.subs = map[*ViewSubscription]struct{}{}
	e.pub.view.Store(&View{RunID: e.runID, Agents: []core.AgentView{}, Connection: e.truth, Headers: []int{}})
	return e
}

// Run starts the engine and runs it until ctx ends, and returns then. Only ctx
// ending stops it: herdr being absent, gone or restarting never does, it
// reconnects. A snapshot that fails once started is logged and read again
// after snapshotRetryDelay. Run closes every subscription and waits for its
// goroutines before it returns. Call it once.
//
// With a Store, Run first loads the epoch and terminal map the previous run
// saved and records a daemon_start marker, then connects; every Transition,
// and every Agent's first sight, is appended as it is observed, and the state
// is saved after each applied snapshot that changed it.
func (e *Engine) Run(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	e.runCtx = runCtx
	defer func() {
		// Held rows are recorded before the run ends (no row is lost).
		e.releaseAll()
		e.stopPublishing()
		e.closeAll()
		cancel()
		e.wg.Wait()
		close(e.stopped)
	}()

	e.restore()
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
	t := e.truth
	t.HerdrVersion, t.Gate, t.ResyncGuaranteed = m.version, m.gate, m.resyncGuaranteed
	if m.gate == core.GateRefused {
		t.Connected = false
	}
	e.setTruth(t)
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

// lost handles a health signal on the lifecycle stream once started, after
// which the engine can no longer trust it has seen every event, or snapshot
// reads failing snapshotFailuresBeforeLost times in a row, after which it can
// no longer read herdr's state. Either way it starts again. A
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
	e.log.Info("connection to herdr lost; reconnecting", "cause", h.Error(), "retry_in", wait.String())
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
	e.snapFailures = 0
	e.unexpectedWarned = false
	t := e.truth
	t.Connected = false
	e.setTruth(t)
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
	// progress or scheduled is abandoned. One already reported is still marked
	// in the record, under the epoch it ran in.
	if e.resyncing {
		e.markCauses(e.resyncCauses)
	}
	e.resyncing = false
	e.resyncPending = nil
	e.resyncReadFrom = 0
	e.resyncAgain = false
	e.resyncScheduled = false
	e.resyncNext = nil
	e.resyncCauses = nil
	e.reconcileFrom = 0
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

// handle handles one inbox message, then publishes what it changed.
func (e *Engine) handle(m any) {
	defer e.publish()
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
			// Whatever is in flight or scheduled, the next read to start
			// serves this request.
			if e.reconcileFrom == 0 {
				e.reconcileFrom = e.snapSeq + 1
			}
			e.requestSnapshot()
		}
	case retentionDueMsg:
		e.sweep()
	case agentsQuery:
		m.reply <- e.agents()
	case profileMsg:
		e.onProfile(m)
	case profileRetryMsg:
		e.onProfileRetry(m)
	case backgroundMsg:
		e.onBackground(m)
	case sessionMsg:
		e.onSession(m)
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
	if d.Unexpected != nil {
		// A well-formed line the engine cannot act on, perhaps from a newer
		// herdr: the stream goes on, and a snapshot reads whatever it was
		// about. The error names only the event, never its payload.
		if !e.unexpectedWarned {
			e.unexpectedWarned = true
			e.log.Warn("unexpected lifecycle event; reading a snapshot", "error", d.Unexpected)
		} else {
			e.log.Debug("unexpected lifecycle event; reading a snapshot", "error", d.Unexpected)
		}
		e.requestSnapshot()
		return
	}
	e.noteEvent()
	ev := d.Event
	switch ev.Kind {
	case core.LifecycleAgentReleased:
		// The release carries the Agent's last Status: record it first, as
		// herdr observed it. The process has gone, so the session record
		// says nothing more: its watch ends first, and with no final Status a
		// Status it caused returns to herdr's, so the record never ends at
		// Needs you for an exited process.
		if p, ok := e.panes[ev.PaneID]; ok && p.known {
			e.stopSession(p, !ev.FinalStatus.Valid())
			if ev.FinalStatus.Valid() {
				if next := e.herdrSays(p, ev.FinalStatus); next != p.status {
					e.transition(p, next, core.ProvenanceLive, nil)
				}
			}
		}
		e.remove(ev.PaneID, string(ev.Kind))
		// The pane's next Agent is another one, even if it starts in the
		// same terminal while herddash is away: never seed it from the
		// record, which ends in this Agent's rows. Saved now, since no
		// snapshot need follow before the Daemon stops; also before a
		// connection's first snapshot, once a state is saved: the epoch and
		// terminals saved with it are then the ones already held.
		e.released[ev.PaneID] = true
		if e.ready || e.saved {
			e.saveState()
		}
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
		// The detection names the program, so a status event that beats the
		// snapshot and names none still reports the Agent under it.
		p.program = ev.AgentProgram
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
		p.cause = healthCause(h)
		e.requestResync(p.cause) // which subscribes again and reads
		return
	}
	ev := m.d.Event
	if e.inFlight {
		p.liveDuring = e.snapSeq
	}
	if !p.known {
		// The Status first, so a report the program change causes carries it.
		e.seen(p, ev.Status, core.ProvenanceLive, nil)
		e.setProgram(p, ev.AgentProgram)
		if !p.known {
			e.report(p)
		}
		return
	}
	// herdr's Status first, so a session verdict a program change ends
	// returns to the Status this event reports, in one row.
	if ev.Status.Valid() {
		p.herdr = ev.Status
	}
	e.setProgram(p, ev.AgentProgram)
	// While the session record says waiting, a herdr change moves herdr's
	// Status alone.
	next := e.herdrSays(p, ev.Status)
	if next == p.status {
		return
	}
	e.transition(p, next, core.ProvenanceLive, nil)
}

// herdrSays records status as herdr's latest for p and returns the effective
// Status it gives. A Status core does not know is returned as it is, for
// NewTransition to reject, and leaves herdr's Status unchanged.
func (e *Engine) herdrSays(p *pane, status core.Status) core.Status {
	if !status.Valid() {
		return status
	}
	p.herdr = status
	return core.EffectiveStatus(p.program, status, p.session)
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
// health signal's kind and code, or closed when the attempt failed before
// herdr answered (a timeout).
func subscribeCause(err error) cause {
	var h *herdr.Health
	if errors.As(err, &h) {
		return healthCause(h)
	}
	return cause{reason: string(herdr.HealthClosed)}
}

func (e *Engine) onSnapshot(m snapshotMsg) {
	e.inFlight = false
	if m.err != nil {
		if !e.ready {
			e.retry(fmt.Errorf("engine: snapshot: %w", m.err))
			return
		}
		e.snapFailures++
		if e.snapFailures >= snapshotFailuresBeforeLost {
			e.lost(fmt.Errorf("engine: %d snapshot reads failed in a row: %w", e.snapFailures, m.err))
			return
		}
		e.log.Warn("snapshot failed; reading again", "error", m.err, "failures", e.snapFailures, "lost_at", snapshotFailuresBeforeLost, "retry_in", snapshotRetryDelay.String())
		e.removedDuring = nil
		e.again = false
		e.schedule(snapshotRetryDelay)
		return
	}
	e.snapFailures = 0
	first := !e.ready
	// The first connection of the run to become ready starts Retention.
	firstOfRun := first && !e.readyOnce
	// The epoch rule's outcome, for the connect or reconnect marker; "" when
	// it did not run.
	epochDetail := ""
	if first {
		e.ready = true
		e.readyAt = time.Now()
		if e.everReady {
			epochDetail = store.DetailEpochIncremented
			if e.decideEpoch(m.snap) {
				epochDetail = store.DetailEpochKept
			}
		}
		e.everReady = true
		e.startReconcile()
	}
	// The read that ends a resync is the first one started after its
	// subscribe attempts settled.
	ending := e.resyncing && e.resyncReadFrom != 0 && m.seq >= e.resyncReadFrom
	var ended []cause
	if ending {
		ended = e.resyncCauses
		e.resyncing = false
		e.resyncReadFrom = 0
		e.resyncPending = nil
		e.resyncCauses = nil
	}
	reconcile := e.reconcileFrom != 0 && m.seq >= e.reconcileFrom
	if reconcile {
		e.reconcileFrom = 0
	}
	// before is the evidence this snapshot replaces: apply compares it so
	// that a pane whose terminal changed is not seeded from the record.
	before := e.terminals
	e.terminals = make(map[string]string, len(m.snap.Panes))
	for _, obs := range m.snap.Panes {
		e.terminals[obs.PaneID] = obs.TerminalID
	}
	// A released pane that is gone will hold no next Agent: drop its mark.
	for id := range e.released {
		if _, shown := e.terminals[id]; !shown {
			delete(e.released, id)
		}
	}
	// Persist the epoch and terminals before this snapshot records anything,
	// so no row is keyed by an epoch the store does not hold.
	e.saveState()
	// The gap this snapshot ends is marked before its rows (FR-7).
	if first {
		reason := store.ReasonReconnect
		if !e.readyOnce {
			reason = store.ReasonConnect
		}
		e.readyOnce = true
		e.mark(reason, epochDetail)
	}
	if ending {
		e.markCauses(ended)
	}
	t := e.truth
	t.Connected, t.LastSnapshotAt = true, e.now()
	e.setTruth(t)
	unsubscribed, changed := e.apply(m.seq, m.snap, first, before)
	if reconcile && changed {
		e.mark(store.ReasonReconcile, "")
	}
	e.removedDuring = nil
	if e.again {
		e.again = false
		e.requestSnapshot()
	}
	if ending {
		e.log.Info("resync done")
		if e.resyncAgain {
			e.resyncAgain = false
			next := e.resyncNext
			e.resyncNext = nil
			for _, c := range next {
				e.requestResync(c) // the first starts or schedules it, the rest join
			}
		}
	}
	if unsubscribed.reason != "" && !e.resyncing {
		e.requestResync(unsubscribed)
	}
	e.resyncSettle() // in case the snapshot removed the last pane it waited for
	if firstOfRun {
		e.startRetention()
	}
}

// decideEpoch applies the epoch rule (AD-3) to the first snapshot after a
// reconnect, before that snapshot is applied. The same herdr instance is shown
// by at least one pane, Agent or not, present with the non-empty terminal_id
// the last applied snapshot showed for it; then the
// epoch is kept, and applying the snapshot removes only the panes that closed
// meanwhile. Otherwise every tracked Agent is removed under the old epoch and
// the epoch increments, so the snapshot's Agents are new ones. A pane-id match
// alone never keeps the epoch. It reports whether the epoch was kept.
func (e *Engine) decideEpoch(s core.Snapshot) bool {
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
	e.historic = kept
	if kept {
		return true
	}
	ids := make([]string, 0, len(e.panes))
	for id := range e.panes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e.remove(id, "herdr restarted")
	}
	// Under the new epoch no pane has a record to be seeded from.
	clear(e.released)
	e.epoch = next
	return false
}

// apply makes the engine agree with snapshot seq: the snapshot is the truth for
// which Agents exist and their Status. It subscribes the panes it starts
// tracking, and, when resubscribe is set (the first snapshot of a connection),
// every pane without a subscription. Otherwise a tracked Agent it finds with
// no live or pending subscription is left to a resync: apply returns that
// pane's cause, or the zero cause when there is none. changed reports whether
// the snapshot disagreed with what the engine held: a snapshot Transition, an
// Agent seen first in it, or an Agent removed. before is every pane's
// terminal_id as the engine held it before this snapshot (saved or last
// applied): a pane it shows on another non-empty terminal than the one now
// seen holds another Agent than the record's last one, so its first sight is
// not seeded from the record.
func (e *Engine) apply(seq uint64, s core.Snapshot, resubscribe bool, before map[string]string) (unsubscribed cause, changed bool) {
	present := map[string]bool{}
	for _, obs := range s.Panes {
		a := obs.Agent
		if a == nil {
			continue
		}
		replaced := false
		present[obs.PaneID] = true
		p, tracked := e.panes[obs.PaneID]
		if tracked && !p.known && seq < p.removableFrom {
			continue // detected by an event; this read may predate its subscription
		}
		if tracked && p.terminalID != "" && obs.TerminalID != p.terminalID {
			// The same pane id on another terminal is another Agent: never
			// join the two. An unreported pane is replaced too, so a
			// resolution in flight, read from the old terminal, is dropped.
			changed = changed || p.known
			e.remove(obs.PaneID, "terminal replaced")
			tracked = false
			replaced = true
		}
		fresh := false
		if !tracked {
			if e.removedDuring[obs.PaneID] {
				continue // removed while this read ran; the next read decides
			}
			p = e.track(obs.PaneID)
			p.removableFrom = seq + 1
			seed := resubscribe && e.historic && !replaced
			if was := before[obs.PaneID]; seed && was != "" && obs.TerminalID != "" && was != obs.TerminalID {
				// The pane id came back on another terminal while the
				// engine was not watching: a new Agent, never joined to
				// the record's.
				e.log.Info("terminal changed since the record; not seeding from it", "pane", obs.PaneID,
					"terminal_id", obs.TerminalID, "recorded_terminal_id", was)
				seed = false
			}
			if seed && e.released[obs.PaneID] {
				// The pane's last Agent was released: this is its next one,
				// never joined to the record's.
				e.log.Info("agent released since the record; not seeding from it", "pane", obs.PaneID)
				seed = false
			}
			p.seedHistory = seed
			fresh = true
		}
		if p.context.PaneTitle != obs.Context.PaneTitle {
			e.log.Debug("pane title", "pane", p.id, "pane_title", obs.Context.PaneTitle)
		}
		p.context = obs.Context
		p.terminalID = obs.TerminalID
		wasKnown := p.known
		// firstSeen is an Agent this snapshot shows first: a disagreement,
		// unless its first sight matched the record and wrote nothing. One
		// still awaiting its Profile was already seen.
		firstSeen := false
		if !wasKnown && p.liveDuring != seq {
			// The Status first, so a report the program change causes
			// carries it; unless a live event arrived while this read ran,
			// which it may predate.
			firstSeen = p.seenBy == ""
			e.seen(p, a.Status, core.ProvenanceSnapshot, a.StateChangeSeq)
		}
		if wasKnown && p.liveDuring != seq && a.Status.Valid() {
			p.herdr = a.Status // as onStatus: before a program change settles
		}
		had := p.program
		programChanged := e.setProgram(p, a.AgentProgram)
		if programChanged && had != "" {
			changed = true // herdr names another program than the engine held
		}
		if wasKnown && resubscribe && !programChanged && p.resolving == 0 {
			// A reconnect that kept the epoch: the Agent may have been
			// restarted under another Profile while herddash was away. A
			// program change has already resolved it again, and a lookup
			// still in flight answers for it: superseding it would restart
			// the wait of any rows held for it.
			e.resolve(p)
		}
		switch {
		case !wasKnown:
			if !p.known {
				e.report(p)
			}
			if p.known && p.firstSkipped {
				firstSeen = false
			}
		case p.liveDuring == seq:
			// A live event arrived while this read ran, and the read may
			// predate it. The read that follows settles the Status.
		default:
			if next := e.herdrSays(p, a.Status); next != p.status {
				e.transition(p, next, core.ProvenanceSnapshot, a.StateChangeSeq)
				changed = true
			}
		}
		e.noteView(p) // a context or program change
		changed = changed || firstSeen
		switch {
		case p.sub != nil || p.subscribing:
		case fresh || resubscribe:
			e.subscribe(p)
		case unsubscribed.reason == "":
			unsubscribed = p.cause
			if unsubscribed.reason == "" {
				unsubscribed = cause{reason: string(herdr.HealthClosed)}
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
		changed = changed || e.panes[id].known
		e.remove(id, "absent from snapshot")
	}
	return unsubscribed, changed
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
// resyncMinInterval since the last one began has elapsed. Requests coalesce,
// keeping every distinct cause for the record.
//
// Until the connection's first snapshot is applied it does nothing: that
// snapshot subscribes every pane, after the epoch is decided.
func (e *Engine) requestResync(c cause) {
	if !e.connected || !e.ready {
		return
	}
	switch {
	case e.resyncing:
		if e.resyncReadFrom == 0 || e.snapSeq < e.resyncReadFrom {
			e.resyncCauses = addCause(e.resyncCauses, c)
			if e.resyncSubscribe() > 0 {
				e.resyncReadFrom = 0 // wait for these too
			}
			return
		}
		e.resyncAgain = true
		e.resyncNext = addCause(e.resyncNext, c)
	case e.resyncScheduled:
		e.resyncNext = addCause(e.resyncNext, c)
	default:
		if wait := resyncMinInterval - time.Since(e.resyncLastStart); wait > 0 {
			e.scheduleResync(wait, c)
			return
		}
		e.startResync([]cause{c})
	}
}

// addCause appends c to cs unless cs already holds it.
func addCause(cs []cause, c cause) []cause {
	if slices.Contains(cs, c) {
		return cs
	}
	return append(cs, c)
}

// scheduleResync starts a resync for cause after delay.
func (e *Engine) scheduleResync(delay time.Duration, c cause) {
	e.resyncScheduled = true
	e.resyncNext = []cause{c}
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

// startResync begins a resync for causes, at least one: it is reported once,
// with the first cause, every tracked pane without a subscription is
// subscribed again, and once those attempts settle a snapshot is read.
func (e *Engine) startResync(causes []cause) {
	if len(causes) == 0 {
		causes = []cause{{reason: string(herdr.HealthClosed)}}
	}
	c := causes[0]
	e.resyncing = true
	e.resyncCauses = slices.Clone(causes)
	e.resyncLastStart = time.Now()
	e.resyncReadFrom = 0
	e.resyncPending = map[string]bool{}
	e.resyncNext = nil
	e.log.Info("resync", "cause", c.reason, "detail", c.detail)
	e.sink.Resynced(c.reason, e.now())
	e.change(Change{Kind: ChangeResync, Reason: c.reason, Detail: c.detail})
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
	p := &pane{id: id, fold: core.Fold(nil)}
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
	e.release(p, "", "")
	delete(e.panes, id)
	delete(e.resyncPending, id) // the caller settles the resync
	e.stopBackground(p)
	e.stopSession(p, false)
	if p.sub != nil {
		p.sub.Close()
	}
	if e.inFlight {
		e.removedDuring[id] = true
	}
	if p.known {
		e.log.Info("agent removed", "pane", id, "agent_program", p.program, "reason", reason)
		e.sink.Removed(e.agent(p), e.now())
		last := p.view
		if !p.noted {
			last = e.viewOf(p)
		}
		e.change(Change{Kind: ChangeRemoved, Key: e.key(p.id), Agent: last})
	}
}

// report reports a pane's Agent for the first time, once its Profile is
// resolved: at once when it is, otherwise when the resolution it starts, or
// the one in flight, ends. Until then the pane's Status is kept up to date
// without being reported.
func (e *Engine) report(p *pane) {
	switch {
	case p.awaitSession:
		// Its first session read reports it (deferSight).
	case p.resolved:
		if e.deferSight(p) {
			return
		}
		p.known = true
		e.log.Info("agent seen", "pane", p.id, "agent_program", p.program, "profile", p.profile, "status", p.status)
		now := e.now()
		p.firstSkipped = e.firstSight(p, now)
		p.seenBy, p.seenOrder, p.seenAt = "", nil, 0
		e.followBackground(p)
		if p.sessGen == 0 { // a deferred first sight's watch runs on
			e.followSession(p, false)
		}
		e.sink.Agent(e.agent(p), now)
		e.noteView(p)
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
	// Another program is another process: the next resolution reads it.
	p.pid, p.background = 0, false
	e.followBackground(p)
	// A first sight waiting on the old process's session record is resolved
	// again for the new program instead.
	deferred := p.awaitSession
	if deferred {
		p.awaitSession, p.resolved = false, false
	}
	e.followSession(p, false)
	if p.known || p.resolving != 0 || deferred {
		e.resolve(p)
	}
	return true
}

// resolve starts resolving p's Profile for a trigger, superseding any
// resolution in flight and any retry waiting. A program with no rule is its
// own Profile, settled now without reading anything; otherwise the reads run
// in a goroutine and the result arrives as a profileMsg, and a reported
// Agent's Transitions are held until it settles.
func (e *Engine) resolve(p *pane) {
	p.profileTries, p.retryGen = 0, 0
	rule, ok := e.profiles[p.program]
	if !ok {
		p.resolving = 0
		e.settleProfile(p, profileMsg{paneID: p.id, profile: core.ResolveProfile(p.program, nil, "", false)})
		return
	}
	if p.known {
		p.holdRows = true
	}
	e.startResolve(p, rule)
}

// startResolve starts the reads of one Profile lookup for p under rule, in a
// goroutine, tagged with a new generation.
func (e *Engine) startResolve(p *pane, rule core.ProfileRule) {
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
// profileTimeout. Every failure gives core.ProfileUnresolved, with the reason;
// running out of time and a failed foreground process call are transient.
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
			done <- profileMsg{err: fmt.Errorf("engine: foreground process: %w", err), transient: true}
			return
		}
		value, set, err := e.environ(fp.PID, rule.Variable)
		if err != nil {
			done <- profileMsg{pid: fp.PID, err: fmt.Errorf("engine: environment of pid %d: %w", fp.PID, err)}
			return
		}
		m := profileMsg{pid: fp.PID, profile: core.ResolveProfile(program, &rule, value, set)}
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
		return profileMsg{profile: core.ProfileUnresolved, transient: true,
			err: fmt.Errorf("engine: profile not resolved within %s: %w", profileTimeout, ctx.Err())}
	}
}

func (e *Engine) onProfile(m profileMsg) {
	p, ok := e.panes[m.paneID]
	if !ok || m.gen == 0 || p.resolving != m.gen {
		return // for a pane gone since, or a resolution superseded
	}
	p.resolving = 0
	if !m.transient {
		p.profileTries = 0
		e.settleProfile(p, m)
		return
	}
	p.profileTries++
	if p.profileTries <= profileRetries {
		wait := profileRetryWait(p.profileTries)
		e.log.Info("profile lookup failed; retrying", "pane", p.id, "agent_program", p.program,
			"attempt", p.profileTries, "retry_in", wait.String(), "error", m.err.Error())
		e.retryProfile(p, wait)
	} else {
		e.log.Warn("profile lookup failed; not retrying until the next trigger", "pane", p.id, "agent_program", p.program,
			"attempts", p.profileTries, "error", m.err.Error())
	}
	if p.known && p.resolved && p.profileProgram == p.program {
		// The Agent holds a Profile resolved for this program, and a failure
		// is no evidence it changed: the Profile and process stay.
		e.log.Debug("profile resolved", "pane", p.id, "agent_program", p.program, "profile", p.profile,
			"error", m.err.Error(), "kept", true)
		e.release(p, "", "")
		e.noteView(p)
		return
	}
	e.settleProfile(p, m)
}

// retryProfile retries p's Profile lookup after wait: a timer goroutine sends
// a profileRetryMsg tagged with a new generation, which a trigger meanwhile
// makes stale.
func (e *Engine) retryProfile(p *pane, wait time.Duration) {
	e.nextGen++
	p.retryGen = e.nextGen
	id, gen := p.id, p.retryGen
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-t.C:
			e.send(profileRetryMsg{paneID: id, gen: gen})
		case <-e.runCtx.Done():
		}
	}()
}

// onProfileRetry starts the retry a profileRetryMsg is for, unless the pane
// is gone or a trigger superseded it. A retry holds no rows.
func (e *Engine) onProfileRetry(m profileRetryMsg) {
	p, ok := e.panes[m.paneID]
	if !ok || m.gen == 0 || p.retryGen != m.gen {
		return
	}
	p.retryGen = 0
	rule, ok := e.profiles[p.program]
	if !ok {
		return
	}
	e.startResolve(p, rule)
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
	var previousProgram core.AgentProgram
	if had {
		previousProgram = p.profileProgram
	}
	p.profile, p.resolved, p.profileProgram = m.profile, true, p.program
	if m.pid != p.pid && (m.pid != 0 || m.err == nil) {
		// Another process: its children are read afresh. A failed read keeps
		// the process already known, so one slow lookup does not end the watch.
		p.pid, p.background = m.pid, false
		e.followBackground(p)
		e.followSession(p, true)
	}
	switch {
	case !p.known:
		e.report(p)
	case had && previous != m.profile:
		e.log.Info("profile changed", "pane", p.id, "agent_program", p.program, "profile", m.profile, "previous", previous)
		e.sink.Profile(e.agent(p), previous, e.now())
		e.release(p, previous, previousProgram)
		e.noteView(p)
	default:
		e.release(p, previous, previousProgram)
		e.noteView(p)
	}
}

// release ends p's hold, if any, and records its held Transitions in order:
// a row of the program p's Profile was resolved for carries that Profile, a
// row of oldProgram (the program oldProfile was resolved for, "" for none)
// carries oldProfile, and any other core.ProfileUnresolved. Each row is
// reported with the Agent's Status, Class and Waiting Time as they were after
// it; the Agent's program and Profile are the current ones, the row's own are
// on the Transition.
func (e *Engine) release(p *pane, oldProfile string, oldProgram core.AgentProgram) {
	if !p.holdRows {
		return
	}
	rows := p.held
	p.holdRows, p.held = false, nil
	final := p.status
	for _, in := range rows {
		switch {
		case p.resolved && in.AgentProgram == p.profileProgram:
			in.Profile = p.profile
		case oldProgram != "" && in.AgentProgram == oldProgram:
			in.Profile = oldProfile
		default:
			in.Profile = core.ProfileUnresolved
		}
		t, err := core.NewTransition(in)
		if err != nil {
			e.log.Warn("transition not recorded", "pane", p.id, "error", err)
			continue
		}
		p.status = in.New
		e.commit(p, t)
	}
	p.status = final
}

// releaseAll releases every pane's held rows, as the run ends.
func (e *Engine) releaseAll() {
	ids := make([]string, 0, len(e.panes))
	for id, p := range e.panes {
		if p.holdRows {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		e.release(e.panes[id], "", "")
	}
}

// followBackground restarts p's background watch for its current Status,
// process and program: a first read after backgroundCheckDelay, then one every
// backgroundInterval. When core.ChecksBackground does not hold, or the Agent is
// not reported or its process is not known, it stops the watch instead and
// clears the verdict. A restart keeps the verdict until the next read.
func (e *Engine) followBackground(p *pane) {
	if !p.known || p.pid <= 0 || !core.ChecksBackground(p.program, p.status) {
		e.stopBackground(p)
		return
	}
	if p.bgCancel != nil {
		p.bgCancel()
	}
	e.nextGen++
	p.bgGen = e.nextGen
	p.bgCtx, p.bgCancel = context.WithCancel(e.runCtx)
	e.checkBackground(p, backgroundCheckDelay)
}

// stopBackground ends p's background watch, if any, and clears the verdict.
func (e *Engine) stopBackground(p *pane) {
	if p.bgCancel != nil {
		p.bgCancel()
	}
	p.bgGen, p.bgCtx, p.bgCancel = 0, nil, nil
	p.background = false
}

// checkBackground reads p's process's children after delay, off the actor,
// and sends the verdict as a backgroundMsg tagged with the watch's generation.
// Ending the watch's context abandons the wait.
func (e *Engine) checkBackground(p *pane, delay time.Duration) {
	if p.bgBusy == nil {
		p.bgBusy = new(atomic.Bool)
	}
	id, gen, pid, program, ctx, busy := p.id, p.bgGen, p.pid, p.program, p.bgCtx, p.bgBusy
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		}
		m := e.readBackground(ctx, busy, pid, program)
		m.paneID, m.gen = id, gen
		e.send(m)
	}()
}

// readBackground reads the children of pid and applies program's background
// rule, within backgroundTimeout. The process read cannot be cancelled, so it
// runs apart and a read that outlives the timeout is abandoned; done is
// buffered so it never blocks. busy is held while a read runs, so a read that
// never returns is not joined by another every backgroundInterval.
func (e *Engine) readBackground(ctx context.Context, busy *atomic.Bool, pid int, program core.AgentProgram) backgroundMsg {
	if !busy.CompareAndSwap(false, true) {
		return backgroundMsg{err: fmt.Errorf("engine: children of pid %d: the previous read has not returned", pid)}
	}
	ctx, cancel := context.WithTimeout(ctx, backgroundTimeout)
	defer cancel()
	done := make(chan backgroundMsg, 1)
	go func() {
		defer busy.Store(false)
		ps, err := e.children(pid)
		if err != nil {
			done <- backgroundMsg{err: fmt.Errorf("engine: children of pid %d: %w", pid, err)}
			return
		}
		argvs := make([][]string, len(ps))
		for i, c := range ps {
			argvs[i] = c.Argv
		}
		done <- backgroundMsg{background: core.HasBackground(program, argvs)}
	}()
	select {
	case m := <-done:
		return m
	case <-ctx.Done():
		return backgroundMsg{err: fmt.Errorf("engine: children of pid %d not read within %s: %w", pid, backgroundTimeout, ctx.Err())}
	}
}

// onBackground records one read's verdict for the watch it belongs to and
// schedules the next read. A failed read is not Background; the first failure
// for a pane is logged at debug level, never as a warning, since a process
// gone or an OS with no reader is ordinary. The view, noted at publish, changes
// only when the verdict does.
func (e *Engine) onBackground(m backgroundMsg) {
	p, ok := e.panes[m.paneID]
	if !ok || m.gen == 0 || p.bgGen != m.gen {
		return // for a pane gone since, or a watch superseded
	}
	next := m.background
	if m.err != nil {
		next = false
		if !p.bgLogged {
			p.bgLogged = true
			e.log.Debug("background not read", "pane", p.id, "pid", p.pid, "error", m.err.Error())
		}
	}
	if next != p.background {
		e.log.Debug("background changed", "pane", p.id, "background", next)
		p.background = next
	}
	e.checkBackground(p, backgroundInterval)
}

// followSession restarts p's session watch for its current process and
// program (ADR-0008): a first read at once, then one every sessionInterval.
// Whatever the record said about an earlier process is dropped first, and a
// Status it caused returns to herdr's; with keep (the same program, another
// process), the verdict is kept instead until the new process's first read
// decides it, so a process that is still waiting records no return and
// reblock. When the program reads no session record, or the Agent is not
// reported or its process is not known, the watch stays stopped.
func (e *Engine) followSession(p *pane, keep bool) {
	watch := p.known && p.pid > 0 && core.ReadsSession(p.program)
	e.endSessionWatch(p)
	// Another process's transcript, or none: read afresh, a failure to read
	// it logged again.
	e.dropTranscript(p)
	p.trLogged = false
	if !keep || !watch {
		e.dropSession(p, true)
	}
	if watch {
		e.startSessionWatch(p)
	}
}

// startSessionWatch starts p's session watch, its first read at once.
func (e *Engine) startSessionWatch(p *pane) {
	e.nextGen++
	p.sessGen, p.sessFails = e.nextGen, 0
	p.sessCtx, p.sessCancel = context.WithCancel(e.runCtx)
	e.checkSession(p, 0)
}

// endSessionWatch ends p's session watch, if any, keeping its verdict.
func (e *Engine) endSessionWatch(p *pane) {
	if p.sessCancel != nil {
		p.sessCancel()
	}
	p.sessGen, p.sessCtx, p.sessCancel, p.sessDir, p.sessFails = 0, nil, nil, "", 0
}

// stopSession ends p's session watch, if any, and forgets what the record
// said. With settle, a Status the record caused returns to herdr's, recorded
// as observed by the record's watch ending; without it (the Agent is going,
// or the caller records herdr's Status next) nothing is recorded.
func (e *Engine) stopSession(p *pane, settle bool) {
	e.endSessionWatch(p)
	e.dropSession(p, settle)
}

// dropSession forgets the record's verdict on p, settling as stopSession
// says.
func (e *Engine) dropSession(p *pane, settle bool) {
	e.dropTranscript(p)
	if p.session == "" {
		return
	}
	p.session = ""
	if settle {
		e.settleSession(p, 0)
	}
}

// dropTranscript forgets what p's transcript said and where it was read to
// (ADR-0009): the process check alone decides until it is read again. A
// recap it held is gone too, logged by pane only (ADR-0010).
func (e *Engine) dropTranscript(p *pane) {
	if p.tasks.Recap() != "" {
		e.log.Debug("claude recap", "pane", p.id, "recap", false)
	}
	p.tail, p.tasks = procenv.Tail{}, core.BackgroundTasks{}
}

// settleSession records a change of p's effective Status that its session
// record caused, as a live Transition observed by claude_session, at
// updatedAt when core.SessionObservedAt allows it. When waiting ends and
// herdr has given no valid Status, the Agent returns to the Status before the
// record made it blocked, or to unknown: it is never left blocked.
func (e *Engine) settleSession(p *pane, updatedAt int64) {
	next := core.EffectiveStatus(p.program, p.herdr, p.session)
	if !next.Valid() {
		if p.status != core.StatusBlocked {
			return
		}
		next = p.preSession
		if !next.Valid() {
			next = core.StatusUnknown
		}
	}
	if !p.known || next == p.status {
		return
	}
	before := p.status
	at := core.SessionObservedAt(updatedAt, p.lastAt, e.now())
	e.transitionAt(p, next, core.ProvenanceLive, nil, core.ObservedByClaudeSession, at)
	switch {
	case p.status != next:
		// not recorded
	case next == core.StatusBlocked && before.Valid():
		p.preSession = before
	case next != core.StatusBlocked:
		p.preSession = ""
	}
}

// deferSight decides whether p's first sight waits for its first session
// read: for an Agent that reads one, with its process known, whose stored
// history ends in blocked observed by claude_session (the Daemon restarted
// while its record said waiting) and which herdr does not itself show
// blocked. Writing herdr's Status first would end the stored wait only for
// the first read to start a new one. The wait is one read, bounded by
// sessionTimeout; it happens at most once per pane.
func (e *Engine) deferSight(p *pane) bool {
	if p.sessDeferred || e.store == nil || !p.seedHistory || p.pid <= 0 ||
		!core.ReadsSession(p.program) || p.status == core.StatusBlocked {
		return false
	}
	p.sessDeferred = true
	ts, ok := e.historyOnce(p)
	if !ok || len(ts) == 0 {
		return false
	}
	last := ts[len(ts)-1]
	if last.New() != core.StatusBlocked || last.ObservedBy() != core.ObservedByClaudeSession || last.AgentProgram() != p.program {
		return false
	}
	if prev, ok := last.Previous(); ok {
		p.preSession = prev
	}
	p.awaitSession = true
	e.log.Debug("first sight waits for the claude session", "pane", p.id)
	e.startSessionWatch(p)
	return true
}

// historyOnce is history(p), read at most once before p's first sight.
func (e *Engine) historyOnce(p *pane) ([]core.Transition, bool) {
	if !p.histRead {
		p.hist, p.histOK = e.history(p)
		p.histRead = true
	}
	return p.hist, p.histOK
}

// checkSession reads p's session record after delay, off the actor, and sends
// what it says as a sessionMsg tagged with the watch's generation. Ending the
// watch's context abandons the wait.
func (e *Engine) checkSession(p *pane, delay time.Duration) {
	if p.sessBusy == nil {
		p.sessBusy = new(atomic.Bool)
	}
	id, gen, pid, dir, ctx, busy := p.id, p.sessGen, p.pid, p.sessDir, p.sessCtx, p.sessBusy
	tail, tasks := p.tail, p.tasks.Clone()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if delay > 0 {
			t := time.NewTimer(delay)
			defer t.Stop()
			select {
			case <-t.C:
			case <-ctx.Done():
				return
			}
		}
		m := e.readSession(ctx, busy, pid, dir, tail, tasks)
		m.paneID, m.gen = id, gen
		e.send(m)
	}()
}

// readSession reads the session record of pid under dir, within
// sessionTimeout. An empty dir is first found from the process's
// CLAUDE_CONFIG_DIR, or the Daemon's ~/.claude when that is unset. The reads
// cannot be cancelled, so they run apart and a read that outlives the timeout
// is abandoned; done is buffered so it never blocks. busy is held while a read
// runs, so a read that never returns is not joined by another.
//
// A record that names its session and working directory has its transcript
// tailed from tail, and what it says folded into tasks, a copy the read owns
// (ADR-0009). Only the markers core.DecodeTranscriptLine finds are kept;
// every line is dropped once decoded.
func (e *Engine) readSession(ctx context.Context, busy *atomic.Bool, pid int, dir string,
	tail procenv.Tail, tasks core.BackgroundTasks) sessionMsg {
	if !busy.CompareAndSwap(false, true) {
		return sessionMsg{err: fmt.Errorf("engine: session of pid %d: the previous read has not returned", pid)}
	}
	ctx, cancel := context.WithTimeout(ctx, sessionTimeout)
	defer cancel()
	done := make(chan sessionMsg, 1)
	go func() {
		defer busy.Store(false)
		if dir == "" {
			value, set, err := e.environ(pid, claudeConfigVar)
			if err != nil {
				done <- sessionMsg{err: fmt.Errorf("engine: session of pid %d: environment: %w", pid, err)}
				return
			}
			if dir = core.ClaudeConfigDir(value, set, e.home); dir == "" {
				done <- sessionMsg{err: fmt.Errorf("engine: session of pid %d: configuration directory not known", pid)}
				return
			}
		}
		s, err := e.session(dir, pid)
		if err != nil {
			done <- sessionMsg{dir: dir, err: fmt.Errorf("engine: session of pid %d: %w", pid, err)}
			return
		}
		m := sessionMsg{dir: dir, status: s.Status, updatedAt: s.StatusUpdatedAt}
		m.tail, m.tasks, m.trErr = e.readTranscript(dir, s, tail, tasks)
		done <- m
	}()
	select {
	case m := <-done:
		return m
	case <-ctx.Done():
		return sessionMsg{err: fmt.Errorf("engine: session of pid %d not read within %s: %w", pid, sessionTimeout, ctx.Err())}
	}
}

// readTranscript tails the transcript the session record s names under dir
// from tail, folding its markers into tasks, and returns where it read to and
// what it says. A read that restarted from the bound starts tasks afresh.
//
// The path follows the record's session id, so another id is another path,
// and the tail restarts there. That is what /clear does: the same process's
// record names a new session id at once (Claude Code 2.1.292, observed
// 2026-10-23; ADR-0009 as amended by Story 5.3), so the old transcript's open
// tasks and recap go and the old file is not read again. A command still
// running across /clear is then left to the process check.
func (e *Engine) readTranscript(dir string, s procenv.Session, tail procenv.Tail,
	tasks core.BackgroundTasks) (procenv.Tail, core.BackgroundTasks, error) {
	path, err := procenv.ClaudeTranscriptPath(dir, s.CWD, s.SessionID)
	if err != nil {
		return procenv.Tail{}, core.BackgroundTasks{}, err
	}
	var lines []core.TranscriptLine
	next, restarted, err := e.transcript(tail, path, func(line []byte) {
		if l, ok := core.DecodeTranscriptLine(line, e.recaps); ok {
			lines = append(lines, l)
		}
	})
	if err != nil {
		return procenv.Tail{}, core.BackgroundTasks{}, err
	}
	if restarted {
		tasks = core.BackgroundTasks{}
	}
	for _, l := range lines {
		tasks.Apply(l)
	}
	return next, tasks, nil
}

// claudeConfigVar names Claude Code's configuration directory in its
// process's environment.
const claudeConfigVar = "CLAUDE_CONFIG_DIR"

// sessionFailLimit is how many session reads in a row may fail, for any
// reason, before the verdict ends.
const sessionFailLimit = 3

// lastingSession reports whether a failed session read means the record will
// not come back: it is gone, or names another process.
func lastingSession(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, procenv.ErrSessionOtherProcess)
}

// onSession records what one read of the session record said, for the watch
// it belongs to, and schedules the next read. A lasting failure, or three in
// a row, is no record: the Status returns to herdr's; a passing one keeps
// the verdict. The first failure for a pane is logged at
// debug level, never as a warning, since an exited process or a Claude Code
// release without the record is ordinary. Only the record's status is ever
// logged.
func (e *Engine) onSession(m sessionMsg) {
	p, ok := e.panes[m.paneID]
	if !ok || m.gen == 0 || p.sessGen != m.gen {
		return // for a pane gone since, or a watch superseded
	}
	if m.dir != "" {
		p.sessDir = m.dir
	}
	next := m.status
	if m.err != nil {
		p.sessFails++
		if !p.sessLogged {
			p.sessLogged = true
			e.log.Debug("claude session not read", "pane", p.id, "pid", p.pid, "error", m.err.Error())
		}
		// A passing failure (a timeout, a read still running, a body caught
		// mid-write) keeps the verdict; a lasting one, or sessionFailLimit in
		// a row, ends it. A deferred first sight takes any failure as none.
		next = p.session
		if p.awaitSession || lastingSession(m.err) || p.sessFails >= sessionFailLimit {
			next = ""
		}
	} else {
		p.sessFails = 0
	}
	e.onTranscript(p, m, next)
	if p.awaitSession {
		// The first sight waited for this read (deferSight): it now sees the
		// effective Status, so a record still waiting finds the Agent
		// unchanged since the record and its wait runs on.
		p.awaitSession = false
		p.session = next
		if next != "" {
			e.log.Debug("claude session", "pane", p.id, "status", next)
		}
		if eff := core.EffectiveStatus(p.program, p.herdr, p.session); eff.Valid() {
			p.status = eff
		}
		if p.session != core.SessionWaiting {
			p.preSession = ""
		}
		e.report(p)
		e.checkSession(p, sessionInterval)
		return
	}
	if next != p.session {
		e.log.Debug("claude session", "pane", p.id, "status", next)
		p.session = next
		e.settleSession(p, m.updatedAt)
	}
	e.checkSession(p, sessionInterval)
}

// onTranscript keeps what one read said of p's transcript (ADR-0009). A read
// of the record that failed keeps it while the record's verdict holds
// (session, the verdict after this read, is not ""); a record that ended, or
// a transcript that could not be named or read, drops it, the transcript
// failure logged at debug level once per pane, by its kind only.
func (e *Engine) onTranscript(p *pane, m sessionMsg, session string) {
	switch {
	case m.err != nil:
		if session == "" {
			e.dropTranscript(p)
		}
		return
	case m.trErr != nil:
		if !p.trLogged {
			p.trLogged = true
			e.log.Debug("claude transcript not read", "pane", p.id, "pid", p.pid, "error", m.trErr.Error())
		}
		e.dropTranscript(p)
		return
	}
	before := p.tasks
	p.tail, p.tasks = m.tail, m.tasks
	if before.Count() != p.tasks.Count() || before.TurnEnded() != p.tasks.TurnEnded() {
		e.log.Debug("claude background tasks", "pane", p.id, "open", p.tasks.Count(), "turn_ended", p.tasks.TurnEnded())
	}
	// A recap's text is never logged (ADR-0010): only that one appeared or
	// cleared.
	if had, has := before.Recap() != "", p.tasks.Recap() != ""; had != has {
		e.log.Debug("claude recap", "pane", p.id, "recap", has)
	}
}

// transition records p's Status changing to next, as herdr observed it, now.
func (e *Engine) transition(p *pane, next core.Status, prov core.Provenance, order *uint64) {
	e.transitionAt(p, next, prov, order, core.ObservedByHerdr, e.now())
}

// transitionAt records p's Status changing to next, observed by by at at.
func (e *Engine) transitionAt(p *pane, next core.Status, prov core.Provenance, order *uint64, by core.Observer, at int64) {
	in := core.TransitionInput{
		Key:          e.key(p.id),
		AgentProgram: p.program,
		Profile:      p.profile,
		New:          next,
		HerdrOrder:   order,
		HerdrVersion: e.herdrVersion,
		ObservedAt:   at,
		Provenance:   prov,
		ObservedBy:   by,
	}
	// A row with no previous Status is an Agent's first (AD-3): the fold
	// starts over there. When the Status is not known but the fold is, this
	// Agent already has rows, so the fold's last Status is the previous one.
	if p.status.Valid() {
		prev := p.status
		in.Previous = &prev
	} else if p.fold.Known {
		prev := p.fold.Status
		in.Previous = &prev
	}
	t, err := core.NewTransition(in)
	if err != nil {
		e.log.Warn("transition not recorded", "pane", p.id, "error", err)
		return
	}
	p.status = next
	p.lastAt = t.ObservedAt()
	e.followBackground(p)
	if p.known && p.resolved && p.profileProgram != p.program {
		// A program change whose resolution has not started yet (a session
		// verdict it ends is recorded first): the row waits for it too, so
		// it never carries the old program's Profile.
		p.holdRows = true
	}
	if p.holdRows {
		// Its Profile is being resolved again: the row waits for it.
		p.held = append(p.held, in)
		return
	}
	e.commit(p, t)
}

// commit folds, records and reports one Transition of p's, which p.status
// already shows.
func (e *Engine) commit(p *pane, t core.Transition) {
	p.fold = p.fold.Add(t)
	e.record(t)
	e.sink.Transition(t, e.agent(p))
	if p.known {
		p.view, p.noted = e.viewOf(p), true
		e.change(Change{Kind: ChangeTransition, Transition: t, Agent: p.view})
	}
}

// seen sets an unreported pane's Status as a snapshot or a status event
// showed it. With a Store it also stamps when that Status was first observed,
// which the first-sight row carries even when the report waits for the Profile.
func (e *Engine) seen(p *pane, status core.Status, prov core.Provenance, order *uint64) {
	if e.store != nil && (p.seenBy == "" || p.status != status) {
		p.seenAt = e.now()
	}
	p.status, p.seenBy, p.seenOrder = status, prov, order
	if status.Valid() {
		p.herdr = status
	}
}

// firstSight seeds an Agent's fold at its first report. For a pane whose
// first sight reads the record (seedHistory), the fold starts from its
// history: if the Status and Agent Program seen are the ones last recorded,
// nothing is written and the class keeps its start, exact if it began live;
// otherwise the change is recorded with the last stored Status as previous.
// Otherwise (a new Agent, a terminal replaced or changed, a released pane's
// next Agent, or an unseeded first snapshot) the first sight is a Transition
// with no previous Status, the boundary where the fold and Retention start
// over for a reused AgentKey (AD-3). Recording that row clears the pane's
// released mark, and saves the change; a row not recorded (a failed Append,
// the run ending) keeps it, so the next start still does not seed. Either way
// the record holds every Agent's current Status (FR-7): snapshot provenance
// when a snapshot showed it, live when a status event did, at the time it was
// observed. reportedAt is the fallback when that is unknown. Without a Store nothing is recorded, but the fold is seeded the
// same way. It reports whether it deliberately recorded nothing: the record
// matched, or the run is ending.
func (e *Engine) firstSight(p *pane, reportedAt int64) (skipped bool) {
	prov, observedAt := p.seenBy, p.seenAt
	if prov == "" {
		if e.store != nil {
			e.log.Warn("first sight with no known source; recording it as snapshot", "pane", p.id)
		}
		prov = core.ProvenanceSnapshot
	}
	if observedAt == 0 {
		observedAt = reportedAt
	}
	ts, ok := e.historyOnce(p)
	p.hist, p.histRead = nil, false
	hist := core.Fold(ts)
	if len(ts) > 0 {
		p.lastAt = ts[len(ts)-1].ObservedAt()
	}
	if hist.Known && hist.Status == p.status && ts[len(ts)-1].AgentProgram() == p.program {
		e.log.Info("agent unchanged since the record", "pane", p.id, "status", p.status, "class", hist.Class, "since", hist.Since)
		p.fold = hist
		return true
	}
	in := core.TransitionInput{
		Key:          e.key(p.id),
		AgentProgram: p.program,
		Profile:      p.profile,
		New:          p.status,
		HerdrVersion: e.herdrVersion,
		ObservedAt:   observedAt,
		Provenance:   prov,
	}
	if hist.Known {
		prev := hist.Status
		in.Previous = &prev
	}
	if prov == core.ProvenanceSnapshot {
		in.HerdrOrder = p.seenOrder
	}
	t, err := core.NewTransition(in)
	if err != nil {
		e.log.Warn("first sight not recorded", "pane", p.id, "error", err)
		p.fold = hist
		return false
	}
	p.fold = hist.Add(t)
	p.lastAt = t.ObservedAt()
	if !ok {
		return true
	}
	e.record(t)
	if _, hasPrev := t.Previous(); !hasPrev && !p.unrecorded && e.released[p.id] {
		// The released pane's next Agent now begins the record's rows for
		// the pane: later starts may seed from them.
		delete(e.released, p.id)
		e.saveState()
	}
	return false
}

func (e *Engine) key(paneID string) core.AgentKey {
	return core.AgentKey{Host: e.host, Server: e.server, Epoch: e.epoch, PaneID: paneID}
}

func (e *Engine) agent(p *pane) Agent {
	a := Agent{Key: e.key(p.id), AgentProgram: p.program, Profile: p.profile, Status: p.status, Context: p.context,
		Class: core.ClassUnknown}
	if p.fold.Known {
		a.Class = p.fold.Class
		a.WaitingSince = p.fold.Since
		a.WaitingExact = p.fold.SinceProvenance == core.ProvenanceLive
	}
	return a
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
