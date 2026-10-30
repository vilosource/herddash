---
title: 'Story 7.1: Say when recording is off'
type: 'feature'
created: '2026-10-30'
status: 'ready-for-dev'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/adr/0016-a-recording-strip-on-the-board.md'
  - '{project-root}/docs/design/board-design.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** When `LoadServerState` fails at start, `restore` turns recording off for the run and logs one error (`internal/engine/recorder.go:37-46`), but `/api/state` and the Board look exactly like a healthy run, so a Daemon writing nothing goes unnoticed (Epic 2 retrospective F-6).

**Approach:** Per ADR-0016 (accepted by the operator 2026-10-30): the published `engine.View` gains an always-sent top-level `recording` boolean, and the Board draws a red recording strip after the connection and version strips while it is false.

## Boundaries & Constraints

**Always:** `recording` is fixed for a run and true from the very first published View only if the run records; a View published before `restore` runs must not claim recording that a failed load then withdraws (decide the value before the first View a client can read, or make `restore` publish). An absent `recording` on the Board means recording. Boxes stay shown and undimmed under the strip. The strip lives in the same `#strips` live region, after connection and version. Board text exactly as `docs/design/board-design.md`'s "Recording off" bullet.

**Never:** No change event for `recording`; no title-bar change (ADR-0013); no change to the record, the routes or the 503 `recording_off` behaviour; no new import edge; `internal/core` untouched unless needed for a type.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Healthy run | Recorder loads (found or not found) | every View `recording: true`; no recording strip | N/A |
| Load fails | `LoadServerState` errors | every View a client sees `recording: false`; strip shown; Boxes live | existing ERROR log kept |
| No Recorder (tests) | `Options.Store` nil | `recording: false` | N/A |
| Older Daemon | `state` without `recording` | no strip | N/A |
| Combined | disconnected + recording off | connection strip, then recording strip; dimming from disconnected only | N/A |

</frozen-after-approval>

## Code Map

- `internal/engine/view.go:17-29` -- `View` struct; add `Recording bool json:"recording"`. `publish` (`:223-229`) builds every later View.
- `internal/engine/engine.go:704,735,762` -- `New` stores the first View before `Run` calls `restore()`; ordering fix needed here.
- `internal/engine/recorder.go:37-46` -- `restore`, the only place `e.store` becomes nil.
- `internal/engine/recorder_test.go:355-389` -- `TestRecordLoadFails` (fake `st.loadErr`, `fake_test.go:953,1170`); assert the View here, plus a healthy-run assertion.
- `internal/httpapi/httpapi_test.go:113-135` -- `TestState` required-field list (`:133`); add `recording`. `events_test.go:252` adapts by itself.
- `internal/httpapi/web/order.mjs:522-532` -- `applyEvent` `state` keeps only agents/connection/runId; keep `recording` too. `boardStatus` (`:752-826`) returns `{strips, dimmed, hideBoxes}`; strip `{kind, parts}`; add a recording input defaulting to on so existing `deepEqual` tests (`order.test.mjs:1034-1293`) hold. `stripsKey` `:831`.
- `internal/httpapi/web/board.mjs:407-445,461` -- renders `<p class="strip strip--{kind}">`; passes model to `boardStatus`.
- `internal/httpapi/web/board.css:55-57,73-75,141-152` -- strip kinds; reuse the refused colour tokens for a `strip--recording` kind (both themes).
- `internal/httpapi/web/check/board-browser.mjs:258,986-1262` -- strip checks; add a recording-off case if the harness can serve one, else rely on `order.test.mjs`.
- `internal/httpapi/engine_contract_test.go:190-197`, `testdata/replay-board.mjs:36` -- compare agents+connection only; add `recording` to both so the contract proves the Board keeps it.
- `docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:430` -- JSON table: list `recording` as always sent.

## Tasks & Acceptance

**Execution:**
- [ ] `internal/engine/view.go`, `engine.go`, `recorder.go` -- add `Recording`, set from whether the run records, ensure no client-visible View says true before a failed load -- AD-3, ADR-0016
- [ ] `internal/engine/recorder_test.go` -- assert `Recording` false after load failure (first View read after `Run` starts) and true on a healthy run -- matrix rows 1-3
- [ ] `internal/httpapi/httpapi_test.go`, `engine_contract_test.go`, `testdata/replay-board.mjs` -- field present and replayed -- spine contract
- [ ] `internal/httpapi/web/order.mjs`, `order.test.mjs` -- keep `recording` from `state`; `boardStatus` adds the strip after connection/version when it is exactly false; tests for matrix rows 2, 4, 5 -- ADR-0016
- [ ] `internal/httpapi/web/board.mjs`, `board.css` -- pass it in, style `strip--recording` -- design doc
- [ ] `internal/httpapi/web/check/board-browser.mjs` -- recording strip absent on the healthy fixture (existing no-strip checks suffice) -- regression
- [ ] `ARCHITECTURE-SPINE.md`, `docs/how-herddash-works.md` (`:129-131`), `docs/implementation/deferred-work.md` (F-6 `resolved:` line), `CLAUDE.md` (repository state: the field and strip) -- documents match code

**Acceptance Criteria:**
- Given a running engine whose saved state failed to load, when `/api/state` is read at any time, then `recording` is false.
- Given F-6 in `deferred-work.md`, when the story is done, then it has a `resolved:` line naming Story 7.1 and ADR-0016.

## Implementation Notes

No compiled `epic-7-context.md`: Epic 7 was written this session (two stories, `docs/planning/epics.md`), and ADR-0016 plus this Code Map carry all the context it would hold.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race ./... && test -z "$(gofmt -l .)"` -- all pass, gofmt prints nothing
- `node --test internal/httpapi/web/` -- pass
- `go build -o /tmp/herddash ./cmd/herddash && node internal/httpapi/web/check/board-browser.mjs /tmp/herddash` -- pass
- `go test -race -count=50 ./internal/engine -run 'TestRecordLoadFails'` -- pass (new concurrency-adjacent assertion)
