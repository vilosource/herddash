package core

// Observation types: what herdr reported, in herddash's vocabulary. The herdr
// adapter maps its wire types to these at its edge (AD-5), and the engine decides
// what they mean (AD-11). They carry no clock reading: when something was observed
// is the engine's to stamp.

// Snapshot is one herdr session snapshot: the server's version and every pane it
// reported, in herdr's order. It is the truth for which panes exist and their
// current Status.
type Snapshot struct {
	// HerdrVersion is the version string the snapshot reported, for example "0.9.5".
	HerdrVersion string
	Panes        []PaneObservation
}

// Pane returns the pane with the given pane id and true, or the zero value and
// false when the snapshot has no such pane.
func (s Snapshot) Pane(paneID string) (PaneObservation, bool) {
	for _, p := range s.Panes {
		if p.PaneID == paneID {
			return p, true
		}
	}
	return PaneObservation{}, false
}

// PaneObservation is one pane as a snapshot reported it.
type PaneObservation struct {
	PaneID string
	// TerminalID changes when herdr restarts even if PaneID survives; it is
	// evidence for epoch detection.
	TerminalID  string
	WorkspaceID string
	TabID       string
	// Agent is set only when herdr reports an agent in this pane.
	Agent *AgentObservation
}

// AgentObservation is the agent herdr reports in a pane.
type AgentObservation struct {
	// AgentProgram is "" when herdr did not name the program.
	AgentProgram AgentProgram
	// Status is herdr's string verbatim; it is not validated here.
	Status Status
	// StateChangeSeq is herdr's server-wide state-change counter for this agent's
	// last change, or nil when herdr did not report one. It restarts with each herdr
	// server instance, so it is comparable only within one.
	StateChangeSeq *uint64
}

// LifecycleKind names which pane lifecycle event herdr reported.
type LifecycleKind string

// The pane lifecycle kinds herddash receives.
const (
	LifecycleCreated       LifecycleKind = "created"
	LifecycleClosed        LifecycleKind = "closed"
	LifecycleMoved         LifecycleKind = "moved"
	LifecycleExited        LifecycleKind = "exited"
	LifecycleAgentDetected LifecycleKind = "agent_detected"
	// LifecycleAgentReleased is herdr reporting that the agent in a pane is gone
	// (an agent-detected event marked released).
	LifecycleAgentReleased LifecycleKind = "agent_released"
)

// Valid reports whether k is one of the lifecycle kinds.
func (k LifecycleKind) Valid() bool {
	switch k {
	case LifecycleCreated, LifecycleClosed, LifecycleMoved, LifecycleExited, LifecycleAgentDetected, LifecycleAgentReleased:
		return true
	}
	return false
}

// LifecycleEvent is one pane lifecycle event, as herdr delivered it live.
type LifecycleEvent struct {
	Kind   LifecycleKind
	PaneID string
	// PreviousPaneID is the pane's id before a move; "" for every other kind.
	PreviousPaneID string
	// TerminalID, WorkspaceID and TabID are the pane's, for created and moved
	// events; "" for every other kind.
	TerminalID  string
	WorkspaceID string
	TabID       string
	// AgentProgram is the program for an agent detection or release; "" otherwise,
	// or when herdr did not name it.
	AgentProgram AgentProgram
	// FinalStatus is the Status herdr reported with an agent detection or release,
	// verbatim; "" when absent.
	FinalStatus Status
}

// StatusEvent is one live Status change of the agent in a pane. It carries no
// herdr sequence: its order is its arrival on the pane's own subscription.
type StatusEvent struct {
	PaneID string
	// Status is herdr's string verbatim; it is not validated here.
	Status Status
	// AgentProgram is "" when herdr did not name the program.
	AgentProgram AgentProgram
}

// ForegroundProcess is the foreground process of a pane, whose environment
// carries the Profile.
type ForegroundProcess struct {
	PID  int
	Name string
	// Argv0 is "" when herdr did not report it.
	Argv0 string
}
