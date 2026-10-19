package engine

import (
	"context"
	"maps"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/store"
)

// Recorder is what the engine needs from the store (AD-4, AD-9): the
// record's two appends, one Agent's history to fold, the Retention sweep,
// and the engine's own state across Daemon restarts. *store.Store provides it; tests provide a fake. Only the
// engine goroutine calls it.
type Recorder interface {
	Append(t core.Transition) error
	History(ctx context.Context, key core.AgentKey) ([]core.Transition, error)
	AppendResyncMarker(m store.ResyncMarker) error
	Sweep(ctx context.Context, cutoff int64, present []store.Present) (store.SweepResult, error)
	LoadServerState(host, server string) (store.ServerState, bool, error)
	SaveServerState(host, server string, st store.ServerState) error
}

var _ Recorder = (*store.Store)(nil)

// restore runs once as Run starts, before the first connect: it adopts the
// epoch and terminal map the previous run saved, so the first snapshot applies
// the epoch rule (AD-3) against them, and records the Daemon start with the
// epoch now in force. With no Recorder it does nothing. If the saved state
// cannot be loaded, recording is off for the whole run: starting at the first
// epoch and saving it would key new rows like old ones (AD-3). The engine still
// observes and reports to the Sink.
func (e *Engine) restore() {
	if e.store == nil {
		return
	}
	st, found, err := e.store.LoadServerState(e.host, e.server)
	switch {
	case err != nil:
		e.log.Error("server state not loaded; recording is off for this run", "error", err.Error())
		e.store = nil
		return
	case found:
		e.epoch = st.Epoch
		e.terminals = st.Terminals
		// The record may hold this epoch's Agents: their first sights read it.
		e.historic = true
		// The first snapshot decides the epoch as after a reconnect. No Agent
		// is tracked yet, so an increment removes nothing.
		e.everReady = true
		e.saved, e.savedEpoch, e.savedTerminals = true, st.Epoch, maps.Clone(st.Terminals)
		e.log.Info("server state loaded", "epoch", st.Epoch, "panes", len(st.Terminals))
	}
	e.mark(store.ReasonDaemonStart, "")
}

// mark appends a resync marker for reason, with detail ("" for none), under
// the epoch now in force (FR-7). With no Recorder it does nothing and reads no
// clock. A failed write is logged and dropped: it never stops the engine.
func (e *Engine) mark(reason, detail string) {
	if e.store == nil {
		return
	}
	m := store.ResyncMarker{Host: e.host, Server: e.server, Epoch: e.epoch, ObservedAt: e.now(), Reason: reason, Detail: detail}
	if err := e.store.AppendResyncMarker(m); err != nil {
		e.log.Error("resync marker not recorded", "reason", reason, "detail", detail, "epoch", m.Epoch, "error", err.Error())
	}
}

// markCauses marks each of a resync's causes.
func (e *Engine) markCauses(cs []cause) {
	for _, c := range cs {
		e.mark(c.reason, c.detail)
	}
}

// record appends t to the record. A failed write is logged and dropped: it
// never stops the engine or the Sink. The pane is marked unrecorded, so
// Retention keeps all its rows.
func (e *Engine) record(t core.Transition) {
	if e.store == nil {
		return
	}
	if err := e.store.Append(t); err != nil {
		e.log.Error("transition not recorded", "pane", t.Key().PaneID, "error", err.Error())
		if p, ok := e.panes[t.Key().PaneID]; ok {
			p.unrecorded = true
		}
	}
}

// saveState saves the epoch and terminal map after an applied snapshot when
// they differ from what was last saved, or when nothing has been saved yet. A
// failed save is logged and tried again after the next snapshot.
func (e *Engine) saveState() {
	if e.store == nil {
		return
	}
	if e.saved && e.epoch == e.savedEpoch && maps.Equal(e.terminals, e.savedTerminals) {
		return
	}
	st := store.ServerState{Epoch: e.epoch, Terminals: maps.Clone(e.terminals)}
	if err := e.store.SaveServerState(e.host, e.server, st); err != nil {
		e.log.Error("server state not saved", "epoch", e.epoch, "error", err.Error())
		return
	}
	e.saved, e.savedEpoch, e.savedTerminals = true, st.Epoch, st.Terminals
}

// history reads the record of the Agent in p under the epoch now in force,
// when p's first sight is to be seeded from it (seedHistory). A failed read is
// logged, and the record is then treated as holding nothing. It returns false
// when the first sight must not be recorded at all: the read failed because
// the run is ending, and recording the Agent as new would reset its Waiting
// Time at the next start.
func (e *Engine) history(p *pane) ([]core.Transition, bool) {
	if e.store == nil || !p.seedHistory {
		return nil, true
	}
	// Not cancelled by shutdown, for the reason Sweep is not (retention.go).
	ts, err := e.store.History(context.WithoutCancel(e.runCtx), e.key(p.id))
	if err != nil {
		p.unrecorded = true // its fold may not match the record
		if e.runCtx.Err() != nil {
			e.log.Info("history not read as the engine stops; first sight not recorded", "pane", p.id)
			return nil, false
		}
		e.log.Error("history not read; recording the first sight as new", "pane", p.id, "error", err.Error())
		return nil, true
	}
	return ts, true
}
