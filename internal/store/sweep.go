package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vilosource/herddash/internal/core"
)

// Present is an Agent the engine reports now, with the instant its current
// Attention Class began (core.FoldResult.Since). Since 0 means it is not
// known, and then none of the Agent's rows is swept.
type Present struct {
	Key   core.AgentKey
	Since int64
}

// SweepResult counts the rows one Sweep deleted.
type SweepResult struct {
	Transitions   int64
	ResyncMarkers int64
}

// presentRow is one Present as the sweep's statement reads it through
// json_each.
type presentRow struct {
	Host   string `json:"h"`
	Server string `json:"s"`
	Epoch  int64  `json:"e"`
	PaneID string `json:"p"`
	Since  int64  `json:"since"`
}

// sweepTransitions deletes the transitions observed before the cutoff (?1),
// except, for an Agent in the present set (?2, a JSON array of presentRow),
// every row from its class start on (all of them when that is unknown, 0) and
// its latest working row, the highest id among its working rows. Those keep
// the fold's Class, Since and EverWorking as they were (FR-9, AD-4). Rows of
// any other key, backfill (epoch 0) among them, go by age alone.
const sweepTransitions = `DELETE FROM transitions AS t
	WHERE t.observed_at < ?1
	AND NOT EXISTS (
		SELECT 1 FROM json_each(?2) AS p
		WHERE p.value ->> 'h' = t.host AND p.value ->> 's' = t.server
		AND p.value ->> 'e' = t.epoch AND p.value ->> 'p' = t.pane_id
		AND (p.value ->> 'since' = 0
			OR t.observed_at >= p.value ->> 'since'
			OR t.id = (SELECT max(w.id) FROM transitions AS w
				WHERE w.host = t.host AND w.server = t.server AND w.epoch = t.epoch
				AND w.pane_id = t.pane_id AND w.new_status = 'working')))`

// sweepMarkers deletes the resync markers observed before the cutoff (?1).
// They go by age alone.
const sweepMarkers = `DELETE FROM resync_markers WHERE observed_at < ?1`

// Sweep applies Retention (FR-9, AD-4) in one transaction: it deletes every
// Transition observed before cutoff (UTC Unix ms) that no present Agent still
// needs, and every resync marker observed before cutoff. For a key in present
// it keeps every row observed at or after its Since, and its latest working
// row. It is the only delete in the codebase, and only the engine calls it.
func (s *Store) Sweep(ctx context.Context, cutoff int64, present []Present) (SweepResult, error) {
	ps := make([]presentRow, 0, len(present))
	for _, p := range present {
		epoch, err := toInt64("epoch", p.Key.Epoch)
		if err != nil {
			return SweepResult{}, err
		}
		ps = append(ps, presentRow{Host: p.Key.Host, Server: p.Key.Server, Epoch: epoch, PaneID: p.Key.PaneID, Since: p.Since})
	}
	b, err := json.Marshal(ps)
	if err != nil {
		return SweepResult{}, fmt.Errorf("store: sweep: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SweepResult{}, fmt.Errorf("store: sweep: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a no-op after Commit
	var r SweepResult
	res, err := tx.ExecContext(ctx, sweepTransitions, cutoff, string(b))
	if err != nil {
		return SweepResult{}, fmt.Errorf("store: sweep transitions: %w", err)
	}
	if r.Transitions, err = res.RowsAffected(); err != nil {
		return SweepResult{}, fmt.Errorf("store: sweep transitions: %w", err)
	}
	res, err = tx.ExecContext(ctx, sweepMarkers, cutoff)
	if err != nil {
		return SweepResult{}, fmt.Errorf("store: sweep resync markers: %w", err)
	}
	if r.ResyncMarkers, err = res.RowsAffected(); err != nil {
		return SweepResult{}, fmt.Errorf("store: sweep resync markers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return SweepResult{}, fmt.Errorf("store: sweep: %w", err)
	}
	return r, nil
}
