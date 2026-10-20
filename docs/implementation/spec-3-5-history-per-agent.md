---
title: 'Story 3.5: History per Agent'
type: 'feature'
created: '2026-10-20'
status: 'done'
baseline_commit: 'd64182a1c2b1224586f1b91f5c9079f4dcf1ea7d'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/implementation/spec-3-4-live-event-stream.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A later history page or tool cannot see what one Agent did. The record holds it, but
there is no route to it (FR-14, AD-3, AD-6).

**Approach:** `GET /api/agents/{host}/{server}/{epoch}/{pane}/history` returns that Agent's
recorded Transitions in the record's order. It reads only through an injected history function,
which serve backs with `store.History`.

## Boundaries & Constraints

**Always:**
- **Wiring:** `httpapi.Options` gains
  `History func(ctx context.Context, key core.AgentKey) ([]core.Transition, error)`. `New`
  panics if it is nil, as it does for View and Subscribe. serve passes the opened store's
  `History`. The handler never calls `Append` or anything that writes.
- **Route:** `/api/agents/{host}/{server}/{epoch}/{pane}/history`.
  - Each segment is percent-decoded from the escaped path, so `w1:p1`, `%3A` and a `/` encoded as
    `%2F` inside a segment all work.
  - It requires exactly these five segments after `/api/agents/` and a final `history`.
  - `epoch` must be a non-negative base-10 integer.
  - Every segment must be non-empty.
  - A malformed path gets 404 `not_found`.
  - The route follows the same Host check, method rules (GET and HEAD, `Allow: GET, HEAD` on 405)
    and headers as `/api/state`.
- **Response 200:** `{"key": AgentKey, "transitions": [ … ]}`.
  - Each transition uses Story 3.4's `wireTransition` shape. Reuse it; do not duplicate it.
  - Transitions come in History's order (`observed_at`, then recording order), so the one that
    began the current Attention Class is included, as Retention keeps it.
  - Epoch 0 (backfill) keys are served like any other.
- **Unknown key:** when History returns no rows, the answer is 404 `not_found` with message
  `no history for <key>`.
- **Errors:** a History error gets 500 `internal`, logged at error, with no detail in the body.
  A cancelled request returns without writing.

**Never:**
- Write anything.
- Filter or aggregate beyond the key (FR-14 v1).
- Call herdr.
- Add query parameters.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Found | GET /api/agents/mac/default/2/w1:p1/history; 3 rows | 200, key and 3 transitions in order with provenance | none |
| Escaped | pane `w1%3Ap1`, server `a%2Fb` | decoded to `w1:p1` and `a/b` before the lookup | none |
| Unknown | no rows | 404 `not_found` | error |
| Bad epoch | `x` or `-1` or empty | 404 `not_found`, History not called | error |
| Shape | extra or missing segment, or a last segment other than `history` | 404 | error |
| Backfill | epoch 0 key | 200 with backfill rows (`possibly_spurious` shown) | none |
| Store error | History returns an error | 500 `internal`, logged | error |
| Rules | POST, bad Host | 405 / 403 JSON | error |
| HEAD | HEAD on a found key | 200, no body | none |
| End to end | serve plus fake herdr; a live change recorded | GET history for w1:p1 shows the first sight and the live row | none |

</frozen-after-approval>

## Code Map

- `internal/httpapi/history.go` (new) -- path parsing and the handler; `history_test.go`.
- `internal/httpapi/httpapi.go` -- route the `/api/agents/` prefix and add `Options.History`.
- `internal/httpapi/events.go` -- `wireTransition` and its constructor. Reuse them from history.go.
- `cmd/herddash/serve.go` -- `History: st.History`. `serve_test.go` -- the end-to-end test.
- Existing httpapi tests that construct `Options` need a no-op History.

## Tasks & Acceptance

**Execution:**
- [x] `internal/httpapi/*` -- the route and tests for every handler matrix row
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- wiring and the end-to-end test

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes, the call-site test still passes and earlier tests still pass.

## Implementation Notes

- **Routing:** a path under `/api/agents/` is split from `r.URL.RawPath` when it is set, each
  segment then decoded with `url.PathUnescape` so `%2F` stays inside its segment; when RawPath is
  empty the path held no such escapes and `r.URL.Path` is split as it is. `EscapedPath()` is not
  used: when RawPath is not canonical (a raw non-ASCII byte or `"` elsewhere in the path) it
  re-escapes the decoded Path and loses every `%2F` (`TestHistoryEscapedMixed`). A path that does
  not parse is 404 before the method check, as an unknown route is; a parsed path then gets the
  `/api/state` method rules (405 with `Allow: GET, HEAD`). `ServeHTTP` records the decision in an
  `isHistory` flag.
- **Epoch** is parsed with `strconv.ParseUint(s, 10, 63)`: digits only (no sign, no space,
  leading zeros allowed), and at most 2^63-1, since the store keeps it in a signed column and
  would otherwise answer an over-range epoch with an error (500) instead of a 404.
- **The 404 message** renders the key as its escaped path segments,
  `no history for mac/default/2/w1:p1 (never recorded, or removed by Retention)`, so a server or
  pane containing `/` stays unambiguous.
- **Retention keeps the class-start row:** FR-14's "including the Transition that began its current
  class" is the store's guarantee, not this handler's. `TestSweepPresentKeepsClass` (and
  `TestSweepKeepsLatestWorking`, `TestSweepDoneThenIdle`) in `internal/store/sweep_test.go` show
  the sweep keeps the class start and everything after it; the handler serves History's rows
  unfiltered and in its order (`TestHistoryOrder`).
- **Write deadline:** the server has no `WriteTimeout` (SSE), so the handler sets
  `h.writeTimeout` through `http.ResponseController` before writing (ignoring `ErrNotSupported`)
  and clears it afterwards, so a later request on a kept-alive connection is not cut short.
- **Cancellation:** if the request context has ended after History returns, the handler returns
  without writing or logging, whatever History returned (rows, none, or any error).
- **HEAD costs as much as GET** on this route: it runs the same History query and marshals the
  same body (an unknown key must still be a 404, and `Content-Length` must match GET), then
  writes only the headers. The 200 body is marshalled whole and written through `writeJSON`.
- Existing tests pass `noHistory` (in `history_test.go`); `TestNewNilHistoryPanics` covers the
  nil check. A request with an invalid escape (`%ZZ`) is refused by `net/http` with 400 before
  the handler sees it; the parser still treats it as 404 defensively.
- **`TestServeHistory`** records a live change against the fake herdr and reads
  `/api/agents/testhost/test/1/w1%3Ap1/history`: `->working snapshot; working->blocked live`.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | ECH | Splitting `EscapedPath()` loses an encoded `/` when another segment forces re-escaping | medium | `EscapedPath` falls back to re-escaping the decoded Path when RawPath is not canonical; patch: split RawPath when set |
| 2 | VG, BH | HEAD and `Content-Length` assertions pass when the header is missing; no HEAD on an unknown key | low | VG pre-verified; patch |
| 3 | BH | `prefixAgents` doubles as a route marker | low | Patch: an explicit route decision |
| 4 | BH | The history body has no write deadline | low | The server has no WriteTimeout because of SSE; patch |
| 5 | BH | No test proves History's order is preserved | low | Patch |
| 6 | BH | The cancellation test misses rows or a non-context error after cancel | low | Patch |
| 7 | BH | The encode log lacks the key; a 404 does not say Retention may have removed rows | low | Patch |
| 8 | BH | No API doc lists the route; FR-14's consequence is not traced to a test | low | Patch: docs and a note naming the store test |
| 9 | ECH | History reads hold the store's single connection and can stall the engine's writes | low | The rows are fully read before anything is written to the client, so the connection is held only for an indexed local query; rejected |
| 10 | ECH, BH | Unbounded response size held twice in memory | low | One Agent within Retention; FR-14 forbids pagination in v1; rejected |
| 11 | BH | `02`, `histor%79`, `.` are accepted forms | low | Lookups are exact, nothing is unsafe; rejected |
| 12 | BH | HEAD costs as much as GET | low | Accepted trade-off, noted |
| 13 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
