---
title: 'Story 3.3: Read-only API on loopback with current state'
type: 'feature'
created: '2026-10-20'
status: 'done'
baseline_commit: 'a8c990fb3466712c8632474d611ed77fa42127d7'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/implementation/spec-3-2-the-engine-publishes-views-and-change-lists.md'
  - '{project-root}/docs/adr/0007-read-only-loopback-http-surface.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Nothing outside the terminal can see what herddash knows. The Board and any later
surface need one local, read-only source of the current state (FR-12, FR-17, AD-1, AD-6,
ADR-0007).

**Approach:** `internal/httpapi` serves `GET /api/state` from the engine's published View. It
rejects any non-loopback `Host`, every method but GET, and unknown paths, all in the JSON error
shape. `herddash serve` listens on the configured loopback bind.

## Boundaries & Constraints

**Always:**
- **Handler:** `httpapi.New(Options{View func() engine.View; Logger *slog.Logger}) http.Handler`.
  It reads only through `View`, never calls herdr, and never touches the store (history is Story
  3.5).
- **Host check, applied first:**
  - The request's `Host` header hostname, with any port, must be `localhost`, `127.0.0.1` or
    `[::1]`.
  - Anything else, including a missing host, gets 403 with code `host_not_allowed`.
- **Routes:** in this story only `/api/state` exists.
  - `GET` on it returns 200 with the View as JSON: `run_id`, `seq`, `agents` (already in sort
    order), `headers` and `connection`. `HEAD` behaves as GET without a body.
  - Any other method on a known route gets 405 with code `method_not_allowed` and
    `Allow: GET, HEAD`.
  - Any other path gets 404 with code `not_found`. That includes `/`, `/assets/`, `/events` and
    the history route until their stories.
- **Error shape:** `{"error":{"code":"…","message":"…"}}` with `Content-Type: application/json`.
- **Every response:**
  - `Content-Type: application/json` for JSON;
  - `Cache-Control: no-store`;
  - `X-Content-Type-Options: nosniff`;
  - never any `Access-Control-*` header, whatever the request (including an `Origin` header or
    an `OPTIONS` preflight, which gets 405).
- **serve:**
  - Listens on `cfg.Bind()` before starting the engine. A listen failure (port in use) prints
    `herddash: <err>` and exits 1.
  - Logs the actual address at info.
  - Serves the handler with `ReadHeaderTimeout`, and shuts down gracefully when ctx ends, before
    the engine's run returns.
  - The non-loopback refusal stays in config (existing).
- **Tests:** serve tests pass `--bind 127.0.0.1:0` through the shared `serveArgs` helper, so no
  test binds 7318. An end-to-end serve test reads the address from the log and GETs `/api/state`.

**Never:**
- Add a write route, a CORS header or any herdr call per request.
- Bind a non-loopback address.
- Serve the Board, `/events` or history (Stories 3.4, 3.5 and 3.7).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| State | GET /api/state, Host `127.0.0.1:7318`; View with 3 Agents | 200, JSON body equal to the View's JSON; agents in sort order | none |
| Hosts | Host `localhost`, `localhost:9`, `[::1]:7318` | 200 | none |
| Bad host | Host `evil.example`, `127.0.0.1.evil.example`, empty | 403 `host_not_allowed` | error |
| Method | POST, PUT, DELETE, PATCH or OPTIONS on /api/state | 405 `method_not_allowed`, `Allow: GET, HEAD` | error |
| HEAD | HEAD /api/state | 200, no body | none |
| Unknown | GET /, /nope, /api/state/x | 404 `not_found` | error |
| CORS | any request with an `Origin` header | no `Access-Control-*` header in the response | none |
| Headers | any response | `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` | none |
| No herdr | the handler with a fake View func | no client calls (the handler has no client) | none |
| Default bind | config defaults | listens on 127.0.0.1:7318 (asserted via config, not by binding) | none |
| Port in use | bind to an address already listening | serve exits 1 with `herddash: <err>` | error |
| Serve end to end | serve with fake herdr and port 0 | GET /api/state shows the fake's Agents with connection connected | none |

</frozen-after-approval>

## Code Map

- `internal/httpapi/httpapi.go` (new) -- Options, New, the host check, routing, JSON error writing, common headers. `httpapi_test.go` uses `httptest`.
- `internal/engine/view.go` -- the View's JSON tags exist (from Story 3.2). Use `engine.View` directly.
- `cmd/herddash/serve.go`:
  - Listen (`net.Listen("tcp", cfg.Bind())`) after the store opens and before `e.Run`.
  - `http.Server{Handler: httpapi.New(...), ReadHeaderTimeout: 10s}`, served in a goroutine, `Shutdown` on ctx end.
  - Log `listening` with the address.
  - `cmd → httpapi` is a permitted import.
- `cmd/herddash/serve_test.go` -- `serveArgs` adds `--bind 127.0.0.1:0`; the end-to-end test and the port-in-use test.

## Tasks & Acceptance

**Execution:**
- [x] `internal/httpapi/*` -- the handler and tests for every handler matrix row
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- the listener, shutdown and tests

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes, arch_test passes and earlier tests still pass.

## Implementation Notes

- **Bind validation matches the Host check.** `config.validateBind` now accepts only the hosts
  `localhost`, `127.0.0.1` and `::1` (written `[::1]:port`), and an all-digit port from 0 to 65535.
  Port 0 lets the kernel pick (needed for the spec's `--bind 127.0.0.1:0`). Any other loopback
  address and `localhost.` are refused, naming the accepted hosts, since the API would 403 every
  request on them. `TestLoopbackBinds` and the new `TestRefusedLoopbackBinds` cover it.
- **`TestNoArgumentsServes`** runs serve with no arguments, so it cannot use `serveArgs`; it writes
  `bind = "127.0.0.1:0"` to the hermetic home's default config file, so it does not bind 7318.
- **Order of checks:** Host first (403), then path (404), then method (405). A non-GET on an
  unknown path is a 404. Host matching is case-insensitive; a port must be 1 to 5 digits.
- **`New` panics on a nil `View`.** The state body is streamed after a 200; an encode or write
  failure is only logged. There is no 500 path.
- **Shutdown:** the engine runs under its own context. When ctx ends, serve shuts the HTTP server
  down first (5 s, then `Close`), then cancels the engine's context and waits for it.
- **Serve failure is fatal:** if `Serve` returns before shutdown, serve prints `herddash: <err>`,
  stops the engine and exits 1. `TestServeHTTPFailure` closes the listener under the server
  through the `listen` seam (a package variable, `net.Listen` outside tests).
- **stdout names the URL:** `<stamp> listening  http://<addr>` before the engine starts.
- **Listen failure** is also logged (`listen`) besides `herddash: <err>` on stderr.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | VG, ECH, BH | Config accepts loopback binds (127.0.0.2, `localhost.`) that the Host check then refuses on every request | medium | `validateBind` and `hostAllowed` disagree; patch: config accepts only the three hosts ADR-0007 names |
| 2 | ECH, BH | The engine can stop before the HTTP server shuts down, contrary to the spec | low | Shared ctx; patch: the engine gets its own context, cancelled after `Shutdown` |
| 3 | ECH, BH | An unexpected `Serve` failure leaves the Daemon running with no API, exit 0 | medium | Patch: fatal, exit 1 |
| 4 | BH | The listening address appears only in the JSON log | low | With port 0 there is no other way to find it; patch: one stdout line |
| 5 | ECH | `New` with a nil View panics per request | low | Patch: panic in `New` |
| 6 | ECH | A bind port of `-0`/`+0` passes validation | low | Patch |
| 7 | BH | The 500 branch cannot happen and is untested | low | Patch: drop it |
| 8 | ECH, BH | `TestServeAPI` fatal inside `waitFor`, body leak, and a freed-port dial that can hit another test | low | Patch |
| 9 | BH | SSE connections will make Shutdown wait out its timeout | low | Story 3.4's concern; patch: a note in serve.go |
| 10 | VG | `ReadHeaderTimeout` is untested | low | VG filed defer; a 10 s test or an injectable timeout for one line of server setup; rejected |
| 11 | BH | Host-rule wording (case, port digits) differs between the frozen text and the notes | low | The code accepts the three names case-insensitively with any numeric port, which satisfies the frozen "any port"; rejected |
| 12 | BH | The default-bind matrix row is covered only by config's existing test | false | `config_test.go` asserts the default bind; the matrix row says "asserted via config" |
| 13 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
