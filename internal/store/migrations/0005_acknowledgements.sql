-- 0005: acknowledgements (ADR-0014, Story 6.1). The operator marks one exact
-- finish of one Agent seen: its AgentKey and the instant its Finished class
-- began (core.FoldResult.Since, the view's waiting_since). Like transitions,
-- the table is append-only: rows are inserted and never updated, an undo is a
-- further row with seen = 0, and the Retention sweep is the only removal.
-- Whether a finish is seen is derived: the latest row, in recording order
-- (id, never observed_at, which a stepped clock can reorder), for the key and the fold's current Since decides
-- (core.Seen). As with any migration, a binary that knows only schema 4
-- (0.7.x and earlier) refuses a database migrated to 5.

CREATE TABLE acknowledgements (
    id             INTEGER PRIMARY KEY,                         -- recording order
    host           TEXT    NOT NULL,
    server         TEXT    NOT NULL,
    epoch          INTEGER NOT NULL CHECK (epoch >= 1),         -- never backfill's epoch 0
    pane_id        TEXT    NOT NULL,
    finished_since INTEGER NOT NULL CHECK (finished_since > 0), -- the finish named, UTC ms
    seen           INTEGER NOT NULL CHECK (seen IN (0, 1)),     -- 1 acknowledged, 0 undone
    observed_at    INTEGER NOT NULL                             -- the Daemon's clock, UTC ms
);
CREATE INDEX acknowledgements_agent ON acknowledgements (host, server, epoch, pane_id, finished_since, id);
CREATE INDEX acknowledgements_observed_at ON acknowledgements (observed_at);
