package core

// AttentionClass is what an Agent asks of the operator, derived from its
// Status and whether it was ever working (FR-2).
type AttentionClass string

// The four Attention Classes.
const (
	ClassNeedsYou AttentionClass = "needs_you"
	ClassFinished AttentionClass = "finished"
	ClassWorking  AttentionClass = "working"
	ClassUnknown  AttentionClass = "unknown"
)

// ClassOf is the Attention Class of an Agent in Status s. An idle Agent that
// was ever working has finished; one that never was is still starting, so it
// counts as working. Anything else, including no Status, is unknown.
func ClassOf(s Status, everWorking bool) AttentionClass {
	switch s {
	case StatusBlocked:
		return ClassNeedsYou
	case StatusDone:
		return ClassFinished
	case StatusIdle:
		if everWorking {
			return ClassFinished
		}
		return ClassWorking
	case StatusWorking:
		return ClassWorking
	}
	return ClassUnknown
}

// FoldResult is what an Agent's ordered Transitions say about it now.
type FoldResult struct {
	// Known is set once at least one Transition has been folded.
	Known bool
	// Status is the latest Transition's new Status.
	Status Status
	Class  AttentionClass
	// Since is the ObservedAt (UTC Unix ms) of the Transition at which Class
	// began; a later Transition in the same Class does not move it, except the
	// first working one (see Add).
	Since int64
	// SinceProvenance is that Transition's Provenance.
	SinceProvenance Provenance
	// EverWorking is set once any Transition's new Status was working.
	EverWorking bool
}

// Fold folds ts in the order given, which is the record's order (History's).
// With no Transitions the result is not Known and its Class is unknown. One
// AgentKey can hold successive Agents (AD-3): a Transition with no previous
// Status is an Agent's first row, and the fold starts over there, so the
// result describes only the Agent the last such row began.
func Fold(ts []Transition) FoldResult {
	f := FoldResult{Class: ClassUnknown}
	for _, t := range ts {
		f = f.Add(t)
	}
	return f
}

// Add extends f by one Transition, later than every one folded so far. Adding
// Transitions one by one gives the same result as folding them all at once.
//
// A Transition with no previous Status is a new Agent's first row: f is
// discarded and the fold starts over from it, Class, Since and EverWorking
// alike, so nothing of an earlier Agent on the same AgentKey carries across.
//
// The first working Transition restarts Since even though the Class stays
// working: before it the Agent was Idle on the Board, a separate state, and
// its Waiting Time as Working starts there (Board layout v4, 2026-10-21).
func (f FoldResult) Add(t Transition) FoldResult {
	if _, ok := t.Previous(); !ok {
		f = FoldResult{Class: ClassUnknown}
	}
	firstWork := !f.EverWorking && t.New() == StatusWorking
	f.EverWorking = f.EverWorking || t.New() == StatusWorking
	class := ClassOf(t.New(), f.EverWorking)
	if !f.Known || class != f.Class || firstWork {
		f.Since = t.ObservedAt()
		f.SinceProvenance = t.Provenance()
	}
	f.Known = true
	f.Status = t.New()
	f.Class = class
	return f
}

// WaitingTime is how long the Agent has been in its current Class at now (UTC
// Unix ms), never negative. It is exact only when the Class began with a live
// Transition; otherwise it is a lower bound. An unknown fold waits 0, inexact.
func WaitingTime(f FoldResult, now int64) (ms int64, exact bool) {
	if !f.Known {
		return 0, false
	}
	return max(now-f.Since, 0), f.SinceProvenance == ProvenanceLive
}
