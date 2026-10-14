# herddash PRD — Addendum

Material that belongs downstream of the PRD or that earned a place without fitting it: the
technical "how", the protocol constraints that shape the architecture, the stack, and the
alternatives already rejected. Architecture and story work reads this beside `prd.md`. The frozen
research records in `docs/research/` remain the primary sources; this condenses them for the
recorder.

## A. Data flow

```
herdr server
   │  unix socket, JSON API, one request per connection
   ▼
herddash Daemon
   ├─ global subscriptions: pane.created, pane.closed, pane.agent_detected
   ├─ per-pane subscriptions: pane.agent_status_changed  (one connection each)
   ├─ session.snapshot on connect, after events_lost, and on a reconcile timer
   ├─ Profile resolution from the pane's foreground process environment
   ├─ SQLite: transitions, resync markers, Profile at time of transition
   └─ HTTP on loopback
         GET /            the Board (embedded HTML + small JS)
         GET /api/state   current state, sorted, with connection truth
         GET /api/panes/{host}/{pane}/history
         GET /events      server-sent events
   ▼
browser: loads /, fetches /api/state once, holds /events, repaints one Box per event
```

The browser never touches the herdr socket. The Daemon never makes a herdr call to serve a page.

## B. Protocol-forced requirements

Each is a requirement on the architecture, not a preference. Provenance per
`docs/research/herdr-socket-api.md`; items marked UNVERIFIED were taken from herdr 0.9.3
documentation and source and not exercised against a running server. Complying costs nothing,
so comply.

- **One request per connection** (confirmed on 0.8.2). A fresh dial per call, one long-lived
  connection per subscription. No pooling, no multiplexing.
- **No global agent-status subscription** (confirmed: 3 of 27 subscription kinds require a pane
  id, and `pane.agent_status_changed` is one). Track the pane set from the global lifecycle streams
  and maintain one status subscription per pane.
- **Subscribe before snapshotting** (UNVERIFIED, 0.9.0 behaviour). Subscriptions deliver live
  events only. Snapshot first and the gap is lost silently.
- **Treat a subscribe as all or nothing** (UNVERIFIED). One stale pane id rejects the request and
  closes the connection. Refresh the pane list and retry; never assume partial success.
- **Decode two envelope conventions** (UNVERIFIED). Lifecycle pushes use underscored names in
  `{event, data}`; the pane-scoped streams keep dotted names.
- **`events_lost` is a resynchronise signal** (UNVERIFIED, 0.9.2+). It arrives as an error on the
  subscription; the server then closes it. Recovery: new subscription, wait for
  `subscription_started`, snapshot on a separate connection, replace the cache. Snapshots and
  events share no sequence boundary; events are invalidation signals, never deltas. Serialise
  refreshes; refresh again if events arrive mid-read. Record a resync marker (FR-7).
- **Version-gate on `ping`**, not on a protocol number. There is no handshake; the `protocol`
  integer in responses belongs to a different protocol.
- **Reconnect indefinitely.** herdr restarts, hands off to a new server, and may not be running
  when the Daemon starts. None of that is an error.
- **Reconcile on a timer regardless.** The snapshot is the source of truth; events are the
  low-latency invalidation signal.
- **Never call `pane.focus` or `agent.focus`** (UNVERIFIED seen-state semantics, asymmetric cost).
- **Never read pane output in v1.** This removes the unbounded-`recent` trap entirely (UNVERIFIED
  behaviour: drives the agent's scroll interface, fails with `agent_not_idle` on non-idle panes).
  If a later version reads output, it reads `visible` or `recent` bounded by `viewport_rows`.

## C. What the snapshot gives for free

Every agent record carries `terminal_id`, `agent_status`, `workspace_id`, `tab_id`, `pane_id`,
`focused`, `revision`; usually also `agent`/`display_agent`, `terminal_title`,
`cwd`/`foreground_cwd`, `state_change_seq`, `agent_session`, `screen_detection_skipped`. Use
`revision` and `state_change_seq` for change detection and ordering. The snapshot rolls status up
to tab and workspace with labels and pane counts, which is the Board's grouping.

## C2. Context Line derivation

FR-2a is a per-Agent-Program rule with a global default, chosen so a harness that sets no usable
pane title still produces a defined Box. The shape the architecture should expect is a small
provider interface keyed by Agent Program, one implementation per known harness, and a default
implementation that returns empty. The Daemon computes the Context Line once and serves it on the
API beside the raw Pane Title, so the Board and any later surface agree without re-deriving it.

Known behaviour, observed on the operator's machine 2026-10-13 (15 agents, all Claude Code):
Pane Titles are the session's short name, 7 to 45 characters, and one pane carried the harness
placeholder `Claude Code`, which the Claude Code rule must treat as absent. Codex and Antigravity
not yet observed.

## D. Attribution: the reference implementation

Profile resolution is a per-Agent-Program rule, the same provider shape as the Context Line: a
registry keyed by Agent Program, one rule per known harness, a default that returns the program
name. The environment-variable rule is one rule type, parameterised by variable name, a
value-to-Profile map and an absent-case name. It obtains the foreground process from herdr's
`pane.process_info` and reads the environment from the OS (`/proc/<pid>/environ` on Linux, the
process-arguments sysctl on macOS), which works only on the same Host and as the same user. That
is the second reason, beside the socket, that the Daemon is Host-local.

The Ansible-deployed herdr plugin on the operator's machine fires on `pane.agent_status_changed`,
reads `CLAUDE_CONFIG_DIR` from the pane's foreground process, and maps it through a rendered
profile map to a launcher name (`cldp`, `cldw`, `claude`, `oneio`). The Daemon does the same with
`pane.process_info` or equivalent, re-resolving when the foreground process changes. Resolution
order per Agent Program: variable set and matched, use the entry; set and unmatched, `unresolved`;
absent, the program's absent-case entry (for Claude Code this is the stock `~/.claude` profile and
the shipped config names it); no entries for the program at all, the program name. Record the
Profile on each Transition rather than joining at read time, so a later map edit does not rewrite
history (FR-11).

Its log is the backfill candidate: 1627 `done` and 288 `blocked` events over several weeks as of
2026-10-05, each with agent, profile, status, pane id, workspace id. Many `done` entries are likely
spurious, predating herdr 0.9.2's fix for false `done` on startup and restore. Open question 2.

## E. Attention Class mapping

| herdr Status | Attention Class | Note |
|---|---|---|
| `blocked` | Needs you | always dominant |
| `done` | Finished | awaiting next prompt |
| `idle` reached from `working` or `done` | Finished | herdr marks `done` as `idle` once seen in a terminal client; the Board still treats it as awaiting a prompt |
| `idle` with no prior `working` in the record | Working-adjacent calm, shown as Finished with no Waiting emphasis | fresh pane, never prompted |
| `working` | Working | calm |
| `unknown`, or none yet | Unknown | calm, dim |

The `done` vs `idle` distinction is not treated as authoritative (observed: the same pane reported
each to different calls minutes apart). Open question 1 decides whether herddash needs its own
acknowledgement set, copying devswha's `completions.json` shape: a server generation marker beside
a set of finished pane ids.

## F. Stack

| Choice | Rationale |
|---|---|
| Go 1.25+ | One static binary per platform, no runtime on target, trivial cross-compilation to macOS and Linux. Many long-lived subscription connections fanning out to browser clients fit its concurrency model. |
| Standard library HTTP | No framework. |
| Server-sent events | One-way feed, plain HTTP, browsers reconnect for free. |
| SQLite, pure-Go driver | Real queries for history; no C toolchain, so cross-compilation stays trivial. |
| Embedded assets, hand-written JavaScript | No npm, no build step, nothing to install per Host. |
| Types generated from `herdr api schema --json` of a real 0.9.3 binary | Field lists in the research lean partly on an older bundled schema; types are not frozen until that dump exists. |

## G. Security posture, and the bar set by prior art

Both existing projects ship unauthenticated on loopback by default; alecuba16's has token auth
and HTTPS machinery that is simply off. herddash matches the loopback-only default and goes
further in one way: it refuses to start with a non-loopback bind and no authentication (FR-17),
and in v1 there is no authentication, so there is no non-loopback mode. Database and log are
`0600`, matching herdr's socket. Because v1 never reads pane output, the recorded data is
Status, Profile, directory and terminal title only; the terminal title is the one field that can
carry prompt text (open question 6).

## H. Multi-Host, deferred

herdr's multi-host support is client-side SSH federation: no server learns another exists, no
machine dimension exists in any payload, and ids collide across machines. The one CLI bridge is
polling-only with no cross-machine subscriptions. Hence one Daemon per Host, Host-namespaced ids
from the first commit, and no aggregation in v1. Entirely UNVERIFIED by direct test; from 0.9.3
documentation and source.

## I. Rejected alternatives

- **herdr plugin event hooks instead of a socket client.** Hooks fire a fresh process per event,
  and the Daemon must hold subscriptions continuously to see every Transition. Also, the machine's
  plugin registry is Ansible-managed and already occupied.
- **On-demand CLI calls instead of a Daemon.** Would only see Transitions while being asked. The
  Daemon's continuous subscription is the property that makes it better than the toasts.
- **Borrowing herdr's seen state for acknowledgement.** Requires focus calls, which mutate it.
- **Forking alecuba16/herdr-webui.** AGPL-3.0-or-later with an unresolved provenance question.
- **Adopting one of the unofficial Go socket clients.** All version-0 with stated instability;
  generating types from the schema is the reusable idea.
- **A board that also shows output excerpts** (the 2026-10-05 draft). Rejected for v1 by the
  operator's "no clutter" and by removing the only non-passive read path.

## J. Milestones, as the build order the data flow forces

1. **Read the world.** Connect, version-gate, subscribe, fan out per-pane status subscriptions,
   snapshot, reconcile, survive `events_lost`, reconnect. Print Transitions to stdout. No HTTP.
   All protocol risk lives here; open question 4 is answered here.
2. **Record and attribute.** SQLite, Profile resolution, Retention sweep, resync markers.
3. **Serve.** `/api/state`, `/api/.../history`, `/events`.
4. **The Board.** Consuming only milestone 3.
5. **Run it unattended.** Service units, first release, then the second and third Hosts.
