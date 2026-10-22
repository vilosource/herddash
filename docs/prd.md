---
title: herddash — Product Requirements
status: final
created: 2026-10-08
updated: 2026-10-14
supersedes: the 2026-10-05 draft of docs/prd.md
governed_by: [docs/adr/0001-what-herddash-is.md, docs/adr/0003-the-board-is-the-product.md]
---

# PRD: herddash

## 0. Document Purpose

This is the product definition for herddash, written for the operator who builds and uses it, and
for the architecture, epic and story work that BMAD derives from it. It replaces the 2026-10-05
draft, whose sections 7 to 12 described a product ADR-0001 rejected. Vocabulary is fixed in §3 and
used exactly everywhere else. Features are grouped in §4 with globally numbered requirements.
Inferences made while drafting were confirmed or revised by the operator; §9 records them.

Protocol detail, stack choices, rejected alternatives and the reasoning behind hard constraints
live in [the addendum](planning/prds/prd-herddash-2026-10-08/addendum.md) and in
`docs/research/`. This document says what herddash does. Those say how and why.

## 1. Vision

An operator running many AI coding agents in herdr has no single place to see them. herdr knows
every agent's state and tells the operator through desktop toasts, which are suppressed for the
active tab, silent when no terminal client is attached, capped at eight, and gone in eight
seconds. The operator ends up herding agents by cycling through panes to find the one that
stopped.

herddash is a **notification center for herdr agents**: one Board, open in a browser window beside
the terminal all day, with one Box per Agent. A glance says which Agents are working, which have
finished and are waiting for the next prompt, and which are blocked on a question, and for how
long. The longest-waiting Agent is at the top. Nothing else is on the page. There is no terminal,
no chat, no pane control, because herdr already does those and the operator is happy with it.

Underneath, herddash is a recorder. It holds its own connection to herdr, records every Transition
with the Agent's Profile, and keeps that history under bounded Retention. The Board is believable
because of that record: "waiting 40 minutes" is a fact, not an estimate, and "which Profile, and
so which subscription, is this" is resolved rather than guessed. The same record is exposed
read-only, so any future surface shows the same truth the Board does.

## 2. Target User

### 2.1 Jobs To Be Done

- **Functional:** know, without looking at each pane, which Agents are waiting on me right now and
  which has waited longest, so I prompt the right one next.
- **Functional:** know which of several Claude Code subscriptions, and which other Agent Programs,
  each running Agent belongs to, because the same binary runs under several Profiles.
- **Emotional:** stop the low-grade anxiety of "something finished and I missed it" that the toast
  channel creates by being transient and lossy.
- **Contextual:** keep working in herdr's own interface. herddash is glanced at, never operated.
- **Builder:** dogfood it on three machines, then publish it for other herdr operators.

### 2.2 Non-Users (v1)

- Anyone who wants to drive agents from a browser. devswha/herdr-web-ui does that, and it also
  has web push and a Needs-you grouping. herddash differs by being read-only, by keeping a
  history, and by attributing each Agent to its Profile; it runs beside devswha, not instead.
- Teams. v1 is one operator, one host, one browser, on loopback.
- Anyone on herdr older than 0.9.5. herddash targets 0.9.5 or newer, because earlier releases
  under-report `blocked` for Codex, Antigravity, and Claude Code in named sessions. It is tested
  against 0.8.2 only because that is what the operator's machine runs today.

### 2.3 Key User Journeys

- **UJ-1. Jason finds the agent that has been waiting longest.** Ten agents across three
  workspaces. Jason has been heads-down in one pane for twenty minutes. He glances at the Board
  in the other window. The top Box is red: `cldw · claude · ~/GitHub/herddash · Needs you · 14m`.
  Below it two amber Boxes finished a few minutes ago. The rest are calm. He switches to that pane
  in herdr, answers the question, and the Box turns to Working within a second without him
  touching the Board.
  **Edge case:** herdr restarts. The Board shows a single thin "disconnected from herdr, last
  update 12:04:31" strip; the Boxes stay but dim. When herdr is back the strip disappears and
  Boxes refresh from a fresh snapshot.

- **UJ-2. Jason tells two identical `claude` panes apart.** Two Boxes both say `claude`. One says
  `cldp`, the other `cldw`. He knows which subscription's quota he is spending and which one is
  the permission-prompting one, without opening either pane.

- **UJ-3. Jason wants to know how long an agent has really been sitting there.** A Box says
  `Finished · 2h 10m`. That number came from a recorded transition at 09:54, not from when the page
  was loaded or when herdr was restarted. He trusts it. Had herddash not been running at 09:54,
  the Box would say `Finished · ≥ 1h 30m` instead, never a smaller number claimed as exact.

- **UJ-4. A second surface, later.** Jason's phone, or a status bar widget, or a terminal one-liner
  asks the Daemon for the current state and gets the same sorted list the Board renders, with the
  same Profiles and the same waiting times.
  *(Deferred beyond v1; it exists to shape the API, not to be built now. A phone off the Host
  needs network exposure, which waits on its own authentication ADR; see §5.)*

## 3. Glossary

- **herdr** — the terminal workspace manager herddash observes. Each **herdr server** is reachable
  over its own Unix socket; a Host usually runs one, but named sessions run one server each.
- **Host** — the machine a herdr server runs on. Every id herddash stores is namespaced by Host and
  herdr server, because herdr ids are scoped to one server.
- **Agent** — a herdr pane that herdr has detected as running an AI coding agent program. herdr
  reports its **Agent Program** (for example `claude`, `codex`) and its **Status**. The program's
  own software is called its **harness** where its behaviour, not its name, is meant.
- **Status** — herdr's own classification of an Agent: `working`, `blocked`, `done`, `idle` or
  `unknown`. herddash records Status verbatim and never redefines it.
- **Transition** — one change of an Agent's Status, with the time herddash observed it, the
  herdr sequence information that orders it, and the Agent's Profile at that moment.
- **Profile** — the launcher identity an Agent runs under, as a short human name such as `cldp`
  or `cldw`. For Claude Code it is resolved from the pane process's `CLAUDE_CONFIG_DIR` through
  the **Profile Map**, a configured mapping from process environment values to Profile names. For
  other Agent Programs it defaults to the Agent Program name. Profiles usually correspond to
  subscriptions, which is why the operator cares; herdr itself has no notion of either.
- **Board** — the single page herddash serves: a flat top zone of Needs you and Finished Boxes,
  longest-waiting first, above the Working and Unknown Boxes grouped by workspace, live.
- **Box** — the Board's representation of one Agent. Its heading is the Profile and Agent Program;
  its body is the project directory, the workspace and tab it lives in, Attention Class, Waiting
  Time, and one Context Line.
- **Pane Title** — the one-line label an Agent sets on its own pane, reported by herdr as the
  pane's terminal title. Set by the Agent Program, not by herdr and not by the operator; Claude
  Code sets it to its short session name, and to a placeholder before one exists.
- **Context Line** — the single line of information inside a Box beneath the Attention Class.
  The Daemon derives it per Agent Program: from the Pane Title when that program provides a
  meaningful one, otherwise from one global default. Every surface receives the Context Line
  from the API; none computes its own.
- **Attention Class** — the Board's grouping of Status for display: **Needs you** (`blocked`),
  **Finished** (`done`, and `idle` reached from `working` or `done`), **Working** (`working`, and
  `idle` with no `working` in the record, a pane never prompted), and **Unknown** (`unknown`, or no
  Status yet). A display concept; Status is the recorded fact.
- **Waiting Time** — time since the Agent entered its current Attention Class. `done` turning into
  `idle` does not reset it, since both mean awaiting a prompt. When the Daemon did not observe that
  moment live (first start, or a change while the Daemon was down or resynchronising), Waiting
  Time is a **lower bound** taken from the earliest evidence (the last recorded Transition, else
  Daemon start) and is shown with `≥`. A restart across which the Status did not change keeps
  the exact time (FR-8).
- **Daemon** — the herddash process: herdr client, recorder, and HTTP server in one binary. It
  runs on the Host it observes, as the same user as the Agents, because it gathers facts that
  exist only there: the herdr socket and the Agents' process environments. It gathers what the
  Board needs to answer its question, and nothing merely because it is available.
- **Retention** — the bounded policy under which recorded Transitions are kept and then deleted.
- **API** — the Daemon's read-only HTTP surface: current state, history, and an event stream.

## 4. Features

### 4.1 The Board

**Description:** The Board is one HTML page served by the Daemon on loopback. It shows every
Agent on the Host as a Box, arranged so the Agent that has needed the operator longest is first.
Boxes in Needs you are visually dominant; Finished is clearly marked; Working and Unknown are
calm. The Board repaints a Box within a second of the Daemon recording a Transition, with no page
refresh. It is readable from across a desk and carries nothing that is not in a Box or the page's
strips. Realizes UJ-1, UJ-2, UJ-3.

Boxes are a responsive grid of cards in two zones. The top zone is flat: every Needs you and
Finished Box, Needs you first, each class longest-waiting first, whatever workspace it is in. Below
it, Working and Unknown Boxes are grouped by workspace under a thin workspace label, since herdr
already provides that hierarchy. Grouping never reorders the top zone.

The Context Line's derivation is FR-2a. Measured on the operator's machine on 2026-10-13, Claude
Code Pane Titles run 7 to 45 characters and read as session names, not prompt text. The Pane
Title comes from the snapshot and status events, so no pane output is read to obtain it.

**Functional Requirements:**

#### FR-1: One Box per Agent

The Board shows exactly one Box for every Agent herdr currently reports on the Host, and no Box
for panes that are not Agents. Realizes UJ-1.

**Consequences (testable):**
- An Agent appearing in herdr produces a Box without a page reload.
- A pane closing removes its Box without a page reload.
- A pane that herdr later detects as an Agent gains a Box at that moment.

#### FR-2: Box contents

Each Box shows the Profile and Agent Program as its heading, and the project directory
(shortened), the workspace and tab labels, the Attention Class, the Waiting Time and the Context
Line as its body. The workspace and tab labels are how the operator finds the pane in herdr.
Realizes UJ-2, UJ-3.

**Consequences (testable):**
- Two Agents with the same Agent Program under different Profiles show different Profile labels.
- An Agent whose Profile cannot be resolved shows `unresolved`, never a blank or a guess.
- When the Daemon observed the start of the wait, Waiting Time is derived from that recorded
  Transition, not from page load or Daemon start.
- A Waiting Time whose start the Daemon did not observe is shown as a lower bound, `≥ 12m`, never
  as an exact figure.
- The Context Line occupies exactly one line: truncated with an ellipsis at the Box width, full
  text on hover, never wrapped, so no title can change a Box's height or the grid.
- The Context Line's space is reserved even when empty, so Boxes stay uniform.

*Amended 2026-10-21, operator decision, approved from a mock of the live Board:* the Box leads
with the tab, large and bold, plus a pane-position badge (`left`, `top-right` and the like) when
the tab holds more than one pane, so two Agents in one tab can be told apart; the badge is part of
the Box's location with the workspace and tab, not a new element. The second line is the
workspace in the Needs you zone and the directory in a workspace group, whose heading already
names the workspace. The heading of a Claude Code Agent is its Profile alone (`cldw`,
`unresolved`). An Agent of class Working whose Status is not `working` reads `IDLE`. See
[the Board design](design/board-design.md).

*Amended 2026-10-21, operator request:* "as a user I need to be able to differentiate between an
agent waiting for a subagent or background task vs really Finished." herdr reports a Claude Code
Agent that ended its turn with a background shell still running as `done`, then `idle`, so the
Board showed it Finished. A Claude Code Agent whose Status is `done` or `idle` and whose process
still has a tool-shell child (a direct child whose arguments name its
`/shell-snapshots/snapshot-` file) reads `BACKGROUND`, ranked right before Working in its
workspace group (right after Working until the FR-3 amendment of 2026-10-21). The Daemon finds this by reading the Agent process's direct children locally,
never from herdr and never from pane output. The Attention Class, Waiting Time and the record are
unchanged: Waiting Time still counts from when the turn ended. A running subagent already reads
Working; a Working, Needs you or Unknown Agent is never Background. Other Agent Programs have no
rule yet and never read Background.

**Consequences (testable):**
- A `done` or `idle` Claude Code Agent with a tool-shell child reads `BACKGROUND`; one whose
  children are only MCP servers, `gopls` or `caffeinate` reads Finished (or Idle).
- When the shell ends, the Box reads Finished again within a few seconds, with its Waiting Time
  unchanged.
- A `working` Agent with a shell child reads Working.

*Amended 2026-10-21, [ADR-0009](adr/0009-background-work-from-claude-code-transcripts.md)
(accepted by the operator):* "I want to know when you are Working and you have background tasks
or you are finished and have background tasks", including subagents. For a Claude Code Agent
whose session record (FR-6's ADR-0008 amendment) names its `sessionId` and `cwd`, the Daemon also
tails that session's transcript, `<config dir>/projects/<project folder>/<sessionId>.jsonl`, the
folder being the cwd with every character but a letter or digit as "-" (or with only "/" and "."
as "-" when only that folder exists), on the same 2 s watch, and decodes only structural markers:
a background command's or subagent's start (its task id); its end, by a task notification (its
task id and status) or by a TaskStop result (its task id, read as killed; added 2026-10-21, since
a stopped task gets no notification); and whether the main thread's turn has ended (a
`stop_reason` of `end_turn` or `stop_sequence`) or started again (a prompt, a tool result or a
queued command). Text, inputs, outputs, prompts and results are never kept,
logged, stored, sent or shown. The AgentView gains `background_tasks`, the open tasks plus one
for a tool shell the process check finds that no open command accounts for. A Working Agent with
tasks open reads `WORKING · N bg`; one whose main turn has ended while tasks are open (herdr
reports `working` while a background subagent runs) reads `BACKGROUND`, not Working; a `done` or
`idle` one with tasks open reads `BACKGROUND` (`BACKGROUND · N` when N > 1). Needs you and Unknown
never change, nor do the record, the Attention Class, the Fold or Waiting Time. A missing or
unreadable record or transcript falls back to the process check alone. This supersedes "A running
subagent already reads Working" above for a subagent running after the main turn. Workflows are
not claimed until one has been observed.

**Consequences (testable):**
- A `working` Claude Code Agent whose transcript shows its main turn ended while a background
  subagent runs reads `BACKGROUND`, not Working; when the subagent's notification arrives it reads
  Working (or whatever herdr then says).
- A `working` Agent with one background command open and its turn not ended reads `WORKING · 1 bg`.
- A task notification without a status leaves its task open.
- No transcript text appears in any log, API response or the record, except Claude Code's session
  recap in the API (`recap`), and only when `claude_recaps` is on (ADR-0010 below); never in a log
  or the record.

*Amended 2026-10-22, [ADR-0010](adr/0010-claude-code-recap-on-the-board.md) (accepted by the
operator: "privacy is not an issue, this is used only for me at this time, and future users will
have to consent, two lines, recaps should be up to date as often as possible"):* "the idea is to
have a recap in the boxes". Claude Code writes a two-sentence session recap into the same
transcript about three minutes after a turn ends. Only when the operator sets the config key
`claude_recaps = true` (default `false`, file only: the consent), the Daemon also decodes one more
record from the transcript it already tails: a non-sidechain `system` record with `subtype`
`away_summary` and a string `content`. It keeps per Agent, in memory only, the newest recap's text,
trimmed, without Claude Code's trailing "(disable recaps in /config)", and cut to at most 400
characters; nothing else of the record. A newer recap replaces it; the main thread's next turn
starting (a prompt, a tool result or a queued command) clears it, so a Working Agent never shows a
stale one. The AgentView gains `recap`, set only on a Finished, Idle or Background Box and ""
on a Working, Needs you or Unknown one, or when there is none (operator decision 2026-10-22, at
review: herdr reports a new turn before the next transcript read). A Box with a recap shows it in the Context
Line's row, clamped to two lines with an ellipsis, the full text in its tooltip and accessible
name; while any Box shows one, every Box is one line taller so all keep one height. This amends
ADR-0009's rule that no transcript text is ever sent or shown, for the recap alone. The record,
the Attention Class, the Fold, Waiting Time and the order are unchanged.

**Consequences (testable):**
- With `claude_recaps` unset or `false`, no recap record is decoded, every AgentView's `recap` is
  `""` and the Board is exactly as before.
- With it `true`, a Finished Claude Code Agent whose transcript ends with a recap shows that recap,
  without the "(disable recaps in /config)" suffix, in place of its Context Line.
- A prompt, tool result or queued command after the recap clears it; a newer recap replaces it.
- A recap longer than 400 characters is cut at a character boundary.
- A sidechain recap, another `system` subtype or a non-string `content` gives no recap.
- Only a Finished, Idle or Background Box shows a recap; a Working (not Background), Needs you or
  Unknown Box has `recap` `""` even while the transcript holds one.
- The recap text never appears in a log or in the record; the log says only, by pane, that a recap
  appeared or cleared.
- While any Box has a recap, every Box is one line taller; with none, Box heights are unchanged.

#### FR-2a: Context Line derivation

The Daemon derives each Agent's Context Line through a per-Agent-Program rule with a global
default, so a harness that provides no usable Pane Title still yields a defined result.

**Consequences (testable):**
- A Claude Code Agent whose Pane Title is a session name shows that name.
- A Claude Code Agent whose Pane Title is the harness placeholder (`Claude Code`) or empty shows
  the global default, not the placeholder.
- An Agent Program with no specific rule shows the global default.
- The global default is an empty Context Line, rather than, say, the project directory again or
  the foreground command; nothing on the Board is duplicated or guessed.
- The rule set is extensible per Agent Program without changing the Board or the API, and is
  documented so a new harness can be added when its title behaviour is known.

#### FR-3: Sort and emphasis

The top zone holds Needs you then Finished, each by Waiting Time descending, flat across
workspaces. Below it, Working then Unknown, grouped by workspace. Needs you is visually dominant.
Realizes UJ-1.

**Consequences (testable):**
- A Transition into `blocked` moves that Box into Needs you at its longest-waiting position
  immediately, so a newly blocked Agent sorts last there (amended 2026-10-20, operator decision;
  see epics.md Story 3.8).
- A Transition into `working` moves that Box out of the top zone into its workspace group
  immediately.
- A `done` Agent becoming `idle` keeps its place and its Waiting Time.
- An `idle` Agent never seen `working` sits with the Working Boxes, with no Waiting Time emphasis.
- Order within a class changes as Waiting Times grow, without reload.

*Amended 2026-10-21, operator decision, approved from a mock of the live Board:* the top zone
holds Needs you alone, longest wait first, flat across workspaces. Every other Agent sits in its
workspace's group, in herdr's workspace order, ranked Working (Status `working`), Finished, Idle
(class Working with any other Status), Unknown, each by Waiting Time descending. The operator's
first question is which workspace, tab and pane to go to, and a Finished Agent belongs beside
its workspace. Each group's heading carries its counts of working, finished and idle Agents, and
the browser tab's title carries the Needs you count, `(2) herddash · Needs you`. The consequences
above hold with "top zone" read as the Needs you zone: a `done` Agent becoming `idle` keeps its
place, and an `idle` Agent never seen `working` is an Idle Box in its group.

*Amended again 2026-10-21, operator decision:* "I want the box to be sorted according to their
status, Most recently Finished first, and then the Idles and then the Working - this allows me to
prioritize which ones to deal with." Inside each workspace group the order is now Finished, Idle,
Background (an Agent waiting on background work, just before Working), Working, Unknown, and
every class is ordered **newest first**: the most recent class start leads. Needs you is
unchanged: alone on top, longest wait first. A group's counts read in the same order,
`1 finished · 3 idle · 1 background · 2 working`. This supersedes the ranking and the "by
Waiting Time descending" of the amendment above for the workspace groups only; the consequences
hold as amended.

#### FR-4: Live without reload

The Board reflects a recorded Transition within one second under normal conditions, through the
API's event stream, and recovers on its own when the stream drops.

**Consequences (testable):**
- Stopping and restarting the Daemon while the Board is open results in the Board reconnecting and
  refreshing without user action.
- No polling loop in the page; updates are pushed.

#### FR-5: Connection truth

The Board shows whether the Daemon is currently connected to herdr and when it last received an
authoritative snapshot. Boxes from a stale state are visibly dimmed, never hidden. Realizes UJ-1
edge case.

**Consequences (testable):**
- Stopping herdr produces the disconnected strip within the Daemon's reconnect interval.
- Restarting herdr clears the strip and refreshes every Box from a new snapshot.

**Out of Scope for the Board:**
- Any action on an Agent: no focus, no send, no kill, no open-in-terminal.
- Pane output, excerpts, or scrollback.
- Per-Agent detail pages. A history timeline per Agent is a natural second page and is deferred;
  see §6.2.

### 4.2 Recording

**Description:** The Daemon holds its own subscriptions to the herdr server and records every
Transition for every Agent on the Host, whether or not any terminal client is attached, whether
or not the Board is open. The record is the source of Waiting Time and of everything the API
serves. It survives Daemon restarts and herdr restarts and is kept under Retention. Realizes UJ-3.

**Functional Requirements:**

#### FR-6: Record every Transition

The Daemon records each Transition with: Host, herdr server, pane id, Agent Program, Profile at
the time, previous Status, new Status, herdr's ordering information, herdr's version, and the
observation time.

**Consequences (testable):**
- A Status change observed by herdr while no browser and no terminal client is attached is in the
  record.
- Two Transitions on one Agent are ordered consistently with herdr's own sequence.
- The record distinguishes a Transition observed live from one inferred by comparing snapshots,
  and both from one backfilled (FR-9a).

*Amended 2026-10-21, [ADR-0008](adr/0008-claude-code-session-record-as-status-authority.md)
(accepted by the operator):* on 2026-10-21 a Claude Code Agent waited on Claude Code's question
dialog while herdr reported it `done`, and the Board showed it Finished. Claude Code's own record
for that process, `<config dir>/sessions/<pid>.json`, said `waiting` from the same second. For a
Claude Code Agent whose process is known, the Daemon reads only that file's `pid`, `status` and
`statusUpdatedAt` every 2 s. While it says `waiting`, the Agent's Status is `blocked` (Needs you,
FR-2, FR-3), whatever herdr reports; otherwise herdr's Status stands. A Status change the record
causes, when `waiting` begins and when it ends, is a live Transition recorded with who observed
it, `claude_session`; every other Transition is observed by herdr.

**Consequences (testable):**
- A Claude Code Agent whose session record says `waiting` while herdr says `done` is Needs you
  within about 2 s, its Waiting Time from the record's `statusUpdatedAt` when that is neither in
  the future nor before the Agent's previous Transition.
- The record shows that Transition, and the return to herdr's Status when `waiting` ends, as
  observed by `claude_session`; the history API says so in `observed_by`.
- A missing, unreadable or mismatched session record changes nothing: herdr's Status alone.
- Other Agent Programs, and the record's other values (`busy`, `idle`, `shell`), change nothing.

#### FR-7: Never lose a Transition silently

When the Daemon cannot guarantee it saw every event, it resynchronises from an authoritative
snapshot and records the resynchronisation. A gap is a recorded fact, not a silent omission.

**Consequences (testable):**
- After herdr signals that the Daemon fell behind, the record contains a resync marker and a
  Status for every Agent consistent with the subsequent snapshot.
- Starting the Daemon while herdr is not running results in the Daemon connecting when herdr
  returns and a snapshot-derived Status for every Agent, each marked as snapshot-derived.

#### FR-8: Survive restarts, indefinitely

The Daemon reconnects to herdr forever, with backoff, and treats herdr's absence as normal.
Recorded history is intact across Daemon and herdr restarts.

**Consequences (testable):**
- Daemon restarted: Waiting Times on the Board after restart equal those before, for Agents whose
  Status did not change.
- herdr restarted: no Transition is recorded for an Agent whose Status is the same before and
  after, other than the resync marker.

#### FR-9: Bounded Retention

Transitions older than the configured Retention are deleted. Retention is finite by default.

**Consequences (testable):**
- With Retention set to N days, no Transition older than N days remains after the next sweep,
  except those an Agent still present needs. Those are the Transitions from the one that began its
  current Attention Class onward, and its latest `working` Transition (spine AD-4).
- Default Retention is 90 days.

#### FR-9a: Backfill from the existing plugin's log

A one-shot import loads the existing herdr plugin's log as initial history, because it is the only
record of Agent activity before the Daemon existed (ADR-0001).

**Consequences (testable):**
- Every imported Transition is marked backfilled and is never presented as observed live.
- Imported `done` entries from before the Host ran herdr 0.9.2 are marked as possibly spurious,
  since earlier versions emit false `done` on startup and restore.
- The plugin log carries no previous Status and no herdr sequence; imported Transitions record both
  as unknown rather than inventing them.
- Entries older than Retention are not imported. Running the import twice adds nothing.

### 4.3 Attribution

**Description:** Several launchers exec the same `claude` binary, so herdr reports every such
pane as Agent Program `claude`. herddash resolves which Profile each Agent runs under through a
**Profile rule** registered for that Agent Program. Each rule uses whatever signal its harness
actually exposes; the only rule known today is Claude Code's, which reads `CLAUDE_CONFIG_DIR`
from the pane's foreground process environment and maps it through the Profile Map. The existing
herdr plugin on the operator's machine is the reference implementation of that rule. Resolution
happens on the Host: the Daemon obtains the foreground process from herdr and reads its
environment from the operating system, which only works locally and as the same user. Realizes
UJ-2.

**Functional Requirements:**

#### FR-10: Resolve Profile per Agent

For each Agent, the Daemon resolves a Profile through the Profile rule registered for its Agent
Program, re-resolving when the pane's foreground process changes. An Agent Program with no rule
resolves to its Agent Program name.

**Consequences (testable):**
- A pane launched with `CLAUDE_CONFIG_DIR=~/.claude-cldw` under a map entry naming it `cldw` is
  recorded and shown as Profile `cldw`.
- A Claude Code pane with `CLAUDE_CONFIG_DIR` unset resolves to the Profile Map's absent-case
  entry for Claude Code, since an unset variable means the stock `~/.claude` profile and is the
  common case, not an error. The shipped configuration includes that entry, so a fresh install
  never shows a stock pane as `unresolved`.
- A pane whose variable is set but matches no entry is recorded as `unresolved` and shown so.
- An Agent Program with no entries in the Profile Map resolves to its Agent Program name, whether
  its distinguishing variable is unknown or merely absent. This holds for Codex and Antigravity
  today (confirmed by the operator, 2026-10-13).
- When Profile equals Agent Program, the Box heading shows the name once, never `codex · codex`.

#### FR-11: Profile rules are selected and parameterised in configuration

The Profile Map lives in the Daemon's configuration file, is documented, and can be changed
without rebuilding. It selects a rule per Agent Program and supplies that rule's parameters. A
harness whose signal fits an existing rule type, such as the environment-variable rule Claude Code
uses, is added by configuration alone. A harness with a new kind of signal needs a new rule,
which touches neither the Board nor the API. Claude Code's rule is the environment-variable rule
on `CLAUDE_CONFIG_DIR` with a named absent case; Codex and Antigravity rules are unknown until one
pane of each has been observed, and until then they resolve to their Agent Program name.

**Consequences (testable):**
- Editing the map and restarting the Daemon changes the Profile shown for affected Agents.
- Transitions recorded before the edit keep the Profile they were recorded with.
- Adding a rule for a new Agent Program changes no Board or API code.

### 4.4 Read-Only API and Event Stream

**Description:** Everything the Board shows comes from the API. The API is read-only, served on
the same loopback listener, and designed for the Board first and for future surfaces second.
Realizes UJ-4.

**Functional Requirements:**

#### FR-12: Current state

A single request returns every Agent on the Host with Profile, Agent Program, project directory,
workspace and tab labels, Status, Attention Class, Waiting Time and whether it is a lower bound,
Pane Title, Context Line, Host and herdr server, already ordered as the Board orders it.

**Consequences (testable):**
- The Board renders from this response, or from the stream's first event, which has the same
  body, alone on first load.
- The response includes connection truth: connected or not, and last snapshot time.

#### FR-13: Event stream

A long-lived server-sent events endpoint pushes each recorded Transition, each resync, and each
connection change as it happens.

**Consequences (testable):**
- A client holding the stream receives a Transition within one second of its recording.
- The stream carries enough to update one Box without a second request.

#### FR-14: History per Agent

A request for one Agent, identified by its pane, returns its recorded Transitions within
Retention, in order.

**Consequences (testable):**
- The response for an Agent contains the Transition that defines its current Waiting Time.
- No filtering or aggregation beyond this in v1; UJ-4 surfaces do not need it yet.

**Feature-specific NFRs:**
- No endpoint mutates anything. There are no POST, PUT or DELETE routes.
- The API never proxies a herdr call on demand; it serves the Daemon's own record and cache.

### 4.5 Running It

**Description:** One static binary per platform, one Daemon per Host, started by the operator or by
a user-level service manager, configured by one file. Realizes the builder job in §2.1.

**Functional Requirements:**

#### FR-15: One binary, one config file

The Daemon ships as a single binary with embedded web assets and reads one configuration file:
herdr socket location (defaulting to herdr's own discovery), bind address (default loopback),
database path, Retention, and the Profile Map.

**Consequences (testable):**
- A fresh Host with herdr and the binary, and a config file with only the Profile Map, runs
  and serves the Board.
- No runtime, package manager or build step is required on the Host.
- Started by launchd or systemd, where `HERDR_SOCKET_PATH` is not set, the Daemon still finds the
  socket by applying herdr's own resolution order, and the config can pin the path.

#### FR-16: Version gate

On connecting, the Daemon checks herdr's reported version and refuses to record from a server
older than the supported minimum, saying so on the Board and in its log.

**Consequences (testable):**
- Against a server below the minimum, the Board shows an unsupported-version strip and records
  nothing.
- The gate is advisory against 0.8.2 during dogfooding on the operator's machine, controlled by
  a config flag, so the Board can be used before that machine is upgraded. The flag is off by
  default, and while it is on the Board shows the unsupported-version strip as a warning.
- Transitions recorded under the flag carry the herdr version (FR-6), so they can be discounted
  later. Below 0.9.0 herdr replays stale events on subscribe and has no lost-events signal, so
  FR-7's resync guarantee does not hold there and the warning strip says so.

#### FR-17: Loopback only, owner only

The Daemon binds to loopback by default, on a default port distinct from devswha/herdr-web-ui's
7317 so both run side by side. Its data directory, database and log are created owner-only. The
browser never reaches the herdr socket.

**Consequences (testable):**
- Default configuration is unreachable from another machine on the network.
- Data directory mode is `0700`; database and log file mode is `0600`.
- Setting a non-loopback bind address without authentication configured is refused at startup.

## 5. Non-Goals (Explicit)

- **Not a herdr client.** No terminal, no chat, no sending prompts or keys, no creating, splitting,
  focusing or killing panes. devswha/herdr-web-ui exists for that and is MIT.
- **Never calls focus.** `pane.focus` and `agent.focus` mutate herdr's seen state and would clear a
  completion the operator has not looked at. This is a correctness rule, not a scope choice.
- **Never reads pane output in v1.** No excerpts, no scrollback, no activity capture. This removes
  the one herdr read path that is not passive and the one data flow that captures secrets.
- **No authentication in v1, therefore no network exposure.** Loopback only. Exposure gets its own
  ADR before any code.
- **No cross-Host aggregation in v1.** One Daemon per Host, for two independent reasons: herdr's
  socket is local, and Profile resolution reads process environments that exist only on the Host.
  Ids are namespaced by Host and herdr server from the first commit so aggregation is additive
  later, by reading several Daemons' APIs, never several sockets.
- **Does not replace herdr's toasts.** They stay on until the Board has earned trust.
- **Not an orchestrator, scheduler, cost tracker or usage dashboard.**
- **Not installed as a herdr plugin.** It runs from its own directory with its own socket
  connection.

## 6. MVP Scope

### 6.1 In Scope

- The Board: FR-1 to FR-5.
- Recording: FR-6 to FR-9, and FR-9a backfill.
- Attribution: FR-10, FR-11.
- API: FR-12, FR-13, FR-14.
- Running it: FR-15, FR-16, FR-17.
- One Host, one operator, loopback, herdr 0.9.5 or newer with the dogfooding gate for 0.8.2
  (FR-16).

### 6.2 Out of Scope for MVP

- **Per-Agent history page.** Deferred to v2. The API already serves it (FR-14); the page is a
  later addition once the Board has proven its restraint. It is the first thing that will want to
  be added, and it comes as its own page, never as clutter on the Board.
- **Acknowledgement.** Marking a Finished Agent as dealt with without prompting it. The shape is
  settled by ADR-0001: devswha's finished-pane set beside a server generation marker. It is built
  only if dogfooding shows herdr's own Transitions are not enough. See §8, question 1.
- **Retiring the existing plugin.** Settled by ADR-0001: retire it once the Daemon is trusted, so
  there is one recorder. The change belongs to the provisioning repository, not this one.
- **Service units** (launchd, systemd user). Deferred to the first release; dogfooding runs the
  binary by hand.
- **Phone or other surfaces.** The API is shaped for them (UJ-4); none is built.
- **Second and third Hosts.** Same binary, one Daemon each, no aggregation.
- **Publishing for others.** Documentation, release artifacts and an install story come after
  dogfooding on all three Hosts.

## 7. Success Metrics

**Primary**
- **SM-1: The Board is the operator's first glance.** Over 30 consecutive working days of
  dogfooding, the operator reports checking the Board rather than cycling panes to find a waiting
  Agent. Validates FR-1 to FR-5. Measured by the operator's own judgement; a tool for one person
  does not need telemetry.
- **SM-2: No missed waiting Agent.** Zero occurrences in those 30 days of an Agent found to have
  been in Needs you for more than ten minutes without the Board having shown it. Validates FR-3,
  FR-4, FR-6, FR-7.

**Secondary**
- **SM-3: Waiting Time is trusted.** Zero occurrences of a Waiting Time the operator believed to be
  wrong, after checking against the terminal. Validates FR-6, FR-8, FR-14.
- **SM-4: Attribution is right.** Every Claude Code Agent on the Host shows its correct Profile on
  the first day of dogfooding. Validates FR-10, FR-11.

**Counter-metrics (do not optimize)**
- **SM-C1: Elements on the Board.** The Board carries only Boxes, workspace labels, and the
  connection and version strips. Any proposal to add a control, an excerpt, or a second data type
  per Box is a request for a different product and needs an ADR superseding ADR-0003, not a story.
  Counterbalances SM-1. *Amended 2026-10-21 (operator decision, Board layout v4):* a workspace
  label is the group heading, which carries that group's working, finished and idle counts, and
  the Needs you zone has a plain heading. Both are labels for the zones, not a control, an excerpt
  or a second data type per Box, so they need no ADR.
- **SM-C2: herdr load.** The Daemon issues no on-demand herdr reads to serve a page view; snapshot
  frequency is bounded by the reconcile timer. Counterbalances SM-2.

## 8. Open Questions

Backfill and the existing plugin's fate were open in the drafts; ADR-0001 settled both, and they
are now FR-9a and §6.2.

1. **Is acknowledgement needed at all?** herdr's own `done` to `idle` Transition happens when the
   operator views the pane in a terminal client. The old PRD observed the same pane reported
   `idle` and `done` to different calls minutes apart, so that may not be enough for the Board to
   stop showing an Agent as Finished. If it is not, the shape is settled (§6.2), but the precedent
   is thin: devswha's `completions.json` was seen once, empty, and how entries are added and
   removed was not observed. Decide after two weeks of dogfooding.
2. **One request per connection on 0.9.x.** Empirically true on 0.8.2 and consistent with 0.9.3
   source, unverified against a 0.9.5 binary. It shapes the whole client; milestone 1
   (addendum §J) proves it. *Answered 2026-10-16: it holds on 0.9.5 (socket API record, finding
   Q4).*
3. **Profile rules for other harnesses.** Observe one Codex pane and one Antigravity pane on the
   operator's machine, record what signal each exposes (Codex is believed to use `CODEX_HOME`,
   UNVERIFIED), and pick or write the rule. Until then FR-10's program-name default applies.
   Belongs in milestone 2 (addendum §J).
4. **Pane Titles from other harnesses.** Claude Code's Pane Title is its session name and has been
   observed to be short and free of prompt text. Codex and Antigravity titles have not been
   observed. Until they are, FR-2a's global default applies to them. Revisit when an Agent of each
   Agent Program has been seen on the Board.

## 9. Confirmed Assumptions

Drafted as assumptions, then confirmed or revised by the operator on 2026-10-13 and 2026-10-14.

- §4.1 — Boxes are a responsive card grid: a flat Needs-you-first top zone above the Boxes
  grouped by workspace.
- §4.1 FR-2a — The global default Context Line is empty.
- §4.2 FR-9 — Default Retention is 90 days.
- §4.3 FR-11 — Claude Code's Profile rule is the environment-variable rule on
  `CLAUDE_CONFIG_DIR` with a named absent case; other harnesses' rules are unknown until observed.
- §4.4 FR-14 — No history filtering or aggregation in v1.
- §4.5 FR-16 — The version gate is advisory against 0.8.2 behind a config flag during dogfooding.
