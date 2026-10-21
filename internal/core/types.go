package core

import (
	"errors"
	"fmt"
)

// AgentKey is an Agent's identity (AD-3). Nothing is keyed by pane id alone.
//
// Host is the configured Host name, defaulting to the OS hostname. Server is the
// herdr session name: "default" for the default socket, otherwise the session's
// name. Epoch identifies one herdr server instance's pane-id space; it changes
// only on evidence that herdr reassigned pane ids. Epoch 0 is reserved for
// backfilled history, so live and snapshot Transitions always carry Epoch >= 1.
type AgentKey struct {
	Host   string `json:"host"`
	Server string `json:"server"`
	Epoch  uint64 `json:"epoch"`
	PaneID string `json:"pane_id"`
}

// BackfillEpoch is the Epoch reserved for imported history (AD-3, AD-13).
const BackfillEpoch uint64 = 0

// Status is herdr's own classification of an Agent, recorded verbatim.
type Status string

// The five Statuses herdr reports.
const (
	StatusWorking Status = "working"
	StatusBlocked Status = "blocked"
	StatusDone    Status = "done"
	StatusIdle    Status = "idle"
	StatusUnknown Status = "unknown"
)

// Valid reports whether s is one of the five Statuses herdr reports.
func (s Status) Valid() bool {
	switch s {
	case StatusWorking, StatusBlocked, StatusDone, StatusIdle, StatusUnknown:
		return true
	}
	return false
}

// AgentProgram is the agent program herdr detected in a pane, for example
// "claude" or "codex".
type AgentProgram string

// Provenance says how a Transition came to be known.
type Provenance string

// The three Provenances.
const (
	// ProvenanceLive is a status event observed as it happened.
	ProvenanceLive Provenance = "live"
	// ProvenanceSnapshot is a disagreement found when a snapshot replaced state.
	ProvenanceSnapshot Provenance = "snapshot"
	// ProvenanceBackfill is history imported from the plugin log.
	ProvenanceBackfill Provenance = "backfill"
)

// Valid reports whether p is one of the three Provenances.
func (p Provenance) Valid() bool {
	switch p {
	case ProvenanceLive, ProvenanceSnapshot, ProvenanceBackfill:
		return true
	}
	return false
}

// Sentinel errors NewTransition wraps in a *TransitionError. Match with errors.Is.
var (
	ErrInvalidStatus     = errors.New("invalid status")
	ErrInvalidProvenance = errors.New("invalid provenance")
	ErrInvalidEpoch      = errors.New("epoch does not match provenance")
	ErrInvalidKey        = errors.New("agent key is incomplete")
	ErrInvalidObservedAt = errors.New("observed time must be positive")
	ErrInvalidImportKey  = errors.New("import key does not match provenance")
	ErrInvalidSpurious   = errors.New("only a backfill transition can be possibly spurious")
	ErrInvalidObserver   = errors.New("observer is unknown or does not match provenance")
)

// TransitionError is the typed error NewTransition returns. Field names the
// offending input, Value is its rejected value, and Err is one of the sentinels
// above.
type TransitionError struct {
	Field string
	Value string
	Err   error
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("core: transition %s %q: %v", e.Field, e.Value, e.Err)
}

func (e *TransitionError) Unwrap() error { return e.Err }

// TransitionInput is everything NewTransition needs to build a Transition.
// Optional values are pointers or empty strings, and absent means unknown.
type TransitionInput struct {
	Key          AgentKey
	AgentProgram AgentProgram
	// Profile is the Profile the Agent ran under at the time.
	Profile string
	// Previous is the Status before this Transition; nil when it is not known.
	// It is never set to StatusUnknown to mean "not known".
	Previous *Status
	New      Status
	// HerdrOrder is herdr's ordering information for this change, when known:
	// its per-server state-change counter, which only a snapshot reports. It is
	// nil for a live Transition, and since it restarts with herdr it orders
	// Transitions only within one herdr server instance.
	HerdrOrder *uint64
	// HerdrVersion is the version of the herdr server observed, when known.
	HerdrVersion string
	// ObservedAt is the Daemon's clock, in UTC Unix milliseconds.
	ObservedAt int64
	Provenance Provenance
	// ImportKey uniquely identifies a backfilled row so re-import adds nothing.
	// It is required for backfill and must be empty otherwise.
	ImportKey string
	// PossiblySpurious flags a backfilled done entry logged before the Host
	// ran herdr 0.9.2, whose completions may be spurious. Only a backfill
	// Transition can carry it.
	PossiblySpurious bool
	// ObservedBy says who observed the change: herdr (empty) or, for a live
	// Transition only, Claude Code's session record (ADR-0008).
	ObservedBy Observer
}

// Transition is one recorded change of an Agent's Status. It is built only by
// NewTransition (AD-13), so every Transition in the system has passed the same
// validation; its fields are read through accessors. The zero value is not a
// valid Transition.
type Transition struct {
	key          AgentKey
	agentProgram AgentProgram
	profile      string
	previous     *Status
	next         Status
	herdrOrder   *uint64
	herdrVersion string
	observedAt   int64
	provenance   Provenance
	importKey    string
	spurious     bool
	observedBy   Observer
}

// NewTransition validates in and returns the Transition it describes.
//
// It rejects:
//   - an AgentKey with an empty Host, Server or PaneID
//   - a new Status that is not one of the five
//   - a known previous Status that is not one of the five
//   - an unknown Provenance
//   - a backfill Transition whose Epoch is not BackfillEpoch, and a live or
//     snapshot Transition whose Epoch is
//   - a backfill Transition without an ImportKey, and a live or snapshot
//     Transition with one
//   - an ObservedAt that is not positive
//   - PossiblySpurious on a live or snapshot Transition
//   - an unknown Observer, and an Observer other than herdr on a Transition
//     that is not live
//
// Errors are *TransitionError.
func NewTransition(in TransitionInput) (Transition, error) {
	switch {
	case in.Key.Host == "":
		return Transition{}, &TransitionError{Field: "host", Value: in.Key.Host, Err: ErrInvalidKey}
	case in.Key.Server == "":
		return Transition{}, &TransitionError{Field: "server", Value: in.Key.Server, Err: ErrInvalidKey}
	case in.Key.PaneID == "":
		return Transition{}, &TransitionError{Field: "pane id", Value: in.Key.PaneID, Err: ErrInvalidKey}
	}
	if !in.New.Valid() {
		return Transition{}, &TransitionError{Field: "new status", Value: string(in.New), Err: ErrInvalidStatus}
	}
	if in.Previous != nil && !in.Previous.Valid() {
		return Transition{}, &TransitionError{Field: "previous status", Value: string(*in.Previous), Err: ErrInvalidStatus}
	}
	if !in.Provenance.Valid() {
		return Transition{}, &TransitionError{Field: "provenance", Value: string(in.Provenance), Err: ErrInvalidProvenance}
	}
	backfill := in.Provenance == ProvenanceBackfill
	if backfill != (in.Key.Epoch == BackfillEpoch) {
		return Transition{}, &TransitionError{
			Field: "epoch",
			Value: fmt.Sprintf("%d with provenance %s", in.Key.Epoch, in.Provenance),
			Err:   ErrInvalidEpoch,
		}
	}
	if backfill != (in.ImportKey != "") {
		return Transition{}, &TransitionError{
			Field: "import key",
			Value: fmt.Sprintf("%s with provenance %s", in.ImportKey, in.Provenance),
			Err:   ErrInvalidImportKey,
		}
	}
	if in.ObservedAt <= 0 {
		return Transition{}, &TransitionError{Field: "observed at", Value: fmt.Sprint(in.ObservedAt), Err: ErrInvalidObservedAt}
	}
	if in.PossiblySpurious && !backfill {
		return Transition{}, &TransitionError{Field: "possibly spurious", Value: "true with provenance " + string(in.Provenance), Err: ErrInvalidSpurious}
	}

	if !in.ObservedBy.Valid() || (in.ObservedBy != ObservedByHerdr && in.Provenance != ProvenanceLive) {
		return Transition{}, &TransitionError{Field: "observed by", Value: string(in.ObservedBy) + " with provenance " + string(in.Provenance), Err: ErrInvalidObserver}
	}

	t := Transition{
		key:          in.Key,
		agentProgram: in.AgentProgram,
		profile:      in.Profile,
		next:         in.New,
		herdrVersion: in.HerdrVersion,
		observedAt:   in.ObservedAt,
		provenance:   in.Provenance,
		importKey:    in.ImportKey,
		spurious:     in.PossiblySpurious,
		observedBy:   in.ObservedBy,
	}
	// Copy pointed-to values so the caller cannot mutate the Transition later.
	if in.Previous != nil {
		p := *in.Previous
		t.previous = &p
	}
	if in.HerdrOrder != nil {
		o := *in.HerdrOrder
		t.herdrOrder = &o
	}
	return t, nil
}

// Key returns the Agent's identity.
func (t Transition) Key() AgentKey { return t.key }

// AgentProgram returns the Agent Program.
func (t Transition) AgentProgram() AgentProgram { return t.agentProgram }

// Profile returns the Profile at the time, or "" when not recorded.
func (t Transition) Profile() string { return t.profile }

// Previous returns the previous Status and true, or "" and false when the
// previous Status is not known.
func (t Transition) Previous() (Status, bool) {
	if t.previous == nil {
		return "", false
	}
	return *t.previous, true
}

// New returns the new Status.
func (t Transition) New() Status { return t.next }

// HerdrOrder returns herdr's ordering information and true, or 0 and false when
// it is not known.
func (t Transition) HerdrOrder() (uint64, bool) {
	if t.herdrOrder == nil {
		return 0, false
	}
	return *t.herdrOrder, true
}

// HerdrVersion returns the herdr version observed, or "" when not known.
func (t Transition) HerdrVersion() string { return t.herdrVersion }

// ObservedAt returns the Daemon's observation time in UTC Unix milliseconds.
func (t Transition) ObservedAt() int64 { return t.observedAt }

// Provenance returns how the Transition came to be known.
func (t Transition) Provenance() Provenance { return t.provenance }

// ImportKey returns the backfill import key, or "" when there is none.
func (t Transition) ImportKey() string { return t.importKey }

// PossiblySpurious reports whether the Transition is a backfilled entry
// flagged possibly spurious.
func (t Transition) PossiblySpurious() bool { return t.spurious }

// ObservedBy returns who observed the change: ObservedByHerdr (empty) or
// ObservedByClaudeSession.
func (t Transition) ObservedBy() Observer { return t.observedBy }
