---
title: 'Story 3.6: Board visual design'
type: 'feature'
created: '2026-10-20'
status: 'in-progress'
baseline_commit: '84b8da4b1cf4230b4c6bea1f81b04951b70e0961'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/adr/0003-the-board-is-the-product.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Board is the product (ADR-0003). It is glanced at all day from another window,
often across a desk, so Needs you must stand out and nothing else may compete for attention.
Its look has not been decided, and Story 3.7 cannot build it until the operator approves one
(FR-1 to FR-3, NFR10, NFR14, SM-C1).

**Approach:** Design the Board against a fixture drawn from the operator's real `/api/state`.
Deliver a short design note and one static, self-contained HTML page that renders the fixture in
the proposed design, in light and dark, including the strips. The operator approves it before
Story 3.7.

## Boundaries & Constraints

**Always:**
- **Fixture:** `docs/design/board-fixture.json` (committed with this spec).
  - It is a real 2026-10-20 capture, anonymised and topped up so every case appears: all four
    classes, three workspaces, `unresolved`, Profile equal to the program (codex, agy), exact and
    lower-bound times, an unknown start, and long and empty Context Lines.
  - Waiting Times are measured from its `now_ms`.
  - Do not change its data. You may only add a top-level key the page needs.
- **Design note:** `docs/design/board-design.md`, short (about one page). It defines:
  - Tokens for palette, type and spacing, light and dark, with contrast stated for text and for
    the Needs you emphasis.
  - Box anatomy: heading, directory, workspace and tab, Attention Class, Waiting Time with `≥` for
    a lower bound, and the Context Line on one line with an ellipsis and full text on hover. Every
    Box has the same fixed height, whatever its content.
  - The emphasis for each class:
    - Needs you is visually dominant, readable across a desk, and never colour alone.
    - Finished is quieter.
    - Working and Unknown recede.
    - An `idle` Agent never seen `working` gets no Waiting Time emphasis.
  - The workspace header in the lower zone, and the zone separation.
  - The connection strip (disconnected, with the time since the last snapshot) and the version
    strip (advisory: herdr below the minimum, including the resync caveat below 0.9.0; refused:
    nothing shown). Both appear only when needed.
  - Responsive behaviour from a narrow side window to a wide monitor.
  - What is deliberately absent (NFR10/SM-C1): no controls, no excerpts, no second data type.
- **Static page:** `docs/design/board.html`, one file with inline CSS and JS and no network
  fetches, fonts or CDNs.
  - It embeds a copy of the fixture, or loads `board-fixture.json` next to it when opened as a
    file (fetch won't work over file://, so embedding a copy is preferred).
  - It orders Boxes by `sort_key` and draws workspace headers at `headers`.
  - It shows Waiting Times as of `now_ms`, ticking from page load for realism.
  - A small design-review-only toolbar, marked as such and not part of the Board, toggles:
    - light, dark and system theme;
    - the strips: none, disconnected, advisory or refused.
  - It uses system fonts only.
- **Accessibility basics:** a sensible reading order, Needs you conveyed with text as well as
  colour, and a visible focus outline.
- Nothing under `internal/` changes; this story produces the design only.

**Never:**
- Build the real Board, serve `/` or `/assets/` (Story 3.7), or add npm or any dependency.
- Add any Board element beyond Boxes, workspace labels and the two strips.
- Mark the design approved. Only the operator records that gate.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Render | open board.html in a browser | every fixture Agent as one Box, ordered by sort_key, headers at fixture indexes | none |
| Needs you | 3 needs_you Agents | dominant treatment, top of the page, readable at a distance | none |
| Long title | a Context Line longer than the Box | one line, ellipsis, full text in `title` | none |
| Empty title | Context Line `""` | its line kept (Box height unchanged) | none |
| unresolved | Profile `unresolved` | heading shows `unresolved · claude` | none |
| Lower bound | `waiting_exact` false | `≥ 12m` style | none |
| Unknown start | no `waiting_since` | no Waiting Time shown (a dash) | none |
| Dark | toolbar → dark | all tokens switch; contrast meets the note | none |
| Strips | toolbar → disconnected / advisory / refused | the specified strip; refused shows no Boxes | none |
| Uniform | every Box | identical height (checked visually and by the CSS) | none |

</frozen-after-approval>

## Code Map

- `docs/design/board-fixture.json` -- already written; read it, don't change its data.
- `docs/design/board-design.md` (new) and `docs/design/board.html` (new).
- `internal/core/view.go` -- the AgentView fields and their JSON names.
- PRD §4.1 (FR-1 to FR-5), §3 (glossary: Attention Class, Waiting Time, Box, Context Line), the SM-C1 counter-metric; spine AD-1 (the Board computes only the clock, order and headers).

## Tasks & Acceptance

**Execution:**
- [ ] `docs/design/board-design.md` -- the design note
- [ ] `docs/design/board.html` -- the static page, with the fixture embedded

**Acceptance Criteria:**
- Given `docs/design/board.html` opened from disk with no network, then it renders every matrix row as described, in light and dark.
- Given the repository's checks, then `go build ./... && go test ./...` still pass (nothing in Go changes).

## Implementation Notes

## Spec Change Log

## Review Triage Log

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- expected: all pass (unchanged)
- `grep -E 'https?://' docs/design/board.html` -- expected: no external resource URLs (inline only)

**Manual checks:**
- Open `docs/design/board.html` in a browser and check every matrix row, in light and dark.
