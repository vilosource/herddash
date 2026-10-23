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
**FRs covered:** none new; hardens ADR-0009, FR-10, FR-1/2 (Board drawing) and PRD §8 Q1

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
