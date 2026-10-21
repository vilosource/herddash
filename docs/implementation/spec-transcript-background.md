---
title: 'Background tasks, subagents and workflows from Claude Code transcripts (ADR-0009)'
type: 'feature'
created: '2026-10-21'
status: 'in-progress'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/adr/0009-background-work-from-claude-code-transcripts.md'
  - '{project-root}/docs/adr/0008-claude-code-session-record-as-status-authority.md'
  - '{project-root}/docs/implementation/spec-background-tasks.md'
  - '{project-root}/docs/implementation/spec-claude-session-status.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The operator: "as a user I want to know when you are Working and you have background
tasks or you are finished and have background tasks", including subagents and workflows; and,
seeing this session read WORKING while only a background subagent ran: "why is the dashboard
showing that 3:herddash is WORKING yet you are just waiting for an Agent to finish its work?"

**Approach:** Build ADR-0009 (accepted 2026-10-21): tail each `claude` Agent's session transcript
for structural markers only, keep a per-Agent set of open background task ids and whether the
main thread's turn has ended, and show `WORKING · N bg`, BACKGROUND or FINISHED accordingly.

## Boundaries & Constraints

**Always:**
- **Locating the transcript:** the session record ADR-0008 already reads gains two more decoded
  fields, `sessionId` and `cwd`; the transcript is
  `<config dir>/projects/<cwd with every "/" and "." replaced by "-">/<sessionId>.jsonl`
  (verify the slug rule against a fixture: `/Users/jasonviloria/GitHub/herddash` →
  `-Users-jasonviloria-GitHub-herddash`). Same file-type safety as the session reader
  (regular, not a symlink, never blocks).
- **Tailing (adapter, `internal/procenv` or alongside it per `arch_test.go`):** keep a byte offset
  per Agent in memory; on first sight read at most the last 8 MiB (start at the first full line);
  then only new bytes, at most 1 MiB per read, complete lines only. A file that shrank or whose
  inode changed restarts from the bound.
- **Markers, decoded by fixed field names only (core, pure):**
  - start (command): a `user` record whose `toolUseResult.backgroundTaskId` is a non-empty string;
  - start (subagent): `toolUseResult.isAsync == true`, `status == "async_launched"`, `agentId`;
  - end: a `queue-operation` record, `operation == "enqueue"`, `content` a string starting
    `<task-notification>`, from which only `<task-id>…</task-id>` and `<status>…</status>` are
    taken; `completed`, `failed`, `killed` close the id; no status leaves it open;
  - main turn ended: an `assistant` record that is not a sidechain (`isSidechain` false or
    absent) with `message.stop_reason == "end_turn"`; a later non-sidechain `user` record
    (prompt or tool result) or `attachment` of `queued_command` starts a turn again.
  Nothing else is decoded; text, inputs, outputs, prompts and results are never kept, logged,
  stored, sent or shown. Undecodable lines are skipped.
- **Per-Agent state (core):** `open` (set of ids, capped at 256) and `turnEnded` (bool). The count
  shown is `len(open)`, plus the existing process-check verdict when it finds a shell no id
  accounts for (it may only add 1, as today).
- **View (core, AD-1):** `AgentView.background_tasks` (int). Display state:
  - Status `working` and `turnEnded` and count > 0 → **Background** (rank 3), as an idle one;
  - Status `working`, count > 0, turn not ended → Working with `background_tasks` shown;
  - Status `done`/`idle` and count > 0 → Background (as built);
  - count 0 → as today. Needs you and Unknown never change.
  `background` (bool) stays, true exactly when the Box is Background. Ranks per the current order
  (Finished 1, Idle 2, Background 3, Working 4).
- **Engine:** the transcript tail runs in the same off-actor watch cadence as the session record
  (2 s), with timeout, busy flag, generation tag, cleanup on removal/program change/epoch
  change/shutdown; a lasting failure (no record, unreadable transcript, bound exceeded every read)
  falls back to the process check alone, debug-logged once.
- **Board:** a Working Box with `background_tasks > 0` reads `WORKING · N bg` in its State label;
  a Background Box reads `BACKGROUND · N` when N > 1. Accessible name says "N background tasks".
  Group counts unchanged.
- **Workflows:** not claimed until observed. Implementation must not special-case them; the
  orchestrator (Claude, not the implementer) will run one authorised workflow and check what the
  transcript records.
- **Docs:** PRD amendment (dated, ADR-0009), design doc (State label variants), spine adapter row.

**Never:**
- Decode or retain any field other than the ones listed; log any id with surrounding content;
  open any transcript but the one the session record names; read a subagent's own transcript.
- Change the record, the Attention Class, the Fold or Waiting Time.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Bg command open, idle | backgroundTaskId x, turn ended, herdr done | Background, background_tasks 1 | test fails |
| Bg command closes | notification x completed | count 0 → Finished | test fails |
| Main turn over, subagent running | async_launched a, end_turn, herdr working | Background (not Working), 1 | test fails |
| Working with bg | open x, turn not ended, herdr working | Working, background_tasks 1, label `WORKING · 1 bg` | test fails |
| Notification without status | enqueue x, no status | x stays open | test fails |
| Sidechain end_turn | isSidechain true end_turn | turn not ended | test fails |
| New prompt | user record after end_turn | turn not ended | test fails |
| Big file first sight | 50 MiB transcript | reads last 8 MiB only | test fails |
| Truncated/rotated | file shrinks | restart from bound | test fails |
| Undecodable line | garbage | skipped | test fails |
| No session record / transcript | missing | process check alone | test fails |
| Privacy | line with text, prompts | nothing but ids/status leaves the decoder | test fails |
| Slug | cwd with "." and "/" | correct project dir | test fails |

</frozen-after-approval>

## Code Map

- ADR-0009; `internal/procenv` (session reader gains sessionId/cwd; transcript tail); `internal/core` (marker decoder, per-Agent state, view fields, display state); `internal/engine` (watch); `internal/httpapi/web` (`order.mjs` labels, `board.mjs`, CSS if needed, node tests); docs.

## Tasks & Acceptance

**Execution:**
- [ ] adapter: session fields + transcript tail with bounds, tests with temp files
- [ ] core: decoder (privacy test), state, view/display rules, tests
- [ ] engine: watch, fallback, tests
- [ ] Board: labels, node tests
- [ ] docs

**Acceptance Criteria:**
- Given the module, when `go build ./... && go vet ./... && go test -race ./...`, `gofmt -l .` and `node --test internal/httpapi/web/*.test.mjs` run, then all pass and every matrix row has a test.
- Given a dev build installed locally, when this session ends its turn while a background subagent runs, then its Box reads BACKGROUND, not WORKING.

## Spec Change Log

## Review Triage Log

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- all pass
- `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs` -- all pass
- `test -z "$(gofmt -l .)"` -- exit 0
