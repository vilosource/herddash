# ADR-0016: A recording strip on the Board

**Status: Accepted** on 2026-10-30 by the operator, who chose this option when Story 7.1 met the
Board design's limit of two strips. It amends [ADR-0003](0003-the-board-is-the-product.md) and
[ADR-0014](0014-acknowledging-a-finished-agent.md) by one element: the Board may carry a third
strip, shown only on a run that records nothing. PRD SM-C1 and the Board design's "Strips" and
"Deliberately absent" sections are amended to match.

## Context

When the Daemon starts and its saved engine state (`server_state`) cannot be loaded, the engine
turns recording off for the whole run (Story 2.2, AD-3): starting at the first epoch and saving it
would key new rows like old ones. It logs one error and goes on observing and publishing. Nothing
on `/api/state` or the Board says so, so the Daemon looks healthy while it writes no Transition,
no resync marker and no acknowledgement (the write routes answer 503 `recording_off`). The Epic 2
retrospective recorded this as F-6, and the operator took it up as Story 7.1 on 2026-10-30.

The approved design limits the Board to the connection and version strips, and SM-C1 says
anything more needs an ADR superseding ADR-0003.

## Decision

- **The published view gains a top-level `recording` boolean**, always sent on `/api/state` and
  in the stream's `state` event: true when the engine has a Recorder for the run, false when
  saved state failed to load. It is fixed for a run, so no change event carries it.
- **The Board shows a recording strip while `recording` is false**: red, like the refused strip,
  saying herddash is not recording this run because its saved state could not be loaded, that
  Boxes are live but no history is being written, and to see `herddash.log`. It comes after the
  connection and version strips. Boxes are shown and not dimmed: what they show is current.
- **An absent field means recording**, so a Board talking to an older Daemon draws nothing new.
- The title bar's summary does not repeat it (ADR-0013: it restates, and adds no fact).
- Nothing else changes: no new route, no change to the record, the Boxes or the controls.

## Consequences

- A Daemon that records nothing says so where the operator looks, until it is restarted with a
  loadable state.
- SM-C1's element list grows by one strip, shown only in a failure no one has yet observed.
- `recording` is a new always-sent field of `engine.View`; the spine's JSON table lists it.
