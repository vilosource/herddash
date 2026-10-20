---
title: 'Story 3.2: The engine publishes views and change lists'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: '8fbb240c846156fcfe1ecf83ac776481ac46407c'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/implementation/spec-3-1-each-agent-s-view-context-line-and-order.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The API and the event stream must read one consistent picture of every Agent and of
the connection to herdr, without locking engine state. Today the engine only calls its Sink
(AD-2, AD-7).

**Approach:** After handling any inbox message that changed something visible, the engine builds
and publishes an immutable `View` together with the change list that produced it. Readers take
the latest View atomically. Subscribers receive each publication in order on a bounded channel,
which is closed on overflow.

## Boundaries & Constraints

**Always:**
- **core:** `ConnectionTruth{Connected bool; LastSnapshotAt int64; HerdrVersion string;
  HerdrMinimum string; Gate Gate; ResyncGuaranteed bool}`.
  - JSON fields: `connected`, `last_snapshot_at` (omitted when 0), `herdr_version` (omitted when
    empty), `herdr_minimum`, `gate` (omitted before the first gate decision) and
    `resync_guaranteed`.
- **engine.View:**
  - Fields: `RunID string`, `Seq uint64`, `Agents []core.AgentView` (sorted with
    `core.SortViews`), `Connection core.ConnectionTruth`, plus the header indexes from
    `core.WorkspaceHeaders`.
  - It is never mutated after publication: each View owns fresh slices.
  - `RunID` is 16 random bytes in hex, made in `New`. `Seq` starts at 1 with the first
    publication and goes up by exactly one per publication.
- **Change list.** `[]Change`, each with a `Kind` and the fields that kind needs:
  - `agent`: an Agent first seen, or any change to its view (Profile, context, program).
    Carries its new AgentView.
  - `removed`: carries the AgentKey and the last AgentView.
  - `transition`: carries the `core.Transition` and the Agent's new AgentView.
  - `resync`: carries the reason and detail, published when the engine reports `Resynced`.
  - `connection`: carries the new ConnectionTruth whenever any of its fields changes.
  - Changes are listed in the order they happened within the message.
- **When to publish:** at the end of handling each inbox message, if its change list is not empty.
  Waiting Time growing on its own is never a change.
- **ConnectionTruth source:**
  - `connected` is true from a connection's first applied snapshot until it is lost.
  - `last_snapshot_at` is the engine clock at each applied snapshot.
  - `herdr_version` and `gate` are set from each connect's gate decision; `herdr_minimum` is
    `core.HerdrMinimum`.
  - `resync_guaranteed` comes from `DecideGate`.
- **Refused gate:** no AgentViews (an empty list), and `connected` is false.
- **AgentView input:**
  - The engine pane's key, program, Profile, Status, fold and context.
  - `Options.Home`: the user's home directory for `ShortenDir`. Empty means unchanged. serve
    passes `os.UserHomeDir()`.
  - Only reported (known) Agents are in the View.
- **API on Engine:**
  - `View() View` is lock-free, through `atomic.Pointer`. It returns the latest View, or a View
    with Seq 0 and the run id before the first publication.
  - `Subscribe(buffer int) (*Subscription, func())`. The subscription has a channel `C` of
    `Publication{View View; Changes []Change}`. Each publication is delivered in order without
    blocking the engine. When the channel is full, the engine closes it and drops the
    subscription (AD-7: the client reconnects to fresh state). The func unsubscribes and is
    idempotent.
  - Both are safe from any goroutine.
- The Sink is unchanged, and serve's printed output is unchanged.

**Never:**
- Let a reader lock or read engine state directly.
- Mutate a published View.
- Keep publication history.
- Add HTTP (Story 3.3).
- Touch a live herdr.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| First start | engine connects, 2 Agents | publications: connection (connected), then agents; Seq 1, 2, … with no gaps | none |
| Transition | w1:p1 working→blocked | one publication: `transition` change plus the new view; the Agent's view has class needs_you | none |
| Removal | pane closed | `removed` change; the Agent is absent from the View | none |
| Resync | events_lost | a `resync` change; then any snapshot changes | none |
| Lost | lifecycle lost | `connection` change with connected false; Agents kept | none |
| Snapshot | reconcile read, nothing changed | `connection` change (last_snapshot_at) only | none |
| Refused | ping 0.8.2, flag off | connection gate refused, connected false, no Agents | none |
| Immutable | reader holds View 3; later publications | View 3's Agents slice and values are unchanged (deep-equal to a copy taken earlier) | none |
| Run id | two engines | different run ids; each starts at Seq 1 | none |
| Slow subscriber | buffer 1, never read | channel closed after overflow; the engine is not blocked; others still receive | none |
| Unsubscribe | call twice | no panic; no further sends | none |
| Sorted | mixed classes | View.Agents in SortViews order; headers as WorkspaceHeaders | none |

</frozen-after-approval>

## Code Map

- `internal/core/view.go` (or `connection.go`) -- ConnectionTruth.
- `internal/engine/view.go` (new) -- View, Change, Publication, Subscription, the publish machinery: a change accumulator reset per message, `publish()` called at the end of `handle`, `atomic.Pointer[View]`, and a subscriber set under a mutex used only by Subscribe and publish.
- `internal/engine/engine.go`:
  - `handle` calls publish at its end.
  - The sites that add changes are `report`, `transition`, `remove`, Profile change, the `Resynced` reporting, connect/disconnect, the gate decision and the end of `onSnapshot`.
  - `Options.Home`.
- `internal/engine/*_test.go` -- tests for every matrix row; read Views through the public API.
- `cmd/herddash/serve.go` -- pass `Home`.

## Tasks & Acceptance

**Execution:**
- [x] `internal/core` -- ConnectionTruth and its JSON test
- [x] `internal/engine` -- View, publish, subscribe, change sites, tests
- [x] `cmd/herddash/serve.go` -- Home

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes (race-clean with concurrent readers) and earlier tests still pass.

## Implementation Notes

- **`ViewSubscription`, not `Subscription`.** `engine.Subscription[T]` already names the herdr
  subscription interface, so the type `Subscribe` returns is `*ViewSubscription`; its shape
  (`C <-chan Publication`) is as specified. `Subscribe` clamps a buffer below 1 to 1. The
  unsubscribe func closes `C` if the engine has not already.
- **Change sites.** `report` (agent), `transition` (transition, carrying the view after it),
  `remove` (removed, with the view as it last was), a Profile change (agent), `startResync`
  (resync), the gate decision in `onConnect`, `disconnect` (connected false) and each applied
  snapshot (connected, `last_snapshot_at`). Context and program changes are noted in `apply`
  per pane, in snapshot order, and `publish` re-checks every reported Agent's view at the end of
  each message as a catch-all, so nothing visible is missed. A pane's last noted view is kept on
  the pane; each View is built from those into a fresh slice and sorted.
- **Connection before agents.** The snapshot's connection change is noted before `apply`, after
  the epoch rule's removals, so a first start publishes `connection` then the `agent`s (matrix
  row "First start").
- **One more clock reading per applied snapshot** (`last_snapshot_at`). Two existing tests that
  counted clock readings were updated: `TestLiveChange` and `TestRecordFirstSightAfterHeldResolution`.
- **Shutdown.** When Run returns, a still-connected truth is published as `connected: false`,
  then every subscription is closed and dropped; `Subscribe` after that returns a closed channel
  and a no-op unsubscribe.
- **`removed` carries the last noted view**, falling back to a fresh one only if none was noted.
- **View JSON tags** (`run_id`, `seq`, `agents`, `connection`, `headers`) are set for Story 3.3's
  use; nothing serialises a View yet.
- **`View()` before any publication** returns Seq 0, the run id, empty (non-nil) `Agents` and
  `Headers`, and a ConnectionTruth with only `herdr_minimum`.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH | Subscriber channels are never closed when the engine stops; Subscribe after stop hangs | medium | Story 3.4's stream handlers would block at shutdown; patch: final disconnected publication, then close every subscription |
| 2 | ECH | View keeps `connected: true` after the engine stopped | low | Patch with 1 |
| 3 | VG, BH | `agent` changes for an already-shown Agent (Profile, context, program) are untested | medium | VG pre-verified: skipping re-notes passes every test; patch |
| 4 | BH | A refused gate's `connected: false` depends on message ordering | low | Direct; patch |
| 5 | ECH | `removed` carries a recomputed view, not the last published one | low | Direct; patch |
| 6 | ECH | Catch-all agent changes are in pane-id order; shared slices across subscribers | low | Documented as read-only and ordering stated; patch |
| 7 | BH | `TestViewUnsubscribe`'s post-close check cannot fail | low | Patch |
| 8 | BH | No concurrent Subscribe/unsubscribe test | low | Patch |
| 9 | BH | Test comparator never returns 0 | low | Patch |
| 10 | BH | serve's Home fallback is redundant and silent | low | Patch |
| 11 | BH | ConnectionTruth cannot say "never reached" vs "reconnecting" vs "refused" | low | The spine fixes ConnectionTruth's fields; `connected`, `gate` and `last_snapshot_at` distinguish refused, and Story 3.9's strip decides presentation; rejected |
| 12 | BH | `TestLiveChange` weakened to inequalities by an extra clock read | low | It still pins order and spacing; the extra read is `last_snapshot_at`; rejected |
| 13 | BH | `Change` has no JSON tags | low | Serialising change events is Story 3.4's (AD-7: `httpapi` turns the list into events); rejected |
| 14 | BH | `publish` rebuilds every view on every message | low | Tens of Agents; rejected |
| 15 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
