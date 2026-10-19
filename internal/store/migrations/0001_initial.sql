-- 0001: the record (ADR-0005, AD-9, AD-10). Append-only: rows are inserted and
-- never updated; Retention is the only delete. Unknown values are NULL, never a
-- placeholder string, because "unknown" is a real herdr Status. Changing the
-- transitions table needs an ADR first.

CREATE TABLE transitions (
    -- The recording order.
    id                INTEGER PRIMARY KEY,
    -- Identity: AgentKey{Host, Server, Epoch, PaneID}. Epoch 0 is backfill only.
    host              TEXT    NOT NULL,
    server            TEXT    NOT NULL,
    epoch             INTEGER NOT NULL,
    pane_id           TEXT    NOT NULL,
    agent_program     TEXT    NULL,
    -- The Profile as resolved at that moment; a Profile Map edit never rewrites it.
    profile           TEXT    NULL,
    previous_status   TEXT    NULL,
    new_status        TEXT    NOT NULL,
    -- herdr's per-server state_change_seq: snapshot Transitions only, compared
    -- only within one server instance.
    state_change_seq  INTEGER NULL,
    herdr_version     TEXT    NULL,
    provenance        TEXT    NOT NULL CHECK (provenance IN ('live', 'snapshot', 'backfill')),
    possibly_spurious INTEGER NOT NULL DEFAULT 0 CHECK (possibly_spurious IN (0, 1)),
    -- UTC Unix milliseconds.
    observed_at       INTEGER NOT NULL,
    -- Imported rows only.
    import_key        TEXT    NULL UNIQUE,
    -- Epoch 0 is backfill only, and every backfilled row has epoch 0.
    CHECK ((epoch = 0) = (provenance = 'backfill')),
    -- import_key is set exactly on imported rows.
    CHECK (import_key IS NULL OR provenance = 'backfill'),
    CHECK (provenance <> 'backfill' OR import_key IS NOT NULL),
    -- state_change_seq is only ever known from a snapshot.
    CHECK (state_change_seq IS NULL OR provenance = 'snapshot')
);

CREATE INDEX transitions_agent ON transitions (host, server, epoch, pane_id, observed_at, id);
-- The Retention sweep by age.
CREATE INDEX transitions_observed_at ON transitions (observed_at);

CREATE TABLE resync_markers (
    id          INTEGER PRIMARY KEY,
    host        TEXT    NOT NULL,
    server      TEXT    NOT NULL,
    epoch       INTEGER NOT NULL,
    -- UTC Unix milliseconds.
    observed_at INTEGER NOT NULL,
    reason      TEXT    NOT NULL,
    -- More about the reason where there is any, e.g. an unknown error code.
    detail      TEXT    NULL
);

CREATE INDEX resync_markers_observed_at ON resync_markers (observed_at);
