---
title: "Story 6.6: Read herdr's focus, and record where a mark came from"
type: 'feature'
created: '2026-10-27'
status: 'done'
route: 'dispatch'
baseline_commit: 'cb2fac71398d9bdfb8f2e918cd59006070f7e89c'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0015-seen-when-you-leave-the-pane.md'
  - '{project-root}/docs/research/herdr-focus-events-2026-10-27.md'
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: []
deferred: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The operator decided while dogfooding Epic 6 (vfkb f14fa1cdfa89, ADR-0015) that a
Finished Agent becomes Seen when its pane was focused in herdr for about 3 s and then left. The
engine cannot build that rule until herddash reads herdr's focus at all, and the record cannot
tell an automatic mark from the operator's click.

**Approach:** ADR-0015's adapter, core and record parts only. `internal/herdr` subscribes to the
global `pane.focused` kind and maps `pane_focused` lines and the snapshot's `focused_pane_id` into
core; `internal/core` gains `AckSource` on every acknowledgement and the pure rule
`core.SeenOnLeave`; migration 0007 adds `acknowledgements.source`, which the store binds and reads
back and the history route sends. The engine ignores focus events for now (Story 6.7).

## Boundaries & Constraints

**Always:** herddash only reads focus: no focus method on `internal/herdr`, genwire's `forbidden`
list unchanged. A focus event never requests a snapshot read and never counts as an event during
one. Migration 0007 is new (the operator's database is already at schema 6), runs in the one
migration transaction, and defaults earlier rows to `'board'`. `internal/core` stays pure; no new
import edge; `AppendAcknowledgements` still called only by the engine and the store.

**Never:** No engine behaviour, view, `in_view`, tunable or Board change (Stories 6.7, 6.8). No
edit to migrations 0005 or 0006. No call to the live herdr server; no reinstall of the running
service. No change to `docs/research/herdr-socket-api.md`; documents are Story 6.9's.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Subscribe | `SubscribeLifecycle` | request names `pane.focused` and no other focus kind | N/A |
| Focus line | `pane_focused` with pane and workspace (also repeated for the same pane) | each delivered as `LifecycleFocused{PaneID, WorkspaceID}` | N/A |
| Sibling lines | `workspace_focused`, `tab_focused` (with or without data) | dropped, no delivery of any kind | N/A |
| No pane id | `pane_focused` without `pane_id` | delivered flagged unexpected; stream goes on | N/A |
| Snapshot focus | `focused_pane_id` set / null / absent | `Snapshot.FocusedPaneID` the id / `""` / `""` | N/A |
| Engine | focus events between and during snapshot reads | no snapshot read, no view change | N/A |
| Source values | `NewAcknowledgement` with `""`, `"other"`, undo with `herdr_focus` | refused | wraps `ErrInvalidAcknowledgement` |
| SeenOnLeave | Finished, not Background, unseen, `waiting_since` > 0, no row, dwell >= min | true; false for min - 1 ms, Needs you, Idle, Working, Unknown, Background, seen, row present | N/A |
| Schema 5 or 6 | database with acknowledgement rows | migrated to 7, rows unchanged, source `board`; older binary refuses | `ErrSchemaNewer` |
| Mixed batch | `board` and `herdr_focus` rows, abort on the second insert after the first was written | none kept; without abort all read back with source, across reopen | error returned |
| CHECK | raw `INSERT` with `source = 'other'` | refused by SQLite | N/A |
| History | Agent with a `board` and a `herdr_focus` row | each item carries `"source"` | N/A |

</frozen-after-approval>

## Code Map

- `internal/herdr/gen/genwire/main.go` -- `roots` gain `pane.focused` (Subscription) and
  `pane_focused` (EventData); `forbidden` untouched. `wire_gen.go` regenerated:
  `SubscriptionPaneFocused`, `EventDataPaneFocused`.
- `internal/herdr/subscribe.go` -- `lifecycleSubscriptions` gains `pane.focused` last; `stream`
  drops a line its decoder answers with `errSkipEvent`.
- `internal/herdr/map.go` -- `errSkipEvent`; `decodeLifecycle` skips `workspace_focused` and
  `tab_focused` before the no-data check and maps `pane_focused` through `lifecycle()` (so no pane
  id is unexpected); `mapSnapshot` reads `FocusedPaneID`.
- `internal/core/observe.go` -- `LifecycleFocused` (`Valid`), `Snapshot.FocusedPaneID`.
- `internal/core/acknowledgement.go` -- `AckSource` (`Valid`), `AckSourceBoard`,
  `AckSourceHerdrFocus`; `AcknowledgementInput.Source`, `Acknowledgement.Source()`; constructor
  refusals; `LeaveInput`, `SeenOnLeave` (only `time.Duration` from `time`, no clock).
- `internal/store/migrations/0007_acknowledgement_source.sql` -- the column, as ADR-0015 gives it.
- `internal/store/acknowledgements.go` -- the one `INSERT` binds `source`; `Acknowledgements`
  selects it and rebuilds through the constructor.
- `internal/engine/acknowledge.go` -- the Board path passes `AckSourceBoard`.
- `internal/engine/engine.go` -- `onLifecycle` returns on `LifecycleFocused` before `noteEvent`.
- `internal/httpapi/history.go` -- `wireAcknowledgement.Source` (`json:"source"`, always sent).

## Tasks & Acceptance

**Execution:**
- [x] genwire roots and `go generate ./internal/herdr/gen` -- the wire types for the focus read.
- [x] `internal/herdr` subscription, decoding and snapshot mapping, with
  `subscribe_test.go` `TestLifecycleFocus` (transcript lines, repeat, sibling lines with and
  without data, no pane id) and `TestLifecycleUnexpected` (the old "unknown kind" case moves to
  `tab_created`; `pane_focused` with no pane id added), and `client_test.go`
  `TestSnapshotFocusedPane` (the 0.9.5 fixture through the client: set, another pane, null,
  absent).
- [x] `internal/core` kinds, source and rule, with `TestLifecycleKindValid`,
  `TestNewAcknowledgement` (three source refusals, a `herdr_focus` mark accepted),
  `TestAckSourceValid` and `TestSeenOnLeave` (table, dwell boundary at min and min - 1 ms).
- [x] Migration 0007 and the store, with `TestUpgradeFromSchema6` (three rows incl. an undo and
  herdr orders; every table unchanged but the new column; `ms[:6]` refuses),
  `TestUpgradeFromSchema6RollsBack`, `TestUpgradeFromSchema5` (now to 7, source `board`),
  `TestAcknowledgementChecks` (CHECK and NOT NULL), `TestAcknowledgementSources` (abort after the
  first row written; round trip across reopen), and `TestColumns`/migration list at schema 7.
- [x] Engine: `onLifecycle` ignores focus; `fake_test.go` gains `harness.focus`;
  `focus_test.go` `TestFocusIgnored` (between and during reads; fails if the ignore is removed).
- [x] History: `source` on every item; `TestHistoryAcknowledgements` updated and
  `TestHistoryAcknowledgementSource` over a real store.

**Acceptance Criteria:**
- Given every criterion of Story 6.6 in `docs/planning/epics.md`, when the tests above run, then
  each is met; `go generate` leaves no diff, and vet, race, gofmt, `arch_test.go` and
  `callsite_test.go` pass with no new import edge.

## Implementation Notes

- `workspace_focused` and `tab_focused` are dropped before the no-data check, so even a bare
  `{"event":"tab_focused"}` never counts as unexpected (an unexpected line requests a snapshot).
- The skip is a decoder sentinel (`errSkipEvent`) honoured by the shared `stream` loop; the status
  decoder never returns it, so status subscriptions are unchanged.
- An existing test asserted `"focused"` is not a `LifecycleKind`; it now is, so that case became
  `"pane_focused"` and `"tab_focused"` (wire names, not kinds).
- Implemented in this session directly rather than through an implementation subagent: the
  orchestrating run provides no subagent tool here.

## Spec Change Log

## Review Triage Log

### 2026-10-27 — Self-review pass (blind, edge-case and verification-gap lenses, run inline)

No reviewer subagents were available in this orchestrated run, so the three lenses were applied
by the implementing session against the diff since the baseline. Every finding is logged.

- `[false]` `[reject]` `TestFocusIgnored`'s between-reads half still passes with the engine's
  focus branch removed — true, since outside a read `noteEvent` does nothing and the switch has no
  default; but the during-read half fails with it removed ("an unexpected snapshot read began",
  checked by mutation), so the change is protected.
- `[false]` `[reject]` A `pane_focused` with no `pane_id` makes the engine read a snapshot — by
  design: Story 6.6's criteria and ADR-0015 make it unexpected, as for every other kind.
- `[false]` `[reject]` A `pane_focused` whose payload fails to decode ends the stream as
  undecodable and the engine reconnects — the same split every lifecycle kind has (`decodeData`);
  herdr 0.9.5's schema requires both fields.
- `[false]` `[reject]` An `Acknowledgement` zero value (empty source) passed to the store — its
  fields are unexported, so only `NewAcknowledgement` builds one with content, and SQLite's
  `NOT NULL` and `CHECK` refuse `''` regardless (`TestAcknowledgementChecks`).
- `[low]` `[reject]` `docs/how-herddash-works.md`, `CLAUDE.md` and `docs/install.md` do not yet
  mention schema 7, `pane.focused` or `source` — Story 6.9's criteria already name exactly these
  updates; the spine was updated when ADR-0015 was planned.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go generate ./internal/herdr/gen` -- no diff against the committed `wire_gen.go`
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test -race ./...` -- all 12 packages pass (`arch_test.go`, `callsite_test.go` at the root)
- `go test -race -count=50 -run TestFocusIgnored ./internal/engine/` -- pass
- `go test -race -count=50 -run 'TestLifecycleFocus|TestLifecycleUnexpected|TestSnapshotFocusedPane' ./internal/herdr/` -- pass
- `node --test internal/httpapi/web/order.test.mjs` -- 102 pass, 0 fail

## Auto Run Result

Status: done

**Summary.** herddash now reads herdr's focus: the lifecycle connection subscribes to
`pane.focused` (and to no other focus kind), `pane_focused` lines become `core.LifecycleFocused`,
`workspace_focused` and `tab_focused` are dropped without a delivery, and the snapshot's
`focused_pane_id` is `core.Snapshot.FocusedPaneID`. The engine ignores focus events until Story
6.7. Every acknowledgement carries a source (`board` or `herdr_focus`, migration 0007, earlier
rows `board`), shown in history, and `core.SeenOnLeave` holds ADR-0015's rule, pure and
table-tested. Nothing was deferred.
