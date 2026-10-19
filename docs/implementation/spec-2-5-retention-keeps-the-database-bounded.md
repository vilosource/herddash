---
title: 'Story 2.5: Retention keeps the database bounded'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: '46008e42dc57a38816be2b9f03ffc835b0e5096d'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-2-context.md'
  - '{project-root}/docs/implementation/spec-2-4-waiting-time-that-survives-restarts.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The record grows forever. `retention_days` is configured but nothing deletes
anything (FR-9, AD-4).

**Approach:** An engine timer runs a store sweep. The sweep deletes rows older than Retention.
For each Agent still present, it keeps every Transition from the instant its current Attention
Class began, plus its latest `working` Transition. Resync markers are swept by age alone.

## Boundaries & Constraints

**Always:**
- **Store:** `(*Store).Sweep(ctx, cutoff int64, present []store.Present) (store.SweepResult, error)`.
  - `Present{Key core.AgentKey; Since int64}`, where `Since` is the fold's class start, or 0 when
    unknown, which keeps everything.
  - In one transaction it deletes `transitions` rows with `observed_at < cutoff`, except rows of a
    present key with `observed_at >= Since`, and that key's row with the highest `id` among its
    `new_status = 'working'` rows.
  - In the same transaction it deletes `resync_markers` rows with `observed_at < cutoff`.
  - It returns the count of each.
  - It is the only `DELETE` in non-test code, which a test enforces.
- **Engine:**
  - `Options.RetentionDays int`. 0 means no sweep, so Epic 1 tests are unaffected. serve passes
    `cfg.RetentionDays()`.
  - A timer every `retentionSweepInterval` (tunable, default 1 h) runs one sweep on the engine
    goroutine. The first sweep comes when the first connection becomes ready, then every interval.
  - `cutoff = now − RetentionDays×24h` in ms.
  - `present` is every reported Agent in the current epoch, with its fold's `Since`.
  - **Skipped**, and the next tick tries again, when there is no store, while not connected and
    ready (the present set is not known), and while a resync is running or scheduled.
  - It logs the counts at info, and an error at error level. It never stops the engine.
- Backfilled rows (epoch 0) are never present, so they go by age alone.
- Only the engine calls `Sweep`. Add it to the call-site test's guarded set.

**Never:**
- Delete anything else.
- Update rows.
- Change the schema.
- Run a sweep during a resync.
- Touch a live herdr.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Old rows, absent Agent | Agent gone; rows 100 days old; N=90 | all its rows deleted | none |
| Present, class kept | present Agent; class began 120 days ago (blocked); older rows before it | rows from the class start onward kept; older ones deleted | none |
| Latest working kept | present idle Agent; last `working` row 200 days old; N=90 | that one working row kept (so `idle` stays Finished); other old rows deleted | none |
| Recent rows | rows newer than the cutoff | untouched | none |
| Markers | markers 100 and 10 days old | the 100-day one deleted | none |
| Backfill | epoch-0 rows older than the cutoff | deleted | none |
| Unknown since | present Agent with Since 0 | none of its rows deleted | none |
| During resync | sweep tick while a resync runs | skipped; runs at the next tick | none |
| Disconnected | tick while reconnecting | skipped | none |
| Disabled | RetentionDays 0 | never sweeps | none |
| Fold intact | after a sweep, History plus Fold for a present Agent | same Class, Since and EverWorking as before | none |
| Only delete | another non-test `DELETE` added | the enforcing test fails | test |

</frozen-after-approval>

## Code Map

- `internal/store/record.go` (or a new `sweep.go`) -- `Present`, `SweepResult`, `Sweep`, and tests against temp files. Seed rows with `Append`.
- `internal/engine/` -- the `Recorder` interface gains `Sweep`. Add the timer in a new `retention.go` or in `engine.go`, tagged with `conn` like the reconcile timer. Present Agents come from the `panes` map (`known`, `fold.Since`). Add the tunable to `tunables.go`. Update the fake store.
- `cmd/herddash/serve.go` -- pass `RetentionDays`.
- `callsite_test.go` -- add `Sweep` to the guarded methods. Add a test asserting that `DELETE` (case-insensitive, in a Go string or a `.sql` file) appears only in the sweep's source file.

## Tasks & Acceptance

**Execution:**
- [x] `internal/store/*` -- Sweep and tests
- [x] `internal/engine/*` -- the timer, skip rules, present set, tests
- [x] `cmd/herddash/serve.go` -- wiring
- [x] `callsite_test.go` -- guards

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes and earlier tests still pass.

## Implementation Notes

- **Store.** `Sweep` lives in `internal/store/sweep.go` with `Present` and
  `SweepResult{Transitions, ResyncMarkers}`. The transitions delete is one statement: the present
  set is passed as a JSON array and read with `json_each`, so there is no temporary table and no
  schema change. A present key keeps rows with `observed_at >= Since` (all of them for `Since` 0)
  and its highest-id `working` row; the key match is the whole `AgentKey`, so backfill (epoch 0)
  and other epochs, Hosts or Servers go by age. Both deletes run in one transaction; a failure
  rolls both back.
- **Timer.** `retentionSweepInterval` (default 1 h) in `tunables.go`; the timer and sweep are in
  `internal/engine/retention.go`. The timer is **run-wide, not tagged with `conn`** (the Code Map
  suggested a `conn` tag like the reconcile timer): it starts when the run's first connection
  becomes ready, after that snapshot is applied, and survives reconnects, so that a tick while
  reconnecting exists to be skipped, as the matrix requires. A later reconnect does not sweep by
  itself; the next tick does.
- **Present set.** Every pane tracked under the current epoch. A reported Agent with a known fold
  carries `fold.Since`. **A pane not yet reported (its Profile still resolving) is present with
  `Since` 0**, which keeps all its rows. The spec says "every reported Agent", but the first
  sweep runs at the first ready snapshot, before Profile resolution finishes. Leaving those
  panes out would delete the history their first sight is about to read, and the Waiting Time
  would reset (FR-8). This keeps more rows, never fewer (`TestRetentionPendingFirstSightIsPresent`).
  A pane whose History read or Transition Append failed is marked `unrecorded` and is also present
  with `Since` 0: its fold may start later than the class-start row the record holds.
- **Skips** are logged at debug as `retention sweep skipped`. A sweep logs
  `retention sweep` at info with `cutoff`, `present`, `transitions` and `resync_markers`, and a
  failure logs `retention sweep failed` at error.
- **Guards.** `callsite_test.go` guards `Sweep` with its own permitted set, `internal/engine` and
  `internal/store`, so the importer may append but not sweep. `TestOnlyDelete` finds `DELETE`
  (case-insensitive, as a word) in non-test Go string literals and in `.sql` files outside
  `internal/store/sweep.go`. It strips SQL comments first, because `0001_initial.sql`'s header
  comment says "Retention is the only delete". Fixtures are under `testdata/callsite` and
  `testdata/deletes`.
- **serve.** passes `cfg.RetentionDays()`; `TestServeSweepsRetention` proves it end to end against
  a real database.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | VG, BH | The rollback test fails at `BeginTx` and never exercises a rollback, nor checks the marker | medium | VG pre-verified: running the two deletes outside a transaction passes every test; patch: a trigger-forced failure of the second delete |
| 2 | ECH | A pane whose fold is not backed by the record (History read or Append failed) passes a `Since` that can sweep its stored class-start row | low | Needs a prior store error; direct per-pane flag; patch |
| 3 | ECH | A sweep interrupted by shutdown logs an error | low | Direct; patch |
| 4 | ECH, BH | A large sweep runs on the engine goroutine with no deadline | low | The largest foreseeable first sweep follows Story 2.6's import of about 3,000 rows; a timeout would roll a large sweep back forever; rejected |
| 5 | ECH, BH | `TestOnlyDelete` strips `--` inside SQL literals, misses assembled SQL and can false-positive on prose | low | A guard against accidents, not adversaries; rejected |
| 6 | BH | `TestRetentionFirstSweepAtReady` ordering can pass vacuously | low | Direct; patch |
| 7 | BH | Nothing guards `UPDATE`, `REPLACE` or cascades | low | `server_state` legitimately upserts; append-only rows are guarded by the store API; rejected |
| 8 | BH | The `Sweep` call-site guard matches any `.Sweep` | low | No other `Sweep` exists; rejected |
| 9 | BH | Rows are bounded, not the file (no vacuum) | low | Freed pages are reused at steady state; rejected |
| 10 | BH | `TestServeSweepsRetention` depends on slog key order and never checks markers in the database | low | Direct; patch |
| 11 | BH | A Story 2.6 vfkb entry rides in this commit with machine details | low | The vfkb journal is committed with each change by convention; its content (a plugin path, a PID, profile names already in the docs) is not sensitive; rejected |
| 12 | BH | Overlong `Recorder` comment; Retention not logged at start | low | Direct; patch |
| 13 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass

- **CI fix after review (2026-10-19).** `TestServeTwice` failed on Linux CI: after serve
  exited, reopening the database got `SQLITE_BUSY`. Likely cause, consistent with but not
  reproduced on macOS: `Sweep` (and `History`) ran under the run context, which shutdown
  cancels; `database/sql` rolls back a cancelled transaction in the background, after
  `Store.Close` has returned, so the connection briefly kept SQLite's lock. Both now run under
  `context.WithoutCancel(runCtx)`, so they finish before `Run` returns.
