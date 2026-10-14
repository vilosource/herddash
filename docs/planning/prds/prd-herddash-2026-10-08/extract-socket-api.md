# Extract: herdr socket API constraints for the recorder

Source: `docs/research/herdr-socket-api.md`, recorded 2026-10-05 against herdr 0.9.3 documentation
and v0.9.3 source, plus live probing of a 0.9.2-era server. Confirmed facts were verified against
the installed herdr **0.8.2**. 0.9.3 is "a two-line hotfix over 0.9.2 affecting terminal
escape-sequence key handling, with no API changes" and "applies equally to 0.9.2."

## 1. Events/fields for status transitions

Agent status transitions arrive on `pane.agent_status_changed`, one of only three pane-scoped
subscription kinds (with `pane.output_matched` and `pane.scroll_changed`) that require a `pane_id`;
it takes an optional `agent_status` filter. **"There is no global agent-status subscription"** —
"the one event that must be subscribed per pane." Pane set is tracked from the global
`pane.created`, `pane.closed`, and `pane.agent_detected` streams.

Push envelopes differ by stream: lifecycle events use `{event, data}` with underscored names and a
redundant `type` in `data`; the three pane-scoped streams (including `agent_status_changed`) keep
dotted `event` names. "A decoder has to handle both."

Snapshot agent records require `terminal_id`, `agent_status`, `workspace_id`, `tab_id`, `pane_id`,
`focused`, `revision`; optional fields include `state_change_seq`, `tokens`, `agent_session`,
`state_labels`. "Use `revision` and `state_change_seq` for change detection." Since 0.9.2, an error
response echoes the original request id "whenever the line reached JSON decoding."

## 2. Not losing transitions

**Subscribe before snapshotting** (UNVERIFIED): since 0.9.0, a new subscription delivers "live
events and no longer replays retained history... Take the subscription first, then the snapshot,
or transitions in the gap are lost silently."

**`events_lost`** (UNVERIFIED, 0.9.2+): an error response bearing the subscription's request id,
`error.code` of `events_lost`, after which "the server closes that subscription connection. Other
clients are unaffected." It fires when a subscriber falls behind the server's shared, non-durable
retained history — "shared across event types, so it can fire even when the evicted events would
not have matched your filters, and it can fire while subscriptions are still initialising."
Documented recovery: open a new subscription, wait for `subscription_started`, keep reading it,
and request a snapshot "on a separate connection"; replace the cache; treat incoming events as
invalidation signals triggering another authoritative read, "rather than as deltas to apply."
**"Snapshots and events share no sequence boundary."** Serialise refreshes; refresh again if events
arrive mid-read.

**All-or-nothing subscribe** (UNVERIFIED): one bad `pane_id` rejects the whole subscribe request
and closes the connection; recovery is to refresh the pane list and retry, "never to assume the
other entries took effect" — a pane can close between the snapshot naming it and the subscribe
referencing it.

**Connection model** (confirmed): one request/response per connection, then it closes; only
`events.subscribe` and the wait methods keep a connection open — "a fresh dial per remote procedure
call, plus one long-lived connection per subscription."

## 3. Per-pane fan-out

Confirmed first-hand: exactly 3 of 27 subscription kinds need `pane_id` — `pane.output_matched`,
`pane.agent_status_changed`, `pane.scroll_changed`; the other 24 are global. Consequence: the
recorder must maintain one `agent_status_changed` subscription per live pane, adding/dropping as
panes come and go per the lifecycle streams.

## 4. Never do this

**Never call `pane.focus` or `agent.focus`** (UNVERIFIED seen-state semantics): focus calls mark a
pane seen; reads do not. "Calling focus would silently clear a completion the operator has not
actually looked at."

**Never do an unbounded `recent`/`recent_unwrapped` read** (UNVERIFIED): for a recognised
full-screen agent at the bottom of its transcript, requesting more `lines` than the visible screen
"drives the agent's own mouse-scroll interface"; if the agent is working, blocked, or unknown, that
same read fails with `agent_not_idle`. Always passive: `visible`, `detection`, subscriptions.
`recent` lines default to 80.

## 5. UNVERIFIED facts (explicit list)

- Unbounded `recent` reads driving scroll / `agent_not_idle` behavior.
- The `events_lost` error, its shape, and its recovery procedure (0.9.2 feature).
- The 0.9.0 live-events-only change and the subscribe-before-snapshot ordering.
- A single bad `pane_id` rejecting the whole subscribe request and closing the connection.
- The two differing push-envelope naming conventions.
- The 0.9.3 seen-state semantics (focus marks seen, reads don't).
- The absence of any concurrent-connection cap.

## 6. Versions

Record written against herdr 0.9.3 docs/source and 0.9.2-era live probing; confirmed items
verified against installed 0.8.2. Open item: whether one-request-per-connection still holds in
0.9.3 is unverified against that exact binary.
