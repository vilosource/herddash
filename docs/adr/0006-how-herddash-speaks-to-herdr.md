# ADR-0006: How herddash speaks to herdr

**Status: Accepted (amended 2026-10-16).** Written 2026-10-15 during the architecture run, under the operator's
delegation. It draws on [herdr-socket-api.md](../research/herdr-socket-api.md) and the PRD addendum
§B. Claims that record marks UNVERIFIED are marked UNVERIFIED here too, and milestone 1 exercises
them against a herdr 0.9.5 binary.

## Context

herdr exposes a JSON API over a Unix socket that is `0600` and owned by the operator. The research
established the following:

- A connection carries one request and closes, except subscriptions and waits. This is confirmed on
  0.8.2 and consistent with 0.9.3 source, but unverified on a 0.9.x binary.
- There is no global agent-status subscription. Of 27 subscription kinds, three require a pane id,
  and `pane.agent_status_changed` is one of them. This is confirmed against the installed schema.
- `events.subscribe` takes an array of subscriptions, but it is all or nothing: one stale pane id
  rejects the whole request and closes the connection. UNVERIFIED.
- Since 0.9.0, subscriptions deliver live events only, with no replay of history. UNVERIFIED by
  test.
- Since 0.9.2, falling behind produces `events_lost`, after which the server closes that
  subscription. Documented in herdr's 0.9.2 release notes; UNVERIFIED by test here.
- Snapshots and events share no sequence boundary.
- Lifecycle pushes use underscored names in `{event, data}`, while the pane-scoped streams keep
  dotted names. UNVERIFIED.
- `pane.focus` and `agent.focus` mutate herdr's seen state. An unbounded output read drives an
  agent's scroll interface and fails on a busy pane. Both are UNVERIFIED, and both carry an
  asymmetric cost.

herdr's latest release is 0.9.5 (2026-10-14). 0.9.4 made Antigravity and Codex dialogs show as
`blocked`, and 0.9.5 does the same for Claude Code questions in named sessions. The operator's
machine runs 0.8.2.

## Decision

- **One package owns the socket.** Only `internal/herdr` dials it. It provides mechanics (dial,
  ping, snapshot, subscribe, unsubscribe, stream-health signals) and makes no decisions. The engine
  owns the pane set, the reconcile timer, resync decisions and their serialisation (architecture
  spine AD-11).
- **One request per connection.** A fresh dial per call, no pooling, no multiplexing.
- **One connection per pane status subscription.** Each Agent pane's
  `pane.agent_status_changed` subscription runs on its own connection. The global lifecycle kinds
  (`pane.created`, `pane.closed`, `pane.agent_detected`) share one connection. A pane that closes
  between snapshot and subscribe then fails only its own subscription, not everyone's.
- **Subscribe before snapshotting**, at start and for every new pane.
- **Snapshot is truth** for which panes exist and their current Status. Status events are recorded
  as live observations. Buffered events are never replayed onto a snapshot. Events arriving during
  a snapshot read trigger another read.
- **Resync triggers.** `events_lost`, an unknown error code, or a rejected subscribe triggers a
  resync.
- **Version gate on `ping`'s version.** The `protocol` integer belongs to a different protocol.
  Supported: 0.9.5 or newer. Older servers are refused unless FR-16's dogfooding flag is set.
- **Find the socket herdr's way:** `--session`, `HERDR_SOCKET_PATH`, `HERDR_SESSION`, then the
  default session. This is resolved by `internal/config` and handed to `internal/herdr`.
- **Absent capabilities.** The client has **no** focus method and **no** pane-output method in v1.
  They are absent from its API, so no caller can reach them.
- **Wire types** are generated from `herdr api schema --json` of a 0.9.5 binary into
  `internal/herdr/gen`. They are mapped to core types at the package edge and never leak past it.
  The two envelope conventions are decoded there.

## Consequences

- The never-focus and never-read-output rules are enforced by the compiler.
- Open connections grow with Agent panes: 10 were observed on 2026-10-05 and 15 on 2026-10-13 on
  the operator's machine. That is acceptable at this scale.
- Against 0.8.2 the resync guarantee does not hold, since subscribing replays stale events and
  there is no `events_lost`. The herdr version on each row and FR-16's warning make that visible.
- A herdr protocol change touches one package.

## Alternatives considered and rejected

- **One connection carrying every pane's subscription in one array.** Fewer connections, but one
  closing pane rejects the whole request, and every change to the pane set means resubscribing
  everything.
- **herdr plugin hooks.** Rejected in ADR-0001 on capability: output events are excluded from hooks,
  and hooks fire a fresh process per event.
- **An unofficial Go client.** All are version 0 with stated instability (ADR-0001).
- **Polling `session.snapshot` only.** Misses short-lived Transitions between polls and loads herdr
  for nothing.
- **Hand-written wire types.** The research's field lists lean on an older bundled schema.

## Amendment, 2026-10-16: milestone-1 findings (Story 1.3)

Exercised against an isolated herdr 0.9.5. The evidence is in the socket API record's "Findings
against herdr 0.9.5". The decision stands.

- **Confirmed on 0.9.5:**
  - A connection carries one request and closes.
  - One unknown `pane_id` fails the whole subscribe request, and the server closes the connection.
  - Error responses echo the request id.
  - A new subscription delivers live events only.
  - Lifecycle pushes use underscored names in `{event, data}`; the pane-scoped streams keep dotted
    names.
- **`events_lost` does not appear anywhere in the 0.9.5 schema dump.** `error.code` is a free
  string, so `events_lost` has no generated type. herddash recognises it by matching the raw
  `error.code`. Its documented behaviour is still UNVERIFIED by test.
- **The wire-type generator** is genwire, a standard-library-only generator in
  `internal/herdr/gen/genwire`. It is run by `go generate ./internal/herdr/gen` against the
  committed 0.9.5 schema dump.
