---
title: "Story 6.8: The Board shows \"in view\" and offers Mark unseen"
type: 'feature'
created: '2026-10-27'
status: 'done'
baseline_revision: '0042464'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0015-seen-when-you-leave-the-pane.md'
  - '{project-root}/docs/adr/0014-acknowledging-a-finished-agent.md'
  - '{project-root}/docs/research/herdr-focus-events-2026-10-27.md'
  - '{project-root}/docs/implementation/spec-6-4-the-board-s-seen-control-bulk-mark-and-undo.md'
  - '{project-root}/docs/implementation/spec-6-7-the-engine-marks-a-finish-seen-when-the-operator-leaves-its-pane.md'
  - '{project-root}/docs/design/board-design.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Story 6.7 makes the Daemon mark a finish Seen when the operator leaves its pane in
herdr after 3 s, and publishes `in_view` on every AgentView, but the Board neither shows which
Finished Box is the pane focused in herdr (so the automatic mark comes as a surprise) nor offers
any way to take a Seen back (so an automatic mark the operator disagrees with is final).

**Approach:** ADR-0015's Board part, exactly as `docs/design/board-design.md`'s section "Seen on
leaving the pane, the in-view tag and Mark unseen" gives it. `order.mjs` gains the pure rules:
which Box shows the tag, the accessible name's ", in view", the pills' accessible names, and the
bar's text for a Mark unseen, its refusal and its Undo's refusal. `board.mjs` draws `● in view`
before a Finished Box's Seen pill and a `Mark unseen` pill on Seen Boxes, posting the existing
per-Agent `unseen` route; the Box changes only when the stream says so. The browser check's fake
herdr learns `pane.focused`, and `serve` takes the smallest seam that lowers the dwell for it.

## Boundaries & Constraints

**Always:** The tag only on kind `finished` with `in_view === true` (never Background, Seen,
Needs you, Idle, Working or Unknown, whatever `in_view` says). `Mark unseen` only on kind `seen`.
Nothing optimistic: a Box changes on the stream's `agent` event. Both pills are disabled while in
flight and while the Boxes are dimmed or hidden. herdr is never told anything: the Board reads
`in_view` as the Daemon sends it, and the Daemon only reads herdr's focus. No new route, header or
event; the CSP is unchanged.

**Never:** A key binding. A tag that is a control (it is a `span`, not focusable). Client-side
seen or focus state. A dwell seam that a configuration file or flag can reach: it is an
environment variable only the browser check sets, and `serve` logs a warning whenever it is set.

## I/O & Edge-Case Matrix

| Case | Board shows |
|---|---|
| Finished, `in_view` true | `● in view` after the badge, before `✓ Seen`; label `…, Finished, in view, waiting …` |
| Finished, `in_view` false or absent | Finished as before, no tag |
| Seen, Background, Needs you, Idle, Working with `in_view` true | No tag |
| Seen | `Mark unseen` pill at the end of the first line, no Seen pill |
| Leave past the dwell (the Daemon's mark) | Seen Box with `Mark unseen`; no Undo bar |
| Mark unseen succeeds | Undo bar 6 s `Marked <tab> not seen`, Undo posts per-Agent `seen` |
| Mark unseen refused, 409 / 503 | The Seen pill's texts: `It changed before it was marked; nothing was marked` / `herddash is not recording; nothing was marked` |
| Mark unseen refused otherwise | `Could not mark it not seen` |
| Undo of Mark unseen refused | `It changed before the undo; it is still not seen` (409), `herddash is not recording; it is still not seen` (503), `Could not undo it; it is still not seen` |
| An Undo that succeeds | No bar |
| A focused pill's Box redrawn with the same pill (in_view toggled) | Focus back on that pill |
| A focused pill's Box redrawn without it | Focus on the Box, as before |

</intent-contract>

## Code Map

- `internal/httpapi/web/order.mjs`: `IN_VIEW_TEXT`, `IN_VIEW_TITLE`, `inViewTag`, `ackLabel`,
  `unackLabel`; `boxLabel` puts `in view` after the class label for a tagged Box; `ackError`
  gains a fourth parameter, `undo`, so it tells a mark, a Mark unseen, the Undo of a mark and the
  Undo of a Mark unseen apart, with `UNSEEN_FAILED` and `REDO_STALE`, `REDO_OFF`, `REDO_FAILED`.
- `internal/httpapi/web/board.mjs`: the `span.in-view` in `fillBox`, `unackButton`;
  `acknowledge` takes `how.undo` (the bar is shown for a mark and a Mark unseen, never for an
  Undo, whose request is the opposite of the action it undoes); `syncControls` covers
  `button.unack`; `render` remembers a focused `button.ack` or `button.unack` and focuses the
  same pill in the redrawn Box when it has one (Story 6.7's review criterion).
- `internal/httpapi/web/board.css`: `.in-view`, `.pane + .in-view`, `.in-view + .ack`, `.unack`,
  `.pane + .unack`, its hover, and `.unack:disabled`; existing tokens only.
- `internal/engine`: `Options.SeenDwell` and `Engine.minDwell()`, the leave's minimum when
  positive, else the package's `seenDwell`.
- `cmd/herddash/serve.go`: `testSeenDwell` reads `HERDDASH_TEST_SEEN_DWELL_MS` (whole
  milliseconds, 1 to 60000, logged at warn; anything else ignored with a warning) into
  `Options.SeenDwell`.
- `internal/httpapi/web/check/board-browser.mjs`: the fake herdr keeps each lifecycle
  subscription naming `pane.focused` and `pushFocus` sends it a `pane_focused` line copied from
  the focus record; `paneSnapshot` (from `ackSnapshot`) sets `focused_pane_id` when given a
  focus; `serve` takes extra environment; a fifth section, `focusChecks`.
- `docs/design/board-design.md`: Accessibility (focus back on the same pill) and the Mark unseen
  bullet (an Undo's refusal texts); `CLAUDE.md`: the browser check's seam.

## Tasks & Acceptance

**Execution:** all done, with these tests:

- `node --test internal/httpapi/web/order.test.mjs` (107 pass): `inViewTag` table (Finished in
  view; not in view, absent, a string; Seen, Background, seen Background, Needs you, Idle,
  Working, Unknown in view, all false); `boxLabel` with and without the tag for every kind;
  `ackLabel` and `unackLabel`; `ackError` for a Mark unseen (409 both codes, 503, 404, 403, 500,
  0) and for the Undo of one; the Undo-of-a-mark table now passes `undo` explicitly.
- `TestFocusSeenDwellOption` (`internal/engine`): with `Options.SeenDwell` 400 ms, a leave 1 ms
  under it marks nothing and a leave at it marks one `herdr_focus` row. `TestTestSeenDwell`
  (`cmd/herddash`): unset gives 0 and logs nothing; 1, 1500 and 60000 are used with a warning;
  empty, 0, -5, 60001, 1.5 and 3s are ignored with a warning.
- `board-browser.mjs` section 5, against the real binary, a fake herdr whose snapshot names
  `w1:p1` focused, a temporary data directory and `HERDDASH_TEST_SEEN_DWELL_MS=1000`, with
  Finished A, B and C, Needs you, Idle and Working in `probe`: A's Box alone has the tag, a `span`
  with `tabIndex` -1, `● in view` and the design's title, between the tab and the Seen pill, in
  `--finished-accent` at weight 600 (so removing `.in-view` fails, tried), its label `…, Finished,
  in view, waiting …`, and `/api/state` has `in_view` on every Agent, true for A alone; through a
  stand-in stream with every Agent `in_view`, A Background and B seen, only C (Finished) has the
  tag; with B's Seen pill focused, past the dwell, `pane_focused` B makes A `box--seen` with
  `Mark unseen` last on its line, no Seen pill or tag, one `seen:herdr_focus` history row and no
  Undo bar, B carries the tag, and focus is still on B's Seen pill; `Mark unseen` has its text,
  title and accessible name and is drawn in `--ink-muted` with a 1px `--border` edge and radius
  999px (so removing `.unack` fails, tried); clicking it makes A Finished with its Seen pill, the
  bar `Marked 1: alpha not seen` with Undo, focus on A's Box; Undo closes the bar and A is seen
  again, history `herdr_focus`, `board` unseen, `board` seen; a second Mark unseen, then focus on
  A past the dwell and a leave, keeps A Finished with no further row; a flick C, Working, Needs you
  at 100 ms each marks nothing anywhere, and Needs you in view shows no tag; after `serve` stops
  (Boxes dimmed) and starts on the same data directory, every Box's kind and pills are as before;
  then a visit to A past the dwell and a leave keeps A Finished, and one to C marks C
  `seen:herdr_focus`.

**Acceptance Criteria:** every criterion of Story 6.8 in `docs/planning/epics.md` is met by the
tests above, including the focus criterion added by Story 6.7's review. The design gate is the
operator's dogfooding with live data (Story 6.9).

## Spec Change Log

## Review Triage Log

### 2026-10-27 — Self-review pass (blind, edge-case and verification-gap lenses, run inline)
- `[low]` `[patch]` The story names `ackError(status, code, seen)` with `seen` false for Mark
  unseen, but Story 6.4 already used `seen` false for the Undo of a mark. A fourth parameter,
  `undo`, keeps both readings and adds the Undo of a Mark unseen, whose refusal would otherwise
  have said "it is still marked seen" of an Agent that is not. Tested.
- `[low]` `[patch]` The first browser run assumed B was marked when focus moved from B to A; B's
  dwell there depends on how fast the preceding steps run, so the check no longer assumes it and
  the flick asserts that no pane gains a row.
- `[low]` `[patch]` Story 6.4's check that the Undo bar is visible failed once (visibility read on
  the fade's first frame, `visibility` transitions over 150 ms); it now waits for the bar to be
  visible, not merely on. Not caused by this story; it hardens an existing check that CI runs.
- `[low]` `[accept]` `● in view` on a detached herdr shows on the pane last focused, and with
  several attached clients on the foreground client's pane: ADR-0015's documented consequences,
  stated in the design's tag bullet. Story 6.9 documents them for the operator.
- `[low]` `[accept]` The dwell seam is an environment variable `serve` reads directly rather than
  configuration: the story asks for the smallest seam `cmd/herddash` can take, it is not listed in
  `docs/config.example.toml`, and `serve` logs a warning whenever it is set, so a Daemon running
  with it says so in its log.

## Design Notes

The tag and both pills follow from `boxKind`, so they cannot disagree with the class label, the
counts or `unseenFinished`: the tag is `finished` and `in_view`, the Seen pill `finished`, Mark
unseen `seen`. The Undo bar's action is always the opposite request of the one that showed it,
flagged `undo`, so an Undo never shows a bar of its own and a refused one names the state that
stands. A herdr focus move toggles `in_view` and redraws the Box; `render` already kept focus on
the Box, and now gives it to the same pill when the redrawn Box has one, so a keyboard user on a
Seen pill is not moved by herdr focus they did not cause.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test ./...` -- all packages pass
- `go test -race -count=50 -run TestFocusSeenDwellOption ./internal/engine` and
  `go test -race -count=50 -cpu=1,4,8 -run 'TestFocusSeenDwellOption|TestFocusShortDwell|TestFocusReadThenLeave' ./internal/engine` -- pass
- `node --test internal/httpapi/web/order.test.mjs` -- 107 pass, 0 fail
- `go build -o /tmp/herddash-e6b ./cmd/herddash && node internal/httpapi/web/check/board-browser.mjs /tmp/herddash-e6b`
  -- every check ok, exit 0, no herddash process of the check left; with `.in-view` removed it
  fails `in view: the .in-view rule draws it…`, and with `.unack` removed `Mark unseen: the
  .unack rule draws a quiet pill…`, exit 1 each
