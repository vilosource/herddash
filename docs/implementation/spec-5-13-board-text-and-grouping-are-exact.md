---
title: 'Story 5.13: Board text and grouping are exact'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_commit: '889ba6b873cabc98e8024d56fe9ebd63df25ee80'
baseline_revision: '889ba6b873cabc98e8024d56fe9ebd63df25ee80'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/implementation/epic-5-context.md'
  - '{project-root}/docs/implementation/epic-3-retro-2026-10-23.md'
warnings: ['oversized']
deferred:
  - summary: >-
      A session recap's bidi controls are not dropped, and reach the Box's title and aria-label,
      which a bdi element cannot isolate.
    evidence: |-
      core/transcript.go recapOf only trims; order.mjs boxLabel puts contextText (the recap when
      there is one) into the Box's title and aria-label. Pre-existing recap path (ADR-0010); the
      story's AC names the Context Line or session name, which the Daemon now cleans.
    location: >-
      internal/core/transcript.go recapOf; internal/httpapi/web/order.mjs boxLabel
    severity: low
  - summary: >-
      The spine's AD-1 still defines the zones as "Top zone (Needs you, Finished)" and "Lower
      zone (Working, Unknown)", which the 2026-10-21 operator decision replaced.
    evidence: |-
      board-design.md and core/view.go put Needs you alone in zone 0 and every other class in
      zone 1. Pre-existing documentation drift; Story 5.8 (documents agents read match the code)
      is the place for it.
    location: >-
      docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md AD-1
    severity: low
  - summary: >-
      The Context Line row's bdi wrapping is asserted only by the hand-run board-browser.mjs
      check, not in CI.
    evidence: |-
      CI runs node --test on internal/httpapi/web/*.test.mjs only; check/ is hand-run by design.
      Reverting the bdi in board.mjs would pass CI. Run by hand for this story: 23 checks ok,
      including the bdi check. Story 5.6 (CI runs the Board's browser check) closes it.
    location: >-
      internal/httpapi/web/board.mjs fillBox; internal/httpapi/web/check/board-browser.mjs
    severity: medium
---

<intent-contract>

## Intent

**Problem:** Three Board text and grouping faults from the Epic 3 retro (D4, D8). (1) Every
lower-zone Agent whose workspace herdr did not position gets `Group = SortUnknown`
(`core/view.go:290-292`), so different workspaces merge under one header named after whichever
view sorts first. (2) Bidi controls (U+202E and the rest) pass `oneLine` and the Context Line row
is not bidi-isolated, so a pane's process can reorder the Box's text. (3) Claude Code's glyph
fallback drops any leading non-letter, non-digit rune followed by a space, eating a session name's
leading `-`, `#` or emoji, while a bare `✳` survives.

**Approach:** Carry the pane's workspace id into `core.PaneContext`; give each unpositioned
workspace its own group number, derived from its id, in a range above every real position and
below `SortUnknown` (kept for a pane with no workspace id at all). Strip Unicode `Bidi_Control`
runes in `oneLine`; wrap the Board's Context Line row text in `<bdi>`. Strip only Claude Code's
recorded status glyphs (`✳`, `◐`) as a prefix, and a title that is only a glyph says nothing.

## Boundaries & Constraints

**Always:** sort key stays the 5-element `[zone, group, class_rank, start, key]` (AD-1, Board
validation unchanged); `internal/core` stays stdlib-only, no clock (`hash/fnv` is stdlib);
group values stay integers ≤ `Number.MAX_SAFE_INTEGER`; the Board still only draws a header
where `sort_key[1]` changes; the golden fixture is shared by Go and `order.test.mjs`.

**Never:** no ADR-level change (no wire-shape change, no new field in `AgentView`'s JSON, no
store schema change); no stripping of other format characters such as ZWJ (emoji sequences);
no change to how positioned workspaces sort; no change to recap content.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Two unpositioned workspaces | panes in `w7` and `w8`, position 0 | two distinct groups, each headed by its own label, both after every positioned group | none |
| No workspace id | position 0, `WorkspaceID` "" | group `SortUnknown`, last | none |
| Bidi control | title `"fix‮gnp.exe"` | Context Line `"fixgnp.exe"` | none |
| Leading `-`/`#`/emoji | `"- wip"`, `"# notes"`, `"🚀 launch"` | unchanged | none |
| Claude glyph | `"✳ fix-auth"`, `"◐ fix"`, `"✳"`, `"◐ Claude Code"` | `"fix-auth"`, `"fix"`, `""`, `""` | none |

</intent-contract>

## Code Map

- `internal/core/view.go:20-41` -- Claude Context Line rule; glyph fallback at 32-35.
- `internal/core/view.go:56-64` -- `oneLine`: add `unicode.Is(unicode.Bidi_Control, r)` to the drop set.
- `internal/core/view.go:193,271-299` -- `newSortKey` takes `WorkspacePosition`; must also take the workspace id. `SortUnknown` at 269.
- `internal/core/observe.go:46-64` -- `PaneContext`: add `WorkspaceID`.
- `internal/herdr/map.go:47-58` -- `paneContext` closure: set `WorkspaceID: workspaceID`.
- `internal/herdr/client_test.go:~449` -- `mapSnapshot` table test; expected contexts gain the id.
- `internal/engine/engine.go:1275` -- `p.context = obs.Context` (whole struct; no change needed).
- `internal/core/view_test.go:12-37,316-376` -- Context Line table; golden fixture loader (`boardFixture.Input` gains `workspace_id`).
- `internal/core/testdata/board-order.json` -- shared golden fixture; `w9:p1` is the existing no-workspace case.
- `internal/httpapi/web/board.mjs:83-87` -- Context Line row: text into a `<bdi>` child.
- `internal/httpapi/web/order.mjs:203-206` -- `headerLabel` (unchanged; correct once groups are per workspace).
- `internal/httpapi/web/order.test.mjs:58-90,222` -- golden fixture order/headers tests.
- `docs/design/board-design.md:104-112`, `ARCHITECTURE-SPINE.md` AD-1 -- document the unpositioned-group rule.

## Tasks & Acceptance

**Execution:**
- `internal/core/observe.go` -- add `PaneContext.WorkspaceID` -- identity for unpositioned groups.
- `internal/herdr/map.go` + `client_test.go` -- populate and assert it.
- `internal/core/view.go` -- `oneLine` drops `Bidi_Control`; Claude rule strips only `✳`/`◐` (prefix + space, or the whole title); `newSortKey` gives position ≤ 0 with a non-empty id the group `2^52 + fnv64a(id) mod (2^52-1)`, else `SortUnknown`; update doc comments.
- `internal/core/view_test.go` -- I/O matrix cases for `ContextLine`; a sort-key test for two unpositioned workspaces, the empty id, and ordering after positioned ones; fixture loader reads `workspace_id`.
- `internal/core/testdata/board-order.json` -- add two Agents in two unpositioned workspaces (one labelled), regenerate order/headers, extend `description`.
- `internal/httpapi/web/board.mjs` -- `<bdi>` in the Context Line row.
- `internal/httpapi/web/order.test.mjs` -- assert the fixture's unpositioned groups get separate headers with their own labels.
- `docs/design/board-design.md`, spine AD-1 -- one sentence each on unpositioned groups.

**Acceptance Criteria:**
- Given two Agents in different workspaces herdr did not position, when the Board lays out, then each has its own header with its own workspace label, after all positioned groups.
- Given a Context Line holding bidi controls, when shown, then `/api/state` carries it without them and the Board renders it inside a `<bdi>`.
- Given a session name starting with `-`, `#` or an emoji, when shown, then it is unchanged.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 22 findings — high 0, medium 4, low 12, false 3, maybe-false 3
- findings:
  - `[low]` `[patch]` (edge-case) Two unpositioned ids can collide on the 52-bit hash and merge — real but negligible; the "never share a header" wording in view.go and the spine was softened to "practically never", naming the collision.
  - `[medium]` `[patch]` (edge-case) Unpositioned groups all read "(no workspace)" — herdr leaves position 0 only for a workspace missing from the snapshot list, whose label is then always "" (map.go), so headers were indistinguishable from the no-id group; `headerLabel` now reads "(unlisted workspace)" for an unlabelled group in [2^52, sentinel), tests updated. A positioned group with an empty label keeps "(no workspace)", since it is listed.
  - `[low]` `[defer]` (edge-case) Recap bidi controls reach the Box's title/aria-label — real; pre-existing recap path outside the AC's Context Line/session name; deferred.
  - `[maybe-false]` `[reject]` (edge-case) Claude Code may emit status glyphs other than ✳/◐ — only those two are recorded (2026-10-19 live check, spec 3.1); a third would need one raw title observed; if true only low (the raw-title fallback path), so rejected.
  - `[medium]` `[patch]` (blind) Identical "(no workspace)" headers for w8 and w9 — same root cause as the edge-case header finding; same patch.
  - `[maybe-false]` `[reject]` (blind) Glyph list may be incomplete — as above.
  - `[low]` `[defer]` (blind) Recap and Box label not bidi-filtered — same as the edge-case recap finding; deferred.
  - `[low]` `[patch]` (blind) "Never share a header" overstated — same as the collision finding; wording patched.
  - `[low]` `[patch]` (blind) Hash order among unpositioned groups undocumented for the operator — board-design.md now says they come in a stable but arbitrary order, by a hash of the workspace id.
  - `[low]` `[defer]` (blind) Spine AD-1 zone definitions are stale — pre-existing drift; deferred to Story 5.8.
  - `[low]` `[reject]` (blind) No Board-level test of two Boxes in one unpositioned group — core's TestUnpositionedGroups covers it, and the JS compare is plain numeric on values below 2^53, already exercised by w7/w8; not worth a further fixture row.
  - `[false]` `[reject]` (blind) TestUnpositionedGroups checks only the header count — it also checks first and last; a positioned pane moved into the middle would add a header and fail the count of four.
  - `[false]` `[reject]` (blind) No spec, sprint status or API docs — the spec is this file; sprint-status is the orchestrator's; no API document defines sort_key's group range besides board-design.md and the spine, both updated.
  - `[low]` `[reject]` (blind) Fixture description is one long string — cosmetic and the fixture's existing pattern.
  - `[low]` `[patch]` (blind) board-design.md line past the wrap width — paragraph rewrapped to ~100 columns.
  - `[medium]` `[defer]` (verification-gap) The bdi wrapping is checked only by the hand-run browser check — real (CI runs only *.test.mjs); ran by hand for this story (23 ok, bdi check included); Story 5.6 puts it in CI.
  - `[medium]` `[patch]` (intent) "Their own name" holds only in a fixture herdr cannot produce; in production every unpositioned header read "(no workspace)" — same root cause as the header finding; patched with "(unlisted workspace)". The workspace's real name is not available from herdr when it is unlisted.
  - `[low]` `[defer]` (intent) Recap stripped of nothing and unisolated in title/aria-label — same as the recap finding; deferred.
  - `[false]` `[reject]` (intent) Raw `pane_title` in the API still carries U+202E — the Board never draws pane_title (grep of web/*.mjs); it is the raw field by design.
  - `[low]` `[reject]` (intent) Workspace, tab and header labels are not isolated — operator-set herdr labels, outside the AC's Context Line/session name; unlikely to carry bidi controls.
  - `[low]` `[defer]` (intent) No bidi control is sent through the rendered DOM in a test — same root cause as the verification-gap finding; deferred with it.
  - `[maybe-false]` `[reject]` (intent) Glyph list may miss real glyphs — as above.

## Design Notes

The group is hashed rather than ranked because a view is built per Agent in core with no view of
other workspaces; ranking would make one Agent's group depend on others and force extra change
events. FNV-1a over a few workspace ids in a 2^52-wide range makes a collision (two groups
merging, the old behaviour) negligible. Unpositioned groups order among themselves by hash:
stable, arbitrary. herdr omits a position only when a pane's workspace is missing from the
snapshot's workspace list, in which case the label is also unknown and the header reads
`(no workspace)`; the group is still that workspace's own.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- pass
- `gofmt -l .` -- prints nothing
- `node --test internal/httpapi/web/*.test.mjs` -- pass
- `node internal/httpapi/web/check/board-browser.mjs` (per its header, if Chrome is available) -- pass

## Auto Run Result

Status: done

**Summary.** Each workspace herdr did not position now gets its own Board group, `2^52 +
FNV-1a(workspace id) mod (2^52-1)`, after every positioned group, headed by its label or
`(unlisted workspace)`. A pane with no workspace id keeps the `SortUnknown` group, `(no workspace)`.
The Daemon drops Unicode Bidi_Control runes from the Context Line, and the Board draws the
Context Line row inside a `<bdi>`. Claude Code's raw-title fallback strips only `✳ ` / `◐ `, and a
title that is only a glyph says nothing, so a session name's leading `-`, `#` or emoji is kept.

**Files changed.**
- `internal/core/observe.go` -- `PaneContext.WorkspaceID`.
- `internal/herdr/map.go`, `internal/herdr/client_test.go` -- populate and assert it.
- `internal/core/view.go` -- `oneLine` drops Bidi_Control; Claude glyph rule; `UnpositionedGroup`; `newSortKey` takes the id.
- `internal/core/view_test.go` -- Context Line matrix cases; `TestUnpositionedGroups`; fixture loader reads `workspace_id`.
- `internal/core/testdata/board-order.json` -- w7:p1 and w8:p1 in two unpositioned workspaces; order and headers regenerated.
- `internal/httpapi/web/board.mjs` -- Context Line row text in a `<bdi>`.
- `internal/httpapi/web/order.mjs` -- `headerLabel` reads `(unlisted workspace)` for an unpositioned group.
- `internal/httpapi/web/order.test.mjs` -- unpositioned headers and labels; header-label cases.
- `internal/httpapi/web/check/board-browser.mjs` -- asserts the `<bdi>`.
- `docs/design/board-design.md`, spine AD-1 -- the unpositioned-group rule.

**Review.** 22 findings. Patched 4 entries: one medium (unpositioned headers indistinguishable),
three low (collision wording, hash order documented, line wrap). Deferred 3 (recap bidi in the
Box label, low; stale spine zone definitions, low; bdi check not in CI, medium). Rejected: the
glyph-list completeness (maybe-false, low if true, fallback path only), a Board-level same-group
test (covered in core), the header-count test (false), missing docs (false), the fixture's long
description (cosmetic), raw `pane_title` (false, not drawn), unisolated operator labels (outside
the AC). Follow-up review: not recommended (one medium patched, no high).

**Verification.** `go build ./...`, `go vet ./...`, `go test -count=1 ./...` all ok; `gofmt -l .`
empty; `node --test internal/httpapi/web/*.test.mjs` 75 pass, 0 fail; the hand-run
`board-browser.mjs` against a scratch build with its own fake herdr: exit 0, 23 checks ok,
including the `<bdi>` check.

**Residual risks.** An unlisted workspace's real name is unknowable from herdr's snapshot, so
its header is the honest `(unlisted workspace)`; a 52-bit hash collision would still merge two
such groups; Claude Code glyphs other than `✳`/`◐` in a raw title would now show.
