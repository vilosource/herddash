-- 0002: the engine's per-server state (AD-3), so a Daemon restart applies the
-- epoch rule against what the previous run last saw instead of starting again
-- at epoch 1. This is engine state, not the record: one row per Host and
-- Server, upserted. It touches neither transitions nor resync_markers.

CREATE TABLE server_state (
    host      TEXT    NOT NULL,
    server    TEXT    NOT NULL,
    -- The epoch in force when the row was saved; never 0, which is backfill's.
    epoch     INTEGER NOT NULL CHECK (epoch >= 1),
    -- A JSON object from pane id to terminal_id, as the latest applied
    -- snapshot showed them: the evidence the epoch rule compares.
    terminals TEXT    NOT NULL,
    PRIMARY KEY (host, server)
);
