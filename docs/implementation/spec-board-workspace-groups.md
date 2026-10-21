---
title: 'Board: Needs you on top, every other Agent grouped by workspace, with pane positions'
type: 'feature'
created: '2026-10-21'
status: 'done'
baseline_commit: 'cf0301086c46a07ce414e333350dec8295eb80ad'
route: 'dispatch'
review_loop_iteration: 1
context:
  - '{project-root}/docs/design/board-design.md'
  - '{project-root}/docs/implementation/spec-3-8-the-board-stays-live.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The operator's first question at the Board is which workspace, tab and pane to go to.
Today Finished Agents sit in a flat top zone away from their workspace, idle Agents are labelled
WORKING, two Agents in one tab look identical, and long workspace names push the tab out of sight.

**Approach:** Build the layout the operator approved on 2026-10-21 from a mock of their live
Board. Needs you alone stays at the top; every other Agent sits in its workspace's group, ordered
Working, Finished, Idle, Unknown; each Box leads with its tab, a pane-position badge and the
workspace or directory; idle Agents say IDLE; the browser tab title carries the Needs you count.

## Boundaries & Constraints

**Always:**
- **Order (core, AD-1):** `newSortKey` gets the Status. Zone 0 holds only `needs_you`, longest
  wait first. Zone 1 holds every other class, grouped by workspace position as today, and ranked
  inside a group: Working (class `working` with status `working`) 1, Finished 2, Idle (class
  `working` with any other status) 3, Unknown 4; then Start (longest wait first), then the key.
  `WorkspaceHeaders` is unchanged in rule. The golden fixture `internal/core/testdata/board-order.json`
  is regenerated to the new order, and the Board's `node --test` suite follows it.
- **Heading (core):** for Agent Program `claude` it is the Profile alone (`cldw`, `unresolved`);
  otherwise unchanged (`codex`, `profile · program`).
- **Pane position (herdr → core → AgentView):** `PaneContext` gains `PanePosition`, computed in
  core from the tab's layout rectangles that the snapshot's `layouts` carry: `""` when the tab
  has one pane; otherwise `left`/`right` (or `middle`) by column and `top`/`bottom` when another
  pane shares the column span, joined as `top-left` etc. `AgentView` gains `pane_position`. The
  lifecycle subscription adds `layout.updated` as a context-changed signal (regenerate the wire
  types), so positions refresh after splits and closes.
- **Box (Board), four fixed rows, height ~8.25rem:**
  1. the tab, large and bold (`1.25rem`, 700), with the pane-position badge right-aligned when
     non-empty; an empty tab shows the workspace;
  2. in the Needs you zone the workspace (smaller, semibold); in a workspace group the directory
     (monospace, faint, truncated from the start);
  3. the Context Line;
  4. the heading on the left; on the right, bold, the class label and Waiting Time, where class
     `working` with a status other than `working` reads `IDLE`. The time is never truncated.
  Hover `title` and the accessible name carry workspace, tab, pane, Context Line, directory,
  heading, class and time.
- **Colours:** Working blue edge (as now); Finished green edge (as now); Idle grey edge
  `--idle-edge` `#7E8592` light / `#6B7380` dark with label `--idle-accent` `#5A616E` / `#A7AEBA`;
  Needs you fill and ring as now; Unknown dashed.
- **Group headings:** the workspace label large and bold (`1.375rem`, 800) with counts of
  working, finished and idle Agents in that group (omitting zeros); the Needs you zone has a
  plain "Needs you" heading.
- **Title:** `document.title` is `(N) herddash · Needs you` when N > 0 Agents need you, else
  `herddash`.
- Docs: `docs/design/board-design.md` and PRD FR-2/FR-3 amended with a dated note naming the
  operator's 2026-10-21 decision; the strips and CSP from Stories 3.7–3.9 unchanged.

**Never:**
- Read pane output, focus panes, or add any other Box element (NFR10): the pane badge is part of
  the Box's location, with workspace and tab.
- Re-derive order, class or heading in JavaScript beyond the `IDLE` label already derived by
  `neverWorking`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Order | needs_you, working, finished, idle in one workspace | Needs you zone first; group: working, finished, idle | test fails |
| Two workspaces | Agents in workspace 2 and 1 | group 1 before group 2 | test fails |
| Idle label | class working, status idle | label `IDLE`, grey edge | test fails |
| One pane | tab with one pane | no badge (`pane_position` "") | test fails |
| Two columns | panes side by side | `left`, `right` | test fails |
| Three panes | one left, two stacked right | `left`, `top-right`, `bottom-right` | test fails |
| Heading | claude/cldw; claude/unresolved; codex/codex | `cldw`; `unresolved`; `codex` | test fails |
| Title | 2 Agents need you | `(2) herddash · Needs you` | test fails |
| Layout change | `layout.updated` event | another snapshot read | test fails |

</frozen-after-approval>

## Code Map

- `internal/core/view.go` -- `newSortKey(class, workspacePosition, since, key)` (~185), its doc (~160), `AgentView`, `NewAgentView`, `WorkspaceHeaders`
- `internal/core/profile.go:51` -- `Heading`
- `internal/core/observe.go:46` -- `PaneContext`; add `PanePosition`; a pure position function from rectangles belongs in core
- `internal/core/testdata/board-order.json`, `view_test.go` -- golden order (regenerate)
- `internal/herdr/map.go` -- snapshot mapping (`mapSnapshot`); `gen.SessionSnapshot.Layouts` is `[]PaneLayoutSnapshot{TabID, Panes[]{PaneID, Rect{X,Y,Width,Height}}}`
- `internal/herdr/subscribe.go:115` -- context-changed subscription kinds; `internal/herdr/gen/genwire/main.go` roots; regenerate with `go generate ./internal/herdr/gen`
- `internal/engine` -- carries `PaneContext` into views; no logic change expected
- `internal/httpapi/web/order.mjs` (`layout`, `boxLabel`, `classLabel`, `neverWorking`, `whereLabel`), `order.test.mjs`, `board.mjs` (`fillBox`, `render`, title), `board.css`
- `docs/design/board-design.md`, `docs/prd.md` FR-2/FR-3
- The approved mock (reference only, not committed): `/private/tmp/claude-501/-Users-jasonviloria-GitHub-herddash/e40daa79-6ca4-4e95-90df-36f730e6ae23/scratchpad/board-mock.html`
- Toolchain: Go `~/.local/share/mise/installs/go/1.26.5/bin`, Node `~/.local/share/mise/installs/node/22/bin`

## Tasks & Acceptance

**Execution:**
- [x] `internal/core` -- sort key, heading, pane position, golden fixture and tests
- [x] `internal/herdr` (+ `gen`) -- map layouts to `PanePosition`, subscribe to `layout.updated`, tests
- [x] `internal/httpapi/web` -- Box, groups, headings, IDLE, title, CSS, node tests
- [x] `docs/design/board-design.md`, `docs/prd.md` -- the approved change, dated

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` and `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs` run, then every matrix row passes and arch_test passes.
- Given `herddash serve` built from this branch against the live herdr on a second port with its own data directory, when the Board is loaded, then it matches the approved mock (checked by screenshot).

## Implementation Notes

- `core.PanePositions([]PaneRect)` (`internal/core/pane_position.go`) names each pane of one tab: horizontal by touching the tab's left/right edge (`left`, `right`, `middle`, nothing when full width); vertical only when another pane's horizontal extent overlaps (`top`, `bottom`, `middle`). Adjacent rectangles (herdr's real dump has `x 0 w 60` beside `x 60`) do not overlap. `mapSnapshot` maps `layouts` by tab id; a tab without a layout gets "".
- `newSortKey` takes the Status; `ClassRank` is needs_you 0, Working 1, Finished 2, Idle 3, Unknown 4; zone 0 is needs_you only. The golden fixture was regenerated from core's own output (three inputs gained `pane_position`); its `herddash` group now reads working, finished, idle, unknown.
- `layout.updated` decodes to `LifecycleContextChanged`, which the engine already answers with a snapshot read (`internal/engine` `TestContextChanged`); engine code is unchanged, two engine test expectations follow the new order and heading.
- JS: `neverWorking` became `isIdle` (class working, Status not working), plus `boxKind`, `tabLabel`, `paneLabel`, `inNeedsYou` (by `sort_key[0]`), `groupCounts`, `documentTitle`. Row titles were dropped: the Box's own `title` equals its accessible name, so hovering anywhere shows everything.
- Checked by screenshot: `herddash serve -bind 127.0.0.1:7399` with its own data directory against the live herdr (read-only, as the running Daemon does) rendered the live groups with pane badges and 8.25rem Boxes and no console warnings; with no Agent then blocked, the Needs you zone was checked with the mock's own data served to the real `board.mjs` from a scratch stub, in dark, light and 360px: it matches the mock.

## Spec Change Log

## Review Triage Log

Three reviewers (blind, edge-case, verification-gap) on 2026-10-21.

- **Patched — duplicate pane positions.** Four columns or rows named two panes `middle`, and a
  full-width pane over a split column made that column's top read `middle-left`. The vertical
  part is now measured within the pane's column (overlapping panes no wider than it), and any name
  still shared within a tab is numbered in reading order (`middle 1`, `middle 2`). Tests: four
  columns, three rows, one wide pane over two, and wide pane over a column of three.
- **Patched — Idle to Working kept the idle clock.** An Agent idle since before it was ever seen
  working showed `WORKING ≥ 3h` once it started. `FoldResult.Add` now restarts `Since` at the
  first working Transition; `TestFold` and the reference fold in `TestAddEqualsFold` follow.
- **Patched — Unknown Waiting Time in full ink.** `.box--unknown .wait` is muted again.
- **Patched — SM-C1 and NFR10.** Both say the Board carries only Boxes, workspace labels and
  strips; amended with a dated note that the group counts and the Needs you heading are zone
  labels, not a control, excerpt or second per-Box datum.
- **Dismissed — `layout.updated` bursts.** Reads are coalesced and rate-limited by
  `requestSnapshot`'s `snapshotMinInterval`, so a burst costs one snapshot read.
- **Dismissed — the restart regression seen in preview.** A stale preview on the same port with an
  empty scratch data directory was answering; on a backup of the live record the branch matches
  0.3.0 Agent for Agent.
- **Kept — `isIdle` reads the Status.** The spec allows exactly this derivation, and it is the
  same rule as core's rank.
- **Deferred — zoomed tabs** (UNVERIFIED what herdr reports for `zoomed: true`); **workspaces with
  an unknown position share one group** (pre-existing); **counts leave out Unknown** (as the spec
  says); **the title keeps its count while disconnected** (the strip says the data is stale);
  **an empty tab repeats the workspace in Needs you** (as the spec says); **no DOM test of
  `fillBox`** (checked by screenshot, as Stories 3.7–3.9 were).

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `go generate ./internal/herdr/gen && git diff --exit-code internal/herdr/gen` -- expected: no diff after committing the regeneration
