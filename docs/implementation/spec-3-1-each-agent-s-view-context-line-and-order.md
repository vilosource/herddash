---
title: "Story 3.1: Each Agent's view: Context Line and order"
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: '378459bff640b523bdf2588ec7bd3f07ce01dddd'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/implementation/spec-2-4-waiting-time-that-survives-restarts.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The API, the Board and any later surface must describe each Agent in exactly the
same way: what it shows, where it sorts, and where workspace headers fall. Nothing yet defines
that once (FR-2a, FR-3, AD-1).

**Approach:**
1. In `internal/core`, add the Context Line rules, `AgentView` and its sort key, built by one
   pure constructor.
2. Commit a golden fixture of a mixed Board, with its expected order and header positions, for
   the Go tests now and the Board's JS tests later.

## Boundaries & Constraints

**Always:**
- **Context Line.** `ContextLine(program AgentProgram, paneTitle string) string` uses a rule
  table keyed by Agent Program. The global default is `""`.
  - `claude`: the Pane Title, trimmed, unless it is empty or equal to `Claude Code`, in which case
    `""`.
  - Any program with no rule: `""`.
  - A new rule is one entry in the table, with a comment documenting the program's title
    behaviour.
- **Shortened directory.** `ShortenDir(dir, home string) string`:
  - `dir` equal to `home`, or under it, gets the home prefix replaced by `~`.
  - Otherwise `dir` is returned as is. An empty `home` changes nothing.
- **AgentView.**
  - Built by `NewAgentView(in AgentViewInput) AgentView`. The input carries the AgentKey, Agent
    Program, Profile, Status, the fold result (`FoldResult`), the `PaneContext`, `home` and `now`.
  - Fields, with JSON tags in snake_case (an empty Context Line is serialised as `""`, never
    omitted):
    - `key` (an object: `host`, `server`, `epoch`, `pane_id`), `agent_program`, `profile`,
      `heading` (from `core.Heading`);
    - `directory` (shortened), `workspace`, `tab`, `status`, `attention_class`;
    - `waiting_since` (UTC ms, omitted when unknown), `waiting_exact`;
    - `pane_title`, `context_line`, `sort_key`.
  - `now` is only used for nothing that is stored. The view stores `waiting_since`, not a
    duration.
- **Sort key.** `[zone, group, class_rank, start, agent_key]`, compared element by element by
  `SortKey.Less`:
  - `zone`: 0 for needs_you and finished, 1 for working and unknown.
  - `group`: 0 in zone 0; in zone 1, the workspace's `WorkspacePosition`, where 0 (unknown)
    sorts last as `MaxInt`.
  - `class_rank`: needs_you 0, finished 1, working 2, unknown 3.
  - `start`: `waiting_since` ascending, so the longest wait comes first. Unknown sorts after
    known, as `MaxInt64`.
  - `agent_key`: `host`, `server`, `epoch`, then `pane_id`, compared as strings, epoch
    numerically.
  - The sort key serialises as a JSON array of five values, the last being the key string
    `host/server/epoch/pane_id`.
- **Board order.** `SortViews([]AgentView)` sorts in place. `WorkspaceHeaders([]AgentView) []int`
  returns the indexes in sorted order where a zone-1 group starts, including the first zone-1
  view.
- **Golden fixture.** `internal/core/testdata/board-order.json` holds about 12 views across 3
  workspaces and every class, including an unknown `waiting_since`, an equal-start tie and an
  unknown workspace position. It has `input` (shuffled view inputs as JSON), `order` (the expected
  key strings) and `headers` (the expected indexes). A Go test builds, sorts and compares. The
  file is documented as the shared fixture for Story 3.7.
- Core stays standard-library only and never reads the clock.

**Never:**
- Change the engine, the store or the printer. Story 3.2 builds views from the engine.
- Add HTTP.
- Duplicate the class or heading logic. Reuse `ClassOf` and `Heading`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Session name | claude, title `fix-auth-bug` | context_line `fix-auth-bug` | none |
| Placeholder | claude, title `Claude Code` or `""` or `  ` | `""` | none |
| No rule | codex, title `anything` | `""` | none |
| Heading once | profile codex, program codex | heading `codex` | none |
| Heading both | profile cldw, program claude | heading `cldw · claude` | none |
| Home dir | `/Users/x/proj`, home `/Users/x` | `~/proj` | none |
| Prefix only | `/Users/xy/proj`, home `/Users/x` | unchanged | none |
| Top zone flat | needs_you in ws2 (since 10), needs_you in ws1 (since 20), finished (since 5) | ws2 needs_you, ws1 needs_you, then finished | none |
| Lower grouped | working in ws2, working in ws1, unknown in ws1 | ws1 working, ws1 unknown, ws2 working | none |
| Unknown since | two needs_you, one with since unknown | the known one first | none |
| Tie | equal class and since | ordered by agent_key | none |
| Headers | golden board | header indexes as committed | none |
| JSON | view with empty context line | `"context_line":""` present; sort_key is an array | none |

</frozen-after-approval>

## Code Map

- `internal/core/view.go` and `view_test.go` (new) -- ContextLine and its rule table, ShortenDir, AgentViewInput, AgentView, SortKey, Less, SortViews, WorkspaceHeaders.
- `internal/core/testdata/board-order.json` (new) -- the golden fixture.
- Reuse: `attention.go` (`ClassOf`, `FoldResult`), `profile.go` (`Heading`), `observe.go` (`PaneContext`), `types.go` (`AgentKey`).
- `docs/how-herddash-works.md` -- one short paragraph on the Context Line rules and how to add one, if the doc has a suitable section; otherwise the doc comment on the rule table is the documentation.

## Tasks & Acceptance

**Execution:**
- [x] `internal/core/view.go`, `view_test.go`, `testdata/board-order.json` -- every matrix row

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes and arch_test passes (core is standard-library only and reads no clock).

## Implementation Notes

- **AgentKey JSON tags.** `AgentKey` gained `json:"host"`, `"server"`, `"epoch"`, `"pane_id"` tags
  so the view's `key` is the spec's object. Nothing marshalled an `AgentKey` before.
- **Class and start.** The class is the fold's when it is `Known`, else `ClassOf(Status,
  EverWorking)`. `waiting_since` is the fold's `Since` when known (0, omitted, otherwise);
  `waiting_exact` comes from `WaitingTime`. `AgentViewInput` has no `now`: nothing in the view
  depends on it.
- **`KeyString`** (`host/server/epoch/pane_id`) is exported for the sort key's JSON and the fixture.
- **ShortenDir** ignores a trailing slash on `home`, and a `home` of `/` shortens nothing.
- **Context Line cleaning.** The Pane Title is made one line first: non-whitespace control
  characters are dropped and whitespace runs become one space, trimmed. The `claude` rule then
  drops one leading non-letter, non-digit rune followed by a space (herdr's raw terminal title
  carries a status glyph, `✳` idle or `◐` working; its stripped title, which the client maps
  first, carried a session name for all 12 live Claude Code agents on 2026-10-19).
- **Sentinel.** An unknown group or start is `SortUnknown` = 2^53−1 (JS `Number.MAX_SAFE_INTEGER`),
  so JS compares it exactly and 32-bit `int` is not involved (`SortKey.Group` is `int64`).
- **Fixture shape.** Each `input` entry is flat: key, program, profile, status, `ever_working`,
  `since` (absent: fold not known), `exact` (class began live), directory, workspace,
  `workspace_position`, tab, pane title, plus `view`, the expected AgentView JSON; the file also
  carries `home`. `TestGoldenBoard` checks every view, the order and the headers; Story 3.7's JS
  tests sort the expected views by `sort_key` and never re-derive it. 13 views; the equal-start
  tie differs in epoch (2 vs 10) so a string comparison of epochs fails.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, VG | The golden fixture holds inputs, not computed views, so JS tests would re-derive core's logic | medium | Against AD-1; patch: expected computed views in the fixture, asserted by Go |
| 2 | ECH, BH | `MaxInt`/`MaxInt64` sentinels lose precision in JS and differ on 32-bit | medium | Patch: one JS-safe sentinel, 2^53−1 |
| 3 | ECH, BH | Control characters and newlines pass into the one-line Context Line | low | Patch: collapse whitespace, drop control characters |
| 4 | BH | The `Claude Code` placeholder rule is unverified against real titles | low | Checked 2026-10-19 with a read-only snapshot of the live server: herddash maps `terminal_title_stripped`, which holds session names for all 12 Claude agents; the raw title starts with a status glyph (✳, ◐). Patch: cite it; drop a leading glyph as a fallback |
| 5 | BH | `AgentViewInput.Now` cannot affect the view | low | Patch: remove it |
| 6 | VG, ECH | Class taken from the fold rather than the current Status is untested | medium | VG pre-verified; patch |
| 7 | ECH, BH | A newly blocked Agent sorts last in Needs you, though FR-3 says it moves "to the head" | maybe-false | FR-3's main rule ("each by Waiting Time descending"), the epic AC and AD-1 all put the longest wait first; the consequence is read as "into the Needs you group at the head of the top zone". Flagged to the operator (decision 18a28ed4ef2a) |
| 8 | ECH, BH | The workspace group ignores Host and Server | false | v1 observes one herdr server per Daemon (spine Deferred), so one Daemon's views share Host and Server |
| 9 | ECH, BH | All unknown-position workspaces share one group and header | low | herdr's snapshot lists every workspace, so position 0 is not observed; rejected |
| 10 | ECH, BH | `KeyString` is ambiguous if a component contains `/` | low | Host names, session names and herdr pane ids contain no `/`; rejected |
| 11 | ECH | A known fold with `Since` ≤ 0 loses its start | low | `observed_at` is a real instant; rejected |
| 12 | ECH | A home with several trailing slashes is not matched | low | Home comes from the OS; rejected |
| 13 | BH | Pending Profiles render blank | false | Only reported Agents become views, and reporting waits for Profile resolution (AD-14) |
| 14 | BH | A typo in a rule-table key fails silently | low | Rejected |
| 15 | BH | The vfkb handoff entry is labelled human | low | The vfkb hook asks for that role on handoffs; rejected |
| 16 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
