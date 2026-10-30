- source_spec: `docs/implementation/spec-1-1-project-skeleton-import-rules-and-ci.md`
  summary: CLAUDE.md should say `go run ./cmd/herddash version` prints `herddash dev (unknown)` and that release ldflags stamping is not wired until Story 4.3's release workflow.
  evidence: Story 1.1 review finding 15; the current text says both `(<commit>)` and `unknown`, and no workflow stamps -X main.version yet.
  resolved: 2026-10-23, Story 5.8 (`docs/implementation/spec-5-8-documents-agents-read-match-the-code.md`). CLAUDE.md's "Build and test" now says `go run ./cmd/herddash version` prints `herddash dev (unknown)`, a plain `go build` prints `herddash dev (<12 hex digits>)` with `-dirty` for a modified tree, and release builds stamp `-s -w -X main.version -X main.commit` through `scripts/release-build.sh` (wired by Story 4.3).
- source_spec: `docs/implementation/spec-1-2-configuration-data-directory-and-socket-discovery.md`
  summary: Story 2.1 should assert created database, WAL, SHM, lock and log files are 0600, which pins the umask set in main.
  evidence: Story 1.2 review; `syscall.Umask(0o077)` in main has no test because no file is created yet.
  resolved: 2026-10-19, Story 2.1 (`f5710db`, `docs/implementation/spec-2-1-a-local-database-herddash-owns.md`); marked by Story 5.8. `store.Open` makes the database, WAL, SHM and lock files 0600 whatever the umask (`internal/store/store.go`, `takeLock` and the file creation in `Open`), and `TestFileModes` and `TestOpenTightensExistingFiles` assert it. The umask line in `cmd/herddash/main.go` itself still has no direct test; that remainder is the Epic 1 retrospective entry below.
- source_spec: `docs/implementation/spec-1-2-configuration-data-directory-and-socket-discovery.md`
  summary: Story 2.1 must ensure a `database` path outside `data_dir` has an owner-only parent directory.
  evidence: Story 1.2 review; EnsureDataDir covers only data_dir, and FR-17 requires owner-only storage.
  resolved: 2026-10-19, Story 2.1 (`f5710db`); marked by Story 5.8. `store.Open` requires an owner-only parent directory for any `database` path, creating it 0700 when missing (`internal/store/store.go`); `TestOpenRefusesLooseParent` proves the refusal.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: CLAUDE.md should warn that a plain `herdr` command run from a pane here is a 0.9.5 client talking to the 0.8.2 live server through the inherited HERDR_SOCKET_PATH, and wrap its rewritten open-questions sentence.
  evidence: Homebrew's /opt/homebrew/bin/herdr is 0.9.5 while PID 1560 runs the 0.8.2 binary (lsof, 2026-10-16); ad-hoc CLI output is evidence for neither version. Agent-context file, so deferred per triage rule.
  resolved: 2026-10-23, Story 5.8. Obsolete in its 0.8.2 form: the live herdr has run 0.9.5 since its 2026-10-20 restart, so client and server versions match. CLAUDE.md's "Things that will bite you" now says a plain `herdr` command typed in a pane is a client of the live server, and the open-questions sentence was rewrapped.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: Nothing enforces that internal/herdr and its gen/genwire use only the standard library.
  evidence: arch_test.go's stdlibOnly holds only core; widening it changes the arch test's permitted set, which CLAUDE.md says needs an ADR.
  resolved: Story 5.5, by ADR-0011; arch_test.go's stdlibOnly now holds herdr, which covers gen and genwire, and the fixture breaks it in internal/herdr and genwire.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: internal/engine may import internal/herdr/gen directly, so wire types can leak past the herdr package edge (AD-5, ADR-0006).
  evidence: arch_test.go maps internal/herdr/** to one node; confining gen to internal/herdr is an arch-test change needing an ADR.
  resolved: Story 5.5, by ADR-0011; arch_test.go's checkTree reports any non-test import of internal/herdr/gen or a package under it from outside internal/herdr, and the fixture's internal/engine/leak.go proves it.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: Story 1.5 should decide whether core.NewTransition rejects a HerdrOrder on a live or backfill Transition.
  evidence: Finding Q3 makes state_change_seq snapshot-only and per server instance; the field comment says nil for live but nothing enforces it, and enforcing adds a public sentinel to core.
  resolved: Story 5.5; core.NewTransition rejects a HerdrOrder unless provenance is snapshot, with the sentinel core.ErrInvalidHerdrOrder (internal/core/types.go), matching migration 0001's CHECK.
- source_spec: `docs/implementation/spec-1-5-watch-every-agent-s-status-live.md`
  summary: Bound the snapshot → subscribe → reject loop when herdr keeps listing a pane it refuses to subscribe (Story 1.7's minimum resync interval).
  evidence: onSubscribed on a known pane requests a snapshot and apply resubscribes at once; nothing limits the rate if the rejection persists.
  resolved: 2026-10-19, Story 1.7 (`88fb175`); marked by Story 5.8. `defaultResyncMinInterval` (5 s, `internal/engine/tunables.go`) also bounds how often a pane herdr keeps rejecting is subscribed again; `TestResyncPersistentRejection` proves it.
- source_spec: `docs/implementation/spec-1-5-watch-every-agent-s-status-live.md`
  summary: Decide whether a pane that herdr moves (new pane id, previous_pane_id) keeps its Agent identity and history, or is a removal plus a new Agent.
  evidence: AD-3 keys identity on pane id; Story 1.5 prints a move as removal plus new Agent; ids are workspace-scoped, so a cross-workspace move changes the id (socket API record).
  resolved: 2026-10-23, decided by the operator for Epic 5 (recorded in `docs/planning/epics.md`'s Epic 5 preamble and vfkb `c56d5199f84c`): a move is a removal plus a new Agent. AD-3's identity rule is unchanged, so no ADR was needed; it only gained a paragraph saying so. Story 5.8 documents it in the spine's AD-3 and in `TestPaneMoved`'s comment (`internal/engine/engine_test.go`).
- source_spec: `docs/implementation/spec-1-6-reconnect-forever-and-know-when-herdr-restarted.md`
  summary: Tell consumers when herdr is lost: serve prints nothing until it reconnects, and Engine.Agents() keeps returning the last Statuses as if current.
  evidence: Only Sink.Reconnected exists; the spine's ConnectionTruth (connected, last_snapshot_at, gate) is the Epic 3 vehicle for this.
  resolved: 2026-10-20, Story 3.9 (`6fce275`); marked by Story 5.8. The Board's connection strip shows herdr lost from ConnectionTruth's `connected`, and the engine logs the loss at Info. Story 5.12 made repeated snapshot failures after start count as lost too (`internal/engine/connection_test.go`). The terminal printer of `serve` still prints no "lost" line; the Board superseded it (Epic 1 retrospective A6, accepted).
- source_spec: `docs/implementation/spec-1-7-resync-when-the-stream-cannot-be-trusted.md`
  summary: Document the engine tunables (snapshot, reconnect, resync and reconcile intervals) with the configuration, as the spine's Deferred list says.
  evidence: Story 1.7 review; they exist only as commented constants in internal/engine/tunables.go, and Story 1.6's backoff values were not documented either.
  resolved: 2026-10-23, Story 5.8. Every engine tunable, the event stream's keepalive and the Board's silence bound are listed under "Fixed timings" at the end of `docs/config.example.toml`, which `docs/how-herddash-works.md` points to; the spine's Deferred list no longer names them.
- source_spec: `docs/implementation/spec-1-9-know-which-profile-each-agent-runs-under.md`
  summary: Run the test suite on a macOS runner in CI so procenv's darwin reader (kern.procargs2) is exercised automatically.
  evidence: Story 1.9 review; ci.yml tests only on ubuntu-latest, so the reader the operator's Macs depend on is only vetted and cross-compiled there.
  resolved: 2026-10-23, Story 5.7 (`docs/implementation/spec-5-7-ci-tests-on-macos.md`). CI's `macos` job (`.github/workflows/ci.yml`) runs `go test -race ./...` on macos-latest with go.mod's Go, so procenv's darwin tests run on every pull request. Run locally on macOS (Go 1.26.5 and 1.27.2, `CI=true`, clean clone): every package passes; the first pull-request run proves it on macos-latest.
- source_spec: `docs/implementation/spec-1-9-know-which-profile-each-agent-runs-under.md`
  summary: Decide whether a Profile resolution that failed transiently (timeout, herdr call error) should be retried, rather than leaving the Agent unresolved until the next agent detection.
  evidence: Story 1.9 review, unverified (medium if real); settle it by counting "profile resolved" debug entries with an error in dogfooding logs.
  resolved: 2026-10-23, Story 5.4 (`docs/implementation/spec-5-4-retry-a-profile-lookup-that-failed-transiently.md`); marked by Story 5.8's final pass. A lookup that ran out of `profileTimeout` or whose foreground process call failed is retried `defaultProfileRetries` (3) times, first after `defaultProfileRetryDelay` (2 s) and doubling (`internal/engine/tunables.go`); a rule outcome or an unreadable environment is never retried, and a known Agent keeps a Profile resolved for its program. The operator log showed no transient failure in two days, so the retry is insurance. Story 5.18 bounded the row hold it added.
- source_spec: `docs/implementation/spec-1-9-know-which-profile-each-agent-runs-under.md`
  summary: CLAUDE.md's "Repository state" should say internal/procenv is built, golang.org/x/sys v0.49.0 is a dependency, and go.mod declares go 1.26.0.
  evidence: Story 1.9 review; agent-context file, so deferred per the triage rule.
  resolved: 2026-10-23, Story 5.8. CLAUDE.md's "Repository state" describes `internal/procenv` and names `go 1.26.0` and the three direct dependencies, `golang.org/x/sys` v0.49.0 among them.
- source_spec: `docs/implementation/spec-2-1-a-local-database-herddash-owns.md`
  summary: Persist the server epoch and the pane-to-terminal_id map so a Daemon restart does not restart at epoch 1 and splice a new herdr instance's Agents onto stored epoch-1 history (AD-3 hazard 3).
  evidence: Story 2.1 review; Story 1.6 keeps both in memory, no Epic 2 story names persisting them, and Story 2.2 starts writing rows keyed by epoch. To be built with Story 2.2.
  resolved: 2026-10-19, Story 2.2 (`b72f192`); marked by Story 5.8. The epoch and the pane-to-`terminal_id` map are saved in `server_state` (`internal/store/migrations/0002_server_state.sql`) before a snapshot's rows and loaded at start. Story 5.8 updated the spine's AD-3 and amended ADR-0005: `server_state` is engine state, not the append-only record.
- source_spec: `docs/implementation/spec-2-2-record-every-transition.md`
  summary: Story 2.3 must record the epoch decided by the first snapshot after a Daemon start (the Reconnected report), since the daemon_start marker carries the epoch saved before that decision.
  evidence: Story 2.2 review; TestServeRecords' second run writes its rows under epoch 2 while its daemon_start marker says epoch 1.
  resolved: 2026-10-19, Story 2.3 (`f8ffd3f`, `docs/implementation/spec-2-3-gaps-are-recorded-not-hidden.md`); marked by Story 5.8. The first snapshot of a run writes a `connect` marker (`store.ReasonConnect`, `internal/engine/engine.go`) carrying the epoch that snapshot decided, after `daemon_start`.
- source_spec: `docs/implementation/spec-3-7-the-board-renders-every-agent.md`
  summary: Story 3.8 must reopen the stream when EventSource closes after a non-200 /events reply (503 during a Daemon restart), and keep keyboard focus across re-renders by AgentKey.
  evidence: EventSource does not retry after a non-200 response; Story 3.7's render replaces every Box on each state event.
  resolved: 2026-10-20, Story 3.8 (`6198ea8`); marked by Story 5.8. `order.mjs`'s stream state machine reopens the stream with backoff after EventSource gives up (a non-200 reply), and `board.mjs` updates only the Boxes concerned and gives focus back by AgentKey.
- source_spec: `docs/implementation/spec-3-8-the-board-stays-live.md`
  summary: Story 3.9's connection strip must also show when the Board itself has lost the Daemon's /events stream (it is reconnecting), not only herdr disconnected per ConnectionTruth.
  evidence: ConnectionTruth comes from the Daemon, so it cannot report the Daemon down; while the Board waits out its backoff (up to 30 s) it shows the last model with ticking Waiting Times and no sign it is stale.
  resolved: 2026-10-20, Story 3.9 (`6fce275`); marked by Story 5.8. While the Board has lost the Daemon's stream it shows `LOST_TEXT` ("Lost contact with herddash. Reconnecting…") and dims (`streamLost`, `order.mjs`). Story 5.11 extended it to a stream that stays open but silent (ADR-0012).
- source_spec: `docs/implementation/spec-3-9-connection-and-version-strips.md`
  summary: Run the Board's headless-browser check (board.mjs drawing: strips, dimming, refused hiding, live updates, reconnect) in CI, not only by hand.
  evidence: Stories 3.7-3.9 each verified board.mjs only in a manual headless-Chrome run; node --test covers order.mjs alone. ubuntu runners ship Chrome, and Node 22's built-in WebSocket can drive it over CDP without npm, but wiring it into CI is work no story plans.
  resolved: 2026-10-23, Story 5.6 (`docs/implementation/spec-5-6-ci-runs-the-board-s-browser-check.md`). CI's `board-browser` job (`.github/workflows/ci.yml`) builds `./cmd/herddash` and runs `board-browser.mjs` on ubuntu-latest with the runner's Node and its `google-chrome`, no npm and no extra action, and fails when Node is older than 22 or no Chrome is found. The job's step was run locally (macOS Chrome 156, Node 22): 23 `ok`, exit 0; the first pull-request run proves it on ubuntu-latest.
- source_spec: `docs/implementation/spec-4-1-clean-shutdown.md`
  summary: CLAUDE.md's operator-checkpoint list still says Story 1.3 must avoid "the live 0.8.2 server"; the live herdr has run 0.9.5 since 2026-10-20.
  evidence: The checkpoint is closed and the server was restarted on 0.9.5 (PID 56666, loaded binary checked via lsof); the line now misleads a reader about the dogfooding flag. Agent-context file, so deferred.
  resolved: 2026-10-23, Story 5.8. CLAUDE.md's checkpoint list marks Story 1.3's checkpoint closed and no longer mentions a live 0.8.2 server.
- source_spec: `docs/implementation/spec-4-2-start-with-my-session-on-macos-and-linux.md`
  summary: Run deploy_test.go's `plutil -lint` check in CI on a macOS runner; today it runs only on a developer's Mac.
  evidence: CI's test job is ubuntu-latest only, so TestLaunchdAgentLints always skips there; the strict Go decoder in TestLaunchdAgent catches most malformed plists on Linux.
  resolved: 2026-10-23, Story 5.7 (`docs/implementation/spec-5-7-ci-tests-on-macos.md`). CI's `macos` job runs the suite with `HERDDASH_REQUIRE_TOOLS: plutil`, so `TestLaunchdAgentLints` fails rather than skips if plutil is missing; locally, `HERDDASH_REQUIRE_TOOLS=plutil go test -run TestLaunchdAgentLints -v .` passes, not skipped, and `HERDDASH_REQUIRE_TOOLS=systemd-analyze` makes `TestSystemdUnitVerifies` fail on macOS naming the variable.
- source_spec: `docs/implementation/spec-transcript-background.md`
  summary: A task whose start is in a transcript but whose end never arrives (Claude Code exiting or crashing with tasks running, then the session resumed) may keep an Agent BACKGROUND until the start falls out of an 8 MiB re-read window; nothing ages tasks out, and the process table only ever adds to the count, never proves an open command gone.
  evidence: Unverified (would be medium). On 2026-10-22 no transcript held more than one sessionId, so whether a resumed session copies earlier task starts into its new file is unknown. Settle it by resuming a session that had a background command open at exit and reading the new transcript's markers; if starts are copied, drop an open command when the process check finds no tool shell for some time.
  resolved: 2026-10-23, Story 5.2 (`docs/implementation/spec-5-2-a-resumed-session-does-not-keep-finished-background-tasks.md`). On Claude Code 2.1.292 a resumed session copies no starts into a new file: it appends to the same transcript and, as it starts, enqueues a `stopped` notification for every task with no completion record (`docs/research/claude-code-resume-background-tasks-2026-10-23.md`). `stopped` now closes a task (ADR-0009 amended); no time-based drop was added.
- source_spec: `docs/implementation/spec-transcript-background.md`
  summary: Whether `sessions/<pid>.json` updates `sessionId` when `/clear` starts a new session in the same process is not known; if it does not, the old transcript's open tasks stay on the Box.
  evidence: Unverified (would be medium). Settle it by running `/clear` in a session with a background task open and reading the record's sessionId before and after.
  resolved: 2026-10-23, Story 5.3 (`docs/implementation/spec-5-3-clear-drops-the-previous-session-s-background-tasks.md`). On Claude Code 2.1.292 `/clear` changes the same process's record to a new `sessionId` at once, and a new transcript begins with the `/clear` record; a background command started before it keeps running and its completion goes to the new transcript (`docs/research/claude-code-clear-background-tasks-2026-10-23.md`). The new id names another path, so the engine's tail restarts there and open tasks and the recap start afresh; `TestTranscriptCleared`, `TestTranscriptClearedRecap` and `TestTranscriptClearedNotWritten` prove it. No code change was needed (ADR-0009 amended).
- source_spec: `docs/implementation/spec-claude-recap.md`
  summary: The recap row's layout (the board--recaps toggle, the taller grid, the two-line clamp) is verified only by the hand-run board-browser.mjs; run it in CI.
  evidence: Removing the toggle or the .board--recaps rule leaves every CI check green; same root as the Story 3.9 deferral to run the browser check in CI.
  resolved: 2026-10-23, Story 5.6 (`docs/implementation/spec-5-6-ci-runs-the-board-s-browser-check.md`). CI's `board-browser` job runs `board-browser.mjs` on every pull request. Its step, run locally (macOS Chrome 156, Node 22), passed with 23 `ok`; with the `.board--recaps .box` rule deleted from `board.css` and the binary rebuilt, "while a Box has a recap, every Box is one line taller" printed FAIL and it exited 1. The first pull-request run proves it on ubuntu-latest.
- source_spec: `docs/implementation/spec-5-2-a-resumed-session-does-not-keep-finished-background-tasks.md`
  summary: Between a Claude Code crash and a resume inside herdr, whether herddash's session watch for the dead process ends, so its open start stops showing, is untested.
  evidence: Unverified (would be medium). The 2026-10-23 experiment ran outside herdr. Settle it by crashing a Claude Code Agent in an isolated herdr and watching its Box.
- source_spec: `docs/implementation/spec-5-2-a-resumed-session-does-not-keep-finished-background-tasks.md`
  summary: What a resume writes for a background subagent left open, and whether `--continue` behaves like `--resume`, were not observed; a status other than stopped would leave the subagent open.
  evidence: Unverified (would be medium). Only shell commands and `--resume` were tested on 2.1.292. Settle it by repeating the SIGKILL-and-resume experiment with a background Agent task and with `--continue`.
- source_spec: `docs/implementation/spec-5-3-clear-drops-the-previous-session-s-background-tasks.md`
  summary: Whether a background subagent started before `/clear` keeps running after it is not known; if it does, it leaves the Box while still working, since it has no tool shell for the process check to find.
  evidence: Unverified (would be medium). The 2026-10-23 `/clear` experiment on 2.1.292 used shell commands only. Settle it by repeating it with a background Agent task and watching whether its completion reaches the new transcript.
- source_spec: `docs/implementation/epic-1-retro-2026-10-23.md`
  summary: Low Epic 1 items, grouped (retro L8, L9, A6). Profile reads use `e.runCtx`, not the connection's context, so a `process_info` call can outlive a gate refusal or disconnect by up to `profileTimeout`. A snapshot first sight stores a Status outside the five without `Valid()`. A set-but-empty variable gives `unresolved`, not the absent case. Profile Map keys match by exact case. A literal `~` in an observed value never matches. The darwin `procargs2` parser is untested with an empty argv[0]. Profile names may equal program names. `syscall.Umask(0o077)` in `cmd/herddash/main.go` has no direct test.
  evidence: Epic 1 retrospective, deferred as low; each is a rare path with no observed effect (three days of production showed zero `unresolved` Profiles and zero errors). The abandonment in L8 is deliberate; only the context is questionable. The umask's effect on files is asserted by `store_test.go`'s `TestFileModes`, which sets its own umask.
- source_spec: `docs/implementation/epic-2-retro-2026-10-23.md`
  summary: A failed `SaveServerState` does not stop the rows that follow it, so rows can be keyed by an epoch the store does not hold (retro F-2).
  evidence: `saveState` logs and returns (`internal/engine/recorder.go`), and `onSnapshot` goes on to mark and apply. Low: Story 2.3's triage #13 rejected it once ("logged and retried at the next snapshot"); the retro records that Story 2.2's commit message claimed more than the code does.
- source_spec: `docs/implementation/epic-2-retro-2026-10-23.md`
  summary: A first-sight row can be stamped `live` at a snapshot's time, so its Waiting Time shows as exact when it is a lower bound (retro F-4).
  evidence: `seen` restamps `seenAt` only when the Status changes but always overwrites `seenBy` (`internal/engine/engine.go`). How often it happens depends on whether herdr's status subscription repeats the current Status on subscribe, which is unchecked; checking needs an isolated herdr 0.9.5. Low until checked.
- source_spec: `docs/implementation/epic-2-retro-2026-10-23.md`
  summary: Retention deletes resync markers by age, including those inside a present Agent's kept span, so its history can lose a recorded gap (retro F-5).
  evidence: `sweepMarkers` is `DELETE FROM resync_markers WHERE observed_at < ?1` (`internal/store/sweep.go`). Low: only reachable for an Agent that stayed in one class longer than Retention (90 days by default).
- source_spec: `docs/implementation/epic-2-retro-2026-10-23.md`
  summary: When saved state fails to load, recording is off for the run and nothing on `/api/state` or the Board says so (retro F-6).
  evidence: `restore` sets `e.store = nil` and logs one error (`internal/engine/recorder.go`), Story 2.2's deliberate choice (triage #1). Low: never observed; the Daemon only looks healthier than it is.
  resolved: 2026-10-30, Story 7.1 (`docs/implementation/spec-7-1-say-when-recording-is-off.md`), per ADR-0016. The engine now loads its saved state in `New` (`load`, `internal/engine/recorder.go`), before the first View is stored, and every published `engine.View` carries an always-sent `recording` boolean, false for the whole run when the load failed; the Board draws a red recording strip after the connection and version strips while it is false (`boardStatus`, `internal/httpapi/web/order.mjs`). `TestRecordLoadFails` asserts no View a client could read says true.
- source_spec: `docs/implementation/epic-2-retro-2026-10-23.md`
  summary: The import summary does not say which Host and Server the rows were filed under, so a run from a herdr pane that inherited another `HERDR_SOCKET_PATH` is not noticed (retro F-7).
  evidence: Story 2.6's triage #2 made a corrected re-run possible, but the summary line names neither. Low: the import is a one-off per Host.
- source_spec: `docs/implementation/epic-2-retro-2026-10-23.md`
  summary: A Retention sweep skipped because a resync was running or scheduled has no catch-up; the next attempt is a full hour later (retro F-8).
  evidence: `sweep` returns when `resyncing || resyncScheduled` (`internal/engine/retention.go`). Low: matters only on a Host where resyncs keep landing on the tick.
- source_spec: `docs/implementation/epic-3-retro-2026-10-23.md`
  summary: `/api/state` and the static assets set no write deadline, and the HTTP server has no `IdleTimeout` (retro D7).
  evidence: `internal/httpapi/httpapi.go`, `board.go`, `cmd/herddash/serve.go`. Low: the server listens on loopback only.
- source_spec: `docs/implementation/epic-3-retro-2026-10-23.md`
  summary: The history endpoint is unbounded and orders by `observed_at, id` rather than append order (retro D10).
  evidence: `internal/httpapi/history.go`, `internal/store/record.go`. Deferred until something consumes it: nothing does yet, and Retention bounds the rows.
- source_spec: `docs/implementation/epic-3-retro-2026-10-23.md`
  summary: Workspace headers are computed in core, sent as the unused `headers` field of `/api/state`, and computed again in `order.mjs`; the wire field could be dropped (retro A3).
  evidence: `engine.View.Headers` (`internal/engine/view.go`) is never read by the Board. Harmless; dropping it changes the public JSON, so do it with the next change there.
- source_spec: `docs/implementation/epic-3-retro-2026-10-23.md`
  summary: `internal/httpapi/web/order.mjs` holds five concerns (ordering, formatting, the event reducer, the stream state machine, the strips) in one file of about 680 lines; split it.
  evidence: Epic 3 retrospective A4. Deferred to the next story that touches it: a split now would be churn with no behaviour change, and `node --test` and the browser check cover it either way.
- source_spec: `docs/implementation/epic-4-retro-2026-10-23.md`
  summary: `TestServeShutdownAndRestart` checks the Transition pushed before SIGTERM only `if printedBlocked`, so the "write in progress at shutdown" case is never forced (retro R5).
  evidence: `cmd/herddash/serve_test.go`. Low: the fake should hold a write open at the signal; production shows 19 clean stops with no error.
- source_spec: `docs/implementation/epic-4-retro-2026-10-23.md`
  summary: The systemd user unit has never run on a real Linux Host; verify it live on the first one the provisioning repository deploys to (retro S1).
  evidence: Story 4.2 was met on macOS in production and on Linux only statically (`systemd-analyze verify` in CI's `release-toolchain` job, `TestSystemdUnit*`). Deployment to Linux is the provisioning repository's job.
- source_spec: `docs/implementation/epic-4-retro-2026-10-23.md`
  summary: Neither `herddash.log` nor `herddash.stderr.log` is rotated; watch their size while dogfooding (retro R8).
  evidence: On the operator's Mac after three days, `herddash.log` was 123 KB and stderr 0 bytes. Rotation is still in the spine's Deferred list; act if the size grows.
- source_spec: `docs/implementation/spec-5-13-board-text-and-grouping-are-exact.md`
  summary: A session recap's bidi controls are not dropped and reach the Box's title and aria-label, which a `<bdi>` element cannot isolate.
  evidence: `core/transcript.go` `recapOf` only trims; `order.mjs` `boxLabel` puts the recap into the title and aria-label. Low: recap text comes from Claude Code's own model output, shown only with `claude_recaps` on. Recorded in Story 5.13's spec frontmatter; copied here by Story 5.8 so the ledger holds it.
- source_spec: `docs/implementation/spec-5-6-ci-runs-the-board-s-browser-check.md`
  summary: No branch protection or ruleset makes CI checks required, so a red CI job does not block a merge into `develop` or `main`.
  evidence: `gh api repos/vilosource/herddash/branches/develop/protection` returned 404 and the rulesets list was empty (Story 5.6 review). A repository setting for every job, outside any story; an operator decision if wanted. Recorded in Story 5.6's spec frontmatter; copied here by Story 5.8.
- source_spec: `docs/implementation/spec-5-4-retry-a-profile-lookup-that-failed-transiently.md`
  summary: No user-facing document described the Profile retry tunables or how rows are held while a Profile is resolved again.
  evidence: Story 5.4 review, low; it belonged with the tunables Story 5.8 documents. Recorded in Story 5.4's spec frontmatter; copied here by Story 5.8's final pass.
  resolved: 2026-10-23, Story 5.8's final pass. `docs/config.example.toml`'s "Fixed timings" lists the retries, their delay and the hold limit (2 x `profileTimeout`, Story 5.18), `docs/how-herddash-works.md`'s profile reader says what a retry and a hold do, and the spine's AD-14 states the rules.
- source_spec: `docs/implementation/spec-5-16-a-resumed-subagent-counts-as-background-work.md`
  summary: A resume of a subagent launched before `/clear` (in another session id's transcript) may never close, and so keep the Box's background count up, if its notification is written to the old transcript.
  evidence: Unverified (would be medium): all 89 resumes on this machine name a subagent launched earlier in the same transcript. Settle it with a throwaway cldp session outside herdr: launch a background subagent, let it finish, `/clear`, resume it with SendMessage, and see which transcript gets its notification; Story 5.3 saw a background command's completion go to the new transcript, which suggests it closes. Location: `internal/core/transcript.go` `taskStart`; ADR-0009's Story 5.3 and 5.16 amendments. Recorded in Story 5.16's spec frontmatter; copied here by Story 5.8's final pass.
- source_spec: `docs/implementation/spec-6-2-the-engine-acknowledges-a-finish.md`
  summary: A turn herddash did not observe still goes unnoticed by an acknowledgement when no herdr order was known for the finish: one learnt live and marked, then missed before a snapshot read its `state_change_seq`, or a `done` to `idle` recorded live after the mark.
  evidence: Epic 6 final review. `core.SeenAt` falls back to `core.Seen` with no order from the mark or the last row (`internal/core/acknowledgement.go`); the in-run check needs a known `herdrSeq` (`internal/engine/engine.go`, `missedTurn`). Live events carry no order (socket API record, finding Q3) and reconcile reads are 60 s apart. Low: needs a mark within that window and a whole turn missed after it. A fix would read a snapshot at mark time, or record herdr's order for live-learnt Statuses.
- source_spec: `docs/implementation/spec-6-2-the-engine-acknowledges-a-finish.md`
  summary: A whole turn herddash did not observe (Daemon down, or events lost between reads) records no Transition, so the later finish keeps the earlier one's Waiting Time and the record misses the turn.
  evidence: Epic 6 final review. `firstSight`'s matching path and the snapshot path compare Status alone; a higher `state_change_seq` with the same Status is now detected (`missedTurn`, `core.SeenAt`) but only ends an acknowledgement. Recording the gap (a resync-style marker, or a synthesised Transition pair) changes what the record says and needs an ADR. Low: before Epic 6 this was the only effect of the gap.
- source_spec: `docs/implementation/spec-6-8-the-board-shows-in-view-and-offers-mark-unseen.md`
  summary: The `● in view` tag's title promises "Leaving the pane after 3 s marks it seen" on every in-view Finished Box, including a finish that already has an acknowledgement row (a Mark unseen, or a Board Seen undone), which a leave never marks.
  evidence: Story 6.8 review, low. `order.mjs` `IN_VIEW_TITLE` is fixed text and `inViewTag` reads only `boxKind` and `in_view`; the leave check refuses once `finishHasRow` is true (`internal/engine/focus.go`, `core.SeenOnLeave`). The AgentView carries nothing that lets the Board tell such a finish apart, and the title is ADR-0015's and the design's wording, so a fix (a view field, or a reworded title) changes ADR-0015's view contract or its text. The Box's state is still right; only the tooltip overpromises.
