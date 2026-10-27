---
title: "Story 6.7: The engine marks a finish Seen when the operator leaves its pane"
type: 'feature'
created: '2026-10-27'
status: 'done'
route: 'dispatch'
baseline_commit: 'a9da463'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0015-seen-when-you-leave-the-pane.md'
  - '{project-root}/docs/research/herdr-focus-events-2026-10-27.md'
  - '{project-root}/docs/implementation/spec-6-6-read-herdr-s-focus-and-record-where-a-mark-came-from.md'
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: []
deferred: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Dogfooding Epic 6 showed the operator reads results in herdr, not on the Board: they
switch to the pane, read, and switch away, and the Box stayed Finished. The operator decided (vfkb
f14fa1cdfa89, ADR-0015) that a Finished Agent becomes Seen when, after its finish, its pane was
focused in herdr for at least about 3 s and focus then moved to another pane. Story 6.6 made the
facts readable (`core.LifecycleFocused`, `Snapshot.FocusedPaneID`, `core.SeenOnLeave`, the
`herdr_focus` source); nothing acts on them yet.

**Approach:** ADR-0015's engine and view parts. The single writer holds the focused pane and its
dwell start in memory, by its own clock. A `pane_focused` for another pane is a leave, judged by
`core.SeenOnLeave` behind the Board mark's own conditions; a mark is one `herdr_focus` row written
through `store.AppendAcknowledgements`. Snapshots set or move focus without a leave. Every
`AgentView` carries `in_view`.

## Boundaries & Constraints

**Always:** herddash only reads focus: no focus call, no herdr mutation. A focus event never
requests a snapshot read and never counts as an event during one. Focus is never recorded. The
automatic rule writes only a finish's first row, so the operator's own mark or undo wins. A failed
write changes nothing. `in_view` is always sent and is not in the sort key. No new import edge;
`AppendAcknowledgements` still called only by the engine.

**Never:** No Board change (Story 6.8), no document change beyond this spec and the tracking file
(Story 6.9). No call to the live herdr server; no reinstall or restart of the running service.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Read, leave | Finished A focused, 3 s, `pane_focused` B | A in view meanwhile; then seen, rank 2, out of view; one `herdr_focus` row naming A's `waiting_since`; one publication, agent changes for A and B only | N/A |
| Short dwell | 2.999 s; or a flick A, B, C, D at 1 s each | nothing marked, no row | N/A |
| Finish while focused | A Working, focused 10 s, then `done`; leave 1 s / 3 s later | 1 s: no mark (the dwell restarted at the finish); 3 s: marked | N/A |
| Finish while away | A finishes into the focused pane; an hour of reconciles naming A | Finished, in view, no row; the next leave marks it | N/A |
| Detached | snapshots name A, no focus events | A in view, never marked | N/A |
| Not markable | Needs you, Idle, Working, Unknown, Background, already seen | no row | N/A |
| done to idle | unfocused: nothing; focused: the dwell goes on | no mark by itself; a focused 3 s still marks | N/A |
| Mark unseen | automatic mark, Board undo, leaves later and after a restart | stays Finished, no further row | N/A |
| Restart | automatic mark, Status unchanged at the first snapshot | seen, same `waiting_since` | N/A |
| Missed turn | herdr's order risen, in the run or at a restart | not seen; a 3 s leave writes a new `herdr_focus` row | N/A |
| Reconnect | focus moved while the lifecycle stream was down | no mark; the snapshot's pane in view, its dwell from then | N/A |
| Snapshot disagrees | reconcile names C while the engine holds B | focus moves to C, B not marked | N/A |
| Stale read | `pane_focused` C during a read naming B | focus stays C | N/A |
| Prompt races leave | `working` and the leave, either order | A ends Working, not seen | N/A |
| Profile hold | leave while the pane's rows are held | nothing; a later 3 s leave marks | N/A |
| Store fails | `AppendAcknowledgements` errors | logged at error, view as before; a later leave marks | logged, not retried |
| Concurrency | Board marks, undos, bulk; status and focus events; clock jumps | no race or hang; seen equals the record; at most one `herdr_focus` row per finish, none after a `board` row | typed errors only |

</frozen-after-approval>

## Code Map

- `internal/engine/tunables.go` -- `defaultSeenDwell = 3 * time.Second` and the package variable
  `seenDwell` (already listed in `docs/config.example.toml` by the planning commit).
- `internal/engine/focus.go` (new) -- `onFocus` (repeat is a no-op; another pane is a leave),
  `leave` (the Board mark's conditions plus `e.ready`, then `core.SeenOnLeave`, one
  `herdr_focus` row with herdr's order when known, info log, `noteView`), `snapshotFocus` (stale
  read ignored; reset at a first or resync snapshot; a disagreeing later snapshot moves focus,
  no leave), `restartDwell`.
- `internal/engine/engine.go` -- `focusPane`, `focusSince`, `focusEvent` on the Engine;
  `pane.rowSince`; `onLifecycle` calls `onFocus`; `disconnect` drops focus; `onSnapshot` calls
  `snapshotFocus` after `apply`; `report` and `commit` restart the dwell (class, start or a new
  Agent's first row; done to idle does not).
- `internal/engine/acknowledge.go` -- `finishHasRow`; `clearStaleSeen` clears `rowSince` too;
  the Board write sets `rowSince` and logs `source=board`; `seedSeen` sets `rowSince` by
  `core.FinishRowAt`; `missedTurn` clears it and restarts the dwell.
- `internal/engine/view.go` -- `viewOf` passes `InView`.
- `internal/core/view.go` -- `AgentViewInput.InView`, `AgentView.InView` (`json:"in_view"`, after
  `seen`, not in the sort key).
- `internal/core/acknowledgement.go` -- `FinishRowAt`, sharing `SeenAt`'s order check through
  `deciding` and `earlierTurn`.
- `internal/core/testdata/board-order.json` -- every view carries `in_view`; w4:p1 is in view
  and keeps its place.

## Tasks & Acceptance

**Execution:**
- [x] Core: `InView`, `FinishRowAt`, with `TestFinishRowAt` (marks and undos of both sources,
  an earlier turn's row refused by order, done to idle) and the golden fixture regenerated by hand
  (`TestGoldenBoard`, `order.test.mjs`).
- [x] Engine: focus state, the leave, snapshot focus, dwell restarts, `rowSince`.
- [x] `fake_test.go`: `fakeSub.sent`, `harness.focusOn` (waits until the engine handled the event),
  `focused(snapshot, pane)`, `newManualClock` and `manualClock.advance` on the existing clock.
- [x] `focus_test.go`: `TestFocusReadsNoSnapshot` (the 6.6 property, kept), `TestFocusReadThenLeave`,
  `TestFocusShortDwell`, `TestFocusDwellRestartsAtFinish`, `TestFocusDoneToIdle`,
  `TestFocusFinishWhileAway`, `TestFocusDetached`, `TestFocusNotMarked`,
  `TestFocusNotMarkedBackground`, `TestFocusMarkUnseenSticks`,
  `TestFocusMarkAcrossRestartAndMissedTurn`, `TestFocusReconnectMissedMove`,
  `TestFocusEventBeforeFirstSnapshot`, `TestFocusFromSnapshot`, `TestFocusWorkingRacesLeave`,
  `TestFocusDuringProfileHold`, `TestFocusStoreFails`, `TestFocusConcurrent`.
- [x] `internal/httpapi/engine_contract_test.go`: the engine's clock gains a test offset; w1:p4
  finishes again, is focused, 4 s pass, focus moves to w1:p2; the stream carries agent events
  with `in_view`, the replay through `order.mjs` equals every published view, and the record holds
  a fourth row, `herdr_focus`.

**Acceptance Criteria:**
- Given every criterion of Story 6.7 in `docs/planning/epics.md`, when the tests above run, then
  each is met; vet, race, gofmt, `arch_test.go` and `callsite_test.go` pass with no new import
  edge.

## Implementation Notes

- **FinishHasRow is a class start, not a bool.** `pane.rowSince` holds the start of the finish
  the record has a row for, and `finishHasRow` holds only while the fold is Finished from that
  instant, exactly as `seenSince` and `seenNow` work. It is also cleared explicitly in
  `clearStaleSeen`, at a new Agent's first row and in `missedTurn`. A bare flag would have needed
  clearing at every place the fold can leave a finish.
- **The seed needs the order check for undos too.** `core.SeenAt` returns false before its
  order check when the deciding row is an undo, so "a row not refused by the order check" needed
  a function of its own: `core.FinishRowAt`, sharing `SeenAt`'s check (`earlierTurn`) so the two
  cannot drift.
- **A stale read is judged by `snapSeq`.** `focusEvent` is `snapSeq` when the latest
  `pane_focused` arrived; a read numbered at most that started before it. ADR-0015 states this
  rule generally, and it is applied to every read, a connection's first and a resync's included.
  The story's text has those set focus from the snapshot unconditionally, but a `pane_focused`
  arriving during that read is newer than it. `TestFocusFromSnapshot/stale_read` covers a
  reconcile read; the first-read case is exercised by `TestFocusEventBeforeFirstSnapshot`.
- **Leaves are judged only once ready.** Before a connection's first snapshot the epoch is not
  decided and the Agents not confirmed. Focus is also dropped at `disconnect`, so the two guards
  overlap. Mutation checks: removing the drop alone fails the in-view-while-reconnecting check;
  removing both writes a mark and fails.
- **The dwell clock is `e.clock()`.** It is the same injected clock `observed_at` uses, so the
  tests drive it. A `time.Time` from `time.Now` keeps the monotonic reading, so a stepped wall
  clock does not lengthen a dwell.
- **Tests synchronise on the lifecycle reader.** `focusOn` pushes the event and a repeat for the
  same pane (a no-op), waits until the reader has taken both, then makes an `Agents` round trip.
  The reader takes the repeat only once the engine received the first, so the round trip follows
  its handling. It never sends anything that counts as an event during a read.
- **The concurrency test had to be paced.** Unpaced, the Board goroutines marked every finish in
  milliseconds and no `herdr_focus` row was ever written (measured: 0 in 10 runs). With a pause of
  up to 4 ms between requests, a run writes 12 to 16. The test now fails if a run writes none, so
  it cannot pass vacuously.
- **Contract test.** The change check now recognises a publication that removes and re-adds
  w1:p4 (terminal replaced) as a first sight, carried by an `agent` event. The focus step makes
  the re-add differ from the Status before it.
- Implemented in this session directly rather than through an implementation subagent: the
  orchestrating run asked for foreground work only, and the change is one engine concern.

## Spec Change Log

## Review Triage Log

### 2026-10-27 — Self-review pass (blind, edge-case and verification-gap lenses, run inline)

No reviewer subagents were run; the three lenses were applied by the implementing session against
the diff since the baseline, with mutation checks on the engine. Every finding is logged.

- `[med]` `[fixed]` `TestFocusConcurrent` passed vacuously: no automatic mark was ever written.
  Paced the Board requests and added a check that at least one `herdr_focus` row is written.
- `[med]` `[fixed]` A mutation removing the dwell restart in `commit` first went undetected
  because the mutant did not compile. Re-run correctly, it fails `TestFocusDwellRestartsAtFinish`.
- `[low]` `[fixed]` The disconnect drop and the ready check each passed alone under mutation.
  Added `TestFocusEventBeforeFirstSnapshot`; now each one is caught.
- `[low]` `[reject]` A resync resets the dwell of the pane still focused, so a resync while the
  operator reads delays the mark by up to 3 s. That is ADR-0015's decision ("Any focus held before
  the resync is dropped"), and resyncs are rare.
- `[low]` `[reject]` A later snapshot naming a different pane moves focus even when the event
  stream is healthy (one client, a no-op navigation). By design: finding 4 of the focus record, and
  only an observed `pane_focused` is a leave, so it never marks.
- `[low]` `[reject]` `docs/how-herddash-works.md`, `CLAUDE.md` and the API notes do not yet
  describe `in_view` or the automatic mark. Story 6.9's criteria name those updates.

Mutations run against `go test -run TestFocus ./internal/engine/`, each caught: the stale-read
rule removed (`TestFocusFromSnapshot`), `FinishHasRow` forced false
(`TestFocusMarkUnseenSticks`), the dwell restart in `commit` removed
(`TestFocusDwellRestartsAtFinish`), the seed of `rowSince` removed (`TestFocusMarkUnseenSticks`),
`missedTurn` keeping the row (`TestFocusMarkAcrossRestartAndMissedTurn`), the disconnect drop
removed, and it with the ready check (`TestFocusEventBeforeFirstSnapshot`).

### 2026-10-27 — Review round 1 findings triaged

Fixed 1, deferred 1, rejected 0.


- `[low]` `[fixed]` A snapshot read newer than a `pane_focused` still on its way (separate
  connections) moved focus without a leave, and the event then counted as a repeat, so a real
  read-then-leave went unmarked. `snapshotFocus` now holds such a move (`focusMove`), and the very
  next `pane_focused` naming the snapshot's pane judges the leave with the dwell held when the
  snapshot was applied; any other event, a later snapshot, a reset, a disconnect or a restarted
  dwell of either pane discards it. ADR-0015 amended in the same change. Tests: three new
  `TestFocusFromSnapshot` cases; "fresh read before its event" fails without the fix.
- `[low]` `[defer]` `in_view` toggling redraws a Finished Box whole, recreating its Seen pill, so a
  keyboard-focused pill (or a Vimium hint on it) is lost on every herdr focus move into or out of
  that pane. Story 6.8 draws `in_view` and adds `Mark unseen`; its acceptance criteria now require
  a redraw to keep focus on the same control (`docs/planning/epics.md`, committed in 8102984).
- Rejected: none.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test ./...` and `go test -race ./...` -- all 12 packages pass (`arch_test.go`,
  `callsite_test.go` at the root)
- `go test -race -count=50 -cpu=1,4,8 -run '^(TestFocusConcurrent|TestAckConcurrent)$' ./internal/engine/` -- pass
- `go test -race -count=20 -cpu=1,4,8 -run TestFocus ./internal/engine/` -- pass
- `go test -race -count=3 -run TestEngineContract ./internal/httpapi/` -- pass
- `node --test internal/httpapi/web/order.test.mjs` -- 102 pass, 0 fail

## Auto Run Result

Status: done

**Summary.** The engine now marks a finish Seen when the operator leaves its pane in herdr after
at least 3 s focused during that finish: one `herdr_focus` row, written only when the finish has no
row yet, so `Mark unseen` sticks across leaves and restarts. Focus is held in memory, set from
snapshots without a leave, dropped at a disconnect, and never recorded. Every `AgentView` carries
`in_view`, which the event stream and the Board's replay carry unchanged. Nothing was deferred.

### 2026-10-27 — Review round 2 findings triaged

Fixed 1, deferred 0, rejected 0.

- `[low]` `[fixed]` Round 1's held snapshot move was still thrown away when the operator really had
  read and left the pane: a second snapshot agreeing with the move cleared it, a restarted dwell of
  the pane moved *to* cleared it, and an event for an intermediate pane (a flick A to B to C, the
  snapshot naming C) cleared it, so A stayed Finished. `snapshotFocus` now keeps the move while a
  later snapshot names a pane (moving it on to another), `restartDwell` discards it only for the
  pane moved from, and `onFocus` treats any `pane_focused` naming another pane than the one moved
  from as that pane's leave with the dwell held, besides the usual leave of the focused pane.
  ADR-0015 amended. Tests: `TestFocusFromSnapshot` "another event first" now expects the mark;
  "agreeing second read", "second read further on" and "restart of the pane moved to" fail without
  the fix; "restart of the pane moved from" fails with the from-pane discard removed.
