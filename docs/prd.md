# herddash — Product Requirements

**Status:** draft, 2026-10-05. Sections marked *open* are unresolved.
**Target herdr version:** 0.9.3. Upgrading the machine to 0.9.3 is out of scope for this project
and assumed already done.

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

## 3. Non-goals for v1

- **Not a replacement for herdr's terminal UI.** herddash is read-mostly. It does not create,
  focus, split or kill panes, and does not send prompts or keys to agents.
- **No authentication.** v1 binds to loopback only. Exposing it to a network requires auth, and
  that is deliberately deferred, not forgotten. See §9.
- **No multi-host aggregation.** v1 shows the agents of one herdr server. See §11.
- **Does not disable herdr's toasts.** They stay on. The two channels coexist until the page has
  earned trust.
- **Not an agent orchestrator, scheduler or cost tracker.**

## 4. Users

One operator, running many concurrent coding agents on their own machine, on the same machine as
the browser. A later phase adds the same operator on a phone over a private network, which is why
§9 exists now rather than later.

## 5. Target environment

- herdr 0.9.3 or newer, running as its usual background server.
- macOS on arm64, and Ubuntu on amd64 and arm64.
- Single static binary, no runtime dependency on the target machine.

## 6. The data herdr already gives us

Observed live from `herdr api snapshot`. Every agent pane carries:

| Field | Use in herddash |
|---|---|
| `agent` | which agent program, for example `claude` or `codex` |
| `agent_status` | one of `idle`, `working`, `blocked`, `done`, `unknown` |
| `terminal_title` / `terminal_title_stripped` | **the best one-line "what is it doing"** |
| `cwd` / `foreground_cwd` | which project |
| `pane_id`, `tab_id`, `workspace_id` | where it lives |
| `focused` | whether the operator is looking at it |
| `state_change_seq` | monotonic, orders transitions |
| `revision` | per-pane change counter |

The snapshot also rolls `agent_status` up to each tab and each workspace, and gives each a `label`
and a `pane_count`. That hierarchy is the page's natural structure, free of charge.

Terminal titles are far more informative than the status alone. Real examples from the same
snapshot:

```
◐ agentcore_a1.tf
◑ JEV, Decision Models, and Ultrathink for VFKB
✳ Proceed
```

### 6.1 Two things herdr does not give us

**Which subscription an agent belongs to.** Several launchers exec the same `claude` binary, so
every pane reports the agent as `claude`. The distinguishing fact lives only in the pane process
environment, as `CLAUDE_CONFIG_DIR`. herddash must resolve it itself, from the pane's foreground
process, and map it to a human name through configuration. An existing herdr plugin on the
author's machine already does exactly this and can be used as the reference implementation.

**A trustworthy "unacknowledged" flag.** `done` and `idle` both mean ready for input. The only
difference is whether that pane has been marked *seen*, and seen-ness is tracked independently per
attached terminal client. herddash is not a terminal client. Observed directly: the same pane
reported `idle` to one CLI invocation and `done` to another minutes later.

**Requirement:** herddash defines acknowledgement as its own concept, derived from its own event
history and its own user interaction, and does not treat the `done` versus `idle` distinction as
authoritative.

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
- herdr's own explanation of why it classified the status as it did
- where the pane lives, and its ids, for finding it in the terminal

## 8. Architecture

A single long-lived daemon that is a real herdr socket API client, plus an HTTP server.

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

**Why a daemon rather than an on-demand CLI call.** The daemon holds its own socket connection, so
it sees every transition whether or not a terminal client is attached. That single property is what
makes herddash strictly better than the toasts, not merely quieter.

Required behaviours:

- **Subscribe before snapshotting.** As of herdr 0.9.0, subscriptions deliver live events and no
  longer replay retained history. Snapshot first and transitions in the gap are lost silently.
- **Treat the `events_lost` error as a resynchronise signal.** herdr reports dropped events to a
  slow subscriber rather than losing them quietly. The handler re-snapshots and reconciles.
- **Reconcile on a timer regardless.** Events alone drift. A periodic full snapshot is the source
  of truth and the event stream is the low-latency path.
- **Reconnect indefinitely.** herdr restarts, hands off to a new server, and may not be running at
  all when herddash starts. None of that is an error.

### 8.1 Activity capture

Terminal output is read per pane on demand rather than streamed, because no push event for output
is available to us. Read when a pane changes status, when its detail page is open, and on the
reconcile timer for panes that are blocked. Never poll every pane continuously.

### 8.2 Persistence

SQLite holds the event history that the detail page's timeline and the "how long blocked" sort both
need. Retention is bounded and configurable, and the default is finite. Captured output is subject
to the same retention as the events.

## 9. Security and privacy

**This is the section that constrains the roadmap.** herddash captures agent terminal output in
order to show what agents are doing. That output routinely contains API tokens, credentials and
private source code.

- v1 binds to loopback by default. The bind address is configurable so that the LAN phase is a
  configuration change rather than a rewrite, but **shipping a non-loopback bind without
  authentication is out of the question.**
- The database and any logs are created with owner-only permissions.
- The browser never reaches the herdr socket. All herdr access is server-side.
- Retention is finite by default, because an unbounded store of agent output is a liability.
- Authentication, and whatever the private-network phase needs, gets its own decision record
  before any code.

## 10. Stack

| Choice | Rationale |
|---|---|
| Go | One static binary per platform, no runtime on target. Trivial cross-compilation for three machines across two operating systems. Concurrency model fits one socket reader fanning out to many browser clients. |
| Standard library HTTP | No framework needed. |
| Server-sent events | The feed is one-way. Plain HTTP, browsers reconnect for free. Future write actions become ordinary POST endpoints. |
| SQLite, pure-Go driver | Real queries for history, and no C toolchain, so cross-compilation stays trivial. |
| Server-rendered templates, embedded assets, hand-written JavaScript | No npm, no build step, nothing extra to install on three machines. |

## 11. Open questions

1. **The socket API wire protocol.** Framing, handshake, version negotiation, the exact subscribe
   request shape, and whether 0.9.3 allows a global lifecycle subscription with no pane id. In
   0.8.2 every subscription required a pane id, which would force herddash to enumerate panes and
   subscribe individually. *Research in progress.*
2. **herdr 0.9.0 multi-host support.** herdr added machine support with machine-scoped
   notifications. If one server can already see agents on other hosts, the multi-machine goal
   needs no transport of our own. The data model carries a machine dimension from day one either
   way. *Research in progress.*
3. **Backfill.** An existing herdr plugin on the author's machine has logged weeks of `blocked` and
   `done` transitions. Worth importing as initial history, or not worth the code?
4. **The fate of that plugin.** Retire it once the daemon works, so there is one mechanism, or keep
   it as an independent push path?
5. **Acknowledgement interaction.** What does the operator actually do on the page to mark an agent
   as dealt with, given §6.1?

## 12. Milestones

1. **Read the world.** Connect, subscribe, snapshot, reconcile, reconnect. Print state to stdout.
   No web server. This is where the protocol risk lives, so it is first and alone.
2. **The board.** HTTP server, SSE feed, the live page, sorted by who needs you longest.
3. **History.** SQLite persistence, the detail page timeline, retention.
4. **Activity.** Output excerpts on the board and the detail page.
5. **Run it unattended.** launchd and systemd user units, reconnect hardening, first release.
