# ADR-0005: The stored record is append-only, keyed by Host, herdr server and pane

**Status: Accepted.** Written 2026-10-15 during the architecture run. It carries decisions the
operator confirmed in the PRD (FR-6 to FR-9a, FR-11). Exact columns and indexes are fixed by an
amendment to this record in milestone 2, once a 0.9.x schema dump shows which herdr ordering fields
exist.

## Context

The record is the reason the Board can be believed (ADR-0003). Waiting Time comes from it, the
history API serves it, and a later surface will read it. Four forces shape it:

- herdr ids are scoped to one server, and one machine can run several named sessions.
- A Profile Map edit must not rewrite the past (FR-11).
- Transitions arrive three ways, observed live, inferred from a snapshot, or imported from the
  plugin's log (FR-6, FR-9a), and they are not equally trustworthy.
- Data recorded against herdr 0.8.2 during dogfooding has known defects: stale replay, and false
  `done` before 0.9.2 (FR-16).

## Decision

- **Append-only.** Transitions and resync markers are inserted and never updated. The only delete is
  the Retention sweep (FR-9).
- **Identity** is `(host, server, pane_id)` on every row. `host` is the configured Host name,
  defaulting to the OS hostname. `server` is the herdr session name, `default` for the default
  socket.
- **Each transition stores:**
  - its identity, the Agent Program, and the Profile as resolved at that moment
  - the previous and new Status
  - herdr's ordering information where known
  - the herdr version
  - its provenance, one of `live`, `snapshot` or `backfill`
  - a possibly-spurious flag
  - `observed_at`, in UTC milliseconds
- **Resync markers** record when, for which Host and server, and why. A resync marker is how a gap
  is a recorded fact rather than an omission (FR-7).
- **Migrations** are embedded, numbered and applied at start in one transaction. The schema version
  is stored in the database. A change to the transitions table needs an ADR first.
- The database lives in an owner-only (`0700`) data directory, and the file is `0600`.

## Consequences

- History is never rewritten. A wrong Profile at the time stays wrong in the record and is
  corrected going forward by editing the map, which is what FR-11 asks.
- Consumers can discount weak evidence. Waiting Time can prefer `live` over `snapshot` over
  `backfill`, and anything flagged possibly spurious.
- Multi-Host aggregation later is additive, since every row already carries its Host and server.
- The schema is a stored-event contract. Changing it is deliberately expensive.

## Alternatives considered and rejected

- **One mutable current-state row per Agent plus a log.** Two sources of truth that can disagree.
  Current state is held in memory by the engine and rebuilt from a snapshot, so it does not need
  storing.
- **Join Profile at read time from the current map.** Rewrites history whenever the map changes.
- **Key by pane id alone, adding Host later.** Ids collide across machines and named sessions, and
  adding a key column to an existing record is a migration of exactly the kind this ADR makes
  expensive.
