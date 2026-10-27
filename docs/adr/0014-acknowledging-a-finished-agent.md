# ADR-0014: Acknowledging a Finished Agent ("Seen")

**Status: Accepted** on 2026-10-27 by the operator, who asked for the control and approved its
design from a mock rendered with their live Board (vfkb 9aee0fc69560). It amends
[ADR-0003](0003-the-board-is-the-product.md) (the Board gains its first controls),
[ADR-0005](0005-append-only-transition-record.md) (the record gains a second append-only table),
[ADR-0007](0007-read-only-loopback-http-surface.md) (the HTTP surface gains its first write
routes) and PRD SM-C1, and closes PRD §8 question 1 by operator decision. Built as Epic 6.

## Context

PRD §8 Q1 asked whether acknowledgement is needed at all, to be decided by Story 5.9 from what the
record shows after two weeks of dogfooding. The operator decided it directly instead: after daily
use since 0.1.0, a Finished Box stays Finished until the Agent's next turn, however long ago the
operator read its result, so a workspace group's Finished count stops meaning "results I have not
read". herdr's own `done` to `idle` Transition, which happens when the operator views the pane in
a terminal client, does not help: `idle` reached from `working` is still Finished (PRD §3), by
design, so that Waiting Time is not reset by looking.

ADR-0001 settled the shape long ago: a finished-pane set beside a server generation marker, as
devswha's client keeps one. herddash's AgentKey already carries the server generation (the epoch,
AD-3), and its record is append-only, so the set becomes rows in the record keyed by AgentKey.

The operator's approved design, from the mock: a `✓ Seen` pill on Finished Boxes only; an
acknowledged Box turns grey, labelled SEEN, and moves down among the Idle Boxes, and its group's
finished count drops; it lasts until the Agent's next Transition; herdr is never told; a
workspace heading offers `✓ Mark N finished seen` when it has two or more unseen Finished Boxes;
a 6 s Undo bar follows each mark; there is no keyboard shortcut (the operator navigates with
Vimium, which reaches ordinary buttons).

Three things make this structural rather than a Board tweak: the Board gains controls (ADR-0003,
SM-C1), the stored schema gains a table (ADR-0005, AD-10), and the HTTP surface gains write routes,
its first, which a page on another origin must not be able to call (ADR-0007).

## Decision

### What an acknowledgement is

- **herddash's own state.** An acknowledgement says the operator has read a Finished Agent's
  result. herdr is never told: no `pane.focus`, no `agent.focus`, no call of any kind, and
  `internal/herdr` gains nothing. A Status, an Attention Class and a Transition are unchanged by
  it; the record of Status stays exactly what herdr reported.
- **It names one exact finish**: the AgentKey and the instant the Agent's current Finished class
  began, `FoldResult.Since`, which the view already sends as `waiting_since`. Two finishes of one
  Agent have different start instants, and a new Agent on a reused AgentKey starts its fold over
  (AD-3), so the pair never names a finish the operator did not see.
- **Only a Finished Box can be acknowledged**: Attention Class `finished`, not Background (ADR-0009),
  with a known class start. Needs you, Idle, Background, Working and Unknown cannot.
- **It lasts until the Agent's next Transition out of that finish**: any Transition that ends the
  Finished class or restarts it (a new turn, Needs you, a new Agent's first row, a new epoch).
  Precisely, it is in force while the Agent's fold has class `finished` and `Since` equal to the
  acknowledged instant. herdr's `done` to `idle` (or back) within the same Finished class moves
  neither, so it does not end the acknowledgement: that Transition is herdr's own seen state
  flipping because the operator looked at the pane, which agrees with the acknowledgement rather
  than contradicting it.
- **It can be undone**, by a further row, never by deleting one.

### The record (migration 0005)

Acknowledgements get **their own append-only table**, not a row kind in `transitions`, because an
acknowledgement is not a change of Status: a row kind would need a nullable `new_status`, would
have to be skipped by the fold, the importer, the Retention sweep's working-row rule and every
reader of Transitions, and would weaken the transitions table's CHECKs. A separate table leaves
the definition of a Transition (PRD §3) exact and the transitions table untouched.

```sql
CREATE TABLE acknowledgements (
    id             INTEGER PRIMARY KEY,                    -- recording order
    host           TEXT    NOT NULL,
    server         TEXT    NOT NULL,
    epoch          INTEGER NOT NULL CHECK (epoch >= 1),    -- never backfill's epoch 0
    pane_id        TEXT    NOT NULL,
    finished_since INTEGER NOT NULL CHECK (finished_since > 0), -- the finish named, UTC ms
    seen           INTEGER NOT NULL CHECK (seen IN (0, 1)),     -- 1 acknowledged, 0 undone
    observed_at    INTEGER NOT NULL                         -- the Daemon's clock, UTC ms
);
CREATE INDEX acknowledgements_agent ON acknowledgements (host, server, epoch, pane_id, finished_since, id);
CREATE INDEX acknowledgements_observed_at ON acknowledgements (observed_at);
```

- Rows are inserted and never updated. An undo is a row with `seen = 0` for the same finish.
- **Whether a finish is acknowledged is derived from the record**: the latest row, in recording
  order (`observed_at`, then `id`), for the Agent's key and its current `Since` decides; no row
  means not acknowledged. The rule is one pure function in `internal/core` (AD-1). So the state
  survives a Daemon restart with nothing else saved: the restart rebuilds the fold from the record
  (AD-12), gets the same `Since`, and reads the same rows.
- `core` owns the `Acknowledgement` type and its constructor, which validates every field; one
  `INSERT` in `store` writes every row, from `store.AppendAcknowledgements`, which writes a batch
  in one transaction (all or nothing) and is called only by the engine (`callsite_test.go`). `store.Acknowledgements`
  reads one Agent's rows in recording order, for the engine and the history route.
- **Retention sweeps them with their Agent's rows**, in the same transaction as the Transitions
  and resync markers: a row older than the cutoff is deleted unless its key is a present Agent
  and its `finished_since` is at or after that Agent's class start (all of a present key's rows are
  kept when its class start is unknown). An acknowledgement in force is therefore never swept.
- Like every migration, 0005 is a one-way door: a binary that knows schema 4 (0.7.x and earlier)
  refuses a database migrated to 5.

### The engine decides (the single writer, AD-2)

- An acknowledgement request reaches the engine actor as an inbox message with a reply channel,
  like the existing `Agents` query. `engine.Engine.Acknowledge` is the only entry point; the HTTP
  layer never writes to the store.
- **The engine validates against its current state**, never the client's: the key names a present
  Agent under the epoch in force; its class is `finished` and it is not Background; its class
  start is known and **equals the instant the request names**; and the pane holds no row the
  record does not (no Profile hold in progress, AD-14, and the record holds its fold). A request
  that names an older finish, or an Agent that has moved on, is **refused as stale (409)** and
  writes nothing. Acknowledging a finish already acknowledged succeeds and writes nothing.
- An undo names the same finish. It writes a `seen = 0` row only when that finish is acknowledged
  now; otherwise there is nothing to undo, and it succeeds and writes nothing, so an Undo pressed
  after the Agent moved on is harmless.
- A bulk request is **all or nothing**: every item is validated first and the rows are written in
  one transaction. One stale item refuses the whole request, which then names the stale keys.
- With recording off (no store, or recording turned off for the run) acknowledgement is refused
  (503): an acknowledgement that does not survive a restart is not the decided feature.
- After a successful write, the engine updates its state and publishes once, with one `agent`
  change per Agent whose view changed. The reply carries the resulting views.
- At an Agent's first sight seeded from the record, and whenever its fold is reseeded, the engine
  reads the Agent's acknowledgements and applies the core rule. Any Transition that moves the
  fold's class or `Since` makes the rule false without a read; a read failure is logged and leaves
  the Agent unacknowledged (the Board shows more, never less).

### The view

- `AgentView` gains `seen` (boolean, always sent): true exactly when the core rule holds.
  `attention_class` stays `finished`; the record's facts are unchanged.
- **A seen Agent ranks with Idle**: its sort key's `class_rank` is 2, Idle's, with Idle's newest-
  first `start`, so its Box moves down among the Idle Boxes of its workspace group. Background
  still takes precedence (rank 3) if background work opens on a seen Agent.
- The Board draws a seen Box as its own kind, `seen`: grey, labelled SEEN, no pill; a workspace
  group's counts list it as `N seen` and not as finished. The title bar and the browser title
  count Needs you only and are unchanged.

### The write routes (amending ADR-0007)

The surface stays one loopback listener. All existing routes and methods are unchanged. Four
`POST` routes are added; they are the only routes that accept anything but `GET`/`HEAD`:

| Route | Body | Does |
|---|---|---|
| `POST /api/agents/{host}/{server}/{epoch}/{pane}/seen` | `{"waiting_since": <ms>}` | acknowledge one finish |
| `POST /api/agents/{host}/{server}/{epoch}/{pane}/unseen` | `{"waiting_since": <ms>}` | undo it |
| `POST /api/seen` | `{"agents": [{"key": {host, server, epoch, pane_id}, "waiting_since": <ms>}, ...]}` | acknowledge several, all or nothing |
| `POST /api/unseen` | same shape | undo several |

The per-Agent path is parsed exactly as the history path is. The bulk form is how the Board marks
a workspace's Finished Boxes: it names each exact finish it showed, never "everything in workspace
X", so nothing the operator did not see is acknowledged. It takes 1 to 256 items, with no key
twice. A success is `200` with `{"agents": [AgentView, ...]}` in request order.

**Guard, in this order, before any body is read** (a cross-site request must never reach the
engine):

1. **Loopback `Host`**, as today (DNS rebinding).
2. **Same-origin `Origin`, required**: the `Origin` header must be present and equal
   `http://` + the request's `Host`, compared case-insensitively. Missing, `null` or any other
   origin is refused `403 origin_not_allowed`. A browser always sends `Origin` on a `POST`, so the
   Board passes; another page's origin cannot match.
3. **`Sec-Fetch-Site`, when present, must be `same-origin`**, else `403 origin_not_allowed`.
4. **A custom header, required**: `X-Herddash-Write: 1`, else `403 write_header_missing`. A page
   on another origin cannot send a custom header without a CORS preflight, which herddash never
   answers (no CORS headers anywhere; `OPTIONS` is a 405).
5. **JSON only**: `Content-Type` must be `application/json` (a `charset=utf-8` parameter allowed),
   else `415 unsupported_media_type`, which also forces a preflight on any cross-site attempt.
6. **Bounded body**: at most 4 KiB per-Agent and 64 KiB bulk, else `413 body_too_large`; exactly
   one JSON object, unknown fields and trailing data refused `400 bad_request`.

Then the engine's answer maps to `404 unknown_agent`, `409 not_finished`, `409 stale_view`,
`503 recording_off` or `500 store_failed`, all in the existing JSON error shape. A `GET` or `HEAD`
on a write route is `405` with `Allow: POST`; a `POST` on any read route stays `405` with its
existing `Allow`. The Board's Content-Security-Policy is unchanged (`connect-src 'self'` already
allows the request). There is still no authentication, so the listener still refuses a non-loopback
bind (UJ-4's ADR is still required before any exposure).

### The event stream and history

- An acknowledgement or undo reaches `/events` as the `agent` event of each Agent whose view
  changed. No event name is added; an acknowledgement is not a Transition (FR-13 is unchanged).
- The history route's body gains `acknowledgements`: the Agent's rows in recording order, each
  `{"finished_since", "seen", "observed_at"}`, always sent (`[]` when none). The Board shows no
  history; the API does, for a later surface.

### The import graph

No edge is added. `httpapi` already imports `engine` (the published view) and calls
`engine.Engine.Acknowledge` through an `Options` function, as it calls `View` and `Subscribe`;
`store.AppendAcknowledgements` is called only by `engine`, which already imports `store`.
`arch_test.go`'s import rules are unchanged. `callsite_test.go`, which keeps each store write to
its callers, guards `AppendAcknowledgements` as engine-only (`internal/engine` and
`internal/store`, which defines it), so neither `httpapi` nor the importer may call it.

## Consequences

- The Board's Finished count means "results I have not read" again, and the operator's daily
  glance (SM-1) loses the Finished Boxes already dealt with.
- **ADR-0003 is amended by one kind of element**: the Board may carry acknowledgement controls,
  the per-Box `✓ Seen` pill, the workspace heading's `✓ Mark N finished seen` button and the 6 s
  Undo bar. It still has no pane control, no terminal, no prompt input, no output and no action on
  herdr. Any other control is still a new decision.
- **ADR-0005 is amended**: the record holds Transitions, resync markers and acknowledgements, all
  append-only, Retention the only delete.
- **ADR-0007 is amended**: the surface is "read-only except acknowledgement". Its security claim
  narrows from "cannot change anything" to "can change nothing but herddash's own acknowledgement
  of a finish, and only from its own page".
- **PRD SM-C1** admits the three controls above; **PRD §8 Q1** is closed by operator decision;
  §6.2's "Acknowledgement" out-of-scope item and §4.4's "no POST routes" NFR are amended; FR-5a
  states the requirement. Story 5.9 is superseded by Epic 6 and its open checkpoint closed.
- Schema 5 is a one-way door for older binaries.
- The worst failure is benign: a refused or lost acknowledgement leaves the Box Finished, which is
  today's behaviour.

## Alternatives considered and rejected

- **Decide by the record (Story 5.9).** The operator chose not to wait: the problem is not that
  herdr's Status is wrong, it is that a correct Finished stays on the Board after it was read.
- **Tell herdr (`pane.focus`, `agent.focus`).** Forbidden (PRD §5): it mutates herdr's seen state
  and would also clear completions on every other client.
- **Client-only state (the mock's `localStorage`-like map).** Lost on another browser or device,
  not in the record, not in history, and the sort key is the Daemon's (AD-1), so the Box could not
  move down without the Board computing its own order.
- **A row kind in `transitions`.** Rejected above: an acknowledgement is not a Status change.
- **A mutable "acknowledged" column or set in `server_state`.** A second source of truth for a
  user action that should be in history; engine state is for what the record cannot hold.
- **Bulk by workspace id.** Could acknowledge a finish that arrived after the Board last drew.
- **Token-based CSRF protection.** A token would need a session or a secret in the page; with a
  loopback listener and no CORS, a required same-origin `Origin` plus a custom header and JSON
  content type is sufficient and stateless.
- **Ending the acknowledgement at any Transition at all, `done` to `idle` included.** The operator
  looking at the pane in herdr would bring a seen Box back as Finished, the opposite of what
  looking means.
