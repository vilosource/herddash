-- 0003: who observed a Transition (ADR-0008). NULL is herdr, as every row
-- before this migration was; 'claude_session' is Claude Code's session record,
-- which records a live Transition when it says the Agent is waiting on the
-- operator and when that ends. ADD COLUMN: the append-only table is not
-- rebuilt, and existing rows are untouched (NULL).

ALTER TABLE transitions ADD COLUMN observed_by TEXT NULL
    CHECK (observed_by IS NULL OR observed_by IN ('claude_session'));
