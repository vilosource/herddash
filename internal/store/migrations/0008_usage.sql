-- 0008: token usage (ADR-0017, Story 8.3). One row per complete model
-- Response of a Claude Code Agent: the transcript records sharing a message id
-- and request id, at their final counts. The pair is the Response's own
-- identity, so a Response seen again (a continued session, a restart, the
-- history pass, a subagent replay) is one row, and it keeps the Agent and
-- Profile that first recorded it. An Agent instance is its AgentKey and
-- agent_since, the observed_at of the key's last Transition with no previous
-- Status (AD-3). Like transitions, the table is append-only: rows are inserted
-- once, complete, and never updated; the Retention sweep is the only removal,
-- keyed by responded_at, the Response's own time. No message text is kept. As
-- with any migration, a binary that knows only schema 7 (0.10.x and earlier)
-- refuses a database migrated to 8.

CREATE TABLE usage (
    id             INTEGER PRIMARY KEY,                           -- recording order
    host           TEXT    NOT NULL CHECK (host <> ''),
    server         TEXT    NOT NULL CHECK (server <> ''),
    epoch          INTEGER NOT NULL CHECK (epoch >= 1),           -- never backfill's epoch 0
    pane_id        TEXT    NOT NULL CHECK (pane_id <> ''),
    agent_since    INTEGER NOT NULL CHECK (agent_since > 0),      -- the Agent instance's start, UTC ms
    profile        TEXT    NOT NULL,
    session_id     TEXT    NOT NULL CHECK (session_id <> ''),
    subagent_id    TEXT    NOT NULL DEFAULT '',                   -- '' on the main thread
    message_id     TEXT    NOT NULL CHECK (message_id <> ''),
    request_id     TEXT    NOT NULL CHECK (request_id <> ''),
    model          TEXT    NOT NULL,
    responded_at   INTEGER NOT NULL CHECK (responded_at >= 0),    -- the Response's record time, UTC ms
    input          INTEGER NOT NULL CHECK (input >= 0),
    output         INTEGER NOT NULL CHECK (output >= 0),
    cache_write_5m INTEGER NOT NULL CHECK (cache_write_5m >= 0),
    cache_write_1h INTEGER NOT NULL CHECK (cache_write_1h >= 0),
    cache_read     INTEGER NOT NULL CHECK (cache_read >= 0),
    stopped        INTEGER NOT NULL CHECK (stopped IN (0, 1)),    -- 1 when its record had a stop reason
    observed_at    INTEGER NOT NULL,                              -- the Daemon's clock, UTC ms
    UNIQUE (message_id, request_id)
);
CREATE INDEX usage_agent ON usage (host, server, epoch, pane_id, agent_since);
CREATE INDEX usage_responded_at ON usage (responded_at);
CREATE INDEX usage_profile ON usage (profile, responded_at);
