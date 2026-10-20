---
title: 'Story 3.6: Board visual design'
type: 'feature'
created: '2026-10-20'
status: 'done'
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
- [x] `docs/design/board-design.md` -- the design note
- [x] `docs/design/board.html` -- the static page, with the fixture embedded

**Acceptance Criteria:**
- Given `docs/design/board.html` opened from disk with no network, then it renders every matrix row as described, in light and dark.
- Given the repository's checks, then `go build ./... && go test ./...` still pass (nothing in Go changes).

## Implementation Notes

- Verified in headless Chrome at 1440 px and 500 px (the browser's minimum); a 320 px column was checked through a narrowed container, not a 320 px viewport.
- The strip demo data (herdr 0.8.2, last snapshot 2m 40s earlier) is set in page code; the fixture is unchanged apart from a trailing newline.
- The embedded fixture is a hand-kept copy of `board-fixture.json`; Story 3.7 should load the real `/api/state` instead.
- Operator approved the design on 2026-10-20 (see Spec Change Log); the zero-Agent bare Board stands as designed.

## Spec Change Log

- **2026-10-20, operator at the approval checkpoint.** The operator approved the design as is,
  kept Needs you ordered longest-waiting first (PR #32's decision), and authorised changing the
  fixture's data despite the frozen "do not change its data": agent 0 `wA:p9` became `wB:p9` and
  the agy agent's duplicate `wB:p7` became `wA:p7`, so every AgentKey is unique and pane-id
  prefixes match their workspaces (`wA` knowledge-base, `wB` payments-platform, `wC` ops). The
  embedded copy in `board.html` was changed identically; the sort order is unchanged because
  the affected Agents already differ earlier in their sort keys. Resolves triage 1-2.

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | ECH, BH, VG | Fixture has two Agents with key `devbox/default/1/wB:p7` | medium | Confirmed by parsing the fixture; a live `/api/state` cannot produce it and Story 3.7 keys Boxes by AgentKey; fixing it changes fixture data, which the frozen block forbids; intent gap, asked at the approval checkpoint |
| 2 | BH | Anonymisation scrambled pane-id prefixes (`wA:p9` in payments-platform, `wB:p7` in knowledge-base) | low | Same root as 1 (fixture data); asked with 1 |
| 3 | ECH | A loud lower-bound Waiting Time can overflow the status row in a narrow column; the note claims every row ellipsises | medium | `.status-row` is nowrap without overflow handling, so digits are clipped silently; patch |
| 4 | ECH, BH | The disconnected strip's ticking time is inside an aria-live region | medium | Re-announced every second; patch |
| 5 | ECH | `headers` index past the end or unordered throws | low | Headers come from the engine; guard adds a branch; rejected |
| 6 | ECH | `sort_key` of the wrong length compares as equal | low | Produced by the engine; rejected |
| 7 | ECH | Empty directory, workspace or tab renders `undefined` or a bare `›` | low | AgentView fields can be empty strings; direct fallback; patch |
| 8 | ECH | `waiting_since` of 0 treated as unknown | low | Direct `== null` fix; patch |
| 9 | ECH | Note says dark strips are "the same, darkened" but the dark refused strip is lighter | low | Wording; patch |
| 10 | VG | The page compares `sort_key[4]` as one string; Go compares host, server, numeric epoch, pane id | medium | `internal/core/view.go:215-237`; differs from epoch 10 and on prefix hosts; the page labels this helper as Story 3.7's; patch: compare the key fields, epoch numerically, and say so in the note |
| 11 | VG, BH | Nothing keeps the embedded fixture in step with the file | low | The fix would add a check to this spec; rejected; noted for Story 3.7 |
| 12 | BH | Full Context Line reachable only by mouse; focusable Boxes do nothing | medium | `title` only; patch: each Box gets an accessible name carrying the full Context Line and directory, and the note says what focus is for |
| 13 | BH | The note does not define how the directory is shortened (FR-2) | medium | End-truncation cuts the segment that names the project; patch: define start-truncation keeping the last segment, and name where `~` substitution belongs |
| 14 | BH | Empty states undefined: no Agents, empty top zone, no lower zone | medium | Note silent; patch the note and page (no rule above a header when the top zone is empty; the zero-Agent board stays bare, since the spec admits no other element, flagged for the operator) |
| 15 | BH | The "never snapshotted" strip text is coded but not designed | low | Patch the note |
| 16 | BH | Disconnected plus advisory, which the note specifies, cannot be shown | medium | Strips control is one radio group; patch: a combined option |
| 17 | BH | Waiting Times on a disconnected Board: keep ticking or freeze? | medium | Unspecified, bears on SM-3; patch: decide in the note (keep ticking, dimmed, with the strip saying the data is old) |
| 18 | BH | Responsive claims not in the acceptance matrix | low | The fix edits this spec; rejected; 500 px and 1440 px measured, 320 px not seen (Implementation Notes) |
| 19 | BH | `.dir` font size disagrees with the `--text-meta` token | low | Direct; patch |
| 20 | BH | A media rule sets the default padding | low | Direct deletion or real narrow gutter; patch |
| 21 | BH | Workspace repeated in Boxes under a workspace header | low | FR-2 lists workspace and tab in the Box; kept consistent across zones; rejected |
| 22 | BH | Status mismatch; approval mechanism vague | low | Status mismatch is workflow ordering (false part); patch the note to name the sprint-status action item |
| 23 | BH | Duration format inconsistent at unit boundaries | low | `1h 04m` vs `2d 3h`; patch: define the format and its boundaries |
| 24 | BH | `board-fixture.json` lacks a trailing newline | low | Not data; direct; patch |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- expected: all pass (unchanged)
- `grep -E 'https?://' docs/design/board.html` -- expected: no external resource URLs (inline only)

**Manual checks:**
- Open `docs/design/board.html` in a browser and check every matrix row, in light and dark.
