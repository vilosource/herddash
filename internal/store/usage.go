package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/vilosource/herddash/internal/core"
)

// UsageRow is one complete Response as the record keeps it (ADR-0017): the
// Response with the Agent instance that recorded it (its AgentKey and
// AgentSince, the observed_at of the key's last Transition with no previous
// Status, AD-3), that Agent's Profile and session id, and when the Daemon
// recorded it.
type UsageRow struct {
	Key        core.AgentKey
	AgentSince int64
	Profile    string
	SessionID  string
	ObservedAt int64
	core.Response
}

// insertUsage is the one INSERT of a usage row, every column bound. A
// Response already recorded, by (message_id, request_id), is ignored, so the
// row written first keeps its Agent and Profile.
const insertUsage = `INSERT OR IGNORE INTO usage
	(host, server, epoch, pane_id, agent_since, profile, session_id, subagent_id, message_id, request_id,
	 model, responded_at, input, output, cache_write_5m, cache_write_1h, cache_read, stopped, observed_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// AppendUsage inserts usage rows, in the order given, in one transaction:
// every new row is written or none is. A row whose Response the record already
// holds is skipped, not updated. None is a no-op. It returns how many rows it
// inserted. Rows are never updated; only the Retention sweep removes them.
// A row breaking a table CHECK (AgentSince 0, an empty session, message or
// request id, epoch 0, a negative At or count) fails the whole batch, so
// callers must pass only rows that satisfy them. Only the engine calls it.
func (s *Store) AppendUsage(ctx context.Context, rows []UsageRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: append usage: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a no-op after Commit
	stmt, err := tx.PrepareContext(ctx, insertUsage)
	if err != nil {
		return 0, fmt.Errorf("store: append usage: %w", err)
	}
	defer stmt.Close()
	var inserted int64
	for _, r := range rows {
		epoch, err := toInt64("epoch", r.Key.Epoch)
		if err != nil {
			return 0, err
		}
		stopped := 0
		if r.Stopped {
			stopped = 1
		}
		res, err := stmt.ExecContext(ctx, r.Key.Host, r.Key.Server, epoch, r.Key.PaneID, r.AgentSince, r.Profile,
			r.SessionID, r.SubagentID, r.MessageID, r.RequestID, r.Model, r.At,
			r.Input, r.Output, r.CacheWrite5m, r.CacheWrite1h, r.CacheRead, stopped, r.ObservedAt)
		if err != nil {
			return 0, fmt.Errorf("store: append usage for %s, response %s/%s: %w", r.Key.PaneID, r.MessageID, r.RequestID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: append usage: %w", err)
		}
		inserted += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: append usage: %w", err)
	}
	return inserted, nil
}

// UsageTotals is one Agent instance's usage summed: its token counts, with
// CacheWrite the 5-minute and 1-hour writes together, how many Responses, and
// the latest Response's time (UTC ms, 0 with none).
type UsageTotals struct {
	Input          int64
	Output         int64
	CacheWrite     int64
	CacheRead      int64
	Responses      int64
	LastResponseAt int64
}

// UsageTotals sums the usage rows of one Agent instance: key and the
// agentSince that began it. With none it returns the zero value.
func (s *Store) UsageTotals(ctx context.Context, key core.AgentKey, agentSince int64) (UsageTotals, error) {
	epoch, err := toInt64("epoch", key.Epoch)
	if err != nil {
		return UsageTotals{}, err
	}
	var t UsageTotals
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(input), 0), coalesce(sum(output), 0),
		coalesce(sum(cache_write_5m + cache_write_1h), 0), coalesce(sum(cache_read), 0), count(*),
		coalesce(max(responded_at), 0)
		FROM usage WHERE host = ? AND server = ? AND epoch = ? AND pane_id = ? AND agent_since = ?`,
		key.Host, key.Server, epoch, key.PaneID, agentSince).Scan(
		&t.Input, &t.Output, &t.CacheWrite, &t.CacheRead, &t.Responses, &t.LastResponseAt); err != nil {
		return UsageTotals{}, fmt.Errorf("store: usage totals of %s: %w", key.PaneID, err)
	}
	return t, nil
}

// UsageBy is what UsageHourly groups each hour's usage by.
type UsageBy string

const (
	// UsageByProfile groups by the Profile that recorded each Response.
	UsageByProfile UsageBy = "profile"
	// UsageByModel groups by model.
	UsageByModel UsageBy = "model"
	// UsageByAgent groups by Agent instance: AgentKey and agent_since.
	UsageByAgent UsageBy = "agent"
)

// UsageBucket is one hour's usage for one key. Start is the hour's first
// millisecond, UTC. Only the fields of the grouping are set: Profile for
// UsageByProfile, Model for UsageByModel, Agent and AgentSince for
// UsageByAgent. CacheWrite is the 5-minute and 1-hour writes together.
type UsageBucket struct {
	Start      int64
	Profile    string
	Model      string
	Agent      core.AgentKey
	AgentSince int64
	Input      int64
	Output     int64
	CacheWrite int64
	CacheRead  int64
	Responses  int64
}

// ErrTooManyBuckets is UsageHourly's answer when the range holds more buckets
// than the caller's limit. No truncated list is returned.
var ErrTooManyBuckets = errors.New("store: usage range holds too many buckets")

// hourMs is one hour in milliseconds; a Response's hour starts at
// responded_at - responded_at % hourMs.
const hourMs = 3600000

// usageGroupColumns are the columns each grouping keys a bucket by, in order.
var usageGroupColumns = map[UsageBy]string{
	UsageByProfile: "profile",
	UsageByModel:   "model",
	UsageByAgent:   "host, server, epoch, pane_id, agent_since",
}

// UsageHourly sums the usage of every Response with from <= responded_at < to
// (UTC ms) into one bucket per hour and key of by, ordered by Start and then
// by key. More than limit buckets is ErrTooManyBuckets.
func (s *Store) UsageHourly(ctx context.Context, from, to int64, by UsageBy, limit int) ([]UsageBucket, error) {
	cols, ok := usageGroupColumns[by]
	if !ok {
		return nil, fmt.Errorf("store: usage hourly: unknown grouping %q", by)
	}
	if limit <= 0 {
		return nil, fmt.Errorf("store: usage hourly: limit %d, want at least 1", limit)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT responded_at - responded_at % `+fmt.Sprint(hourMs)+` AS start, `+cols+`,
		sum(input), sum(output), sum(cache_write_5m + cache_write_1h), sum(cache_read), count(*)
		FROM usage WHERE responded_at >= ? AND responded_at < ?
		GROUP BY start, `+cols+` ORDER BY start, `+cols+` LIMIT ?`, from, to, limit+1)
	if err != nil {
		return nil, fmt.Errorf("store: usage hourly: %w", err)
	}
	defer rows.Close()
	var out []UsageBucket
	for rows.Next() {
		if len(out) == limit {
			return nil, ErrTooManyBuckets
		}
		var b UsageBucket
		sums := []any{&b.Input, &b.Output, &b.CacheWrite, &b.CacheRead, &b.Responses}
		var err error
		switch by {
		case UsageByProfile:
			err = rows.Scan(append([]any{&b.Start, &b.Profile}, sums...)...)
		case UsageByModel:
			err = rows.Scan(append([]any{&b.Start, &b.Model}, sums...)...)
		case UsageByAgent:
			var epoch int64
			err = rows.Scan(append([]any{&b.Start, &b.Agent.Host, &b.Agent.Server, &epoch, &b.Agent.PaneID, &b.AgentSince}, sums...)...)
			if err == nil && epoch < 1 {
				err = fmt.Errorf("epoch %d", epoch)
			}
			b.Agent.Epoch = uint64(epoch)
		}
		if err != nil {
			return nil, fmt.Errorf("store: usage hourly: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: usage hourly: %w", err)
	}
	return out, nil
}
