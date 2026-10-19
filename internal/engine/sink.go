package engine

import "github.com/vilosource/herddash/internal/core"

// Agent is one Agent as the engine currently holds it.
type Agent struct {
	Key          core.AgentKey
	AgentProgram core.AgentProgram
	// Profile is the Profile the Agent runs under (FR-10): its rule's result,
	// core.ProfileUnresolved when that could not be applied, or the Agent
	// Program's name for a program with no rule.
	Profile string
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

// Gate is core.Gate, named here for the same reason as Transition.
type Gate = core.Gate

// The Gate outcomes and the minimum supported herdr, as core defines them.
const (
	GateOK       = core.GateOK
	GateAdvisory = core.GateAdvisory
	GateRefused  = core.GateRefused
	HerdrMinimum = core.HerdrMinimum
	// ProfileUnresolved is the Profile of an Agent whose rule could not be
	// applied.
	ProfileUnresolved = core.ProfileUnresolved
)

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
	// Resynced reports that a resync began: the engine could no longer trust a
	// status stream, so it subscribes again where needed and reads a snapshot
	// (AD-11). cause names the trigger: a herdr.HealthKind (events_lost, error,
	// rejected, closed, undecodable). It comes before any Transition or removal
	// the resync's snapshot causes.
	Resynced(cause string, observedAt int64)
	// Gate reports the version gate's outcome (FR-16) for a connect to herdr
	// whose outcome or herdr version differs from the last one reported,
	// including the first. It comes before anything else that connect causes:
	// the removals a refusal causes, or the Agents a start reports.
	Gate(gate core.Gate, herdrVersion string, observedAt int64)
	// Profile reports that resolving an Agent's Profile again gave another
	// Profile: a carries the new one, and previous is the one it replaces.
	// Transitions after it carry the new one.
	Profile(a Agent, previous string, observedAt int64)
}
