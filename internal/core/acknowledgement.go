package core

import (
	"errors"
	"fmt"
)

// ErrInvalidAcknowledgement is wrapped by every error NewAcknowledgement
// returns. Match with errors.Is.
var ErrInvalidAcknowledgement = errors.New("invalid acknowledgement")

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
}

// NewAcknowledgement validates in and builds an Acknowledgement. It refuses an
// empty host, server or pane id, epoch 0, and a non-positive FinishedSince or
// ObservedAt, with an error wrapping ErrInvalidAcknowledgement.
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
	}
	return Acknowledgement{key: in.Key, finishedSince: in.FinishedSince, seen: in.Seen, observedAt: in.ObservedAt}, nil
}

// Key is the acknowledged Agent's identity.
func (a Acknowledgement) Key() AgentKey { return a.key }

// FinishedSince is the finish named: when the Agent's Finished class began.
func (a Acknowledgement) FinishedSince() int64 { return a.finishedSince }

// Seen is set for an acknowledgement and clear for an undo.
func (a Acknowledgement) Seen() bool { return a.seen }

// ObservedAt is when the Daemon recorded it, UTC Unix ms.
func (a Acknowledgement) ObservedAt() int64 { return a.observedAt }

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
