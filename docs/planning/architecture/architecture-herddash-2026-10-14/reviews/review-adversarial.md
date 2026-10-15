---
review: adversarial
lens: 'Construct two units one level down that each obey every AD to the letter yet build incompatibly'
target: docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md
context: [docs/adr/0004-runtime-dependencies.md, docs/adr/0005-append-only-transition-record.md, docs/adr/0006-how-herddash-speaks-to-herdr.md, docs/adr/0007-read-only-loopback-http-surface.md, docs/prd.md]
date: '2026-10-15'
---

# Adversarial review: the spine, attacked one level down

**Verdict: not yet a build substrate.** The package boundaries and the single-writer actor are
sound, but the spine names its shared shapes (`AgentView`, the transition, connection truth, the
SSE sequence) without fixing them, and it gives three responsibilities (reconcile/resync, socket
resolution, the database lock) two plausible owners each. Sixteen incompatible pairs follow. Six
are blocking, because they surface only when two milestones are integrated, after both have
passed their own tests.

The units assumed below are the milestones' epics: **M1** read herdr, **M2** record and attribute
(plus the FR-9a importer), **M3** serve the API and SSE, **M4** the Board, **M5** run unattended.
Each "Unit A / Unit B" pair is two stories, each of which a reviewer would pass against the spine
as written.

## Severity summary

| # | Hole | Units in conflict | Severity | Fix |
| --- | --- | --- | --- | --- |
| 1 | Reconcile timer and resync have two owners | M1 herdr client / M2 engine | Blocking | New AD-11 |
| 2 | SSE sequence rule drops events, and connect order loses them | M3 SSE / M4 Board | Blocking | Tighten AD-7 |
| 3 | Box order cannot travel in a single-Agent event | M3 SSE / M4 Board | Blocking | Tighten AD-1 |
| 4 | Waiting Time and Attention Class need history the engine does not own | M2 engine / M2 store / M5 Retention | Blocking | New AD-12, tighten AD-4 |
| 5 | Importer and engine build different transitions | M2 importer / M2 engine | Blocking | New AD-13 |
| 6 | Pane ids are not stable across herdr restarts | M1 / M2 / importer | Blocking | Tighten AD-3 |
| 7 | Profile resolution: synchronous or asynchronous, and "not yet" vs `unresolved` | M2 recording / M2 attribution | High | Tighten AD-2, AD-1 |
| 8 | The lock file has no defined mechanism | M2 importer / M5 daemon | High | Tighten AD-9 |
| 9 | Socket resolution and Server naming have two owners | M1 herdr / config / importer | High | Tighten AD-8, AD-3 |
| 10 | Connection truth has no shape | M1 / M2 engine / M4 Board | High | New core type in AD-1 |
| 11 | Removal: who diffs, and what a removal carries | M2 engine / M3 SSE | High | Tighten AD-7 |
| 12 | Store write and view publish have no order | M2 engine / M3 history route | Medium | Tighten AD-2 |
| 13 | Ordering mixes herdr sequence and wall clock | M2 store / M2 core | Medium | Tighten Time convention |
| 14 | Pane set has two owners | M1 herdr client / M2 engine | Medium | Fold into AD-11 |
| 15 | SQLite side files escape the 0600 rule; reads vs sweep | M2 store / M5 | Medium | Tighten AD-9, AD-10 |
| 16 | Version gate: does a refused server still populate the Board? | M1 / M2 engine / M4 | Medium | Fold into hole 10 |

---

## 1. Reconcile timer and resync have two owners (Blocking)

**Text in conflict.** AD-2: "The herdr client, the reconcile and Retention timers, and Profile
re-resolution send it [the engine] messages." AD-5 and ADR-0006: `internal/herdr` "reconciles on a
timer, and runs at most one resync at a time", and "Each one is recorded as a resync marker". AD-2
also says the engine "alone calls `store` writes", and `herdr` has no arrow to `store`.

**Unit A (M1, herdr client epic).** Implements ADR-0006 literally. On a lost-events close it
re-subscribes, calls `session.snapshot`, and emits `Snapshot{Reason: "lost_events"}` to its
consumer. It runs its own reconcile ticker that emits `Snapshot{Reason: "reconcile"}`. Single-flight
is a mutex inside the client.

**Unit B (M2, engine epic).** Implements AD-2 literally. It owns a reconcile ticker. On receiving
`StreamLost` it calls `herdr.Snapshot()` itself, guarded by its own in-flight flag, and writes a
resync marker.

**Integration.** Two tickers, two snapshot paths, and a resync that runs twice, once in each unit,
recording one or two markers depending on timing. Neither single-flight guard sees the other. Worse,
if the engine treats every `Snapshot` message as a resync it writes a marker on every reconcile
tick, flooding the record with gaps that never happened (and FR-8's "no Transition other than the
resync marker" becomes "a marker every N seconds").

**Fix: new AD-11, "Policy in the engine, mechanism in the adapter".**
- `internal/herdr` exposes a mechanism-only port: `Ping`, `Snapshot`, `SubscribeLifecycle`,
  `SubscribeAgentStatus(pane)`, `Unsubscribe(pane)`, and emits raw events plus stream-health
  signals (`StreamLost{pane|lifecycle, cause}`). It owns no timer and no resync decision.
- The engine owns the reconcile ticker, the resync decision, single-flight, and the marker.
- A reconcile tick whose snapshot agrees with current state writes nothing. A reconcile tick that
  disagrees writes `snapshot`-provenance transitions **and** a marker with reason
  `reconcile_drift`. A lost-events signal, unknown error code or rejected subscribe writes a marker
  with that reason. Marker reasons are a closed enum in core.
- Amend ADR-0006's "Resync" and "Truth" bullets to say the client provides the means and the engine
  decides.

## 2. SSE sequence rule drops events, and the connect order loses them (Blocking)

**Text in conflict.** AD-7: "every published view carries a monotonically increasing sequence
number. An SSE event is a whole `AgentView`, a removal of one AgentKey, or the connection truth,
stamped with that sequence number. On every connect and reconnect the client fetches `/api/state`
first, then applies only events with a higher sequence number."

**Pair 2a: several events, one sequence number.** A resync changes eight Agents and removes two, in
one published view, sequence 41. **Unit A (M3)** stamps all ten events `id: 41`, which AD-7
requires. **Unit B (M4)** keeps `lastSeq` and applies an event only if `id > lastSeq`, which AD-7
also reads as. It applies the first event, sets `lastSeq = 41`, and drops the other nine. The Board
now disagrees with `/api/state` until the next reconnect.

**Pair 2b: fetch-then-subscribe loses the gap.** Unit B fetches `/api/state` (sequence 40) and then
opens `EventSource`, exactly as "fetches `/api/state` first" says. A transition published as 41
between the two is never delivered, since there is no replay. The Box is wrong until the next
change to that Agent, which for a Needs you Box is the operator answering it. That is SM-2's
failure case.

**Pair 2c: browser auto-reconnect.** `EventSource` reconnects on its own and sends
`Last-Event-ID`. A Board that only refetches on its own `onerror` handler, and a server that ignores
`Last-Event-ID`, are each AD-7-compliant; together they leave a window after an automatic
reconnect where no refetch happens.

**Fix: tighten AD-7.**
- `/api/state` carries `view_seq`. Every SSE event carries the `view_seq` of the view it belongs to,
  and one view may produce several events.
- The client rule is "apply an event if its `view_seq` is greater than the `view_seq` of the
  `/api/state` it last loaded", never "greater than the last event applied".
- Order of operations: open the stream, buffer events, then fetch `/api/state`, then apply buffered
  and later events by that rule. Alternatively, and simpler: the server sends a `state` event
  carrying the whole `/api/state` body as the first event of every stream, and the Board never
  calls `/api/state` at all. Pick one; the second removes 2b and 2c together.
- The Board resets on every `open` event, including automatic reconnects. The server ignores
  `Last-Event-ID`.
- `view_seq` resets on Daemon restart; `/api/state` also carries a `boot_id` so a client never
  compares sequences across Daemon lifetimes.

## 3. Box order cannot travel in a single-Agent event (Blocking)

**Text in conflict.** AD-1: Box order is computed in core; "`/api/state` and every SSE event carry
`AgentView` verbatim. The Board renders it and computes nothing, except advancing the displayed
Waiting Time." AD-7: an SSE `agent` event is one whole `AgentView`.

**Unit A (M3).** `AgentView` carries a `rank` integer from core's order. An `agent` event carries
the one changed view. A transition into `blocked` changes the ranks of every Box below it, but those
Agents did not change, so no event is sent for them.

**Unit B (M4).** Has to place the Box somewhere. Either it trusts `rank` and now has two Boxes at
rank 3, or it sorts client-side, which AD-1 forbids. Both readings are compliant with something.

Waiting Time growth makes it worse: FR-3 says order within a class changes as Waiting Times grow,
which happens with no event at all.

**Fix: tighten AD-1.** `AgentView` carries a **sort key**, not a rank: `zone` (`top` or `grouped`),
`class_rank` (Needs you before Finished, Working before Unknown), `waiting_since` (UTC ms, the
start instant), `workspace` for the grouped zone, and the `AgentKey` as the final tie-breaker.
Core defines the comparator; the Board is permitted exactly one computation besides the clock, a
lexicographic sort on that key, documented in AD-1 beside the Waiting Time exception. Since
"Waiting Time descending" equals "`waiting_since` ascending", order then never changes without an
event. Add a golden test in core that the Board's sort, run on `/api/state`, reproduces the
server's order.

## 4. Waiting Time and Attention Class need history the engine does not own (Blocking)

**Text in conflict.** AD-1: Attention Class and Waiting Time are pure core functions "taking state
and a clock". The PRD makes both depend on the record: `idle` is Finished only if reached from
`working` or `done`, and Working "with no `working` in the record"; Waiting Time is "time since the
Agent entered its current Attention Class", with `done` to `idle` not resetting it, and a lower
bound "from the earliest evidence (a backfilled or recorded Transition, else Daemon start)". AD-2
holds state only in memory. AD-4 lets Retention delete.

**Pair 4a: who computes the start instant on restart.** **Unit A (M2 store)** adds
`LastTransitionInto(key, status)` and the engine uses its `observed_at`. **Unit B (M2 core)**
implements class entry with the `done`→`idle` merge. After a restart, an Agent that went
`working → done → idle` gets its start from the `idle` row in Unit A and from the `done` row in
Unit B. Waiting Time jumps on restart, breaking FR-8's "Waiting Times after restart equal those
before" and SM-3.

**Pair 4b: Retention changes the class.** An Agent has idled for 100 days since its last `working`
(rare but legal; or Retention set to 1 day for testing). The engine, running all along, holds
`everWorked = true` in memory: Finished. The M5 sweep deletes the `working` row. On next restart
the engine rebuilds from the store: no `working` in the record, so Working. Same Agent, same
Status, different class across a restart. FR-14 ("the response contains the Transition that defines
its current Waiting Time") is also broken by the sweep.

**Pair 4c: exactness across a gap.** A `live` transition into `done` was recorded at 09:54; the
Daemon was down 10:00 to 11:00; the snapshot at 11:00 still says `done`. Unit A reports exact 09:54
(FR-8's wording). Unit B reports `≥ 1h` since the Agent may have gone `working` and back while
nobody watched (the glossary's lower-bound rule). Both are defensible; the PRD contradicts itself
here and the spine does not choose.

**Fix: new AD-12, "Per-Agent derivation state is hydrated from the record by one port".**
- Core defines `AgentHistory` (the minimal facts derivation needs: current class entry instant,
  its provenance, whether a gap intervened, `ever_working`) and a pure fold
  `core.Fold(AgentHistory, Transition) AgentHistory`. Core alone decides class entry.
- `store` exposes one read port, `LoadHistories(host, server) map[AgentKey][]Transition`, raw rows
  with no domain logic, which the engine folds at start and after a resync. No `store` method
  encodes a domain rule.
- Exactness rule, chosen explicitly: a start instant is exact only if its transition is `live` and
  no resync marker or Daemon downtime for that server lies between it and now; otherwise it is a
  lower bound. Record a Daemon start as a resync marker with reason `daemon_start` so downtime is in
  the record. Amend PRD FR-8's consequence to say "equal, or shown as a lower bound if the Daemon
  was down", or explicitly choose the opposite; either way, in an AD.
- Tighten AD-4: the Retention sweep never deletes the newest transition per AgentKey nor the one
  that defines its current class entry, and `ever_working` survives deletion (store it on the
  surviving row or as a per-Agent fact).

## 5. Importer and engine build different transitions (Blocking)

**Text in conflict.** AD-4 lists the fields; PRD FR-9a says the log has no previous Status and no
sequence, "imported Transitions record both as unknown", "running the import twice adds nothing",
and pre-0.9.2 `done` entries are possibly spurious. The importer imports `core` and `store` but not
`config`.

**Pair 5a: "unknown" is a real Status.** **Unit A (M2 importer)** writes `prev_status = 'unknown'`,
which is what the PRD literally says. **Unit B (M2 core fold, hole 4)** reads `prev_status =
'unknown'` as herdr's `unknown` Status, so `unknown → done` counts as a class entry from Unknown, and
`ever_working` stays false for every imported Agent. Fix: absent means SQL `NULL` and Go
`*Status == nil`, never the string.

**Pair 5b: whose Host name.** The importer has no config arrow, so it defaults Host to
`os.Hostname()`. The Daemon uses the configured Host name. On a Mac whose configured name is `mbp`
and whose hostname is `Jasons-MacBook-Pro.local`, no backfilled row ever joins a live Agent, and
FR-9a's "earliest evidence" silently never applies.

**Pair 5c: idempotency key.** "Running twice adds nothing" with append-only and no update requires a
unique key. Unit A adds `UNIQUE(host, server, pane_id, observed_at, new_status)`. Unit B's engine
can legitimately record two `snapshot` transitions with the same millisecond and status for one
pane after a fast herdr restart, and its insert now fails. That index is also a transitions-table
change, which AD-10 says needs an ADR.

**Pair 5d: which Profile.** The importer can store the plugin's literal label, or re-resolve it
through today's Profile Map. The former matches FR-11 ("as resolved then"); the latter rewrites
history. Nothing says which.

**Pair 5e: possibly spurious needs a cutover date.** The log does not carry herdr's version per
entry. The importer needs an input saying when the Host moved to 0.9.2; one unit guesses from the
binary today, another marks everything.

**Fix: new AD-13, "One transition constructor, one append port".**
- `core.NewTransition(...)` is the only constructor; `previous Status`, `herdr ordering` and
  `herdr version` are optional pointers whose absence is `NULL`.
- `store.AppendTransitions([]core.Transition)` is the only write path for both engine and importer.
- Backfill idempotency is a separate `import_key` column (hash of the source log line), nullable and
  unique where not null, so live and snapshot rows are never constrained by it. Put it in
  ADR-0005's milestone-2 amendment.
- The importer resolves Host and Server through the same `config` function the Daemon uses (add the
  `importer --> config` arrow, or have `cmd` pass a resolved `Target`), and requires
  `--herdr-0.9.2-since=<date>` (or a config key) for the spurious flag. Profile is the log's own
  value, mapped through the Profile Map only if the log carries a `CLAUDE_CONFIG_DIR`, and never
  re-resolved later.

## 6. Pane ids are not stable across herdr restarts (Blocking)

**Text in conflict.** AD-3: identity is `(Host, Server, PaneID)`, and nothing else. FR-8 expects
continuity across herdr restarts. The research does not establish that herdr preserves pane ids
across a server restart, or that it never reuses one.

**Unit A (M2 engine).** After herdr restarts, the snapshot reports pane `p3`, which existed before.
It matches the stored `p3`, compares Status, and records nothing (FR-8).

**Unit B (M1 herdr client, against the real binary).** herdr restored the session layout with
fresh ids, and `p3` is now a different Agent (a `codex` pane where a `claude` pane was). Unit A has
just given the codex pane the claude pane's history, `ever_working`, Waiting Time and, via the
record, its Profile in the history route. The importer has the same problem across months of log:
`p3` in March and `p3` in October are different Agents under one key.

**Fix: tighten AD-3 before ADR-0005's columns freeze.** Make milestone 1 establish, against a 0.9.x
binary, whether pane ids survive a herdr restart and whether they are reused. Then either (a) add a
disambiguator to `AgentKey` (a herdr server instance id from `ping`, or the pane's creation time if
the schema exposes it), or (b) rule that a key whose Agent Program changed is a new Agent and that
continuity across a herdr restart is asserted only when the Agent Program and project directory
also match, recording a marker otherwise. Either way the rule lives in core and is applied by the
engine and the importer alike.

## 7. Profile resolution: synchronous or asynchronous, and "not yet" vs `unresolved` (High)

**Text in conflict.** AD-2 lists "Profile re-resolution" among the things that "send it messages",
implying a goroutine outside the actor. The engine imports `procenv` directly, implying a call. FR-2
says `unresolved` means "matched nothing"; FR-6 stores "Profile at the time".

**Unit A (M2 recording).** On a pane's first `agent_status_changed`, appends the transition with
whatever Profile the engine holds, which is empty, and stores `NULL` or `unresolved`.

**Unit B (M2 attribution).** Runs a `procenv` goroutine that reads the environment and sends
`ProfileResolved{key, "cldw"}` a few milliseconds later.

**Integration.** The first transition of every new Agent is permanently recorded as `unresolved`,
because the record is append-only (AD-4) and cannot be corrected. The Box flashes `unresolved`,
which FR-2 promises means a configuration mismatch. SM-4 fails on day one from a race, not a
mapping error. There is also no stated trigger for "when the pane's foreground process changes"
(FR-10): an event, a snapshot field, or a poll, and in which unit.

**Fix: tighten AD-2 and AD-1.** Profile resolution for a pane's first appearance is a synchronous,
time-bounded call made by the engine before it records or publishes that pane (a `procenv` read is
a `sysctl` or a `/proc` read, well under a millisecond). Re-resolution happens on the engine's
reconcile pass when the snapshot's foreground pid differs from the one last resolved. Core's Profile
value is a sum type: `Resolved(name)`, `Unresolved` (set, matched nothing), and no "pending" state
ever leaves the engine. Specify the rule interface in AD-1: `procenv` returns the process's
environment as a map, core's rule selects the variable; `procenv` never interprets.

## 8. The lock file has no defined mechanism (High)

**Text in conflict.** AD-9: "an exclusive lock file beside the database ... `import-plugin-log`
takes the same lock".

**Unit A (M5 Daemon lifecycle).** Creates `herddash.db.lock` with `O_CREATE|O_EXCL`, writes its
pid, removes it on clean exit.

**Unit B (M2 importer).** Opens the same path and calls `flock(LOCK_EX|LOCK_NB)`.

**Integration.** The importer's `flock` succeeds against a file the Daemon merely created, so the two
write concurrently, the exact thing AD-9 prevents. Separately, Unit A's pidfile survives a `kill -9`
or a crash, and a launchd-restarted Daemon then refuses to start forever, the opposite of "run
unattended" and FR-8. SQLite's own locking does not save this, because both processes would hold
valid SQLite connections.

**Fix: tighten AD-9.** The lock is `flock(2)` with `LOCK_EX|LOCK_NB` on `<database>.lock`, taken by one
function, `store.OpenExclusive(path)`, which both `serve` and `import-plugin-log` call and which
returns the open `*sql.DB` only while holding the lock. The kernel releases it on process death, so
no stale lock exists. State that the lock file is never deleted.

## 9. Socket resolution and Server naming have two owners (High)

**Text in conflict.** AD-8: "The socket path, when not configured, follows herdr's own resolution
order" (in `internal/config`). ADR-0006: "Find the socket by herdr's own order" (in
`internal/herdr`). AD-3: Server is `default` for the default socket and `<name>` for
`sessions/<name>/herdr.sock`.

**Unit A (config story)** resolves the path and passes it to the client. **Unit B (M1 client)**
re-resolves from the environment because ADR-0006 tells it to; under launchd, where
`HERDR_SOCKET_PATH` is unset but `--session` was a flag consumed by config, they reach different
sockets. Separately, a pinned `socket_path = "/tmp/x.sock"` fits neither AD-3 pattern: one unit
names the server `default`, another `x`, another the full path, and the stored key differs by
which unit wrote it. The importer, which never sees a socket, must also produce a Server name.

**Fix: tighten AD-8 and AD-3.** One function, `config.ResolveTarget() Target{SocketPath, Server}`,
owns the order and the naming; `internal/herdr` takes a `SocketPath` and never reads the
environment. A pinned path requires an explicit `server` name in the config (validation fails
otherwise). Also state in AD-3 that **one Daemon observes exactly one herdr server in v1**; the
spine currently lets a config unit accept a list of sessions while the engine assumes one, and
connection truth (hole 10) is singular only under that rule.

## 10. Connection truth has no shape (High)

**Text in conflict.** AD-2 publishes "the connection truth"; AD-7 makes it an SSE event; FR-5 needs
connected and last snapshot time and dimming of stale Boxes; FR-16 needs an unsupported-version
strip that is an error when refusing and a warning under the dogfooding flag, plus the 0.8.2 "resync
guarantee does not hold" message.

**Unit A (M2 engine)** publishes `{connected bool, last_snapshot_at}`. **Unit B (M4 Board)** must
show the version strip and decide when Boxes are "stale" to dim them. With nothing in the view, the
Board either computes staleness from `last_snapshot_at` and the browser clock (forbidden
computation, and a threshold nobody chose), or shows nothing. A third unit (M1 version gate) has no
field to put the version state in.

**Pair 16 folded in: does a refused server populate the Board?** FR-16 says a refused server
"records nothing". One engine unit still applies snapshots to in-memory state and publishes Boxes
without writing; another applies nothing. The Board shows ten Boxes, or none, under the same strip.

**Fix: add `core.ConnectionTruth` to AD-1**, carried verbatim like `AgentView`:
`state` (`connecting`, `connected`, `disconnected`), `last_snapshot_at`, `herdr_version`,
`version_gate` (`ok`, `advisory`, `refused`), `resync_guaranteed` (false below 0.9.0), and `stale`
(computed in core, true whenever `state != connected`). State in AD-2 that under `refused` the
engine publishes no `AgentView`s and writes nothing, and under `advisory` it does both.

## 11. Removal: who diffs, and what a removal carries (High)

**Text in conflict.** AD-2: the engine publishes "an immutable view"; readers "take that view".
AD-7: SSE events are per-Agent `AgentView`s and removals. Nothing says who turns views into events.

**Unit A (M2 engine)** publishes whole views through an atomic pointer plus a broadcast "changed"
signal; a slow reader sees view 5 then view 8. **Unit B (M3 SSE)** expects a delta list per view and
emits exactly those events; with views 6 and 7 skipped, it misses a removal, and the Board keeps a
Box for a closed pane (FR-1 broken) until the next reconnect. Conversely, if the engine sends deltas
on a buffered channel and the SSE unit drops on a full buffer, the same happens.

The removal's payload is also open: `{"key": {...}}` nested, or flat `host`/`server`/`pane_id`; the
Board's DOM key must match whichever `AgentView` uses. And "removed" covers two different facts,
pane closed and pane no longer an Agent, plus a third during resync, absent from the new snapshot.

**Fix: tighten AD-7.** The engine publishes each view with its delta (`changed []AgentView`,
`removed []AgentKey`, `connection *ConnectionTruth`). Each SSE client has a bounded queue; on
overflow the server closes that client's stream, forcing the hole-2 refetch, and never drops
silently. Fix one JSON shape for `AgentKey`, a nested `key` object, used identically in
`AgentView` and `removed`, with a canonical string form `host/server/pane_id` (URL-escaped
per segment) for DOM ids and the history route. Removal means "absent from current state" for any
reason; the reason is not carried.

## 12. Store write and view publish have no order (Medium)

**Unit A (M2 engine)** publishes the new view, then appends the transition. **Unit B (M3 history
route)** reads the store. A client that receives an `agent` event and immediately fetches history
sees no transition behind the Waiting Time on screen, violating FR-14's consequence. And on a store
write error (disk full), one engine unit publishes anyway, another stops; the Board then shows state
the record will never have.

**Fix: tighten AD-2.** Append, then publish. A failed append is fatal to that change: the engine
logs it, marks connection truth degraded (or exits for the supervisor to restart), and does not
publish state the record lacks.

## 13. Ordering mixes herdr sequence and wall clock (Medium)

**Text in conflict.** Time convention: "Order within one Agent is herdr's sequence when known, else
`observed_at`."

**Unit A (M2 store history query)** orders `ORDER BY herdr_seq NULLS LAST, observed_at`. **Unit B
(M2 core fold)** orders by `observed_at` and breaks ties by sequence. A `snapshot` row (no sequence)
between two `live` rows lands last in Unit A and in the middle in Unit B. herdr's sequence also
restarts with herdr, so sequence 3 after a restart sorts before sequence 900 from before it. Backfill
imported after weeks of live recording has later row ids but earlier times, so row id is no answer
either. On a Mac that sleeps, `observed_at` can jump.

**Fix: tighten the Time convention into a core comparator** used by both store queries and the fold:
order by `observed_at`, then herdr sequence only when both rows carry one from the same herdr
server instance (hole 6's instance id), then row id. History SQL sorts by `observed_at, id` and core
re-sorts with the comparator; `store` does no domain ordering.

## 14. The pane set has two owners (Medium)

ADR-0006 has the client track the pane set from lifecycle events to manage one subscription per
Agent pane. AD-2 has the engine own current state, which is also a pane set. **Unit A (M1)**
auto-subscribes on `pane.created` and unsubscribes on `pane.closed`. **Unit B (M2)** removes an
Agent on a snapshot diff. After a missed `pane.closed`, the client keeps a subscription the engine
no longer tracks, and a pane that becomes an Agent later (FR-1) has no subscription if the client
subscribed only to panes that were Agents at creation. **Fix:** fold into AD-11. The engine owns the
pane set and commands `SubscribeAgentStatus` and `Unsubscribe`; the client keeps only the
connections the engine asked for, and reports a failed subscribe as a stream-health signal.

## 15. SQLite side files and reads versus the sweep (Medium)

AD-9 makes the database, lock and log `0600`, but SQLite creates `-wal`, `-shm` and `-journal` files
using the process umask, so a store unit that `chmod`s only the main file leaves agent activity in a
world-readable WAL. Separately, the journal mode is unspecified. **Unit A (M5 sweep)** deletes 90
days of rows in one transaction; **Unit B (M3 history route)** reads on another connection, and in
rollback-journal mode its reads fail with `SQLITE_BUSY` during the sweep. **Fix:** tighten AD-9: set
`umask(0077)` at process start, before any file is created. Tighten AD-10 or ADR-0005: the store
opens in WAL mode with a `busy_timeout`, uses one write connection (the engine's) and a separate
read pool for `httpapi`, and sweeps in bounded batches.

---

## Pairs considered and found closed

- **Two writers of the record.** AD-2 plus AD-9 close it, once hole 8 fixes the lock mechanism.
- **The Board computing Attention Class.** AD-1 forbids it plainly; the only leak is ordering
  (hole 3) and staleness (hole 10).
- **A focus or output read sneaking in.** AD-5 makes the capability absent; the compiler enforces it.
- **Timezone.** Storage and transport are UTC milliseconds; the Board formats in local time. Same
  Host, so browser and Daemon clocks agree. Nothing to fix beyond hole 13.
- **Config mutation at runtime.** AD-8's immutable struct closes it. The config struct's owner is
  `internal/config`; the Profile Map's types belong in `core` (the `config --> core` arrow already
  says so). One addition: validation must reject a rule type core does not register, so the
  registry lives in core.

## Recommended order of fixes

1. AD-11 (holes 1, 14) and the AD-3 identity question (hole 6), because milestone 1 must answer it
   against a real binary before ADR-0005's columns freeze.
2. AD-12 and AD-13 (holes 4, 5) together, as they share the transition shape; amend ADR-0005 with
   `NULL` semantics, `import_key`, and the Retention floor.
3. AD-7 and AD-1 tightening (holes 2, 3, 10, 11), before milestone 3 starts, since they fix the wire
   contract the Board is written against.
4. The rest can land as amendments while milestone 2 is in progress.
