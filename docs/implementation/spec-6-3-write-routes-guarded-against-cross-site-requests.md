---
title: "Story 6.3: Write routes, guarded against cross-site requests"
type: 'feature'
created: '2026-10-27'
status: 'done'
baseline_revision: 'ca6b61bf19d0553bcdb3f5777bed989cb01ebd58'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0014-acknowledging-a-finished-agent.md'
  - '{project-root}/docs/adr/0007-read-only-loopback-http-surface.md'
  - '{project-root}/docs/implementation/spec-6-2-the-engine-acknowledges-a-finish.md'
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Story 6.2 gave the engine `Acknowledge`, but nothing outside the process can reach it,
and the history route does not show the acknowledgement rows Story 6.1 records. The Board needs an
HTTP way to mark a finish seen and undo it, and no other web page the operator visits may be able
to use it.

**Approach:** ADR-0014's write routes in `internal/httpapi`, wired in `cmd/herddash serve`. Four
`POST` routes reach the engine only through a new `Options.Acknowledge` func, after a guard that runs
in the ADR's order before any body byte is read: loopback `Host`, required same-origin `Origin`,
`Sec-Fetch-Site` when present, the custom `X-Herddash-Write: 1` header, a JSON `Content-Type`, then
a bounded, strictly decoded body. The engine's typed errors map to statuses in the existing JSON
error shape. The history body gains `acknowledgements`, read through a new
`Options.Acknowledgements` func.

## Boundaries & Constraints

**Always:** The guard runs before the body is read, and a refused request never calls
`Acknowledge`. No response, success or error, carries an `Access-Control-*` header; `OPTIONS` is a
405 everywhere. Every existing route and method keeps its behaviour and `Allow`. The per-Agent path
is parsed by the same function as the history path. Both new `Options` funcs are required (`New`
panics on nil). No new import edge (`httpapi` already imports `engine`); the store is written only
by the engine (`callsite_test.go`).

**Never:** Call herdr; write to the store from `httpapi`; answer a CORS preflight; change the
Board's Content-Security-Policy, the `/events` event set or the Board's files (Story 6.4).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error |
|----------|--------------|---------------------------|-------|
| Per-Agent mark / undo | `POST /api/agents/{h}/{s}/{e}/{p}/seen` or `unseen`, `{"waiting_since": ms}`, same-origin headers | 200 `{"agents":[view]}`; `Acknowledge` gets one item, `Seen` as the route says | — |
| Bulk mark / undo | `POST /api/seen` or `/api/unseen`, `{"agents":[{"key":{...},"waiting_since":ms},...]}` | 200, views in request order; `Acknowledge` gets exactly the items | — |
| Non-loopback Host (even with its own Origin) | | refused first | 403 `host_not_allowed` |
| Origin missing, `null`, another host, port or scheme, a path, more than one | | refused | 403 `origin_not_allowed` |
| `Sec-Fetch-Site` present and not `same-origin` | `cross-site`, `same-site`, `none` | refused | 403 `origin_not_allowed` |
| `X-Herddash-Write` missing or not `1` | | refused | 403 `write_header_missing` |
| Content-Type not `application/json` (only `charset=utf-8` allowed as a parameter) | `text/plain`, form, multipart, missing, `charset=latin1`, another parameter | refused | 415 `unsupported_media_type` |
| Body over 4 KiB (per-Agent) or 64 KiB (bulk) | one byte over | refused (`http.MaxBytesReader`) | 413 `body_too_large` |
| Not exactly one JSON object; unknown field (also inside a key); trailing data; `waiting_since` missing, 0, negative, fractional or a string; 0 or 257 items; a key twice; a key missing a field, empty, or with an epoch over 2^63-1 | | refused | 400 `bad_request` |
| Engine: `ErrUnknownAgent`, `ErrNotFinished`, `*StaleError`, `ErrRecordingOff`, `ErrBadRequest`, anything else | | 404 `unknown_agent`, 409 `not_finished`, 409 `stale_view` (message names the keys), 503 `recording_off`, 400 `bad_request`, 500 `store_failed` (detail only in the log) | |
| Request cancelled while the engine answers | | no response body written | — |
| `GET`, `HEAD`, `PUT`, `DELETE`, `PATCH`, `OPTIONS` on a write route | | 405, `Allow: POST` | `method_not_allowed` |
| `POST`, `OPTIONS`, others on `/`, `/assets/…`, `/api/state`, history | | 405, `Allow: GET, HEAD` as before | `method_not_allowed` |
| Same on `/events` | | 405, `Allow: GET` as before | `method_not_allowed` |
| Malformed write path | `/api/seen/`, bad epoch, wrong case | 404 as before | `not_found` |
| History of an Agent with acknowledgement rows | | Transitions as before, then `acknowledgements` `[{finished_since, seen, observed_at}]` in recording order | — |
| History with none | | `"acknowledgements":[]` | — |
| Acknowledgements read fails | | 500, detail only in the log | `internal` |

</intent-contract>

## Code Map

- `internal/httpapi/ack.go` -- new: the write routes. `writeRouteOf` recognises `/api/seen`,
  `/api/unseen` and the per-Agent `.../seen` and `.../unseen` (through `parseAgentPath`);
  `serveWrite` (POST only, else 405 `Allow: POST`); `guardWrite` (Origin, Sec-Fetch-Site,
  custom header, content type, in that order, after `ServeHTTP`'s Host check); `jsonContentType`;
  `readBody` (`http.MaxBytesReader`, and a read deadline of the handler's write timeout, since the
  server bounds only the headers' read); `decodeWrite` (one object, `DisallowUnknownFields`, no
  trailing token, positive `waiting_since`, 1 to 256 items, well-formed and distinct keys);
  `writeAckError` (the engine error mapping).
- `internal/httpapi/httpapi.go` -- `Options.Acknowledge` and `Options.Acknowledgements`, both
  required; `ServeHTTP` dispatches a write route before the read routes; doc comments say the
  surface is read-only but for acknowledgement.
- `internal/httpapi/history.go` -- `parseAgentPath` (the former `parseHistoryPath` body, returning
  the final segment), with `parseHistoryPath` now a thin wrapper, so the history and write paths
  name an Agent identically; `wireHistory.Acknowledgements` and `wireAcknowledgement`;
  `serveHistory` reads the rows after the Transitions (only when there are some) and sends `[]`
  for none; `boundWrite` factors the per-response write deadline shared by history and the write
  routes.
- `internal/httpapi/doc.go` -- package comment.
- `cmd/herddash/serve.go` -- wires `Acknowledge: e.Acknowledge` and
  `Acknowledgements: st.Acknowledgements`.
- `internal/httpapi/ack_test.go` -- new: the tests below.
- `internal/httpapi/history_test.go` -- `noAcknowledge`, `noAcknowledgements`; `fakeHistory`
  serves acknowledgement rows; history tests below.
- `internal/httpapi/engine_contract_test.go` -- the real engine now runs on a real store, and the
  handler on the real `Acknowledge`, `History` and `Acknowledgements`; the acknowledgement
  scenario below.
- `cmd/herddash/serve_test.go` -- the fake herdr's push can send a chosen status (`pushTo`);
  `TestServeAcknowledge`.
- Every other `httpapi.New` call in the tests passes the two new funcs.

## Tasks & Acceptance

**Execution:** all done, as mapped above, with these tests:

- `TestWriteValid`: per-Agent mark and undo, an escaped per-Agent path, bulk mark and undo, an
  upper-case Origin with `Application/JSON; Charset=UTF-8` and no `Sec-Fetch-Site`, and each body
  exactly at its bound: 200, views in request order, and the fake `Acknowledge` received exactly
  one request equal to the expected `AckRequest`.
- `TestWriteRefused`: one row per refusal in the matrix (44 rows, every case the story lists and
  more), each with its status and code, and `Acknowledge` never called; then 256 items pass.
- `TestWriteGuardBeforeBody`: a body that records any read is never read by an Origin,
  Sec-Fetch-Site, custom-header or content-type refusal.
- `TestWriteEngineErrors`: each engine error on a per-Agent and a bulk route maps to its status and
  code; the stale message names both keys, escaped; a store error's detail is logged at error and
  not sent; `ErrStopped` is 500 `store_failed`.
- `TestWriteCancelled`: a request cancelled while `Acknowledge` runs gets no response, whatever it
  returns.
- `TestWriteMethods`: every non-POST method (GET, HEAD, PUT, DELETE, PATCH, OPTIONS) on the four
  write routes is 405 `Allow: POST` (HEAD with no body); POST, OPTIONS and the rest on `/`, an
  asset, `/api/state` and history are 405 `Allow: GET, HEAD`, on `/events` 405 `Allow: GET`; a
  cross-site preflight on each write route is a 405. Every response in the file goes through
  `checkCommon`, which fails on any `Access-Control-*` header.
- `TestWriteUnknownPath`: malformed write paths are 404 and never reach the engine.
- `TestNewNilAcknowledgePanics`, `TestNewNilAcknowledgementsPanics`.
- `TestHistoryFound` (exact body now ends `"acknowledgements":[]`, and the rows are read once for
  the key), `TestHistoryAcknowledgements` (rows sent in recording order even where `observed_at`
  steps back; another key's history is `[]`), `TestHistoryAcknowledgementsError` (500, detail in
  the log only; a key with no Transitions is a 404 without reading acknowledgements).
- `TestEngineContract`: after the existing scenarios, `w1:p4` goes `done`; it is marked seen through
  `POST /api/agents/mac/default/1/w1%3Ap4/seen` with same-origin headers (retried while a Profile
  hold refuses it as stale), undone through `.../unseen`, marked again, then goes `working`. The
  test asserts each publication: seen with `class_rank` 2, unseen with rank 1, seen, working and
  unseen. The history route then holds three rows: seen, undone, seen, all for that finish. For
  each change, the stream carries an `agent` event with the publication's id and the right `seen`
  (or, for the move to `working`, the `transition` event). Replaying through
  `testdata/replay-board.mjs` gives, after every publication, a Board model equal to the published
  view, `seen` included.
- `TestServeAcknowledge` (the wiring, through the real `serve`, store and engine, with no new
  seam): the fake herdr's Agent finishes. A bulk `POST /api/seen` without `Origin` is 403
  `origin_not_allowed` and the history still shows `"acknowledgements":[]`. The same request with
  `Origin` is 200 with `seen` true, `/api/state` shows the Agent seen with the same
  `waiting_since`, and the history holds the row.

**Acceptance Criteria:** every criterion of Story 6.3 in `docs/planning/epics.md` is met by the
tests above. Story 6.4's browser check exercises the same wiring through the real binary from the
Board.

## Spec Change Log

## Review Triage Log

### 2026-10-27 — Self-review pass
- `[low]` `[accept]` `engine.ErrStopped` (a request that arrives as the Daemon shuts down) falls
  under the story's "other" mapping, 500 `store_failed`. Nothing was recorded, so the worst
  outcome is the Box staying Finished, which ADR-0014 calls benign. No story needed.
- `[low]` `[patch]` The server has no `ReadTimeout` (the event stream outlives any), so a write
  body trickled in byte by byte would hold its request open. `readBody` sets a read deadline of the
  handler's write timeout (10 s) for the body's read and clears it after.
- `[low]` `[accept]` A JSON body may name a field twice (`encoding/json` keeps the last). That is
  still exactly one object with known fields, and the result is a request the engine validates
  against its own state like any other.
- `[low]` `[accept]` The guard requires exactly one `Origin`, `X-Herddash-Write` and
  `Content-Type` header value, and one `Sec-Fetch-Site` value when present. A browser sends one of
  each, so a repeated header is refused as the matching failure.

## Design Notes

The guard is stateless, as ADR-0014 decided against a token. A cross-site page cannot set
`Origin`. It cannot send `X-Herddash-Write` or `application/json` without a preflight, and the
preflight gets a 405 with no CORS header, so the browser never sends the request. `Sec-Fetch-Site`
is checked when present to refuse a same-site page on another loopback port in browsers that send
it, though the `Origin` comparison already refuses that. A per-Agent route and a bulk route share
one decoder and one error mapping, and duplicate keys are refused in `httpapi` (before the engine)
so a malformed request is never the engine's to judge. Acknowledgement rows are read only for a key
with Transitions, because a key with none is a 404 regardless.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test -race ./...` -- all 12 packages pass (arch_test.go and callsite_test.go included)
- `go test -race -count=50 -run 'TestEngineContract|TestWrite' ./internal/httpapi` -- pass (259 s)
- `go test -race -count=50 -run TestServeAcknowledge ./cmd/herddash` -- pass (54 s)
- `node --test internal/httpapi/web/order.test.mjs` -- 91 pass, 0 fail
