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
	// Context is what the snapshot said about where the pane lives and what it
	// is titled. The engine tracks it and refreshes it with every snapshot.
	Context PaneContext
	// Agent is set only when herdr reports an agent in this pane.
	Agent *AgentObservation
}

// PaneContext is a pane's tracked context: the facts a Box shows beside the
// Status, and the workspace order the Board sorts by. Every field is "" or 0
// when herdr did not report it.
type PaneContext struct {
	// ProjectDir is the pane's foreground process's working directory, else the
	// pane's own.
	ProjectDir string
	// WorkspaceLabel and TabLabel are the labels of the workspace and tab the
	// pane lives in.
	WorkspaceLabel string
	TabLabel       string
	// WorkspacePosition is the 1-based position of the pane's workspace in
	// herdr's workspace order; 0 when the snapshot did not list the workspace.
	WorkspacePosition int
	// PaneTitle is the one-line label the Agent Program set on its pane, which
	// herdr reports as the pane's terminal title. Log it only at debug level.
	PaneTitle string
	// PanePosition is where the pane sits in its tab, as PanePositions names
	// it ("left", "top-right" and the like); "" when the tab has one pane or
	// herdr reported no layout for it.
	PanePosition string
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
	// LifecycleContextChanged is herdr reporting that a workspace or tab was
	// renamed, moved or reordered, or that a pane was updated. It carries no
	// detail: it is only a signal that tracked context may be stale.
	LifecycleContextChanged LifecycleKind = "context_changed"
)

// Valid reports whether k is one of the lifecycle kinds.
func (k LifecycleKind) Valid() bool {
	switch k {
	case LifecycleCreated, LifecycleClosed, LifecycleMoved, LifecycleExited, LifecycleAgentDetected, LifecycleAgentReleased,
		LifecycleContextChanged:
		return true
	}
	return false
}

// LifecycleEvent is one pane lifecycle event, as herdr delivered it live.
type LifecycleEvent struct {
	Kind LifecycleKind
	// PaneID is the pane the event is about; "" for a context change, which is
	// not about one pane.
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
