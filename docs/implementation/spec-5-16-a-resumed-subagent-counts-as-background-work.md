---
title: 'Story 5.16: A resumed subagent counts as background work'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: 'a58b1e39e2099e7e2bce1717ee3270f0ef40c605'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0009-background-work-from-claude-code-transcripts.md'
warnings: []
deferred:
  - summary: >-
      A resume of a subagent launched before /clear (in another session id's transcript) may never close if its notification is written to the old transcript.
    evidence: |-
      Not observed: all 89 resumes on this machine name a subagent launched earlier in the same transcript. Settle it with a throwaway cldp session outside herdr: launch a background subagent, let it finish, /clear, resume it with SendMessage, and see which transcript gets its notification. Story 5.3 saw a background command's completion written to the new transcript, which suggests the same here.
    location: >-
      internal/core/transcript.go taskStart; ADR-0009 Story 5.3 and 5.16 amendments
    severity: medium (unverified)
---

<intent-contract>

## Intent

**Problem:** When Claude Code resumes a finished subagent (SendMessage), the transcript records a user record whose `toolUseResult` is `{"success":true,"message":"Resuming agent …","resumedAgentId":"<id>","pin":{…}}`. ADR-0009's decoder does not treat it as a task start, so the Box under-counts background work (operator saw 1 while 3 ran).

**Approach:** Treat a `toolUseResult` with `success` exactly `true` and a well-formed `resumedAgentId` as a subagent start for that id, so the existing notification handling closes it. Verified (2026-10-23, structure only, every transcript under `~/.claude-cldp`, `~/.claude-cldw`, `~/.claude` and `~/.claude-oneio`, Claude Code 2.1.267 to 2.1.292): every resume record is a non-sidechain `user` record with exactly the keys `success`, `message`, `resumedAgentId`, `pin`; in the operator's transcript each id equals a subagent's original `agentId`, whose launch and `completed` notification precede the resume; and when a resumed subagent finishes, a later `queue-operation` `enqueue` `<task-notification>` carries the same `<task-id>` with `<status>completed</status>` (88 resume records in 18 transcripts, all `success` true; 92 later enqueued notifications name a resumed id, all `completed`). In the operator's own transcript the two resumes had no later notification yet when read.

## Boundaries & Constraints

**Always:** decode by fixed field names only (`success`, `resumedAgentId`); id must pass `taskID`; the start is a subagent (`StartCommand` false); `TranscriptLine` field set unchanged; `internal/core` stays stdlib-only and clock-free; fixtures use placeholder text only; ADR-0009 gets a dated amendment.

**Never:** read `message` or `pin`; carry anything else from the record; change Apply, the engine read loop, tail bounds, the view or the Board; treat a resume as a command.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Resume after completion | agent start, its `completed` notification, then resume record for the same id | id open again (count 1) | none |
| Resume closes | resume, then notification `completed`/`failed`/`killed`/`stopped` for id | count 0 | none |
| Resume, no status notification | resume, notification without status | stays open | none |
| Failed resume | `success` false with id, or `success` true with no/empty id, or `success` the string "true" | nothing opens | none |
| Hostile id | `resumedAgentId` with `/`, spaces or > 128 chars | nothing opens | none |
| Resume of an open id | id already open | still counted once | none |
| Main turn over | prompt, resume, end_turn | 1 open, turn ended (Background) | none |

</intent-contract>

## Code Map

- `internal/core/transcript.go` `taskStart` (~line 262) -- add `Success json.RawMessage "success"` and `ResumedAgentID json.RawMessage "resumedAgentId"`; after the async-launch branch, return `(id, false)` when success is JSON `true` and the id passes `taskID`. `DecodeTranscriptLine` case "user" already calls `taskStart`, so a resume line also carries `TurnStarted` (it is a tool result). Update the marker list in `DecodeTranscriptLine`'s doc comment and `taskStart`'s doc.
- `internal/core/transcript_test.go` -- constants `agentStart`, `notify`, `fold`, `TestTranscriptMatrix`, `TestTranscriptPrivacy` hostile lines. Add a `resumeShape` placeholder constant shaped on 2.1.292 (message tool_result block array, `toolUseResult` with all four keys, placeholder SECRET text) and a `TestTranscriptResumedSubagent` covering the matrix.
- `internal/engine/transcript_test.go` -- `startTranscript`, `writeTranscript`, `awaitTasks`, `trDone`, `noLeak`, pattern `TestTranscriptSubagentAfterTurn`. Add an engine test: subagent launched and completed, resumed, turn ended -> Background 1; notification -> 0; log clean.
- `docs/adr/0009-background-work-from-claude-code-transcripts.md` -- dated "Amended 2026-10-23 (Story 5.16)" note under the Transcript/Open tasks decision.

## Tasks & Acceptance

**Execution:**
- `internal/core/transcript.go` -- decode resume as subagent start; docs -- the fix.
- `internal/core/transcript_test.go` -- shape + matrix test; privacy hostile lines -- pins the shape.
- `internal/engine/transcript_test.go` -- resumed-subagent engine test -- outermost surface (AgentView).
- `docs/adr/0009-background-work-from-claude-code-transcripts.md` -- amendment with evidence counts.

**Acceptance Criteria:**
- Given a Claude Agent whose transcript shows a subagent launched, completed, then resumed and the main turn ended, when the engine reads it, then the AgentView shows 1 background task and Background; after a `completed` notification for that id, 0 and not Background.
- Given a resume record with `success` not `true` or no valid `resumedAgentId`, when decoded, then no task opens.
- Given the 2.1.292 resume shape with SECRET placeholder text, when decoded, then the marker carries only the id, StartCommand false and TurnStarted, and no text leaks to markers or logs.
- Given `go build/vet/test ./...` and `gofmt -l`, then all pass and print nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 18 findings — high 0, medium 0, low 8, false 7, maybe-false 3
- findings:
  - `[maybe-false]` `[reject]` (edge) a sidechain resume record opens a task on the main Agent — 0 of 89 observed resumes is a sidechain; an async launch on a sidechain opens a task the same way (pre-existing rule), so the resume is "exactly like a launched subagent"; if true only low.
  - `[false]` `[reject]` (edge) the resumed run's notification can be enqueued before its resume record, leaving the id open forever — 87 of 89 resumes are followed by a notification for the id; the other 2 sit within the last 6 lines of their transcripts (still running). The resume record is the tool's immediate result; the run happens after it.
  - `[low]` `[reject]` (edge) a resume naming an id already open as a background command stays a command — command ids (`backgroundTaskId`) and subagent ids (`agentId`) are separate Claude Code namespaces; a collision was never seen and the fix adds a branch to `Apply`.
  - `[maybe-false]` `[reject]` (blind) sidechain resume — same as the first row.
  - `[false]` `[reject]` (blind) notification before resume — same evidence as the second row.
  - `[low]` `[patch]` (blind) evidence counts (88 resumes vs 92 notifications) did not say how many resumes closed, nor what happens to one that never does — ADR-0009's amendment now gives 89 resumes, 87 closed by a later notification, 2 still running at end of file, all launched in the same transcript, none a sidechain, and that an unclosed resume stays open as a launch does.
  - `[false]` `[reject]` (blind) the id-matching evidence covered one transcript but read as general — the amendment said "in the operator's transcript"; it is now checked across all 18 (every resumed id launched earlier in the same transcript).
  - `[maybe-false]` `[defer]` (blind) a resume across `/clear` may never close — not observed (0 of 89); deferred in frontmatter as medium (unverified) with the experiment that would settle it; the ADR amendment says it was not observed.
  - `[low]` `[reject]` (blind) resume of an id open as a command — same as the third row.
  - `[low]` `[patch]` (blind) the engine test's resume followed a prompt and lacked `tool_use_id` — the engine test now writes a main-thread SendMessage `tool_use` before the resume, and `trResume` carries `tool_use_id` as observed.
  - `[low]` `[reject]` (blind) the engine test asserts bare rank numbers — the convention of every neighbouring transcript test (`TestTranscriptSubagentAfterTurn`, `TestTranscriptCommandIdle`); changing it here alone adds inconsistency.
  - `[low]` `[reject]` (blind) the core "main turn over" row does not check Background or a later prompt — Background is the engine's derivation and is asserted by the engine test; turn reopening is generic and covered by "new prompt" in `TestTranscriptMatrix`.
  - `[false]` `[reject]` (blind) the gofmt command skips untracked files and sprint-status is not updated — `gofmt -l .` was also run and printed nothing; sprint-status is the orchestrator's by instruction.
  - `[false]` (intent) `[reject]` the Board surface is untested — the Board draws `AgentView.background_tasks` unchanged and `/api/state` serves the view verbatim; the engine test asserts the view (same ruling as Stories 5.14, 5.15).
  - `[low]` `[patch]` (intent) no test reproduces the reported three-open case — core matrix row "one running, two resumed" added: one running subagent plus two finished and resumed ones give 3 open with the turn ended.
  - `[false]` `[reject]` (intent) the close path rests on the survey, not the reported case — the survey covers 87 closed resumes, and re-reading on 2026-10-23 found the operator's own two resumes since closed by `completed` notifications naming the same ids.
  - `[low]` `[reject]` (intent) the id and `success` checks are stricter than "non-empty"/"true" — deliberate and the same `taskID` rule every launched id passes; every observed resume satisfies it.
  - `[false]` `[reject]` (intent) the spec file is an extra artifact — every story in this repository carries one (`docs/implementation/spec-<key>.md`).

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./... && gofmt -l $(git ls-files '*.go')` -- expected: success, gofmt prints nothing
- `go test -race -count=20 -run 'Transcript|Resumed' ./internal/core/ ./internal/engine/` -- expected: pass

## Auto Run Result

Status: done

**Summary.** A `toolUseResult` with `success` exactly JSON `true` and a `resumedAgentId` that passes `taskID` is now a subagent start (never a command) in `taskStart`, so a subagent resumed by SendMessage reopens on its Agent's Box and its next closing notification (completed, failed, killed, stopped) closes it, exactly as a launch. A failed or malformed resume opens nothing; `message` and `pin` are never read. Observed shape, Claude Code 2.1.292 (structure only): a non-sidechain `user` record, `message.content` a `tool_result` block array, `toolUseResult` with exactly `success`, `message`, `resumedAgentId`, `pin` (`pin` holding `id`, `name`, `ref`). Re-read at finalize across every transcript under the four config directories: 89 resumes in 18 transcripts, all `success` true, none a sidechain, every id a subagent launched earlier in the same transcript; 87 followed by an enqueued `<task-notification>` with the same `<task-id>`, all `completed`; 2 still running at end of file. The operator's own two resumes, without a notification when planning read them, have since been closed that way (the intent-contract's counts, 88/92, are the planning-time read).

**Files.**
- `internal/core/transcript.go` -- resume branch in `taskStart`; decoder and `taskStart` docs.
- `internal/core/transcript_test.go` -- `resumeShape` (2.1.292, placeholder text), `TestTranscriptResumedSubagent` matrix incl. the operator's three-open case; privacy and recaps-off parity lines.
- `internal/engine/transcript_test.go` -- `TestTranscriptResumedSubagent`: launched, completed, SendMessage, resumed, turn over -> 1 task, Background; notification -> 0, Working; rows unchanged; log clean.
- `docs/adr/0009-background-work-from-claude-code-transcripts.md` -- dated Story 5.16 amendment with the evidence.

**Review.** 18 findings: 3 low patched (ADR evidence counts and the never-closed case, engine test shape, three-open test row), 1 deferred (resume across `/clear`, maybe-false, medium if true, not observed), 14 rejected (7 false, 2 maybe-false, 5 low) with reasons in the triage log.

**Follow-up review recommended:** false. Patched: 0 high, 0 medium, 3 low.

**Verification.** `go build ./...` ok; `go vet ./...` ok; `go test -count=1 ./...` all 12 packages ok; `gofmt -l .` printed nothing; `go test -race -count=20 -run TestTranscript ./internal/core/ ./internal/engine/` ok. With the resume branch disabled, both new `TestTranscriptResumedSubagent` tests fail.

**Residual risks.** A resume across `/clear` is unobserved (deferred). The shape is Claude Code's internal format; a change to it falls back to under-counting, as before.
