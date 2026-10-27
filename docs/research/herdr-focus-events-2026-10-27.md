# herdr 0.9.5: how focus reaches an API client

**Recorded 2026-10-27 against herdr 0.9.5** (`/opt/homebrew/Cellar/herdr/0.9.5/bin/herdr`,
protocol 22), on macOS. Frozen research record, written for the operator decision of 2026-10-27
(vfkb `f14fa1cdfa89`): a Finished Agent becomes Seen automatically when, after its finish, its pane
was focused in herdr for about 3 s and focus then moved to another pane. The question is whether
herddash, which only reads, can tell from herdr's socket API that the operator left a pane.

## Provenance

**Observed** against an isolated herdr 0.9.5: a headless `herdr server` under `env -i` with
`HOME=/tmp/hdf/home`, its own socket `/tmp/hdf/home/.config/herdr/herdr.sock`, so neither the
operator's live server nor the inherited `HERDR_SOCKET_PATH` was involved. The terminal clients
were real `herdr` clients, each started under the same `env -i` wrapper on its own pseudo-terminal
(Python's `pty.fork`, 120x40 requested) and driven by writing keystrokes to it: `ctrl+b` then `h`,
`l`, `n`, `p` (the default bindings), and `.` and `,`, bound in the isolated `config.toml` to
`next_workspace` and `previous_workspace`, which have no default key. Agent status was driven by
`pane.report_agent`, never by a real agent. On the isolated server only, the probe also called
`pane.focus`, `tab.focus` and `workspace.focus`, because what they emit is part of the question.
Reproduce with [herdr-focus-probe.sh](herdr-focus-probe.sh); it writes its transcript to
`/tmp/hdf/evidence/transcript.txt`. Event lines below are copied from that transcript.

The layout throughout: workspace `w1` with tab `w1:t1` holding panes `w1:p1` and `w1:p2` side by
side and tab `w1:t2` holding `w1:p3`; workspace `w2` with tab `w2:t1` holding `w2:p1`.

**Inferred**, and marked so where it appears: how herdr decides which client's view the snapshot
reports, and that a finish counts as seen because a client displays the pane's tab. Both fit every
observation below but were not read from herdr's source.

**Not tested**: Linux; a real terminal emulator rather than a pty; mouse navigation (clicking a
pane or the sidebar); herdr's navigate mode, workspace picker, `goto` and notification target; a
remote (`--remote`) client; a pane moved, zoomed or closed while focused; more than two clients.

A trap for anyone repeating this: in a fresh `HOME` the first client opens a welcome modal
("this is a mouse-first terminal ... enter continue"), then the agent integrations list ("enter
install, esc close"). While it is open, prefix keys do nothing, so the first attempt saw no focus
events at all from client navigation. The probe presses enter once and then esc. Enter on the
integrations list would install one.

## Findings

### 1. The subscription: global `pane.focused`, payload `{pane_id, workspace_id}`

`pane.focused`, `tab.focused` and `workspace.focused` are three of the 24 global subscription
kinds: no `pane_id`, one entry each in `events.subscribe` (schema
`#/schemas/request/$defs/Subscription`; `gen.SubscriptionTypePaneFocused` and its siblings). herddash's
lifecycle subscription (`internal/herdr/subscribe.go`, `lifecycleSubscriptions`) does not request
any of them today. They push in the underscored lifecycle envelope:

```text
{"data":{"type":"workspace_focused","workspace_id":"w1"},"event":"workspace_focused"}
{"data":{"tab_id":"w1:t1","type":"tab_focused","workspace_id":"w1"},"event":"tab_focused"}
{"data":{"pane_id":"w1:p2","type":"pane_focused","workspace_id":"w1"},"event":"pane_focused"}
```

The payloads carry exactly what the schema requires (`EventData` variants `workspace_focused`:
`workspace_id`; `tab_focused`: `tab_id`, `workspace_id`; `pane_focused`: `pane_id`,
`workspace_id`) and nothing else: **no previous pane, no client, no timestamp, no sequence
number.** herddash has to time a dwell from when it receives the event.

### 2. Every focus change emits all three, ending with the pane landed on

Every change of focus observed, from a client's keys or from an API call, emitted the same three
events in the same order, `workspace_focused`, `tab_focused`, `pane_focused`, together, even
when the workspace and tab did not change. **`pane_focused` always named the pane landed on**, so a
tab or workspace switch needs no separate handling: the pane focused in the new tab arrives as
`pane_focused`.

| Action | `pane_focused.pane_id` | Snapshot afterwards (`focused_workspace_id`, `_tab_id`, `_pane_id`) |
|---|---|---|
| client `ctrl+b l`, same tab | `w1:p2` | `w1`, `w1:t1`, `w1:p2` |
| client `ctrl+b h`, same tab | `w1:p1` | `w1`, `w1:t1`, `w1:p1` |
| client `ctrl+b n`, next tab | `w1:p3` | `w1`, `w1:t2`, `w1:p3` |
| client `ctrl+b p`, previous tab | `w1:p1` | `w1`, `w1:t1`, `w1:p1` |
| client `ctrl+b .`, next workspace | `w2:p1` | `w2`, `w2:t1`, `w2:p1` |
| client `ctrl+b ,`, previous workspace | `w1:p1` | `w1`, `w1:t1`, `w1:p1` |
| API `pane.focus w1:p2` | `w1:p2` | `w1`, `w1:t1`, `w1:p2` |
| API `tab.focus w1:t2` | `w1:p3` | `w1`, `w1:t2`, `w1:p3` |
| API `workspace.focus w2` | `w2:p1` | `w2`, `w2:t1`, `w2:p1` |
| API `pane.focus w1:p3` (another tab) | `w1:p3` | `w1`, `w1:t2`, `w1:p3` |
| API `pane.focus w2:p1` (another workspace) | `w2:p1` | `w2`, `w2:t1`, `w2:p1` |
| API `pane.focus` on the pane already focused | none | unchanged |

The client rows are from one attached client. The API rows are from no client attached; the
first three API calls were repeated with one client attached, with identical events. A workspace
switch lands on a remembered tab and, in it, that tab's remembered pane: client C, back in `w1`
from `w2`, landed on `w1:t2`, the tab it had left `w1` from; client D landed on `w1:t1`, the tab
it had started on. The snapshot's per-tab `layouts[].focused_pane_id` holds each tab's remembered
pane.

**`pane_focused` can repeat for the pane already focused.** An API `pane.focus` on the focused
pane emitted nothing, but a second client's navigation that landed on the pane the snapshot
already named emitted all three events again (finding 4). So the stream is "a client landed on
this pane", not "focus changed".

Nothing of the API's events distinguishes a client's keys from another API client's
`pane.focus`, `tab.focus` or `workspace.focus`. The server log tags API calls `changes_ui=true`;
the event stream does not.

### 3. The snapshot always names a focused pane, attached or not

`session.snapshot`'s `focused_workspace_id`, `focused_tab_id` and `focused_pane_id` were set at
every point, and agreed with the `focused` flag on the pane, tab, workspace and agent records:

- **Before any focus change**: `workspace.create` on an empty server focused the new workspace
  (`"focused":true` in its result although `focus` defaults to false), giving `w1`, `w1:t1`,
  `w1:p1`. Later `pane.split`, `tab.create` and `workspace.create` with `focus:false` did not move
  it.
- **After each change**: as in the table above.
- **With no terminal client attached**: still set. A client attaching emitted no event and opened
  on the server's current focus (client C, attached after `pane.focus w1:p2`, showed `w1:p2` and
  the snapshot kept `w1`, `w1:t1`, `w1:p2`). Clients detaching emitted no event and the snapshot
  kept the last focus (`w1`, `w1:t2`, `w1:p3` after both clients A and B ended).

**Nothing herddash may call says whether any client is attached.** The server log records
`client connected client_id=2` and `client detached client_id=4`, but no API read exposes it. The
only places the schema mentions a client are mutating calls whose results can report
`no_foreground_client` (`notification.show`, `client.window_title.set`); herddash must not call
them. *Inferred from the schema; not probed.* So a `focused_pane_id` in the snapshot does not
mean someone is looking at that pane.

### 4. Two clients: per-client views, one reported focus, no client identity

Each client keeps its own view (workspace, tab, focused pane). The API reports one of them, and
nothing says which client moved:

```text
-- B focuses pane right                 (A and B both on w1:t1 at w1:p1)
   event: ... "pane_focused" ... "pane_id":"w1:p2"     snapshot w1:t1 / w1:p2
-- A next tab
   event: ... "pane_focused" ... "pane_id":"w1:p3"     snapshot w1:t2 / w1:p3
-- B next workspace
   event: ... "pane_focused" ... "pane_id":"w2:p1"     snapshot w2:t1 / w2:p1
-- A focus pane left                    (A on w1:t2, whose only pane has no left neighbour)
   events: (none)                                      snapshot w1:t2 / w1:p3
```

The last step is the important one: **the snapshot's focus jumped from B's view (`w2:p1`) back to
A's (`w1:p3`) with no event at all**, because A pressed a navigation key that moved nothing. The
second run repeated it with clients C and D: D moved to `w2`, then C's no-op `ctrl+b l` put the
snapshot back on C's `w1:p2`, again with no event. The server log did record
`workspace focused workspace_id="w1"` and `tab focused tab_id="w1:t1"` at that moment. Plain typing
into a pane by either client (`x`, then backspace) changed nothing and emitted nothing. And when D
then went back to `w1`, landing on `w1:p2`, which the snapshot already named, all three events
fired again.

*Inferred:* the snapshot reports the view of the client that most recently navigated (herdr's
"foreground client", the term its schema uses in `no_foreground_client`), a client becomes that one
by navigating even when its view does not change, and only a change of that client's own view emits
events.

Consequences for an API client:

- `pane_focused` from two clients interleave in one stream with nothing to tell them apart. With
  two clients attached, client B's navigation reads exactly like the operator at client A leaving
  A's pane.
- The snapshot can change focus without an event, so after any resync the snapshot's
  `focused_pane_id` may disagree with the last `pane_focused`.
- There is no per-client distinction visible to an API client at all (question 4).

### 5. Seen state: `done` means no client is displaying the pane's tab

Not asked, but it bears on the decision not to use herdr's done/idle for Seen, and it answers the
socket API record's open item about `done` with a client attached:

| When the agent went `working` then idle | Reported |
|---|---|
| No client attached, any pane | `idle` |
| One client; pane focused | `idle` |
| One client; pane in the client's current tab but not focused (`w1:p1` beside focused `w1:p2`) | `idle` |
| One client; pane in another tab (`w1:p3`) or workspace (`w2:p1`) | `done` |
| Two clients; pane in a tab one client shows | `idle` |
| Two clients; pane in a tab neither shows | `done` |

A `done` pane turned `idle`, with a `pane.agent_status_changed` event, the moment a client showed
its tab, focused or not: `w1:p3` when client C switched to `w1:t2`, `w2:p1` when C switched to
`w2`, `w1:p2` when C came back to `w1:t1`. Going back to `w1` on its other tab (`w1:t2`) left
`w1:p2` `done`. An API `pane.focus` with no client attached also turned a `done` pane `idle`, as the
socket API record says focus calls do. A pane left `done` when the clients detached stayed `done`.

*Inferred:* a finish is seen when some attached client is displaying the pane's tab at that
moment, or later displays it. This is coarser than focus (a pane beside the focused one counts as
seen), shared across clients in effect, and never reported while detached, which is why the
operator's decision does not use it.

## What this means for "Seen on leaving a pane"

- **The signal exists and is cheap**: add the global `pane.focused` subscription to the lifecycle
  connection. Each `pane_focused` names the pane landed on, including after a tab or workspace
  switch. "Left pane X" is a `pane_focused` naming a pane other than X while X was the last pane
  landed on. A repeated `pane_focused` for X is not a leave. `tab_focused` and `workspace_focused`
  add nothing.
- **The dwell must be timed by herddash**, from when it received the `pane_focused` for X; the
  events carry no time.
- **The starting point after a Daemon start or resync is the snapshot's `focused_pane_id`**, with
  no known start time.
- **A detached herdr never auto-marks**, as decided: with no client, focus moves only through
  API focus calls, so no `pane_focused` arrives unless some other API client calls them. That
  exception is real: another tool's `pane.focus` reads exactly like the operator moving.
- **"In view" cannot mean that someone is looking.** `focused_pane_id` stays set with no client
  attached, and nothing herddash may read says whether one is. An "in view" tag drawn from it shows
  on the last-focused pane of a detached herdr.
- **Several attached clients break the rule's premise.** Their views are separate and their
  events are anonymous, so one client's navigation can mark Seen a pane the operator is still
  reading at another, and the snapshot can switch to another client's view with no event. With one
  attached client the rule is exact.
