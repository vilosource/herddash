---
title: 'Story 3.9: Connection and version strips'
type: 'feature'
created: '2026-10-20'
status: 'done'
baseline_commit: '6198ea80bfb4fa7d47f494cc3af3b0bdbf147c70'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/design/board-design.md'
  - '{project-root}/docs/implementation/spec-3-8-the-board-stays-live.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Board looks the same whether its data is live or stale, and whether herdr is
supported or not, so the operator can trust a Board that is lying (FR-5, FR-16).

**Approach:** Draw the approved design's connection and version strips from the ConnectionTruth
the Board already keeps (Story 3.8), dim Boxes while herdr is disconnected, hide them when the
gate is refused, and also show the connection strip when the Board itself has lost the Daemon's
stream (deferred from Story 3.8).

## Boundaries & Constraints

**Always:**
- **Strips** follow `docs/design/board-design.md` "Strips": thin, full width, stuck to the top,
  shown only when needed, connection first then version, wrapping on a narrow window, inside one
  polite live region that announces only appearance and changes.
- **Disconnected** (`connection.connected` false): slate strip "Disconnected from herdr. Last
  snapshot <age> ago; Boxes show that snapshot." with the age ticking (`aria-hidden`; screen
  readers get the fixed UTC time). With no `last_snapshot_at`: "Disconnected from herdr. No
  snapshot received yet." Every Box dimmed and desaturated, never hidden; Waiting Times keep
  ticking.
- **Daemon unreachable** (the Board's own `/events` stream is down, between losing it and the next
  valid `state`): the same slate strip, reading "Lost contact with herddash. Reconnecting…", with
  Boxes dimmed as above. It replaces the herdr-disconnected text while both apply, since the
  Board's ConnectionTruth is then itself stale.
- **Advisory** (`gate` `advisory`): amber strip saying herdr *version* is below the supported
  minimum *minimum* and herddash is recording under the dogfooding flag; when
  `resync_guaranteed` is false it adds that resync is not guaranteed. Boxes shown.
- **Refused** (`gate` `refused`): red strip naming `herdr_version` and `herdr_minimum` and saying
  nothing is being recorded. No Boxes shown, whatever the model holds.
- **Clearing:** the strips and dimming follow the model; the next `state` or `connection` event
  showing herdr connected (and a valid `state` after the Board's stream returns) clears them, and
  Boxes refresh from that event.
- **Logic** that decides which strips show and their text (from ConnectionTruth, the stream state
  and the clock) is a pure function in `order.mjs`, tested by `node --test`; `board.mjs` only
  draws it. The design note's tokens and contrast are used as approved.

**Never:**
- Add any Board element beyond Boxes, workspace labels and these strips (NFR10).
- Fetch, poll or reach anything but `/events`; the CSP stays.
- Change the Daemon's ConnectionTruth, the engine or the API.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| All well | connected, gate `ok`, stream up | no strip, Boxes normal | none |
| Disconnected | `connected` false, last snapshot 160 s ago | slate strip with `2m 40s`, Boxes dimmed | none |
| Never snapshotted | `connected` false, no `last_snapshot_at` | slate strip "No snapshot received yet." | none |
| Daemon unreachable | Board stream closed, reconnecting | slate "Lost contact with herddash. Reconnecting…", Boxes dimmed | none |
| Advisory | gate `advisory`, resync guaranteed | amber strip without the resync caveat, Boxes shown | none |
| Advisory below 0.9.0 | gate `advisory`, `resync_guaranteed` false | amber strip with the resync caveat | none |
| Refused | gate `refused`, model has views | red strip with both versions, no Boxes | none |
| Both | disconnected and advisory | slate strip first, then amber | none |
| Clears | `connection` event with `connected` true | slate strip gone, Boxes undimmed | none |

</frozen-after-approval>

## Code Map

- `internal/httpapi/web/order.mjs` -- Story 3.8's model (`connection` kept from `state` and `connection` events), `streamDecision`/`streamState`, `formatDuration`; add the pure strips function here
- `internal/httpapi/web/order.test.mjs` -- node tests (CI: `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs`)
- `internal/httpapi/web/board.mjs`, `board.css`, `index.html` -- draw the strips region, dim Boxes, hide Boxes when refused; the CSP forbids inline style, so dimming is a class
- `docs/design/board.html` -- the approved prototype's strip markup, CSS and render code to reuse (its toolbar and fixture stay out)
- `docs/design/board-design.md` -- "Strips", tokens and contrast (`slate, amber, red`)
- `internal/core` ConnectionTruth JSON: `{connected, last_snapshot_at, herdr_version, herdr_minimum, gate: ok|advisory|refused, resync_guaranteed}`
- `internal/engine/engine.go` `disconnect` publishes `connected: false` at once, so the strip appears as soon as herdr's stream is lost

## Tasks & Acceptance

**Execution:**
- [x] `internal/httpapi/web/order.mjs`, `order.test.mjs` -- the pure strips function and every matrix row as a test
- [x] `internal/httpapi/web/board.mjs`, `board.css`, `index.html` -- draw strips, dim and hide Boxes
- [x] `internal/httpapi/*_test.go` -- only if the page's static checks need updating

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` and the node tests run, then every matrix row passes and arch_test passes.
- Given `herddash serve` against a fake herdr (never the live server), when the fake herdr stops and returns, then the open Board shows and clears the disconnected strip (checked in a headless browser).

## Implementation Notes

- `order.mjs` adds `boardStatus(connection, lost, nowMs)` returning `{strips, dimmed, hideBoxes}`. Each strip is `{kind: disconnected|advisory|refused, parts}`; a part is `{text}`, with `tick: true` on the ticking age (drawn `aria-hidden`) or `sr: true` on the fixed `at HH:MM:SS UTC` read in its place. Texts are the approved prototype's (`docs/design/board.html` `renderStrips`). `formatAge` is the prototype's `formatSince` (`Mm SSs` below an hour, then `formatDuration`); `clockUTC` and `LOST_TEXT` are exported beside it. `stripsKey` identifies strips without the ticking age, so `board.mjs` redraws the live region only when a strip appears, goes or changes.
- The Board-side stream loss is `st.lost` in `streamDecision`: set by any `error` on the current source (CONNECTING or CLOSED), kept across a reopen, cleared only by a valid `state` (on a new source or on EventSource's own retry of the same one). While lost, the slate strip reads `LOST_TEXT` instead of any herdr text; the version strip still follows the last ConnectionTruth. Before the first `state`, with the stream up, nothing shows.
- A missing `herdr_version` or `herdr_minimum` reads "(version unknown)"; an advisory without `resync_guaranteed: true` carries the caveat.
- `board.mjs` toggles `board--stale` on `#board` (dimming is CSS, `opacity: var(--stale-opacity)` and `saturate(0.35)`, the prototype's) and leaves the zones out when `hideBoxes`; the Box map is kept so Boxes return in place. It re-renders on a model change or a change of `lost`, and the 1 s tick updates the age in place. The strip tokens (light and dark) and `.strips`/`.strip--*` rules are copied from the prototype; `overflow-wrap: anywhere` keeps a long strip wrapping on a narrow window. `index.html` carries `<div class="strips" id="strips" role="status" aria-live="polite">` before `<main>`; `TestBoardPage` checks it.
- Checked end to end in headless Chrome (CDP from a Node script) with `herddash serve` against `serve_test.go`'s fake herdr on a kernel-picked port, never the live herdr: fake herdr stopped, the slate strip with the ticking age appeared within the next poll (1 ms) and Boxes dimmed to 0.5 / saturate(0.35); the fake returned and the strip and dimming cleared 0.73 s later. Then serve itself stopped: "Lost contact with herddash. Reconnecting…" with Boxes dimmed; serve restarted on the same address and the strip cleared within 1 s on the new `state`. No console warnings. Advisory and refused were checked only through `boardStatus` fixtures (the epic forbids exercising them on the live server). The scratch driver and test were not committed.

- Review fixes: any gate but `ok` or `advisory` (missing or unknown) is refused, hides Boxes and reads "herdr version gate unknown (<gate or none>); nothing is being recorded."; anything but `connected: true` is disconnected; a non-finite or non-positive `last_snapshot_at` is "No snapshot received yet."; with Boxes hidden the disconnected strip ends "." instead of "; Boxes show that snapshot."; the advisory caveat is "Resync is not guaranteed with this herdr version." whenever `resync_guaranteed` is not true.
- A missing or empty `gate` while not connected is the Daemon before its first gate decision: no version strip and Boxes not hidden for it, so startup does not flash "nothing is being recorded". A missing gate while connected, and any present value other than `ok` or `advisory`, stays refused.
- The lost strip waits `LOST_GRACE_MS` (3 s): `streamDecision`'s `error` input carries `now`, the state keeps `lostSince` (the first error since the last valid state), and `streamLost(st, now)` is true once the grace has passed. `board.mjs`'s 1 s tick shows it, so a drop EventSource heals within the grace never flashes.
- The live region is `aria-atomic="false"` inside a sticky `<header class="strips-bar">`; the tick changes only the `aria-hidden` age span. Focus on a Box when the gate turns refused is remembered and restored when Boxes return, unless focus has moved elsewhere.
- `internal/httpapi/web/check/board-browser.mjs` is the committed hand-run check (Node built-ins only, its own fake herdr on a unix socket, its own HOME): herdr stop/return, serve stop past the grace/restart, refused, advisory, no console warnings. Not embedded; `TestBoardServesEveryFile` asserts nothing under `web/check/` is embedded or served.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH | Disconnected plus refused (or a cold page) says "Boxes show that snapshot" over an empty Board | low | `boardStatus` keeps the clause when `hideBoxes`; patch: drop it when no Boxes are shown |
| 2 | BH | The ticking age inside an atomic `role=status` region may be re-read every second | medium | `role=status` implies `aria-atomic=true`; patch: keep the ticking text outside the announced content (`aria-atomic=false` on the region, and the age span outside what changes the announcement), pinned by the page test |
| 3 | BH | Spec/tracker status mismatch | false | Workflow ordering |
| 4 | BH | Stated behaviour untested: version unknown, advisory with `resync_guaranteed` absent, a `state` clearing the strip, lost plus refused | low | Patch with tests |
| 5 | BH, VG | `board.mjs` drawing (refused hides Boxes, `board--stale`, lost re-render, ticking age) runs in no committed check; refused was never run even by hand | medium | VG pre-verified: dropping the `hideBoxes` guards or the lost re-render passes every test; patch: commit a hand-run headless-Chrome check (Node built-ins and the local Chrome, no npm) covering disconnected, lost, refused and advisory against the fake herdr; wiring it into CI is unplanned work, deferred |
| 6 | BH | The advisory caveat hard-codes "below 0.9.0" though the Daemon owns `resync_guaranteed` | low | Patch: word the caveat from the flag alone |
| 7 | BH, ECH | The snapshot age compares the browser's clock with the Daemon's | false | The Board is served only on loopback (ADR-0007), so browser and Daemon share one clock |
| 8 | BH, ECH | A transient stream error flashes the lost strip and dims every Box | medium | `lost` sets on the first `error`, including EventSource's own retry; patch: show it only after a named grace period with the stream still down |
| 9 | BH | Recovery is not announced | low | The design is silent and announcing adds text; rejected |
| 10 | BH | The strips sit outside every landmark | low | Direct; patch: put them in a `header` |
| 11 | ECH | A missing or unknown `gate` is treated as `ok` | medium | Silently shows Boxes from a Daemon whose gate is unknown; patch: treat any gate other than `ok` and `advisory` as refused, naming it |
| 12 | ECH | A ConnectionTruth without a boolean `connected` reads as connected | medium | Stale Boxes shown undimmed; patch: anything but `connected: true` is disconnected |
| 13 | ECH | A non-finite `last_snapshot_at` gives `NaN:NaN:NaN` | low | Direct `Number.isFinite`; patch |
| 14 | ECH | Focus is lost when the gate turns refused and not restored when Boxes return | low | Patch: keep the focused key and restore it when Boxes come back |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `grep -rE 'https?://|fetch\(' internal/httpapi/web/ --exclude-dir=check` -- expected: nothing (`web/check/` is the hand-run browser check, never embedded or served)
- `node internal/httpapi/web/check/board-browser.mjs <herddash binary>` -- hand-run, needs Chrome; expected: every line `ok`, exit 0
