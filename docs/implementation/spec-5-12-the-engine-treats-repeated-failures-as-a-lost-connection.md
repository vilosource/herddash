---
title: 'Story 5.12: The engine treats repeated failures as a lost connection'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: 'fc4573c12bd4713e580e10c93fc4921e1d2c160c'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Once ready, a failing snapshot read is retried every `snapshotRetryDelay` forever while the lifecycle stream stays up, so ConnectionTruth keeps `connected: true` and the Board looks current while nothing is read (Epic 1 retro L3, action item #2). Conversely, a well-formed but unexpected lifecycle line (unknown kind, no data, no pane id) ends the lifecycle subscription as `undecodable` and forces a full reconnect, and a newer herdr sending such lines repeatedly would make the Daemon flap (retro L4, item #3).

**Approach:** Count consecutive snapshot failures after ready; at a bound set in `tunables.go` the engine handles it as a lost connection through the existing `lost()` path (ConnectionTruth `connected: false`, reconnect with backoff, a `reconnect` gap marker on recovery). For the lifecycle subscription only, a line that decodes as JSON but is not an event it can deliver becomes a non-final delivery flagged unexpected; the stream goes on and the engine logs it and requests a snapshot.

## Boundaries & Constraints

**Always:** AD-2 single writer: the counter and every decision live on the Run goroutine. A success resets the counter, and so does a disconnect. Bounds go in `internal/engine/tunables.go` as a `default…` const plus a test-lowerable var. Unparsable JSON and a known kind whose data fails to decode stay `HealthUndecodable` and end the stream. Status subscriptions keep today's behaviour (an unexpected status line still ends that stream). Only the event name, never payload content, is logged.

**Never:** No new herdr request, method or subscription, and no change to what herddash sends herdr (so no ADR: the change is in how already-received lines are classified). No change to `core.ConnectionTruth`'s shape, the stored schema, the Board's files, or `arch_test.go`'s permitted set. Do not touch Profile resolution code (Story 5.4 is editing it concurrently).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Transient failure | ready; reads fail fewer than the bound times, then succeed | stays connected, no new ping; the read after recovery applies | warn per failure, as today |
| Persistent failure | ready; reads keep failing | on the bound-th consecutive failure ConnectionTruth publishes `connected: false` and the engine reconnects | info "connection to herdr lost; reconnecting" with the cause |
| Recovery | after that, reads succeed again | the next start's snapshot publishes `connected: true`; a `reconnect` marker is recorded as for any loss | none |
| Unexpected lifecycle line | `{"event":"pane_focused","data":{…}}`, `{"event":"pane_closed"}` (no data), pane event with no pane id | delivery with `Unexpected` set, stream continues, later events still arrive; engine requests a snapshot, stays connected, no reconnect | first per connection logged at warn with the error, later ones at debug |
| Garbage | `{"event":` or a known kind with mistyped data | unchanged: `HealthUndecodable`, stream ends, engine reconnects | unchanged |

</intent-contract>

## Code Map

- `internal/engine/engine.go:1097-1110` `onSnapshot` -- the `m.err != nil` branch; after `!e.ready` (failed start, unchanged) it warns and `schedule(snapshotRetryDelay)`. Add the consecutive-failure count and escalation to `e.lost(...)` here; reset the count on a successful read.
- `engine.go:749-759` `lost(h error)` -- reused unchanged except its log line, which says "lifecycle stream lost"; reword to cover both causes. The callers wrap their cause (`engine: lifecycle stream lost: …` at :927 stays).
- `engine.go:780-824` `disconnect` -- reset the new counter (and the unexpected-warned flag) with the other per-connection state.
- `engine.go:228-245` -- per-connection state fields next to `connected`/`ready`; add the new fields with comments.
- `engine.go:925-936` `onLifecycle` -- handle `d.Unexpected != nil` before `noteEvent`: log, `e.requestSnapshot()`, return. `requestSnapshot` already coalesces with a read in flight or scheduled and honours `snapshotMinInterval`.
- `engine.go:90-120` -- package/type doc comment describing snapshot reads and resync; add one sentence each for the two behaviours.
- `internal/engine/tunables.go` -- add `defaultSnapshotFailuresBeforeLost = 3` and var `snapshotFailuresBeforeLost`.
- `internal/herdr/subscribe.go:69-77` `Delivery` -- add `Unexpected error` (non-final, Event zero). `:160-176` `subscribe` is shared by lifecycle and status; give it a parameter so only `SubscribeLifecycle` passes lines matching `errors.Is(err, errUnexpectedEvent)` on. `:215-237` `stream` -- the decode-error branch.
- `internal/herdr/map.go:131-133` `errUnexpectedEvent` ("decodes as JSON but is not an event this subscription can deliver"); raised at :143 (no data), :191 (unknown kind), :204 (no pane id) for lifecycle, :217/:224 for status.
- `internal/engine/client.go:21-27` -- `Subscription` doc says "events in order and at most one health signal last"; mention unexpected deliveries.
- `internal/herdr/doc.go:14` -- package doc on Health; check and amend if it says every non-event delivery is final.
- Tests: `internal/herdr/subscribe_test.go:279` `TestLifecycleUndecodable` (cases "unknown kind", "no data", "no pane id" move to a new unexpected test; "garbage", "bad data type" stay); `:252` status "wrong event" stays undecodable. `internal/engine/fake_test.go:95-97` fakeSub helpers (add `unexpected(err)`), `fail("snapshot", n)` and `snapErr` script failures, `pings()`/`awaitPings`, `callList()`. `internal/engine/engine_test.go:467` `TestLaterSnapshotFailureIsRetried` (one failure, stays the reference). `internal/engine/view_test.go:286` `TestViewLost` is the pattern for a publication-level assertion (`startedViewed`, `w.awaitAfter`, `ChangeConnection`).
- Board (read-only evidence): `internal/httpapi/web/order.mjs`/`board.mjs` already show the connection strip and dim on `connection.connected === false` (Story 3.9); nothing to change.

## Tasks & Acceptance

**Execution:**
- `internal/engine/tunables.go` -- add the bound -- the story requires it there.
- `internal/herdr/subscribe.go` -- `Delivery.Unexpected`; lifecycle-only pass-through in `stream` -- the stream must survive a well-formed unexpected line to avoid a reconnect.
- `internal/engine/engine.go` -- failure counter and escalation in `onSnapshot`; reset in success and `disconnect`; `onLifecycle` resnapshot on `Unexpected`; reworded `lost` log; doc comments.
- `internal/engine/client.go`, `internal/herdr/doc.go` -- doc comments.
- `internal/herdr/subscribe_test.go` -- unexpected lines deliver `Unexpected` and a following valid event still arrives; garbage and mistyped data stay undecodable; status "wrong event" unchanged.
- `internal/engine/fake_test.go`, `internal/engine/engine_test.go` or `view_test.go` -- tests below.

**Acceptance Criteria:**
- Given a ready engine whose snapshot reads keep failing, when the bound-th consecutive read fails, then a publication carries a `ChangeConnection` with `connected: false` (Agents kept), the engine pings again, and once reads succeed a publication carries `connected: true` with a later `last_snapshot_at`.
- Given a ready engine, when fewer than the bound reads fail in a row and then one succeeds, then no ping follows the first and `connected` never publishes false.
- Given a ready engine, when the lifecycle subscription delivers an unexpected line, then a snapshot is read, the lifecycle subscription is not closed, no new ping or lifecycle subscribe happens, and `connected` stays true.
- Given the whole module, when `go build ./... && go vet ./... && go test ./...`, `gofmt -l .` and `node --test internal/httpapi/web/*.test.mjs` run, then all pass and gofmt prints nothing; the new engine tests also pass with `-race -count=20`.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 16 findings — high 0, medium 0, low 11, false 5, maybe-false 0
- findings:
  - `[low]` `[patch]` (blind) No test shows snapshot failures continuing past the reconnect (start reads failing through `retry()`) — added `TestViewSnapshotFailuresOutlastReconnect`: bound+2 failures, 4 pings, no `connected: true` between the disconnect and the recovery.
  - `[false]` `[reject]` (blind) Unexpected lines cause unbounded snapshot reads — `requestSnapshot` coalesces with a read in flight or scheduled and honours `snapshotMinInterval`, exactly as for the frequent known context events (`pane_updated`, `layout_updated`), so the load is no worse than today's.
  - `[low]` `[reject]` (blind) Warn-once per connection hides a second distinct unknown kind — needs a per-name set; a second new kind on one connection is unlikely, and every line is still logged at debug.
  - `[low]` `[patch]` (blind) No-data/no-pane-id lines are unexpected while mistyped data stays undecodable, without a written reason — the split follows the retro (L4) and `errUnexpectedEvent`'s definition; extended its comment in `internal/herdr/map.go` to state it.
  - `[false]` `[reject]` (blind) The status stream still flaps — an unexpected status line ends one pane's subscription and leads to a resync of that pane, at most once per `resyncMinInterval`, not a Daemon reconnect; the story names lifecycle events only.
  - `[false]` `[reject]` (blind) The status consumer has no guard against `Unexpected` — `SubscribeStatus` passes `passUnexpected=false`, so no status delivery can carry it.
  - `[low]` `[reject]` (blind) Untested: unexpected line before ready or during a resync, `Close` racing an unexpected delivery — both go through existing guarded paths (`requestSnapshot` sets `again`; `deliver` returning false returns as every other branch does); tests would add harness machinery for paths a developer is unlikely to meet.
  - `[low]` `[patch]` (blind) The "snapshot failed" warn shows no progress toward the bound — added `failures` and `lost_at` fields.
  - `[low]` `[patch]` (blind) `lost()`'s doc gives the lifecycle reason for the snapshot cause — reworded to name each cause.
  - `[low]` `[reject]` (blind) The new tunable sits outside the documented var block and has no <1 guard — kept as its own `var` on purpose so the shared block is not realigned while Story 5.4 edits the same file; it is not configurable, so the guard has no caller.
  - `[false]` `[reject]` (blind) The "reads before the loss" check does not pin the bound — the test fails exactly the bound's reads and then asserts exactly 2 pings; an early escalation would make the reconnect's read fail and cause a third ping.
  - `[low]` `[reject]` (edge) A known context kind with no data is logged at warn as unexpected — it still leads to the snapshot a context event would cause; herdr 0.9.5's schema always carries data, so only the log level differs.
  - `[low]` `[reject]` (intent) The Board surface is not exercised for this cause — the engine publishes the same `ChangeConnection` with `connected: false` as `TestViewLost`, and the Board's reaction to it is cause-agnostic and tested since Story 3.9; Story 5.11 adds the publication-replay test through `applyEvent`.
  - `[low]` `[reject]` (intent) No single test crosses from the herdr client to the engine — each side is tested against the `Delivery.Unexpected` contract; an end-to-end socket test would need a new harness.
  - `[low]` `[reject]` (intent) Failures started by the reconcile timer alone are not driven by a test — the reconcile read goes through the same `onSnapshot` branch as the tested event-driven reads.
  - `[false]` `[reject]` (intent) "Repeated failures" narrowed to snapshot reads after ready — the story's acceptance criteria name snapshots after startup; failed starts already back off through `retry()` with `connected: false`.

## Design Notes

Why not an ADR: CLAUDE.md requires one for "a change to how herdr is spoken to". Nothing herddash sends changes: the same ping, snapshot and subscriptions. What changes is how the client classifies lines it already receives, and the engine reacts with a snapshot read it already makes on any context change. The escalation reuses `lost()`, the existing reconnect path, so the record shows the gap with the existing `reconnect` marker.

Bound: 3 consecutive failures. One failure stays a blip (today's test); with `snapshotRetryDelay` 2 s, a herdr that cannot snapshot shows as lost within seconds when reads fail fast, and within about 3 × `snapshotTimeout` when they hang.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- expected: pass
- `gofmt -l .` -- expected: no output
- `node --test internal/httpapi/web/*.test.mjs` -- expected: pass
- `go test -race -count=20 -run '<new engine tests>' ./internal/engine/` -- expected: pass

## Auto Run Result

Status: done

**Summary.** Once ready, `snapshotFailuresBeforeLost` (3) failed snapshot reads in a row are handled as a lost connection through the existing `lost()` path: ConnectionTruth publishes `connected: false` (Agents kept), the engine reconnects with backoff, and the next good read publishes `connected: true` and records the reconnect. A well-formed but unexpected lifecycle line (unknown kind, no data, no pane id) no longer ends the lifecycle subscription: it arrives as a non-final `Delivery.Unexpected`, and the engine logs it (warn once per connection, then debug) and requests a snapshot. Status subscriptions and unparsable lines are unchanged. No ADR: nothing herddash sends herdr changed.

**Files.**
- `internal/engine/tunables.go` -- `defaultSnapshotFailuresBeforeLost` and its test-lowerable var.
- `internal/engine/engine.go` -- failure counter and escalation in `onSnapshot`, resets, `Unexpected` handling in `onLifecycle`, reworded `lost()` log and doc, package doc.
- `internal/engine/client.go`, `internal/herdr/doc.go` -- doc comments.
- `internal/herdr/subscribe.go` -- `Delivery.Unexpected`, lifecycle-only pass-through in `stream`.
- `internal/herdr/map.go` -- `errUnexpectedEvent` comment states the unexpected/undecodable split.
- `internal/herdr/subscribe_test.go` -- `TestLifecycleUnexpected`; three cases moved out of `TestLifecycleUndecodable`.
- `internal/engine/connection_test.go` -- `TestViewSnapshotFailuresLoseConnection`, `TestViewSnapshotFailuresBelowBound`, `TestViewSnapshotFailuresOutlastReconnect`, `TestViewUnexpectedLifecycleLine`; `internal/engine/fake_test.go` -- `fakeSub.unexpected`.

**Review.** 16 findings: 4 patched (all low), 0 deferred, 12 rejected with reasons in the triage log. Follow-up review recommended: false (patched: high 0, medium 0, low 4).

**Verification.** `go build ./...`, `go vet ./...`, `go test ./...` pass; `gofmt -l .` prints nothing; `node --test internal/httpapi/web/*.test.mjs` 75/75; `go test -race -count=20` on the new engine tests plus `TestLaterSnapshotFailureIsRetried` and `TestViewLost`, and on `TestLifecycleUnexpected`, `TestLifecycleUndecodable`, `TestSubscriptionHealth`, passes.

**Residual risks.** A read that hangs until `snapshotTimeout` escalates only after about 3 x 30 s; untested, same code path. An unexpected status line still ends that pane's stream and resyncs it (unchanged, out of this story).
