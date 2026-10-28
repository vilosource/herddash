package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vilosource/herddash/internal/core"
)

// The errors Acknowledge refuses a request with (ADR-0014). Match with
// errors.Is; a stale request's error is a *StaleError, which matches
// ErrStaleView.
var (
	// ErrBadRequest refuses a request with no items or with a key twice.
	ErrBadRequest = errors.New("engine: bad acknowledgement request")
	// ErrUnknownAgent refuses a key that names no present, published Agent of
	// this engine under the epoch in force.
	ErrUnknownAgent = errors.New("engine: unknown agent")
	// ErrNotFinished refuses a mark of an Agent whose Box is not Finished:
	// Needs you, Idle, Working, Unknown or Background.
	ErrNotFinished = errors.New("engine: agent not finished")
	// ErrStaleView refuses a mark that names another finish than the one the
	// engine holds now, or one whose rows the record does not hold yet.
	ErrStaleView = errors.New("engine: stale view")
	// ErrRecordingOff refuses every request while nothing is recorded: an
	// acknowledgement that does not survive a restart is not the feature.
	ErrRecordingOff = errors.New("engine: recording is off")
)

// AckRequest asks the engine to acknowledge (Seen) or undo (not Seen) one
// exact finish of each of Items' Agents, all or nothing.
type AckRequest struct {
	Seen  bool
	Items []AckItem
}

// AckItem names one finish: an Agent, and the instant its Finished class
// began as the view showed it (waiting_since).
type AckItem struct {
	Key          core.AgentKey
	WaitingSince int64
}

// StaleError refuses a request whose Keys name a finish the engine no longer
// holds as the request saw it. It matches ErrStaleView.
type StaleError struct {
	Keys []core.AgentKey
}

func (e *StaleError) Error() string {
	names := make([]string, len(e.Keys))
	for i, k := range e.Keys {
		names[i] = fmt.Sprintf("%s/%s/%d/%s", k.Host, k.Server, k.Epoch, k.PaneID)
	}
	return "engine: stale view: " + strings.Join(names, ", ")
}

// Is makes a StaleError match ErrStaleView.
func (e *StaleError) Is(target error) bool { return target == ErrStaleView }

// ackMsg carries one request to the engine goroutine.
type ackMsg struct {
	req   AckRequest
	reply chan ackReply
}

type ackReply struct {
	views []core.AgentView
	err   error
}

// Acknowledge marks seen, or undoes, the finishes req names, and returns the
// Agents' views after it, in request order (ADR-0014). The engine decides
// against its own state, never the caller's, and writes every row in one
// store call or none. It asks the engine goroutine by message, as Agents
// does, and waits for its answer or ctx; a request already delivered is
// carried out even when ctx ends first. herdr is never told.
func (e *Engine) Acknowledge(ctx context.Context, req AckRequest) ([]core.AgentView, error) {
	m := ackMsg{req: req, reply: make(chan ackReply, 1)}
	select {
	case e.inbox <- m:
	case <-e.stopped:
		return nil, ErrStopped
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case r := <-m.reply:
		return r.views, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// seenNow reports whether p's Agent is acknowledged now: its finish's
// acknowledgement is in force while the fold is Finished with the class start
// that was acknowledged.
func seenNow(p *pane) bool {
	return p.seenSince > 0 && p.fold.Known && p.fold.Class == core.ClassFinished && p.fold.Since == p.seenSince
}

// clearStaleSeen forgets p's acknowledgement, and that its finish has a row,
// once its fold has left that finish, so neither ever comes back: core.Seen
// holds only for the finish named.
func clearStaleSeen(p *pane) {
	if p.seenSince != 0 && !seenNow(p) {
		p.seenSince = 0
	}
	if p.rowSince != 0 && !finishHasRow(p) {
		p.rowSince = 0
	}
}

// finishHasRow reports whether the record holds an acknowledgement row, of
// any source, mark or undo, for p's current finish (ADR-0015): the leave
// rule's FinishHasRow. Like seenNow, it holds while the fold is Finished from
// the instant the row names.
func finishHasRow(p *pane) bool {
	return p.rowSince > 0 && p.fold.Known && p.fold.Class == core.ClassFinished && p.fold.Since == p.rowSince
}

// onAcknowledge validates every item of m's request against the engine's
// state, writes the rows in one store call, applies them and replies. A
// refused or failed request changes nothing and publishes nothing. The
// publication goes out before the reply, so a caller that reads the View
// after its reply sees its change.
func (e *Engine) onAcknowledge(m ackMsg) {
	views, err := e.acknowledge(m.req)
	e.publish()
	m.reply <- ackReply{views: views, err: err}
}

func (e *Engine) acknowledge(req AckRequest) ([]core.AgentView, error) {
	if e.store == nil {
		return nil, ErrRecordingOff
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: no items", ErrBadRequest)
	}
	seenKeys := make(map[core.AgentKey]bool, len(req.Items))
	for _, it := range req.Items {
		if seenKeys[it.Key] {
			return nil, fmt.Errorf("%w: %s twice", ErrBadRequest, it.Key.PaneID)
		}
		seenKeys[it.Key] = true
	}
	panes := make([]*pane, len(req.Items))
	// write holds the items whose state changes, by index.
	write := make([]bool, len(req.Items))
	var stale []core.AgentKey
	for i, it := range req.Items {
		k := it.Key
		p, ok := e.panes[k.PaneID]
		if k.Host != e.host || k.Server != e.server || k.Epoch != e.epoch || !ok || !p.known || !p.noted {
			return nil, fmt.Errorf("%w: %s", ErrUnknownAgent, k.PaneID)
		}
		panes[i] = p
		if !req.Seen {
			// An undo writes only for the finish acknowledged now; otherwise
			// there is nothing to undo. One that would write while the pane's
			// rows are held is stale, as a mark is: the view keeps what was
			// last released until the hold ends, so neither the reply nor a
			// publication could show the undo.
			write[i] = seenNow(p) && p.seenSince == it.WaitingSince
			if write[i] && p.holdRows {
				stale = append(stale, k)
			}
			continue
		}
		if p.view.Class != core.ClassFinished || p.view.Background {
			return nil, fmt.Errorf("%w: %s", ErrNotFinished, k.PaneID)
		}
		if !p.fold.Known || p.fold.Since <= 0 || p.fold.Since != it.WaitingSince || p.holdRows || p.unrecorded {
			stale = append(stale, k)
			continue
		}
		write[i] = !seenNow(p)
	}
	if len(stale) > 0 {
		return nil, &StaleError{Keys: stale}
	}
	now := e.now()
	var acks []core.Acknowledgement
	for i, it := range req.Items {
		if !write[i] {
			continue
		}
		in := core.AcknowledgementInput{Key: it.Key, FinishedSince: it.WaitingSince, Seen: req.Seen, ObservedAt: now,
			Source: core.AckSourceBoard}
		if p := panes[i]; p.herdr == p.status {
			// herdr's order for the Status acknowledged, when a snapshot
			// has read it since it last changed: a restart compares it with
			// the order herdr then shows (core.SeenAt).
			in.HerdrOrder = p.herdrSeq
		}
		a, err := core.NewAcknowledgement(in)
		if err != nil {
			return nil, fmt.Errorf("engine: acknowledgement: %w", err)
		}
		acks = append(acks, a)
	}
	if len(acks) > 0 {
		// Not cancelled by shutdown, for the reason Sweep is not (retention.go).
		if err := e.store.AppendAcknowledgements(context.WithoutCancel(e.runCtx), acks); err != nil {
			e.log.Error("acknowledgement not recorded", "items", len(acks), "seen", req.Seen, "error", err.Error())
			return nil, fmt.Errorf("engine: acknowledgement not recorded: %w", err)
		}
	}
	for i, it := range req.Items {
		p := panes[i]
		if write[i] {
			if req.Seen {
				p.seenSince = it.WaitingSince
			} else {
				p.seenSince = 0
			}
			// The finish has a row now: the operator's choice wins over the
			// automatic mark for it (ADR-0015).
			p.rowSince = it.WaitingSince
			e.log.Info("acknowledgement", "pane", p.id, "since", it.WaitingSince, "seen", req.Seen,
				"source", core.AckSourceBoard)
			e.noteView(p)
		}
	}
	views := make([]core.AgentView, len(panes))
	for i, p := range panes {
		views[i] = p.view
	}
	return views, nil
}

// seedSeen derives p's acknowledgement from the record, once its fold has
// been seeded from it (ADR-0014): only a Finished fold with a known start
// can be acknowledged, so nothing else is read. last is the Agent's last
// recorded Transition; with herdr's order for its Status now (p.herdrSeq),
// core.SeenAt refuses an acknowledgement of an earlier turn that ended with
// the same Status, which a Daemon down for a whole turn leaves behind. A
// failed read is logged and leaves the Agent unacknowledged: the Board shows
// more, never less.
//
// The same read decides whether the finish already has a row (ADR-0015),
// by core.FinishRowAt: a row of an earlier turn with the same start does not
// count, so after a turn not observed the automatic mark may write again. A
// failed read counts as a row: the record may hold the operator's own choice
// for this finish, a Mark unseen above all, which a leave must not overrule,
// so no leave marks this finish; the next one, or a later seed that reads,
// is judged as usual.
func (e *Engine) seedSeen(p *pane, last core.Transition) {
	p.seenSince, p.rowSince = 0, 0
	if e.store == nil || !p.seedHistory || !p.fold.Known || p.fold.Class != core.ClassFinished || p.fold.Since <= 0 {
		return
	}
	acks, err := e.store.Acknowledgements(context.WithoutCancel(e.runCtx), e.key(p.id))
	if err != nil {
		e.log.Error("acknowledgements not read; the agent is shown not seen", "pane", p.id, "error", err.Error())
		p.rowSince = p.fold.Since
		return
	}
	if core.FinishRowAt(p.fold, acks, last, p.herdrSeq) {
		p.rowSince = p.fold.Since
	}
	if core.SeenAt(p.fold, acks, last, p.herdrSeq) {
		p.seenSince = p.fold.Since
		e.log.Debug("agent seen since the record", "pane", p.id, "since", p.seenSince)
	} else if core.Seen(p.fold, acks) {
		e.log.Info("acknowledged finish ended by a turn not observed", "pane", p.id, "since", p.fold.Since,
			"herdr_order", *p.herdrSeq)
	}
}

// missedTurn handles a snapshot showing herdr's Status for p unchanged but
// its order risen from was to now, with no live event between: herdr's
// Status changed and came back while herddash was not told, so the Agent ran
// a turn herddash did not observe. Its acknowledgement, of the finish before
// that turn, ends; nothing is written, and the record's Status, unchanged,
// stays as it is. A restart reaches the same answer from the order the
// acknowledgement carries (core.SeenAt). The rows of the earlier finish no
// longer count for this one, so the automatic mark may write again, and a
// focused pane's dwell is counted from now (ADR-0015).
func (e *Engine) missedTurn(p *pane, was, now uint64) {
	p.rowSince = 0
	e.restartDwell(p)
	if !seenNow(p) {
		e.log.Debug("turn not observed", "pane", p.id, "status", p.herdr, "herdr_order", was, "now", now)
		return
	}
	p.seenSince = 0
	e.log.Info("acknowledged finish ended by a turn not observed", "pane", p.id, "since", p.fold.Since,
		"herdr_order", was, "now", now)
}

// copyOrder copies a herdr order, so a pane never shares one with a
// snapshot.
func copyOrder(o *uint64) *uint64 {
	if o == nil {
		return nil
	}
	v := *o
	return &v
}
