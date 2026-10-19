---
title: 'Story 1.7: Resync when the stream cannot be trusted'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: 'c31f9e111daf8dd552827368b6ec8d011af65555'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/implementation/spec-1-6-reconnect-forever-and-know-when-herdr-restarted.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** When a status subscription reports a health signal or a subscribe is rejected, the
engine asks for a snapshot and resubscribes. Nothing says this happened, nothing serialises
these recoveries, and a pane herdr lists but refuses to subscribe loops without a bound. No
reconcile timer catches drift that no signal announced (FR-7, AD-11).

**Approach:** Make resync a named engine operation: one at a time, no closer together than a
minimum interval, announced to the Sink. Add a reconcile timer that snapshots regardless.

## Boundaries & Constraints

**Always:**
- **Triggers:** any health signal on a status subscription (`events_lost`, `error`, `closed`,
  `undecodable`), a rejected or failed status subscribe, and a snapshot that shows a tracked
  Agent with no live or pending subscription. Many signals at once cause one resync.
- **A resync:** report `Resynced(cause, observedAt)` to the Sink once, as it begins. Resubscribe
  every tracked pane without a subscription. Once every one of those attempts has settled, read a
  snapshot that started after they settled, and apply it as usual. Disagreements print as
  `snapshot` Transitions; a resubscribed Agent whose Status is unchanged prints nothing. The resync
  ends when that snapshot is applied.
- **Guard:** at most one resync runs. A trigger during one causes exactly one more after it. A
  resync starts no sooner than `resyncMinInterval` after the previous one began. A trigger inside
  that interval is scheduled, not dropped.
- **Bounded rejection:** `apply` subscribes only panes it starts tracking. A known pane left
  without a subscription is resubscribed only by a resync. So a pane that herdr keeps rejecting
  costs one subscribe per `resyncMinInterval`.
- **Reconcile:** while connected and ready, the engine requests a snapshot every
  `reconcileInterval`, using the existing snapshot guard (no resync line).
- Both tunables are named defaults in `tunables.go` behind package variables tests can lower. The
  defaults are `defaultResyncMinInterval` = 5 s and `defaultReconcileInterval` = 60 s.
- Every resync and reconcile timer message carries the connection tag. A reconnect cancels any
  resync in progress or scheduled.
- Lifecycle-stream signals keep Story 1.6's reconnect, which already resubscribes, snapshots and
  replaces state. Its `Reconnected` report is the marker for that case.
- The `terminal replaced` rule applies to resync and reconcile snapshots. Only the first snapshot
  after a reconnect decides the epoch.

**Never:**
- Replay buffered events onto a snapshot. Events arriving during a read still cause another read.
- Persist resync markers (Epic 2), gate on version (Story 1.8), or touch a live herdr in tests.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| events_lost on one pane | w1:p1 status stream ends `events_lost`, Status unchanged | one `resync cause=events_lost`, w1:p1 resubscribed, then a snapshot, no Transition | none |
| Disagreement | as above; snapshot shows w1:p1 `idle` (was `working`) | resync line, then `snapshot` Transition working→idle | none |
| Many drop at once | 3 panes' streams end together | one resync, 3 resubscribes, one snapshot | none |
| Unknown error code | stream ends `error` with code `weird_new` | resync, no crash | none |
| Trigger during resync | second signal while one runs | exactly one more resync after it, no sooner than the min interval | none |
| Min interval | two signals 10 ms apart, resyncs finishing fast | second resync begins ≥ `resyncMinInterval` after the first began | none |
| Persistent rejection | herdr lists w1:p2 but rejects every subscribe | subscribes to w1:p2 no more often than once per min interval | none |
| Pane gone | rejected pane absent from the resync snapshot | removed, resync ends, no further resync | none |
| Events mid-read | status event arrives while the resync snapshot is in flight | another read follows; the event is not replayed | none |
| Reconcile | interval elapses, no signals | snapshot read; any difference is a `snapshot` Transition | none |
| Reconnect mid-resync | lifecycle stream lost during a resync | resync abandoned; reconnect proceeds as Story 1.6 | none |

</frozen-after-approval>

## Code Map

- `internal/engine/engine.go` -- `onStatus` health branch and `onSubscribed` error branch call `requestSnapshot` today; route them to a new `requestResync(cause)`. `apply`'s closing `if p.sub == nil && !p.subscribing { e.subscribe(p) }` is the unbounded loop: subscribe only panes `apply` just tracked, and request a resync for known panes left unsubscribed. Reuse `requestSnapshot`/`startSnapshot`/`again`/`removedDuring` for the read. Resync state (running, again, scheduled, last start, pending subscribes, `afterSeq`) goes beside the snapshot state and is reset in `disconnect`. Start the reconcile timer when `ready` first becomes true for a connection; tag it with `conn`.
- `internal/engine/tunables.go` -- add the two defaults and package variables.
- `internal/engine/sink.go` -- add `Resynced(cause string, observedAt int64)`.
- `internal/engine/fake_test.go` -- `fakeSub.health`, `reject`, `setSnapshot`, `awaitSubscribe` and `callList` exist; the `recorder` needs `Resynced`.
- `internal/engine/engine_test.go` -- existing tests that expect a reject→resubscribe at the snapshot interval may need the lowered resync interval.
- `cmd/herddash/serve.go`, `serve_test.go` -- printer gains `resync     cause=<cause>` in the same column and key=value form.
- `internal/herdr/subscribe.go` -- `HealthKind` values are the causes. Do not change it.

## Tasks & Acceptance

**Execution:**
- [x] `internal/engine/engine.go`, `tunables.go`, `sink.go` -- resync operation, guard, min interval, reconcile timer, bounded resubscribe
- [x] `internal/engine/engine_test.go`, `fake_test.go` -- every matrix row with lowered intervals, under `-race`
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- resync line

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes, arch_test passes, and Story 1.5 and 1.6 tests still pass.

## Design Notes

A pane's status stream that closes when the pane closes triggers a resync if the close arrives
before the lifecycle `pane_closed`. The resync line is then truthful but redundant. Telling the
two apart would need evidence the client does not give, so it is accepted.

## Implementation Notes

- **Joining before the read.** A trigger that arrives while a resync is still waiting on its
  resubscribes, or before its snapshot read has started, joins it: the newly unsubscribed pane is
  subscribed as part of it and the read waits for that too. Only a trigger after the read has
  started causes one more resync. This is what makes "many drop at once" one resync rather than
  two: the drops arrive one inbox message at a time.
- **Per-pane bound.** A failed subscribe made outside a resync (by `apply` or a lifecycle event)
  marks the pane, and the resync that failure triggers skips it once, clearing the mark. Without
  it, a pane rejected by `apply`'s initial subscribe would be retried immediately by that resync,
  so two attempts would land inside one interval. The snapshot that ends the resync then finds the
  pane unsubscribed and schedules the next resync, at least `resyncMinInterval` later, which
  subscribes it. A failure of a resync's own subscribe is not marked, so a pane herdr keeps
  rejecting is retried once per interval. A pane whose established stream ended is not delayed.
- **Causes.** A status health signal reports its `HealthKind`. A failed subscribe reports the kind
  of the `*herdr.Health` it wraps (`rejected` for herdr's error answer), or `closed` when the
  attempt failed before herdr answered (a timeout). A snapshot that finds an unsubscribed tracked
  Agent reports the cause that pane last recorded.
- **Rejected unknown panes.** A rejected subscribe for a pane detected by an event and not yet
  reported also triggers a resync, as the spec's trigger list says. Before this story it requested a
  plain snapshot. The pane is dropped as before, so the resync only reads.
- **Reconnect.** The first snapshot after a (re)connect subscribes every tracked pane without a
  subscription, as Story 1.6 did. `apply` no longer resubscribes in any other snapshot.
  `disconnect` clears all resync state, and resync and reconcile timers carry the connection tag.
- **Reconcile** starts when a connection becomes ready and stops when its context ends. It goes
  through `requestSnapshot`, so it shares the snapshot guard and minimum interval and prints
  nothing of its own.
- `TestRejectedKnownPaneIsResubscribed` had a latent race: it lifted the rejection after the
  subscribe call was recorded but possibly before the fake checked it. The race was reproducible
  under `-race -cpu=8`. It now waits for the resync line first.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | ECH | A resync can start while connected but before the first snapshot: Resynced precedes Reconnected and old-epoch panes are resubscribed before `decideEpoch` | medium | Reachable: a pane detected by a lifecycle event before the first snapshot has its subscribe rejected; patch: `requestResync` and `resyncDueMsg` require `ready` |
| 2 | ECH | `resyncDueMsg` handled while connected but not ready | medium | Same root cause as 1; patch with 1 |
| 3 | BH, VG | The per-pane bound measures from when the attempt began, so the next resync may skip the pane too (rate halved) and a timed-out attempt is never skipped | medium | `subscribedAt` set in `subscribe`, after `resyncLastStart`; the test's lower bound accepts the halved rate; patch: a flag set only by a failure outside a resync, consumed by the next resync |
| 4 | ECH | A first rejection prints two resyncs, the first of which subscribes nothing | low | Real, but the first resync's read is the useful one: in the common case (pane closed) it removes the pane and no second resync follows; rejected |
| 5 | BH | A trigger that joins a running or scheduled resync loses its cause | low | Real; `events_lost` is unreproduced and the joined trigger is still covered by the resync; merging causes adds branches; rejected |
| 6 | BH | A pane herdr keeps rejecting costs a resync line, a log line, a subscribe and a read every interval, forever | low | Bounded as the deferred item asked; persistent rejection of a listed pane is unobserved; a backoff adds state; rejected |
| 7 | BH | `p.cause` is not cleared after a successful subscribe | false | Every path that leaves a known pane unsubscribed (status health, failed subscribe) sets `cause` first; after a reconnect the first snapshot resubscribes every pane, so a stale cause is never reported |
| 8 | BH | `scheduleResync` waits on `runCtx`, so after a disconnect it sends a stale message | low | Discarded by the `conn` tag; direct fix; patch |
| 9 | BH | The deferred-work entry for the rejection loop is still listed | low | The workflow forbids modifying existing deferred entries; the PR notes it is resolved; rejected |
| 10 | BH | The new tunables are not documented with the configuration | low | Pre-existing: Story 1.6's backoff tunables are not either; defer |
| 11 | BH | Spec status and sprint status disagree | false | Workflow ordering: the spec moves to in-review before the tracker is synced at finalize |
| 12 | BH, VG | `TestResyncMinInterval` decides its branch by a 10 ms sleep | medium | A slow `-race` runner joins the second signal to the first resync and the test times out; patch |
| 13 | BH | `TestResyncPersistentRejection` windows are tight, and its failure message can panic | low | Direct test edit; patch |
| 14 | VG | `onLifecycle`'s `resyncSettle` after a removal is never exercised | medium | VG pre-verified: deleting it fails no test and stalls a resync until the reconcile tick; patch |
| 15 | VG | The reconcile timer is never tested after a reconnect | medium | VG pre-verified: starting it only once passes every test; patch |
| 16 | VG | `subscribeCause`'s non-Health fallback is untested | low | VG pre-verified; a small table test; patch |
| 17 | BH | Untested: reconcile after reconnect, `requestResync` while disconnected, reconcile finding an unsubscribed Agent, a health signal before `ready` | low | Reconnect and before-ready covered by 1 and 15; the others go through the same `apply` path the resync tests cover; rest rejected |
| 18 | BH | Two edited doc comments in `serve.go` overrun the line width | low | Direct reflow; patch |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
