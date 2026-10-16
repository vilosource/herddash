# Epic 1 Context: See every agent's status, live, from the terminal

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

The operator runs `herddash serve` and watches every Agent's Status changes print to the terminal as they happen, each labelled with its Profile, whether or not a herdr terminal client is attached. This epic lays the foundation everything else builds on: the Go module and its enforced package layout, CI, configuration, the herdr socket client, reconnect and resync behaviour, the version gate, Profile resolution, and the four open protocol questions answered against a real herdr 0.9.5 binary. Nothing is stored yet; Epic 2 adds the persisted record, and Epic 3 adds the Board and API.

## Stories

- Story 1.1: Project skeleton, import rules and CI
- Story 1.2: Configuration, data directory and socket discovery
- Story 1.3: Milestone-1 findings against a herdr 0.9.5 binary
- Story 1.4: herdr socket client mechanics
- Story 1.5: Watch every Agent's Status live
- Story 1.6: Reconnect forever, and know when herdr restarted
- Story 1.7: Resync when the stream cannot be trusted
- Story 1.8: Refuse unsupported herdr, with a dogfooding override
- Story 1.9: Know which profile each agent runs under

## Requirements & Constraints

- **One binary, one TOML config**, runnable with only the Profile Map configured. Static builds with `CGO_ENABLED=0` for darwin/arm64, darwin/amd64, linux/amd64, linux/arm64.
- **Socket discovery follows herdr's order:** `--session`, `HERDR_SOCKET_PATH`, `HERDR_SESSION`, then `~/.config/herdr/herdr.sock`. Must work under launchd/systemd.
- **Reconnect forever** with capped backoff; herdr being absent, restarting or handing off is normal operation, logged as such, not as an error. A herdr restart prints nothing for an Agent whose Status is unchanged.
- **Resync** (fresh snapshot replacing state) whenever the engine cannot guarantee it saw every event; disagreements surface as snapshot-derived changes. Snapshot load on herdr is bounded by the reconcile timer and a minimum resync interval, one resync at a time.
- **Version gate:** herdr 0.9.5+ is `ok`. Older is `refused` (nothing emitted as a Transition, log names version and minimum) unless the dogfooding flag is set, which makes it `advisory`, warns that the resync guarantee does not hold below 0.9.0, and carries the herdr version with every Transition. Version comes from `ping`, never the `protocol` integer.
- **Profile attribution:** Claude Code resolves via `CLAUDE_CONFIG_DIR`; unset maps to the Profile Map's named absent case; set-but-unmatched or a timeout yields `unresolved`; a program with no rule (Codex, Antigravity) shows its program name, never duplicated in the heading. New Agent Programs get environment-variable rules through config alone.
- **Owner-only data:** umask `077`, data directory `0700`.
- **Hard prohibitions:** never call `pane.focus`/`agent.focus`; never read pane output in v1; never touch `~/.config/herdr/config.toml`; never install as a herdr plugin; tests never touch a live herdr.
- CI passes `go build`, `go vet`, `go test`, and `gofmt -l .` prints nothing.

## Technical Decisions

- **Module** `github.com/vilosource/herddash`, `go 1.26`. Packages: `cmd/herddash` and `internal/{core,engine,herdr,procenv,store,httpapi,importer,config}`. Story 1.1 starts with no dependencies.
- **Permitted imports (enforced by a test):** cmd → engine, httpapi, importer, config, store; engine → core, herdr, procenv, store; httpapi → core, engine, store; importer → core, store, config; store → core; herdr → core; config → core. `core` imports only the standard library.
- **Dependencies, all pinned (no others without an ADR):** `github.com/BurntSushi/toml` v1.6.0; `golang.org/x/sys` v0.49.0 for darwin only; later `modernc.org/sqlite` v1.60.1 with `modernc.org/libc` exactly v1.77.1, upgraded together. No blanket `go get -u`.
- **Core is pure:** domain types (`AgentKey`, `Status`, Agent Program, `Transition` built only via `core.NewTransition`) live in `internal/core`; time-dependent functions take a clock and never call `time.Now()`. Go identifiers use the PRD glossary terms verbatim; herdr wire names stay inside `internal/herdr`.
- **Agent identity** is `AgentKey{Host, Server, Epoch, PaneID}`, never pane id alone. Host is the configured name, else OS hostname; Server is `default` for the default socket, `<name>` for `sessions/<name>/herdr.sock`. One `internal/config` function resolves both. Epoch changes only on evidence that pane ids were reassigned; epoch 0 is reserved for backfill.
- **Single-writer engine actor:** only the engine goroutine holds the pane set and current state; others reach it by message.
- **The engine decides, `internal/herdr` only does.** The client provides dial, ping, snapshot, subscribe, unsubscribe and stream-health signals. The engine owns the pane set, reconcile timer, resync guard and minimum interval, version gate and epoch detection.
- **herdr client rules:** only `internal/herdr` opens the socket. One request per connection, fresh dial each call, no pooling. Each Agent pane's `pane.agent_status_changed` subscription gets its own long-lived connection; lifecycle kinds (`pane.created`, `pane.closed`, `pane.agent_detected`) share one. Wait for `subscription_started`. Decode both the underscored lifecycle envelope and the dotted pane-scoped envelope into core types at the package edge. The API has no focus method and no pane-output method, and a test asserts no non-`gen/` code builds such requests.
- **Ordering of operations:** subscribe before snapshotting, at start and for every new pane. Snapshot is truth for which panes exist and their Status. Status events are live observations. Buffered events are never replayed onto a snapshot; events arriving during a snapshot read trigger another read.
- **Resync triggers:** `events_lost`, an unknown error code, a rejected subscribe, or a dropped subscription. herdr error codes are an open set: unknown means resync, never crash.
- **Wire types** generated from `herdr api schema --json` of a 0.9.5 binary into `internal/herdr/gen` by a documented, repeatable command; generator chosen in Story 1.3.
- **Config:** one TOML file parsed into one immutable struct, validated fail-fast (unknown key or bad value exits non-zero naming it), applied only on restart; flag beats file beats default. Defaults: `~/.config/herddash/config.toml`, data dir `$XDG_DATA_HOME/herddash` else `~/.local/share/herddash`, loopback bind, Retention 90 days. Profile Map entries name a rule type plus its parameters (for the env-var rule: variable name, value-to-Profile map, absent-case name). Ship a documented example config.
- **Profile resolution** goes through `internal/procenv` before an Agent's first Transition, with a bounded timeout; re-resolved on agent detection and foreground-process change. Linux reads `/proc/<pid>/environ`; darwin uses `kern.procargs2` via x/sys in a darwin-only file.
- **Tunables** (reconcile interval, minimum resync interval, backoff limits, Profile timeout) are each a named constant in one place, chosen when first built and documented with the config.
- **Logging:** `log/slog` JSON to `herddash.log` in the data directory. Never log environment values except a matched `CLAUDE_CONFIG_DIR`; Pane Titles only at debug. Adapters wrap errors with `%w`.
- **Time and values:** UTC Unix milliseconds; Status strings are herdr's verbatim (`working`, `blocked`, `done`, `idle`, `unknown`); provenance is `live`, `snapshot` or `backfill`.
- **Testing:** core is table-tested with no I/O; the client is tested against a fake socket server; the engine against a fake herdr client that injects each signal.

## Cross-Story Dependencies

- **1.1 first:** every story lands in its package layout and uses its core types.
- **1.3 gates 1.4, 1.6 and 1.9:** it answers whether one connection carries one request on 0.9.5, whether pane ids survive a herdr restart and what marks a new server instance (needed for epoch detection in 1.6), which call yields a pane's foreground process and which event reports a change (needed by 1.4 and 1.9), and which field orders Transitions. It runs herdr 0.9.5 in isolation (own session and config dir), never against the operator's live 0.8.2 server. Each finding amends `docs/research/herdr-socket-api.md` (UNVERIFIED to confirmed or corrected) in the same change, and any contradiction updates the affected architecture rule or ADR in that change too.
- **1.2 feeds 1.4 onward:** socket path, Host and Server resolution, the dogfooding flag and the Profile Map.
- **1.4 underlies 1.5 to 1.9;** 1.5's engine is extended by 1.6 (reconnect, epoch), 1.7 (resync) and 1.8 (gate).
- **Into later epics:** the herdr version kept per Transition and the live/snapshot provenance feed Epic 2's record. The tracked project directory, workspace and tab labels, workspace order and Pane Title feed Epic 3's Context Line and sort key. The gate's Board strip lands in Epic 3.
- **Machine reality:** the operator's herdr is 0.8.2 and cannot be upgraded now, so dogfooding runs behind the advisory flag.
