package engine

import (
	"context"
	"time"

	"github.com/vilosource/herddash/internal/core"
)

// onFocus handles herdr's pane_focused for id (ADR-0015). One for the pane
// already focused changes nothing, its dwell going on. One for another pane
// is a leave of the pane focused before it, evaluated by leave, and the new
// pane is focused with its dwell starting now. Either way the event is newer
// than any snapshot read already started, whose focus is then ignored. The
// views of both panes change (in_view), and publish notes them.
//
// While a snapshot's move is held (snapshotFocus), any pane_focused naming
// another pane than the one it moved from is also that pane's leave, judged
// with the dwell held, as it would have been had no snapshot come between:
// the events arrive in herdr's order, so focus left it.
func (e *Engine) onFocus(id string) {
	e.focusEvent = e.snapSeq
	moved := e.focusMoved
	e.focusMoved = nil
	if moved != nil && id != moved.from {
		e.log.Debug("focus", "pane", id, "left", moved.from, "dwell", moved.dwell.String(), "after", "snapshot")
		e.leave(moved.from, moved.dwell)
	}
	if id == e.focusPane {
		return
	}
	now := e.clock()
	left, since := e.focusPane, e.focusSince
	e.focusPane, e.focusSince = id, now
	if left == "" {
		e.log.Debug("focus", "pane", id)
		return
	}
	dwell := now.Sub(since)
	e.log.Debug("focus", "pane", id, "left", left, "dwell", dwell.String())
	e.leave(left, dwell)
}

// leave marks the finish of the Agent in pane id, which focus has just left
// after dwell, Seen when the rule says so (ADR-0015): the Board mark's
// conditions (a present, published Agent under the epoch in force, its fold
// known with a positive start, no rows held for a Profile, its fold in the
// record), then core.SeenOnLeave on its published view, the finish's row and
// the dwell against seenDwell. Leaves are judged only once the connection is
// ready: before its first snapshot the Agents and the epoch are not yet
// confirmed. A mark is one herdr_focus row, with herdr's order for the Status
// when a snapshot read it; a failed write is logged and changes nothing, and
// a later leave of the same finish tries again.
func (e *Engine) leave(id string, dwell time.Duration) {
	p, ok := e.panes[id]
	if !ok || e.store == nil || !e.ready || !p.known || !p.noted {
		return
	}
	if !p.fold.Known || p.fold.Since <= 0 || p.holdRows || p.unrecorded || p.view.WaitingSince != p.fold.Since {
		return
	}
	if !core.SeenOnLeave(core.LeaveInput{View: p.view, FinishHasRow: finishHasRow(p), Dwell: dwell, MinDwell: seenDwell}) {
		return
	}
	in := core.AcknowledgementInput{Key: e.key(p.id), FinishedSince: p.fold.Since, Seen: true, ObservedAt: e.now(),
		Source: core.AckSourceHerdrFocus}
	if p.herdr == p.status {
		in.HerdrOrder = p.herdrSeq // as a Board mark carries it (acknowledge)
	}
	a, err := core.NewAcknowledgement(in)
	if err != nil {
		e.log.Error("acknowledgement not recorded", "pane", p.id, "source", core.AckSourceHerdrFocus, "error", err.Error())
		return
	}
	// Not cancelled by shutdown, for the reason Sweep is not (retention.go).
	if err := e.store.AppendAcknowledgements(context.WithoutCancel(e.runCtx), []core.Acknowledgement{a}); err != nil {
		e.log.Error("acknowledgement not recorded", "pane", p.id, "since", p.fold.Since,
			"source", core.AckSourceHerdrFocus, "error", err.Error())
		return
	}
	p.seenSince, p.rowSince = p.fold.Since, p.fold.Since
	e.log.Info("acknowledgement", "pane", p.id, "since", p.fold.Since, "seen", true,
		"source", core.AckSourceHerdrFocus, "dwell", dwell.String())
	e.noteView(p)
}

// focusMove is a focus move a snapshot showed before any pane_focused for it
// arrived: from the pane focused before, with the dwell it had then, to the
// snapshot's pane.
type focusMove struct {
	from, to string
	dwell    time.Duration
}

// snapshotFocus applies the focus a snapshot read numbered seq names, id, ""
// for none (ADR-0015). A read that started before the latest pane_focused
// arrived is older than that event, and its focus is ignored. Otherwise, with
// reset (a connection's first snapshot, or the one that ends a resync) the
// focus becomes id with its dwell starting now, whatever was held; and a
// later snapshot naming another pane than the one held moves the focus to it
// the same way. Neither is a leave: only an observed pane_focused is, since a
// snapshot's focus can move with no navigation (another client's view).
//
// But a read can also be newer than a pane_focused still on its way: the
// snapshot and the lifecycle stream are separate connections. So a later
// snapshot's move from a held pane is kept as focusMoved, and the next
// pane_focused naming any pane but the one moved from is that pane's leave
// (onFocus), judged with the dwell it had when the snapshot was applied.
// Later snapshots keep the move while they name a pane: the same one, or
// another, which the move then goes to (focus went on through herdr while
// its events were on their way). A reset, a snapshot naming no pane or the
// pane moved from, a disconnect or a restarted dwell of the pane moved from
// discards it.
func (e *Engine) snapshotFocus(seq uint64, id string, reset bool) {
	if e.focusEvent >= seq {
		return
	}
	held := e.focusMoved
	e.focusMoved = nil
	if !reset && id == e.focusPane {
		e.focusMoved = held
		return
	}
	now := e.clock()
	if id != e.focusPane {
		e.log.Debug("focus from a snapshot", "pane", id, "was", e.focusPane)
		switch {
		case reset || id == "":
		case held != nil:
			if id != held.from {
				e.focusMoved = &focusMove{from: held.from, to: id, dwell: held.dwell}
			}
		case e.focusPane != "":
			e.focusMoved = &focusMove{from: e.focusPane, to: id, dwell: now.Sub(e.focusSince)}
		}
	}
	e.focusPane, e.focusSince = id, now
}

// restartDwell counts the dwell of p's pane from now when it is the focused
// pane: its Agent's finish began or ended, or a new Agent arrived in it, so
// time focused before then was not spent on the current finish.
func (e *Engine) restartDwell(p *pane) {
	if e.focusPane != "" && p.id == e.focusPane {
		e.focusSince = e.clock()
	}
	// A held move's leave is of the finish its pane had then. The pane
	// moved to has its own dwell, which does not change that leave.
	if m := e.focusMoved; m != nil && p.id == m.from {
		e.focusMoved = nil
	}
}
