- source_spec: `docs/implementation/spec-1-1-project-skeleton-import-rules-and-ci.md`
  summary: CLAUDE.md should say `go run ./cmd/herddash version` prints `herddash dev (unknown)` and that release ldflags stamping is not wired until Story 4.3's release workflow.
  evidence: Story 1.1 review finding 15; the current text says both `(<commit>)` and `unknown`, and no workflow stamps -X main.version yet.
- source_spec: `docs/implementation/spec-1-2-configuration-data-directory-and-socket-discovery.md`
  summary: Story 2.1 should assert created database, WAL, SHM, lock and log files are 0600, which pins the umask set in main.
  evidence: Story 1.2 review; `syscall.Umask(0o077)` in main has no test because no file is created yet.
- source_spec: `docs/implementation/spec-1-2-configuration-data-directory-and-socket-discovery.md`
  summary: Story 2.1 must ensure a `database` path outside `data_dir` has an owner-only parent directory.
  evidence: Story 1.2 review; EnsureDataDir covers only data_dir, and FR-17 requires owner-only storage.
- source_spec: `docs/implementation/spec-1-3-milestone-1-findings-against-a-herdr-0-9-5-binary.md`
  summary: CLAUDE.md should warn that a plain `herdr` command run from a pane here is a 0.9.5 client talking to the 0.8.2 live server through the inherited HERDR_SOCKET_PATH, and wrap its rewritten open-questions sentence.
  evidence: Homebrew's /opt/homebrew/bin/herdr is 0.9.5 while PID 1560 runs the 0.8.2 binary (lsof, 2026-10-16); ad-hoc CLI output is evidence for neither version. Agent-context file, so deferred per triage rule.
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
- source_spec: `docs/implementation/spec-1-5-watch-every-agent-s-status-live.md`
  summary: Decide whether a pane that herdr moves (new pane id, previous_pane_id) keeps its Agent identity and history, or is a removal plus a new Agent.
  evidence: AD-3 keys identity on pane id; Story 1.5 prints a move as removal plus new Agent; ids are workspace-scoped, so a cross-workspace move changes the id (socket API record).
- source_spec: `docs/implementation/spec-1-6-reconnect-forever-and-know-when-herdr-restarted.md`
  summary: Tell consumers when herdr is lost: serve prints nothing until it reconnects, and Engine.Agents() keeps returning the last Statuses as if current.
  evidence: Only Sink.Reconnected exists; the spine's ConnectionTruth (connected, last_snapshot_at, gate) is the Epic 3 vehicle for this.
- source_spec: `docs/implementation/spec-1-7-resync-when-the-stream-cannot-be-trusted.md`
  summary: Document the engine tunables (snapshot, reconnect, resync and reconcile intervals) with the configuration, as the spine's Deferred list says.
  evidence: Story 1.7 review; they exist only as commented constants in internal/engine/tunables.go, and Story 1.6's backoff values were not documented either.
- source_spec: `docs/implementation/spec-1-9-know-which-profile-each-agent-runs-under.md`
  summary: Run the test suite on a macOS runner in CI so procenv's darwin reader (kern.procargs2) is exercised automatically.
  evidence: Story 1.9 review; ci.yml tests only on ubuntu-latest, so the reader the operator's Macs depend on is only vetted and cross-compiled there.
- source_spec: `docs/implementation/spec-1-9-know-which-profile-each-agent-runs-under.md`
  summary: Decide whether a Profile resolution that failed transiently (timeout, herdr call error) should be retried, rather than leaving the Agent unresolved until the next agent detection.
  evidence: Story 1.9 review, unverified (medium if real); settle it by counting "profile resolved" debug entries with an error in dogfooding logs.
- source_spec: `docs/implementation/spec-1-9-know-which-profile-each-agent-runs-under.md`
  summary: CLAUDE.md's "Repository state" should say internal/procenv is built, golang.org/x/sys v0.49.0 is a dependency, and go.mod declares go 1.26.0.
  evidence: Story 1.9 review; agent-context file, so deferred per the triage rule.
- source_spec: `docs/implementation/spec-2-1-a-local-database-herddash-owns.md`
  summary: Persist the server epoch and the pane-to-terminal_id map so a Daemon restart does not restart at epoch 1 and splice a new herdr instance's Agents onto stored epoch-1 history (AD-3 hazard 3).
  evidence: Story 2.1 review; Story 1.6 keeps both in memory, no Epic 2 story names persisting them, and Story 2.2 starts writing rows keyed by epoch. To be built with Story 2.2.
- source_spec: `docs/implementation/spec-2-2-record-every-transition.md`
  summary: Story 2.3 must record the epoch decided by the first snapshot after a Daemon start (the Reconnected report), since the daemon_start marker carries the epoch saved before that decision.
  evidence: Story 2.2 review; TestServeRecords' second run writes its rows under epoch 2 while its daemon_start marker says epoch 1.
- source_spec: `docs/implementation/spec-3-7-the-board-renders-every-agent.md`
  summary: Story 3.8 must reopen the stream when EventSource closes after a non-200 /events reply (503 during a Daemon restart), and keep keyboard focus across re-renders by AgentKey.
  evidence: EventSource does not retry after a non-200 response; Story 3.7's render replaces every Box on each state event.
- source_spec: `docs/implementation/spec-3-8-the-board-stays-live.md`
  summary: Story 3.9's connection strip must also show when the Board itself has lost the Daemon's /events stream (it is reconnecting), not only herdr disconnected per ConnectionTruth.
  evidence: ConnectionTruth comes from the Daemon, so it cannot report the Daemon down; while the Board waits out its backoff (up to 30 s) it shows the last model with ticking Waiting Times and no sign it is stale.
- source_spec: `docs/implementation/spec-3-9-connection-and-version-strips.md`
  summary: Run the Board's headless-browser check (board.mjs drawing: strips, dimming, refused hiding, live updates, reconnect) in CI, not only by hand.
  evidence: Stories 3.7-3.9 each verified board.mjs only in a manual headless-Chrome run; node --test covers order.mjs alone. ubuntu runners ship Chrome, and Node 22's built-in WebSocket can drive it over CDP without npm, but wiring it into CI is work no story plans.
- source_spec: `docs/implementation/spec-4-1-clean-shutdown.md`
  summary: CLAUDE.md's operator-checkpoint list still says Story 1.3 must avoid "the live 0.8.2 server"; the live herdr has run 0.9.5 since 2026-10-20.
  evidence: The checkpoint is closed and the server was restarted on 0.9.5 (PID 56666, loaded binary checked via lsof); the line now misleads a reader about the dogfooding flag. Agent-context file, so deferred.
- source_spec: `docs/implementation/spec-4-2-start-with-my-session-on-macos-and-linux.md`
  summary: Run deploy_test.go's `plutil -lint` check in CI on a macOS runner; today it runs only on a developer's Mac.
  evidence: CI's test job is ubuntu-latest only, so TestLaunchdAgentLints always skips there; the strict Go decoder in TestLaunchdAgent catches most malformed plists on Linux.
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
- source_spec: `docs/implementation/spec-5-2-a-resumed-session-does-not-keep-finished-background-tasks.md`
  summary: Between a Claude Code crash and a resume inside herdr, whether herddash's session watch for the dead process ends, so its open start stops showing, is untested.
  evidence: Unverified (would be medium). The 2026-10-23 experiment ran outside herdr. Settle it by crashing a Claude Code Agent in an isolated herdr and watching its Box.
- source_spec: `docs/implementation/spec-5-2-a-resumed-session-does-not-keep-finished-background-tasks.md`
  summary: What a resume writes for a background subagent left open, and whether `--continue` behaves like `--resume`, were not observed; a status other than stopped would leave the subagent open.
  evidence: Unverified (would be medium). Only shell commands and `--resume` were tested on 2.1.292. Settle it by repeating the SIGKILL-and-resume experiment with a background Agent task and with `--continue`.
- source_spec: `docs/implementation/spec-5-3-clear-drops-the-previous-session-s-background-tasks.md`
  summary: Whether a background subagent started before `/clear` keeps running after it is not known; if it does, it leaves the Box while still working, since it has no tool shell for the process check to find.
  evidence: Unverified (would be medium). The 2026-10-23 `/clear` experiment on 2.1.292 used shell commands only. Settle it by repeating it with a background Agent task and watching whether its completion reaches the new transcript.
