---
status: final
stepsCompleted: [1, 2, 3, 4]
inputDocuments:
  - docs/prd.md
  - docs/planning/prds/prd-herddash-2026-10-08/addendum.md
  - docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md
  - docs/adr/0001-what-herddash-is.md
  - docs/adr/0003-the-board-is-the-product.md
  - docs/adr/0004-runtime-dependencies.md
  - docs/adr/0005-append-only-transition-record.md
  - docs/adr/0006-how-herddash-speaks-to-herdr.md
  - docs/adr/0007-read-only-loopback-http-surface.md
  - docs/adr/0014-acknowledging-a-finished-agent.md
---

# herddash - Epic Breakdown

## Overview

This document breaks the requirements from the PRD and the architecture spine into implementable
stories. There is no separate UX design; by the operator's decision (2026-10-16) the Board's visual
design is a story inside the Board epic, built against real data. PRD vocabulary (§3) is used
exactly, and `AD-n` refers to the architecture spine's rules.

## Requirements Inventory

### Functional Requirements

FR-1: The Board shows exactly one Box for every Agent herdr reports on the Host, appearing and disappearing without a page reload.
FR-2: Each Box shows Profile and Agent Program as its heading, and the shortened project directory, the workspace and tab labels, the Attention Class, the Waiting Time (exact or `≥` lower bound) and a one-line Context Line, with space reserved when it is empty.
FR-2a: The Daemon derives the Context Line per Agent Program with an empty global default. Claude Code's session-name Pane Title is shown, and its placeholder `Claude Code` counts as absent.
FR-3: The top zone holds Needs you then Finished, each longest-waiting first, flat across workspaces. Working then Unknown sit below, grouped by workspace. A `done` Agent becoming `idle` keeps its place and its Waiting Time, and a never-prompted `idle` Agent sits with Working.
FR-4: The Board reflects a recorded Transition within one second through the event stream, with no polling, and recovers by itself when the stream drops.
FR-5: The Board shows whether the Daemon is connected to herdr and when it last received a snapshot. Stale Boxes are dimmed, never hidden.
FR-6: The Daemon records every Transition with Host, herdr server, pane id, Agent Program, Profile at the time, previous and new Status, herdr ordering, herdr version and observation time. Each is marked live, snapshot-derived or backfilled.
FR-7: When the Daemon cannot guarantee it saw every event, it resynchronises from a snapshot and records a resync marker.
FR-8: The Daemon reconnects to herdr forever with backoff. Waiting Times survive a Daemon restart, and a herdr restart records no Transition for an unchanged Agent.
FR-9: Retention deletes Transitions older than N days (default 90), except those a present Agent still needs.
FR-9a: A one-shot import loads the existing plugin's log as backfilled history. Pre-0.9.2 `done` entries are flagged possibly spurious, unknown fields stay unknown, entries older than Retention are skipped, and running it twice adds nothing.
FR-10: The Daemon resolves each Agent's Profile through the rule for its Agent Program. Claude Code uses `CLAUDE_CONFIG_DIR` with a named absent case, a set-but-unmatched variable is `unresolved`, a program with no rule shows its program name, and the heading never shows the same name twice.
FR-11: The Profile Map lives in the config file, selects and parameterises a rule per Agent Program, and changes on restart without rewriting recorded history.
FR-12: `/api/state` returns every Agent's view, already ordered as the Board orders it, with connection truth.
FR-13: A server-sent events endpoint pushes each recorded Transition, resync and connection change, carrying enough to update one Box with no second request.
FR-14: A history request for one Agent returns its Transitions within Retention, in order, including the one that defines its current Waiting Time.
FR-15: herddash is one binary with embedded assets and one config file, and runs with only the Profile Map configured. It finds the herdr socket by herdr's own resolution order, including under launchd or systemd.
FR-16: A version gate refuses to record from herdr older than 0.9.5 and shows an unsupported-version strip. A config flag makes the gate advisory for dogfooding on 0.8.2, with a warning strip, and records the herdr version on every row.
FR-17: The Daemon binds to loopback by default on a port distinct from 7317, refuses a non-loopback bind without authentication, and creates the data directory `0700` and its files `0600`.

### NonFunctional Requirements

NFR1: Read-only surface. No route changes anything; there are no POST, PUT or DELETE routes (PRD §4.4, ADR-0007).
NFR2: No on-demand herdr call serves a page or API response (PRD §4.4, SM-C2).
NFR3: Latency. A recorded Transition reaches an open Board within one second (FR-4, FR-13).
NFR4: Never call `pane.focus` or `agent.focus` (PRD §5). The client has no such method (ADR-0006).
NFR5: Never read pane output in v1 (PRD §5). The client has no such method (ADR-0006).
NFR6: Loopback only. The API rejects requests whose `Host` header is not a loopback name, which stops DNS rebinding (ADR-0007).
NFR7: Owner-only files. umask `077`; the data directory is `0700`, and the database, WAL, SHM, lock and log are `0600` (AD-9).
NFR8: One static binary per OS and architecture, with `CGO_ENABLED=0` and no runtime on the Host (FR-15, ADR-0004).
NFR9: Resilience. Reconnect indefinitely; herdr's absence, restart or handoff is normal, not an error (FR-8).
NFR10: Restraint. The Board carries only Boxes, workspace labels and the connection and version strips. Anything more needs an ADR superseding ADR-0003 (SM-C1). Since 2026-10-21 a workspace label carries its group's counts and the Needs you zone has a plain heading (SM-C1's amendment).
NFR11: Bounded herdr load. Snapshot frequency is bounded by the reconcile timer and a minimum resync interval (SM-C2, AD-11).
NFR12: Not a herdr plugin, and herdr's Ansible-owned configuration is never touched (PRD §5).
NFR13: Supported herdr is 0.9.5 or newer; older versions run only behind the dogfooding flag (FR-16).
NFR14: Box uniformity. Every Box has the same height whatever its title, and the Board is readable from across a desk (FR-2, PRD §4.1).
NFR15: Tests never touch a live herdr. `go test ./...`, `go vet ./...` and `gofmt -l .` pass in CI (CLAUDE.md, spine conventions).

### Additional Requirements

From the architecture spine, ADRs 0004 to 0007, and CLAUDE.md:

- **No starter template.** Greenfield Go module `github.com/vilosource/herddash` with `go 1.26`, and the package layout from the spine's Structural Seed: `cmd/herddash` and `internal/{core,engine,herdr,procenv,store,httpapi,importer,config}`. The import graph in the spine is enforced by a test.
- **Board tests.** The Board's ordering and event logic is a plain ES module tested in CI with Node's built-in `node --test`, using no npm packages and the same golden fixture as the Go tests. This adds no runtime dependency and no build step.
- **Board tests.** The Board's ordering and event logic is a plain ES module tested in CI with Node's built-in `node --test`, using no npm packages and the same golden fixture as the Go tests. This adds no runtime dependency and no build step.
- **CI.** A GitHub Actions workflow runs build, vet, test and gofmt on every pull request and on pushes to `develop`, plus a cross-compile check for darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64 with `CGO_ENABLED=0`. Today only release-please exists.
- **Dependencies, pinned (ADR-0004):** `modernc.org/sqlite` v1.60.1 with `modernc.org/libc` pinned exactly to v1.77.1, `github.com/BurntSushi/toml` v1.6.0, and `golang.org/x/sys` v0.49.0 for darwin only. Nothing else.
- **AD-1:** core is pure. It computes Attention Class, Waiting Time and its exactness, Profile, Context Line and the zone-aware sort key `[zone, group, class_rank, start, agent_key]` into one `AgentView`. A golden fixture shared by Go and the Board's JavaScript checks order and workspace headers.
- **AD-2:** a single engine goroutine owns state and calls `store.Append`, and publishes an immutable view with its change list, a sequence number and a run id.
- **AD-3:** the Agent key is `(Host, Server, Epoch, PaneID)`. `internal/config` resolves Host and Server, the epoch changes only on evidence that pane ids were reassigned, and epoch 0 is reserved for backfill.
- **AD-4 / ADR-0005:** the record is append-only. Each Daemon start writes a `daemon_start` resync marker, and the Retention floor follows FR-9.
- **AD-5 / ADR-0006:** only `internal/herdr` opens the socket.
  - One request per connection; each pane's status subscription has its own connection, and lifecycle kinds share one.
  - Subscribe before snapshotting, at start and for every new pane.
  - Wire types are generated from `herdr api schema --json` of a 0.9.5 binary.
  - There is no focus method and no output method.
- **AD-6 / ADR-0007:** GET-only routes `/`, `/assets/`, `/api/state`, `/api/agents/{host}/{server}/{epoch}/{pane}/history` and `/events`. Requests need a loopback `Host` header, errors use a JSON error shape, and no CORS headers are sent.
- **AD-7:** SSE opens with a `state` event (the `/api/state` body plus run id and sequence), then `agent`, `removed`, `transition`, `resync` and `connection` events. Each stream has a bounded buffer and drops on overflow.
- **AD-8:** one TOML config, flag over file over default, applied on restart. Defaults are `~/.config/herddash/config.toml` and the data directory `$XDG_DATA_HOME/herddash` (falling back to `~/.local/share/herddash`).
- **AD-9:** `store.Open` takes an exclusive `flock` for serve and import alike, and sets umask `077`.
- **AD-10:** migrations are embedded and numbered. The database runs in WAL mode, and a newer schema is refused.
- **AD-11:** the engine owns the pane set, the reconcile timer, resync decisions with a one-at-a-time guard and minimum interval, the version gate and epoch detection. Status events are recorded as live, and snapshot disagreements as snapshot-derived.
- **AD-12:** the engine seeds from the record through `core.Fold` at start and after each resync. A Waiting Time is exact only from a live start whose Status is unchanged; otherwise it is a lower bound.
- **AD-13:** `core.NewTransition` and `store.Append` are the only constructor and insert. Unknown values are NULL, imported rows carry a unique `import_key` and epoch 0, and the operator supplies the date the Host reached herdr 0.9.2.
- **AD-14:** Profile is resolved before an Agent's first Transition, with a timeout yielding `unresolved`. It is re-resolved on agent detection and on a foreground-process change.
- **Logging:** `log/slog` JSON to `herddash.log` in the data directory. Environment values are never logged except a matched `CLAUDE_CONFIG_DIR`, and Pane Titles only at debug.
- **Milestone-1 findings (spine open questions):** whether pane ids survive a herdr restart and how a new server instance shows; the foreground-process call and its change event; herdr's ordering field; and one request per connection on 0.9.5. Each is recorded by amending the research record in the same change (CLAUDE.md).
- **Release (milestone 5):** a launchd agent and a systemd user unit that set no `HERDR_SOCKET_PATH`, clean shutdown on SIGINT and SIGTERM, and release artifacts built by a workflow keyed off release-please's tag. Release-please PR #2 is held until the first working daemon.

### UX Design Requirements

No UX design document exists. Board visual design (palette, typography, card styling, emphasis
treatment for Needs you, Finished, Working and Unknown, and the strips) is delivered by a design
story in the Board epic, within the constraints of FR-1 to FR-5, NFR10 and NFR14.

### FR Coverage Map

FR-1: Epic 3 - one Box per Agent, live
FR-2: Epic 3 - Box contents, exact or lower-bound Waiting Time
FR-2a: Epic 3 - Context Line derivation per Agent Program
FR-3: Epic 3 - two zones, sort key, workspace groups
FR-4: Epic 3 - pushed updates within one second, self-recovering stream
FR-5: Epic 3 - connection and version strips, dimmed stale Boxes
FR-6: Epic 2 - record every Transition with provenance
FR-7: Epic 1 - resync behaviour against herdr; Epic 2 - resync markers in the record
FR-8: Epic 1 - reconnect forever; Epic 2 - Waiting Times and history intact across restarts
FR-9: Epic 2 - Retention sweep with the present-Agent floor
FR-9a: Epic 2 - plugin-log import
FR-10: Epic 1 - Profile resolution per Agent
FR-11: Epic 1 - Profile Map in configuration
FR-12: Epic 3 - /api/state
FR-13: Epic 3 - /events
FR-14: Epic 3 - history route
FR-15: Epic 1 - one binary, one config, socket discovery; Epic 4 - service units
FR-16: Epic 1 - version gate and dogfooding flag (its Board strip lands in Epic 3)
FR-17: Epic 3 - loopback listener and Host check; Epic 1 - owner-only data directory

## Epic List

#### Epic 5: After 0.5.0: closing what dogfooding and reviews left open

The deferred-work list and the PRD's open question on acknowledgement are closed. Background
tracking stays correct across resumed and cleared sessions, CI checks what only a developer's Mac
or a hand-run browser checked before, and the documents agents read stop contradicting the code.
Planned on 2026-10-23 with the operator's decisions taken up front (vfkb c56d5199f84c, 1e89662f83af).
**FRs covered:** none new; hardens ADR-0009, FR-10, FR-1/2 (Board drawing) and PRD §8 Q1.
Added 2026-10-26 after 0.6.0: Story 5.19, the Board's title bar (ADR-0013), at the operator's request.
Amended 2026-10-27: PRD §8 Q1 was closed by the operator's decision instead, so Story 5.9 is
superseded by Epic 6.

#### Epic 6: Acknowledging a Finished Agent

The operator marks a Finished Box Seen once its result is read: the Box turns grey, moves down
among the Idle Boxes and leaves its group's finished count, until the Agent's next Transition out
of that finish. herdr is never told. Acknowledgements are recorded append-only, survive a Daemon
restart, appear in history, and are written through the first write routes, guarded against
cross-site requests. Decided 2026-10-27 by the operator (vfkb 9aee0fc69560, ADR-0014) from a mock
rendered with live data.
**FRs covered:** FR-5a (new); amends FR-3's order (Seen ranks with Idle), FR-13 (the `agent` event
carries it), FR-14 (history shows acknowledgements), FR-17 (the write guard).

## Epic 1: See every agent's status, live, from the terminal

The operator runs `herddash` and watches every Agent's Status changes stream by as they happen,
each labelled with its Profile, whether or not a terminal client is attached. The project
skeleton, CI, configuration, the herdr client, the version gate, reconnecting forever, Profile
resolution and the four milestone-1 findings land here.
**FRs covered:** FR-7 (resync behaviour), FR-8 (reconnect), FR-10, FR-11, FR-15, FR-16, FR-17 (data directory)

### Epic 2: History that survives restarts

Every Transition is recorded with its provenance. Waiting Times are facts that survive Daemon and
herdr restarts, Retention keeps the database bounded, and the operator's weeks of plugin log are
imported as history.
**FRs covered:** FR-6, FR-7 (markers), FR-8 (history intact), FR-9, FR-9a

### Epic 3: The Board

The operator keeps the Board open all day beside the terminal: one Box per Agent, Needs you and
Finished on top longest-waiting first, live without reload, honest about its connection, served
read-only on loopback, with an API and event stream any later surface can use.
**FRs covered:** FR-1, FR-2, FR-2a, FR-3, FR-4, FR-5, FR-12, FR-13, FR-14, FR-16 (strip), FR-17

### Epic 4: Running unattended on all three machines

herddash starts with the operator's session under launchd or systemd, shuts down cleanly, and a
tagged release produces binaries for every Host. The held release-please pull request is merged
here.
**FRs covered:** FR-15 (service units), NFR8

## Epic 1: See every agent's status, live, from the terminal

The operator runs `herddash` and watches every Agent's Status changes stream by as they happen,
each labelled with its Profile, whether or not a terminal client is attached. Nothing is stored
yet; Epic 2 adds the record.

### Story 1.1: Project skeleton, import rules and CI

As the builder,
I want a Go module laid out as the architecture spine says, with CI enforcing it,
So that every later story lands in the right package and cannot break the rules unnoticed.

**Acceptance Criteria:**

**Given** a fresh clone
**When** `go build ./...`, `go vet ./...`, `go test ./...` and `gofmt -l .` run
**Then** all pass and `gofmt -l .` prints nothing
**And** `go.mod` declares module `github.com/vilosource/herddash` with `go 1.26` and no dependencies yet

**Given** the packages `cmd/herddash` and `internal/{core,engine,herdr,procenv,store,httpapi,importer,config}`
**When** the import-graph test runs
**Then** it fails if any package imports another outside the arrows in the spine's dependency diagram
**And** it fails if `internal/core` imports anything outside the standard library

**Given** a pull request or a push to `develop`
**When** the CI workflow runs
**Then** it runs build, vet, test and gofmt, and cross-compiles darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64 with `CGO_ENABLED=0`
**And** `herddash version` prints the version and commit
**And** `internal/core` defines AgentKey, Status, Agent Program and Transition with its constructor `core.NewTransition`, so later stories share one vocabulary

### Story 1.2: Configuration, data directory and socket discovery

As the operator,
I want one TOML file with sensible defaults that tells herddash where herdr is and what my profiles are called,
So that it runs on my machine with nothing but a Profile Map configured (FR-15, FR-11, AD-8).

**Acceptance Criteria:**

**Given** no config file
**When** herddash starts
**Then** it uses `~/.config/herddash/config.toml` defaults, the data directory `$XDG_DATA_HOME/herddash` (else `~/.local/share/herddash`), loopback bind and Retention of 90 days

**Given** a config file with an invalid value or an unknown key
**When** herddash starts
**Then** it exits non-zero naming the key and the problem, before doing anything else

**Given** a value set both in the file and by a command-line flag
**When** herddash starts
**Then** the flag wins, and the file beats the default

**Given** no socket path configured
**When** herddash resolves the socket
**Then** it follows herdr's order: `--session`, `HERDR_SOCKET_PATH`, `HERDR_SESSION`, then `~/.config/herdr/herdr.sock`
**And** it derives Server as `default` for the default socket and `<name>` for `sessions/<name>/herdr.sock`, and Host as the configured name, else the short hostname (the OS hostname up to its first dot; reworded 2026-10-23, Story 5.8, to match the code)

**Given** herddash starts for the first time
**When** it creates the data directory
**Then** the process umask is `077` and the directory is mode `0700` (FR-17)
**And** the repository ships a documented example config whose Profile Map includes Claude Code's absent-case entry

**Given** the Profile Map in TOML
**When** it is parsed
**Then** each Agent Program entry names a rule type and that rule's parameters (for the environment-variable rule: variable name, value-to-Profile map, absent-case name)
**And** a test adds an environment-variable rule for a new Agent Program through configuration alone, with no code change

### Story 1.3: Milestone-1 findings against a herdr 0.9.5 binary

As the builder,
I want the four open protocol questions answered against a real herdr 0.9.5 server and its schema dumped,
So that the client and the record are built on observed behaviour, not documentation.

**Acceptance Criteria:**

**Given** a herdr 0.9.5 binary run in isolation (its own session and config directory, never the operator's live 0.8.2 server, never installed as a plugin, and never touching `~/.config/herdr/config.toml`)
**When** the investigation runs
**Then** it records with evidence:
  - whether one connection carries one request
  - whether pane ids survive a herdr restart, and what marks a new server instance
  - which call yields a pane's foreground process and which event reports a change
  - which field orders Transitions within an Agent

**And** each finding amends `docs/research/herdr-socket-api.md` in the same change, moving claims from UNVERIFIED to confirmed or corrected

**Given** the 0.9.5 binary
**When** `herdr api schema --json` is dumped
**Then** the dump is committed and the generator tool chosen
**And** wire types are generated into `internal/herdr/gen` by a documented, repeatable command

**Given** a finding that contradicts the spine or an ADR
**When** it is recorded
**Then** the affected `AD` or a superseding ADR is updated in the same change rather than left to disagree

### Story 1.4: herdr socket client mechanics

As the builder,
I want one package that speaks herdr's socket protocol and nothing more,
So that every other part of herddash reaches herdr the same safe way (AD-5, AD-11, ADR-0006).

**Acceptance Criteria:**

**Given** a socket path
**When** the client calls ping or snapshot
**Then** it dials a fresh connection per request and closes it after the response

**Given** a subscription request for the lifecycle kinds or one pane's `pane.agent_status_changed`
**When** it is opened
**Then** it holds one long-lived connection, waits for `subscription_started`, and delivers decoded events from both the underscored lifecycle envelope and the dotted pane-scoped envelope as core types

**Given** a subscription that receives `events_lost`, an unknown error code, a rejected subscribe or a closed connection
**When** it happens
**Then** the client reports it as a stream-health signal and makes no decision itself

**Given** the package's exported API
**When** it is inspected
**Then** it has no focus method and no pane-output method
**And** a test asserts that no code in `internal/herdr` outside `gen/` constructs a focus or pane-read request, even though the generated wire types describe them

**Given** the call and event Story 1.3 identified for a pane's foreground process
**When** the client is asked for a pane's foreground process id, or a change is pushed
**Then** it returns the pid, or reports the change as an event, as core types

**And** all tests run against a fake socket server, never a live herdr

### Story 1.5: Watch every Agent's Status live

As the operator,
I want `herddash` to connect to herdr and print every Status change as it happens,
So that I can see agents finish or block even with no terminal client attached.

**Acceptance Criteria:**

**Given** herdr is running with Agents
**When** `herddash serve` starts
**Then** the engine subscribes to the lifecycle kinds first, then snapshots, then opens one status subscription per Agent pane
**And** it prints one line per Agent with Host, pane, Agent Program and Status
**And** for each Agent it tracks, from the snapshot and events, the project directory, the workspace and tab labels, the workspace's position in herdr's order, and the Pane Title

**Given** a new pane detected as an Agent
**When** herdr reports it
**Then** the engine subscribes to its status before reading its state, and prints it

**Given** an Agent changes Status
**When** the event arrives
**Then** the engine prints the Transition within one second, marked as observed live

**Given** a pane that closes between snapshot and subscribe
**When** its subscription is rejected
**Then** only that pane's subscription fails, and the engine refreshes the pane set without disturbing the others

**Given** a pane closes, or a pane stops being an Agent
**When** herdr reports it
**Then** the engine closes its subscription, drops it from the pane set and prints its removal

**And** the engine is the only goroutine holding the pane set and current Status (AD-2)

### Story 1.6: Reconnect forever, and know when herdr restarted

As the operator,
I want herddash to wait out herdr being down or restarted and pick up again by itself,
So that I never restart herddash to trust it (FR-8, AD-3, AD-11).

**Acceptance Criteria:**

**Given** herdr is not running at start, or stops later
**When** the engine cannot connect
**Then** it retries forever with capped backoff and logs it as normal operation, not an error
**And** it reconnects, re-subscribes and snapshots when herdr returns

**Given** herdr restarts
**When** the engine reconnects
**Then** it changes the server epoch only on the evidence Story 1.3 identified, so unchanged Agents keep their identity
**And** it prints a reconnect line, and nothing for Agents whose Status is unchanged

### Story 1.7: Resync when the stream cannot be trusted

As the operator,
I want herddash to rebuild its picture from herdr whenever it may have missed something,
So that what it shows is never silently wrong (FR-7, AD-11).

**Acceptance Criteria:**

**Given** a stream-health signal (`events_lost`, unknown error code or dropped subscription)
**When** the engine receives it
**Then** it resyncs: resubscribes, snapshots on a separate connection, replaces its state, and prints any disagreement as a snapshot-derived change
**And** at most one resync runs at a time, and resyncs are no closer together than the minimum interval

**Given** events arriving while a snapshot read is in flight
**When** the read completes
**Then** the engine reads again rather than replaying the buffered events onto the snapshot

**Given** a reconcile interval elapses
**When** the timer fires
**Then** the engine snapshots and prints any disagreement as a snapshot-derived change

**And** the engine is tested against a fake herdr client that injects each signal

### Story 1.8: Refuse unsupported herdr, with a dogfooding override

As the operator,
I want herddash to refuse herdr older than 0.9.5 unless I deliberately allow it,
So that I am never shown a Board built on versions that under-report `blocked` (FR-16).

**Acceptance Criteria:**

**Given** herdr 0.9.5 or newer
**When** the engine connects
**Then** the gate is `ok` and Transitions are printed

**Given** herdr older than 0.9.5 and the dogfooding flag off
**When** the engine connects
**Then** the gate is `refused`, nothing is printed as a Transition, and the log names the version and the minimum

**Given** herdr 0.8.2 and the dogfooding flag on
**When** the engine connects
**Then** the gate is `advisory`, Transitions are printed with the herdr version, which the engine keeps for Epic 2 to record, and the log warns that the resync guarantee does not hold below 0.9.0

**And** the gate reads herdr's version from `ping`, never from the `protocol` integer

### Story 1.9: Know which profile each agent runs under

As the operator,
I want every Agent labelled with the profile it was launched under,
So that two `claude` panes on different subscriptions are told apart (FR-10, FR-11, AD-14).

**Acceptance Criteria:**

**Given** a Claude Code pane whose foreground process has `CLAUDE_CONFIG_DIR=~/.claude-cldw` and a Profile Map entry naming it `cldw`
**When** the engine first sees the Agent
**Then** it resolves Profile `cldw` before printing anything for it

**Given** a Claude Code pane with `CLAUDE_CONFIG_DIR` unset
**When** it is resolved
**Then** it takes the Profile Map's absent-case name for Claude Code

**Given** the variable is set but matches no entry, or resolution exceeds its timeout
**When** it is resolved
**Then** the Profile is `unresolved`

**Given** a Codex or Antigravity Agent with no rule
**When** it is resolved
**Then** the Profile is its Agent Program name, and the heading shows that name once

**Given** a pane's foreground process changes, or herdr detects a new Agent in it
**When** the engine is told
**Then** it re-resolves the Profile

**And** environment reading works on linux (`/proc/<pid>/environ`) and darwin (`kern.procargs2` via `golang.org/x/sys`), with the darwin reader built only for darwin
**And** no environment value is logged except a matched `CLAUDE_CONFIG_DIR`

## Epic 2: History that survives restarts

Every Transition is recorded with its provenance. Waiting Times are facts that survive Daemon and
herdr restarts, Retention keeps the database bounded, and the plugin's log becomes history.

### Story 2.1: A local database herddash owns

As the operator,
I want a single owner-only database that only one herddash process can open,
So that the record is private and can never be written by two processes at once (AD-9, AD-10, ADR-0005).

**Acceptance Criteria:**

**Given** a fresh data directory
**When** `store.Open` runs
**Then** it takes an exclusive `flock`, enables WAL, applies the numbered migrations for the transitions and resync-marker tables in one transaction, and records the schema version
**And** the transitions table carries every field ADR-0005 lists, including the nullable unique `import_key` and the possibly-spurious flag, with the herdr ordering column named from Story 1.3's finding; any later change to it needs an ADR first
**And** the database, WAL, SHM, lock and log files are mode `0600`

**Given** a second process on the same data directory
**When** it calls `store.Open`
**Then** it fails at once naming the lock, and a crashed process's lock is released by the OS

**Given** a database whose schema version is newer than the binary
**When** `store.Open` runs
**Then** it refuses and says so

**And** the store is tested against temporary files only

### Story 2.2: Record every Transition

As the operator,
I want every Status change written the moment it is observed,
So that what happened is kept even when nothing is watching (FR-6, FR-11, AD-4, AD-13).

**Acceptance Criteria:**

**Given** a live Status event or a snapshot disagreement
**When** the engine records it
**Then** it is built by `core.NewTransition` and written by `store.Append` with Host, Server, epoch, pane id, Agent Program, Profile, previous and new Status, herdr ordering, herdr version, provenance and `observed_at`
**And** unknown values are stored as NULL, never as the string `unknown`

**Given** herddash starts
**When** the store opens
**Then** a resync marker with reason `daemon_start` is written

**Given** two Transitions on one Agent whose `observed_at` order disagrees with herdr's ordering
**When** they are read back
**Then** they come in herdr's order, and in `observed_at` order only where herdr's ordering is unknown

**Given** a Profile Map edit followed by a restart
**When** affected Agents next change Status
**Then** new Transitions carry the new Profile, and Transitions recorded before the edit keep the Profile they were recorded with

**And** only the engine and the importer call `store.Append`, which a call-site test enforces

### Story 2.3: Gaps are recorded, not hidden

As the operator,
I want every resync written into the record with its reason,
So that a period herddash could not see is visible as a fact (FR-7).

**Acceptance Criteria:**

**Given** a resync triggered by `events_lost`, an unknown error code, a rejected subscribe, a reconnect or the reconcile timer finding a disagreement
**When** it completes
**Then** a resync marker with Host, Server, epoch, time and reason is in the record
**And** every Agent's Status in the record is consistent with the snapshot that ended the resync

**Given** herddash started while herdr was down
**When** herdr returns
**Then** every Agent has a snapshot-derived Transition and a marker records the gap

### Story 2.4: Waiting Time that survives restarts

As the operator,
I want each Agent's Attention Class and Waiting Time computed from the record, exact only when it truly is,
So that "waiting 40 minutes" means 40 minutes even after a restart (FR-2, FR-3, FR-8, AD-1, AD-12).

**Acceptance Criteria:**

**Given** an Agent's stored Transitions
**When** `core.Fold` runs
**Then** it returns the instant the current Attention Class began, that Transition's provenance, and whether the Agent was ever `working`
**And** `done` becoming `idle` does not move the start instant

**Given** the class began with a live Transition and the Status seen on reconnect is unchanged
**When** herddash restarts
**Then** the Waiting Time is exact and equals the value before the restart

**Given** the start was not observed live, or the Status changed while herddash was down
**When** the Waiting Time is computed
**Then** it is a lower bound from the earliest evidence (last recorded Transition, else Daemon start), flagged not exact

**Given** an `idle` Agent never recorded `working`
**When** its Attention Class is computed
**Then** it is Working, not Finished

**Given** herdr restarts and an Agent's Status is unchanged
**When** herddash reconnects
**Then** no Transition is recorded for it, only the resync marker

**And** the terminal output shows Attention Class and Waiting Time, with `≥` for lower bounds
**And** core is table-tested with an injected clock and no I/O

### Story 2.5: Retention keeps the database bounded

As the operator,
I want old history deleted on a schedule without losing what current Boxes depend on,
So that the database stays small and the Board stays correct (FR-9, AD-4).

**Acceptance Criteria:**

**Given** Retention of N days (default 90)
**When** the sweep runs
**Then** no Transition older than N days remains, except for each Agent still present, every Transition from the one that began its current Attention Class onward and its latest `working` Transition

**Given** an Agent no longer present
**When** the sweep runs
**Then** its Transitions are removed purely by age

**And** the sweep runs on an engine timer, is the only delete in the codebase, and never runs during a resync

### Story 2.6: Import the plugin's log as history

As the operator,
I want the weeks of transitions my existing herdr plugin logged imported once,
So that herddash starts with the only history that exists (FR-9a, AD-13).

**Acceptance Criteria:**

**Given** the plugin log on the operator's machine
**When** the story starts
**Then** its actual format is documented from a sample in the importer's tests, with Profile values anonymised where needed

**Given** `herddash import-plugin-log <file> --herdr-0.9.2-since <date>` with no Daemon running
**When** it runs
**Then** each entry becomes a Transition with provenance `backfill`, epoch 0, the Profile the log recorded, NULL previous Status and NULL herdr ordering
**And** `done` entries before the given date are flagged possibly spurious, and entries older than Retention are skipped
**And** each row's Host and Server equal what `serve` resolves from the same configuration

**Given** the same import run twice
**When** the second run completes
**Then** it adds nothing, because each imported row carries a unique `import_key`

**Given** a Daemon holding the data directory's lock
**When** the import starts
**Then** it refuses, naming the running Daemon

**And** imported rows never appear as a live Agent and never set a live Agent's Waiting Time

## Epic 3: The Board

The operator keeps the Board open all day beside the terminal, served read-only on loopback from
the record, with an API and event stream any later surface can use.

### Story 3.1: Each Agent's view: Context Line and order

As the operator,
I want each Agent described once, in core, with its Context Line and its place on the Board,
So that the API, the Board and any later surface agree exactly (FR-2a, FR-3, AD-1).

**Acceptance Criteria:**

**Given** a Claude Code Agent whose Pane Title is a session name
**When** its AgentView is built
**Then** the Context Line is that name

**Given** a Claude Code Pane Title of `Claude Code` or empty, or an Agent Program with no rule
**When** its AgentView is built
**Then** the Context Line is the empty global default

**Given** Agents across several workspaces and every Attention Class
**When** their sort keys `[zone, group, class_rank, start, agent_key]` are compared
**Then** Needs you then Finished come first, flat and longest-waiting first, followed by Working then Unknown, grouped by workspace in herdr's snapshot order
**And** the expected order and the positions of workspace headers are committed as a golden fixture that later Board tests reuse

**And** AgentView carries AgentKey, Profile, Agent Program, the heading (the name once when Profile equals Agent Program), the shortened directory, the workspace and tab labels, Status, Attention Class, `waiting_since`, `waiting_exact`, the Pane Title, the Context Line and the sort key
**And** adding a Context Line rule for a new Agent Program touches only `internal/core` and its documentation

### Story 3.2: The engine publishes views and change lists

As the builder,
I want the engine to publish an immutable view with the changes that produced it,
So that the API and the stream read one consistent picture without locking engine state (AD-2, AD-7).

**Acceptance Criteria:**

**Given** any change to an Agent, a recorded Transition, a resync or a connection change
**When** the engine applies it
**Then** it publishes a new immutable view (every AgentView, ConnectionTruth, a view sequence one higher than the last, and the run id) together with the change list that produced it

**Given** a reader holding a view
**When** the engine publishes newer ones
**Then** the reader's view never changes under it

**And** the run id is new on every Daemon start, and the sequence starts again with it

**Given** the connection to herdr is lost, a snapshot completes, or the gate is decided
**When** the engine publishes
**Then** ConnectionTruth reflects it: `connected`, `last_snapshot_at`, `herdr_version`, `herdr_minimum`, `gate` and `resync_guaranteed`

### Story 3.3: Read-only API on loopback with current state

As the operator,
I want a local, read-only `/api/state` that returns every Agent already ordered,
So that the Board and any later surface show the same truth (FR-12, FR-17, AD-1, AD-6, ADR-0007).

**Acceptance Criteria:**

**Given** default configuration
**When** herddash serves
**Then** it listens on loopback on a default port other than 7317, and a non-loopback bind is refused at start

**Given** a request whose `Host` header is not `localhost`, `127.0.0.1` or `[::1]` (any port)
**When** it arrives
**Then** it is rejected with the JSON error shape

**Given** a POST, PUT or DELETE, or an unknown path
**When** it arrives
**Then** the response is 405 or 404 in the JSON error shape, and no CORS headers are ever sent

**Given** Agents in the engine's published view
**When** `GET /api/state` is called
**Then** it returns every `AgentView` from Story 3.1 ordered by sort key, with ConnectionTruth, the run id and the view sequence

**And** serving the response makes no herdr call

### Story 3.4: Live event stream

As the operator,
I want a stream that starts with the whole state and then pushes each change,
So that the Board updates within a second and can never drift (FR-4, FR-13, AD-7).

**Acceptance Criteria:**

**Given** a client opening `GET /events`
**When** the stream starts
**Then** the first event is `state` with exactly the `/api/state` body plus run id and view sequence

**Given** the engine publishes a view with a change list
**When** a client is connected
**Then** it receives `agent`, `removed`, `transition`, `resync` and `connection` events in publish order within one second, each with `id:` set to the view sequence
**And** an `agent` body is a full AgentView, `removed` carries the AgentKey, `transition` carries the full Transition and its AgentKey, `resync` the marker, and `connection` the full ConnectionTruth, so one Box updates with no second request

**Given** a client too slow to drain its bounded buffer
**When** the buffer overflows
**Then** the server closes that stream only, and a reconnect starts again with `state`

**And** the server keeps no event history and handles no `Last-Event-ID`, and the write deadline is cleared per stream

### Story 3.5: History per Agent

As the operator,
I want one Agent's recorded Transitions on request,
So that a later history page or tool can show what it did (FR-14).

**Acceptance Criteria:**

**Given** an Agent key
**When** `GET /api/agents/{host}/{server}/{epoch}/{pane}/history` is called
**Then** it returns that Agent's Transitions within Retention in order, including the one that began its current Attention Class, each with provenance

**Given** an unknown key
**When** it is requested
**Then** the response is 404 in the JSON error shape

**And** the handler reads only through `store`'s read functions and never calls `store.Append`

### Story 3.6: Board visual design

As the operator,
I want the Board designed against my real agents before it is built,
So that Needs you stands out across the desk and nothing else competes for attention (FR-3, NFR10, NFR14).

**Acceptance Criteria:**

**Given** a golden fixture of AgentViews drawn from a real `/api/state` response (anonymised), covering every Attention Class, several workspaces, lower-bound and exact times, long and empty Context Lines, and `unresolved`
**When** the design is produced
**Then** it defines the palette, type and spacing tokens, the Box anatomy, the emphasis for Needs you, Finished, Working and Unknown, the workspace header, and the connection and version strips, in light and dark

**And** it is delivered as a short design note plus a static HTML page rendering the fixture
**And** the operator's approval is a gate recorded in sprint status before Story 3.7 starts, not something this story's developer can satisfy
**And** it adds nothing to the Board beyond Boxes, workspace labels and strips (SM-C1)

### Story 3.7: The Board renders every Agent

As the operator,
I want the Board to show every Agent as a Box in the right place,
So that one glance tells me which agent needs me and for how long (FR-1, FR-2, FR-3, AD-1).

**Acceptance Criteria:**

**Given** the Board at `/` with assets under `/assets/`
**When** it receives a `state` event
**Then** it renders exactly one Box per AgentView and none for any other pane

**Given** an Agent's Box
**When** it renders
**Then** it shows the AgentView's heading, the shortened directory, the workspace and tab labels, the Attention Class, the Waiting Time ticking from `waiting_since` with `≥` when not exact, and the Context Line on one line, truncated with an ellipsis, with full text on hover and space reserved when empty
**And** Box height is fixed in CSS, so no content changes it

**Given** the fixture's `unresolved` Agent and two `claude` Agents under different Profiles
**When** they render
**Then** the first shows `unresolved`, never a blank, and the other two show different Profile labels

**Given** the golden fixture from Story 3.1
**When** the Board's ordering module orders it
**Then** the order and workspace headers equal core's, checked in CI by `node --test` against the same fixture, with no npm packages

**And** the ordering and formatting logic lives in a plain ES module with no DOM access, and the Board computes nothing except the ticking clock, the order by sort key and workspace headers

### Story 3.8: The Board stays live

As the operator,
I want the Board to update itself as agents change,
So that I never reload it and it never drifts (FR-1, FR-3, FR-4, AD-7).

**Acceptance Criteria:**

**Given** the Board loaded
**When** it starts
**Then** it opens `/events` first and fetches nothing else, with no polling

**Given** an Agent appears, changes or disappears in herdr
**When** its event arrives
**Then** its Box is added, moved or removed without a reload, a Transition into `blocked` moves it into Needs you at its longest-waiting position (a newly blocked Agent sorts last there, as the operator confirmed on 2026-10-20), a Transition into `working` moves it out of the top zone into its workspace group, and a `done` Agent turning `idle` keeps its place and its Waiting Time

**Given** the stream drops or the Daemon restarts
**When** the Board notices
**Then** it reconnects by itself and re-renders from the new `state`

**And** event handling is unit-tested in the same ES module tests, and the one-second budget from recording to delivery is checked in a Go test of the stream

### Story 3.9: Connection and version strips

As the operator,
I want the Board to tell me plainly when it cannot see herdr or herdr is unsupported,
So that I never trust a stale Board (FR-5, FR-16).

**Acceptance Criteria:**

**Given** ConnectionTruth with `connected: false`
**When** the Board renders
**Then** a thin strip says it is disconnected from herdr and gives the last snapshot time, and Boxes are dimmed, never hidden
**And** after herdr stops, the strip appears within the Daemon's reconnect interval

**Given** herdr returns
**When** the next `state` or `connection` event arrives
**Then** the strip clears and Boxes refresh

**Given** gate `refused`
**When** the Board renders
**Then** an unsupported-version strip names `herdr_version` and `herdr_minimum` from ConnectionTruth, and no Boxes are shown

**Given** gate `advisory`
**When** the Board renders
**Then** the strip is a warning that the version is unsupported and resync is not guaranteed, and Boxes are shown

## Epic 4: Running unattended on all three machines

herddash starts with the operator's session, shuts down cleanly, and each tagged release produces
binaries for every Host.

### Story 4.1: Clean shutdown

As the operator,
I want herddash to stop cleanly when told,
So that a restart or logout never corrupts the record.

**Acceptance Criteria:**

**Given** herddash serving with open streams
**When** it receives SIGINT or SIGTERM
**Then** it stops accepting requests, closes streams, lets the engine finish its current write, closes the database and releases the lock, then exits zero within a few seconds

**And** a restart immediately afterwards starts cleanly with a `daemon_start` marker

### Story 4.2: Start with my session on macOS and Linux

As the operator,
I want herddash to run under launchd on macOS and systemd on Linux,
So that the Board is there whenever I log in (FR-15).

**Acceptance Criteria:**

**Given** the shipped launchd agent and systemd user unit
**When** they start herddash as the operator's user
**Then** it finds the herdr socket without `HERDR_SOCKET_PATH`, and logs to the data directory
**And** it restarts on failure, but not in a tight loop

**And** installation and removal are documented step by step for both, without root, and without touching herdr's configuration or plugin registry

### Story 4.3: Release binaries for every Host

As the operator,
I want each release to publish ready-to-run binaries,
So that the second and third machines install herddash without building it (NFR8).

**Acceptance Criteria:**

**Given** release-please creates a tag
**When** the release workflow runs
**Then** it builds darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64 with `CGO_ENABLED=0` and the version stamped in, and attaches them with SHA-256 checksums to the GitHub release

**Given** Epics 1 to 3 are done and herddash has run on the operator's machine for at least a day
**When** the held release-please pull request is reviewed
**Then** its proposed version is checked to be 0.x before it is merged, and the operator is asked before merging

## Epic 5: After 0.5.0: closing what dogfooding and reviews left open

Every item comes from `docs/implementation/deferred-work.md` (audited 2026-10-23: 9 of its 24
entries were already resolved), from the operator's planning answers, or from PRD §8. The operator
decided these in advance, so no story asks them again:

- Stories are built and installed locally as `-dev`. When the epic is done the operator is asked
  once, and on their OK everything ships as one pull request and one release.
- A pane herdr moves stays a removal plus a new Agent. AD-3 is unchanged and gets no ADR; the
  behaviour is documented.
- ADR-0011 (Story 5.5) is accepted in advance, provided it only tightens the import rules.
- PRD §8 Q1 is decided by the record, not by the operator, unless the record shows a problem.
  *Superseded 2026-10-27:* the operator decided it directly (ADR-0014); Story 5.9 is superseded
  by Epic 6.
- Background-task experiments may run throwaway Claude Code sessions under `cldp`, outside herdr,
  in a scratch directory.

### Story 5.1: Retrospectives for Epics 1 to 4

As the builder,
I want each finished epic reviewed against the evidence it left behind,
So that lessons and any missed work reach this epic before it ships.

**Acceptance Criteria:**

**Given** Epics 1 to 4 are done
**When** `/bmad-retrospective` runs headless for each
**Then** each writes its retrospective under `docs/implementation/` with sourced findings and action items
**And** any action item that is code or documentation work is added to this epic as a story, or to `deferred-work.md` with a reason
**And** any action item that needs an operator decision not already taken is collected for one question at the end of the epic

### Story 5.2: A resumed session does not keep finished background tasks

As the operator,
I want a Box to leave BACKGROUND when its background work is really over, even after a session was resumed,
So that a Box never stays BACKGROUND because of a task that ended in an earlier process.

**Acceptance Criteria:**

**Given** a throwaway Claude Code session under `cldp`, outside herdr, exits with a background command still running, and is then resumed
**When** the new transcript is read
**Then** the experiment's finding (whether earlier task starts are copied into the new transcript) is recorded in `docs/research/` with the Claude Code version and date
**And** if starts are copied, the engine drops an open command when the process check has found no tool shell for a bounded time, covered by a test
**And** if they are not, the deferred entry is closed with the evidence and no code changes

### Story 5.3: `/clear` drops the previous session's background tasks

As the operator,
I want a cleared session's Box to show only the new session's work,
So that tasks from before `/clear` never linger on the Board.

**Acceptance Criteria:**

**Given** a throwaway Claude Code session under `cldp` with a background task open
**When** `/clear` is run
**Then** whether `sessions/<pid>.json` changes `sessionId` is recorded in `docs/research/` with the version and date
**And** when the session id changes, the engine resets the transcript tail and open tasks for that Agent, covered by a test
**And** if the id does not change, the story finds and documents another signal, or records that none exists and keeps the deferred entry with that evidence

### Story 5.4: Retry a Profile lookup that failed transiently

As the operator,
I want a Profile that failed to resolve because of a timeout or a herdr call error to be tried again,
So that an Agent does not stay `unresolved` until its next agent detection.

**Acceptance Criteria:**

**Given** Profile resolution fails with a timeout or call error (not a rule outcome such as absent or unmatched)
**When** the engine handles the failure
**Then** it retries with a bounded number of attempts and a backoff from `tunables.go`, never on the engine goroutine's critical path
**And** a rule outcome (`unresolved` from an unmatched variable, the absent case) is never retried
**And** `TestProfileFailures` and a new test prove both
**And** when an Agent's Agent Program changes, its Profile is resolved again before the next row is written, so no row pairs the new program with the old Profile (Epic 1 retro #1), and `agent_detected` for a new pane records its program (#4, test first)
**And** a reconnect that keeps the epoch re-resolves Profiles, or a test documents why it need not (#5), and every Profile Map value is cleaned before the duplicate check (#6)

### Story 5.5: Tighter import rules and Transition invariants (ADR-0011)

As the builder,
I want the architecture test to keep herdr's wire types and dependencies where the spine intends,
So that drift is caught by CI rather than by review.

**Acceptance Criteria:**

**Given** ADR-0011, which only tightens the import rules
**When** `arch_test.go` runs
**Then** it fails on any import of `internal/herdr/gen` or `genwire` from outside `internal/herdr`, and on any non-standard import in `internal/herdr` and its gen packages
**And** `core.NewTransition` rejects a `HerdrOrder` on a live or backfilled Transition with a sentinel error, covered by a test

### Story 5.6: CI runs the Board's browser check

As the builder,
I want CI to run `internal/httpapi/web/check/board-browser.mjs` against headless Chrome,
So that the Board's drawing (strips, dimming, recap rows, live updates, reconnect) cannot break with CI green.

**Acceptance Criteria:**

**Given** a pull request
**When** CI runs on ubuntu-latest
**Then** a job runs the browser check against the runner's Chrome, with no npm packages
**And** removing the `.board--recaps` rule makes that job fail
**And** CLAUDE.md no longer calls the check hand-run

### Story 5.7: CI tests on macOS

As the builder,
I want the test suite to run on a macOS runner too,
So that procenv's darwin reader and the launchd plist lint are exercised automatically.

**Acceptance Criteria:**

**Given** a pull request
**When** CI runs
**Then** a `macos-latest` job runs `go test ./...` and `TestLaunchdAgentLints` does not skip there
**And** the existing ubuntu jobs are unchanged
**And** CI vets, tests and runs `scripts/release-build.sh` under the release toolchain (Go 1.27.2) as well as go.mod's minimum (Epic 4 retro #1)
**And** `TestReleaseBuild` asserts each binary carries the commit and a clean-tree build setting (#2)
**And** on ubuntu, `TestSystemdUnitVerifies` fails rather than skips when `systemd-analyze` is missing (#3)

### Story 5.8: Documents agents read match the code

As the builder,
I want CLAUDE.md, the spine and the configuration docs to state what the code does,
So that a fresh session is not misled.

**Acceptance Criteria:**

**Given** the open documentation items in `deferred-work.md`
**When** this story is done
**Then** CLAUDE.md says what `go run` and a plain `go build` print for `version`, marks the Story 1.3 checkpoint closed without "live 0.8.2", names `golang.org/x/sys` and the exact `go` directive, and describes Epic 5's additions
**And** the engine tunables are documented beside the configuration, and the spine's Deferred list no longer names them
**And** the spine's AD-3 notes that a moved pane is a removal plus a new Agent, and `TestPaneMoved`'s comment agrees
**And** every closed entry in `deferred-work.md` is marked resolved with where
**And** the retrospectives' documentation items are applied: the spine's AD-3 says the epoch and terminal map are persisted (`server_state`) as engine state outside the append-only record (and ADR-0005 agrees), the Deferred list drops the milestone-2 column seed, "OS hostname" becomes the short hostname, the PRD glossary and spine describe the v4 layout instead of a flat Finished top zone, the spine's JSON omission rule matches the empty strings the API sends and names the packages owning wire shapes, and `docs/install.md`'s shell blocks stop on a failed checksum, an empty database path and wait for launchd to stop
**And** the retrospectives' low-severity items are added to `deferred-work.md` with their reasons, and the process lessons (copy a deferral into its target story's criteria; give every spine Open item an owner; run new concurrency tests with `-race -count=50`; test record-derived state across a Daemon restart; force rollback tests to fail mid-transaction) are recorded where agents read them

### Story 5.9: Decide whether acknowledgement is needed (PRD §8 Q1)

**Superseded by Epic 6 (operator decision 2026-10-27).** The operator decided PRD §8 Q1 directly,
without waiting for the record: acknowledgement is needed, and its design was approved from a mock
rendered with live data (vfkb 9aee0fc69560, ADR-0014). The story is closed without running; its
criteria below are kept as they were written.

As the operator,
I want the acknowledgement question settled by what the record shows,
So that the Board gets an acknowledgement feature only if it needs one.

**Acceptance Criteria:**

**Given** the date is 2026-11-03 or later
**When** the record is queried for Agents that stayed Finished after the operator viewed the pane (a `done` never followed by `idle` while the operator was active, or `idle` and `done` disagreeing)
**Then** if there are none, PRD §8 Q1 is closed as "not needed" with the query and its result
**And** if there are some, the evidence is prepared for the operator and the story stops for their decision
**And** before that date the story is skipped, not failed

### Story 5.10: A new Agent in a reused pane starts fresh

As the operator,
I want a Claude session started again in the same pane to start with its own Waiting Time,
So that it never shows as Finished with the previous session's wait after a Daemon restart.

Source: Epic 2 retrospective F-1 and Epic 1 retrospective #19. Decided by the orchestrator under the
operator's delegation (no schema change, no ADR): the record's key stays host, server, epoch and
pane id; the Waiting Time fold starts over at an Agent's first row (a row with no previous Status)
and when the saved `terminal_id` for the pane changes; AD-3 documents that one AgentKey can hold
successive Agents separated by such a row.

**Acceptance Criteria:**

**Given** an Agent is quit and a new one starts in the same pane, and the Daemon then restarts keeping the epoch
**When** the Daemon rebuilds Waiting Times from the record
**Then** the new Agent's Attention Class and Waiting Time come only from rows since its first row
**And** the same holds when a pane id comes back on a new terminal while the Daemon was down
**And** restart and Retention-sweep tests cover both, with the test fake matching the store's behaviour
**And** history for the Agent includes the Transition that began its current class (Epic 3 retro)

### Story 5.11: The Board notices a hung Daemon

As the operator,
I want the Board to dim and say so when the Daemon stops sending, even if the connection stays open,
So that I never act on stale Boxes that look live.

**Acceptance Criteria:**

**Given** the Daemon's event stream stays open but sends nothing, not even its keepalive, for longer than the keepalive interval allows
**When** the Board checks the stream
**Then** it treats the Daemon as lost (strip and dimming as for a dropped stream) and reconnects
**And** the keepalive is something the page can observe, a skipped sequence number or a rejected `state` event triggers a fresh `/api/state` load, covered by `node --test`
**And** the version strip says the version could not be read, rather than "below the supported minimum", when herdr's version is unknown
**And** a Go test drives the SSE handler from the real engine and replays its publications through `applyEvent`, asserting the Board model equals the published view

### Story 5.12: The engine treats repeated failures as a lost connection

As the operator,
I want the connection strip to say so when the Daemon can no longer get snapshots from herdr,
So that a herdr that answers but cannot snapshot does not leave the Board looking current.

**Acceptance Criteria:**

**Given** snapshots keep failing after startup
**When** the failures pass a bound set in `tunables.go`
**Then** ConnectionTruth reports the connection lost and the Board shows it, and recovery clears it
**And** a well-formed but unexpected lifecycle event leads to a resnapshot, not a full reconnect, covered by a test

### Story 5.13: Board text and grouping are exact

As the operator,
I want each Box's text and workspace group to show exactly what herdr and Claude Code report,
So that names are not mangled and groups are not merged.

**Acceptance Criteria:**

**Given** a Context Line or session name containing bidi control characters, or starting with `-`, `#` or an emoji
**When** it is shown on the Board
**Then** bidi controls are neutralised and the text is isolated, and only Claude Code's own glyph prefix is stripped
**And** workspaces with no known position each get their own group with their own name, ordered after positioned ones


### Story 5.14: A manual `/recap` reaches the Board, and local commands are not turns

As the operator,
I want the recap I ask for with `/recap` to show on the Box, and commands such as `/usage` to leave the Box alone,
So that the Board shows the latest recap however it was made.

Source: operator report, 2026-10-23. Running `/recap` in a pane did not update its Box. Evidence
from this machine's transcripts (Claude Code 2.1.292): a manual `/recap` writes a `user` record whose
content starts `<command-name>/recap</command-name>` (preceded by an `isMeta` user record holding
`<local-command-caveat>`), then a `system` record with subtype `local_command`,
`commandRun: {"command":"recap","args":""}` and content `<local-command-stdout>…</local-command-stdout>`.
The decoder reads only `away_summary`, and both user records count as a new turn, which clears the
recap and resets the turn state. A skill command such as `/vfkb:handoff` writes a user record that
starts `<command-message>` and is followed by an `isMeta` user record and an assistant turn, so it
is a real turn. Seen across transcripts: `/recap` 63, `/usage` 53, `/compact` 42, `/model` 20, `/rename` 12.

**Acceptance Criteria:**

**Given** recaps are on and the operator runs `/recap`
**When** the `local_command` record with `commandRun.command` "recap" is read
**Then** its stdout text (tags removed, same trimming and cap as `away_summary`) becomes the Box's recap within the Board's usual latency
**And** a local command's own records (the caveat, the `<command-name>` user record and its `local_command` result) neither clear the recap nor mark a turn as started
**And** a skill command that does start a model turn still clears the recap as a turn does
**And** decoder and fold tests use record shapes copied from real transcripts, with no transcript text beyond what the test needs
**And** with recaps off, neither record's content is read (ADR-0010 consent)

### Story 5.15: Bash-mode commands are not turns either

As the operator,
I want a `!command` I run in Claude Code's bash mode to leave the Box's recap and turn state alone,
So that the Board treats every local action the same way.

Source: Story 5.14's deferred finding, confirmed by the orchestrator on 2026-10-23. This machine's
transcripts hold 56 user records whose content starts `<bash-input>` and 56 starting
`<bash-stdout>`; both count as a turn start today. `<local-command-stderr>` was also left unverified.

**Acceptance Criteria:**

**Given** the operator runs `!cmd` in bash mode, or a local command writes `<local-command-stderr>`
**When** those records are read
**Then** they neither clear the recap nor mark a turn as started, judged by structure only and verified against real record shapes
**And** a prompt typed after them still starts a turn
**And** Story 5.14's code gets the follow-up review its builder recommended, and its findings are fixed or deferred

**Amended 2026-10-23 (built):** the verification this story asks for refuted its premise for bash mode.
Claude Code sends a `!cmd`'s output to the model, which answers it: 54 of the 56 bash commands in
this machine's transcripts were answered by a model turn with no prompt between
(`docs/research/claude-code-bash-mode-2026-10-23.md`). Bash-mode records therefore stay turn starts
and clear the recap, now pinned by tests; only the `<local-command-stderr>` half was changed. The
title is kept for traceability. See `docs/implementation/spec-5-15-bash-mode-commands-are-not-turns-either.md`.

### Story 5.16: A resumed subagent counts as background work

As the operator,
I want a subagent that Claude Code resumes to count as background work on its Box,
So that a pane whose agent is waiting on resumed subagents does not look less busy than it is.

Source: operator report, 2026-10-23. The orchestrator resumed two subagents with SendMessage; the
transcript recorded a user record whose `toolUseResult` is
`{"success":true,"message":"Resuming agent …","resumedAgentId":"<id>",…}`, which ADR-0009's decoder
does not treat as a task start, so the Box counted 1 background task while 3 ran. The earlier
"Finished while waiting" observation was correct: both subagents had sent completed notifications and
nothing was running.

**Acceptance Criteria:**

**Given** a transcript where a `toolUseResult` with `success` true and a non-empty `resumedAgentId` appears
**When** it is read
**Then** that id is an open task until a notification for it closes it (completed, failed, killed or stopped), exactly like a launched subagent
**And** a failed resume (`success` false or no id) opens nothing
**And** tests use the record shape observed on Claude Code 2.1.292 with placeholder text, and ADR-0009 gets a dated amendment

### Story 5.17: A released pane's next Agent starts fresh across a restart

As the operator,
I want a Claude session I start in a pane whose previous Agent had quit to start fresh, even if the Daemon was down when it started,
So that it never shows as Finished with the previous Agent's wait.

Source: independent review of Story 5.10, 2026-10-23, reproduced by a failing test. Agent A goes
working→done live, herdr sends `agent_released` (the engine removes the pane, writes no row), the
Daemon stops or loses herdr, B starts in the same pane and terminal, and the Daemon comes back keeping
the epoch: B is seeded from A's history and shows Finished with A's wait. Decided by the orchestrator
under the operator's delegation (extends vfkb aa3dcbf096bc; no stored-schema change, no ADR): the
release is recorded in the engine's saved state outside the append-only record (`server_state`'s
pane-to-terminal map, or an equivalent there), so a pane released before the restart is not seeded
and B's first sight is written with no previous Status.

**Acceptance Criteria:**

**Given** the reviewer's scenario, both for a Daemon restart and for an in-run reconnect where B appears during the disconnect
**When** the Daemon sees B
**Then** B's first row has no previous Status and B shows Idle (or its live Status) from its own first row, never A's class or wait
**And** a pane that was not released (A still running across the restart) is still seeded exactly as before
**And** saved state written by 0.5.0 (no release information) still loads and seeds as before
**And** AD-3's "Successive Agents on one AgentKey" paragraph states the case and the remaining residual

### Story 5.18: A Profile hold always ends

As the operator,
I want an Agent's Box never to freeze while its Profile is being looked up,
So that repeated detections or a hung lookup cannot stop its rows reaching the record and the Board.

Source: follow-up review of Story 5.4, 2026-10-23, reproduced by a test (`profileTimeout` 200 ms, a
blocked environment read, `agent_detected` every 50 ms: after 3 s and 61 lookups the row was still
held). Each `agent_detected` or program change on a known pane calls `resolve`, which supersedes the
lookup in flight, so while triggers keep arriving no lookup settles, `holdRows` stays true and
`p.held` grows. Also: `readProfile`'s inner environment-read goroutine is neither tracked nor guarded,
so a permanently hung read leaves 1 + `profileRetries` goroutines per trigger.

**Acceptance Criteria:**

**Given** triggers keep arriving while rows are held, or the environment read never returns
**When** the hold has lasted its hard deadline (a tunable, at most a small multiple of `profileTimeout`)
**Then** the held rows are released in order with the best Profile known (the old one for the old program, `unresolved` otherwise), and the Board shows the Agent's current state
**And** a trigger arriving while rows are held does not move the hold's deadline
**And** at most one environment read per pane is in flight, so a hung read cannot multiply goroutines
**And** the reviewer's reproduction becomes a test that passes, alongside the existing Profile tests

### Story 5.19: A title bar on the Board

As the operator,
I want a standard title bar with a text logo at the top of the Board,
So that the page says what it is, and whether anything needs me and herdr is connected, at a glance.

Source: operator request, 2026-10-26; the Board design forbade a title bar, so ADR-0013 amends
ADR-0003 and SM-C1. Design approved by the operator from the Board rendered with their live data.

**Acceptance Criteria:**

**Given** the Board is open
**Then** a title bar, sticky above the strips, shows the text logo `herddash` with its three-bar mark, and a summary of the Needs you count and herdr's version
**And** the summary's rules are a pure function in `order.mjs` (`titleBar`), tested with `node --test`: the count is 0 while the Boxes are hidden, and the herdr item reads "Reconnecting…" when the stream is lost, "herdr disconnected" when herdr is, and otherwise `herdr <version>` with a tone of ok, warn (advisory) or bad (Boxes hidden)
**And** the summary is not a live region and holds no control; the strips are unchanged
**And** the browser check asserts the title bar's text in the connected, needs-you and disconnected states
**And** the design doc, SM-C1 and the ADR index record the change

## Epic 6: Acknowledging a Finished Agent

The operator marks a Finished Box Seen once its result is read, so a workspace's finished count
means "results I have not read" again. Decided 2026-10-27 by the operator (vfkb 9aee0fc69560) from
a mock rendered with their live Board; the structure is
[ADR-0014](../adr/0014-acknowledging-a-finished-agent.md), which every story obeys and which
amends ADR-0003, ADR-0005, ADR-0007 and PRD SM-C1 and closes PRD §8 Q1. Decided in advance, so no
story asks them again:

- Stories are built and installed locally as `-dev` builds. When the epic is done the operator is
  asked once, to dogfood; on their OK it ships as one pull request and one release.
- An acknowledgement is herddash's own state; herdr is never told. It names one exact finish (the
  AgentKey and the class start the view sends as `waiting_since`), is recorded append-only in its
  own table (migration 0005), and is in force while the Agent's fold is Finished with that same
  start, so `done` to `idle` within the finish keeps it and any other Transition ends it.
- No keyboard shortcut (the operator uses Vimium); the controls are ordinary buttons.
- No new import edge; `httpapi` reaches the engine through `engine.Engine.Acknowledge`.

Build order: the record and its rule, then the engine, then the write routes, then the Board, then
the documents and the dogfooding build.

*Amended 2026-10-27 (operator decision while dogfooding, vfkb f14fa1cdfa89,
[ADR-0015](../adr/0015-seen-when-you-leave-the-pane.md)):* visiting a pane in herdr and leaving it
did not mark its Box Seen. A Finished Agent now also becomes Seen when, after its finish began, its
pane was focused in herdr for at least 3 s and focus then moved to another pane; a Board click
still marks at once. herdr's `done`/`idle` is not used. herddash only reads focus (the global
`pane.focused` event and the snapshot's `focused_pane_id`, observed in
`docs/research/herdr-focus-events-2026-10-27.md`) and never calls a focus method. Automatic marks
are rows with source `herdr_focus` (migration 0007, since the operator's database is already at
schema 6); a Finished Box whose pane is focused shows `● in view`; a Seen Box offers `Mark unseen`,
after which the Daemon never re-marks that finish. Stories 6.6 to 6.9 build it, in this order: what
herdr says about focus and where a mark came from (adapter, core, store); the engine's focus
tracking and automatic mark; the Board's tag and pill; the documents and a new dogfooding build.
The epic's dogfooding checkpoint moves to the end of Story 6.9.

### Story 6.1: Acknowledgements in the record

As the operator,
I want an acknowledgement stored as a fact the record keeps, with the rule that reads it in core,
So that whether a finish is Seen is derived from the record and survives anything the Daemon does.

Builds ADR-0014's record: migration `0005_acknowledgements.sql` (the table and two indexes exactly
as the ADR gives them); `core.Acknowledgement`, built only by `core.NewAcknowledgement`
(`AcknowledgementInput{Key, FinishedSince, Seen, ObservedAt}`; it refuses an empty host, server
or pane id, epoch 0, and a non-positive `FinishedSince` or `ObservedAt`, with an error wrapping
`core.ErrInvalidAcknowledgement`); the pure rule `core.Seen(f FoldResult, acks []Acknowledgement)
bool`, true exactly when `f` is known, its class is `finished`, `f.Since > 0`, and the last of
`acks` (given in recording order) whose `FinishedSince` equals `f.Since` has `Seen` set;
`AgentViewInput.Seen` and `AgentView.Seen` (`json:"seen"`, always sent), set only when the input
says so and the class is `finished`; and the sort key's `class_rank` 2 (Idle's) for a seen,
non-Background Agent. In `store`: `AppendAcknowledgements(ctx, []core.Acknowledgement) error`, one
prepared `INSERT` in one transaction, all or nothing, a no-op for none; `Acknowledgements(ctx, key)
([]core.Acknowledgement, error)`, one Agent's rows ordered by `id` (recording order, not `observed_at`), each rebuilt
through the constructor; and `Sweep` deleting acknowledgements in its existing transaction (a row
older than the cutoff goes unless its key is present and its `finished_since` is at or after the
present `Since`, or that `Since` is 0), counted in `SweepResult.Acknowledgements`. The `DELETE`
stays in `sweep.go` (`TestOnlyDelete`). `callsite_test.go` guards `AppendAcknowledgements` to
`internal/engine` and `internal/store`. No engine, HTTP or Board change.

**Acceptance Criteria:**

**Given** a database at schema 4 holding Transitions, resync markers and `server_state`
**When** this binary opens it
**Then** it is migrated to schema 5 in one transaction, every existing row is unchanged, and a binary knowing only schema 4 is refused by the existing `store.ErrSchemaNewer` check (tested by opening a schema-5 database with the migration list truncated)

**Given** a batch of three acknowledgements and a test-only SQLite trigger that raises `ABORT` on the second row's insert
**When** `AppendAcknowledgements` runs
**Then** it returns an error and the table holds no row from the batch, the first row's insert having been rolled back (a rollback forced mid-transaction, after a row was written, not at `BeginTx`)

**Given** rows written by `AppendAcknowledgements`, the store closed and opened again
**When** `Acknowledgements` reads the Agent and `core.Seen` folds them with that Agent's `core.Fold` of its `History`
**Then** the result is the same as before the reopen, for: no rows; seen; seen then undone; undone then seen again; a row for an older finish only; a later Agent on the same key (its first row has no previous Status, so its `Since` differs); and another key's rows

**Given** `core.Seen` and `core.NewAgentView`
**When** they are table-tested with no I/O
**Then** `seen` is false for Needs you, Idle, Working, Unknown and an unknown or zero `Since`; `done` to `idle` within one finish keeps it (same `Since`); a seen view has `class_rank` 2 and the Idle `start`, and a seen Background view keeps `class_rank` 3; `attention_class` stays `finished`; and the shared golden ordering fixture gains a workspace with a seen Agent between its Finished and older Idle Agents, which the Board's `order.mjs` test orders identically

**Given** a Retention sweep with a present Agent seen on its current finish, the same Agent's acknowledgement of an older finish, and an absent Agent's acknowledgements, all older than the cutoff
**When** `Sweep` runs
**Then** only the present Agent's current-finish rows remain; with the present `Since` 0 all of that key's rows remain; and a test that makes the acknowledgement `DELETE` fail (a test-only trigger raising on delete), after the Transition and marker deletes ran, leaves every Transition, marker and acknowledgement in place

**And** `go vet ./...`, `go test -race ./...`, `gofmt -l .` and `arch_test.go` pass, with no new import edge

### Story 6.2: The engine acknowledges a finish

As the operator,
I want the Daemon to accept an acknowledgement only for the finish I am looking at, and keep it across a restart,
So that a Seen Box is never one whose Agent has moved on since I looked.

Builds ADR-0014's engine part. `engine.Engine.Acknowledge(ctx, engine.AckRequest) ([]core.AgentView,
error)`, with `AckRequest{Seen bool; Items []AckItem}` and `AckItem{Key core.AgentKey; WaitingSince
int64}`, sends one inbox message with a reply channel (as `Agents` does) and waits for the reply or
`ctx`. The actor validates every item before writing any: duplicate keys refused
(`engine.ErrBadRequest`); the key's Host and Server are the engine's, its epoch the one in force,
and its pane a present Agent with a published view (else `engine.ErrUnknownAgent`, also while the
gate is refused); for a mark, the class is `finished` and the view is not Background (else
`engine.ErrNotFinished`), and the fold is known with `Since > 0` equal to `WaitingSince`, the pane
holds no Profile-held rows and its fold is in the record (else a `*engine.StaleError` naming the
keys, matching `engine.ErrStaleView` with `errors.Is`); an item already in the asked state is a
no-op. An undo writes a `seen = 0` row only for a finish acknowledged now; otherwise it is a no-op.
All items validate or nothing is written; the rows go through one `AppendAcknowledgements`.
Without a store, or with recording off for the run, it returns `engine.ErrRecordingOff`; a store
failure returns an error wrapping it and changes nothing. On success the actor sets each pane's
seen state, publishes once with one `ChangeAgent` per Agent whose view changed, and replies with
the views in request order. The `Recorder` interface gains `AppendAcknowledgements` and
`Acknowledgements`. Seen state is derived: wherever a pane's fold is seeded from the record (its
first sight, and every reseed), the engine reads `Acknowledgements` for the key and applies
`core.Seen`; any fold change that moves the class or `Since` clears it without a read; a read
failure is logged and leaves the Agent unseen. Each write is logged at info (pane, since, seen),
nothing else. Retention needs no change beyond Story 6.1's sweep (its `Present.Since` already is
the fold's), except that its `retention sweep` log line also reports the acknowledgements swept,
`SweepResult.Acknowledgements` (deferred from Story 6.1, which added the count and changed no engine
code).

**Acceptance Criteria:**

**Given** a Finished Agent on the fake herdr
**When** `Acknowledge` marks it with its view's `waiting_since`
**Then** one row is recorded, the published view has `seen` true and `class_rank` 2, one publication carries one `agent` change for it, and the reply is that view

**Given** the same Agent, and in turn: a `waiting_since` one millisecond off; a Profile hold in progress on its pane; an unknown pane; the right pane under an older epoch; a Needs you, an Idle, a Working and a Background Agent
**When** each is marked
**Then** the first two are `ErrStaleView`, the next two `ErrUnknownAgent`, the rest `ErrNotFinished`, and none writes a row or publishes

**Given** a bulk mark of three Finished Agents where one `waiting_since` is stale
**When** it is sent
**Then** it fails naming that key, and no row is written for any of the three

**Given** a seen Agent
**When** it goes `done` to `idle`
**Then** it stays seen; **and when** it goes `working`, or `blocked`, or its pane gets a new Agent (a row with no previous Status), or the epoch increments, it is no longer seen, with no acknowledgement row written; **and when** it finishes again, it is Finished and unseen

**Given** an undo of a seen Agent, and an undo of an Agent that has since gone `working`
**When** each is sent
**Then** the first writes a `seen = 0` row and the view returns to Finished with `class_rank` 1; the second succeeds, writes nothing and publishes nothing

**Given** a seen Agent and a Daemon that stops and starts again over the same database (a new engine, the same store)
**When** the first snapshot shows the Agent's Status unchanged
**Then** its view is seen with the same `waiting_since`; **and when** the snapshot shows it changed, it is not; **and** the same holds across an in-run reconnect that keeps the epoch, and a Retention sweep run before the restart does not change the outcome (state derived from the record across a restart, not only within one run)

**Given** an engine with no store, and one whose `AppendAcknowledgements` fails
**When** an Agent is marked
**Then** the first returns `ErrRecordingOff`; the second returns the store's error, and the view and the next publication are as if no request was made

**Given** 8 goroutines sending marks, undos and bulk requests while the fake herdr sends status events for the same panes
**When** the test runs under `go test -race -count=50` (and `-cpu=1,4,8`)
**Then** it never races, hangs or deadlocks, every reply is either the resulting views or one of the typed errors, and after it settles each Agent's `seen` equals `core.Seen` of its record

**Given** a Retention sweep that deletes acknowledgements
**When** the engine logs it
**Then** the `retention sweep` line carries `acknowledgements` with `SweepResult.Acknowledgements` beside `transitions` and `resync_markers` (deferred from Story 6.1)

**And** `go vet ./...`, `go test -race ./...`, `gofmt -l .`, `arch_test.go` and `callsite_test.go` pass

### Story 6.3: Write routes, guarded against cross-site requests

As the operator,
I want the Board, and only the Board, to be able to make an acknowledgement over HTTP,
So that no other web page I visit can change what my Board shows.

Builds ADR-0014's write routes in `internal/httpapi`, wired in `cmd/herddash`. `httpapi.Options`
gains `Acknowledge func(ctx, engine.AckRequest) ([]core.AgentView, error)` and
`Acknowledgements func(ctx, core.AgentKey) ([]core.Acknowledgement, error)`, both required (`New`
panics on nil, as for the others). Routes, all `POST`: `/api/agents/{host}/{server}/{epoch}/{pane}/seen`
and `.../unseen` with body `{"waiting_since": <ms>}`, the path parsed exactly as the history path is
(a final segment of `seen` or `unseen` instead of `history`); `/api/seen` and `/api/unseen` with
body `{"agents": [{"key": {"host", "server", "epoch", "pane_id"}, "waiting_since": <ms>}]}`, 1 to
256 items, no key twice. The guard runs in this order and before any body byte is read: loopback
`Host` (`403 host_not_allowed`, as today); `Origin` present and equal, case-insensitively, to
`http://` + `Host` (`403 origin_not_allowed`, also for `null`); `Sec-Fetch-Site`, when present,
`same-origin` (`403 origin_not_allowed`); `X-Herddash-Write: 1` (`403 write_header_missing`);
`Content-Type` `application/json`, with at most a `charset=utf-8` parameter (`415
unsupported_media_type`); body at most 4 KiB per-Agent or 64 KiB bulk (`413 body_too_large`, via
`http.MaxBytesReader`); exactly one JSON object with no unknown fields and no trailing data, a
positive `waiting_since` and a well-formed key (`400 bad_request`). Engine errors map to `404
unknown_agent`, `409 not_finished`, `409 stale_view` (its message names the stale keys), `503
recording_off`, `400 bad_request` and `500 store_failed`; a cancelled request writes no response
body. Success is `200 {"agents": [AgentView, ...]}`. `GET`/`HEAD` on a write route is `405` with
`Allow: POST`; every other method on any route keeps today's 405 and `Allow`; `OPTIONS` is a 405
everywhere; no response, success or error, carries an `Access-Control-*` header. The history body
gains `acknowledgements`, the Agent's rows in recording order as `{"finished_since", "seen",
"observed_at"}`, always sent (`[]` when none). The `/events` stream needs no new event; its `agent`
event carries `seen`.

**Acceptance Criteria:**

**Given** a valid same-origin request (`Origin: http://127.0.0.1:<port>`, `Host` the same, `X-Herddash-Write: 1`, `Content-Type: application/json`)
**When** it marks a Finished Agent per-Agent and in bulk, and undoes each
**Then** each returns 200 with the views, and the fake `Acknowledge` received exactly the request's items

**Given** a cross-site or malformed request, one case per row of a table: no `Origin`; `Origin: null`; `http://evil.example`; the right host on another port; `https://` with the right host; `Sec-Fetch-Site: cross-site` and `same-site`; no `X-Herddash-Write`; `X-Herddash-Write: 0`; `Content-Type` `text/plain`, `application/x-www-form-urlencoded` and `multipart/form-data`; a body one byte over each limit; an unknown field; trailing data; 0 and 257 bulk items; a duplicate key; a non-loopback `Host` with a valid `Origin` for it
**When** each is sent
**Then** each gets its status and code from the guard, `Acknowledge` is never called, and nothing is recorded (a cross-site request is refused: the retrospectives' habit)

**Given** each engine error
**When** it is returned for a request
**Then** the response has the mapped status and code in the JSON error shape

**Given** `GET` on `/api/seen`, `POST` on `/api/state`, `/events` and the history route, and `OPTIONS` on every route
**When** each is sent
**Then** each is a 405 with the right `Allow`, and no response in this whole test file carries an `Access-Control-*` header

**Given** an Agent with acknowledgement rows
**When** its history is read
**Then** the body has its Transitions as before and `acknowledgements` in recording order; with none it is `[]`

**Given** `internal/httpapi/engine_contract_test.go` driving the SSE handler from the real engine
**When** an Agent is marked, then undone, then goes `working`
**Then** the stream carries an `agent` event for each change, and replaying the publications through `order.mjs` (`testdata/replay-board.mjs`) gives a Board model equal to the published view, `seen` included

**And** `cmd/herddash`'s `serve` wires `Options.Acknowledge` to the engine and `Options.Acknowledgements` to the store, shown by a test (adding the smallest test seam if `serve`'s wiring has none); Story 6.4's browser check then exercises the same wiring through the real binary
**And** `go vet ./...`, `go test -race ./...`, `gofmt -l .`, `arch_test.go` and `callsite_test.go` pass, with no new import edge

### Story 6.4: The Board's Seen control, bulk mark and Undo

As the operator,
I want a Seen pill on each Finished Box, a mark-all for a workspace, and a short Undo,
So that I can clear what I have read in one click and take it back if I misclicked.

Builds ADR-0014's Board part, from the approved mock (its `board.mjs` changes and the `MOCK` section
of its `board.css`, kept in the session scratchpad `ackmock/`; everything needed is restated here).
In `order.mjs` (pure, `node --test`): `boxKind` returns `"seen"` for `view.seen === true` unless
Background (Background first); `classLabel` gives `Seen` (drawn uppercase, SEEN, as every class
label); `groupCounts` counts `seen` and lists it after idle, `1 finished · 3 idle · 2 seen · 1
background · 2 working`, so a seen Agent is not counted finished; `unseenFinished(views)` is the
group's Boxes of kind `finished`; `ackBody(views)` builds the bulk body from each view's key and
`waiting_since`; `ackToast(views, seen)` gives `Marked <tab> seen` or `Marked N Agents seen`
(undo: `Marked <tab> not seen`); `ackError(status, code)` gives the bar text for a refusal: 409
`It changed before it was marked; nothing was marked`, 503 `herddash is not recording;
nothing was marked`, else `Could not mark it seen`. In `board.mjs`: a `button.ack` (`✓` and `Seen`,
`title` "Mark seen: you have read this Agent's result", `aria-label` "Mark <tab> seen") at the end
of the location line of every Box of kind `finished` and no other; a `button.ack-all` in every
workspace heading after its counts, text `✓ Mark N finished seen`, `aria-label` "Mark N finished
Agents in <workspace> seen", hidden when the group has fewer than 2 unseen Finished Boxes; a click
sends `fetch` `POST` with `Content-Type: application/json` and `X-Herddash-Write: 1` (the browser
adds `Origin`), the per-Agent route for one Box and `/api/seen` for a heading; the button is
disabled while its request is in flight and every control is disabled while the Boxes are dimmed
or hidden. The Board changes nothing optimistically: the Box changes when the stream's `agent`
event arrives. On success an Undo bar (`div.toast`, `role="status"`, fixed at the bottom centre)
shows the toast text and an `Undo` button for 6 s; Undo sends `/api/unseen` (or the per-Agent
`unseen`) for the same items and closes the bar; a new mark replaces the bar and its timer; a
refusal shows `ackError`'s text for 6 s with no Undo. No key binding is added. CSS moves out of the
mock's `MOCK` section into named rules using the existing tokens, so dark mode follows: `.ack`
(pill, finished accent, 1px finished edge, inverted on hover); `.box--seen` (6px left border in
`--border-quiet`, class label `--ink-faint`, location and Context Line `--ink-muted`, Waiting Time
`--ink-faint` at weight 600); `.ack-all` (pill, `margin-left: auto`, `[hidden]` not displayed);
`.toast`, `.toast--on`, `.toast-undo` (ink background, surface text, 150 ms fade). The Box's
accessible label says Seen for a seen Box. `docs/design/board-design.md` gains a dated
"Acknowledgement controls" section (the pill, the seen Box, the heading button, the Undo bar), and
its Box, zones-and-counts and "Deliberately absent" sections are amended to match (ADR-0014).

**Acceptance Criteria:**

**Given** `order.mjs`
**When** `node --test` runs
**Then** `boxKind`, `classLabel`, `groupCounts`, `unseenFinished`, `ackBody`, `ackToast` and `ackError` are table-tested, including a seen Background view (Background) and a group whose finished count drops by one and gains `1 seen` when one of its Finished views becomes seen

**Given** the browser check (`internal/httpapi/web/check/board-browser.mjs`) against the real binary, the fake herdr and a temporary data directory, with several Finished Agents in one workspace and Needs you, Idle, Background and Working Agents beside them
**When** it runs
**Then** it asserts: a `button.ack` on every Finished Box and on no other kind; clicking one makes that Box `box--seen`, labelled SEEN, placed after the group's Finished Boxes and among its Idle Boxes by the Daemon's order, and the heading's counts drop one finished and show `1 seen`; the Undo bar appears with `Marked <tab> seen` and its Undo returns the Box to Finished; the bar is gone after 6 s; the heading's `✓ Mark N finished seen` is hidden with fewer than 2 unseen Finished Boxes and marks exactly those N when clicked; the fake herdr turning a seen Agent `working` and then `done` brings back a Finished Box with its pill; and after `serve` is stopped and started again on the same data directory, a seen Box is still seen

**Given** the browser check also serves a page from a second origin (another loopback port) that `fetch`es a `POST` to the per-Agent route with the custom header and JSON
**When** the page runs
**Then** the request fails (no CORS preflight answer, or 403), and the Board still shows that Box Finished

**And** CI's `board-browser` job runs these checks and fails when, for example, the `.box--seen` rule is removed
**And** `docs/design/board-design.md` records the controls, and the design matches the mock the operator approved; the live-data design gate is the operator's dogfooding at the end of the epic
**And** `go test ./...`, `node --test internal/httpapi/web/` and `gofmt -l .` pass

### Story 6.5: Documents, and the build the operator dogfoods

As the operator,
I want the documents agents and I read to describe acknowledgement, and a `-dev` build of the epic running on my machine,
So that I can dogfood it and decide whether it ships.

Updates `docs/how-herddash-works.md` (what Seen does and does not do; "Never accept a change over
the web" becomes "Never accept a change over the web but an acknowledgement, and only from its own
page"), `CLAUDE.md` (the repository state: Epic 6, migration 0005 and its one-way door, the write
routes and their guard, `seen` on the Board; the open operator checkpoint), `docs/install.md` (back
up the database before installing a build with schema 5, since 0.7.x refuses it, per its existing
backup section), and `docs/implementation/deferred-work.md` for anything the epic's reviews deferred
without a story. Then builds a `-dev` binary of the branch, backs up the database while the
launchd agent is stopped, installs the binary at `~/.local/bin/herddash` as Epic 5's `-dev`
builds were, starts the agent, and checks it. It never touches the live herdr server, and makes no
acknowledgement itself.

**Acceptance Criteria:**

**Given** the epic's code is merged into this branch
**When** the documents are read
**Then** how-herddash-works, CLAUDE.md and install.md agree with ADR-0014 and the code (routes, guard, `seen`, migration 0005), and every deferral from Epic 6's reviews is either in a story's criteria or in `deferred-work.md`

**Given** the installed `-dev` binary under launchd
**When** it is checked
**Then** `herddash version` prints the branch's commit, `/api/state` answers on loopback with `seen` on every Agent, the database is at schema 5 with the backup beside it, and a `POST` to `/api/seen` without `Origin` is refused with 403

**And** the operator is then asked, once, to dogfood the Seen control; on their OK the epic ships as one pull request and one release

### Story 6.6: Read herdr's focus, and record where a mark came from

As the operator,
I want herddash to read which pane herdr has focused, and every acknowledgement to say whether I clicked it or the Daemon inferred it,
So that the automatic mark can be built on facts herdr reports and audited afterwards.

Builds ADR-0015's adapter, core and record parts; no engine behaviour, view or Board change. In
`internal/herdr/gen/genwire`, `roots` gain the `pane.focused` variant of `Subscription` and the
`pane_focused` variant of `EventData`; `go generate ./internal/herdr/gen` regenerates `wire_gen.go`
from the committed 0.9.5 schema (the drift test passes), and `forbidden` is unchanged
(`pane.focus`, `agent.focus` and the output reads still refused). In `internal/herdr`,
`lifecycleSubscriptions` gains `pane.focused` (and only that focus kind: not `tab.focused` or
`workspace.focused`); a `pane_focused` line maps to `core.LifecycleEvent{Kind:
core.LifecycleFocused, PaneID, WorkspaceID}` (a new `core.LifecycleKind`, `Valid`), one with no
`pane_id` is delivered flagged unexpected as for the other kinds, and a `workspace_focused` or
`tab_focused` line is dropped without a delivery; `mapSnapshot` maps
`focused_pane_id` to `core.Snapshot.FocusedPaneID` (`""` when absent or null). Until Story 6.7 the
engine's `onLifecycle` ignores `LifecycleFocused` entirely: it does not call `noteEvent` or
`requestSnapshot`, so a focus event never causes a snapshot read. In `internal/core`:
`AckSource` with `AckSourceBoard = "board"` and `AckSourceHerdrFocus = "herdr_focus"`;
`AcknowledgementInput.Source` and `Acknowledgement.Source()`; `NewAcknowledgement` refuses any
other value, the empty one included, wrapping `ErrInvalidAcknowledgement`, and refuses an undo
(`Seen` false) with source `herdr_focus`; the pure rule `core.SeenOnLeave(in LeaveInput) bool`,
`LeaveInput{View AgentView; FinishHasRow bool; Dwell, MinDwell time.Duration}`, true exactly when
the view's class is `finished`, it is not Background and not seen, `WaitingSince > 0`,
`FinishHasRow` is false and `Dwell >= MinDwell` (it reads no clock: `arch_test.go`). Migration
`0007_acknowledgement_source.sql`: `ALTER TABLE acknowledgements ADD COLUMN source TEXT NOT NULL
DEFAULT 'board' CHECK (source IN ('board', 'herdr_focus'))`. In `internal/store`, the one `INSERT`
binds `source` and `Acknowledgements` reads it back through the constructor. The engine's Board
path (`acknowledge`) passes `AckSourceBoard`. The history route's `acknowledgements` items gain
`"source"`, always sent. `docs/research/herdr-socket-api.md` gets no change; the focus record is
the evidence.

**Acceptance Criteria:**

**Given** a database at schema 6 with acknowledgement rows, and one at schema 5 with rows (the operator's `-dev` databases)
**When** this binary opens each
**Then** each is migrated to schema 7 in one transaction, every earlier row reads back with source `board` and its other columns unchanged, and a binary knowing only schema 6 refuses the migrated database with `store.ErrSchemaNewer` (the migration list truncated in a test)

**Given** `AppendAcknowledgements` with a batch mixing `board` and `herdr_focus` rows and a test-only trigger raising `ABORT` on the second insert
**When** it runs
**Then** no row of the batch remains (a rollback forced after a row was written); without the trigger every row reads back with its source, across a store close and reopen

**Given** a raw `INSERT` with `source = 'other'`
**When** it runs against schema 7
**Then** SQLite's CHECK refuses it; and `core.NewAcknowledgement` refuses `""`, `"other"` and an undo with source `herdr_focus`

**Given** `core.SeenOnLeave` table-tested with no I/O
**When** each input is tried
**Then** it is true only for a Finished, non-Background, unseen view with a known `waiting_since`, no row for the finish and a dwell at or above the minimum; false at the minimum minus 1 ms, for Needs you, Idle, Working, Unknown, Background, an already seen view, and a finish with a row (`Mark unseen` sticks)

**Given** the herdr fake socket in `internal/herdr`'s tests sending `workspace_focused`, `tab_focused`, `pane_focused` (copied from the focus record's transcript), a repeated `pane_focused` for the same pane, and a `pane_focused` without `pane_id`
**When** `SubscribeLifecycle` reads them
**Then** the subscribe request names `pane.focused` and no other focus kind, the first two lines are dropped without a delivery (neither an event nor an unexpected line, so herdr sending them, subscribed or not, never causes a snapshot read), each `pane_focused` is delivered as `LifecycleFocused` with its pane and workspace, the last is flagged unexpected, and the stream goes on; `forbidden_test.go` still proves `internal/herdr` has no focus method

**Given** a snapshot fixture with `focused_pane_id` set, null and absent
**When** it is mapped
**Then** `core.Snapshot.FocusedPaneID` is the pane id, `""` and `""`

**Given** the engine receiving `LifecycleFocused` events from the fake herdr in `internal/engine/fake_test.go` (which gains a way to emit them) during and between snapshot reads
**When** the test counts snapshot reads
**Then** the focus events cause none, and change no view (Story 6.7 gives them meaning)

**Given** an Agent with a `board` and a `herdr_focus` row (the latter written directly through the store in the test)
**When** its history is read
**Then** each item carries its `source`

**And** `go generate ./internal/herdr/gen` leaves no diff, and `go vet ./...`, `go test -race ./...`, `gofmt -l .`, `arch_test.go` and `callsite_test.go` pass, with no new import edge

### Story 6.7: The engine marks a finish Seen when the operator leaves its pane

As the operator,
I want a result I read in herdr to be marked Seen when I move to another pane, and the Board to know which pane is focused,
So that I do not have to click Seen for something I already read where I work.

Builds ADR-0015's engine and view parts. The engine holds, per connection and in memory only, the
focused pane id and its dwell start (`e.clock()`, a `time.Time`). `onLifecycle` handles
`LifecycleFocused` without `noteEvent` or `requestSnapshot`: a `pane_focused` for the pane already
focused changes nothing; one for another pane is a **leave** of the previous pane, then focuses the
new one with its dwell starting now. A leave evaluates `core.SeenOnLeave` for the left pane's
Agent, with `Dwell` = now minus its dwell start and `MinDwell` = `seenDwell` (new tunable
`defaultSeenDwell = 3 * time.Second` in `tunables.go`, a package variable tests lower, listed in
`docs/config.example.toml`, which the planning commit already did), after the Board mark's own
conditions (present, published Agent under the epoch in force; fold known with `Since > 0`; no
Profile-held rows; fold in the record). `FinishHasRow` comes from a per-pane flag, set whenever a
row for the current finish is written (Board or automatic) or read at a seed (`seedSeen` reads the
rows already; the flag is set when any row names the fold's `Since` and the rows are not refused
by `core.SeenAt`'s order check) and cleared wherever `clearStaleSeen` or `missedTurn` ends the
finish. A mark writes one row through `AppendAcknowledgements` with source `herdr_focus` and
herdr's order when known, sets the pane seen and the flag, logs `acknowledgement` at info with
`source=herdr_focus`, and publishes; a write failure is logged and changes nothing. The **dwell
restarts at now** whenever the focused pane's fold changes class or `Since`, its pane gets a new
Agent, or `missedTurn` ends its acknowledgement; `done` to `idle` within a finish does not restart
it. At a connection's first snapshot and at every resync, the focused pane is
`Snapshot.FocusedPaneID`, its dwell starting when the snapshot is applied, and any earlier focus is
dropped without a leave. A later snapshot whose `FocusedPaneID` differs moves focus to it, dwell
from now, **without** a leave, unless a `pane_focused` arrived after that read started (the event
is newer and stands). Focus is never recorded. `core.AgentViewInput.InView` and `AgentView.InView`
(`json:"in_view"`, always sent): true for the Agent whose pane is focused. A focus move publishes
once, with `ChangeAgent` for each Agent whose view changed (the pane left, the pane landed on, and
a mark). It does not change the sort key. The engine contract test
(`internal/httpapi/engine_contract_test.go`) and its replay through `order.mjs` include `in_view`.

**Acceptance Criteria:**

**Given** a Finished Agent in pane A on the fake herdr, with the test's clock
**When** `pane_focused` A arrives, the clock moves 3 s, and `pane_focused` B arrives
**Then** A's view is `in_view` between the two events and not after, A is seen with `class_rank` 2, one `herdr_focus` row names A's `waiting_since`, and the publication for the leave carries `agent` changes for A and B only

**Given** the same, with the clock moving 2.999 s before the leave; and a flick A, B, C, D with 1 s on each
**When** the events arrive
**Then** nothing is marked and no row is written (flicking through panes does not mark)

**Given** pane A focused for 10 s with its Agent Working, then the Agent finishes (`done`) while A stays focused
**When** focus moves to B 1 s after the finish, and in a second run 3 s after it
**Then** the first does not mark (the dwell restarted at the finish) and the second does

**Given** pane A focused, its Agent finishing into it while the operator is away, and no further focus event for an hour (reconcile snapshots naming A arriving meanwhile)
**When** the hour passes
**Then** A stays Finished and `in_view` with no row; **and when** `pane_focused` B then arrives, A is marked Seen (the dwell counted from the finish)

**Given** a herdr with no focus events at all (detached), its snapshots naming a focused pane whose Agent is Finished
**When** many reconciles pass
**Then** nothing is ever marked; the pane is `in_view`

**Given** a pane left after 3 s whose Agent is Needs you, Idle, Working, Unknown or Background, or is already seen
**When** the leave is evaluated
**Then** nothing is written; and a `done` to `idle` of an unfocused Finished Agent marks nothing (herdr's done/idle is not used)

**Given** an automatic mark, then a Board `unseen` for that finish (`Mark unseen`)
**When** the operator returns to the pane for 10 s and leaves again, and then the Daemon stops and starts over the same database and the snapshot shows the Status unchanged and the focus on that pane, and the operator leaves after 10 s more
**Then** the Agent stays Finished each time and no further row is written (the explicit choice sticks, derived from the record across a restart)

**Given** an automatic mark and a Daemon that stops and starts again over the same database
**When** the first snapshot shows the Agent's Status unchanged
**Then** its view is seen with the same `waiting_since`; **and** an Agent marked by the Board before a missed turn (herdr's order risen at the restart) is not seen and, focused 3 s and left, is marked again with a new `herdr_focus` row (the earlier rows do not count for the new turn)

**Given** a focused pane A dwelt on for 10 s, and a reconnect that keeps the epoch during which the operator moved to B (the event missed), the snapshot after it naming B
**When** the reconnect completes
**Then** A is not marked (a missed focus move never marks), B is `in_view` with its dwell counted from the snapshot's application, and leaving B after 3 s marks B if it is Finished

**Given** a reconcile snapshot naming pane C while the engine holds B, with no event (another client's view, finding 4), and in a second case a `pane_focused` C arriving while a snapshot read that started before it names B is in flight
**When** each snapshot is applied
**Then** in the first, focus moves to C and B is not marked; in the second, focus stays C

**Given** a status `working` for pane A arriving just after `pane_focused` B (the prompt raced the leave) with A dwelt on 5 s
**When** both are handled
**Then** A ends Working, not seen, whatever the order the two events arrive in

**Given** a leave during a Profile hold on the left pane, and a leave whose `AppendAcknowledgements` fails
**When** each is evaluated
**Then** nothing is marked, the failure is logged, the view and next publication are as if no leave happened, and a later leave of the same finish after 3 s more marks it

**Given** 8 goroutines sending Board marks, undos and bulk requests while the fake herdr sends status events and `pane_focused` events for the same panes and the clock advances
**When** the test runs under `go test -race -count=50` and `-cpu=1,4,8`
**Then** it never races, hangs or deadlocks; every reply is the views or a typed error; after it settles each Agent's `seen` equals `core.SeenAt` of its record, no finish has more than one `herdr_focus` row, and no `herdr_focus` row follows a `board` row for the same finish

**Given** the engine contract test driving the SSE handler
**When** focus moves A to B and A is marked by the leave
**Then** the stream carries `agent` events for A and B with `in_view` and `seen`, and the replay through `order.mjs` equals the published view

**And** `internal/engine/fake_test.go` gains `pane_focused` events and a snapshot `focused_pane_id`; `go vet ./...`, `go test -race ./...`, `gofmt -l .`, `arch_test.go` and `callsite_test.go` pass, with no new import edge

### Story 6.8: The Board shows "in view" and offers Mark unseen

As the operator,
I want to see which Finished Box is the pane I have open in herdr, and take back a Seen I disagree with,
So that the automatic mark is visible before it happens and reversible after.

Builds ADR-0015's Board part, exactly as `docs/design/board-design.md`'s "Seen on leaving the pane,
the in-view tag and Mark unseen" section (written in the planning commit) gives it. In `order.mjs`
(pure, `node --test`): `inViewTag(view)` true exactly when `boxKind(view) === "finished"` and
`view.in_view === true`; the Box's accessible name gains ", in view" after the class label for such
a Box; `unackLabel(view)` gives "Mark <tab> not seen"; `ackToast(views, false)` gives "Marked <tab>
not seen" (or "Marked N Agents not seen"); `ackError(status, code, seen)` gives "Could not mark it
not seen" for an unseen refusal outside 409 and 503. In `board.mjs`: a `span.in-view` (`● in view`,
the design's title, not focusable) on the first line of a Finished Box with `in_view`, after the
pane badge and before `button.ack`; a `button.unack` (`Mark unseen`, the design's title,
`aria-label` "Mark <tab> not seen") on every Box of kind `seen` and no other kind; a click posts
`{"waiting_since"}` to the per-Agent `unseen` route with the same headers as Seen, the button
disabled in flight and every control while the Boxes are dimmed or hidden; on success the Undo bar
shows "Marked <tab> not seen" for 6 s with an `Undo` that posts the per-Agent `seen` route; a
refusal shows the refusal bar. Nothing is optimistic: the Box changes on the stream's `agent`
event. CSS: `.in-view` and `.unack` as the design gives them, from existing tokens, so dark mode
follows. The browser check's fake herdr (`internal/httpapi/web/check/board-browser.mjs`) gains the
`pane.focused` subscription, `pane_focused` lines it can push, a moving `focused_pane_id` in its
snapshot, and a lowered dwell through a test-only flag or environment variable, the smallest seam
`cmd/herddash` can take (`HERDDASH_TEST_SEEN_DWELL_MS` honoured only when the check sets it, or an
equivalent), so the check does not wait 3 s per step.

**Acceptance Criteria:**

**Given** `order.mjs`
**When** `node --test` runs
**Then** `inViewTag`, the accessible name, `unackLabel`, `ackToast` for unseen and `ackError` for unseen are table-tested, including: a Seen, a Background, a Needs you and an Idle view with `in_view` true show no tag; a Finished view with `in_view` false shows none

**Given** the browser check against the real binary, its fake herdr and a temporary data directory, with Finished Agents in panes A and B and Needs you, Idle, Working and Background Agents beside them, focus on A in the snapshot
**When** it runs
**Then** it asserts: A's Box has `span.in-view` reading `● in view` and no other Box has one (Needs you included even when its pane is focused); after the fake pushes `pane_focused` B past the dwell, A's Box becomes `box--seen` with a `button.unack` and no `button.ack`, and B's Box carries the tag; clicking A's `Mark unseen` makes A Finished with its `button.ack`, and the bar reads `Marked <tab> not seen`, whose Undo makes A seen again; after a second `Mark unseen`, pushing `pane_focused` A, waiting past the dwell and pushing `pane_focused` B leaves A Finished; a flick A to B to C faster than the dwell marks nothing; and after `serve` stops and starts on the same data directory, A's state is as it was

**And** CI's `board-browser` job runs these and fails when the `.in-view` or `.unack` rule is removed; `go test ./...`, `node --test internal/httpapi/web/` and `gofmt -l .` pass
**And** the design gate is the operator's dogfooding with live data (Story 6.9), as for Story 6.4
**And** a Box redrawn for a view change while one of its controls (`button.ack` or `button.unack`) has keyboard focus gives focus back to the same control of the redrawn Box, not to the Box: since Story 6.7 every herdr focus move into or out of a Finished pane toggles `in_view` and redraws that Box, and the browser check asserts the Seen pill keeps focus across such a move (Story 6.7's review)

### Story 6.9: Documents, and the build the operator dogfoods again

As the operator,
I want the documents to describe Seen on leaving the pane, and a `-dev` build with it on my machine,
So that I can dogfood the whole of Epic 6 and decide whether it ships.

Updates `docs/how-herddash-works.md` (Seen on leave, in view, Mark unseen, that herddash reads
herdr's focus and never sets it, and the caveats: several attached clients, another API client's
focus call, `● in view` on a detached herdr), `CLAUDE.md` (the repository state: migration 0007 and
its one-way door, the `pane.focused` subscription, `in_view`, the source column, the open
checkpoint), `docs/install.md` (back up before installing a build with schema 7, which a build
knowing schema 6 refuses), and `docs/implementation/deferred-work.md` for anything Stories 6.6 to
6.8's reviews deferred without a story. Then builds a `-dev` binary of the branch, backs up the
database while the launchd agent is stopped, installs it at `~/.local/bin/herddash` as Story 6.5
did, starts the agent and checks it. It never touches the live herdr server and makes no
acknowledgement itself.

**Acceptance Criteria:**

**Given** Stories 6.6 to 6.8 on this branch
**When** the documents are read
**Then** how-herddash-works, CLAUDE.md and install.md agree with ADR-0015 and the code (the focus subscription, the rule, `in_view`, migration 0007, `source`), and every deferral from those stories' reviews is in a story's criteria or in `deferred-work.md`

**Given** the installed `-dev` binary under launchd
**When** it is checked
**Then** `herddash version` prints the branch's commit, `/api/state` answers with `seen` and `in_view` on every Agent and exactly one Agent `in_view` when herdr names a focused Agent pane, the database is at schema 7 with the backup beside it, the log shows the lifecycle subscription started with no snapshot storm from focus events, and a `POST` to `/api/seen` without `Origin` is still refused with 403

**And** the operator is then asked, once, to dogfood Epic 6 with Seen on leave; on their OK the epic ships as one pull request and one release

## Epic 7: Daemon health

Two open `deferred-work.md` entries the operator chose on 2026-10-30, after v0.8.0, as the ones
that could hurt day to day without anyone noticing: a run that records nothing while looking
healthy (Epic 2 retrospective F-6), and a log that grows without bound (Epic 4 retrospective R8).
Decided in advance, so no story asks them again:

- Each story is its own pull request into `develop`; no dogfooding checkpoint, since neither
  changes what a healthy Daemon shows or records.
- No new dependency and no change to the record's schema.

### Story 7.1: Say when recording is off

As the operator,
I want the Board and `/api/state` to say when this run records nothing,
So that a Daemon whose saved state failed to load does not look healthy while it drops every
Transition.

When `LoadServerState` fails at start, `restore` turns recording off for the run (Story 2.2's
choice, AD-3) and logs one error. The published view gains a top-level `recording` boolean, sent
on `/api/state` and in the stream's `state` event, and the Board shows a strip saying the run is
not recording and pointing at `herddash.log` while it is false. Nothing else changes: the engine
still observes and publishes, and acknowledgement's write routes still answer 503 `recording_off`.

**Acceptance Criteria:**

**Given** an engine whose Recorder's `LoadServerState` fails
**When** it publishes its first view
**Then** the view's `recording` is false, and with a Recorder that loads, true; it never changes
within a run

**Given** a `state` event or `/api/state` body with `recording: false`
**When** the Board renders it
**Then** a recording strip says herddash is not recording this run and to see `herddash.log`, and
with `recording: true` or the field absent (an older Daemon) no such strip is drawn

**Given** `deferred-work.md`'s F-6 entry
**When** the story is done
**Then** it carries a `resolved:` line naming this story

### Story 7.2: The log is rotated

As the operator,
I want `herddash.log` kept to a bounded size,
So that a Daemon that runs for months does not fill the disk.

The Daemon rotates its own `herddash.log` by size with the standard library (no new dependency),
keeping a bounded number of earlier files; `herddash.stderr.log` (launchd's) and the journal
(systemd) are left as they are and documented.

**Acceptance Criteria:**

**Given** a log file that reaches the size limit
**When** the next line is written
**Then** the file is renamed aside, a new one is started with the same owner-only mode, and no
more than the documented number of earlier files is kept

**Given** `docs/install.md` and `deferred-work.md`'s R8 entry
**When** the story is done
**Then** install.md states the limit and what is kept, and R8 carries a `resolved:` line naming
this story
