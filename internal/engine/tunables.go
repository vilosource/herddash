package engine

import "time"

// The engine's tunables, in one place. At most one snapshot read is in
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
)

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
