---
title: 'Story 3.8: The Board stays live'
type: 'feature'
created: '2026-10-20'
status: 'done'
baseline_commit: 'b55474c968d92f4988dc4568905c1713bd198864'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/implementation/spec-3-7-the-board-renders-every-agent.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Board renders only the opening `state` event. It goes stale as soon as an Agent
changes, and it freezes for good if the Daemon restarts (FR-1, FR-3, FR-4, AD-7).

**Approach:** The Board applies every stream event to its model of AgentViews and re-renders in
place, reconnects by itself after any drop or a Daemon restart, and keeps keyboard focus across
re-renders. Event handling is a pure function in the ordering module, tested by `node --test`; a
Go test checks the one-second budget from recording to delivery.

## Boundaries & Constraints

**Always:**
- **Model:** the Board holds the latest AgentViews keyed by AgentKey (host, server, epoch, pane
  id) plus the latest ConnectionTruth. A pure function in `order.mjs` applies one event to the
  model and returns the new model:
  - `state`: replaces everything (AgentViews and ConnectionTruth);
  - `agent`: adds or replaces that AgentView;
  - `transition`: replaces the AgentView with the event's `agent`;
  - `removed`: drops the key;
  - `connection`: replaces the ConnectionTruth (Story 3.9 draws it);
  - `resync` and any unknown name: no change.
  A malformed payload leaves the model unchanged and warns once per event in the console.
- **Placement is the Daemon's:** after each event the Board re-sorts by the AgentViews' own
  `sort_key`s with the Story 3.7 functions. It never computes a position itself. So, with core's
  key, a Transition into `blocked` lands in Needs you at its longest-waiting position (a newly
  blocked Agent sorts last there, operator decision 2026-10-20), a Transition into `working`
  moves the Box out of the top zone into its workspace group, and a `done` Agent turning `idle`
  keeps its place and Waiting Time because its key and `waiting_since` do not change.
- **Rendering in place:** Boxes are matched by AgentKey (a `data-key`), so an update changes,
  moves, adds or removes only the Boxes concerned. Keyboard focus stays on the same Agent's Box if
  it still exists. Waiting Times keep ticking.
- **Reconnect:** the Board opens `/events` first and fetches nothing else, with no polling.
  `EventSource` retries a dropped stream itself; when it closes for good (a non-200 reply, such as
  the 503 during a Daemon restart), the Board reopens it with capped backoff (named constants:
  start about 1 s, cap about 30 s, reset on a `state` event). Every new stream starts with `state`,
  which replaces the model.
- **Tests:** `order.test.mjs` covers the event function for every name and the cases above,
  including blocked placement, working leaving the top zone, done→idle keeping place, removal, an
  unknown name and a malformed payload. A Go test in `internal/httpapi` (with the engine or a fake
  View/Subscribe) checks that a recorded Transition reaches an `/events` client within one second.
- The CSP from Story 3.7 still holds: the page reaches nothing but `/events`.

**Never:**
- Draw the connection or version strips (Story 3.9) or dim Boxes.
- Re-derive class, zone, group, position or Waiting Time start in JavaScript.
- Add npm, a dependency, polling or a fetch.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| New Agent | `agent` for an unseen key | Box added in sort position | none |
| Change | `transition` working→blocked with the new view | Box moves into Needs you at its sort position | none |
| Leave top | `transition` blocked→working | Box moves into its workspace group | none |
| Done to idle | `transition` done→idle, same sort key | Box keeps its place and Waiting Time | none |
| Removed | `removed` key | Box gone | none |
| Unknown event | `resync` or a new name | nothing changes | none |
| Malformed | `agent` with bad JSON | nothing changes, one warning | none |
| Restart | stream closes with 503 | reopened with backoff; `state` replaces the model | none |
| Focus | focused Box updated by a `transition` | focus stays on that Agent's Box | none |
| Budget | Transition recorded | `/events` client receives it within 1 s | test fails |

</frozen-after-approval>

## Code Map

- `internal/httpapi/web/order.mjs` -- Story 3.7's `sortViews`, `workspaceHeaders`, `layout`, labels; add the pure event function and the AgentKey string helper here
- `internal/httpapi/web/order.test.mjs` -- node tests; the golden fixture is `internal/core/testdata/board-order.json`
- `internal/httpapi/web/board.mjs` -- today opens `EventSource('/events')`, listens for `state` only and rebuilds every Box with `replaceChildren`; becomes model + keyed in-place rendering + reconnect
- `internal/httpapi/events.go` -- event names (`state`, `agent`, `removed`, `transition`, `resync`, `connection`) and payloads: `agent` is an AgentView, `removed` is `{key}`, `transition` is `{key, transition, agent}`, `connection` is ConnectionTruth, `resync` is `{reason, detail}`; a stream after shutdown answers 503
- `internal/httpapi/events_test.go` -- stream test helpers to reuse for the one-second budget test
- `internal/httpapi/board.go`, `board_test.go` -- embedded files and the CSP; no change expected beyond any new file
- `docs/design/board-design.md` -- the approved design (unchanged)
- Node: `~/.local/share/mise/installs/node/22/bin`; Go: `~/.local/share/mise/installs/go/1.26.5/bin`; CI runs `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs`

## Tasks & Acceptance

**Execution:**
- [x] `internal/httpapi/web/order.mjs`, `order.test.mjs` -- the event function and key helper, with the matrix's ordering rows as tests
- [x] `internal/httpapi/web/board.mjs` -- model, keyed in-place rendering, focus kept, reconnect with backoff
- [x] `internal/httpapi/*_test.go` -- the one-second recording-to-delivery test

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` and `node --test internal/httpapi/web/*.test.mjs` run, then every matrix row except Restart and Focus passes in a test, and arch_test passes.
- Given `herddash serve` against a fake herdr (never the live server), when an Agent changes and when the Daemon is restarted, then the open Board updates and recovers by itself (checked in a headless browser).

## Implementation Notes

- `order.mjs` adds `keyId` (the AgentKey as a JSON array string, unambiguous when a host or server contains `/`; it is the Box's `data-key`), `emptyModel`, `modelViews`, `applyEvent(model, name, rawData)` returning `{model, warning}` (the same model object when nothing changed), and the backoff constants `RECONNECT_START_MS` (1 s) and `RECONNECT_CAP_MS` (30 s) with `nextReconnectDelay` (doubling). `applyEvent` stays pure; `board.mjs` writes the one console warning.
- Malformed means: JSON that does not parse; an AgentView without a valid key (string host, server, pane id, integer epoch) or a five-element `sort_key` whose first four are finite numbers; a `transition` whose `key` differs from its `agent.key`; a `removed` without a valid key; a `connection` that is not an object; a `state` without an `agents` array of valid views or without a `connection` object. A `state` with one bad view is rejected whole. A `removed` for an unknown key is a no-op, not a warning.
- `board.mjs` keeps one `article` per key across updates and redraws its contents only when the view's JSON changed; `place` moves only out-of-place children. A Box moved between zones or groups loses focus in the browser, so the focused key is captured before each render and the same element refocused (`preventScroll`). Workspace groups are keyed by `sort_key[1]`.
- Reconnect and event gating are the pure `streamDecision(st, input)` in `order.mjs` (inputs `open`, `error`, `event`), with `EVENT_NAMES` (the Daemon's six names) exported beside it; `board.mjs` only executes its decisions. On `error` it reopens only when the source is current, CLOSED and no reopen is pending; a valid `state` resets the delay; a new source's events are ignored until a valid `state` arrives on it. While `readyState` is CONNECTING, EventSource's own retry (the stream's `retry: 2000`) is left alone.
- `validView` also requires a known `attention_class`; a `state` with two views for one AgentKey is rejected with a warning. A Box's `aria-label` is set only when its text changes.
- `internal/core/view_test.go` `TestNewlyBlockedSortsLast` pins the core guarantee that a newly blocked Agent sorts after an earlier one in Needs you; PRD FR-3 is amended to match (operator decision 2026-10-20).
- The publication-to-delivery budget test is `TestEventsPublicationToDeliveryBudget` (fake View/Subscribe): five Transitions published one after another (starting blocked→working, then working→blocked), each View holding the changed Agent, each must arrive as a `transition` event with its previous and new status within 1 s. The recording-to-delivery budget, through the real engine and fake herdr, is `cmd/herddash`'s `TestServeEvents`.
- Checked end to end in headless Chrome (CDP from a Node script), with `herddash serve` against the fake herdr from `serve_test.go` on a scratch port, never the live herdr: the working Box moved to Needs you 22 ms after the fake's status change, focus stayed on the same element; serve stopped and a stub answered 503 on the port for 4 s: EventSource went CLOSED, the Board reopened after 1 s (503 again) and 2 s, received `state` and re-rendered with Waiting Time intact; a fake herdr restart (new epoch) replaced the Box via `removed` and `agent`. No console warnings. The scratch driver was not committed.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH | PRD FR-3 still says a Transition into `blocked` moves the Box "to the head of the top zone's Needs you group" | medium | `docs/prd.md:213`; contradicts the operator's 2026-10-20 decision (4122c87bc8a3) now in `epics.md`; patch: amend FR-3 with a dated note |
| 2 | BH | Nothing in core proves a newly blocked Agent sorts last in Needs you | medium | The JS test passes a hand-made key; patch: a core test with two blocked Agents, the later start sorting after |
| 3 | VG, BH | `board.mjs` stream wiring (subscribed names, reopen on CLOSED, delay reset) runs in no test | medium | VG pre-verified: dropping `transition` from `EVENT_NAMES` passes every test; patch: move `EVENT_NAMES` and the stream-error decision into `order.mjs` as pure exports and test them, including that `EVENT_NAMES` covers every name the Daemon sends |
| 4 | BH | Nothing tells the operator the Daemon itself is unreachable while the Board waits to reconnect | medium | ConnectionTruth comes from the Daemon, so it cannot report the Daemon down; the connection strip is Story 3.9's; defer: 3.9 must also show a Board-side stream loss |
| 5 | BH | Spec/tracker status mismatch | false | Workflow ordering |
| 6 | BH, ECH | The budget test's starting status is wrong: the base Agent is already `blocked` | medium | First step is blocked→blocked, so working→blocked is never tested; patch: derive `prev` from the view |
| 7 | BH | Each published View in the budget test contradicts its Change | low | Patch: put the changed Agent into the View |
| 8 | BH, ECH | The budget test reads raw events (skipping the helper's ended/unexpected checks), re-checks the timeout and claims "recording" | low | Patch: use the helper's checks, drop the redundant check, name it publication-to-delivery and cite `TestServeEvents` for recording-to-delivery |
| 9 | BH, ECH | `validView` accepts a missing or unknown `attention_class` | low | Renders `box--undefined` silently; direct check; patch |
| 10 | BH | The focused Box's `aria-label` is rewritten every second | low | Screen readers may re-announce it continually now that focus is kept; patch: rewrite the label only when its text changes |
| 11 | BH | No fast reconnect on `online` or `visibilitychange` | low | The backoff is capped at 30 s; extra listeners add paths; rejected |
| 12 | BH | The spec names machine-specific toolchain paths | low | The fix edits this spec; rejected |
| 13 | BH | "Unknown names handled" is misleading: EventSource drops names nobody listens for | low | Patch the comment and test naming with 3 |
| 14 | VG | The done→idle test only shows `applyEvent` keeps the key it is given | low | Core owns the guarantee (`internal/core/attention_test.go`, `internal/engine/attention_test.go`); patch: rename the test to what it shows |
| 15 | ECH | A `state` with two views for one AgentKey silently drops one | low | Direct check with a warning; patch |
| 16 | ECH | After a reopen or retry, events from the new stream apply to the old model until a valid `state` arrives | medium | Leaves Boxes for Agents that no longer exist; patch: ignore non-`state` events on a new stream until its `state` is valid (in the pure stream decision, tested) |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `grep -rE 'https?://|fetch\(' internal/httpapi/web/` -- expected: nothing
