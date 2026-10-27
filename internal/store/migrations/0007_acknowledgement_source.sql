-- 0007: where an acknowledgement came from (ADR-0015). 'board' is the
-- operator's click on the Board, a mark or an undo; 'herdr_focus' is the
-- Daemon's mark when the operator left a Finished Agent's pane in herdr after
-- reading it. Every row recorded before this migration is a Board mark or
-- undo, so 'board' is their truthful source. The table stays append-only. A
-- binary that knows only schema 6 refuses a database migrated to 7.

ALTER TABLE acknowledgements ADD COLUMN source TEXT NOT NULL DEFAULT 'board'
    CHECK (source IN ('board', 'herdr_focus'));
