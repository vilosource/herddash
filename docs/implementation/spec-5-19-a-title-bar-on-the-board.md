---
title: 'Story 5.19: A title bar on the Board'
type: 'feature'
created: '2026-10-26'
status: 'done'
baseline_revision: '9a0067a0b3782e80af1259bf0398ff1fd6608778'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0013-a-title-bar-on-the-board.md'
  - '{project-root}/docs/design/board-design.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** The Board has no title bar. The page does not say what it is, and whether anything
needs the operator and whether herdr is connected are told only by the zones present and by a strip
that is usually absent. The approved design forbade a title bar (SM-C1), so the operator's request
("a standard title pane, with a text logo") needs ADR-0013.

**Approach:** One sticky title bar above the strips: a text logo (a three-bar mark in the Board's
red, amber and green, then `herddash` in monospace with `herd` bold) and a summary that restates
the Needs you count and herdr's connection. The summary's rules are a pure `titleBar` in
`order.mjs`; `board.mjs` draws it from the same status as the strips. The operator chose the
summary's content and stickiness, and approved the design rendered with their live data
(2026-10-26).

## Boundaries

- No new route, API field or event; no change to Boxes, zones or strips.
- The summary is not a live region and holds no control; the strips stay the announced region.

</intent-contract>

## Done

- `index.html`: the `.titlebar` inside the sticky header, before `#strips`; the logo is the `h1`.
- `order.mjs`: `titleBar(views, connection, lost, status)` returns `{needs, needsText, link,
  linkText}`. `needs` is 0 while the Boxes are hidden. `link` is `lost`, `down`, `none` (no state
  yet), `bad`, `warn` or `ok`, in that precedence.
- `board.mjs`: `drawTitleBar`, called from `drawStatus`; `currentStatus` carries `lost`.
- `board.css`: title bar, wordmark, mark and summary styles from existing tokens only, so light and
  dark both follow.
- Tests: two `node --test` cases for `titleBar`. The browser check asserts the bar connected with a
  Needs you Agent, with herdr stopped and with the Daemon hung.
- Docs: ADR-0013, ADR index, PRD SM-C1 amendment, the Board design's Title bar section, reading
  order and "Deliberately absent", Story 5.19 in `epics.md`.

## Verification

`go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...`, `node --test order.test.mjs` (90
pass) and `board-browser.mjs` (all `ok`, including the three title-bar checks) on darwin/arm64 with
Go 1.26.5. The rendered page was checked at 1400 px and 420 px, light and dark, against the live
Daemon through a local proxy serving the new assets.
