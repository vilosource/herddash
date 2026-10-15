# ADR-0006: How herddash speaks to herdr

**Status: Accepted.** Written 2026-10-15 during the architecture run, from the protocol constraints
in [herdr-socket-api.md](../research/herdr-socket-api.md) and the PRD addendum §B. Items that record
marks UNVERIFIED stay unverified here; milestone 1 exercises them against a 0.9.2-or-newer binary.

## Context

herdr exposes a JSON API over a Unix socket. Its shape forces most of the client design. A
connection carries one request and closes, except subscriptions. There is no global agent-status
subscription. Subscriptions replay nothing. A lost-events condition closes the stream. Two calls,
`pane.focus` and `agent.focus`, mutate herdr's seen state, and an unbounded output read drives an
agent's own scroll interface. herdr is at 0.9.4 (2026-10-13); the operator's machine runs 0.8.2.

## Decision

- **One package owns the socket.** Only `internal/herdr` dials it. No other package imports a
  socket or speaks the wire format.
- **One request per connection.** A fresh dial per call, and one long-lived connection per
  subscription. No pooling, no multiplexing.
- **Subscriptions.** Global lifecycle subscriptions track the pane set. One
  `pane.agent_status_changed` subscription runs per Agent pane. Subscribe before snapshotting, for
  every new pane as well as at start.
- **Truth.** `session.snapshot` is the source of truth, and events are only invalidation signals.
  Reconcile on a timer regardless of events.
- **Resync.** A lost-events signal, an unknown error code, or a rejected subscribe triggers a
  resync. Resyncs are serialised: one at a time across all connections. Each one is recorded as a
  resync marker (ADR-0005).
- **Connection.** Find the socket by herdr's own order: `--session`, `HERDR_SOCKET_PATH`,
  `HERDR_SESSION`, then the default session. Reconnect forever with backoff. Version-gate on `ping`
  (FR-16).
- **Absent capabilities.** The client has **no** focus method and **no** pane-output method in v1.
  They are absent from its API, not merely unused, so no caller can reach them.
- **Wire types** are generated from `herdr api schema --json` of a 0.9.2-or-newer binary and live
  in `internal/herdr/gen`. They never leak past the package; it maps them to core types at its edge.

## Consequences

- The never-focus and never-read-output rules are enforced by the compiler.
- The number of open connections grows with the number of Agent panes, about 15 on the operator's
  machine today. That is acceptable, and it is the only design the protocol allows.
- Against 0.8.2 the resync guarantee does not hold: there is no lost-events signal, and
  subscribing replays stale events. FR-16's flag and the herdr version on each row make that
  visible rather than hidden.
- A herdr protocol change touches one package.

## Alternatives considered and rejected

- **herdr plugin hooks.** Rejected in ADR-0001 on capability: output events are excluded from
  hooks, and hooks fire a fresh process per event.
- **Adopt an unofficial Go client.** All are version 0 with stated instability (ADR-0001).
- **Poll `session.snapshot` only, no subscriptions.** Simple, but it misses short-lived Transitions
  between polls and loads herdr for nothing.
- **Hand-written wire types.** The research's field lists lean on an older bundled schema;
  generating from the binary removes the guesswork.
