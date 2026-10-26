---
title: 'Story 5.2: A resumed session does not keep finished background tasks'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: 'dc975c819bb99df65a8f184b3aae4f8a484639d5'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/implementation/epic-5-context.md' # on the epic branch (Story 5.5)
  - '{project-root}/docs/research/claude-code-resume-background-tasks-2026-10-23.md'
warnings: []
deferred:
  - summary: >-
      Between a crash and a resume inside herdr, whether herddash's session watch for the dead Claude Code process ends (so its open start is not shown) is untested.
    evidence: |-
      The experiment ran outside herdr by design. If herdr kept reporting the Agent with the dead pid and the stale sessions/<pid>.json stayed readable, the Box would stay BACKGROUND until a resume writes stopped. Settle it by observing a crashed Claude Code Agent in an isolated herdr.
    location: >-
      internal/engine/engine.go followSession
    severity: medium (unverified)
  - summary: >-
      What a resume writes for a background subagent left open, and whether --continue behaves like --resume, were not observed.
    evidence: |-
      Only shell commands and --resume were tested on 2.1.292. If a subagent gets a status other than stopped (or none), it stays open after a resume. Settle it by repeating the SIGKILL-and-resume experiment with a background Agent task and with --continue.
    location: >-
      internal/core/transcript.go closingStatuses
    severity: medium (unverified)
---

<intent-contract>

## Intent

**Problem:** A Claude Code session that crashed (or was killed) with a background command
running leaves the command's start in its transcript with no end. The experiment on Claude Code
2.1.292 (2026-10-23, `docs/research/claude-code-resume-background-tasks-2026-10-23.md`) found that
`--resume` does **not** copy starts into a new transcript: it appends to the same
`<sessionId>.jsonl` and, about a second after start, enqueues a `<task-notification>` for each
task with no completion record, with `<status>stopped</status>`. herddash closes a task only on
`completed`, `failed` or `killed`, so the resumed Agent's view keeps that task open and the Box
stays BACKGROUND until the start falls out of the 8 MiB window.

**Approach:** Treat `stopped` as a closing notification status. The story's conditional
"drop an open command after a bounded time without a tool shell" branch is not taken: the
evidence says starts are not copied on resume. Its "no code changes" branch is not taken either,
because the evidence shows the one-word gap above. Record the finding, amend ADR-0009's list of
closing statuses, and close the deferred entry with the evidence.

## Boundaries & Constraints

**Always:** Decode only the fields ADR-0009 names (the status word and the task id). Ids keep their
`[A-Za-z0-9_-]{1,128}` shape. `core` stays pure. Tests use synthetic text marked SECRET.

**Never:** No new tunable, no time-based ageing of tasks, and no change to the process check, the
tail bounds, the stored schema or the import graph. Never read a real transcript in tests. Never
treat an unknown status (`running`, none) as closing.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Resumed after a crash | start `bx1`, then enqueue notification `bx1` status `stopped` | 0 open | No error expected |
| Observed shape | the 2.1.292 notification, with `<tool-use-id>`, `<summary>`, `<note>` | EndID `bx1`, EndStatus `stopped` | No error expected |
| Unknown status | `running`, or no status | stays open (unchanged) | No error expected |
| Engine, new process same session | Agent `done` with 1 open command; process changes to a new pid whose record names the same session; transcript gains the `stopped` notification | view shows 0 tasks, Background false | No error expected |

</intent-contract>

## Code Map

- `internal/core/transcript.go` -- `closingStatuses` (line ~42) is the one set both `taskNotification` and `BackgroundTasks.Apply` consult; the `TranscriptLine.EndStatus` comment and `DecodeTranscriptLine`'s doc list the statuses.
- `internal/core/transcript_test.go` -- `TestTranscriptMatrix` (`notify(id, status)` helper) gets the `stopped` row; a decode test pins the observed full shape.
- `internal/engine/transcript_test.go` -- `startTranscript`, `writeTranscript`, `awaitTasks`, `trBgStart` (id `bk7q2x9`) and `trEndTurn`. A process change is driven as in `session_test.go`'s `TestSessionNewProcessStillWaiting`: `r.env.set(p2, claudeVar, cfg)`, `r.sess.recs[p2] = procenv.Session{...same SessionID/CWD}`, `r.h.fake.setProc("w1:p1", p2)`, then a `LifecycleAgentDetected` event. `engine.go` `onProfile` (line ~1736) calls `followSession(p, true)`, which drops the tail and re-reads from the bound.
- `docs/adr/0009-background-work-from-claude-code-transcripts.md` -- the Decision's "Open tasks" bullet names the closing statuses: add a dated amendment.
- `docs/implementation/deferred-work.md` -- line ~64 entry (resumed session): mark resolved with the evidence.
- `docs/research/` -- new frozen record of the experiment.

## Tasks & Acceptance

**Execution:**
- `docs/research/claude-code-resume-background-tasks-2026-10-23.md` -- ALREADY WRITTEN during planning by whoever ran the experiment (it holds evidence only they saw); read it, do not rewrite it, only fix a factual mismatch with the code -- AC 1.
- `internal/core/transcript.go` -- add `stopped` to `closingStatuses`; update the comments naming the statuses and why -- the fix.
- `internal/core/transcript_test.go` -- add matrix row "stopped on resume closes" and a test decoding the observed 2.1.292 notification shape -- covers the fix.
- `internal/engine/transcript_test.go` -- add `TestTranscriptResumedStopped`: a crashed process's open command, then a new process on the same session whose transcript gains `stopped`: the view drops to 0 and Background false -- surface-level proof.
- `docs/adr/0009-background-work-from-claude-code-transcripts.md` -- add an amendment note (2026-10-23, Story 5.2) adding `stopped` -- the decision text matches the code.
- `docs/implementation/deferred-work.md` -- mark the resumed-session entry resolved, pointing at the research record and this spec.

**Acceptance Criteria:**
- Given the experiment, when the research record is read, then it names Claude Code 2.1.292, the date 2026-10-23, and says that resume appends to the same transcript (starts not copied) and closes unfinished tasks with `stopped`.
- Given a Claude Agent whose transcript holds an open background command and then a `stopped` notification for it, when the engine reads it, then the Board's view shows no background task and the Agent is not BACKGROUND.
- Given the change, when `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` and `node --test internal/httpapi/web/*.test.mjs` run, then all pass and `gofmt` prints nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 18 findings — high 0, medium 0, low 7, false 9, maybe-false 2
- findings:
  - `[low]` `[patch]` (edge) The engine test's "1 task before stopped" check also holds if the engine never followed the new process — patched: p1's record is removed when p2's is added, so only p2's re-read can show the task or read the stopped line.
  - `[false]` `[reject]` (edge) readsOf(p2) >= 3 is not tied to the transcript read — reads are serialized by the busy flag and the transcript is read in the same off-actor read, so by the third read the first's result is applied; a stale view would also fail, not pass.
  - `[false]` `[reject]` (edge) A stopped line already in the file at follow time is untested — the engine folds the re-read's lines through the same core fold; the matrix row "stopped on resume closes" covers start and stopped in one pass.
  - `[false]` `[reject]` (blind) The AC deviation is not recorded — it is recorded in the spec's Approach and Design Notes and in deferred-work's resolution; amending a final epics.md is not this story's.
  - `[false]` `[reject]` (blind) sprint-status.yaml not updated — the orchestrator owns it for this run.
  - `[low]` `[patch]` (blind) Same as the first edge finding (re-read not proven) — patched as above.
  - `[maybe-false]` `[defer]` (blind) Crash-without-resume inside herdr, subagents and --continue untested — deferred twice (frontmatter and deferred-work.md), would be medium; settled by the experiments named there.
  - `[false]` `[reject]` (blind) Fix depends on the version and nothing says so — the ADR amendment and the resolution both name Claude Code 2.1.292; an older version without stopped behaves exactly as before.
  - `[low]` `[patch]` (blind) stopped does not mean the work ended (orphaned shell) — patched: the ADR amendment and the research record say so and why closing is deliberate.
  - `[false]` `[reject]` (blind) No row for a second close of an id — Apply ignores an end for an id not open; the matrix's "other id's notification" row covers that path.
  - `[low]` `[patch]` (blind) epic-5-context.md stale and conflicting — patched: dropped from this change; the epic branch already carries the file from Story 5.5.
  - `[low]` `[patch]` (blind) The research record does not list its sessions — patched: a per-session table with timings added, the count corrected to five sessions and three resumes.
  - `[low]` `[patch]` (blind) Magic numbers in the engine test — patched: comments on the three reads and on ClassRank 1.
  - `[false]` `[reject]` (intent) Neither literal AC branch taken — deliberate: the evidence was a third case, branch (b) would leave the Box BACKGROUND, and the story's goal decides; documented in Design Notes.
  - `[low]` `[reject]` (intent) The Box stays BACKGROUND for about a second until the resumed process writes stopped — inherent to the source and negligible; fixing it would need the timed drop the evidence argues against.
  - `[maybe-false]` `[defer]` (intent) A crashed session never resumed, or a resume that never writes stopped — same entry as the crash-without-resume deferral.
  - `[false]` `[reject]` (intent) No test at the HTTP or Board surface — the API serializes the AgentView the engine test checks, and existing httpapi tests cover that mapping.
  - `[false]` `[reject]` (intent) The -dev install is not visible — the orchestrator installs; this run is told not to install anything.

## Design Notes

Why not the AC's bounded-time drop: it was conditional on starts being copied into a new
transcript on resume. They are not. The one path that does copy history into a new session
("Move to background and exit") moves the session into Claude Code's daemon, which keeps running
the task and records its completion in the new file, and the pane's process is gone, so
nothing stale reaches a pane's Agent. A time-based drop would also wrongly end a long command
whose shell the process check cannot see (a subagent's), so it is not added speculatively.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./... && gofmt -l .` -- expected: all pass, gofmt prints nothing
- `node --test internal/httpapi/web/*.test.mjs` -- expected: all pass

## Auto Run Result

Status: done

**Summary.** The experiment (Claude Code 2.1.292, 2026-10-23) found that `--resume` copies nothing
into a new transcript. It appends to the same file and, as it starts, enqueues a `<status>stopped</status>`
notification for every task with no completion record. herddash ignored `stopped`, so a resumed
Agent stayed BACKGROUND. `stopped` now closes a task.

**Files changed.**
- `internal/core/transcript.go`: `stopped` added to `closingStatuses`; comments updated.
- `internal/core/transcript_test.go`: two matrix rows and `TestTranscriptResumeStoppedShape`, which pins the observed shape.
- `internal/engine/transcript_test.go`: `TestTranscriptResumedStopped` (new process, same session, re-read, closed by stopped); the rig gains `cfg`.
- `docs/research/claude-code-resume-background-tasks-2026-10-23.md`: the frozen record of the experiment.
- `docs/adr/0009-background-work-from-claude-code-transcripts.md`: dated amendment adding `stopped`.
- `docs/implementation/deferred-work.md`: the resumed-session entry resolved; two new unverified entries.

**Review.** 18 findings. 7 low: 6 patched, 1 rejected as inherent. 9 false, rejected with the
reasons in the triage log. 2 maybe-false, deferred. Patched by verdict: high 0, medium 0, low 6.
Follow-up review: not recommended (no high or medium patched).

**Verification.** `go build ./...` ok, `go vet ./...` ok, `go test -race ./...` all packages ok,
`gofmt -l .` empty, `node --test internal/httpapi/web/*.test.mjs` 74/74 passed. The verification-gap
reviewer removed `stopped` and saw all three new tests fail.

**Residual risks.** Untested: a crash inside herdr before any resume; a background subagent's resume
status; `--continue`. All three are deferred.
