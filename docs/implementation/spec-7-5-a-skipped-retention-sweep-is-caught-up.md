---
title: 'Story 7.5: A skipped Retention sweep is caught up'
type: 'feature'
created: '2026-11-03'
status: 'done'
route: 'oneshot'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `sweep` (`internal/engine/retention.go`) returns early while the present set is not known (not connected and ready) or while a resync runs or is scheduled, and nothing retries it: the next attempt is the next hourly tick, so a Host where resyncs keep landing on the tick never trims its record (Epic 2 retrospective F-8).

**Approach:** A sweep that skips marks itself owed on the engine. At the end of every applied snapshot in `onSnapshot`, after the resync it may end and any resync it requests are settled, an owed sweep is run once if its conditions now hold; running clears the debt, and a sweep that skips again stays owed. The snapshot that ends a resync is where a resync ends, and the first snapshot after a reconnect is where readiness returns, so both are caught up the same way (a resync abandoned by a disconnect is caught up at the reconnect). The hourly ticker is unchanged, and a Daemon restart already sweeps at its first ready connection, so the debt lives in memory only. Several skipped ticks owe one sweep. A sweep that ran and failed is not owed: it is logged, as now, and the next tick tries again.

</frozen-after-approval>

## Implementation Notes

- `Engine.sweepOwed` (`internal/engine/engine.go`, memory only) is set by `sweep` when it skips and cleared when it runs, before the store call, so a failed sweep is not owed. The skip condition is now `sweepBlocked()` (`retention.go`), shared by `sweep` and the catch-up at the end of `onSnapshot` (`else if e.sweepOwed && !e.sweepBlocked()`, after the resync handoff and `resyncSettle`, so a snapshot that starts or schedules the next resync keeps the debt). The catch-up logs `retention sweep caught up` at debug before the usual INFO line. Checking `sweepBlocked` first means snapshots read while a resync waits log no repeated skip.
- Behaviour change beyond the resync case: a reconnect now sweeps once when a tick was skipped while disconnected. `TestRetentionSkippedWhileDisconnected` asserted the reverse ("the reconnect itself does not sweep") and now asserts one catch-up; `TestRetentionReconnectOwesNothing` keeps the old rule when nothing was skipped. After an epoch-incrementing reconnect, the old epoch's Agents are no longer present, so that catch-up deletes their old rows by age only, as the next tick would have done an hour later.
- Limit, stated in the F-8 `resolved:` line and `docs/config.example.toml`: resyncs that keep following one another, with no snapshot leaving none running or scheduled, still keep the sweep waiting.
- Mutation-checked: disabling the catch-up fails the resync, scheduled, disconnected and abandoned tests; dropping the `sweepBlocked` guard fails the scheduled test (it logs a second skip).
- Files: `internal/engine/engine.go`, `retention.go`, `retention_test.go`; `docs/config.example.toml`, `deferred-work.md`, `sprint-status.yaml` (also Story 7.4's `# PR #69`).

## Verification

- `go build ./... && go vet ./... && go test -race ./... && test -z "$(gofmt -l .)"` -- all pass
- `go test -race -count=50 -cpu=1,4,8 ./internal/engine -run 'TestRetention'` -- pass

## Review Triage Log

Blind Hunter, one pass:

- No test of a resync whose ending snapshot starts the next — low, real: patched, `TestRetentionSkippedAcrossChainedResyncs` (the debt survives the first end and is paid once at the last).
- Back-to-back resyncs can still starve the sweep, unstated — low, real: patched the wording (F-8 `resolved:` line, `config.example.toml`) to state the limit; bounding it (sweep anyway after K skips) would sweep with an unknown present set, which `sweep` exists to avoid.
- `config.example.toml` overstates when the catch-up runs — low, real: patched.
- A catch-up is indistinguishable in the log — low, real: patched, a debug `retention sweep caught up` line.
- No test of a failing catch-up — low; rejected: tick and catch-up share `sweep`, whose clear-before-store is tested from a tick by `TestRetentionSweepFails`.
- The reconnect behaviour change is not recorded — low, real: patched in Implementation Notes.
- Spec lacks notes, triage log and a run record; tracking says in-progress — false: filled at this step, as in 7.3 and 7.4.
- Story 7.4's `# PR #69` rides along — low; kept, named in Implementation Notes and the commit message.
- The `onSnapshot` comment is narrower than the rule — low, real: patched to the general rule.
