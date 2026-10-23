package engine

import "time"

// The engine's tunables, in one place; docs/config.example.toml lists them
// for operators under "Fixed timings", so change it with them. At most one
// snapshot read is in
// flight, any number of requests that arrive during it coalesce into one more
// read after it, and consecutive reads start at least
// defaultSnapshotMinInterval apart.
const (
	// defaultSnapshotMinInterval is the least time between the starts of two
	// snapshot reads. A request inside it schedules one read for when it
	// elapses. Status subscriptions are not delayed by it.
	defaultSnapshotMinInterval = time.Second
	// defaultSnapshotRetryDelay is how long the engine waits before reading
	// again after a snapshot read failed, once started.
	defaultSnapshotRetryDelay = 2 * time.Second
	// defaultSnapshotFailuresBeforeLost is how many snapshot reads in a row
	// must fail, once started, before the engine treats herdr as lost and
	// reconnects. Fewer are retried as blips.
	defaultSnapshotFailuresBeforeLost = 3
	// defaultReconnectInitialDelay is how long the engine waits after losing
	// herdr before its first attempt to start again, and after its first failed
	// attempt. Each further failure doubles the wait.
	defaultReconnectInitialDelay = time.Second
	// defaultReconnectMaxDelay caps the wait between attempts to reach herdr,
	// however long it has been gone. The engine retries forever.
	defaultReconnectMaxDelay = 30 * time.Second
	// defaultReconnectStableAfter is how long a connection must stay up
	// before losing it no longer counts towards the backoff.
	defaultReconnectStableAfter = 30 * time.Second
	// defaultResyncMinInterval is the least time between the starts of two
	// resyncs (AD-11). A trigger inside it schedules one resync for when it
	// elapses. It also bounds how often a pane herdr keeps rejecting is
	// subscribed again: once per interval.
	defaultResyncMinInterval = 5 * time.Second
	// defaultReconcileInterval is how often the engine reads a snapshot while
	// connected, whether or not anything signalled a change: the snapshot is
	// the truth, and events are only the low-latency signal.
	defaultReconcileInterval = 60 * time.Second
	// defaultRefusedRecheckInterval is how often the engine pings a herdr the
	// version gate refused, to see whether it has been upgraded. It is not a
	// backoff: a refused herdr is reachable, and it neither grows the wait nor
	// counts into the reconnect backoff.
	defaultRefusedRecheckInterval = 30 * time.Second
	// defaultProfileTimeout bounds resolving one Agent's Profile: reading its
	// pane's foreground process from herdr and that process's environment. An
	// Agent's first report waits for it, and running out gives
	// core.ProfileUnresolved.
	defaultProfileTimeout = 2 * time.Second
	// defaultProfileRetries is how many times a Profile lookup that failed
	// transiently (it ran out of profileTimeout, or herdr's foreground process
	// call failed) is tried again before the engine gives up until the next
	// trigger. A rule outcome or an unreadable environment is never retried.
	defaultProfileRetries = 3
	// defaultProfileRetryDelay is the wait before the first retry of a
	// transiently failed Profile lookup; each further retry doubles it.
	defaultProfileRetryDelay = 2 * time.Second
	// defaultProfileHoldFactor sets how long a reported Agent's rows may be
	// held while its Profile is resolved again: profileHoldFactor times
	// profileTimeout from the hold's start, never moved by a later trigger.
	// One trigger superseding a lookup just before it ends still settles
	// inside it; only an unending stream of triggers or a lookup that cannot
	// run reaches it, and the rows are then released with the best Profile
	// known.
	defaultProfileHoldFactor = 2
	// defaultBackgroundCheckDelay is how long after a background-capable
	// Agent's Status changes to done or idle, or its process becomes known,
	// the engine first reads that process's children for background work: a
	// turn's last tool shell is given a moment to exit.
	defaultBackgroundCheckDelay = 250 * time.Millisecond
	// defaultBackgroundInterval is how often the engine reads those children
	// again while the Status stays done or idle.
	defaultBackgroundInterval = 5 * time.Second
	// defaultBackgroundTimeout bounds one read of an Agent process's
	// children; a read that outlives it is abandoned and counts as no
	// background work.
	defaultBackgroundTimeout = 2 * time.Second
	// defaultSessionInterval is how often the engine reads a Claude Code
	// Agent's session record for whether it waits on the operator (ADR-0008).
	defaultSessionInterval = 2 * time.Second
	// defaultSessionTimeout bounds one read of that record, with the
	// environment read that finds its configuration directory; a read that
	// outlives it is abandoned and counts as no record.
	defaultSessionTimeout = time.Second
	// defaultRetentionSweepInterval is how often the engine sweeps the record
	// for rows older than Retention, from the first connection of a run
	// becoming ready on (FR-9, AD-4).
	defaultRetentionSweepInterval = time.Hour
	// pingTimeout bounds the ping that reads herdr's version at each start.
	pingTimeout = 10 * time.Second
	// subscribeTimeout bounds the setup of a subscription, lifecycle or status,
	// until herdr confirms it started. The subscription itself lives on.
	subscribeTimeout = 10 * time.Second
	// snapshotTimeout bounds one snapshot read. A large session is the
	// biggest response herdr sends.
	snapshotTimeout = 30 * time.Second
)

// firstEpoch is the epoch a Daemon starts at when its Store holds no saved
// state for its Host and Server, or it has no Store (AD-3).
const firstEpoch uint64 = 1

// The package variables below are the defaults above; tests lower them.
var (
	snapshotMinInterval    = defaultSnapshotMinInterval
	snapshotRetryDelay     = defaultSnapshotRetryDelay
	reconnectInitialDelay  = defaultReconnectInitialDelay
	reconnectMaxDelay      = defaultReconnectMaxDelay
	reconnectStableAfter   = defaultReconnectStableAfter
	resyncMinInterval      = defaultResyncMinInterval
	reconcileInterval      = defaultReconcileInterval
	refusedRecheckInterval = defaultRefusedRecheckInterval
	profileTimeout         = defaultProfileTimeout
	profileRetries         = defaultProfileRetries
	profileRetryDelay      = defaultProfileRetryDelay
	profileHoldFactor      = defaultProfileHoldFactor
	retentionSweepInterval = defaultRetentionSweepInterval
	backgroundCheckDelay   = defaultBackgroundCheckDelay
	backgroundInterval     = defaultBackgroundInterval
	backgroundTimeout      = defaultBackgroundTimeout
	sessionInterval        = defaultSessionInterval
	sessionTimeout         = defaultSessionTimeout
)

// snapshotFailuresBeforeLost is defaultSnapshotFailuresBeforeLost; tests may
// lower it.
var snapshotFailuresBeforeLost = defaultSnapshotFailuresBeforeLost

// reconnectDelay is the wait after the nth consecutive failed attempt to start
// (n >= 1): the initial delay, doubled for each further failure, never more
// than the cap.
func reconnectDelay(n int) time.Duration {
	d := reconnectInitialDelay
	for i := 1; i < n && d < reconnectMaxDelay; i++ {
		d *= 2
	}
	return min(d, reconnectMaxDelay)
}

// profileRetryWait is the wait before the nth retry of a Profile lookup
// (n >= 1): profileRetryDelay, doubled for each retry before it.
func profileRetryWait(n int) time.Duration {
	d := profileRetryDelay
	for i := 1; i < n; i++ {
		d *= 2
	}
	return d
}

// profileHoldLimit is the longest a hold of a reported Agent's rows lasts:
// profileHoldFactor times profileTimeout.
func profileHoldLimit() time.Duration {
	return time.Duration(profileHoldFactor) * profileTimeout
}
