---
title: 'Story 2.2: Record every Transition'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: 'f5710dbf8dcd45e94e8723b5a0c821580cfcf8b3'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-2-context.md'
  - '{project-root}/docs/implementation/spec-2-1-a-local-database-herddash-owns.md'
  - '{project-root}/docs/adr/0005-append-only-transition-record.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The engine only prints what it sees; when serve stops, it is gone. Its epoch also
lives in memory. A Daemon restart starts again at epoch 1, so new rows would be indistinguishable
from the previous run's, even for a different herdr instance (FR-6, FR-11, AD-3 hazard 3, AD-4,
AD-13).

**Approach:** The engine writes every Transition through `store.Append` at the moment it observes
it, writes a `daemon_start` resync marker when it starts, and persists the epoch and the
pane-to-`terminal_id` map. The first snapshot after a Daemon start then applies Story 1.6's epoch
rule against what was persisted.

## Boundaries & Constraints

**Always:**
- **Store writes:**
  - `(*Store).Append(core.Transition) error` maps every accessor to its 0001 column. Unknown
    values are NULL: no previous Status, no `HerdrOrder`, empty Agent Program, Profile, herdr
    version or import key. `possibly_spurious` stays 0 here.
  - `(*Store).AppendResyncMarker(store.ResyncMarker{Host, Server, Epoch, ObservedAt, Reason, Detail}) error`.
- **Store reads:** `(*Store).History(ctx, core.AgentKey) ([]core.Transition, error)` returns one
  Agent's rows ordered by `observed_at`, then `id`. Rows are rebuilt through `core.NewTransition`.
  This is the binding spine rule. The epic's "herdr's order" wording predates the Story 1.3
  finding: `state_change_seq` exists only on snapshot rows and only within one herdr instance.
- **Epoch persistence:** migration `0002` adds `server_state(host, server, epoch, terminals)`.
  `terminals` is a JSON object from pane id to `terminal_id`, with primary key (host, server). It
  is engine state, not the record, so it is upserted. The store adds `LoadServerState` and
  `SaveServerState`.
- **Engine:**
  - `Options.Store` takes a narrow interface with `Append`, `AppendResyncMarker`,
    `LoadServerState` and `SaveServerState`. Nil means record nothing, which keeps every Epic 1
    test as it is.
  - **When `Run` starts:**
    1. Load the server state. If found, adopt its epoch and use its terminals as the
       last-applied map, with `everReady` set so the first snapshot runs `decideEpoch`. Since no
       Agents are tracked yet, nothing is removed.
    2. Write a `daemon_start` marker with the epoch now in force.
    3. Then connect.
  - Every `transition()` also calls `Append`.
  - The first sight of an Agent (`report`) is recorded as a Transition with no previous Status.
    Its provenance is `snapshot` from `apply` and `live` from a status event, so the record holds
    every Agent's current Status (FR-7 consequence 2).
  - After each applied snapshot, if the epoch or the terminal map changed, call
    `SaveServerState`.
  - A failed write is logged at error with the pane and reason, never crashes, and never blocks
    the Sink.
- **Profile:** rows carry the Profile resolved at the time. A Profile Map edit plus a restart
  changes only new rows.
- **Call sites:** a test fails if any non-test file outside `internal/engine`,
  `internal/importer` and `internal/store` calls `Append` on a `*store.Store` or the engine's
  store interface.
- **serve:** passes the opened store as `Options.Store`.

**Never:**
- Update or delete a `transitions` or `resync_markers` row.
- Write markers for other resync reasons (Story 2.3).
- Change Waiting Time, the fold, or skip a first-sight row whose Status equals the last stored one
  (Story 2.4 owns reading history back into the engine).
- Touch a live herdr.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Live change | w1:p1 working→blocked live | one row: previous working, new blocked, provenance live, state_change_seq NULL, herdr_version set, profile set | none |
| Snapshot change | snapshot shows idle with seq 7 | row provenance snapshot, state_change_seq 7 | none |
| First sight | snapshot reports a new Agent `working` | row previous NULL, new working, provenance snapshot | none |
| NULLs | no profile resolved, empty version | `profile` and `herdr_version` NULL, not "" and not "unknown" | none |
| Daemon start | Run starts | one `daemon_start` marker with the epoch in force, before any row | none |
| Restart, same herdr | persisted epoch 3 with terminals; the first snapshot keeps a terminal id | epoch stays 3; rows keyed epoch 3 | none |
| Restart, new herdr | persisted epoch 3; every terminal id differs | epoch 4; new rows keyed epoch 4 | none |
| Nothing persisted | fresh database | epoch 1 | none |
| Read order | rows with observed_at 20, 10, then 10 again | History returns 10 (lower id), 10, 20 | none |
| Profile edit | rows under `cldw`; restart with the map changed to `cldp` | old rows still `cldw`; new rows `cldp` | none |
| Write fails | store returns an error | logged at error; engine continues and still prints | none |
| Call site | a non-test file in cmd calls Append | call-site test fails | test |

</frozen-after-approval>

## Code Map

- `internal/store/store.go` and new `record.go` -- Append, AppendResyncMarker, History, ResyncMarker, Load/SaveServerState; `prepare` is unchanged.
- `internal/store/migrations/0002_server_state.sql` (new) -- touches only the new table. No ADR is needed (the transitions table is unchanged).
- `internal/store/*_test.go` -- round-trip tests and the read order; update `TestMigrationsNumbered` and the columns test for 0002.
- `internal/engine/engine.go`:
  - `Run` does the load, the marker, then `connect`.
  - `transition()` and `report()` record.
  - `apply` ends by saving the state.
  - `decideEpoch` already runs on the first snapshot when `everReady` is set.
- `internal/engine/` -- the store interface, in `client.go` or a new `recorder.go`. The tests get a fake recorder in `fake_test.go`.
- `cmd/herddash/serve.go` -- pass `st`.
- Call-site test -- beside `arch_test.go` at the module root, or in `internal/store`, scanning non-test files with go/ast.

## Tasks & Acceptance

**Execution:**
- [x] `internal/store/*` -- writes, reads, server state, migration 0002, tests
- [x] `internal/engine/*` -- recording, daemon_start, epoch load and save, tests for every engine matrix row
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- wiring; an end-to-end check that a fake-herdr run leaves rows and a marker in the database
- [x] call-site test

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes, arch_test passes and earlier stories' tests still pass.

## Implementation Notes

- **Store API.** `LoadServerState(host, server) (ServerState, bool, error)` and
  `SaveServerState(host, server, ServerState) error`, with `ServerState{Epoch, Terminals}`; the
  spec named the methods but not their signatures. `Append`, `AppendResyncMarker` and the state
  calls take no context, as the spec's `Append` signature does; `History` takes one.
  `ReasonDaemonStart` is the only reason constant (Story 2.3 adds the others).
- **Engine interface** is `engine.Recorder` in `internal/engine/recorder.go`; the test fake is
  `fakeStore`, since `recorder` was already the fake Sink's name.
- **Save before rows.** `apply` saves the state right after taking the snapshot's terminal map
  (after `decideEpoch`), before it records anything, so no row is keyed by an unpersisted epoch.
- **Load failure turns recording off** for the run (no rows, marker or saves; one error logged):
  starting at epoch 1 and saving it would overwrite a higher persisted epoch.
- **First save.** State is saved after an applied snapshot when the epoch or terminal map differs
  from what was last saved *or loaded*, and also when nothing has been saved or loaded yet in this
  run. Without that, a fresh database whose first snapshot shows no panes would never persist
  epoch 1, and rows written under it could collide with the next run's epoch 1.
- **Daemon start with saved state** sets `everReady`, so the first snapshot runs `decideEpoch`,
  which reports `Reconnected` to the Sink: serve prints a `reconnect` line at start when a
  previous run saved state. The marker carries the epoch in force before that decision (the
  persisted one).
- **First sight** carries the time its Status was first observed (stamped when a snapshot or
  status event set it, only with a Store), not the later report time after Profile resolution,
  and, when a snapshot showed it, that snapshot's `state_change_seq`. An unknown source is
  recorded as snapshot with a warning. A Status core rejects records no first sight (logged at warn).
  With no Store no clock reading is added, so Epic 1's observed_at assertions are unchanged.
- **Failed writes** log at error: `transition not recorded` (pane, error), `resync marker not
  recorded`, `server state not saved` (retried after the next snapshot), and `server state not
  loaded; recording is off for this run`.
- **Call-site test** (`callsite_test.go` at the module root, fixture `testdata/callsite`) reads
  syntax, not types: outside engine, importer and store it reports any `.Append` or
  `.AppendResyncMarker` or `.SaveServerState` selector whose left side is not an imported package name. It is broader
  than "on a `*store.Store` or the Recorder", so an unrelated `Append` method in cmd or httpapi
  would also trip it; nothing does today.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH, VG | A failed state load starts at epoch 1, then the first save overwrites the persisted epoch | high | `restore` leaves `saved` false and `saveState` upserts; patch: a load error turns recording off for the run |
| 2 | BH, ECH | The `daemon_start` marker carries the saved epoch, not the one the first snapshot decides | medium | Real; the first snapshot's decision is reported as `Reconnected`, and Story 2.3 records reconnect markers with their epoch; defer to 2.3 |
| 3 | BH, ECH | First-sight rows are written before the epoch is saved; a crash between leaves rows under an unpersisted epoch | medium | `saveState` runs at the end of `apply`; patch: save before the snapshot's first Append |
| 4 | VG | A failed save being retried is untested | medium | VG pre-verified; patch |
| 5 | ECH | A first sight reported after Profile resolution is stamped with the resolution time | medium | `report` stamps `now()`; patch: capture `observed_at` at observation |
| 6 | ECH | Status changes while unreported collapse into one first-sight row | low | By AD-14 the first Transition waits for the Profile; the first-sight row carries the latest Status, observed before any report; rejected |
| 7 | ECH | A Daemon restart on a kept epoch writes a first-sight row with no previous Status mid-history | low | The spec's Never leaves reading history back into the engine to Story 2.4; rejected |
| 8 | BH, ECH | A start with saved state prints a `reconnect` line | low | It is the epoch decision being reported, which Story 2.3 will mark; rejected |
| 9 | BH, ECH | Writes are synchronous on the engine goroutine | low | A single local SQLite insert; the spec's "never blocks" refers to failures, which are logged; rejected |
| 10 | ECH | `ORDER BY observed_at, id` misorders after a backwards clock step | false | The binding spine rule orders by `observed_at` then recording order |
| 11 | BH, ECH | The call-site checker can be fooled by a shadowed package name, or flag a `/vN` import | low | Syntax-only check; neither occurs in the module; rejected |
| 12 | BH | `SaveServerState` is not guarded by the call-site test | low | Direct addition; patch |
| 13 | BH | `firstSight` silently falls back to snapshot provenance | low | Patch with 5: warn |
| 14 | BH, ECH | `TestServeRecords` sleeps 200 ms; closes the store twice | medium | Flake risk under `-race`; patch |
| 15 | BH | Comment debris and an unwrapped doc comment | low | Patch |
| 16 | BH | No test for a first sight after Profile resolution | medium | Patch with 5 |
| 17 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
