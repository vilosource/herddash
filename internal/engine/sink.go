package engine

import "github.com/vilosource/herddash/internal/core"

// Agent is one Agent as the engine currently holds it.
type Agent struct {
	Key          core.AgentKey
	AgentProgram core.AgentProgram
	// Status is herdr's string verbatim.
	Status core.Status
	// Context is the tracked context from the latest snapshot that showed the
	// Agent; the zero value until a snapshot has.
	Context core.PaneContext
}

// Transition is core.Transition, named here so that a Sink outside the engine
// can be written without importing core, which cmd may not (the spine's import
// graph).
type Transition = core.Transition

// Sink is where the engine reports what it sees. The engine calls it only from
// its own goroutine, one call at a time and in the order things happened, so an
// implementation needs no locking of its own; it must not block for long, since
// the engine waits for it.
type Sink interface {
	// Agent reports an Agent seen for the first time, at observedAt (UTC Unix
	// milliseconds, from the engine's clock).
	Agent(a Agent, observedAt int64)
	// Transition reports a change of an Agent's Status.
	Transition(t Transition)
	// Removed reports an Agent that is gone, as it last was.
	Removed(a Agent, observedAt int64)
	// Reconnected reports that the engine reached herdr again after losing it:
	// herdr's version, the epoch now in force, and whether it was kept (the
	// same herdr instance) or incremented (AD-3). It comes before the Agent,
	// Transition and removal reports the reconnect causes.
	Reconnected(version string, epoch uint64, kept bool, observedAt int64)
}
