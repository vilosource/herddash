# Board design (Story 3.6)

**Status: approved 2026-10-20, amended 2026-10-21.** On 2026-10-21 the operator approved, from a
mock of their live Board, a layout that answers their first question, which workspace, tab and
pane to go to: Needs you alone on top, every other Agent in its workspace's group, and each Box
leading with its tab and pane position. This document describes the amended Board; where it
differs from [`board.html`](board.html), this document wins.

The Board gets glanced at from across a desk all day. Needs you has to win that glance and
nothing else may compete with it (ADR-0003, FR-3, NFR10, NFR14). [`board.html`](board.html)
renders [`board-fixture.json`](board-fixture.json) in this design. Open it from disk; it needs no
network. Its striped, dashed toolbar is for design review only and is not part of the Board.

## Tokens

System fonts only: `system-ui` for text and `ui-monospace` for Waiting Times and directories.
Times use tabular figures so they do not jitter as they tick. Spacing runs on a 4px scale:
`0.25, 0.5, 0.75, 1, 1.5, 2rem`. Type is set in rem: label `0.8125`, meta `0.875`, body `1`,
tab `1.25` (bold), group heading `1.375` (extra-bold), Needs you time `1.125`. Meta (`0.875`) sets
the workspace line and the heading; label (`0.8125`) sets the directory and the class.

| Token | Light | Dark | Contrast (light / dark) |
|---|---|---|---|
| `--bg` page | `#F3F4F6` | `#0E1014` | |
| `--surface` Finished Box | `#FFFFFF` | `#1A1E25` | |
| `--ink` text | `#16191F` | `#E9EBEF` | 16.0 / 16.0 on bg |
| `--ink-muted` Working text, headers | `#4B5361` | `#A7AEBA` | 7.0 / 8.5 on bg |
| `--ink-faint` unemphasised time, dash | `#646C79` | `#8B93A0` | 4.8 / 6.2 on bg |
| `--needs-bg` Needs you fill | `#B42318` | `#FF7A59` | **6.0 / 7.4 against the page** |
| `--needs-ink` / `--needs-ink-2` | `#FFFFFF` / `#FFE1DC` | `#1A0703` / `#3D1307` | 6.6, 5.3 / 7.6, 6.3 on the fill |
| `--finished-accent` label | `#17663F` | `#5BD69A` | 7.0 / 9.2 on surface |
| `--finished-edge` leading edge | `#1F8A55` | `#3FB37B` | 4.0 / 7.2 on bg (non-text, ≥ 3) |
| `--working-accent` label | `#2D5A96` | `#8DB6EE` | 6.3 / 9.1 on bg |
| `--working-edge` leading edge | `#3E6FB0` | `#5B8FD6` | 4.7 / 5.8 on bg (non-text, ≥ 3) |
| `--idle-accent` label | `#5A616E` | `#A7AEBA` | 5.7 / 8.5 on bg |
| `--idle-edge` leading edge | `#7E8592` | `#6B7380` | 3.4 / 4.0 on bg (non-text, ≥ 3) |
| `--badge-bg` / `--badge-ink` pane badge | `#E4E7EC` / `#2B313B` | `#2A303A` / `#DDE1E7` | 10.6 / 10.1 |
| `--focus` outline | `#1D4ED8` | `#7AA2FF` | 6.1 / 7.7 on bg |
| strips: disconnected, advisory, refused | slate, amber, red | the same, darkened | 10.2, 9.4, 8.9 / 9.7, 10.8, 7.4 |

Every text pair meets WCAG AA (4.5:1), and the Needs you fill clears 3:1 against the page by a
wide margin in both themes. Dark mode switches every token: by the system setting, or by
`data-theme` on `<html>`.

## The Box

The grid is `repeat(auto-fill, minmax(min(100%, 17rem), 1fr))`. **Every Box is `height: 8.25rem`**,
built from four fixed-height rows. Every text row is `nowrap` with an ellipsis, so no content can
change a Box's height. Empty fields keep their row. The rows, in reading order:

1. **The tab**, which is how the operator finds the pane in herdr, in the Box's largest and
   boldest text (`1.25rem`, 700). An empty tab shows the workspace instead. When the tab holds
   more than one pane, a **pane-position badge** sits right-aligned on the same line: `left`,
   `right`, `middle`, `top`, `bottom`, or a pair such as `top-right`, computed by the Daemon from
   the tab's layout rectangles (`core.PanePositions`). It is part of the Box's location, with the
   workspace and tab, and the only thing that tells two Agents in one tab apart.
2. **The workspace or the directory.** In the Needs you zone, the workspace (`0.875rem`,
   semibold). In a workspace group, whose heading already names the workspace, the directory
   (monospace, faint), truncated from the start (`…sted/path/to/the-project-name`) so the last
   segment, the project, stays visible. `~` for the home directory is substituted by the Daemon
   (`core.ShortenDir`, AD-1), not by the Board.
3. **Context Line**: one line, ellipsis. The row is always present, so an empty Context Line
   keeps its space.
4. **Heading and state.** On the left the heading exactly as the API sends it: the Profile alone
   for Claude Code (`cldw`, `unresolved`), otherwise `profile · program` or the program alone
   (`codex`), in the quiet meta style; it is the part that gives way with an ellipsis in a narrow
   Box. On the right, in bold, the **class label** (uppercase text) and the **Waiting Time**,
   which is never truncated. Class Working with a Status other than `working` reads `IDLE`. A
   lower bound reads `≥ 12m`. An absent `waiting_since` shows a dash. It ticks from
   `waiting_since`. Its format has defined boundaries, with the second unit always two digits:
   `0s` to `59s`, `1m` to `59m`, `1h 00m` to `23h 59m`, then `1d 00h` onwards.

The Box's hover `title` and its accessible name both carry everything, untruncated: workspace and
tab, pane position, Context Line, directory, heading, class and Waiting Time. The rows carry no
titles of their own, so hovering anywhere on the Box shows it all.

*Changed 2026-10-21 at the operator's request, three times.* First, workspace › tab moved from
the last row to the second and the heading to the last. Then workspace › tab became the first and
largest line, and the Attention Class and Waiting Time moved to the last row, bold, beside the
heading; the Box shrank from 10.5rem to 9.5rem and Working gained a blue leading edge. Last, from
a mock of the live Board: long workspace names pushed the tab out of sight and two Agents in one
tab looked identical, so the first line became the tab and the pane badge, the workspace moved to
the group heading (or the second line in Needs you), the heading lost ` · claude`, idle Agents
read `IDLE`, and the Box shrank to 8.25rem. `docs/design/board.html` predates all three.

**Order.** Boxes arrive sorted by `sort_key`, `[zone, group, class_rank, start, key]`. Zone 0 is
Needs you alone; zone 1 is every other Agent, grouped by workspace position and ranked Working 1,
Finished 2, Idle 3, Unknown 4. A Board that re-sorts must tie-break exactly as the Daemon does
(`internal/core/view.go`, `SortKey.compare`): zone, group, class rank, start, then the AgentKey
as host, server, epoch (numerically) and pane id. It must never compare `sort_key[4]` as one
string. Otherwise it should trust the API's order.

## Emphasis by class

- **Needs you is dominant.** It has a solid fill, a 3px ring, white (light) or near-black (dark)
  text, a bold Context Line and an extra-bold Waiting Time. It never relies on colour alone: the
  uppercase `NEEDS YOU` label, the heavy ring and the weight carry it in greyscale too, and it is
  always first on the page.
- **Finished is quieter.** It sits on a raised surface with a 6px green leading edge and a green
  `FINISHED` label, in its workspace's group.
- **Working has a blue leading edge**, the same 6px bar as Finished in blue, flush with the page
  rather than raised, with a blue `WORKING` label. Blue says busy, green says done; the label
  says it in text too.
- **Unknown recedes.** It sits flush with the page with a dashed hairline border and muted text.
- **Idle is grey.** An Agent of class Working whose Status is not `working`, such as one idle
  since before it was ever seen working, has a 6px grey leading edge (`--idle-edge`), an `IDLE`
  label in `--idle-accent`, and no Waiting Time emphasis: its time is set in `--ink-faint`. It is
  the one thing the Board derives from the Status, and the Daemon ranks the same Agents Idle.

## Zones and workspace groups

*Amended 2026-10-21 (operator decision).* The top zone holds **Needs you alone**, longest wait
first, flat across workspaces, under a plain visible "Needs you" heading. Below it, one group per
workspace in herdr's workspace order (`headers` gives where each starts), each holding that
workspace's Working, then Finished, then Idle, then Unknown Agents. A group's heading is the
workspace label, large and bold (`1.375rem`, 800), followed by its counts in muted meta text,
`2 working · 1 finished · 3 idle`, leaving out zeros. Every heading but the first on the page
draws a 1px rule above it. The page adds no other dividers.

The browser tab's title carries the Needs you count: `(2) herddash · Needs you` when two Agents
need you, else `herddash`, so the tab says it while the Board is out of sight.

Empty states:
- With no Agent needing you, the Needs you zone and its heading are not drawn, and the first
  workspace heading draws no rule.
- With no other Agent, nothing is drawn below the Needs you zone.
- With zero Agents, the Board is bare: just the page background and any strip. The spec admits
  no other element, so there is no "no agents" message. **That is a deliberate choice for the
  operator to confirm.**

## Strips

Strips are thin, full width, stuck to the top, and shown only when needed. When both appear,
connection comes first, then version.

- **Disconnected** (`connected: false`): a slate strip reading "Disconnected from herdr. Last
  snapshot 2m 40s ago; Boxes show that snapshot." The age ticks as `Mm SSs`, then in the Waiting
  Time format. It is `aria-hidden`; screen readers get the fixed UTC time instead, so the live
  region announces only the strip's appearance and changes. Every Box is dimmed to 50% opacity
  and desaturated, never hidden. The dimming deliberately lowers contrast, because the state is
  stale. **Waiting Times keep ticking while disconnected:** the wait is real, the Boxes are dimmed,
  and the strip says how old the data is.
  - If there has never been a snapshot (`last_snapshot_at` absent), the same slate strip reads
    "Disconnected from herdr. No snapshot received yet." There is no age to show, and with no
    snapshot there are no Boxes, so the Board is bare beneath the strip.
- **Advisory** (`gate: advisory`): an amber strip saying herdr *X* is below the supported minimum
  *Y* and herddash is recording under the dogfooding flag. When `resync_guaranteed` is false
  (herdr below 0.9.0), it adds that resync is not guaranteed, because herdr can replay stale events
  and cannot signal lost ones. Boxes are shown.
- **Refused** (`gate: refused`): a red strip naming `herdr_version` and `herdr_minimum` and saying
  nothing is being recorded. No Boxes are shown.

## Responsive behaviour

The layout runs from a side window of about 320px, with one column and a 16px gutter, to a wide
monitor, with as many 17rem-minimum columns as fit. The gutter is 16px at every width. Box height never changes. Only the column
count does. Strips wrap to two lines on a narrow window. There is no horizontal scroll at any
width.

## Accessibility

DOM order is the reading order: strips (a polite live region), then the Needs you zone, then the
workspace groups. The Needs you heading is visible; the lower zone has a visually hidden heading,
and workspace labels are headings.
Each Box is focusable and shows a 3px `--focus` outline offset outside it, and the review
toolbar's controls show the same outline. Boxes are focusable so that keyboard and screen-reader
users can read a Box in full. Each Box's accessible name carries its workspace and tab, pane position, full
Context Line, directory, heading, class label and Waiting Time ("at least" for a lower bound),
and it is kept current as the time ticks. Focusing a Box does nothing else. Needs you is conveyed in
text, not only colour.

## Deliberately absent (NFR10, SM-C1)

The Board has no controls: no focus, send, kill or open-in-terminal action, no filters, no
search and no settings. It shows no pane output, excerpts or scrollback, and no second data type
per Box: no history or charts. The only counts are each workspace group's heading counts
(amended 2026-10-21). It has no title bar, logo or footer. The Board is Boxes, the Needs you and
workspace headings and the two strips. Anything more needs an ADR superseding
ADR-0003.
