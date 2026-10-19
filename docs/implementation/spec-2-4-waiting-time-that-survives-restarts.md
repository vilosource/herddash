---
title: 'Story 2.4: Waiting Time that survives restarts'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: 'f8ffd3f733e3fea3a2ae83d2e9080db8164a660b'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-2-context.md'
  - '{project-root}/docs/implementation/spec-2-2-record-every-transition.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Nothing computes Attention Class or Waiting Time. A Daemon restart also writes a
fresh first-sight row for every Agent even when nothing changed, which would reset every Waiting
Time and break FR-8 (FR-2, FR-3, FR-8, AD-1, AD-12).

**Approach:**
1. A pure `core.Fold` over an Agent's ordered Transitions gives its Attention Class, the instant
   that class began, that Transition's provenance, and whether the Agent was ever `working`.
2. The engine keeps each Agent's fold up to date.
3. At an Agent's first sight, the engine seeds the fold from the record. If the Status is the one
   last recorded, it writes nothing.
4. serve prints the class and the Waiting Time, with `≥` for a lower bound.

## Boundaries & Constraints

**Always:**
- **core** (pure; clock injected as `now int64` ms):
  - `AttentionClass` takes the values `needs_you`, `finished`, `working` and `unknown`.
  - `ClassOf(s Status, everWorking bool)`:
    - `blocked` is needs_you.
    - `done` is finished.
    - `idle` is finished when ever working, else working.
    - `working` is working.
    - `unknown`, or no Status, is unknown.
  - `Fold`: `Fold(ts []Transition) FoldResult{Known bool; Status Status; Class AttentionClass; Since int64; SinceProvenance Provenance; EverWorking bool}`.
    - It folds in the given order, which is `History`'s order.
    - `Since` is the `ObservedAt` of the Transition at which the current class began; a later
      Transition in the same class does not move it. So `done` to `idle` keeps `Since`.
    - An `Add(t)` method extends a result by one Transition, giving the same answer as
      re-folding.
  - `WaitingTime(f FoldResult, now int64) (ms int64, exact bool)`. `exact` is true only when
    `SinceProvenance` is `live`. A lower bound is `now - Since`, never negative.
- **Engine:**
  - Each pane holds a `FoldResult`. Every recorded or reported Transition and every first sight
    goes through `Add`. This works with or without a store.
  - **First sight with a store and the epoch kept or loaded:** read `History(key)` before
    recording.
    - If it is known and its last Status equals the Status seen: write no row and keep the
      folded result. A class that began `live` stays exact across the restart.
    - If it is known and the Status differs: record a `snapshot` (or `live`) Transition whose
      previous Status is the last stored one.
    - If it is unknown: behave as Story 2.2 does.
  - A failed History read is logged. The engine then behaves as for unknown history.
  - `Agent` gains `Class AttentionClass`, `WaitingSince int64` and `WaitingExact bool`, filled
    from the fold.
- **serve:** the agent and transition lines gain `class=<class> waiting=<d>` or `waiting=≥<d>`.
  `<d>` is a compact duration such as `0s`, `12m`, `1h05m`, `2d3h`, computed at print time from
  the line's own timestamp. The profile and removed lines are unchanged.
- **Kept-epoch reconnect, Status unchanged:** no Transition, only the Story 2.3 marker. This
  already holds; a test proves it. A real herdr restart increments the epoch (Story 1.6), so its
  Agents are new.
- **Records:** backfilled rows (epoch 0) never reach a live Agent's fold, since History is keyed
  by epoch.

**Never:**
- Change what is stored, or the schema.
- Read the clock in core.
- Build `AgentView`, sort keys or the Board (Epic 3).
- Touch a live herdr.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Class start | working@1 live, blocked@5 live | needs_you since 5, live | none |
| done→idle | working@1, done@5 live, idle@9 | finished since 5 | none |
| Never working | idle@3 snapshot (first sight) | class working, ever false | none |
| Idle after working | working@1, idle@4 | finished since 4 | none |
| Exact | class began live | WaitingTime exact, now−since | none |
| Lower bound | class began with a snapshot row | not exact, now−since | none |
| Unknown | no Transitions | Known false, class unknown | none |
| Restart unchanged | stored blocked (live @100); Daemon restarts; snapshot blocked | no row; Agent waiting since 100, exact | none |
| Restart changed | stored blocked; snapshot shows working | a row with previous blocked, new working, snapshot; lower bound | none |
| Kept-epoch reconnect | lifecycle lost; same terminals; Status unchanged | only the reconnect marker | none |
| Add equals Fold | random sequence | Add step by step equals Fold of the whole | none |
| Print | lower bound of 12 minutes | `class=needs_you waiting=≥12m` | none |

</frozen-after-approval>

## Code Map

- `internal/core/` -- add `attention.go` and `attention_test.go`. Table tests with injected times; no I/O.
- `internal/engine/engine.go`:
  - `report`/`firstSight` hold the first-sight logic.
  - `transition` calls `Add`.
  - The pane struct gains the fold.
  - `agent()` fills the new fields.
- `internal/engine/recorder.go` -- `Recorder` gains `History(ctx, core.AgentKey) ([]core.Transition, error)`, which `*store.Store` already has. Update the fake store.
- `internal/engine/sink.go` -- the Agent fields. Re-export `AttentionClass` for cmd, as is done for `Gate`.
- `cmd/herddash/serve.go`, `serve_test.go` -- the printer fields; an end-to-end restart run showing that no second first-sight row is written and the Waiting Time is exact.

## Tasks & Acceptance

**Execution:**
- [x] `internal/core/attention.go`, `attention_test.go`
- [x] `internal/engine/*` -- the fold per pane, history-seeded first sight, Agent fields, tests
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- printing and the restart test

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes, arch_test passes (core reads no clock), and earlier tests still pass.

## Implementation Notes

- **Sink signature.** `Sink.Transition` became `Transition(t Transition, a Agent)`, with the Agent
  as it is after `t`. The printer cannot compute a transition line's class from `t` alone, and
  `cmd` may not import `core` to fold for itself. The two Sinks (the printer and the engine test
  recorder) are updated. `engine` re-exports `AttentionClass` and its four values.
- **"Epoch kept or loaded"** is the engine flag `historic`: set by `restore` when a saved state is
  found, then set to the outcome of each `decideEpoch` (kept true, incremented false). Only a pane
  first tracked by the **first snapshot of a connection** under such an epoch reads its history
  (`pane.seedHistory`); a pane tracked later by lifecycle detection or a later snapshot, or after a
  `terminal replaced` removal, is a new Agent and reads none. The record matches only when both
  the last stored Status and Agent Program equal what is seen; otherwise a row is recorded.
- **Shutdown.** A History read that fails because the run context is cancelled records nothing for
  that first sight (logged at info), so the next start does not take the Agent for new.
- **Reconcile disagreement.** A first sight that matched the record writes no row and is not a
  disagreement (`pane.firstSkipped`). Setting a fresh pane's Agent Program from empty is no longer
  counted as a program change either; the first sight itself is what counts. Without a Store a
  first sight still counts, as Story 2.3 defined for what the engine reported.
- **First sight without a Store** now builds the first-sight Transition to seed the fold (it is not
  recorded), and warns about an unknown source only with a Store. It reads no extra clock: its time
  falls back to the report's. A new pane's fold starts as `core.Fold(nil)` (class unknown).
- **Restart, Status unchanged** logs `agent unchanged since the record` at info, writes no row, and
  reports the Agent with the stored fold. A failed History read logs `history not read; recording
  the first sight as new` at error.
- **Printing.** `<d>` truncates to its two largest units, the second zero-padded: `45s`, `12m`,
  `1h05m`, `2d03h` (the spec's example `2d3h` is printed `2d03h`, for consistency with `1h05m`). A line
  timestamped before the class began prints `0s`. An Agent with no fold prints `waiting=-`.
- **Kept-epoch reconnect** within a run is proven by the existing `TestMarkerReconnect` (only the
  `reconnect … epoch kept` marker, no row).
- **Remaining limit:** an Agent that exited and restarted in the same pane while the Daemon was
  down, with the same Status and program, is still taken for the recorded one at the next start.
  Nothing observable distinguishes them (AD-3).

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | Impl, ECH, BH, VG | History seeding reaches panes detected mid-run and terminal-replaced panes, so a new Agent in a reused pane id inherits the old class start; behaviour also differs within one run | medium | `historic` gates every first sight; patch: seed only panes first tracked by a connection's first snapshot, and match on program as well as Status |
| 2 | ECH, BH | A History read failing because of shutdown records a fresh first-sight row | low | Direct guard on `runCtx.Err()`; patch |
| 3 | ECH | A reconcile first sight that matched the record still writes a reconcile marker | low | Direct; patch |
| 4 | ECH | No-store engine warns "recording it as snapshot" | low | Direct; patch |
| 5 | BH | Zero-value fold has Class "" unlike `Fold(nil)` | low | Direct; patch |
| 6 | BH | Duration format pads minutes but not hours | low | Direct; patch |
| 7 | BH, VG | "Exact" survives a Daemon outage during which the Status may have flapped | false | PRD glossary and AD-12: a restart across which the Status did not change keeps the exact time (FR-8) |
| 8 | BH | Retention (2.5) could prune the rows the fold needs | false | AD-4 already requires Retention to keep the class-start row and the latest `working` row for present Agents |
| 9 | BH | History reads are synchronous and unbounded | low | A local indexed read per Agent at a connection's first snapshot; rejected |
| 10 | ECH | A wall clock behind the stored rows orders the new row first | low | The binding order is `observed_at`; clock steps are out of scope; rejected |
| 11 | BH | Missing tests: live "restart changed", printed duration, `NewTransition` error branch | low | Patch the first two; the error branch needs an invalid Transition that `firstSight` cannot construct; rejected |
| 12 | VG | The kept-reconnect `historic` flag is untested | medium | Patch with 1 |
| 13 | BH | `unknown` class shows a Waiting Time | low | The PRD defines Waiting Time per class without excepting Unknown; rejected |
| 14 | BH | `WaitingSince == 0` doubles as unknown | low | `observed_at` 0 is not a real instant; rejected |
| 15 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
