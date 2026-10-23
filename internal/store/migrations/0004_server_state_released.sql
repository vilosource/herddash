-- 0004: the panes whose Agent herdr released (Story 5.17), so a Daemon restart
-- does not seed a released pane's next Agent from its predecessor's history.
-- This is engine state, not the record: it touches neither transitions nor
-- resync_markers. A row saved before this migration defaults to no released
-- panes, which seeds exactly as before. As with any migration, a binary that
-- knows only schema 3 (0.5.0 and earlier) refuses a database migrated to 4.

ALTER TABLE server_state ADD COLUMN released TEXT NOT NULL DEFAULT '[]';
