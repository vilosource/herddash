---
title: 'Story 2.3: Gaps are recorded, not hidden'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: 'b72f1923f0f659281926dcf961c12afe260d3416'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-2-context.md'
  - '{project-root}/docs/implementation/spec-2-2-record-every-transition.md'
  - '{project-root}/docs/implementation/spec-1-7-resync-when-the-stream-cannot-be-trusted.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** When the engine resyncs, reconnects or reconciles, the record only shows the
snapshot Transitions that follow. Nothing records that herddash could not see for a while, or
which epoch the first snapshot after a start decided. The record shows a gap as if nothing
happened (FR-7).

**Approach:** Write a resync marker into the record for each of these: every resync, every
connection becoming ready (start or reconnect), and every reconcile that disagreed. Each marker
carries the epoch then in force, its reason and, where there is one, a detail.

## Boundaries & Constraints

**Always:**
- **Reasons** (store constants, beside `ReasonDaemonStart`):
  - `connect`: the first connection of this run became ready.
  - `reconnect`: a later connection became ready.
  - The Story 1.7 resync causes verbatim: `events_lost`, `error`, `rejected`, `closed`,
    `undecodable`.
  - `reconcile`: a reconcile read disagreed.
- **Detail:**
  - For `error`, `events_lost` and `rejected`: herdr's error code. The engine keeps the code from
    the `*herdr.Health` beside the cause.
  - For `connect` and `reconnect`: `epoch kept` or `epoch incremented` when the epoch rule ran,
    else empty (NULL).
  - Never an environment value or a Pane Title.
- **When and with what epoch:**
  - **Connect or reconnect:** on the first snapshot of a connection, after `decideEpoch` and
    before that snapshot's rows, with the epoch it decided. This also marks the epoch decided
    after a Daemon start (deferred from Story 2.2), and the gap while herdr was down at start.
  - **Resync:** when the snapshot that ends it is about to be applied, before its rows. The reason
    is the resync's cause, and the epoch is the current one.
  - **Reconcile:** after applying a read that the reconcile timer requested, and only if it
    produced at least one snapshot Transition, first sight or removal. A read that served both a
    reconcile and another request counts as a reconcile.
- Markers go through the existing `Recorder.AppendResyncMarker`. With recording off (nil store),
  nothing is written. A failed write is logged at error and never stops the engine.
- The record's Status for every Agent is consistent with the snapshot that ended the resync. This
  already holds through `apply` and Story 2.2's recording; a test proves it.

**Never:**
- Write a marker for a read that was not a resync, a connection's first snapshot, or a
  disagreeing reconcile.
- Change the Sink's output.
- Update or delete rows.
- Touch a live herdr.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| events_lost | status stream ends `events_lost` code `events_lost` | marker `events_lost`, detail `events_lost`, before the resync snapshot's rows | none |
| Unknown code | stream ends `error` code `weird_new` | marker `error`, detail `weird_new` | none |
| Rejected | subscribe rejected `pane_not_found` | marker `rejected`, detail `pane_not_found` | none |
| Closed | status stream closed | marker `closed`, detail NULL | none |
| Started with herdr down | ping fails twice, then works; 2 Agents | markers `daemon_start`, then `connect`; 2 snapshot-provenance first-sight rows | none |
| Reconnect kept | lifecycle lost; same terminals | marker `reconnect`, detail `epoch kept`, epoch unchanged | none |
| Restart new herdr | persisted state, new terminals | `daemon_start` (old epoch), then `connect` with the new epoch and `epoch incremented` | none |
| Reconcile agrees | reconcile read, nothing differs | no marker | none |
| Reconcile disagrees | reconcile read shows w1:p1 idle (was working) | snapshot row working→idle, then marker `reconcile` | none |
| Consistency | after an events_lost resync | the latest row per Agent equals the snapshot's Status | none |
| Recording off | load failed | no markers | none |

</frozen-after-approval>

## Code Map

- `internal/store/record.go` -- add the reason constants beside `ReasonDaemonStart`.
- `internal/engine/engine.go`:
  - `onStatus`/`onSubscribed` set `p.cause`. Keep the Health code beside it, and carry cause and code into the resync (`resyncNext` and the running resync's cause and detail).
  - `onSnapshot` has `first`, `decideEpoch` and the `ending` resync; write the markers there in the order given above.
  - `reconcileDueMsg` handling is where a read is tagged as a reconcile, through the `snapSeq` it will use.
  - `apply` must report whether it changed anything.
- `internal/engine/recorder.go` -- a `mark(reason, detail)` helper over `AppendResyncMarker`.
- `internal/engine/recorder_test.go` and `fake_test.go` -- the fake store already logs markers in order. Cover every matrix row.
- `cmd/herddash/serve_test.go` -- extend `TestServeRecords`' expected markers if needed.

## Tasks & Acceptance

**Execution:**
- [x] `internal/store/record.go` -- reasons
- [x] `internal/engine/*` -- markers at connect, reconnect, resync and reconcile, plus tests for every matrix row
- [x] `cmd/herddash/serve_test.go` -- updated expectations

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes and earlier tests still pass.

## Implementation Notes

- **Cause type.** The engine's resync cause is now `cause{reason, detail}` (was a string), held per
  pane, in `resyncNext` and in the running resync (`resyncCause`). `healthCause` takes herdr's
  code as the detail only for `events_lost`, `error` and `rejected`; `subscribeCause` falls back
  to `closed` with no detail. The Sink still gets only the reason.
- **Where markers are written.** `onSnapshot` now builds the terminal map and calls `saveState`
  (moved out of `apply`), then writes the connect/reconnect marker and the resync marker, then
  `apply`s. So writes run: save (if changed), marker(s), rows. A reconcile marker follows `apply`.
- **connect vs reconnect** uses a new run-level `readyOnce`, not `everReady`, because a loaded
  server state sets `everReady` before the first connection.
- **Reconcile tagging.** `reconcileDueMsg` sets `reconcileFrom = snapSeq + 1` (the next read to
  start, whatever is in flight or scheduled) unless one is already outstanding; the first applied
  read with `seq >= reconcileFrom` counts as the reconcile. `disconnect` clears it.
- **apply's `changed`** is true for a snapshot Transition, a known Agent removed (absent or
  terminal replaced), or an Agent whose Status this snapshot is the first to show
  (`seenBy == ""` before `seen`), so a pane still awaiting its Profile does not count on every read.
- **A read that both ends a resync and serves a reconcile** writes the resync marker before its
  rows and, if it disagreed, the reconcile marker after them.
- With a Store, the connect marker adds one clock reading before the first snapshot's first
  sight; `TestRecordFirstSightAfterHeldResolution` now expects `clockAt(4)`.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH | Causes that join a running or scheduled resync are lost from the record | medium | `requestResync` keeps only the first cause; patch: one marker per distinct cause |
| 2 | ECH | A resync cut short by a disconnect writes no marker | medium | `disconnect` clears the cause without marking; patch |
| 3 | BH | A reconcile that only corrects the Agent Program writes no marker | low | `setProgram` result not counted; direct; patch |
| 4 | BH | Store reason constants are unused and untied to herdr's kinds | low | Patch: a test tying them |
| 5 | BH | Markers record when a gap ended, not when it began | low | The Sink's Resynced marks the start live; the record's marker at the end is the fact FR-7 asks for; adds a field; rejected |
| 6 | BH | Reconcile markers follow their rows; others precede them | false | Deliberate: a reconcile is known to have disagreed only after apply (decision bb7a481d4fbc) |
| 7 | BH | Reconcile detail is empty | low | Nothing needs it; rejected |
| 8 | BH | No closing marker for the old epoch on an increment | low | The `reconnect … epoch incremented` marker states it; rejected |
| 9 | BH, VG | Untested: undecodable, read serving reconcile and another request, resync-plus-reconcile order, failed marker write, first-sight reconcile, held resolution | medium | VG pre-verified for the reconcile tagging and first-sight guards; patch |
| 10 | VG | Clearing `reconcileFrom` on disconnect is unverified | low | Cheap test; patch |
| 11 | BH, ECH | Reconcile "no more markers" checks run before the read is applied | medium | Assertions can pass vacuously; patch |
| 12 | BH | `TestMarkerRejected` races an in-flight resync and reports every bad marker | low | Patch |
| 13 | ECH | A failed save leaves the marker keyed by an unpersisted epoch | low | The failure is logged and retried at the next snapshot; rejected |
| 14 | BH | The `mark` error log omits epoch and detail | low | Patch |
| 15 | BH | Spec and tracker statuses disagree | false | Workflow ordering |
| 16 | BH | `TestServeRecords` doc comment overlong | low | Patch |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
