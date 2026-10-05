# herddash — Product Requirements

**Status:** draft, 2026-10-05.
**Target herdr version:** 0.9.3. That release is a two-line hotfix over 0.9.2 with no API changes,
so everything here applies from 0.9.2 onward. Upgrading a machine to it is out of scope for this
project and assumed done.

Protocol detail behind the design decisions here lives in
[docs/research/herdr-socket-api.md](research/herdr-socket-api.md) and
[docs/research/herdr-multi-host.md](research/herdr-multi-host.md).

---

## 1. The problem

herdr already knows the state of every agent pane it manages. The only way it tells you is a
desktop notification, and that channel has limits that are not configurable:

- **Popups for the active tab are always suppressed.** Hard-coded, no setting.
- **Delivery requires a foreground attached client.** `no_foreground_client` is an explicit
  non-delivery reason. A detached session raises nothing, in any delivery mode.
- **At most 8 notifications queue**, oldest dropped, with fixed durations of 8 seconds for
  "needs attention" and 5 seconds for "finished".
- **Only two agent transitions notify at all**: entering `blocked`, and a background completion
  into `idle`.

So the current channel is transient, lossy, silent when detached, and answers only "something
changed". It cannot answer "what is everything doing", and it keeps no history.

The scale makes this concrete. A live snapshot of the author's machine on 2026-10-05:

| Dimension | Count |
|---|---|
| agent panes | 10 |
| total panes | 15 |
| tabs | 9 |
| workspaces | 3 |

Those agents span several Claude Code subscriptions and two different agent programs. There is no
single place to see them.

## 2. The goal

One web page, always current, that answers: **what is every agent doing right now, and which ones
are waiting on me?**

Success looks like the author checking the page instead of watching the notification tray, and
noticing a blocked agent sooner than the toast would have told them, including while fully
detached.

## 3. Prior art, and why this project exists anyway

**There is substantial prior art and it must be read before any code is written.** The
`herdr-plugin` topic on GitHub and the awesome-herdr list index several browser interfaces for
herdr already:

| Project | Stack | Overlap |
|---|---|---|
| devswha/herdr-web-ui | TypeScript | Large. Browser and phone client, per-pane chat, live terminal, remote hosts over SSH, web push. Several hundred stars. |
| alecuba16/herdr-webui | Rust and Axum | Standalone browser interface with its own backend, workspace navigation, agent status. |
| DebugZHY/Herdr-Dash | JavaScript | Local web interface showing panes as conversation or raw transcript. |

There are also three unofficial Go socket clients. The most complete generates its types from
`herdr api schema` and is a reusable approach even if the library itself is not adopted.

So the honest framing is that "a web interface for herdr" is taken. What is not obviously taken is
this specific thesis:

1. **A daemon that does not depend on an attached client.** The existing projects are largely
   interfaces onto a session you are already in. herddash's reason to exist is working while you
   are detached, which is exactly when herdr's own notifications go silent.
2. **Triage, not remote control.** The board is sorted by who has been blocked longest and is
   meant to be read from across a desk. It is not a terminal in a browser.
3. **Persisted history.** A timeline of what each agent did, which none of the above appears to
   keep.
4. **Several subscriptions of the same agent program, disambiguated.** herdr cannot tell these
   apart. See §6.1.
5. **One static binary deployable to several machines by configuration management**, rather than a
   Node or Rust toolchain per host.

**This is open question 1 in §11 and it should be answered before milestone 1 starts.** Reading
devswha's project may well shorten this project or end it.

## 4. Non-goals for v1

- **Not a replacement for herdr's terminal UI.** herddash is read-mostly. It does not create,
  focus, split or kill panes, and does not send prompts or keys to agents. Focus is additionally
  forbidden for a correctness reason, not just scope. See §6.1.
- **No authentication.** v1 binds to loopback only. Exposing it to a network requires auth, and
  that is deliberately deferred, not forgotten. See §9.
- **No cross-host aggregation.** v1 serves the agents of one herdr server, on its own host. The
  data model carries a host dimension from the first commit so that the next phase is additive.
- **Does not disable herdr's toasts.** They stay on. The two channels coexist until the page has
  earned trust.
- **Not an agent orchestrator, scheduler or cost tracker.**

## 5. Users

One operator, running many concurrent coding agents on their own machine, on the same machine as
the browser. A later phase adds the same operator on a phone over a private network, which is why
§9 exists now rather than later.

## 6. The data herdr already gives us

From `session.snapshot`. Every agent record carries `terminal_id`, `agent_status`, `workspace_id`,
`tab_id`, `pane_id`, `focused` and `revision`, and usually also:

| Field | Use in herddash |
|---|---|
| `agent` / `display_agent` | which agent program, for example `claude` or `codex` |
| `agent_status` | one of `idle`, `working`, `blocked`, `done`, `unknown` |
| `terminal_title` / `terminal_title_stripped` | **the best one-line "what is it doing"** |
| `cwd` / `foreground_cwd` | which project |
| `state_change_seq` | monotonic, orders transitions |
| `agent_session` | the agent's own session identity |
| `screen_detection_skipped` | whether status came from a hook rather than screen reading |

The snapshot also rolls `agent_status` up to each tab and each workspace, and gives each a `label`
and a `pane_count`. That hierarchy is the page's natural structure, free of charge.

Terminal titles are far more informative than the status alone. They typically carry a status
glyph and the agent's current task, in the shape of `◐ <what it is working on>`, which is a better
activity line than any status word.

### 6.1 Three things herdr does not give us

**Which subscription an agent belongs to.** Several launchers exec the same `claude` binary, so
every pane reports the agent as `claude`. The distinguishing fact lives only in the pane process
environment, as `CLAUDE_CONFIG_DIR`. herddash must resolve it itself, from the pane's foreground
process, and map it to a human name through configuration. An existing herdr plugin on the
author's machine already does this and is the reference implementation.

**A trustworthy "unacknowledged" flag.** `done` and `idle` both mean ready for input. The only
difference is whether that pane has been marked *seen* in the server's seen state, and each
terminal client tracks viewed completions independently. Observed directly: the same pane reported
`idle` to one call and `done` to another minutes later.

**Requirement:** herddash defines acknowledgement as its own concept, derived from its own event
history and its own user interaction, and does not treat the `done` versus `idle` distinction as
authoritative.

**A hard constraint follows.** `pane.focus` and `agent.focus` *mutate* that seen state. Reads do
not. **herddash must never call focus.** Doing so would silently clear a completion the operator
has not actually looked at, which is the exact failure the product exists to prevent.

**Stable ids across hosts.** herdr ids are scoped to one server and are documented to collide
between machines. Every id herddash stores is namespaced by the host it came from, from the first
commit.

## 7. The views

### 7.1 The board, which is the whole point

One row or card per agent pane, grouped by workspace and tab, **sorted so the agent that has been
waiting on you longest is first.** Each row shows:

- the resolved subscription or profile name, and the agent program
- the status, with time in that status
- the terminal title as the activity line
- the project directory
- a short excerpt of recent output

Blocked agents are visually dominant. Everything else is calm. The page must be readable at a
glance from across a desk.

### 7.2 The detail page

One pane, reached by clicking a row:

- the full state transition timeline for that pane, from stored history
- a longer window of recent terminal output
- herdr's own explanation of why it classified the status as it did, from `agent.explain`
- where the pane lives, and its ids, for finding it in the terminal

## 8. Architecture

A single long-lived daemon that is a herdr socket API client, plus an HTTP server.

```
herdr server ──unix socket──▶ herddash daemon ──SSE──▶ browser
                                     │
                                     ▼
                                  SQLite
```

**Why a socket client rather than a herdr plugin.** Plugin event hooks fire a fresh process per
event and, decisively, the output-related events are deliberately excluded from plugin hooks. A
manifest naming one gets a validation warning and the hook silently never fires. Activity cannot
come from plugin events at all.

**Why a daemon rather than an on-demand CLI call.** The daemon holds its own subscriptions, so it
sees every transition whether or not a terminal client is attached. That single property is what
makes herddash strictly better than the toasts, not merely quieter.

### 8.1 What the protocol forces on the design

Each of these is a requirement, not a preference. The reasoning is in the research record.

- **One request per connection.** A connection carries one request and one response and then
  closes. Only subscriptions and waits stay open. So: a fresh dial per call, plus one long-lived
  connection per subscription. There is no pool to reuse and no multiplexing.
- **Version-gate on `ping`, not on a protocol number.** The JSON API has no handshake and no
  version negotiation, and the `protocol` integer in responses belongs to a different protocol.
- **Subscribe before snapshotting.** Subscriptions deliver live events and no longer replay
  history. Snapshot first and the transitions in the gap are lost silently.
- **Fan out agent-status subscriptions per pane.** There is no global agent-status subscription,
  which is the most awkward fact in the API for this product. Track the pane set from the global
  `pane.created`, `pane.closed` and `pane.agent_detected` streams and maintain one status
  subscription per pane as panes come and go.
- **Treat a subscribe as all or nothing.** A single stale `pane_id` rejects the whole request and
  closes the connection. Refresh the pane list and retry; never assume the other entries took.
- **Decode two naming conventions.** Lifecycle pushes use underscored event names; the three
  pane-scoped streams keep dotted ones.
- **Treat `events_lost` as a resynchronise signal.** It arrives as an error on the subscription and
  the server then closes it. Recovery is a new subscription, then a snapshot **on a separate
  connection**, then replace the cache. Snapshots and events share no sequence boundary, so
  buffered events are never replayed onto a snapshot. Serialise refreshes.
- **Reconnect indefinitely.** herdr restarts, hands off to a new server, and may not be running
  when herddash starts. None of that is an error.
- **Reconcile on a timer regardless.** The snapshot is the source of truth; the event stream is
  the low-latency invalidation signal.

### 8.2 Activity capture, and the trap in it

Output is read per pane on demand, because no push event for output is available to us.

**Reading is not unconditionally passive.** For a recognised full-screen agent at the bottom of its
transcript, a `recent` read asking for more lines than the visible screen drives the agent's own
scroll interface and pages through its alternate screen. Worse for this product, that same read
fails with `agent_not_idle` when the agent is working, blocked or unknown.

A naive read loop would therefore fail on exactly the panes the board most wants to show.

**Requirement:** herddash reads `visible`, or `recent` bounded by the pane's own `viewport_rows`.
Never an unbounded `recent`. Read when a pane changes status, when its detail page is open, and on
the reconcile timer for blocked panes. Never poll every pane continuously.

### 8.3 Persistence

SQLite holds the event history that the detail page's timeline and the "how long blocked" sort both
need. Retention is bounded and configurable, with a finite default. Captured output is subject to
the same retention as the events.

### 8.4 Go types

Generate from `herdr api schema --json` dumped from a real 0.9.3 binary, rather than hand-writing
them. The research record's field lists lean partly on an older bundled schema, so the types are
not frozen until that dump exists.

## 9. Security and privacy

**This is the section that constrains the roadmap.** herddash captures agent terminal output in
order to show what agents are doing. That output routinely contains API tokens, credentials and
private source code.

- v1 binds to loopback by default. The bind address is configurable so the private-network phase
  is a configuration change rather than a rewrite, but **shipping a non-loopback bind without
  authentication is out of the question.**
- The database and any logs are created with owner-only permissions, matching the herdr socket's
  own `0600`.
- The browser never reaches the herdr socket. All herdr access is server-side.
- Retention is finite by default, because an unbounded store of agent output is a liability.
- Authentication, and whatever the private-network phase needs, gets its own decision record
  before any code.

## 10. Stack

| Choice | Rationale |
|---|---|
| Go | One static binary per platform, no runtime on target. Trivial cross-compilation for several machines across two operating systems. Concurrency model fits many long-lived subscription connections fanning out to many browser clients, which §8.1 makes unavoidable. |
| Standard library HTTP | No framework needed. |
| Server-sent events | The feed is one-way. Plain HTTP, browsers reconnect for free. Future write actions become ordinary POST endpoints. |
| SQLite, pure-Go driver | Real queries for history, and no C toolchain, so cross-compilation stays trivial. |
| Server-rendered templates, embedded assets, hand-written JavaScript | No npm, no build step, nothing extra to install per host. |

## 11. Open questions

1. **Does this project survive contact with the prior art?** Read devswha/herdr-web-ui and
   alecuba16/herdr-webui against §3's five differentiators before milestone 1. Adopting or forking
   one of them is a legitimate outcome.
2. **Verify one-request-per-connection against a real 0.9.3 binary.** It is empirically true on an
   earlier release and consistent with unchanged 0.9.3 source, but it determines the entire
   connection design, so it is the first thing milestone 1 proves.
3. **Backfill.** An existing herdr plugin on the author's machine has logged weeks of `blocked` and
   `done` transitions. Worth importing as initial history, or not worth the code?
4. **The fate of that plugin.** Retire it once the daemon works, so there is one mechanism, or keep
   it as an independent push path?
5. **Acknowledgement interaction.** What does the operator actually do on the page to mark an agent
   as dealt with, given that §6.1 forbids borrowing herdr's own seen state?

Resolved since the first draft, and recorded in the research files rather than here: the socket
wire protocol, and whether herdr's multi-host support gives us several machines for free. It does
not. See [docs/research/herdr-multi-host.md](research/herdr-multi-host.md).

## 12. Milestones

1. **Read the world.** Connect, version-gate, subscribe, fan out per-pane status subscriptions,
   snapshot, reconcile, survive `events_lost`, reconnect. Print state to stdout. No web server.
   All the protocol risk is here, so it is first and alone.
2. **The board.** HTTP server, SSE feed, the live page, sorted by who needs you longest.
3. **History.** SQLite persistence, the detail page timeline, retention.
4. **Activity.** Bounded output excerpts on the board and the detail page.
5. **Run it unattended.** launchd and systemd user units, reconnect hardening, first release.
