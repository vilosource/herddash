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
	// pingTimeout bounds the ping that reads herdr's version at start.
	pingTimeout = 10 * time.Second
	// subscribeTimeout bounds the setup of a subscription, lifecycle or status,
	// until herdr confirms it started. The subscription itself lives on.
	subscribeTimeout = 10 * time.Second
	// snapshotTimeout bounds one snapshot read. A large session is the
	// biggest response herdr sends.
	snapshotTimeout = 30 * time.Second
)

// liveEpoch is the Epoch of every live and snapshot Transition until Story 1.6
// detects server epochs.
const liveEpoch uint64 = 1

// snapshotMinInterval and snapshotRetryDelay are the defaults above; tests
// lower them.
var (
	snapshotMinInterval = defaultSnapshotMinInterval
	snapshotRetryDelay  = defaultSnapshotRetryDelay
)
