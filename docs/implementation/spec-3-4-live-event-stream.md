---
title: 'Story 3.4: Live event stream'
type: 'feature'
created: '2026-10-20'
status: 'done'
baseline_commit: 'dfe94feed1ffffa2ad46f9cc2d2ee50a588757a3'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/implementation/spec-3-3-read-only-api-on-loopback-with-current-state.md'
  - '{project-root}/docs/implementation/spec-3-2-the-engine-publishes-views-and-change-lists.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Board must update within a second and can never drift, but `/api/state` is a
snapshot (FR-4, FR-13, AD-7).

**Approach:** `GET /events` is a Server-Sent Events stream. It opens with a `state` event built
from the same View a subscription starts from, then sends one event per published change, in
publish order. A slow client is cut off, and it reconnects to a fresh `state`.

## Boundaries & Constraints

**Always:**
- **Wiring:** `httpapi.Options` gains `Subscribe func(buffer int) (*engine.ViewSubscription, func())`.
  `New` returns a `*Handler`, which implements `http.Handler`, with `CloseStreams()`: it ends every
  open stream and makes new ones end at once. serve registers it with `srv.RegisterOnShutdown`.
- **Route:** `/events` follows the same Host check, method rules (GET only, HEAD gets 405 here)
  and JSON errors as `/api/state`.
- **Opening:**
  1. Subscribe with a fixed buffer (named constant, 64).
  2. Take `View()`.
  3. Write `event: state` with `id:` the view's Seq and `data:` exactly the `/api/state` JSON body
     on one line.
  4. Then forward only publications whose `View.Seq` is greater than the state's Seq. The
     subscription is taken first, so nothing is missed between the snapshot and the stream.
- **Change events** carry `id:` = the publication's `View.Seq`. They are in publish order and,
  within a publication, in change-list order. `data:` is single-line JSON:
  - `agent`: the full AgentView.
  - `removed`: `{"key": AgentKey}`.
  - `transition`: `{"key": AgentKey, "transition": {...}, "agent": AgentView}`. The `transition`
    object carries `agent_program`, `profile`, `previous` (omitted when unknown), `new`,
    `herdr_order` (omitted when unknown), `herdr_version` (omitted when empty), `observed_at`,
    `provenance` and `possibly_spurious`. The wire struct lives in httpapi, built from
    `core.Transition`'s accessors.
  - `resync`: `{"reason": …, "detail": …}`, with `detail` omitted when empty.
  - `connection`: the full ConnectionTruth.
  - Each event is flushed at once.
- **Headers:** `Content-Type: text/event-stream`, `Cache-Control: no-store`,
  `X-Content-Type-Options: nosniff` and `X-Accel-Buffering: no`. Never a CORS header.
- **Per stream:**
  - Clear the write deadline (`http.ResponseController.SetWriteDeadline(time.Time{})`). Then set
    a deadline of `streamWriteTimeout` (named, 10 s) before each write and clear it after, so a
    stalled client cannot pin a goroutine forever.
  - Send a comment line `: keepalive` every `keepaliveInterval` (named, 15 s).
  - The stream ends when any of these happens: the request context ends, the subscription channel
    closes (overflow or engine stop), a write fails, or `CloseStreams` is called. It always
    unsubscribes.
- **History:** none is kept, and `Last-Event-ID` is ignored. A reconnect always starts with
  `state`.
- **Tests:** use `httptest` with a fake View and Subscribe that the test drives. The serve
  end-to-end test opens `/events` against the fake herdr and sees `state`, then a `transition`
  within 1 s of a live status change.

**Never:**
- Replay history or handle `Last-Event-ID`.
- Block the engine. Delivery is through the engine's bounded subscription.
- Call herdr per request.
- Add a write route.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Opening | View Seq 7 with 2 Agents | first event `state`, `id: 7`, data equal to the /api/state body | none |
| No gap | a publication Seq 8 arrives right after subscribing | sent after `state` | none |
| Stale | a publication Seq ≤ 7 is already queued | skipped | none |
| Kinds | publication with agent, removed, transition, resync, connection | five events in that order, each `id:` = Seq, bodies as specified | none |
| Transition body | live working→blocked | includes key, transition fields and the new AgentView (class needs_you) | none |
| Overflow | subscription channel closed by the engine | the response ends; a new GET gets `state` | none |
| Keepalive | no publications for keepaliveInterval (lowered in test) | `: keepalive` comment | none |
| Client gone | request context cancelled | handler returns; unsubscribed | none |
| CloseStreams | open stream, then CloseStreams | stream ends promptly; a later /events ends at once | none |
| Shutdown | serve with an open /events, then ctx cancelled | serve returns well before shutdownTimeout | none |
| Rules | POST/HEAD /events, bad Host | 405 / 403 JSON | error |
| Headers | any stream | event-stream content type, no-store, no CORS | none |
| Last-Event-ID | header sent | ignored; starts with `state` | none |
| End to end | serve plus fake herdr; live status change | `transition` event within 1 s | none |

</frozen-after-approval>

## Code Map

- `internal/httpapi/httpapi.go` -- the route switch adds `/events`. `Options.Subscribe`, the `Handler` type and `CloseStreams` (a stop channel closed once under `sync.Once`).
- `internal/httpapi/events.go` (new) -- the stream loop, SSE writing, wire structs for removed, transition and resync, keepalive, deadlines. `events_test.go`.
- `internal/engine/view.go` -- `ViewSubscription`, `Publication` and `Change` already exist; `Engine.Subscribe` matches the signature.
- `cmd/herddash/serve.go` -- pass `Subscribe: e.Subscribe`, then `srv.RegisterOnShutdown(h.CloseStreams)`. Update the doc comment that anticipated this.
- `cmd/herddash/serve_test.go` -- the end-to-end and shutdown tests.

## Tasks & Acceptance

**Execution:**
- [x] `internal/httpapi/*` -- /events and tests for every handler matrix row
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- wiring, shutdown hook, end-to-end tests

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes and earlier tests still pass.

## Implementation Notes

- **`New` panics on a nil `Subscribe`**, as it already did on a nil `View`. The existing
  `httpapi_test.go` call sites pass `noSubscribe`; `TestUnknown` no longer lists `/events`
  (it now checks `/events/` and `/api/events` stay 404).
- **Check order on `/events`:** Host (403), then path, then method: anything but GET gets 405
  with `Allow: GET` and the JSON error. No subscription is taken by a refused request.
- **Refusals:** after `CloseStreams` (without subscribing), or when the subscription is already
  closed (the engine has stopped), `/events` answers 503 `unavailable` in the JSON error shape,
  so an EventSource does not reconnect in a loop. Every real stream's first line is
  `retry: 2000`. The opening View is marshalled before the 200; a failure answers 500 `internal`.
- **Event names** are httpapi constants mapped explicitly from engine kinds, so an engine rename
  cannot change the public contract. An unknown kind is logged once per kind per process.
- **A failed flush (`ErrNotSupported`)** is logged at warn; other write failures at debug.
- Between the changes of one publication the loop returns at once if `CloseStreams` was called
  or the request context ended. A keepalive interval of 0 or less falls back to the constant.
- **Deadlines:** `SetWriteDeadline` and `Flush` go through `http.ResponseController`;
  `ErrNotSupported` from a writer without deadlines is ignored, any other error ends the stream.
  The keepalive ticker and per-write timeout are `Handler` fields set from the named constants,
  so tests lower them.
- **`TestServeEvents`** covers the end-to-end and shutdown rows together: it opens `/events`,
  sees `state`, pushes a live change and requires `transition` within 1 s (observed ~1-3 ms),
  then cancels ctx with the stream open and requires serve to return within half of
  `shutdownTimeout` (observed ~2 ms). Without `RegisterOnShutdown` it fails at the 5 s timeout.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | VG, BH | The per-write deadline is never exercised against a stalled client | medium | VG pre-verified: removing it passes every test; patch |
| 2 | VG, ECH | "After CloseStreams, no subscription" is not asserted | low | Patch, with 4 |
| 3 | BH | SSE event names come from engine constants | medium | Renaming an engine constant would change the public contract; patch: explicit names and a coverage test |
| 4 | BH | After the engine stops or CloseStreams, `/events` answers an empty 200, so clients reconnect in a loop | medium | Patch: 503 JSON, and `retry:` on real streams |
| 5 | ECH | The opening View is marshalled after the 200 header | low | Direct; patch |
| 6 | BH | An unsupported Flush ends every stream with only a debug log | low | Patch: warn |
| 7 | ECH | Stop or cancel is not checked between changes of one publication; keepalive ≤ 0 panics | low | Patch |
| 8 | BH | Unknown kinds warn per change per stream | low | Patch: once per kind |
| 9 | BH | `import_key` omission and `observed_at` units undocumented on the wire type | low | Patch |
| 10 | BH | Tautological assertions | low | Patch: removed |
| 11 | BH, ECH | Bare channel sends and a test reader that can call `t.Errorf` after the test or block | low | Patch |
| 12 | BH | No two-stream test | low | Patch |
| 13 | ECH | A non-`ErrNotSupported` failure clearing the deadline is only logged | low | The first write's own deadline call then fails and ends the stream; rejected |
| 14 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
