---
title: 'Story 1.6: Reconnect forever, and know when herdr restarted'
type: 'feature'
created: '2026-10-16'
status: 'done'
baseline_commit: '85f1ed2d712912578b5a30e47096523b8dee17d0'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/implementation/spec-1-5-watch-every-agent-s-status-live.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `herddash serve` exits when herdr is absent at start or its lifecycle stream ends,
and every Agent's Epoch is fixed at 1. The operator would have to restart herddash after any herdr
restart, and a herdr that reissues pane ids would splice unrelated Agents together (FR-8, AD-3).

**Approach:** The engine treats herdr's absence as normal: it retries forever with capped backoff,
then re-subscribes and snapshots. On every reconnect it decides the epoch from Story 1.3's
evidence, the per-pane `terminal_id`, and never from pane ids alone.

## Boundaries & Constraints

**Always:**
- **Reconnect:** a failed start (ping, lifecycle subscribe, first snapshot) or any lifecycle health
  signal puts the engine into reconnecting: close every status subscription, keep the tracked
  Agents and their last Status, and retry the start sequence forever with exponential backoff from
  a named initial delay to a named cap (tunables). Each attempt and the wait are logged at info,
  not as errors. Only the run context ending stops it. A snapshot failure while connected stays
  Story 1.5's retry.
- **Epoch rule (AD-3):** the engine holds the epoch (starting at 1) and, for each tracked pane, the
  `terminal_id` it last saw. On the first snapshot after a reconnect:
  - **Same instance** if at least one tracked pane is present with the same `terminal_id`: keep
    the epoch. Panes missing from the snapshot are removed as in Story 1.5 (a pane closed during
    the outage), and Agents whose Status is unchanged print nothing.
  - **Otherwise** (no tracked pane kept its `terminal_id`, including when no panes were tracked):
    increment the epoch. Every previously tracked Agent is removed under the old epoch, and the
    snapshot's Agents are new Agents under the new one. A pane-id match alone never keeps the
    epoch.
- **Printing:** one `reconnected` line per successful reconnect naming the herdr version and
  whether the epoch was kept or incremented (and its value), plus the usual Agent, Transition and
  removal lines. Transitions after a kept epoch compare against the Status held before the outage.
- **Messages from before a reconnect** (old subscriptions, old snapshot reads) are ignored by tag.
- The epoch exists only in memory: a Daemon restart starts at 1 again until milestone 2 persists
  the epoch and the pane-to-`terminal_id` map. AD-3's hazard 3 stays open and is said so in AD-3.

**Never:**
- Exit on herdr being absent, gone or restarting; exit 1 is left only for configuration and data
  directory errors before the engine starts.
- Record resync markers, enforce a minimum resync interval, or gate on version (Stories 1.7, 1.8).
- Touch a live herdr in tests.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Absent at start | ping fails 3 times, then succeeds | waits with growing backoff, then starts normally | none |
| Backoff cap | ping fails many times | delay never exceeds the cap | none |
| Stream lost, same instance | lifecycle closed; reconnect snapshot has w1:p1 with the same terminal id, Status unchanged | one `reconnected … epoch kept` line, nothing for w1:p1 | none |
| Changed during outage | as above but w1:p1 now `idle` (was `working`) | reconnected line, then a `snapshot` Transition working→idle under the kept epoch | none |
| Closed during outage | same instance; w1:p2 absent | epoch kept; removal line for w1:p2 | none |
| Restarted | all tracked panes have new terminal ids, same pane ids | `reconnected … epoch 2`; removals under epoch 1; Agent lines under epoch 2 | none |
| Fresh reissue | new terminal ids, pane ids reused for different agents | epoch increments; never joined | none |
| Stale message | status event from a pre-reconnect subscription arrives late | ignored | none |
| Stop while waiting | context cancelled during backoff | Run returns nil promptly | none |

</frozen-after-approval>

## Code Map

- `internal/engine/engine.go` -- `Run`, `start` (ping, lifecycle subscribe, first snapshot), `onLifecycle` (health → currently fatal), `onSnapshot` (`started` flag, retry), `apply`, `key()` (uses `liveEpoch`), message tags (`gen`, `snapSeq`); `pane` gains its last `terminal_id`
- `internal/engine/tunables.go` -- `liveEpoch` goes; add reconnect backoff initial delay and cap as `default…` constants behind package vars tests can lower, as `snapshotMinInterval` does
- `internal/engine/sink.go` -- add a `Reconnected(version string, epoch uint64, kept bool, observedAt int64)` report
- `internal/engine/engine_test.go`, `fake_test.go` -- fake client already scripts ping/snapshot/subscribe failures; extend it to fail N times and to swap terminal ids
- `internal/core/observe.go` -- `PaneObservation.TerminalID` exists
- `cmd/herddash/serve.go`, `serve_test.go` -- printer gains the reconnected line; serve no longer exits on herdr errors
- Spine `ARCHITECTURE-SPINE.md` AD-3 -- replace "The detection rule is decided and tested in Story 1.6" with the rule above, keeping the three hazards and how each is handled; `docs/research/herdr-socket-api.md` Q1 references AD-3 and needs no change

## Tasks & Acceptance

**Execution:**
- [x] `internal/engine/engine.go`, `tunables.go`, `sink.go` -- reconnecting state, backoff loop, epoch rule, stale-message tags across reconnects
- [x] `internal/engine/engine_test.go`, `fake_test.go` -- every matrix row, with lowered backoff, under `-race`
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- reconnected line; a fake socket that goes away and comes back keeps serve running and prints it
- [x] `ARCHITECTURE-SPINE.md` AD-3 -- the decided rule and the hazards' status

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes and arch_test passes.

## Implementation Notes

- `Engine.Run` no longer returns an error: only its context ends it.
- After a lost stream the wait counts into the same backoff as a failed start; the count resets only when the lost connection had stayed up for `reconnectStableAfter` (30 s).
- Detected but never reported panes are dropped on disconnect; the reconnect snapshot finds them again.
- The epoch evidence is every pane's `terminal_id` (shells included); a known pane whose `terminal_id` changes is replaced by a new Agent in any snapshot.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH | In a kept epoch, a pane whose pane id survives with a different `terminal_id` is joined to the old Agent | medium | `apply` matches by pane id and overwrites `terminalID`; AD-3 forbids joining on pane id alone; patch: such a pane is removed and reported as a new Agent |
| 2 | BH | The mixed case (one pane kept, another replaced) is untested | medium | No test or matrix row; patch with 1 |
| 3 | BH | The rule assumes `terminal_id`s are not counters reissued by a fresh server | low | Recorded 0.9.5 ids are long increasing hex values (`term_65df2e06a79ca1` before, `term_65df2e18d44a41` after a restart), not small counters; patch AD-3 to cite this and mark it as observed once |
| 4 | BH, ECH, VG | `Run` now always returns nil; serve's error branch is dead | low | Direct deletion and doc fix; patch |
| 5 | BH | Nothing on stdout says herdr was lost; `Agents()` keeps serving stale Statuses | low | Real; the spec asks only for a reconnected line, and connection state for consumers is ConnectionTruth (Epic 3); defer |
| 6 | BH | Printed lines omit the epoch | low | After a restart, removal and Agent lines for the same pane id look alike; direct printer field; patch |
| 7 | BH | `reconnected` line breaks the column width and key=value form | low | Direct printer fix; patch |
| 8 | BH | `TestNoArgumentsServes` checks INFO anywhere in the log | low | Would pass with the line at WARN; test edit; patch |
| 9 | BH | Two info lines per attempt forever into an unrotated log | low | ~5,760 lines a day at the cap; patch: the attempt line at debug |
| 10 | BH | `setReconnectDelays` restores fixed values | low | Test helper; patch |
| 11 | BH | Index arithmetic in two reconnect tests can inspect the wrong calls or panic | low | Test edit; patch |
| 12 | BH | Spec/tracker status mismatch; empty notes | false | Workflow ordering; notes are added at finalize |
| 13 | BH, ECH, VG | A herdr that drops right after each start never backs off: `lost()` waits the initial delay and a successful first snapshot resets failures | medium | Flapping cycles at 1 s forever with churn; patch: count losses into the backoff and reset only after the connection has stayed up for a named period |
| 14 | BH | The research record still says Story 1.6 must account for `server.live_handoff` | low | Frozen records must agree with the code; direct pointer to AD-3; patch |
| 15 | ECH | serve_test fake: a connection accepted during shutdown is never closed | low | Can hang the test; direct check; patch |
| 16 | ECH | All Agent panes closed during the outage while shell panes survive → spurious increment | medium | `decideEpoch` compares only tracked Agent panes; patch: compare the `terminal_id`s of every pane in the last snapshot |
| 17 | VG | The non-empty `terminal_id` guard is untested | medium | VG pre-verified; patch |
| 18 | VG | Dropping detected-but-unreported panes on disconnect is untested | medium | VG pre-verified: removing the drop makes such a pane invisible forever and no test fails; patch |
| 19 | VG | `requestSnapshot` does not check `connected` | low | No caller reaches it while reconnecting; a guard would protect undemonstrated state; rejected |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
