# ADR-0005: The stored record is append-only, keyed by Host, Server, server epoch and pane

**Status: Accepted.** Written 2026-10-15 during the architecture run, under the operator's
delegation, from decisions the operator confirmed in the PRD (FR-6 to FR-9a, FR-11). This record
fixes the invariants of the stored record. Its exact columns and indexes are architecture seed,
owned by the migrations once milestone 2 writes them.

## Context

The record is why the Board can be believed (ADR-0003). Waiting Time comes from it, the history API
serves it, and a later surface will read it. Five forces shape it:

- herdr ids are scoped to one server, and one machine can run several named sessions.
- Whether herdr keeps pane ids across its own restart is unverified, so a reused id could join two
  Agents' histories.
- A Profile Map edit must not rewrite the past (FR-11).
- Transitions arrive three ways, observed live, inferred from a snapshot, or imported from the
  plugin's log (FR-6, FR-9a), and they are not equally trustworthy.
- Data recorded against herdr 0.8.2 while dogfooding has known defects (FR-16).

## Decision

- **Append-only.** Transitions and resync markers are inserted and never updated.
- **Retention is the only delete** (FR-9). For each Agent still present, it keeps every Transition
  from the one that began its current Attention Class onward, plus its latest `working`
  Transition, because the current class and its start instant depend on them. Agents no longer
  present are swept purely by age.
- **Identity on every row** is Host, Server (the herdr session name), server epoch and pane id.
  The epoch changes only on evidence that herdr reassigned pane ids, so a reused pane id starts a
  new Agent rather than continuing an old one. Epoch 0 is reserved for backfilled rows, which are
  history only and never join a live Agent.
- **Each Transition stores:**
  - its identity, the Agent Program, and the Profile as resolved at that moment
  - previous and new Status
  - herdr's ordering where known, and the herdr version
  - its provenance, one of `live`, `snapshot` or `backfill`
  - a possibly-spurious flag
  - `observed_at`, in UTC milliseconds
  - a unique `import_key` for imported rows only
- **Unknown values are NULL**, never a placeholder string. `unknown` is a real herdr Status.
- **Resync markers** record when, for which Host, Server and epoch, and why. Each Daemon start is
  recorded with reason `daemon_start`, so downtime is in the record rather than absent from it.
- **Migrations** are embedded and numbered, applied at start in one transaction. A database newer
  than the binary is refused. A change to the transitions table needs an ADR first.
- **The database file is `0600`** in a `0700` data directory.

## Consequences

- History is never rewritten. A Profile that was wrong at the time stays wrong in the record and is
  corrected going forward by editing the map, which is what FR-11 asks.
- Consumers can weigh evidence. Waiting Time is exact only from a `live` start (architecture spine
  AD-12). Backfilled rows, under epoch 0, never set a live Agent's Waiting Time.
- Multi-Host aggregation later is additive, because every row already carries Host and Server.
- If milestone 1 shows herdr keeps pane ids across restarts, the epoch still costs nothing: it
  simply changes less often.

## Alternatives considered and rejected

- **A mutable current-state row per Agent beside a log.** Two sources of truth. Current state lives
  in memory and is rebuilt from the record and a snapshot.
- **Joining Profile at read time from the current map.** That rewrites history whenever the map
  changes.
- **Keying by pane id alone, adding Host later.** Ids collide across machines and sessions, and
  adding a key column later is exactly the kind of migration this record makes expensive.
