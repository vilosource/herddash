# Board design (Story 3.6)

**Status: approved 2026-10-20, amended 2026-10-21 and 2026-10-27 (ADR-0014, the acknowledgement
controls, from a mock the operator approved with their live Board; and ADR-0015, the in-view tag
and the Mark unseen pill, decided by the operator while dogfooding).** On 2026-10-21 the operator approved, from a
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
| `--needs-bg` Needs you fill | `#B42318` | `#F2555A` | **6.0 / 5.6 against the page** |
| `--needs-ink` / `--needs-ink-2` | `#FFFFFF` / `#FFE1DC` | `#1A0505` / `#3A0C0C` | 6.6, 5.3 / 5.8, 5.0 on the fill |
| `--finished-accent` label | `#2D5A96` | `#8DB6EE` | 7.0 / 8.0 on surface |
| `--finished-edge` leading edge | `#3E6FB0` | `#5B8FD6` | 4.6 / 5.8 on bg (non-text, ≥ 3) |
| `--working-accent` label | `#17663F` | `#5BD69A` | 6.3 / 10.5 on bg |
| `--working-edge` leading edge | `#1F8A55` | `#3FB37B` | 4.0 / 7.2 on bg (non-text, ≥ 3) |
| `--idle-accent` label | `#8A5300` | `#FFC15E` | 5.8 / 11.8 on bg |
| `--idle-edge` leading edge | `#C77700` | `#F5A524` | 3.1 / 9.3 on bg (non-text, ≥ 3) |
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
   workspace and tab, and the only thing that tells two Agents in one tab apart. *Added
   2026-10-27, ADR-0014:* a Finished Box, and no other kind, ends this line with its **Seen
   pill** (see "Acknowledgement controls"), after the badge when there is one. *Amended
   2026-10-27, ADR-0015:* a Finished Box whose pane is focused in herdr shows the **in-view tag**
   `● in view` before its Seen pill, and a Seen Box ends this line with a **Mark unseen pill**.
2. **The workspace or the directory.** In the Needs you zone, the workspace (`0.875rem`,
   semibold). In a workspace group, whose heading already names the workspace, the directory
   (monospace, faint), truncated from the start (`…sted/path/to/the-project-name`) so the last
   segment, the project, stays visible. `~` for the home directory is substituted by the Daemon
   (`core.ShortenDir`, AD-1), not by the Board.
3. **Context Line**: one line, ellipsis. The row is always present, so an empty Context Line
   keeps its space. *Added 2026-10-22, ADR-0010:* when the AgentView's `recap` is not empty
   (Claude Code's own session recap, sent only when the operator set `claude_recaps`), the recap
   takes this row in place of the Context Line (class `recap`), wrapping to at most **two lines**
   with an ellipsis (`line-clamp: 2`). A Box with no recap keeps its Context Line.
4. **Heading and state.** On the left the heading exactly as the API sends it: the Profile alone
   for Claude Code (`cldw`, `unresolved`), otherwise `profile · program` or the program alone
   (`codex`), in the quiet meta style; it is the part that gives way with an ellipsis in a narrow
   Box. On the right, in bold, the **class label** (uppercase text) and the **Waiting Time**,
   which is never truncated. Class Working with a Status other than `working` reads `IDLE`. An
   Agent the Daemon marks `background` reads `BACKGROUND`, whatever its class. *Added 2026-10-27,
   ADR-0014:* a Finished Agent the Daemon marks `seen` reads `SEEN`, unless it is Background,
   which comes first. *Added
   2026-10-21, ADR-0009:* the label carries the AgentView's `background_tasks` count: a Working
   Box with tasks open reads `WORKING · N bg`, a Background Box with more than one reads
   `BACKGROUND · N` (`BACKGROUND` alone for one); the count, set in the label's colour, keeps its
   lower-case unit (`bg`) while the class word stays uppercase. No other class shows a count.
   Group counts are unchanged: each Box counts under the kind it is drawn as, so a `working`
   Agent shown as Background counts as background, not working. A
   lower bound reads `≥ 12m`. An absent `waiting_since` shows a dash. It ticks from
   `waiting_since`. Its format has defined boundaries, with the second unit always two digits:
   `0s` to `59s`, `1m` to `59m`, `1h 00m` to `23h 59m`, then `1d 00h` onwards.

**Height with recaps** (*added 2026-10-22, ADR-0010*). Every Box keeps one height. While any Box
on the Board has a recap, the Board carries `board--recaps` and every Box is one line taller,
`calc(8.25rem + 1.5rem)`, its context row `3rem` (two lines of `1.5rem`) instead of `1.5rem`; a
Box without a recap shows its one-line Context Line in that taller row. With no recap anywhere,
the layout and heights are exactly as above.

The Box's hover `title` and its accessible name both carry everything, untruncated: workspace and
tab, pane position, Context Line (or, *added 2026-10-22 for ADR-0010*, the full recap in its
place), directory, heading, class, its background tasks in words
("2 background tasks", added 2026-10-21 for ADR-0009) and Waiting Time. A seen Box's class in
its accessible name is `Seen` (ADR-0014). The rows carry no
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
Needs you alone, longest wait first; zone 1 is every other Agent, grouped by workspace position
and ranked Finished 1, Idle 2, Background 3, Working 4, Unknown 5, each class newest first, so a
zone-1 `start` is the negated class start; a seen Agent (ADR-0014, 2026-10-27) ranks 2 with Idle,
by the same newest-first start, so it moves down among the Idle Boxes (operator decision 2026-10-21, which replaced Working
1, Background 2, Finished 3, Idle 4, Unknown 5). A workspace herdr did not position gets its own
group and header after the positioned ones (Story 5.13); such groups come in a stable but
arbitrary order, a hash of the workspace id, each headed by its label or `(unlisted workspace)`.
Agents with no workspace id at all come last, under `(no workspace)`. A Board that re-sorts must
tie-break exactly as the Daemon does (`internal/core/view.go`, `SortKey.compare`): zone, group,
class rank, start, then the AgentKey as host, server, epoch (numerically) and pane id. It must
never compare `sort_key[4]` as one string. Otherwise it should trust the API's order.

## Emphasis by class

*Amended 2026-10-21 (operator decision): traffic lights.* Red is Needs you, amber is Idle and
green is Working; Finished, which green meant before, takes the blue Working gave up. The dark
Needs you fill moved from red-orange to red so it cannot be mistaken for amber.

- **Needs you is dominant.** It has a solid fill, a 3px ring, white (light) or near-black (dark)
  text, a bold Context Line and an extra-bold Waiting Time. It never relies on colour alone: the
  uppercase `NEEDS YOU` label, the heavy ring and the weight carry it in greyscale too, and it is
  always first on the page.
- **Finished is quieter.** It sits on a raised surface with a 6px blue leading edge and a blue
  `FINISHED` label, in its workspace's group.
- **Working has a green leading edge**, the same 6px bar as Finished in green, flush with the
  page rather than raised, with a green `WORKING` label. Green says busy and fine, blue says
  done; the label says it in text too.
- **Unknown recedes.** It sits flush with the page with a dashed hairline border and muted text.
- **Idle is amber.** An Agent of class Working whose Status is not `working`, such as one idle
  since before it was ever seen working, has a 6px amber leading edge (`--idle-edge`), an `IDLE`
  label in `--idle-accent`, and no Waiting Time emphasis: its time is set in `--ink-faint`. It is
  the one thing the Board derives from the Status, and the Daemon ranks the same Agents Idle.
- **Background is Working's edge, dashed.** *Added 2026-10-21, operator request.* An Agent whose
  Status is `done` or `idle` but whose process still runs a background task (the AgentView's
  `background: true`, decided by the Daemon and never derived by the Board) has a 6px **dashed**
  leading edge in `--working-edge` and a `BACKGROUND` label in `--working-accent`, flush with the
  page like Working, so it reads as not yet done; its Waiting Time is in full ink, as Finished's,
  counting from when the turn ended; a `working` Agent shown as Background (ADR-0009) keeps
  Working's Waiting Time, counting from when it started working, since the Daemon's Waiting Time
  is the Attention Class's and never changes for background work. It takes precedence over Finished and Idle, and (ADR-0009)
  over Working for a `working` Agent whose main turn has ended while background tasks run. The dash, the
  label and its place right before Working carry it without colour.

- **Seen is grey.** *Added 2026-10-27, ADR-0014.* A Finished Agent the operator marked seen
  (the AgentView's `seen: true`, decided by the Daemon and never derived by the Board) has a 6px
  leading edge in `--border-quiet`, flush with the page, a `SEEN` label in `--ink-faint`, its
  tab and Context Line in `--ink-muted`, and its Waiting Time in `--ink-faint` at weight 600, as
  Idle's: it is dealt with, so it recedes. It carries no Seen pill. Background takes precedence
  over it. *Amended 2026-10-27, ADR-0015:* it carries a quiet `Mark unseen` pill instead, and it
  looks the same whether the operator clicked Seen or the Daemon marked it when they left the
  pane in herdr; the source is in the Agent's history, not on the Board.

## Zones and workspace groups

*Amended 2026-10-21 (operator decision).* The top zone holds **Needs you alone**, longest wait
first, flat across workspaces, under a plain visible "Needs you" heading. Below it, one group per
workspace in herdr's workspace order (`headers` gives where each starts), each holding that
workspace's Finished, then Idle (with Seen among them, by the Daemon's order, ADR-0014), then
Background, then Working, then Unknown Agents, each class most recent first: what to deal with
first comes first (operator decision 2026-10-21). A group's heading is the workspace label, large
and bold (`1.375rem`, 800), followed by its counts in muted meta text in the same order,
`1 finished · 3 idle · 2 seen · 1 background · 2 working`, leaving out zeros; a seen Agent counts
as seen, not finished, so the finished count is the results not yet read (amended 2026-10-27,
ADR-0014). After the counts, at the right, the heading may show its bulk mark (see
"Acknowledgement controls"). Every heading but the first on the page
draws a 1px rule above it. The page adds no other dividers.

The browser tab's title carries the Needs you count: `(2) herddash · Needs you` when two Agents
need you, else `herddash`, so the tab says it while the Board is out of sight.

Empty states:
- With no Agent needing you, the Needs you zone and its heading are not drawn, and the first
  workspace heading draws no rule.
- With no other Agent, nothing is drawn below the Needs you zone.
- With zero Agents, the Board is bare: just the title bar and any strip. The spec admits
  no other element, so there is no "no agents" message. **That is a deliberate choice for the
  operator to confirm.**

## Title bar

Added 2026-10-26 (ADR-0013, Story 5.19), approved by the operator from the Board rendered with
their live data. One bar, `--surface` with a `--border` rule below it, sticky at the top with the
strips beneath it:

- **Left, the text logo:** a mark of three bottom-aligned bars, `--needs-bg`, `--idle-edge` and
  `--working-edge` at full, 70% and 45% height, then `herddash` in `--font-mono` at
  `--text-where`, `herd` 800 in `--ink`, `dash` 400 in `--ink-muted`. It is the page's `h1`.
- **Right, the summary** at `--text-meta`, each item led by an 8px dot:
  - the Needs you count: "1 needs you" or "N need you", bold `--ink` with a `--needs-bg` dot,
    or "Nothing needs you" muted with an `--ink-faint` dot. It is 0 while the Boxes are hidden,
    as the browser title's is.
  - herdr: `herdr <version>` with a `--working-edge` dot, `--idle-edge` when the gate is advisory
    and `--needs-bg` when it hides the Boxes; "herdr disconnected" or "Reconnecting…", semibold
    with an `--ink-faint` dot, while the disconnected strip shows.
- The summary restates what the strips and the browser title say. It is not a live region and
  holds no control. On a narrow screen it wraps under the logo.

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
- **Version unreadable** (*added 2026-10-23*): when `herdr_version` is empty or absent, herddash
  could not read herdr's version, and the gate treats it as below the minimum. The strips say so
  instead of calling it old. Refused reads "herddash could not read herdr's version. It needs
  *Y* or newer and is recording nothing." Advisory reads "herddash could not read herdr's version;
  recording under the dogfooding flag.", plus the resync sentence when `resync_guaranteed` is not
  true.

**A silent Daemon is a lost stream.** *Added 2026-10-23 (ADR-0012).* The Daemon sends a
`keepalive` event every 15 s. When the stream delivers nothing at all for 35 s, the Daemon is hung
or the socket is half-open, so the Board shows the slate "Lost contact with herddash.
Reconnecting…" strip at once, dims every Box and reopens the stream with its usual backoff. A
`state` or change event the Board rejects, or a skipped view sequence, also counts as a lost
stream and is reopened, because a reopened stream starts with the whole state. The strip for
those shows only if the reopen has not healed within the usual 3 s grace period.

**A restarted Daemon reloads the page.** *Added 2026-10-21.* When the stream's `state` event
comes from another Daemon run (`run_id`), such as a Daemon restarted onto a new build, the Board
reloads itself, so an open tab never draws new data with old code. A reconnect to the same run
does not reload.

## Acknowledgement controls

*Added 2026-10-27, ADR-0014, from the mock the operator approved with their live Board (Story
6.4).* The Board's only controls. They are ordinary buttons, reachable by Tab and by Vimium's
link hints; **there is no keyboard shortcut**. They act on herddash's own acknowledgement and
never on herdr: no focus, nothing sent to the pane.

- **The Seen pill.** Every Finished Box, and no other kind, ends its first line with a pill,
  `✓ Seen` (`.ack`): `--text-label`, weight 700, in `--finished-accent` with a 1px
  `--finished-edge` border, radius 999px; on hover it inverts to a `--finished-edge` fill and
  `--surface` text. Its title is "Mark seen: you have read this Agent's result" and its
  accessible name "Mark <tab> seen". A click posts the Box's exact finish (its AgentKey and the
  `waiting_since` it shows) to `/api/agents/{host}/{server}/{epoch}/{pane}/seen`.
- **The seen Box.** The Board changes nothing optimistically: when the stream's `agent` event
  arrives with `seen` true and the Daemon's rank 2, the Box turns grey, reads `SEEN` (see
  "Emphasis by class"), loses its pill, moves down among the Idle Boxes of its group, and the
  heading's finished count drops by one and shows `N seen`. It stays seen until the Agent's next
  Transition out of that finish (a new turn, Needs you, a new Agent in the pane); herdr's own
  `done` to `idle` within the finish keeps it. The next finish is a Finished Box with its pill.
- **The heading's bulk mark.** A workspace heading with **2 or more** Finished Boxes shows
  `✓ Mark N finished seen` (`.ack-all`) after its counts, at the right: a pill in
  `--finished-accent` with a 1px `--border` border that turns `--finished-edge` on hover; its
  accessible name is "Mark N finished Agents in <workspace> seen". With fewer it is hidden. A
  click posts exactly the Finished Boxes the group shows, each by its exact finish, to
  `/api/seen`, all or nothing; never "everything in this workspace".
- **The Undo bar.** After a mark succeeds, a bar (`.toast`, `role="status"`) fixed at the bottom
  centre, `--ink` background and `--surface` text, fades in over 150 ms with "Marked <tab> seen"
  or "Marked N Agents seen" and an `Undo` button, and goes after **6 s**. Undo posts the same
  items to the matching `unseen` route and closes the bar. A new mark replaces the bar and its
  timer. A refused mark shows the bar for 6 s with no Undo: "It changed before it was marked;
  nothing was marked" (409), "herddash is not recording; nothing was marked" (503), else "Could
  not mark it seen". A refusal leaves the Box Finished, as it was.
- **Disabled** while its request is in flight, and every control while the Boxes are dimmed or
  hidden: a Box the Board cannot vouch for is not one to mark.

### Seen on leaving the pane, the in-view tag and Mark unseen

*Added 2026-10-27, ADR-0015 (operator decision, vfkb f14fa1cdfa89).* The Daemon marks a finish
Seen when the operator, after it began, had its pane focused in herdr for at least 3 s and then
focused another pane. The Board draws the result exactly as a click's: the stream's `agent` event
arrives with `seen` true, and the Box turns grey. No Undo bar appears for an automatic mark (the
Board did not ask for it); `Mark unseen` takes it back.

- **The in-view tag.** A Finished Box (kind `finished`, so not Background and not Seen) whose
  AgentView has `in_view: true` shows `● in view` (`.in-view`) on its first line, after the
  pane-position badge and before the Seen pill: `--text-label`, weight 600, in
  `--finished-accent`, no border, no fill, the dot a filled circle in the same colour. Its title is
  "Focused in herdr now. Leaving the pane after 3 s marks it seen." It is a label, not a control:
  it is not focusable and does nothing on click. The Box's accessible name gains ", in view" after
  its class label. No other kind shows it, Needs you included. herdr names a focused pane even with
  no terminal attached, so on a detached herdr the tag stays on the pane last focused; it never
  leads to a mark by itself.
- **The Mark unseen pill.** A Seen Box (kind `seen`) ends its first line with `Mark unseen`
  (`.unack`): the Seen pill's shape (`--text-label`, radius 999px, 1px border) but quiet, in
  `--ink-muted` with a 1px `--border` edge, turning `--ink` with an `--ink-faint` edge on hover,
  so it does not pull the eye back to a Box that has receded. Its title is "Mark not seen: show
  this result as unread again" and its accessible name "Mark <tab> not seen". A click posts the
  Box's exact finish (its key and `waiting_since`) to
  `/api/agents/{host}/{server}/{epoch}/{pane}/unseen`. On success the Undo bar shows "Marked <tab>
  not seen" for 6 s, whose `Undo` marks it seen again; a refusal shows the refusal bar, "Could not
  mark it not seen" unless 409 or 503 as for Seen. After it the Box is Finished with its Seen pill,
  and the Daemon will not mark that finish Seen again when the operator leaves the pane: their
  explicit choice wins. It is disabled while in flight and while the Boxes are dimmed or hidden.

The states a Box passes through, for the operator's three ways of working:

| What the operator does | Box |
|---|---|
| Opens the pane, reads, leaves without prompting | Finished, `● in view` while there; Seen on leaving (after 3 s there) |
| Opens it, prompts, leaves | Working; Finished, not in view, when the turn ends while they are elsewhere |
| Stays and watches several turns, then leaves | Finished, `● in view`, after each finish; Seen on leaving |
| Flicks through panes, under 3 s each | unchanged |
| Away while an Agent finishes in the focused pane | Finished, `● in view`, until they come back and leave it |
| herdr detached | never marked automatically |
| Clicks `Mark unseen` | Finished with its Seen pill, and stays so for that finish |

## Responsive behaviour

The layout runs from a side window of about 320px, with one column and a 16px gutter, to a wide
monitor, with as many 17rem-minimum columns as fit. The gutter is 16px at every width. Box height never changes. Only the column
count does. Strips wrap to two lines on a narrow window. There is no horizontal scroll at any
width.

## Accessibility

DOM order is the reading order: the title bar, the strips (a polite live region), then the Needs you zone, then the
workspace groups. The Needs you heading is visible; the lower zone has a visually hidden heading,
and workspace labels are headings.
Each Box is focusable and shows a 3px `--focus` outline offset outside it, and the review
toolbar's controls show the same outline. Boxes are focusable so that keyboard and screen-reader
users can read a Box in full. Each Box's accessible name carries its workspace and tab, pane position, full
Context Line (or the full recap in its place, ADR-0010), directory, heading, class label and Waiting Time ("at least" for a lower bound),
and it is kept current as the time ticks. Focusing a Box does nothing else. The acknowledgement
controls (ADR-0014) are buttons with accessible names; a Box redrawn without its pill keeps
focus, and the Undo bar is a status region. Needs you is conveyed in
text, not only colour.

## Deliberately absent (NFR10, SM-C1)

The Board has no controls but the acknowledgement controls (amended 2026-10-27, ADR-0014: the
Seen pill, the heading's bulk mark and the Undo bar; and ADR-0015: the Mark unseen pill; none of
which tells herdr anything; the in-view tag is a label, not a control): no
focus, send, kill or open-in-terminal action, no filters, no search, no settings and no keyboard
shortcut. It shows no pane output, excerpts or scrollback, and no second data type
per Box: no history or charts. The only counts are each workspace group's heading counts
(amended 2026-10-21). It has no footer. The Board is the title bar (ADR-0013, 2026-10-26), Boxes,
the Needs you and workspace headings, the two strips and the Undo bar. Anything more needs an ADR
superseding ADR-0003 and ADR-0014.
