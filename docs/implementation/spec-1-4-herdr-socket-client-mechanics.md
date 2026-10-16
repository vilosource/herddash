---
title: 'Story 1.4: herdr socket client mechanics'
type: 'feature'
created: '2026-10-16'
status: 'done'
baseline_commit: '2e4ba49e3f8c847e18a11d148b16810d9153919e'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/research/herdr-socket-api.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Nothing in herddash can talk to herdr yet. Stories 1.5 to 1.9 need one package that
speaks the socket protocol safely and decides nothing (AD-5, AD-11, ADR-0006).

**Approach:** `internal/herdr` gets a `Client` over a socket path that pings, snapshots, reads a
pane's foreground process, and opens lifecycle and per-pane status subscriptions. It maps wire types
from `internal/herdr/gen` to new observation types in `internal/core`, and reports stream trouble as
health signals for the engine to act on.

## Boundaries & Constraints

**Always:**
- **One request per connection:** `Ping`, `Snapshot` and `ForegroundProcess` each dial the socket,
  write one line, read one response line, and close. No pooling, no reuse.
- **Subscriptions:** `SubscribeLifecycle` subscribes to `pane.created`, `pane.closed`,
  `pane.moved`, `pane.exited` and `pane.agent_detected` on one connection. `SubscribeStatus(paneID)`
  subscribes to that pane's `pane.agent_status_changed` on its own connection. Each waits for
  `subscription_started` before returning, then delivers decoded events in arrival order on a
  channel until the connection ends. `Close` ends it (that is unsubscribe).
- **Two envelopes:** lifecycle lines are `{event, data}` with underscored names; status lines keep
  the dotted name. Both decode through the generated types.
- **Core observation types** (new, in `internal/core`, stdlib only, no clock): a snapshot with the
  herdr version and its panes, each pane with pane id, terminal id, workspace id, tab id, and, when
  herdr reports an agent there, its Agent Program, Status and `state_change_seq`; a lifecycle event
  with its kind, pane id, previous pane id (for moves) and Agent Program (for detection); a status
  event with pane id, Status and Agent Program; a foreground process with pid, name and argv0.
  Status strings pass through verbatim.
- **Health signals** (an `internal/herdr` type): `events_lost`, any other error code (carrying the
  code and message), a rejected subscribe, a closed or failed connection, and an undecodable line.
  A subscription delivers at most one health signal, as its last delivery, and then closes. A
  subscribe rejected before `subscription_started` returns an error that `errors.As` unwraps to that
  health signal.
- **Foreground process:** from `pane.process_info`, the process whose pid equals the foreground
  process group id, else the first foreground process; no foreground process is an error.
- Every call honours its `context.Context`; a missing response line is an error, never a hang.
- Errors are wrapped with `%w`, and herdr error responses are a typed error carrying code and
  message.

**Never:**
- A method, or a request built anywhere in `internal/herdr` outside `gen/`, for `pane.focus`,
  `agent.focus`, `pane.read`, `agent.read`, `pane.wait_for_output` or `pane.output_matched`.
- Retry, reconnect, resync, gate on version, or track the pane set: those are the engine's (AD-11).
- Read `HERDR_*` environment variables (config resolves the socket), or dial a real herdr in tests.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Ping | fake answers pong 0.9.5 | version `0.9.5`; one connection, closed | none |
| Snapshot | recorded 0.9.5 snapshot | panes with ids and terminal ids; agent pane has program, Status, seq 9 | none |
| Lifecycle | `pane_created`, `pane_moved`, `pane_agent_detected` lines | three core lifecycle events, in order, moved carries previous id | none |
| Status | dotted `pane.agent_status_changed` line | core status event | none |
| Rejected | error response instead of `subscription_started` | Subscribe returns error unwrapping to a rejected health signal | no channel |
| Lost | `events_lost` error after start | health `events_lost` delivered last, channel closed | none |
| Unknown code | error code `brand_new` after start | health carrying `brand_new` | none |
| Closed | server closes mid-stream | health closed, channel closed | none |
| Garbage | non-JSON line | health undecodable, channel closed | none |
| No response | server accepts, never answers | call returns context error | no hang |
| Foreground | recorded process_info | pid of the group leader `sleep` | none |
| Forbidden | non-gen source naming a forbidden method | test fails naming the file | test |

</frozen-after-approval>

## Code Map

- `internal/herdr/gen/wire_gen.go` -- generated types to decode with: `RequestPing`, `RequestSessionSnapshot`, `RequestPaneProcessInfo`, `RequestEventsSubscribe`, `Subscription*` variants, `SuccessResponse` + `ResponseResult*` (discriminate by `result.type`), `ErrorResponse`, `EventEnvelope` + `EventData*`, `SubscriptionEventEnvelope` + `PaneAgentStatusChangedEvent`
- `internal/herdr/gen/testdata/*.json` -- recorded 0.9.5 lines; reuse as fake-server responses
- `internal/herdr/doc.go` -- package doc; keep and extend
- `internal/core/types.go` -- existing `Status`, `AgentProgram`; add observation types in a new `internal/core/observe.go`
- `arch_test.go` -- herdr may import core and its own gen only; core stays stdlib only
- `docs/research/herdr-socket-api.md` "Findings against herdr 0.9.5" -- envelopes, error shapes, `events_lost` matched by raw `error.code`
- Unix socket paths over 104 bytes fail on macOS: tests create sockets under `os.MkdirTemp("/tmp", …)` (as `internal/config` tests do)

## Tasks & Acceptance

**Execution:**
- [x] `internal/core/observe.go`, `internal/core/observe_test.go` -- observation types as in Always -- the engine's vocabulary for what herdr reported
- [x] `internal/herdr/client.go` -- `Client`, `New(socketPath)`, `Ping`, `Snapshot`, `ForegroundProcess`, the one-shot request helper, typed herdr error -- AD-5
- [x] `internal/herdr/subscribe.go` -- `SubscribeLifecycle`, `SubscribeStatus`, `Subscription` with its channel and `Close`, health signal type -- AD-11
- [x] `internal/herdr/map.go` -- wire → core mapping for both envelopes, snapshot and process info -- ADR-0006 package edge
- [x] `internal/herdr/fake_test.go`, `client_test.go`, `subscribe_test.go` -- a fake unix-socket server scripted per test, covering every matrix row and asserting one connection per one-shot call
- [x] `internal/herdr/forbidden_test.go` -- parse every non-test, non-`gen/` `.go` file in `internal/herdr`, fail on any string literal naming a forbidden method, and fail if `*Client` has a method whose name contains `Focus`, `Read` or `Output`

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes, arch_test passes, and no test dials a path outside its temp dir.

## Implementation Notes

- The review patches also fixed a flaky fake-server check (`expectClientClose`) that failed when test cleanup closed the connection first; harness only.

## Spec Change Log

- **2026-10-16, review pass 1.** Findings 1-3 (agent release reported as a detection; pane-level
  agent ignored; lifecycle events dropping terminal, workspace and tab ids) extend the frozen
  block's observation types: `LifecycleEvent` gains a released kind with its final Status and the
  created/moved pane's terminal, workspace and tab ids, and a snapshot pane falls back to the pane
  record's agent fields. Strictly bad_spec; patched in place instead of reverted and re-derived,
  because the existing code is correct and only extended, under the operator's standing delegation
  of technical decisions. KEEP: one connection per call, `Subscription[T]` with one final health
  signal, `errors.As` to `*Health` and `*Error`, the fake-server tests.

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | ECH | `pane_agent_detected` with `released`/`final_status` reaches the engine as a detection | medium | `EventDataPaneAgentDetected` has `Released` and `FinalStatus`; `decodeLifecycle` ignores both; patch (see change log) |
| 2 | BH, ECH | Snapshot pane's agent taken only from agent records; `PaneInfo.Agent`/`AgentStatus` ignored | medium | `PaneInfo` carries both; a pane with no agent record maps to no agent; patch: fall back with nil seq |
| 3 | BH | Lifecycle created/moved drop terminal, workspace and tab ids | low | `EventDataPaneCreated/Moved` carry a full `PaneInfo`; engine tracks the pane set and Story 1.6 uses `terminal_id`; patch |
| 4 | BH | Snapshot omits cwd, titles, labels, order, `completion_seq` | low | No Epic 1 story consumes them; CLAUDE.md forbids code no story asks for; Epic 3 extends; rejected |
| 5 | BH | No default timeout: `Ping(context.Background())` hangs on a silent server | medium | Spec Always: "a missing response line is an error, never a hang"; `Client` doc admits it waits; patch: named default timeout when ctx has no deadline, for one-shot calls and subscribe setup |
| 6 | BH | sprint-status vs spec status mismatch | false | Step 5 moves the story on; workflow ordering |
| 7 | BH | `HealthEventsLost` comment states unverified semantics as fact | low | Research record marks `events_lost` unreproduced; direct wording fix; patch |
| 8 | BH | Lifecycle subscribe marshals every kind through `SubscriptionPaneCreated` | low | Works only while all five variants are `{type}`; direct fix with per-variant types; patch |
| 9 | BH | Unknown event names end the stream | low | Deliberate: an unsubscribed kind on a filtered stream means the protocol changed, and the spine's open-set rule is "unknown means resync"; rejected |
| 10 | BH | Forbidden method-name check too broad (`Ready`) and too narrow (`Subscription`) | low | A false positive fails loudly; `Subscription` has only `Close`; rejected |
| 11 | BH | `TestCloseWhileUndrained` does not assert the goroutine exits or the fake saw the close | low | Test edit; patch |
| 12 | BH, VG | No test for an error id mismatch, an empty error id, or a line over `maxLine` | medium | VG pre-verified: changing the empty-id guard passes all tests; patch |
| 13 | VG | Lines over the 4096-byte buffer never tested | medium | VG pre-verified: largest fixture 2506 bytes; breaking the ErrBufferFull path passes all tests; real snapshots exceed it; patch |
| 14 | VG | Subscription outliving its setup context untested | medium | VG pre-verified: dropping `stop()` passes all tests; patch |
| 15 | VG, BH | Orphan agent record branch in `mapSnapshot` untested | medium | VG pre-verified: deleting the loop passes; patch with `TestMapSnapshot` |
| 16 | BH | AC "no test dials outside its temp dir" unenforced | low | All dials go through the fake under `os.MkdirTemp`; a guard adds machinery; rejected |
| 17 | BH | `herdr/doc.go` says imports are core only | low | It imports `gen`; direct fix; patch |
| 18 | BH | Recompiled epic context dropped the Host/Server and config rules | low | Stories 1.5-1.9 build AgentKey from them; direct one-line pointer; patch |
| 19 | BH | `method()` type switch fragile; `Health.Error()` prints events_lost twice | low | Cosmetic; rejected |
| 20 | ECH | `pane_moved` without `previous_pane_id` | low | Required by the schema; guard adds a branch; rejected |
| 21 | ECH | Duplicate pane ids in a snapshot | low | No evidence herdr sends them; rejected |
| 22 | ECH | Agent record with empty pane id | low | Required non-empty in practice; rejected |
| 23 | ECH | Status event without `agent_status` | low | Required by the schema; rejected |
| 24 | ECH | Status event for a pane other than the subscribed one | low | Would need a herdr bug; guard adds a parameter; rejected |
| 25 | ECH | Empty ping version returned without error | low | Required by the schema; the engine's gate refuses an unparsable version; rejected |
| 26 | ECH | Partial line before EOF decoded as a line | maybe-false | Changing it risks breaking a response without a trailing newline; both kinds lead to resync; rejected |
| 27 | ECH | A delivery can race `Close` | low | `deliver`'s select may send while `done` closes; doc promises silence; patch the doc to say one in-flight delivery may still arrive |
| 28 | ECH | Consumer that stops reading without `Close` leaks the goroutine | low | Real; patch doc: `Close` is required |
| 29 | ECH | Forbidden method built by string concatenation evades the test | low | Deliberate evasion; rejected |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
