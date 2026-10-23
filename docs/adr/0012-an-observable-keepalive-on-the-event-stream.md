# ADR-0012: An observable keepalive on the event stream

**Status: Accepted** on 2026-10-23, under the operator's Epic 5 delegation for a change that only
adds an event type (Story 5.11). It amends [ADR-0007](0007-read-only-loopback-http-surface.md)
and the spine's AD-7 and SSE convention by adding one event type to `/events`, and nothing else.

## Context

`/events` sends a keepalive every 15 s on a quiet stream, so that proxies and idle timers do not
close it. Until now it was an SSE comment line, `: keepalive`. EventSource consumes comments
itself and never passes them to the page, so the Board could not see it.

The Board marked the stream lost only on an EventSource `error`. A Daemon that is wedged or
SIGSTOPped, or a half-open socket, raises no error: the connection stays open and simply goes
quiet. The Board then went on showing stale Boxes, undimmed and with no strip, which breaks
FR-5's "never trust a stale Board" (Epic 3 retrospective, D1).

To tell a quiet Daemon from a hung one, the page needs a sign of life it can observe.

## Decision

- **The keepalive is a named event**: exactly `event: keepalive\ndata: {}\n\n`, sent on the same
  15 s ticker as before.
- **It has no `id:` line**, so `id:` stays the view sequence of real changes. It is not a change:
  it carries no data the Board applies.
- **The Board treats silence as a lost stream.** When the current source has delivered no event of
  any name, keepalive included, for `STREAM_SILENCE_MS` (35 s, in `web/order.mjs`), the Board shows
  the lost strip, dims its Boxes, closes the source and reopens `/events` with its usual backoff.
  A Go test keeps `STREAM_SILENCE_MS` above twice the server's keepalive interval.
- Nothing else changes: no new route, no change to any other event's shape, no `Last-Event-ID`
  handling and no server-side history.

## Consequences

- A hung Daemon or a half-open socket shows as lost within about 35 s, and a reopened stream starts
  with the whole state, as every stream does (AD-7).
- A client that does not listen for `keepalive` is unaffected: EventSource dispatches a named event
  only to listeners for that name.
- The keepalive interval and the Board's bound are now coupled. Lengthening the interval needs the
  bound raised too, and the test says so.

## Alternatives considered and rejected

- **Keep the comment and add a timer in the page.** Impossible: EventSource hides comments, so the
  page cannot tell a quiet Daemon from a hung one.
- **Re-send a `connection` event as the heartbeat.** It would tell every client that the
  ConnectionTruth changed when it did not, and it would carry the view sequence of no real change.
- **Poll `/api/state` from the page.** That is FR-4's rejected option, and fetching beside the
  stream is what ADR-0007 rules out.
