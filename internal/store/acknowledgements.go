package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vilosource/herddash/internal/core"
)

// insertAcknowledgement is the one INSERT of an acknowledgement (ADR-0014),
// every column bound, its source (ADR-0015) included.
const insertAcknowledgement = `INSERT INTO acknowledgements
	(host, server, epoch, pane_id, finished_since, seen, observed_at, herdr_order, source)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

// AppendAcknowledgements inserts acknowledgements, in the order given, in one
// transaction: every row is written or none is. None is a no-op. Rows are
// never updated; an undo is a row with Seen clear. Only the engine calls it.
func (s *Store) AppendAcknowledgements(ctx context.Context, acks []core.Acknowledgement) error {
	if len(acks) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: append acknowledgements: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a no-op after Commit
	stmt, err := tx.PrepareContext(ctx, insertAcknowledgement)
	if err != nil {
		return fmt.Errorf("store: append acknowledgements: %w", err)
	}
	defer stmt.Close()
	for _, a := range acks {
		k := a.Key()
		epoch, err := toInt64("epoch", k.Epoch)
		if err != nil {
			return err
		}
		seen := 0
		if a.Seen() {
			seen = 1
		}
		var order any // NULL when unknown
		if o, ok := a.HerdrOrder(); ok {
			if order, err = toInt64("state_change_seq", o); err != nil {
				return err
			}
		}
		if _, err := stmt.ExecContext(ctx, k.Host, k.Server, epoch, k.PaneID, a.FinishedSince(), seen, a.ObservedAt(), order,
			string(a.Source())); err != nil {
			return fmt.Errorf("store: append acknowledgement for %s at %d: %w", k.PaneID, a.FinishedSince(), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: append acknowledgements: %w", err)
	}
	return nil
}

// Acknowledgements returns one Agent's acknowledgements in recording order:
// by id, the order the single writer appended them. Not by observed_at: a
// wall clock stepped back between a Seen and its Undo would otherwise put the
// undo first and core.Seen would read the opposite of the operator's last
// action. Each row is
// rebuilt through core.NewAcknowledgement, so a row that no longer validates
// is an error. core.Seen reads them in this order.
func (s *Store) Acknowledgements(ctx context.Context, key core.AgentKey) ([]core.Acknowledgement, error) {
	epoch, err := toInt64("epoch", key.Epoch)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, finished_since, seen, observed_at, herdr_order, source
		FROM acknowledgements WHERE host = ? AND server = ? AND epoch = ? AND pane_id = ?
		ORDER BY id`, key.Host, key.Server, epoch, key.PaneID)
	if err != nil {
		return nil, fmt.Errorf("store: acknowledgements of %s: %w", key.PaneID, err)
	}
	defer rows.Close()
	var out []core.Acknowledgement
	for rows.Next() {
		var id, since, observedAt int64
		var seen bool
		var order sql.NullInt64
		var source string
		if err := rows.Scan(&id, &since, &seen, &observedAt, &order, &source); err != nil {
			return nil, fmt.Errorf("store: acknowledgements of %s: %w", key.PaneID, err)
		}
		in := core.AcknowledgementInput{Key: key, FinishedSince: since, Seen: seen, ObservedAt: observedAt,
			Source: core.AckSource(source)}
		if order.Valid {
			if order.Int64 < 0 {
				return nil, fmt.Errorf("store: acknowledgements of %s: row %d: negative herdr order %d", key.PaneID, id, order.Int64)
			}
			o := uint64(order.Int64)
			in.HerdrOrder = &o
		}
		a, err := core.NewAcknowledgement(in)
		if err != nil {
			return nil, fmt.Errorf("store: acknowledgements of %s: row %d: %w", key.PaneID, id, err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: acknowledgements of %s: %w", key.PaneID, err)
	}
	return out, nil
}
