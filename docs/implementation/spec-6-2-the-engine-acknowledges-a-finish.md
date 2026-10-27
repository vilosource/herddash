---
title: "Story 6.2: The engine acknowledges a finish"
type: 'feature'
created: '2026-10-27'
status: 'done'
baseline_revision: '714c8606ccdf9c28378d3cc0d1742e5032f68906'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0014-acknowledging-a-finished-agent.md'
  - '{project-root}/docs/implementation/spec-6-1-acknowledgements-in-the-record.md'
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Story 6.1 put acknowledgements in the record and the rule that reads them in core, but
nothing writes them and no view carries `seen` yet. The single writer (AD-2) must accept an
acknowledgement only for the exact finish the operator is looking at, write it all or nothing, and
derive the seen state again from the record after a restart or a reseed.

**Approach:** ADR-0014's engine part. `engine.Engine.Acknowledge(ctx, AckRequest)` sends one inbox
message with a reply channel, as `Agents` does. The actor validates every item against its own
state before writing anything, writes the rows through one `store.AppendAcknowledgements`, keeps the
acknowledged class start per pane (`pane.seenSince`), publishes once and replies with the views in
request order. A pane's seen state is in force while its fold is Finished from that same instant, so
any Transition that moves the class or its start ends it with no read and no row; `done` to `idle`
within the finish keeps it. Wherever a pane's fold is seeded from the record (its first sight), the
engine reads `store.Acknowledgements` and applies `core.Seen`.

## Boundaries & Constraints

**Always:** AD-2: all state changes on the engine goroutine; the caller only sends a message and
waits. Every item validated before any write; one store call per request. A refused or failed
request changes no state and publishes nothing. One publication per successful request, one
`ChangeAgent` per Agent whose view changed; no new change kind and no new SSE event. Each written
row is logged at info with pane, since and seen. `AppendAcknowledgements` stays engine-only
(`callsite_test.go`); no new import edge (`arch_test.go`).

**Never:** Call herdr for this (no `pane.focus`, no `agent.focus`, nothing in `internal/herdr`).
Change the stored schema, the Sink interface, Retention's rule or the HTTP surface (Story 6.3).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error |
|----------|--------------|---------------------------|-------|
| Mark | Finished Agent, its view's `waiting_since` | one row; view `seen`, rank 2; one publication with one `agent` change; reply is that view | — |
| Mark again | already seen | no row, no publication; reply the view | — |
| Stale | `waiting_since` 1 ms off; Profile hold on the pane; fold not in the record (`unrecorded`) | nothing written or published | `*StaleError` naming the keys, `errors.Is(ErrStaleView)` |
| Unknown | unknown pane; older epoch; another host or server; gate refused (no panes) | nothing | `ErrUnknownAgent` |
| Not finished | Needs you, Idle (never worked), Working, Background | nothing | `ErrNotFinished` |
| Bad request | no items; a key twice | nothing | `ErrBadRequest` |
| Bulk, one stale | three items, one off | nothing written for any | `*StaleError` naming that key |
| Undo | seen Agent | one `seen = 0` row; view Finished, rank 1 | — |
| Undo, nothing to undo | not seen, or moved on (`working`) | no row, no publication; reply the view | — |
| done to idle, idle to done | seen Agent | still seen | — |
| working, blocked, new Agent's first row, epoch increment | seen Agent | not seen; no row written; finishing again is unseen | — |
| Restart, Status unchanged or done to idle | seen in the record | seen, same `waiting_since` | — |
| Restart, Status changed | seen in the record | not seen | — |
| Acknowledgements read fails at a seed | — | logged at error, Agent unseen | — |
| No store, or state not loaded (recording off for the run) | any request | nothing | `ErrRecordingOff` |
| Store write fails | any write | nothing changed or published | wraps the store's error |

</intent-contract>

## Code Map

- `internal/engine/acknowledge.go` -- new: `ErrBadRequest`, `ErrUnknownAgent`, `ErrNotFinished`,
  `ErrStaleView`, `ErrRecordingOff`; `AckRequest`, `AckItem`; `StaleError` (`Keys`, `Is` matching
  `ErrStaleView`); `Engine.Acknowledge`; the actor's `onAcknowledge` (publishes before it replies,
  so a caller that reads the View after its reply sees its change) and `acknowledge`; `seenNow`,
  `clearStaleSeen`, `seedSeen`.
- `internal/engine/engine.go` -- `pane.seenSince`; `handle` dispatches `ackMsg`; `commit` clears
  the acknowledgement on a first row and whenever the fold leaves the acknowledged finish;
  `firstSight` calls `seedSeen` after seeding the fold from the record, on both the unchanged and
  the recorded-change paths (the latter so that `done` to `idle` across a restart keeps it).
- `internal/engine/view.go` -- `viewOf` passes `Seen: seenNow(p)` to `core.AgentViewInput`.
- `internal/engine/recorder.go` -- `Recorder` gains `AppendAcknowledgements` and
  `Acknowledgements`; `*store.Store` already provides both (Story 6.1).
- `internal/engine/retention.go` -- the `retention sweep` line reports `acknowledgements`
  (`SweepResult.Acknowledgements`), deferred from Story 6.1.
- `internal/engine/fake_test.go` -- `fakeStore` keeps acknowledgements in recording order, fails
  either call on demand, and sweeps them by the store's rule.
- `internal/engine/acknowledge_test.go` -- new: the tests below.

## Tasks & Acceptance

**Execution:** all done, as mapped above, with these tests against the fake herdr:

- `TestAckMark`: one row, reply `seen` with rank 2 equal to the View, exactly one publication with
  one `agent` change carrying the reply's view; marking again writes and publishes nothing.
- `TestAckRefused` (epoch 3 kept, so an older epoch exists): 1 ms off is `ErrStaleView` and a
  `*StaleError` naming the key; unknown pane, older epoch, another host and an undo of an unknown
  pane are `ErrUnknownAgent`; Needs you, Idle and Working are `ErrNotFinished`; no items and a key
  twice are `ErrBadRequest`; none writes or publishes (View `Seq` and row count checked after a
  round trip).
- `TestAckRefusedDuringProfileHold`: a redetection holds the pane's rows (lookup blocked); a mark is
  `ErrStaleView`, writes and publishes nothing; once the lookup settles, the same finish is marked.
- `TestAckRefusedBackground`: a done Agent with a tool shell child (Background) is `ErrNotFinished`.
- `TestAckBulkAllOrNothing`: three Finished Agents, one stale: the error names that key alone and
  nothing is written; then the bulk mark succeeds with the views in request order, and a bulk undo
  writes three `seen = 0` rows.
- `TestAckLastsUntilTheFinishEnds`: `done` to `idle` and back keep it; `working` ends it and the
  next finish is unseen (rank 1); `blocked` ends it; a new Agent in the pane (another terminal,
  first row with no previous Status) is not seen; an epoch increment ends it; only the marks are
  written.
- `TestAckUndo`: undo writes `seen = 0` and the view returns to rank 1 with an `agent` change; an
  undo again, and an undo after the Agent went `working`, succeed, write nothing and publish
  nothing.
- `TestAckRecordingOff`: no store, and a store whose saved state failed to load, are
  `ErrRecordingOff`.
- `TestAckStoreFails`: a failing `AppendAcknowledgements` returns its error; the View and the next
  publication (a transition) are as if no request was made.
- `TestAckInRunReconnect`: a reconnect keeping the epoch with the Status unchanged keeps it seen; a
  second showing `working` ends it; a pane first tracked by that reconnect is seeded from the
  record with its acknowledgement (shown `idle`, seen, since 200).
- `TestAckRestartFromRecord` (fake store): restart showing `done` or `idle` is seen with
  `waiting_since` 200 (an older finish's row ignored); `working` or `blocked` is not; a failing
  acknowledgements read is logged at error and leaves it unseen; a restart writes no row.
- `TestAckRestartRealStore`: the real `store.Store`, closed and opened again between Daemon runs.
  Two finishes are marked, the clock jumps three days and a one-day Retention sweep removes the
  older finish's acknowledgement (logged `acknowledgements: 1`) and keeps the current one's. Then a
  new engine on the same database: `done` is seen with the same `waiting_since`; `idle` is seen;
  `working` is not; the next `done` is a new finish, not seen.
- `TestAckRetentionLogsAcknowledgements`: the `retention sweep` line reports `acknowledgements`
  beside `transitions` and `resync_markers`.
- `TestAckConcurrent`: 8 goroutines send 25 requests each (single and bulk, marks and undos, often
  stale) while a ninth sends random status events for the same four panes. Every reply is views
  (one per item) or `ErrStaleView`/`ErrNotFinished`; at least one is accepted; after settling,
  each Agent's `seen` and `waiting_since` equal `core.Seen` and `core.Fold` of its record.

**Acceptance Criteria:** every criterion of Story 6.2 in `docs/planning/epics.md` is met by the
tests above.

## Spec Change Log

## Review Triage Log

### 2026-10-27 — Self-review pass
- `[low]` `[patch]` The reply was sent before the publication (`handle` publishes in a defer), so
  a caller reading the View right after its reply could see the old one. `onAcknowledge` now
  publishes before it replies; `TestAckMark` asserts the View equals the reply.
- `[low]` `[accept]` An undo accepted while the pane's rows are held for a Profile lookup records
  its row and clears the state, but the published view keeps `seen` until the hold releases, as
  every view does during a hold (Story 5.4); the reply carries that view. The release's
  publication shows it unseen. Documented here, no story needed.
- `[low]` `[accept]` `clearStaleSeen` is redundant with `seenNow`'s comparison of the fold's
  `Since` (a mutation removing either leaves the tests green; removing `seedSeen` turns three
  restart and reseed tests red). It is kept so no stale class start lingers on a pane.
- `[false]` `[reject]` A mark while herdr is unreachable (reconnecting) is accepted against the
  engine's last state: ADR-0014 has the engine decide against its own state, and the reconnect's
  first snapshot ends the acknowledgement if the Status moved.

## Design Notes

The seen state is one integer per pane, the acknowledged class start, rather than a boolean, so it
can never outlive its finish: `seenNow` requires the fold to be Finished from that exact instant.
That makes `done` to `idle` keep it (neither class nor `Since` moves) and every other Transition end
it, which is core's rule restated on the engine's in-memory fold; the concurrency test checks the
two agree with the record. A mark is refused as stale while a hold is in progress or the pane's
fold may not match the record (`unrecorded`), because the row would name a finish the record could
fold differently after a restart. The acknowledgements read at a seed is not cancelled by shutdown,
for the reason `History` and `Sweep` are not.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test ./...` and `go test -race ./...` -- all 12 packages pass (arch_test.go and
  callsite_test.go included)
- `go test -race -run TestAckConcurrent -count=50 -cpu=1,4,8 ./internal/engine/` -- pass (33 s)
- `go test -race -run TestAck -count=50 ./internal/engine/` -- pass (161 s)
- `node --test internal/httpapi/web/order.test.mjs` -- 91 pass, 0 fail

## Auto Run Result

Status: done

**Summary.** The engine accepts an acknowledgement only for the exact finish its own state holds,
writes a request's rows in one store call or none, and publishes once with an `agent` change per
changed view; refusals are typed (`ErrBadRequest`, `ErrUnknownAgent`, `ErrNotFinished`,
`*StaleError`/`ErrStaleView`, `ErrRecordingOff`) and change nothing. The seen state is derived from
the record whenever a fold is seeded from it, survives a restart or kept reconnect only while the
Status shows the same finish, and ends without a write at any Transition that moves the class or its
start. herdr is never called for it. The HTTP routes are Story 6.3.
