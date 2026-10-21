# Board design (Story 3.6)

**Status: proposed, awaiting the operator's approval.** Story 3.7 does not start until it is
approved. Approval means the operator sets the `action_items` entry "Story 3.6: operator approves
the Board design before Story 3.7 starts" in `docs/implementation/sprint-status.yaml` to
`status: done`.

The Board gets glanced at from across a desk all day. Needs you has to win that glance and
nothing else may compete with it (ADR-0003, FR-3, NFR10, NFR14). [`board.html`](board.html)
renders [`board-fixture.json`](board-fixture.json) in this design. Open it from disk; it needs no
network. Its striped, dashed toolbar is for design review only and is not part of the Board.

## Tokens

System fonts only: `system-ui` for text and `ui-monospace` for Waiting Times and directories.
Times use tabular figures so they do not jitter as they tick. Spacing runs on a 4px scale:
`0.25, 0.5, 0.75, 1, 1.5, 2rem`. Type is set in rem: label `0.8125`, meta `0.875`, body `1`,
workspace and tab `1.25` (bold), Needs you time `1.125`. Meta (`0.875`) sets the directory and the
heading.

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
| `--focus` outline | `#1D4ED8` | `#7AA2FF` | 6.1 / 7.7 on bg |
| strips: disconnected, advisory, refused | slate, amber, red | the same, darkened | 10.2, 9.4, 8.9 / 9.7, 10.8, 7.4 |

Every text pair meets WCAG AA (4.5:1), and the Needs you fill clears 3:1 against the page by a
wide margin in both themes. Dark mode switches every token: by the system setting, or by
`data-theme` on `<html>`.

## The Box

The grid is `repeat(auto-fill, minmax(min(100%, 17rem), 1fr))`. **Every Box is `height: 9.5rem`**,
built from four fixed-height rows. Every text row is `nowrap` with an ellipsis, and so is the
Waiting Time in the last row, so no content can change a Box's height. Empty fields keep their
row, with no stray separator and no tooltip. The rows, in reading order:

1. **Workspace › tab**, which is how the operator finds the pane in herdr, in the Box's largest
   and boldest text (`1.25rem`, 700). When the line is too long, the workspace name gives way
   first, with an ellipsis, so the tab stays whole; the tab is truncated only past 75% of the
   width. In the lower zone the workspace is also the group's header. An empty label is left out
   together with its `›`.
2. **Context Line**: one line, ellipsis, full text in `title`. The row is always present, so an
   empty Context Line keeps its space.
3. **Directory** (monospace), with full text in `title`. When it is too long it is truncated from
   the start (`…sted/path/to/the-project-name`), so the last segment, the project, stays
   visible. `~` for the home directory is substituted by the Daemon (`core.ShortenDir`, AD-1),
   not by the Board.
4. **Heading and state.** On the left the heading, `profile · program` exactly as the API sends
   it (`cldw · claude`, `unresolved · claude`, `codex`), in the quiet meta style; it is the part
   that gives way with an ellipsis in a narrow Box. On the right, in bold, the **Attention Class
   label** (uppercase text) and the **Waiting Time**. A lower bound reads `≥ 12m`. An absent
   `waiting_since` shows a dash, labelled "Waiting Time unknown". It ticks from `waiting_since`.
   Its format has defined boundaries, with the second unit always two digits: `0s` to `59s`, `1m`
   to `59m`, `1h 00m` to `23h 59m`, then `1d 00h` onwards.

*Changed 2026-10-21 at the operator's request, twice.* First, workspace › tab moved from the last
row to the second and the heading to the last. Then the operator said the state matters less than
where the pane is: workspace › tab became the first and largest line, and the Attention Class and
Waiting Time moved to the last row, bold, beside the heading. The Box lost a row and shrank from
10.5rem to 9.5rem. Working gained a blue leading edge to match Finished's green one.
`docs/design/board.html` predates both changes.

**Order.** Boxes arrive sorted by `sort_key`. A Board that re-sorts must tie-break exactly as the
Daemon does (`internal/core/view.go`, `SortKey.compare`): zone, group, class rank, start, then
the AgentKey as host, server, epoch (numerically) and pane id. It must never compare
`sort_key[4]` as one string. Otherwise it should trust the API's order. The design page compares
the same way.

## Emphasis by class

- **Needs you is dominant.** It has a solid fill, a 3px ring, white (light) or near-black (dark)
  text, a bold Context Line and an extra-bold Waiting Time. It never relies on colour alone: the
  uppercase `NEEDS YOU` label, the heavy ring and the weight carry it in greyscale too, and it is
  always first on the page.
- **Finished is quieter.** It sits on a raised surface with a 6px green leading edge and a green
  `FINISHED` label.
- **Working has a blue leading edge**, the same 6px bar as Finished in blue, flush with the page
  rather than raised, with a blue `WORKING` label. Blue says busy, green says done; the label
  says it in text too.
- **Unknown recedes.** It sits flush with the page with a dashed hairline border and muted text.
- **An `idle` Agent never seen `working`** (status `idle`, class Working) gets no Waiting Time
  emphasis: its time is set small in `--ink-faint`.

## Zones and workspace headers

The top zone is a flat grid of Needs you, then Finished, with no header. The lower zone follows
after a 2rem gap. At each index in `headers` it draws a thin workspace header: a 1px rule across
the full width, then the workspace label in small semibold muted text. The first header's rule is
what separates the two zones. The page adds no other dividers.

Empty states:
- With an empty top zone, the first workspace header draws no rule.
- With no lower zone, nothing is drawn below the top zone.
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

DOM order is the reading order: strips (a polite live region), then the top zone, then the lower
zone by workspace. Each zone has a visually hidden heading, and workspace labels are headings.
Each Box is focusable and shows a 3px `--focus` outline offset outside it, and the review
toolbar's controls show the same outline. Boxes are focusable so that keyboard and screen-reader
users can read a Box in full. Each Box's accessible name carries its Attention Class, Waiting Time
("at least" for a lower bound), workspace and tab, full Context Line, directory, and heading, and
it is kept current as the time ticks. Focusing a Box does nothing else. Needs you is conveyed in
text, not only colour.

## Deliberately absent (NFR10, SM-C1)

The Board has no controls: no focus, send, kill or open-in-terminal action, no filters, no
search and no settings. It shows no pane output, excerpts or scrollback, and no second data type
per Box: no history, charts, counts or status totals. It has no title bar, logo or footer. The
Board is Boxes, workspace labels and the two strips. Anything more needs an ADR superseding
ADR-0003.
