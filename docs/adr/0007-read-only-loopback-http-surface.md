# ADR-0007: A read-only HTTP surface on loopback

**Status: Accepted.** Written 2026-10-15 during the architecture run, under the operator's
delegation. It carries the PRD's FR-12 to FR-14 and FR-17 and ADR-0003's restraint.

## Context

herddash's one network surface serves the Board and a read-only API for future surfaces.
devswha/herdr-web-ui already listens on `127.0.0.1:7317` with the `drive` role and no
authentication. That is the bar to stay clear of, and the port to avoid. v1 has no authentication.

Binding to loopback does not, by itself, stop a web page in the operator's browser from reading a
local service. A page on another origin can use DNS rebinding to make its own hostname resolve to
`127.0.0.1`. Leaving out CORS headers does not stop that, because the browser then treats the
request as same-origin.

## Decision

- **One listener**, bound to loopback by default, on a default port distinct from 7317.
- **GET only.** The routes are `/` (the root path only), `/assets/` (embedded Board files),
  `/api/state`, `/api/agents/{host}/{server}/{epoch}/{pane}/history` and `/events`. There are no
  write routes. Other methods get a 405 in the JSON error shape.
- **Loopback `Host` only.** A request whose `Host` header is not `localhost`, `127.0.0.1` or `[::1]`
  (with any port) is rejected. This closes the DNS-rebinding read.
- **No CORS headers.**
- **No herdr call per request.** Responses come from the engine's published view or a store read.
- **The event stream begins with the whole state.** Each connection's first event is the full
  current state, with exactly `/api/state`'s body plus a run id and view sequence, followed by
  changes in publish order. There is no replay buffer and no `Last-Event-ID` handling. Each stream
  has a bounded buffer; a client that falls behind is disconnected and reconnects to a fresh state.
- **Startup refusal.** A non-loopback bind is refused while no authentication exists.

## Consequences

- The surface cannot change anything, which is herddash's real security difference from devswha's
  client.
- Exposure to a phone or a private network needs an ADR adding authentication first (PRD UJ-4).
- A reconnecting client costs one full-state event. The server holds no per-client history.

## Alternatives considered and rejected

- **WebSockets.** A two-way capability the read-only surface must not have.
- **Polling `/api/state` from the page.** Rejected by FR-4.
- **An SSE replay buffer keyed by `Last-Event-ID`.** A second state store whose semantics can drift
  from `/api/state`.
- **Fetch `/api/state`, then subscribe.** It loses whatever changes between the two requests.
