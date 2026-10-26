# ADR-0013: A title bar on the Board

**Status: Accepted** on 2026-10-26 by the operator, who asked for it and approved its design
rendered with their live Board (Story 5.19). It amends [ADR-0003](0003-the-board-is-the-product.md)
by one element: the Board may carry one title bar. PRD SM-C1 and the Board design's "Deliberately
absent" section are amended to match.

## Context

The approved Board design (Story 3.6, amended 2026-10-21) has no title bar, logo or footer: the
page opens straight onto the Needs you zone or the first workspace group, and the strips appear
above them only when something is wrong. SM-C1 counts every element on the Board, and anything
beyond Boxes, zone headings and the two strips needs an ADR superseding ADR-0003.

After daily use since 0.1.0 the operator asked for "a standard title pane, with a text logo".
Without one the page does not say what it is, and its two most important facts, whether
anything needs the operator and whether herdr is connected, are told only by the zones present
and by a strip that is usually absent.

## Decision

- **One title bar, sticky with the strips** at the top of the page: on the left, a text logo,
  `herddash` in the monospace font with `herd` bold, after a mark of three bars in the Board's
  red, amber and green; on the right, a summary.
- **The summary repeats facts the Board already shows, and adds none.** It names how many
  Agents need the operator ("2 need you", or "Nothing needs you"), the number the browser title
  already carries; and herdr's connection: `herdr <version>` with a green, amber (advisory) or red
  (refused) dot, or "herdr disconnected" or "Reconnecting…" when the strips say so. The rules are
  `titleBar` in `web/order.mjs`.
- **It is not a control and not a live region.** It holds no link or button, and the strips
  stay the announced live region; the summary is read in the header's reading order.
- Nothing else changes: no new route, no API field, no change to the Boxes, zones or strips.

## Consequences

- The Board says what it is and whether anything needs the operator before any Box is read.
- SM-C1's element list grows by one. The bar's summary must stay a restatement: a new fact in it,
  such as a count the Board does not otherwise show, a host name or a control, is a new decision.
- The bar costs about 2.5 rem of height at the top of every view, more on a phone, where the
  summary wraps under the logo.
