---
title: Rubric review of the herddash architecture spine
reviewed: docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md (status draft, updated 2026-10-15)
also_read: [docs/prd.md, docs/adr/0004..0007, docs/planning/prds/prd-herddash-2026-10-08/addendum.md, docs/adr/README.md]
date: 2026-10-15
---

# Rubric review — herddash architecture spine

## Verdict

**Revise before epics.** The spine is terse, well-layered and mostly enforceable, and its stack is
verified current. But two High findings would let separately built stories diverge on things the
PRD tests directly: how an Agent's identity and Waiting Time survive restarts, and how the history
route reaches the store without breaking the import graph. The operational envelope is also largely
silent. None of this needs a redesign. It needs about four ADs to be tightened, one added, and a
short Open Questions section.

Severity is the impact on downstream builders: **High** means two stories will predictably build
incompatible things or a PRD consequence cannot be met as written. **Medium** means a likely rework
or a silent gap in a dimension the spine owns. **Low** means a tidy-up.

## Checklist summary

| Criterion | Result |
| --- | --- |
| Fixes the real divergence points for epics and stories | Mostly. Misses restart identity and state seeding (F1), Board ordering under per-Agent events (F3), and the one-Daemon-per-server question (F5). |
| Every AD's Rule is enforceable and prevents its stated divergence | Mostly. The import graph contradicts AD-6 (F2). AD-1 conflicts with AD-7 (F3). ADR-0007's no-CORS rule does not stop the cross-origin read it is meant to stop (F8). |
| Nothing under Deferred lets two units diverge | One problem. Deferring transitions columns "by amendment to ADR-0005" conflicts with the ADR immutability rule (F7). Service units are deferred, but nothing fixes the default paths they will need (F4). |
| Named tech is verified current | Yes, with one stale entry. modernc.org/sqlite v1.60.1, BurntSushi/toml v1.6.0 and Go 1.27.2 are all latest as of 2026-10-15. herdr's latest is **0.9.5** (2026-10-14), not 0.9.4 (F9). |
| Covers FR-1..FR-17, FR-2a, FR-9a | Every FR is mapped. FR-8, FR-13, FR-9a and FR-10 each have a consequence the spine cannot yet meet (F1, F6, F9). |
| Every altitude-owned dimension is decided, deferred, or an open question | **No.** Default locations, log rotation, backup, schema downgrade, SQLite journal mode and sidecar file modes, shutdown and health are all silent (F4). There is no Open Questions section (F9). |
| Spine and ADRs agree | Mostly. See F7 and F9 for the disagreements. |

## Findings

### F1 — High — Identity and current state across restarts are undecided (AD-1, AD-2, AD-3)

The spine keeps current state only in engine memory (AD-2). ADR-0005 rejects a stored
current-state row. Several PRD consequences, however, depend on facts that live only in the record:

- **FR-8:** after a Daemon restart, Waiting Times must equal what they were before.
- **Glossary and FR-3:** `idle` counts as Working only when there is "no `working` in the record".
- **Glossary:** a lower-bound Waiting Time is taken from "the earliest evidence (a backfilled or
  recorded Transition, else Daemon start)".

AD-1 says core functions take "state and a clock". None of the ADs says that state includes
per-Agent facts derived from the record, or that the engine seeds those facts from `store` at
startup and after each resync. Without that rule, one story will compute Waiting Time from memory
alone, which breaks FR-8. Another will query the store on every view, which turns the store into a
second source of truth that AD-2 is meant to prevent.

AD-3 has a related gap. It keys identity on `(Host, Server, PaneID)` but sets no rule for pane ids
across a herdr restart. Addendum §E notes that "pane ids may not survive a herdr restart" and that
a server generation marker matters to FR-8. If herdr reuses an id, a new Agent inherits an old
Agent's history and Waiting Time. If herdr renumbers panes, every Waiting Time is lost. FR-9a makes
this worse: the plugin log carries pre-restart pane ids that can collide with live ones, so an
imported row can define a live Agent's Waiting Time.

**Fix.**

1. Add a Rule to AD-2: at start and after each resync, the engine loads, for each AgentKey in the
   snapshot, the facts the core needs. These are the last Transition, whether the Agent was ever
   `working`, and the earliest evidence for the current Attention Class, each with its provenance.
2. Fix in AD-1 how provenance and the possibly-spurious flag decide whether Waiting Time is exact
   or a lower bound.
3. Decide in AD-3 either a server generation component in the key or a rule for continuing an
   Agent across a herdr restart. Make the same decision for how backfilled rows join to live
   AgentKeys.

### F2 — High — The import graph forbids the history route (Invariants graph, AD-6, Capability Map)

Two parts of the spine require `internal/httpapi` to read the store:

- AD-6 builds responses "from the engine's published view or a `store` read".
- The Capability Map places FR-14 in "`internal/httpapi`, `internal/store` reads".

The graph, however, declares that "arrows are the only permitted import directions" and has no
`httpapi --> store` edge. The only path to the store is the engine's "read port only". This leaves
the FR-14 story with three options: break the declared rule, route history reads through the engine
actor (which then serialises reads behind writes, something AD-2 implies it should not), or have
the engine expose a store reader through its port. Different builders will pick differently.

**Fix.** Choose one and draw it. Either add `httpapi --> store` labelled read-only, with a rule
that `store` exposes a read-only interface type that the HTTP layer receives, or state that the
engine's read port includes history and draw no edge. A read-only SQLite connection, opened with
`mode=ro` and `query_only`, makes the read-only promise enforceable.

### F3 — Medium-High — Per-Agent SSE events conflict with "the Board computes nothing" (AD-1 vs AD-7)

AD-1 makes Box order a core function and forbids the Board from computing anything except the
Waiting Time ticker. AD-7 makes each SSE event a single `AgentView`, a removal, or the connection
truth. When one Agent moves into `blocked`, every other Box's position changes, and FR-3 requires
that "immediately". Either the event stream carries order (a position or sort key in each
`AgentView`, or a full re-send of the order), or the Board sorts. The second option violates AD-1.
Workspace group order in the lower zone is also unspecified.

**Fix.** Add a server-computed sort key and zone/group key to `AgentView`, plus a rule that the
Board orders by those keys and nothing else. Alternatively, publish an `order` event that carries
the list of AgentKeys after any change that reorders.

### F4 — Medium-High — The operational envelope is mostly silent (Deployment note, AD-8, AD-9, AD-10)

The spine owns these dimensions at initiative altitude. Each of the following is neither decided,
deferred, nor an open question.

| Dimension | Gap | Why builders diverge |
| --- | --- | --- |
| Default locations | No default config file path, `--config` flag, data directory (XDG vs `~/Library/Application Support`), database path, or log path. FR-15 says a config holding only the Profile Map must work. | The config, store, logging and service-unit stories each invent a path. The Deferred item for units says "nothing here constrains them", but they need these paths. |
| SQLite mode and file modes | Journal mode (WAL or rollback) and busy timeout are undecided. Under WAL the `-wal` and `-shm` files are created by the driver under the process umask, not explicitly at `0600`. | FR-17 owner-only can silently fail for sidecar files. Rule: set umask `0077` at process start. WAL mode also changes the backup and lock story. |
| Logging operations | `slog` JSON goes "to the log file", but there is no rotation or size cap. | Under launchd or systemd a daemon that runs forever grows its log without limit, or two stories pick stderr and a file respectively. Decide: stderr only, leaving rotation to the service manager, or a capped file. |
| Upgrades and migrations | AD-10 covers forward migrations only. Nothing covers an older binary meeting a newer schema, or a failed migration. | Rule needed: refuse to start when the stored schema version is newer than the binary knows, and leave the database untouched when a migration fails. |
| Backup and restore | Silent. The record is the product's claim to truth (ADR-0003). | At least defer it explicitly, and say whether a plain file copy is safe given the journal mode, or name `VACUUM INTO`. |
| Shutdown and health | No SIGTERM/SIGINT rule (stop subscriptions, close SSE, close the database, release the lock) and no liveness signal. | Lock-file staleness after a crash is undecided. If AD-9 uses a pid file rather than `flock`, a crashed Daemon blocks the next start and the importer. Rule: an OS advisory lock (`flock`) that the kernel releases on exit. |
| Environments | The dogfooding environment is covered by the FR-16 flag. Running test builds beside the live Daemon on one Host (port, data directory) is not. | Minor once default paths and the port exist. |

The default port is deferred to milestone 3 in ADR-0007 but is not listed in the spine's Deferred
section. Add it there.

### F5 — Medium — One Daemon per Host or per herdr server? (AD-3, AD-8, AD-9)

AD-3 puts Server in the key, and the Glossary allows several named sessions per Host. AD-8,
however, resolves exactly one socket, and AD-9 allows one Daemon per database. FR-12 promises
"every Agent on the Host". Nothing decides whether one Daemon watches every
`sessions/*/herdr.sock`, or one Daemon runs per server with its own database and port. These lead
to different engine, config and service-unit designs.

**Fix.** Decide v1 as one server per Daemon, with multi-server on a Host following the multi-Host
pattern, or else add a socket list to config. Either way, state it.

### F6 — Medium — Attribution has unverified inputs and no trigger rule (AD-1, procenv, ADR-0004)

- Addendum §D marks the source of the foreground pid (`pane.process_info`) as UNVERIFIED. The spine
  neither carries it as an open question nor puts it in the milestone 1 or 2 verification list.
- FR-10 says "re-resolving when the pane's foreground process changes", but no AD says what detects
  that change: a reconcile tick, a status event, or a pid comparison on snapshot. AD-2 only says
  re-resolution "sends it messages".
- On darwin, reading another process's environment needs a raw `sysctl` (`KERN_PROCARGS2` with a
  pid). The standard library's `syscall.Sysctl` cannot express this, so builders will reach for
  `golang.org/x/sys/unix`. That is a new direct dependency, which ADR-0004 forbids "without a
  further ADR". The alternative, shelling out to `ps`, is a different design. Decide which before
  the procenv story.
- The failure path is undefined. When the process has exited or reading its environment is denied,
  does the Agent resolve to `unresolved` or keep its last Profile?

### F7 — Medium — ADR-0005 is Accepted yet is to be "amended" in milestone 2 (Deferred bullet 1, ADR-0005 header)

`docs/adr/README.md` says that a record is never edited once accepted, except to change its status.
ADR-0005 and the spine's Deferred section both plan to fix columns and indexes "by an amendment to
this record". AD-10 also says a transitions-table change needs an ADR. That is a contradiction a
milestone 2 builder will run into.

**Fix.** Either mark ADR-0005 accepted in principle and say the columns come from a follow-on ADR
that the spine names now, or carve out an explicit exception in the ADR README. The Deferred bullet
should also name what milestone 2 must preserve. The FR-9a idempotence consequence ("running the
import twice adds nothing") needs a uniqueness key, and backfill rows carry no herdr sequence, so
the key has to be settled as part of those columns.

### F8 — Medium — "No CORS" does not stop cross-origin reads on loopback (AD-6, ADR-0007)

ADR-0007 relies on omitting CORS headers so that "a page from another origin cannot read the API".
A DNS-rebinding page, which resolves its own hostname to `127.0.0.1`, is same-origin with the
listener as far as the browser is concerned, so it can read `/api/state` and `/events`. Those carry
project directories, Profiles and Pane Titles. The Rule therefore does not prevent the divergence
ADR-0007 states.

**Fix.** Add one line to AD-6: reject any request whose `Host` header is not the configured
loopback host:port. This is cheap, testable and in line with the read-only posture.

### F9 — Low — Staleness, small disagreements and smaller FR gaps

- **herdr version:** the spine Stack table and ADR-0006 say "latest 0.9.4". herdr **0.9.5** was
  released 2026-10-14. Update the spine. ADR-0006's dated context can stay.
- **Go version:** the spine sets the `go` directive to 1.26, while CLAUDE.md and addendum §F say
  "Go 1.25 or newer". Align them.
- **Stale history route in the addendum:** addendum §A still shows
  `/api/panes/{host}/{pane}/history`, while the spine and ADR-0007 use
  `/api/agents/{host}/{server}/{pane}/history`. The spine is right. Amend the addendum so a story
  author does not copy the old route.
- **FR-13 resync events:** FR-13 requires the stream to push "each resync". The SSE event names are
  `agent`, `removed` and `connection`, and none is named for a resync. Either define that a resync
  appears as a `connection` event carrying the new snapshot time, or add a `resync` event.
- **FR-16 strip state:** the unsupported-version strip needs gate state, meaning the herdr version
  and whether the result is refused or advisory. Say that this is part of "connection truth".
- **FR-9a dates:** marking entries "from before the Host ran herdr 0.9.2" as possibly spurious needs
  a cutoff date. Nothing says where it comes from (a config value, an import flag, or "all backfill
  rows").
- **FR-10 heading:** showing the name once when Profile equals Agent Program is display logic. Under
  AD-1 it belongs in `AgentView`, for example as a `heading` field. Say so.
- **Engine and adapters:** the paradigm says hexagonal, but the graph has `engine` importing the
  concrete adapters, and no rule says where port interfaces live. Without one, engine tests cannot
  use the fakes the Tests convention assumes, and each story will put its interfaces in a different
  place. Rule: ports are interfaces declared in `internal/engine`, and `cmd` wires the adapters in.
- **No Open Questions section:** PRD open question 2 (one request per connection on 0.9.x) still
  shapes AD-5 and is not carried. Open questions 1 and 3 appear only inside Deferred, and 4 is
  absent. Add a short Open Questions list with each question's owning milestone, alongside F5, F6
  and the `process_info` item.
- **Retention scope:** state whether the sweep also deletes resync markers, and what Waiting Time
  shows if the Transition that defines it is swept. The answer is presumably a lower bound.

## What is good and should be kept

- The single-writer actor, immutable published view and sequence number (AD-2 with AD-7) remove a
  whole class of concurrency and staleness bugs, and the rule is short enough to enforce in review.
- "The capability is absent, not merely unused" (AD-5) turns the never-focus and never-read-output
  rules into compile-time facts.
- The append-only record with provenance and a possibly-spurious flag (AD-4) is the right shape for
  the 0.8.2 and backfill realities.
- The Stack is pinned and verified. modernc.org/sqlite v1.60.1 (2026-09-29), BurntSushi/toml v1.6.0
  and Go 1.27.2 are each the latest per proxy.golang.org and go.dev on 2026-10-15.
- The Capability Map maps every PRD FR, and the spine stays decisions-only, as intended.
