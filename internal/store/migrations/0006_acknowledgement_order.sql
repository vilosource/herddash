-- 0006: herdr's order on an acknowledgement (ADR-0014, amended 2026-10-27).
-- herdr_order is herdr's state_change_seq for the Agent's Status when the
-- operator marked it, as a snapshot read it, NULL when unknown (a Status last
-- learnt live, every row recorded under schema 5). It lets a restart notice a
-- whole turn the Daemon missed, which ends with the same Status and fold
-- start but a higher order (core.SeenAt). Rows recorded before this migration
-- keep NULL, so they are judged as before. The table stays append-only. A
-- binary that knows only schema 5 refuses a database migrated to 6.

ALTER TABLE acknowledgements ADD COLUMN herdr_order INTEGER NULL CHECK (herdr_order IS NULL OR herdr_order >= 0);
