---
title: 'Story 7.4: The HTTP server bounds idle and slow connections'
type: 'feature'
created: '2026-11-02'
status: 'done'
baseline_commit: '84868871ef98f6b8be635a2666f40f8cf19fc68d'
route: 'dispatch'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Daemon's `http.Server` (`cmd/herddash/serve.go:169-173`) sets only `ReadHeaderTimeout`. With no `IdleTimeout`, a keep-alive connection that sends nothing more is held forever. History and the acknowledgement routes bound their own writes (`boundWrite`, `internal/httpapi/history.go:156`; called at `history.go:124`, `ack.go:160`), but `/api/state`, the Board page, its assets, and every error, 403, 404 and 405 reply have no write deadline, so a client that stops reading holds the handler (Epic 3 retrospective D7).

**Approach:** (1) Give the server an `IdleTimeout` of 60 s, a fixed tunable beside `readHeaderTimeout`, built by a small unexported constructor that tests can call with a short timeout. (2) Bound every reply's write: `h.boundWrite` sets the existing 10 s `writeTimeout` as a write deadline at the top of `Handler.ServeHTTP`, before the Host check, so every route, error and method refusal is covered; history and acknowledge arm it again after their work, so their reply keeps its own full `writeTimeout` as before. `/events` already clears the deadline before it writes anything (`events.go:183`) and keeps its own per-write deadlines, so streams are unaffected; `IdleTimeout` applies only between requests, never during a stream.

## Boundaries & Constraints

**Always:** A deadline set for one request never cuts short a later request on the same connection (each request arms its own at `ServeHTTP` entry, and net/http clears it after each finished reply). `/events` streams of any length keep working, keepalives included. Values are listed in `docs/config.example.toml`'s timings ("HTTP and the Board"). Loopback-only binding and every existing guard are unchanged.

**Never:** No server-wide `WriteTimeout` (it would cut streams that do not clear it, and it covers handler time). No config key. No change to `ReadHeaderTimeout`, the ack routes' read deadline, or the stream's 10 s per-write deadline. No new import edge.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Idle keep-alive | a reply read, then nothing sent | server closes the connection after the idle timeout | N/A |
| Stalled reader | client requests a large `/api/state` and never reads | the write fails at the deadline, the handler returns, the connection closes | the write error is the handler's; nothing else logged unless `boundWrite` cannot set the deadline (existing Warn) |
| Error reply | a 404/405/403 to a client that never reads | bounded the same way | N/A |
| Long stream | `/events` open well past 10 s and past the idle timeout | stays open, receives keepalives | N/A |
| Keep-alive reuse | two requests on one connection, the second after more than `writeTimeout` but less than idle | the second is answered | N/A |

</frozen-after-approval>

## Code Map

- `cmd/herddash/serve.go:30-35,169-173` -- `readHeaderTimeout`/`shutdownTimeout` consts; the `http.Server` literal. Add `idleTimeout = 60 * time.Second` and an unexported constructor, e.g. `newHTTPServer(h http.Handler, logger *slog.Logger, idle time.Duration) *http.Server`, used by `runServe` with `idleTimeout`.
- `internal/httpapi/httpapi.go:135` -- `ServeHTTP`; add the `boundWrite` defer first. `writeTimeout` (`:103,123`) is `streamWriteTimeout` and tests lower it.
- `internal/httpapi/history.go:124,150-165` -- remove the call at `:124`; rewrite `boundWrite`'s comment (it now bounds every reply from `ServeHTTP`).
- `internal/httpapi/ack.go:160` -- remove the call; `readBody`'s read deadline (`:214-226`) stays.
- `internal/httpapi/events.go:175-200` -- unchanged; its clear-at-start comment already anticipates an outer deadline.
- Tests: `internal/httpapi/*_test.go` already build a `Handler` with a fake `View` and lower `writeTimeout`; a fake View returning tens of thousands of AgentViews gives a body larger than the socket buffers, so a client that never reads stalls the write. `cmd/herddash/serve_test.go` for the idle test through the constructor (raw `net.Conn`, keep-alive request, read the reply, then expect EOF).
- `docs/config.example.toml` ("HTTP and the Board" timings): add "an idle keep-alive connection is closed after 60 s" and "one reply's write, timeout 10 s" (the existing stream line stays).
- `docs/implementation/deferred-work.md` -- D7 `resolved:` naming Story 7.4.

## Tasks & Acceptance

**Execution:**
- [x] `cmd/herddash/serve.go` -- `idleTimeout` and the constructor -- D7
- [x] `internal/httpapi/httpapi.go`, `history.go`, `ack.go` -- one top-level `boundWrite`, drop the two inner calls, update comments -- D7
- [x] `internal/httpapi/*_test.go` -- stalled reader on `/api/state` and on an error reply (handler returns within a few `writeTimeout`s); long stream past the lowered `writeTimeout`; keep-alive reuse after more than `writeTimeout` -- matrix rows 2-5
- [x] `cmd/herddash/serve_test.go` -- idle connection closed by a server built with a short idle -- matrix row 1
- [x] `docs/config.example.toml`, `deferred-work.md` -- timings listed, D7 resolved

**Acceptance Criteria:**
- Given the existing httpapi, events, history, ack and board-browser tests, when they run, then they pass unchanged.

## Implementation Notes

- `boundWrite(w, r)` (in `httpapi.go`, beside `ServeHTTP`) sets the write deadline and neither clears nor flushes it. net/http writes a short reply to the socket only in `finishRequest`, after `ServeHTTP` returns, so a deadline cleared when the handler returns would leave matrix row 3 (error replies to a client that never reads) unbounded; left set, that final write lands inside it. An earlier version flushed before clearing, which turned short replies' framing from `Content-Length` to `Transfer-Encoding: chunked`; `TestKeepAliveReuse` asserts a short `/api/state` reply carries `Content-Length` and is not chunked. Mutation-checked: dropping the `ServeHTTP` call makes both stalled-reader tests fail.
- History and the write routes call `boundWrite` again after their work (history after its store reads; the write routes after the body read and after the engine's answer), so their reply gets a full `writeTimeout` of its own, as before this story: the deadline set at entry would otherwise also cover that work, and a slow engine would leave a recorded mark with its reply lost. `TestAckReplyAfterSlowEngine` (engine 300 ms, `writeTimeout` 50 ms) fails without the re-arm after the engine's answer. The 10 s therefore runs from the handler's start for every other route, and from the end of the work for history and marking; `docs/config.example.toml` says so.
- No `ConnState` hook: a review asked for one to clear the write deadline when a connection goes idle, but Go 1.26's net/http already clears it right after each finished reply (`net/http/server.go:2081`, `c.rwc.SetWriteDeadline(time.Time{})` after `w.finishRequest()`, checked in the toolchain's source), so a 400 or 431 net/http writes itself is never refused by a passed deadline. `TestHTTPServerClosesIdle`'s malformed-request check (a 400 after a passed deadline the handler left set) pins that behaviour.
- `TestHTTPServerClosesIdle` also asserts the idle close comes no sooner than `idle/2`. `TestErrorReplyStalledReader` covers a Board asset (`/assets/board.css`) as well as 403, 404 and 405.
- Every route's deadline is set at entry, so other routes' 10 s covers their handler time; `/api/state` and the Board's files do no slow work before they write.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race ./... && test -z "$(gofmt -l .)"` -- all pass
- `go test -race -count=50 -timeout 20m ./internal/httpapi ./cmd/herddash -run '<the new tests>'` -- pass
- `go build -o /tmp/hd ./cmd/herddash && node internal/httpapi/web/check/board-browser.mjs /tmp/hd` -- pass (long-lived stream through the real server)

## Spec Change Log

- 2026-11-02, review pass 1 (Blind Hunter, Verification Gap, Edge Case Hunter: the frozen block named a defer-and-clear mechanism the code does not use). Amended inside the frozen block, by the agent, because the operator pre-approved this spec under their standing decision and the mechanism wording was the agent's own, not theirs: the guarantee ("a deadline never cuts short a later request") is unchanged; only how it is met changed. A defer-and-clear needs a Flush before the clear, which made short replies chunked (`/api/state` lost `Content-Length`, checked with curl against a develop build). Known-bad state avoided: chunked framing. KEEP: the set-at-entry deadline, `/events` clearing it, the stalled-reader and keep-alive tests, the `Content-Length` assertion.

## Review Triage Log

Pass 1 (Blind Hunter BH, Edge Case Hunter ECH, Verification Gap VG):

| # | Finding | Verdict | Evidence | Route |
|---|---|---|---|---|
| 1 | BH, ECH, VG: frozen intent names defer-and-clear; code is set-only | low | the guarantee holds (`TestKeepAliveReuse`); wording was the agent's | spec change log above |
| 2 | ECH1, ECH2, ECH6, BH6, VG: the entry deadline now covers the ack body read and engine call and history's store reads; a slow one loses the reply (a mark is written, its reply lost) | medium | before, `boundWrite` ran after the work (`ack.go:160`, `history.go:124` at baseline) | patch: re-arm before each reply; test a blocking `Acknowledge` |
| 3 | ECH3, ECH7, BH8: a deadline left expired on an idle connection fails net/http's own 400/431 | false | net/http clears the write deadline after each finished reply (`net/http/server.go:2081`, Go 1.26.5); a test sending a malformed request after the deadline passed gets the 400 | reject; the test is kept, the `ConnState` hook first added for it is removed |
| 4 | ECH4: a slow but progressing reader of a large reply is cut at 10 s total | low | replies are a few KB on loopback; a per-chunk bound adds machinery | reject |
| 5 | BH2: spec ACs reduced to "existing tests pass" | low | fix edits the spec; the matrix maps to tests | reject |
| 6 | BH3: no stalled-reader test for Board assets | low | same entry line covers them; cheap to add | patch |
| 7 | BH4: idle test has no lower bound | low | `d > 2*time.Second` only | patch |
| 8 | BH5: no test of real `/events` through `newHTTPServer` past the idle timeout | low | `IdleTimeout` never applies during a handler (net/http); `TestHTTPServerClosesIdle` streams past idle through the constructor | reject |
| 9 | BH7: config.example understates what the bound covers | low | wording | patch |
| 10 | BH9: `boundWrite` in history.go, no route in its Warn | low | single caller is `ServeHTTP` | patch |
| 11 | BH10: tracking state inconsistent before merge | false | sprint-status and the PR number are set at the end, as in 7.1-7.3 | reject |
| 12 | BH11: spine and CLAUDE.md describe the old model | low | `ARCHITECTURE-SPINE.md:~431` | patch (spine; CLAUDE.md at the end) |
| 13 | BH12: stalled-reader test allows 100x `writeTimeout` and builds ~10 MB | low | a generous bound avoids CI flakes (Story 7.3); runs pass `-count=50` | reject |
| 14 | VG1: nothing checks `runServe` passes the 60 s `idleTimeout` | low | needs a seam in `runServe`; same call as Story 7.2's unpinned constants | reject |
