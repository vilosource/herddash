# ADR-0007: A read-only HTTP surface on loopback

**Status: Accepted.** Written 2026-10-15 during the architecture run. It carries PRD decisions
FR-12 to FR-14 and FR-17 and ADR-0003's restraint.

## Context

herddash's one network surface serves the Board and a read-only API for future surfaces. devswha's
herdr-web-ui already listens on `127.0.0.1:7317` with the `drive` role and no authentication. That
is the bar to stay clear of, and the port to avoid. v1 has no authentication, so nothing can be
exposed beyond loopback.

## Decision

- **One listener**, bound to loopback by default. Its default port is distinct from 7317; the
  number is chosen in milestone 3 and recorded in the configuration documentation.
- **GET only.** The routes are `/`, `/api/state`, `/api/agents/{host}/{server}/{pane}/history` and
  `/events`. There are no POST, PUT or DELETE routes.
- **No herdr call per request.** Responses come from the engine's published view or from a store
  read, never from herdr.
- **Server-sent events with no replay.** Each event is stamped with the view sequence number. A
  client refetches `/api/state` on every connect and applies only later events.
- **Startup refusal.** A non-loopback bind is refused while no authentication exists.
- **No CORS headers**, so a page from another origin cannot read the API from a browser.

## Consequences

- The surface cannot change anything, which is herddash's real security difference from devswha's
  client.
- Exposure to a phone or a private network needs a new ADR adding authentication first (PRD UJ-4).
- With no replay buffer the server stays stateless per client; a reconnect costs one `/api/state`
  fetch.

## Alternatives considered and rejected

- **WebSockets.** Two-way capability the read-only surface must not have, and more code than
  server-sent events.
- **Polling `/api/state` from the page.** Rejected by FR-4: updates are pushed.
- **An SSE replay buffer keyed by `Last-Event-ID`.** A second state store whose semantics can drift
  from `/api/state`.
