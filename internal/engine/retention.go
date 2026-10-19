package engine

import (
	"time"

	"github.com/vilosource/herddash/internal/store"
)

// startRetention runs once, when the run's first connection becomes ready:
// with a Store and a Retention, it sweeps the record now and then every
// retentionSweepInterval until the run ends (FR-9, AD-4). The timer outlives
// connections; a tick that cannot sweep is skipped and the next one tries
// again.
func (e *Engine) startRetention() {
	if e.store == nil {
		return
	}
	if e.retentionDays <= 0 {
		e.log.Info("retention disabled")
		return
	}
	e.log.Info("retention enabled", "retention_days", e.retentionDays, "interval", retentionSweepInterval.String())
	e.sweep()
	ctx := e.runCtx
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(retentionSweepInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if !e.send(retentionDueMsg{}) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// sweep runs one Retention sweep on the engine goroutine: it deletes the rows
// older than Retention that no present Agent still needs. The present set is
// every Agent tracked under the current epoch, with the instant its current
// Attention Class began, or 0, which keeps all its rows, while that is not
// known (not yet reported, or nothing folded) or may not match the record
// (unrecorded). A pane awaiting its first report is present too: its first
// sight may still read its history.
//
// It is skipped while the present set is not known (not connected and ready)
// and while a resync is running or scheduled. A failed sweep is logged; it
// never stops the engine.
func (e *Engine) sweep() {
	if e.store == nil || e.retentionDays <= 0 {
		return
	}
	if !e.connected || !e.ready || e.resyncing || e.resyncScheduled {
		e.log.Debug("retention sweep skipped", "connected", e.connected, "ready", e.ready,
			"resyncing", e.resyncing || e.resyncScheduled)
		return
	}
	cutoff := e.now() - int64(e.retentionDays)*int64(24*time.Hour/time.Millisecond)
	present := make([]store.Present, 0, len(e.panes))
	for id, p := range e.panes {
		var since int64
		if p.known && p.fold.Known && !p.unrecorded {
			since = p.fold.Since
		}
		present = append(present, store.Present{Key: e.key(id), Since: since})
	}
	r, err := e.store.Sweep(e.runCtx, cutoff, present)
	if err != nil {
		if e.runCtx.Err() != nil {
			e.log.Info("retention sweep interrupted as the engine stops")
			return
		}
		e.log.Error("retention sweep failed", "cutoff", cutoff, "error", err.Error())
		return
	}
	e.log.Info("retention sweep", "cutoff", cutoff, "present", len(present),
		"transitions", r.Transitions, "resync_markers", r.ResyncMarkers)
}
