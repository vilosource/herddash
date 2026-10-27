# ADR-0003: The board is the product, and the recorder is what makes it trustworthy

**Status: Accepted.** Decided by the operator 2026-10-08 during the PRD rework, written
2026-10-14. Amends one consequence of [ADR-0001](0001-what-herddash-is.md), which otherwise stays
in force. Amended by [ADR-0013](0013-a-title-bar-on-the-board.md) (a title bar, 2026-10-26)
and [ADR-0014](0014-acknowledging-a-finished-agent.md) (acknowledgement controls, 2026-10-27).

## Context

ADR-0001 accepted option B: herddash is a headless recorder that persists every agent status
transition with per-account attribution, beside whichever browser interface the operator prefers.
One of its consequences reads: "The board becomes a secondary surface, not the product."

Reworking the PRD against ADR-0001 showed that consequence to be wrong about how the operator
actually uses herddash. The daily value is a browser window open all day beside the terminal,
showing one box per agent: working, finished, or waiting on the operator. The operator glances at
it and does not interact with it. Both of the reworked PRD's primary success metrics measure the
board: SM-1, that the board is the operator's first glance rather than cycling panes, and SM-2,
that no agent waits more than ten minutes without the board having shown it.

A PRD that calls the board secondary produces epics that treat it as secondary, and they would
under-build the one thing the operator actually looks at.

ADR-0001's warning still holds. The page overlaps the existing browser interfaces most, so it must
stay restrained or it drifts back into duplicating them. What changes is what that restraint is
for. It is no longer a reason to keep the page small. It is what distinguishes the product.

## Decision

**The board is the feature herddash is measured by.** It is a notification center: one box per
agent showing its status, the profile that owns it and how long it has been waiting, sorted
longest-waiting first. It is read-only. It has no terminal, no chat, no prompt input and no pane
control, and it never will within this product.

**The recorder engine is what makes the board trustworthy.** Subscribing to herdr, persisting
transitions, attributing panes to profiles, and serving a read-only API and event stream are not a
separate product beside the board. They are why the board's waiting times survive a restart, why
its profile names are right, and why a later surface can be built from the same record.

ADR-0001's scope is unchanged. herddash still runs beside a full browser client rather than
replacing one, and still records history and attribution that neither prior-art project keeps.

## Consequences

- The PRD leads with the board (§4.1) and measures success by it (§7, SM-1 and SM-2). The
  recorder requirements follow as the engine the board depends on.
- The build order still follows the data flow: read herdr, record and attribute, serve, then the
  board (PRD addendum §J). The first three milestones are the engine, but the product is not
  delivered until the fourth puts the board in front of the operator.
- The PRD's counter-metric SM-C1 holds the board to boxes and the connection strip, so the
  restraint is measured, not just stated.
- Restraint is a requirement, not a matter of taste. A request to add pane control, output
  streaming or prompt input to the board is a request for a different product and needs a
  superseding ADR, not a story.
- ADR-0001's other consequences stand: backfill matters, the existing plugin is retired once the
  daemon works, and authentication stays deferred with loopback-only binding.
- ADR-0001 is not edited, per the immutability rule in this directory's README. Its board
  consequence is read through this record.

## Alternatives considered and rejected

- **Leave ADR-0001's consequence standing and treat the board as secondary.** Rejected because it
  misdescribes the operator's use, and planning derived from it would under-build the part that
  delivers the value.
- **Supersede ADR-0001 entirely.** Rejected as too broad. Its decision, scope and other
  consequences are correct; only one consequence was wrong.
- **Make the board a full browser client after all.** Rejected for the reasons ADR-0001 gives
  against option C: two mature projects already do that, and the restraint is the differentiator.
