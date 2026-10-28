---
title: "Story 6.4: The Board's Seen control, bulk mark and Undo"
type: 'feature'
created: '2026-10-27'
status: 'done'
baseline_revision: 'cb003cf'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0014-acknowledging-a-finished-agent.md'
  - '{project-root}/docs/adr/0003-the-board-is-the-product.md'
  - '{project-root}/docs/implementation/spec-6-3-write-routes-guarded-against-cross-site-requests.md'
  - '{project-root}/docs/design/board-design.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Stories 6.1 to 6.3 record an acknowledgement, decide it in the engine and accept it
over guarded write routes, and the view already carries `seen` with the Daemon's rank 2, but the
Board neither draws a seen Agent differently nor offers any way to mark one. A Finished Box the
operator has read still counts as finished in its group.

**Approach:** ADR-0014's Board part in `internal/httpapi/web`, embedded, no npm, from the mock the
operator approved with their live Board (scratchpad `ackmock/`). `order.mjs` gains the pure rules:
a `seen` Box kind after Background, its `Seen` label, `seen` in the group counts, which Boxes a
heading marks, the bulk body, the per-Agent path, and the Undo bar's and a refusal's text.
`board.mjs` draws a `✓ Seen` pill on Finished Boxes, a heading's `✓ Mark N finished seen`, and a
6 s Undo bar, and sends the writes; the Box itself changes only when the stream says so. The mock's
CSS moves into named rules on the existing tokens.

## Boundaries & Constraints

**Always:** Nothing optimistic: the Box changes when the stream's `agent` event arrives with
`seen` and the Daemon's sort key, so the order stays the Daemon's (AD-1). A pill only on kind
`finished` (never Needs you, Idle, Background, Working, Unknown, or a seen Box). A bulk mark names
each Finished Box's exact finish (key and `waiting_since`), never a workspace. Controls are plain
buttons, disabled while their request is in flight and all of them while the Boxes are dimmed or
hidden. herdr is never told anything. The CSP is unchanged: no inline script or style.

**Never:** A keyboard shortcut or key binding (the operator uses Vimium). Any request but `/events`
and the four write routes. Client-side seen state (the mock's map).

## I/O & Edge-Case Matrix

| Case | Board shows |
|---|---|
| Finished, not seen | Blue Finished Box with `✓ Seen` at the end of its first line |
| `seen: true`, class finished | Grey `SEEN` Box, no pill, among Idle by the Daemon's rank 2; counted `N seen` |
| `seen: true` and `background: true` | Background Box (Background first), no pill |
| `seen` absent (older Daemon) | Finished, as before |
| Group with 2+ Finished Boxes | `✓ Mark N finished seen` after the counts |
| Group with 0 or 1 | No bulk mark |
| Mark succeeds | Undo bar 6 s, `Marked <tab> seen` / `Marked N Agents seen`, with Undo |
| 409 | Bar 6 s: `It changed before it was marked; nothing was marked`, no Undo |
| 503 | Bar 6 s: `herddash is not recording; nothing was marked`, no Undo |
| Any other refusal or network failure | Bar 6 s: `Could not mark it seen`, no Undo |
| Undo | Posts the same items to `unseen` (per-Agent or bulk, as marked), closes the bar |
| Boxes dimmed or hidden | Every control disabled |

</intent-contract>

## Code Map

- `internal/httpapi/web/order.mjs`: `SEEN_LABEL`, `isSeen`; `boxKind` returns `seen` after the
  Background check; `classLabel` gives `Seen`; `groupCounts` counts `seen` after `idle`;
  `unseenFinished`, `ackBody`, `ackPath`, `ackToast`, `ackError` with `ACK_STALE`, `ACK_OFF`,
  `ACK_FAILED`, and `ACK_TOAST_MS` (6000). `boxLabel` says Seen through `classLabel`.
- `internal/httpapi/web/board.mjs`: `ackButton` (the pill, appended last on the `.where` line of a
  Finished Box); the heading's `button.ack-all` and `drawBulk`; `acknowledge` (one view per-Agent,
  a heading's views through `/api/seen`, Undo through the matching `unseen`), `postAck` (`fetch`
  `POST`, `Content-Type: application/json`, `X-Herddash-Write: 1`); the one Undo bar
  (`div.toast`, `role="status"`), `showToast`/`hideToast`; `syncControls` and `controlsOff` (set
  from `boardStatus`'s dimmed or hidden in `drawStatus`). A control inside a Box counts as its Box
  for focus, so a Box redrawn without its pill keeps focus.
- `internal/httpapi/web/board.css`: `.box--seen`, `.ack`, `.pane + .ack`, `.ack-all`,
  `.ack-all[hidden]`, the disabled state, `.toast`, `.toast--on`, `.toast-undo`, and the toast's
  150 ms transition under `prefers-reduced-motion: no-preference`. Existing tokens only, so dark
  mode follows.
- `internal/httpapi/web/order.test.mjs`: the acknowledgement tests below; the golden layout test's
  rank table gains `seen: 2`.
- `internal/httpapi/web/check/board-browser.mjs`: the fake herdr takes a snapshot function and
  keeps each pane's status subscription so it can push a status change; a fourth section,
  `ackChecks`, with its own fake herdr, data directory and seven Agents in two workspaces. The
  existing heading assertion reads the label and counts, since the heading now holds the button.
- `internal/httpapi/board_test.go`: `TestBoardPage` allowed no `fetch(` at all; it now allows
  exactly one, in `board.mjs`, and requires it to be a JSON `POST` with the write header naming
  `/api/seen` and `/api/unseen`, and `order.mjs` to hold the per-Agent route. No asset may use
  `XMLHttpRequest`, `WebSocket` or `sendBeacon`.
- `docs/design/board-design.md`: status line amended; the Box's first and last rows, accessible
  name and Order; "Seen is grey" under Emphasis; zones and counts; a new dated "Acknowledgement
  controls" section; Accessibility; "Deliberately absent" amended (ADR-0014).

## Tasks & Acceptance

**Execution:** all done, with these tests:

- `node --test internal/httpapi/web/*.test.mjs`: `seen: the Daemon's field, drawn as its own kind,
  after Background` (seen Background is Background; `done` to `idle` keeps seen; a view without
  the field is Finished); `seen box label says Seen, not Finished`; `group counts: seen after idle,
  not counted finished` (`1 finished · 3 idle · 2 seen · 1 background · 2 working`); `group
  counts: marking one Finished view drops one finished and adds 1 seen`; `unseenFinished`;
  `ackBody`; `ackPath`; `ackToast`; `ackError` (table of 409 both codes, 503, 404, 403, 500, 0);
  `layout: a seen view sits after its group's Finished views, by the Daemon's rank 2`.
- `board-browser.mjs` section 4, against the real binary, the fake herdr and a temporary data
  directory, with Needs you, Working, three Finished and Idle in `probe` and one Finished in
  `solo`: a pill on every Finished Box, last on its first line, and on no other kind, with its text,
  title and accessible name; `probe`'s heading offers `✓ Mark 3 finished seen` after its counts
  and `solo`'s none; through a stand-in stream, Background Boxes, one of them seen, carry no pill;
  a click makes that Box `box--seen` reading SEEN with a Seen accessible name, the Daemon's view
  seen at rank 2 with the same `waiting_since`, its computed left edge 6px in `--border-quiet` (so
  removing `.box--seen` fails the check, tried), placed after the Finished Boxes next to the Idle
  Box in exactly the Daemon's order (`sortViews` of `/api/state`), the heading at `2 finished · 1
  idle · 1 seen · 1 working`; the Undo bar fixed, `role="status"`, `Marked 4: docs seen` with
  Undo; Undo closes it and the Box is Finished again; marked again, the bar goes after 6 s and the
  Box stays seen; the heading's bulk mark marks exactly the two remaining Finished Agents (`Marked 2
  Agents seen`), then hides; the fake herdr turning the seen Agent `working` shows a Working Box,
  and `done` a Finished Box with its pill; a page on a second loopback port `fetch`-POSTing to the
  per-Agent route with the custom header and JSON fails, the Agent stays unseen and the Board shows
  it Finished; with `serve` stopped the Boxes dim and every pill and bulk mark is disabled; after
  `serve` restarts on the same data directory the two seen Boxes are still SEEN and the controls
  enabled again.
- `TestBoardPage` as narrowed above.

**Acceptance Criteria:** every criterion of Story 6.4 in `docs/planning/epics.md` is met by the
tests above. The live-data design gate is the operator's dogfooding at the end of the epic
(Story 6.5).

## Spec Change Log

## Review Triage Log

### 2026-10-27 — Self-review pass
- `[low]` `[accept]` If a mark's response arrives before its `agent` event, the pill is enabled for
  that moment; a second click then marks a finish already marked, which the engine answers with
  success and no row, so only a second Undo bar results. Holding the pill disabled until the event
  would need client-side seen state, which ADR-0014 rejects.
- `[low]` `[accept]` A failed Undo shows `Could not mark it seen` (or the 409/503 text), the
  story's single refusal wording. An Undo the engine finds nothing to undo succeeds, so this needs
  a refused request or a lost Daemon.
- `[low]` `[patch]` `node --test internal/httpapi/web/` (a directory) is not accepted by Node 22;
  CI and this spec run the glob `internal/httpapi/web/*.test.mjs`, which is the same file set.
- `[low]` `[patch]` The existing browser check read a heading's whole `textContent`, which now
  includes the hidden bulk mark's text; it reads the label and counts instead.

### 2026-10-27 — Review round 1
- `[low]` `[patch]` A refused Undo showed a refused mark's text ("nothing was marked") while the
  mark stood, reversing the self-review's accept above. `ackError` takes `seen`; a refused Undo
  reads `It changed before the undo; it is still marked seen` (409), `herddash is not recording;
  it is still marked seen` (503) or `Could not undo it; it is still marked seen`. Tested in
  `order.test.mjs`.
- `[low]` `[patch]` Focus fell to the page when a focused control was disabled (the pill or the
  heading's bulk mark while its request is in flight, Undo when the Daemon is lost), when a
  heading's bulk mark was hidden, and when the Undo bar closed with Undo focused. `setDisabled`
  moves focus to the control's Box (the group's first Box for a heading) before disabling it;
  `render` gives a hidden bulk mark's focus to its group's first Box; `hideToast` moves focus
  from the bar to the Box it was about. The browser check asserts focus after a pill, an Undo and
  a bulk mark.
- Round 1 outcome: both findings fixed (387496a); none deferred, none rejected. A refused Undo's
  409 is accurate as worded: the engine refuses an undo only when the mark is current and the
  pane's rows are held (`internal/engine/acknowledge.go`), so the mark does stand.

## Design Notes

The seen Box's place comes from the Daemon's sort key (rank 2, Idle's newest-first start), so
the Board computes nothing about order; moving down is just the next render after the `agent`
event. `boxKind` is the one place a seen view becomes its own kind, so the CSS class, the label,
the accessible name, the counts and `unseenFinished` all follow from it, and Background keeps
precedence exactly as the Daemon ranks it 3. The Undo bar is one element for the page, so a new
mark replaces its text, action and timer. The pill and the heading's button are created by the
render that draws their Box or group, and `syncControls` sets every control's `disabled` from
`controlsOff` and the in-flight sets after each render and status change.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test ./...` -- all packages pass
- `go test -race ./internal/httpapi` -- pass
- `node --test internal/httpapi/web/*.test.mjs` -- 101 pass, 0 fail
- `go build -o /tmp/herddash-e6 ./cmd/herddash && node internal/httpapi/web/check/board-browser.mjs /tmp/herddash-e6`
  -- all 51 checks ok, exit 0, no herddash process left; with `.box--seen` removed from the CSS
  it fails `marked: the .box--seen rule draws its quiet 6px edge`, exit 1
