---
title: 'Story 4.1: Clean shutdown'
type: 'feature'
created: '2026-10-20'
status: 'done'
baseline_commit: '6fce27516c15fbd89ae02dcd2659206895ec0670'
route: 'oneshot'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A restart or logout must never corrupt the record. Serve already shuts down in order
(HTTP server first, which ends `/events` streams through `CloseStreams`; then the engine, whose
store writes take no context; then the store, releasing its flock), but nothing proves the whole
sequence: a signal with streams open, a prompt zero exit, and a clean immediate restart.

**Approach:** Prove it with a test that runs serve, opens an `/events` stream, sends the process
a real SIGTERM, requires exit 0 within a few seconds with the stream ended, then starts serve
again on the same data directory at once and requires it to start cleanly and record a second
`daemon_start` marker. Fix whatever the test exposes; change nothing it does not.

</frozen-after-approval>

## Implementation Notes

- Production code changed only to make the proof possible and documented: `main` now calls `mainCode(args)`, so a child process can run the real signal path; the `runServe` doc comment states the restart guarantee. Shutdown itself was already correct: serve already shuts down in the order the story asks for. HTTP `Shutdown` first, whose `RegisterOnShutdown(h.CloseStreams)` ends every `/events` stream; then the engine's context is cancelled and `Run` is waited for. Its store writes (`Append`, `AppendResyncMarker`) take no context, so a write in progress finishes. Then the deferred `Store.Close` releases the flock, and serve returns 0.
- After review, `TestServeShutdownAndRestart` allows 2 s (well under `shutdownTimeout`), pushes a live Transition just before SIGTERM, requires an EOF from the server, a log with no `http shutdown` or `database close` error, a restart on the same address, `PRAGMA integrity_check` ok, and any printed blocked Transition present in `transitions`. `TestServeExitCode` runs serve through `mainCode` in a child process and checks the process exits 0 on SIGTERM.
- Added `TestServeShutdownAndRestart` (`cmd/herddash/serve_test.go`): an open `/events` stream, a real SIGTERM, exit 0 within 5 s (measured 1-2 ms), the stream ended, then an immediate restart on the same data directory that starts, serves an Agent and exits 0, with two `daemon_start` markers in `resync_markers`.
- Checked by mutation: removing the `CloseStreams` registration makes the test fail (serve hangs past 5 s), and skipping `Store.Close` makes it fail (the restart finds the lock held). Both were restored.

## Review Triage Log

| # | Finding | Verdict | Evidence / route |
|---|---------|---------|------------------|
| 1 | The test's 5 s limit equals `shutdownTimeout`, so a hung stream races it | medium | Confirmed: `serve.go` `shutdownTimeout = 5s`; patched to a 2 s limit, and the CloseStreams mutation now fails at 2 s |
| 2 | No write is in flight at the signal; the record's soundness is not checked | medium | Patched: a live Transition pushed just before SIGTERM, `PRAGMA integrity_check`, and any printed blocked Transition must be in `transitions` |
| 3 | The shutdown order is not checked | low | Patched: the log must have no `http shutdown` (Shutdown timed out) or `database close` error, and a stop entry |
| 4 | Both serves bind port 0, so a fixed-port restart is untested | medium | launchd and systemd restart on the configured bind; patched: the restart binds the first serve's address |
| 5 | In-process `run` never checks the real exit status | medium | launchd and systemd act on it; patched: `TestServeExitCode` through `mainCode` in a child process |
| 6 | The second-signal force-quit is untested | low | Needs a deliberately hanging shutdown in a child; extra machinery for a manual-escape path; rejected |
| 7 | Any read error counts as the stream ending | low | Patched: must be EOF, before the client closes |
| 8 | The restart wait's `select` inside `waitFor` | low | Patched: one loop over exit, deadline and output |
| 9 | The spec lacks Verification and Tasks sections | false | The oneshot route's spec is frontmatter, Intent and Implementation Notes by design |
| 10 | Status values do not follow the review workflow | false | Workflow ordering |
| 11 | CLAUDE.md's checkpoint list still says the live herdr is 0.8.2 | low | Real (its Story 1.3 line); an agent-context file, deferred |
| 12 | The `runServe` doc comment omits the restart guarantee | low | Patched |
