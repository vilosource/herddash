# Epic 1 Context: See every agent's status, live, from the terminal

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

The operator runs `herddash serve` and watches every Agent's Status changes print to the terminal as they happen, each labelled with its Profile, whether or not a herdr terminal client is attached. This epic lays the foundation everything else builds on: the Go module and its enforced package layout, CI, configuration, the herdr socket client, reconnect and resync behaviour, the version gate and Profile resolution. The protocol questions that shaped the client are already answered against a real herdr 0.9.5 binary. Nothing is stored yet; Epic 2 adds the persisted record, and Epic 3 adds the Board and API.

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
- **Socket discovery follows herdr's order:** `--session`, `HERDR_SOCKET_PATH`, `HERDR_SESSION`, then `~/.config/herdr/herdr.sock`. Must work under launchd/systemd, which set no herdr variables.
- **Host and Server resolution, and config defaults:** Host is the configured name, else the OS hostname; Server is `default`, or `<name>` from `sessions/<name>/herdr.sock`; both are resolved by one `internal/config` function (Story 1.2). Config defaults and the Profile Map shape are in `docs/config.example.toml`.
- **Reconnect forever** with capped backoff; herdr being absent, restarting or handing off is normal operation, logged as such, not as an error. A herdr restart prints nothing for an Agent whose Status is unchanged.
- **Resync** (fresh snapshot replacing state) whenever the engine cannot guarantee it saw every event; disagreements surface as snapshot-derived changes. Snapshot load on herdr is bounded by the reconcile timer and a minimum resync interval, one resync at a time.
- **Version gate:** herdr 0.9.5+ is `ok`. Older is `refused` (no AgentViews, nothing emitted as a Transition, log names version and minimum) unless the dogfooding flag is set, which makes it `advisory`, warns that the resync guarantee does not hold below 0.9.0, and carries the herdr version with every Transition. Version comes from `ping`'s `version`, never the `protocol` integer.
- **Profile attribution:** Claude Code resolves via `CLAUDE_CONFIG_DIR`; unset maps to the Profile Map's named absent case; set-but-unmatched or a timeout yields `unresolved`; a program with no rule (Codex, Antigravity) shows its program name, never duplicated in the heading. New Agent Programs get environment-variable rules through config alone.
- **Owner-only data:** umask `077`, data directory `0700`.
- **Hard prohibitions:** never call `pane.focus`/`agent.focus`; never read pane output in v1; never touch `~/.config/herdr/config.toml`; never install as a herdr plugin; tests never touch a live herdr.
- CI passes `go build`, `go vet`, `go test` (with `-race`), and `gofmt -l .` prints nothing.

## Technical Decisions

- **Permitted imports (enforced by a test):** cmd → engine, httpapi, importer, config, store; engine → core, herdr, procenv, store; httpapi → core, engine, store; importer → core, store, config; store → core; herdr → core; config → core. `core` imports only the standard library and never reads the clock. Changing this needs an ADR.
- **Pinned dependencies (no others without an ADR):** `github.com/BurntSushi/toml` v1.6.0; `golang.org/x/sys` v0.49.0 for darwin only; later `modernc.org/sqlite` v1.60.1 with `modernc.org/libc` exactly v1.77.1. No blanket `go get -u`.
- **Vocabulary:** PRD glossary terms are Go identifiers verbatim; herdr wire names stay inside `internal/herdr`, mapped to core types at its edge.
- **Agent identity** is `AgentKey{Host, Server, Epoch, PaneID}`, never pane id alone. Epoch changes only on evidence that herdr reassigned pane ids; epoch 0 is reserved for backfill.
- **Single-writer engine actor:** only the engine goroutine holds the pane set and current state; others reach it by message.
- **The engine decides, `internal/herdr` only does.** The client provides dial, ping, snapshot, subscribe, unsubscribe, `pane.process_info` and stream-health signals. The engine owns the pane set, reconcile timer, resync guard and minimum interval, version gate and epoch detection.
- **Socket protocol facts (confirmed on 0.9.5):**
  - One request per connection: the server answers the first line and closes. Fresh dial per call, no pooling.
  - Each Agent pane's `pane.agent_status_changed` subscription gets its own long-lived connection; lifecycle kinds share one. Wait for `subscription_started`. Subscriptions deliver live events only, never a replay.
  - Subscribe is all or nothing: one unknown `pane_id` fails the whole request (`pane_not_found`, request id echoed) and the server closes the connection.
  - Lifecycle events use an underscored envelope (`pane_agent_detected`), pane-scoped ones a dotted one (`pane.agent_status_changed`); decode both.
  - Unknown method is `invalid_request`. herdr error codes are an open set: unknown means resync, never crash. `events_lost` is still unreproduced and absent from the schema, so match it on the raw `error.code`.
  - The API exposes no focus method and no pane-output method; a test asserts no non-`gen/` code builds such requests.
- **Wire types** are generated by genwire (standard library only) via `go generate ./internal/herdr/gen` from the committed 0.9.5 schema dump.
- **Ordering of operations:** subscribe before snapshotting, at start and for every new pane. Snapshot is truth for which panes exist and their Status. A status event is recorded at once as a `live` Transition. Buffered events are never replayed onto a snapshot; events arriving during a snapshot read trigger another read.
- **Resync triggers:** `events_lost`, an unknown error code, a rejected subscribe, or a dropped subscription.
- **Ordering and time:** UTC Unix milliseconds; `observed_at` is the Daemon's clock, and order within an Agent is `observed_at`, then engine record order. Live events carry no herdr sequence; their order is arrival on the pane's own connection. herdr's `state_change_seq` (one server-wide counter, on snapshot agent records only, restarting at 1 with each server instance) is kept as evidence for snapshot-derived Transitions and compared only within one instance.
- **Restart evidence (for epoch detection):** herdr names no server instance; `ping` is identical across restarts. A new instance shows as EOF on every open connection, a new `terminal_id` for every pane, and `state_change_seq` restarting. With a saved session, pane ids survive (gaps and next-number counters kept) but agents do not until re-detected; without one, ids restart at `w1:p1`. The rule must never keep an epoch on a pane-id match alone, must not split every Agent because one pane closed while disconnected, and has no persisted pane-to-`terminal_id` map across a Daemon restart until milestone 2. `server.live_handoff`'s effect on `terminal_id` is untested.
- **Profile resolution** goes through `internal/procenv` before an Agent's first Transition, with a bounded timeout. The foreground process comes from `pane.process_info` (shell pid, foreground group, each foreground process's pid, name, argv0, argv, cwd). Re-resolve on `pane.agent_detected`; starting another program or changing directory emits no event, so other foreground changes are seen only by calling `pane.process_info` again. Linux reads `/proc/<pid>/environ`; darwin uses `kern.procargs2` via x/sys in a darwin-only file.
- **Config:** one immutable struct, fail-fast validation, applied only on restart; flag beats file beats default.
- **Tunables** (reconcile interval, minimum resync interval, backoff limits, Profile timeout) are each a named constant in one place, chosen when first built and documented with the config.
- **Logging:** `log/slog` JSON to `herddash.log` in the data directory. Never log environment values except a matched `CLAUDE_CONFIG_DIR`; Pane Titles only at debug. Adapters wrap errors with `%w`.
- **Values:** Status strings are herdr's verbatim (`working`, `blocked`, `done`, `idle`, `unknown`); provenance is `live`, `snapshot` or `backfill`.
- **Testing:** core is table-tested with no I/O; the client is tested against a fake socket server; the engine against a fake herdr client that injects each signal.

## Cross-Story Dependencies

- **1.1, 1.2 and 1.3 are done.** Their findings (one request per connection, restart signals, `pane.process_info` and `pane.agent_detected`, `state_change_seq`) are recorded in `docs/research/herdr-socket-api.md` under "Findings against herdr 0.9.5", re-runnable with its probe script. A later finding that contradicts the record or the spine amends them in the same change.
- **1.4 underlies 1.5 to 1.9;** 1.5's engine is extended by 1.6 (reconnect, epoch), 1.7 (resync) and 1.8 (gate). 1.6 owns deciding and testing the epoch rule.
- **1.9 depends on 1.4** providing `pane.process_info` and the `pane.agent_detected` event as core types.
- **Into later epics:** the herdr version kept per Transition, the live/snapshot provenance and `state_change_seq` feed Epic 2's record. The tracked project directory, workspace and tab labels, workspace order and Pane Title feed Epic 3's Context Line and sort key. The gate's Board strip lands in Epic 3.
- **Machine reality:** the operator's herdr binary is now 0.9.5, but the live server still runs 0.8.2 until it is restarted, so dogfooding against it runs behind the advisory flag. Any 0.9.5 testing uses an isolated server (own HOME/session), never the live one.
