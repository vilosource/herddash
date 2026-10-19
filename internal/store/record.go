package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/vilosource/herddash/internal/core"
)

// The resync marker reasons this package knows of. Story 2.2 writes only
// ReasonDaemonStart.
const (
	// ReasonDaemonStart marks a Daemon start, so downtime is in the record
	// rather than absent from it (ADR-0005).
	ReasonDaemonStart = "daemon_start"
)

// ResyncMarker is one row of resync_markers: when, for which Host, Server and
// epoch, and why the record may have a gap.
type ResyncMarker struct {
	Host   string
	Server string
	Epoch  uint64
	// ObservedAt is the Daemon's clock, in UTC Unix milliseconds.
	ObservedAt int64
	Reason     string
	// Detail says more about the reason where there is any; "" is stored as
	// NULL.
	Detail string
}

// ServerState is the engine's state for one Host and Server that must
// survive a Daemon restart (AD-3): the epoch in force and every pane's
// terminal_id in the latest applied snapshot.
type ServerState struct {
	Epoch     uint64
	Terminals map[string]string
}

// Append inserts one Transition into the record. Every accessor maps to its
// column; an unknown value (no previous Status, no herdr order, an empty Agent
// Program, Profile, herdr version or import key) is NULL, never a placeholder.
// possibly_spurious is left at its default, 0. Rows are never updated.
func (s *Store) Append(t core.Transition) error {
	k := t.Key()
	epoch, err := toInt64("epoch", k.Epoch)
	if err != nil {
		return err
	}
	var previous, order any
	if p, ok := t.Previous(); ok {
		previous = string(p)
	}
	if o, ok := t.HerdrOrder(); ok {
		if order, err = toInt64("state_change_seq", o); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`INSERT INTO transitions
		(host, server, epoch, pane_id, agent_program, profile, previous_status, new_status,
		 state_change_seq, herdr_version, provenance, observed_at, import_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.Host, k.Server, epoch, k.PaneID, nullable(string(t.AgentProgram())), nullable(t.Profile()), previous, string(t.New()),
		order, nullable(t.HerdrVersion()), string(t.Provenance()), t.ObservedAt(), nullable(t.ImportKey()))
	if err != nil {
		return fmt.Errorf("store: append transition for %s: %w", k.PaneID, err)
	}
	return nil
}

// AppendResyncMarker inserts one resync marker. Rows are never updated.
func (s *Store) AppendResyncMarker(m ResyncMarker) error {
	epoch, err := toInt64("epoch", m.Epoch)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO resync_markers (host, server, epoch, observed_at, reason, detail)
		VALUES (?, ?, ?, ?, ?, ?)`, m.Host, m.Server, epoch, m.ObservedAt, m.Reason, nullable(m.Detail))
	if err != nil {
		return fmt.Errorf("store: append resync marker %s: %w", m.Reason, err)
	}
	return nil
}

// History returns one Agent's Transitions in recording order: by observed_at,
// then by id, the order they were appended. herdr's state_change_seq is not
// used to order them; it exists only on snapshot rows and only within one
// herdr instance (ADR-0005's amendment). Each row is rebuilt through
// core.NewTransition, so a row that no longer validates is an error.
func (s *Store) History(ctx context.Context, key core.AgentKey) ([]core.Transition, error) {
	epoch, err := toInt64("epoch", key.Epoch)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, agent_program, profile, previous_status, new_status,
		state_change_seq, herdr_version, provenance, observed_at, import_key
		FROM transitions WHERE host = ? AND server = ? AND epoch = ? AND pane_id = ?
		ORDER BY observed_at, id`, key.Host, key.Server, epoch, key.PaneID)
	if err != nil {
		return nil, fmt.Errorf("store: history of %s: %w", key.PaneID, err)
	}
	defer rows.Close()
	var out []core.Transition
	for rows.Next() {
		var (
			id                                        int64
			program, profile, previous, version, ikey sql.NullString
			next, prov                                string
			order                                     sql.NullInt64
			observedAt                                int64
		)
		if err := rows.Scan(&id, &program, &profile, &previous, &next, &order, &version, &prov, &observedAt, &ikey); err != nil {
			return nil, fmt.Errorf("store: history of %s: %w", key.PaneID, err)
		}
		in := core.TransitionInput{
			Key:          key,
			AgentProgram: core.AgentProgram(program.String),
			Profile:      profile.String,
			New:          core.Status(next),
			HerdrVersion: version.String,
			ObservedAt:   observedAt,
			Provenance:   core.Provenance(prov),
			ImportKey:    ikey.String,
		}
		if previous.Valid {
			p := core.Status(previous.String)
			in.Previous = &p
		}
		if order.Valid {
			if order.Int64 < 0 {
				return nil, fmt.Errorf("store: history of %s: row %d has state_change_seq %d", key.PaneID, id, order.Int64)
			}
			o := uint64(order.Int64)
			in.HerdrOrder = &o
		}
		t, err := core.NewTransition(in)
		if err != nil {
			return nil, fmt.Errorf("store: history of %s: row %d: %w", key.PaneID, id, err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: history of %s: %w", key.PaneID, err)
	}
	return out, nil
}

// LoadServerState returns the state last saved for host and server, and false
// when none has been.
func (s *Store) LoadServerState(host, server string) (ServerState, bool, error) {
	var epoch int64
	var terminals string
	err := s.db.QueryRow(`SELECT epoch, terminals FROM server_state WHERE host = ? AND server = ?`, host, server).
		Scan(&epoch, &terminals)
	if errors.Is(err, sql.ErrNoRows) {
		return ServerState{}, false, nil
	}
	if err != nil {
		return ServerState{}, false, fmt.Errorf("store: load server state: %w", err)
	}
	if epoch < 1 {
		return ServerState{}, false, fmt.Errorf("store: load server state: epoch %d", epoch)
	}
	st := ServerState{Epoch: uint64(epoch)}
	if err := json.Unmarshal([]byte(terminals), &st.Terminals); err != nil {
		return ServerState{}, false, fmt.Errorf("store: load server state: terminals: %w", err)
	}
	if st.Terminals == nil {
		st.Terminals = map[string]string{}
	}
	return st, true, nil
}

// SaveServerState replaces the state saved for host and server. It is engine
// state, not the record, so unlike the record it is overwritten.
func (s *Store) SaveServerState(host, server string, st ServerState) error {
	epoch, err := toInt64("epoch", st.Epoch)
	if err != nil {
		return err
	}
	terminals := st.Terminals
	if terminals == nil {
		terminals = map[string]string{}
	}
	b, err := json.Marshal(terminals)
	if err != nil {
		return fmt.Errorf("store: save server state: %w", err)
	}
	_, err = s.db.Exec(`INSERT INTO server_state (host, server, epoch, terminals) VALUES (?, ?, ?, ?)
		ON CONFLICT (host, server) DO UPDATE SET epoch = excluded.epoch, terminals = excluded.terminals`,
		host, server, epoch, string(b))
	if err != nil {
		return fmt.Errorf("store: save server state: %w", err)
	}
	return nil
}

// nullable is NULL for "", the value otherwise.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// toInt64 converts an unsigned value for an INTEGER column, which SQLite
// holds as a signed 64-bit integer.
func toInt64(what string, v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("store: %s %d does not fit an SQLite integer", what, v)
	}
	return int64(v), nil
}
