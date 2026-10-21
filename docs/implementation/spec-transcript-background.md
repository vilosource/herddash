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
- [x] adapter: session fields + transcript tail with bounds, tests with temp files
- [x] core: decoder (privacy test), state, view/display rules, tests
- [x] engine: watch, fallback, tests
- [x] Board: labels, node tests
- [x] docs

**Acceptance Criteria:**
- Given the module, when `go build ./... && go vet ./... && go test -race ./...`, `gofmt -l .` and `node --test internal/httpapi/web/*.test.mjs` run, then all pass and every matrix row has a test.
- Given a dev build installed locally, when this session ends its turn while a background subagent runs, then its Box reads BACKGROUND, not WORKING.

## Implementation Notes

- **Adapter (`internal/procenv`, no new package, `arch_test.go` unchanged).** `Session` gains
  `SessionID` and `CWD`, decoded only when they are JSON strings (absent or another type is "",
  and the record's status still counts). `ClaudeTranscriptPath(configDir, cwd, sessionID)` builds
  `<configDir>/projects/<slug>/<sessionId>.jsonl`, slug = cwd with every "/" and "." as "-"
  (`/Users/jasonviloria/GitHub/herddash` gives `-Users-jasonviloria-GitHub-herddash`), and refuses
  a relative config dir or cwd and a session id that is not 1 to 128 of `[A-Za-z0-9_-]`, so the
  path cannot leave `projects/`. `TailTranscript(tail, path, line)` is value-in, value-out: the
  `Tail` (path, file identity, offset, skip flag) lives on the engine's pane. First sight, another
  path, a shrink or another inode restart from the bound (last 8 MiB, from the byte before it so a
  line starting exactly at the bound is kept) and report `restarted`; later reads take at most
  1 MiB, complete lines only. The session reader's file-type safety is now `openRegular` (Lstat,
  `O_NOFOLLOW|O_NONBLOCK`, re-Stat and `SameFile`), shared by both readers. Errors wrap
  `ErrTranscript` and never name the path (it carries the cwd).
- **Core.** `DecodeTranscriptLine` decodes only `type`, `isSidechain`, `toolUseResult`
  (`backgroundTaskId`, `isAsync`, `status`, `agentId`), `message.stop_reason`, `operation`,
  `content` (only `<task-id>` and `<status>` out of a string starting `<task-notification>`) and
  `attachment.type`, and returns a `TranscriptLine` of two ids, a closing status and three bools.
  Ids must be 1 to 128 of `[A-Za-z0-9_-]`, and a status other than completed/failed/killed is
  dropped, so no text can travel in an id or a status. `BackgroundTasks` keeps the open ids
  (insertion order, capped at `MaxOpenTasks` = 256, the oldest dropped) with whether each is a
  command, and `turnEnded`. `AgentViewInput.Transcript` and `AgentView.BackgroundTasks`
  (`background_tasks`); `backgroundOf` applies the display rules. The golden fixture gained
  `tasks`/`turn_ended` inputs (w3:p3 now Working with 1 bg; new w3:p5 working with its turn over
  and two subagents open, Background) and `background_tasks` on every view, regenerated from
  `NewAgentView`'s own output.
- **Engine.** No new watch: the session watch's read (`readSession`, same goroutine, 1 s timeout,
  busy flag, generation tag) tails the transcript after a successful record read when it names a
  session and cwd. The pane's `Tail` and a clone of its `BackgroundTasks` go into the read; only
  decoded `TranscriptLine`s are collected, folded after the read (a restart starts afresh), and
  returned in the `sessionMsg`. `onTranscript` keeps them; a transcript that cannot be named or
  read drops them (debug-logged once per pane, `claude transcript not read`, by kind only); a
  session read failure drops them only when the record's verdict ends (the ADR-0008 rule), so a
  passing failure keeps them. `followSession` (report, process change, program change) and
  `dropSession` (removal, epoch change, release) drop them; shutdown ends the watch's context.
  `Options.Transcript` (default `procenv.TailTranscript`) is injectable like `Session`; tests use
  temp dirs. Changes log `claude background tasks` with the count and `turn_ended` only.
- **Board.** `order.mjs`: `backgroundTasks`, `stateCount`, `stateLabel`, `tasksLabel`;
  `board.mjs` appends the count as a `.count` span inside `.class`, which CSS keeps lower case so
  the label reads `WORKING · 2 bg`; `boxLabel` adds "N background tasks" (singular for 1).
  `groupCounts` unchanged.
- **Docs.** PRD FR-2 amendment (ADR-0009) with its own consequences, the design doc's State label
  and Background paragraphs, the spine's procenv adapter row, `procenv/doc.go`.
- **Judgment calls.**
  1. "Plus the process check when it finds a shell no id accounts for": the shell is taken as
     accounted for when any open task is a background *command*; a shell beside only subagents
     adds 1 (the ADR's subagent-own-shell case). The process table cannot be matched to ids
     without reading arguments more closely than the background rule does.
  2. A line longer than one 1 MiB read is skipped (counted as undecodable) rather than failing
     every read, so "bound exceeded every read" cannot happen; the fallback covers a transcript
     that cannot be named, opened or read.
  3. A new process for the same Agent (and every `followSession`) re-reads the transcript from
     the bound, so tasks a previous process left unclosed may reappear if their start is in the
     last 8 MiB. Known gap; leaked ids (a start with no closing notification) stay open until they
     fall out of a restart's window.
  4. Only the slug rule the spec states is applied; whether Claude Code also replaces other
     characters (for example "_" or a space) was not checked against real directories (not
     allowed here). A wrong slug is a missing transcript, so the process check alone.
  5. Needs you and Unknown views carry `background_tasks: 0`.
- **Not done here.** The acceptance criterion's live check (a dev build showing BACKGROUND while
  this session waits on a background subagent) and the authorised workflow observation are the
  orchestrator's. The hand-run headless-Chrome check (`web/check/board-browser.mjs`) was not run.

## Spec Change Log

## Review Triage Log

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- all pass
- `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs` -- all pass
- `test -z "$(gofmt -l .)"` -- exit 0
