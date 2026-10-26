---
title: "Story 5.17: A released pane's next Agent starts fresh across a restart"
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: '214d1a647aabb8a0661f8e47066381c8bb04ed20'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Agent A goes working→done live, herdr sends `agent_released` (the engine removes the pane and writes no row), then the Daemon stops or loses herdr, Agent B starts in the same pane and terminal, and the Daemon comes back keeping the epoch. The first snapshot seeds B from A's history, so B is written as `done->idle` and shows Finished with A's wait. Reproduced by a reviewer's failing test against Story 5.10.

**Approach:** Decided (vfkb bc7b36741097, extending aa3dcbf096bc), not reopened: the engine remembers which panes had their Agent released, as engine state saved in `server_state` beside the pane-to-terminal map, outside the append-only record. A released pane is never seeded from the record; its next Agent's first sight is a row with no previous Status, and writing that row clears the mark. `server_state` gains one column through migration 0004 (engine state only; AD-10 needs an ADR only for the transitions table): the existing `terminals` column is a JSON object of string to string that 0.5.0 decodes strictly, so it cannot carry the mark without a magic key.

## Boundaries & Constraints

**Always:** AD-2 single writer: the mark lives on the Run goroutine and is saved only through `saveState`. The mark only ever prevents seeding (it can split one Agent, never join two). Saved state without the column's data (written by 0.5.0, defaulted by the migration) loads with no released panes and seeds exactly as before. `internal/core` untouched. `arch_test.go` and `callsite_test.go` rules hold.

**Never:** No change to the transitions table, the AgentKey, or any row type; no ADR. Do not touch the live herdr or the running service. Do not edit `sprint-status.yaml` or `epic-5-context.md`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Restart after release | A `nil->working`, `working->done` live; `agent_released`; stop; restart epoch kept, snapshot shows w1:p1 idle on the same terminal | No history read for w1:p1; row `w1:p1 nil->idle`; B Working (Idle) since that row; mark cleared and saved | none |
| In-run reconnect | same, but the lifecycle stream is lost after the release and B appears before the reconnect's first snapshot | same as above | none |
| Not released | A working→done, no release, restart, snapshot shows done | seeded as before: nothing written, Finished since A's done row | none |
| 0.5.0 saved state | `server_state` row from schema 3, no released column data | loads with no released panes; seeded as before | none |
| First sight not written | B tracked but its first row not recorded (Append fails, run ends, or Profile pending) | mark kept, so the next start still does not seed | log only |
| Epoch increments / pane gone | reconnect increments the epoch, or a snapshot no longer shows the pane | mark dropped | none |

</intent-contract>

## Code Map

- `internal/store/migrations/0004_server_state_released.sql` -- new: `ALTER TABLE server_state ADD COLUMN released TEXT NOT NULL DEFAULT '[]'` (JSON array of pane ids), with a header comment like 0002's.
- `internal/store/record.go` -- `ServerState` (~60) gains `Released []string` (sorted, nil = none); `LoadServerState` (~250) selects and decodes it; `SaveServerState` (~276) encodes it (nil → `[]`), upsert sets it too.
- `internal/store/store_test.go` -- `schemaVersion` const (~569) to 4; migration name list (~560); `server_state` column table (~400) gains `released TEXT notnull`.
- `internal/store/record_test.go` -- `TestServerState` round-trips `Released`; `TestUpgradeFromSchema2` asserts `schemaVersion != 3` (~486) — change to compare against `schemaVersion`; add `TestUpgradeFromSchema3`: a schema-3 DB with a server_state row loads with no Released, and saves one.
- `internal/engine/engine.go` -- engine fields (~272): add `released map[string]bool` beside `terminals`, and `savedReleased`. `onLifecycle` `LifecycleAgentReleased` (~1012): mark `e.released[ev.PaneID]` and, when `e.ready`, `saveState()`. `onSnapshot` (~1228): after rebuilding `e.terminals`, drop marks for panes it does not show, before `saveState`. `decideEpoch` (~1288) not-kept branch: clear marks. `apply` (~1359): `seed` false when `e.released[obs.PaneID]`, logged at Info. `firstSight` (~2548): after recording a row with no previous Status and the pane not `unrecorded`, delete the mark and `saveState()` if it existed. Update the `seedHistory` (~374) and `firstSight` doc comments.
- `internal/engine/recorder.go` -- `restore` (~37) adopts `st.Released`; `saveState` (~95) compares and saves the released set too.
- `internal/engine/fake_test.go` -- `LoadServerState`/`SaveServerState` (~1051) clone `Released`.
- `internal/engine/attention_test.go` -- helpers `stored`, `restartOn`, `agentByPane`, `historyReads`, `paneRows`, `terminalChangedRestart`; `TestWaitingNewAgentInReusedPaneAcrossRestart` (~440) is the pattern the reviewer's test follows. Reviewer's test: `/private/tmp/claude-501/-Users-jasonviloria-GitHub-herddash/fff4672d-5559-480a-a694-9955071e114b/scratchpad/rev/internal/engine/zz_review_test.go` (fails on baseline: B finished).
- `internal/engine/engine_test.go` -- `twoAgents` (~35), `harness.loseLifecycle` (~580) for the in-run reconnect.
- `cmd/herddash/serve_test.go` -- `TestServeHistoryAfterSweepReusedPane` (~1570) pattern: real DB seeded via `store.Open`, `SaveServerState`, rows, then `run(serveArgs(...))` against `startFakeHerdr`.
- `docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md` -- AD-3 "Successive Agents on one AgentKey" (~154-167).

## Tasks & Acceptance

**Execution:**
- `internal/store/migrations/0004_server_state_released.sql`, `internal/store/record.go` -- add and persist the released set -- engine state survives the restart.
- `internal/store/store_test.go`, `internal/store/record_test.go` -- schema 4, round trip, upgrade from schema 3 -- the 0.5.0 compatibility AC.
- `internal/engine/engine.go`, `internal/engine/recorder.go`, `internal/engine/fake_test.go` -- mark on release, never seed a marked pane, clear on its next first row, drop on epoch increment or pane gone -- the fix.
- `internal/engine/attention_test.go` -- the reviewer's scenario as a real test (restart), the in-run reconnect variant, the not-released control, mark kept when the first row is not recorded, mark dropped on epoch increment; each must fail on baseline where it asserts the fix.
- `cmd/herddash/serve_test.go` -- a real DB with A's rows and `Released: [w1:p1]`; after serving, w1:p1's history ends in a row with no previous Status and `/api/state` does not show it Finished.
- `ARCHITECTURE-SPINE.md` -- AD-3: a released pane's next Agent is not seeded (mark in `server_state`), and the residual: quit and restart in the same terminal with no `agent_released` observed (the Daemon was down for the release too) still joins.

**Acceptance Criteria:**
- Given the reviewer's scenario across a Daemon restart, when the Daemon sees B, then B's first row has no previous Status and B shows Idle (or its live Status) from its own first row, never A's class or wait.
- Given the same scenario as an in-run reconnect with B appearing during the disconnect, when the reconnect keeps the epoch, then the same holds.
- Given a pane that was not released, when the Daemon restarts, then it is seeded exactly as before.
- Given saved state written by 0.5.0, when the Daemon starts, then it loads and seeds as before.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 15 findings — high 0, medium 0, low 10, false 5, maybe-false 0
- findings:
  - `[false]` `[reject]` Intent: the story's literal "no stored-schema change" conflicts with migration 0004 — the decision the run was given (vfkb bc7b36741097, relayed by the orchestrator) scopes "no stored-schema change" to the transitions table and allows a migration that changes only `server_state`; AD-10 requires an ADR only for a migration touching transitions. The Intent section states the choice and why `terminals` could not carry it.
  - `[low]` `[reject]` Intent: the in-run reconnect is tested only at engine level, and the release itself is never driven through serve — the serve harness has no lifecycle-loss control; the engine tests exercise the same `apply`/`firstSight` code with the real decision logic, and the serve test covers the real store and API with the saved mark.
  - `[low]` `[patch]` Blind: no test for B detected live after the release clearing the mark — added `TestReleasedMarkClearedByLiveDetection`.
  - `[low]` `[reject]` Blind: a failed `SaveServerState` at release loses the mark if the Daemon stops before the next save — needs a save failure and a stop in the same window; every save failure in the engine already waits for the next save, and a retry timer adds state for a path never observed.
  - `[false]` `[reject]` Blind: `firstSight`'s save is unguarded while the release's is guarded by `ready` — saving with the held epoch and terminals is correct whenever a state was saved or loaded; the release guard is now `ready || saved`, so both are safe.
  - `[low]` `[patch]` Blind: `firstSight` doc comment had an over-long line — rewrapped.
  - `[false]` `[reject]` Blind: `firstSight`'s comment omits "the Profile still resolving" — `firstSight` is not reached while the Profile resolves; the comment lists firstSight's own paths, and the spine names the pending case.
  - `[low]` `[patch]` Blind: the downgrade cost is not recorded — the migration's header comment now says a schema-3 binary refuses a schema-4 database; the run's report carries it to the operator.
  - `[low]` `[patch]` Blind: the serve test asserted only the last history row — it now requires exactly A's two rows plus the new Agent's one.
  - `[low]` `[reject]` Blind: the `profile pending` subtest relies on a `quiet` sleep after cancel — passed 40 runs under `-race`; replacing it needs a new hook into the run's end, more than a direct correction.
  - `[low]` `[reject]` Blind: `TestNotReleasedSeededAcrossRestart` repeats `releaseA`'s setup — cosmetic duplication of six lines.
  - `[false]` `[reject]` Blind: the store accepts duplicate or unknown released ids — the only writer is the engine's `saveState`, which writes a set's sorted keys; `'null'` decodes to none and a non-array fails the load loudly.
  - `[low]` `[patch]` Blind + Edge: a release handled before a connection's first snapshot (`!e.ready`) was not saved, so a stop before that snapshot lost it — the release now saves when `e.ready || e.saved`; with a saved state the epoch and terminals saved are the ones already held. Not separately tested (the harness cannot deliver a lifecycle event before the first snapshot).
  - `[low]` `[reject]` Edge: save failure at release with no later save — same as the Blind row above.
  - `[false]` `[reject]` Edge: B's first row Append fails, later B rows join A in the record — pre-existing Story 5.10 behaviour for any failed first-sight Append (the pane is marked unrecorded and Retention keeps all its rows); with the mark kept, the next start writes B a boundary row and the engine never seeds B from A. Not caused by this change.
  - Verification-gap layer: no gaps found.

## Design Notes

Clearing only after B's first row is recorded matters: a Daemon stopping while B's Profile resolves, or a failed Append, leaves the record ending in A's rows, so the next start must still not seed. Saving on release (when ready) matters because no snapshot need follow a release before the Daemon stops. Downgrade cost: a 0.5.0 binary refuses a schema-4 database (`ErrSchemaNewer`), as any migration makes it.

## Verification

**Commands** (Go 1.26.5 is at `$HOME/.local/share/mise/installs/go/1.26.5/bin`; prepend it to PATH. Run from the repository root that holds this spec. Never touch the live herdr or service):
- `go build ./... && go vet ./... && go test ./...` -- expected: pass
- `gofmt -l $(git ls-files '*.go')` -- expected: no output
- `go test -race -count=20 -run '<new tests>' ./internal/engine/ ./internal/store/ ./cmd/herddash/` -- expected: pass

## Auto Run Result

Status: done

**Summary.** On `agent_released` the engine marks the pane released, as engine state saved in `server_state` (new column `released`, migration 0004) outside the append-only record. A marked pane is never seeded from the record, so the next Agent's first sight is a row with no previous Status, after a Daemon restart or an in-run reconnect alike. Recording that row clears the mark; a row not recorded keeps it. The mark is dropped when the epoch increments or the pane is gone. State saved by 0.5.0 loads with no marks and seeds as before. No transitions-table change, no ADR.

**Files.**
- `internal/store/migrations/0004_server_state_released.sql`: adds `server_state.released` (JSON array, default `[]`).
- `internal/store/record.go`: `ServerState.Released`, loaded and saved.
- `internal/engine/engine.go`: mark on release (saved when ready or a state is saved), no seeding for a marked pane, clear on its next first row, drop on epoch increment or pane gone.
- `internal/engine/recorder.go`: restore and save the marks.
- `internal/engine/fake_test.go`: fake store clones `Released`.
- `internal/engine/attention_test.go`: reviewer's scenario across a restart and an in-run reconnect, live detection clearing the mark, the not-released control, mark kept when B's row is not recorded, mark dropped.
- `internal/store/store_test.go`, `internal/store/record_test.go`: schema 4, round trip, upgrade from schema 3.
- `cmd/herddash/serve_test.go`: `TestServeReleasedPaneStartsFresh` against a real database and the API.
- `ARCHITECTURE-SPINE.md`: AD-3 states the released-pane case and the remaining residual.

**Review.** 5 patches (all low), 0 deferred, 5 low rejected and 5 false (reasons in the triage log). Verification-gap layer: none.

**Follow-up review:** false. No high or medium finding was patched.

**Verification.** `go build ./...`, `go vet ./...`, `gofmt -l .` (no output) and `go test -count=1 ./...` pass. `go test -race -count=20 -run Released ./internal/engine/ ./cmd/herddash/` and `go test -race -count=20 ./internal/store/` pass. The reviewer's original reproduction test passes against the fix and failed on the baseline.

**Residual risk.** An Agent that quits and another that starts in the same terminal while the Daemon is down for the release too still join (AD-3). A failed save of the mark followed by a stop before any later save loses it. A schema-3 binary (0.5.0) refuses a database migrated to schema 4, so rolling back to 0.5.0 needs the database restored or removed.
