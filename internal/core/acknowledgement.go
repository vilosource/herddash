package core

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidAcknowledgement is wrapped by every error NewAcknowledgement
// returns. Match with errors.Is.
var ErrInvalidAcknowledgement = errors.New("invalid acknowledgement")

// AckSource says where an acknowledgement came from (ADR-0015).
type AckSource string

// The acknowledgement sources.
const (
	// AckSourceBoard is the operator's click on the Board: a mark or an undo.
	// Every row recorded before migration 0007 is one.
	AckSourceBoard AckSource = "board"
	// AckSourceHerdrFocus is the Daemon's inference that the operator read a
	// finish: its pane was focused in herdr long enough and then left
	// (ADR-0015). It is only ever a mark, never an undo.
	AckSourceHerdrFocus AckSource = "herdr_focus"
)

// Valid reports whether s is one of the acknowledgement sources.
func (s AckSource) Valid() bool {
	return s == AckSourceBoard || s == AckSourceHerdrFocus
}

// AcknowledgementInput is everything NewAcknowledgement needs (ADR-0014).
type AcknowledgementInput struct {
	// Key is the acknowledged Agent's identity. Its epoch is never backfill's
	// 0: only a live Agent can be acknowledged.
	Key AgentKey
	// FinishedSince is the finish named: the instant the Agent's Finished
	// class began, FoldResult.Since (the view's waiting_since), UTC Unix ms.
	FinishedSince int64
	// Seen is set for an acknowledgement and clear for its undo.
	Seen bool
	// ObservedAt is the Daemon's clock when it was recorded, UTC Unix ms.
	ObservedAt int64
	// HerdrOrder is herdr's state_change_seq for the Agent's Status when the
	// acknowledgement was made, as a snapshot read it; nil when unknown, as
	// it is for a Status last learnt from a live event. Comparable only
	// within the key's epoch, one herdr server instance.
	HerdrOrder *uint64
	// Source is where it came from (ADR-0015); an undo is always
	// AckSourceBoard.
	Source AckSource
}

// Acknowledgement is one row of the record's acknowledgements: the operator
// marked one exact finish of one Agent seen, or undid that (ADR-0014). It is
// herddash's own state; herdr is never told. It is built only by
// NewAcknowledgement, so every value is valid.
type Acknowledgement struct {
	key           AgentKey
	finishedSince int64
	seen          bool
	observedAt    int64
	herdrOrder    *uint64
	source        AckSource
}

// NewAcknowledgement validates in and builds an Acknowledgement. It refuses an
// empty host, server or pane id, epoch 0, a non-positive FinishedSince or
// ObservedAt, a Source that is not one of the AckSource values (the empty one
// included), and an undo whose Source is AckSourceHerdrFocus, with an error
// wrapping ErrInvalidAcknowledgement.
func NewAcknowledgement(in AcknowledgementInput) (Acknowledgement, error) {
	bad := func(field string, value any) (Acknowledgement, error) {
		return Acknowledgement{}, fmt.Errorf("core: acknowledgement %s %q: %w", field, fmt.Sprint(value), ErrInvalidAcknowledgement)
	}
	switch {
	case in.Key.Host == "":
		return bad("host", in.Key.Host)
	case in.Key.Server == "":
		return bad("server", in.Key.Server)
	case in.Key.PaneID == "":
		return bad("pane id", in.Key.PaneID)
	case in.Key.Epoch == BackfillEpoch:
		return bad("epoch", in.Key.Epoch)
	case in.FinishedSince <= 0:
		return bad("finished since", in.FinishedSince)
	case in.ObservedAt <= 0:
		return bad("observed at", in.ObservedAt)
	case !in.Source.Valid():
		return bad("source", in.Source)
	case !in.Seen && in.Source == AckSourceHerdrFocus:
		return bad("undo source", in.Source)
	}
	a := Acknowledgement{key: in.Key, finishedSince: in.FinishedSince, seen: in.Seen, observedAt: in.ObservedAt,
		source: in.Source}
	if in.HerdrOrder != nil {
		o := *in.HerdrOrder
		a.herdrOrder = &o
	}
	return a, nil
}

// Key is the acknowledged Agent's identity.
func (a Acknowledgement) Key() AgentKey { return a.key }

// FinishedSince is the finish named: when the Agent's Finished class began.
func (a Acknowledgement) FinishedSince() int64 { return a.finishedSince }

// Seen is set for an acknowledgement and clear for an undo.
func (a Acknowledgement) Seen() bool { return a.seen }

// ObservedAt is when the Daemon recorded it, UTC Unix ms.
func (a Acknowledgement) ObservedAt() int64 { return a.observedAt }

// Source is where it came from: the Board, or herdr's focus (ADR-0015).
func (a Acknowledgement) Source() AckSource { return a.source }

// HerdrOrder is herdr's state_change_seq for the Agent's Status when it was
// recorded, and whether that was known.
func (a Acknowledgement) HerdrOrder() (uint64, bool) {
	if a.herdrOrder == nil {
		return 0, false
	}
	return *a.herdrOrder, true
}

// Seen reports whether the Agent folded as f is acknowledged by acks, its
// acknowledgements in recording order (ADR-0014). It holds exactly when f is
// known, its class is finished, its Since is known (positive), and the last of
// acks naming that Since is an acknowledgement rather than an undo. No row for
// that Since means not seen, so any Transition that ends the finish or starts
// it over (a new turn, Needs you, a new Agent's first row) ends it, while
// done to idle within the finish, which moves neither class nor Since, keeps
// it. acks are expected to be one Agent's; the key is not compared.
func Seen(f FoldResult, acks []Acknowledgement) bool {
	if !f.Known || f.Class != ClassFinished || f.Since <= 0 {
		return false
	}
	for i := len(acks) - 1; i >= 0; i-- {
		if acks[i].finishedSince == f.Since {
			return acks[i].seen
		}
	}
	return false
}

// SeenAt is Seen, also refusing an acknowledgement that herdr's ordering
// shows is of an earlier turn with the same start (ADR-0014, amended
// 2026-10-27). An Agent can run a whole turn herddash never observes (the
// Daemon down, or events lost between two snapshot reads) and finish again:
// its Status and so its fold are unchanged, but the result is new. last is
// the Agent's last recorded Transition and current herdr's state_change_seq
// for its Status now, nil when no snapshot read it. herdr's order as herddash
// last knew it is the deciding acknowledgement's, when it was recorded at or
// after last (so nothing changed between them), or else last's; herdr's order
// rises on every Status change and on nothing else, so a current order above
// it means the Status changed since and came back. When either order is
// unknown, SeenAt is Seen. Acknowledgements and last are expected to be of the
// Agent current names, in one epoch.
func SeenAt(f FoldResult, acks []Acknowledgement, last Transition, current *uint64) bool {
	if !Seen(f, acks) {
		return false
	}
	if current == nil {
		return true
	}
	var deciding Acknowledgement
	for i := len(acks) - 1; i >= 0; i-- {
		if acks[i].finishedSince == f.Since {
			deciding = acks[i]
			break
		}
	}
	known, ok := deciding.HerdrOrder()
	if !ok || deciding.observedAt < last.ObservedAt() {
		known, ok = last.HerdrOrder()
	}
	return !ok || *current <= known
}

// LeaveInput is everything SeenOnLeave needs: the facts the engine holds when
// the operator's focus in herdr moved off an Agent's pane (ADR-0015).
type LeaveInput struct {
	// View is the left pane's Agent as published.
	View AgentView
	// FinishHasRow is set when the record already holds an acknowledgement
	// row, of any source, mark or undo, for the view's current finish.
	FinishHasRow bool
	// Dwell is how long the pane was focused during the current finish, by
	// the engine's clock; MinDwell is the least that counts as reading it.
	Dwell, MinDwell time.Duration
}

// SeenOnLeave reports whether leaving a pane marks its Agent's finish Seen
// (ADR-0015): exactly when the view is Finished, not Background and not
// already seen, its finish's start (WaitingSince) is known, the finish has no
// acknowledgement row yet (the automatic rule only writes a finish's first
// row, so the operator's own mark or undo wins), and the pane was focused for
// at least MinDwell. It reads no clock: the engine measures Dwell.
func SeenOnLeave(in LeaveInput) bool {
	v := in.View
	return v.Class == ClassFinished && !v.Background && !v.Seen && v.WaitingSince > 0 &&
		!in.FinishHasRow && in.Dwell >= in.MinDwell
}
