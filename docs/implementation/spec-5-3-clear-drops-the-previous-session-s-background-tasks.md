---
title: "Story 5.3: /clear drops the previous session's background tasks"
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: '510741540cf7c32926598bf34b7f75c51d650e4c'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/implementation/epic-5-context.md'
  - '{project-root}/docs/research/claude-code-clear-background-tasks-2026-10-23.md'
warnings: []
deferred:
  - summary: >-
      Whether a background subagent started before /clear keeps running after it, and so leaves the Box while still working, is not known.
    evidence: |-
      The experiment used shell commands only. A subagent has no tool shell, so after the transcript restarts nothing counts it. Settle it by repeating the /clear experiment with a background Agent task. Recorded in deferred-work.md.
    location: >-
      internal/engine/engine.go readTranscript; internal/core/view.go backgroundOf
    severity: medium (unverified)
---

<intent-contract>

## Intent

**Problem:** It was not known whether `/clear` changes `sessionId` in `sessions/<pid>.json`. If it
did not, herddash would keep tailing the old transcript and an open start there would stay on the
Box. The experiment on Claude Code 2.1.292 (2026-10-23, `docs/research/claude-code-clear-background-tasks-2026-10-23.md`)
found that it does. The same process's record names a new session id at once, and the new
transcript begins with the `/clear` command record. A background command started before `/clear`
**keeps running** as a child of the same process, and its completion notification is written to
the **new** transcript.

**Approach:** When the record's session id changes, the transcript path changes. `procenv.TailTranscript`
then restarts from the bound and `readTranscript` starts open tasks afresh, so the reset already
exists. The story proves it at the engine with a test, says so in the code, records the
finding, amends ADR-0009 and closes the deferred entry. A command still running across `/clear`
is then counted only by the process check (a tool-shell child of the process) and goes when it
ends.

## Boundaries & Constraints

**Always:** Decode only ADR-0009's fields. Tests use synthetic text marked SECRET and never a real
transcript. `core` stays pure. Keep the diff away from `internal/core/transcript.go`'s
local-command/recap handling (Story 5.14 is changing it concurrently).

**Never:** No new state on `pane` and no session-id tracking. Change nothing in the process check,
the tail bounds, the stored schema or the import graph. Never carry tasks from one session id's
transcript into another's.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| `/clear` | Agent `done`, old transcript holds open `bk7q2x9` (1 task, Background); record's SessionID changes to a new id whose transcript holds the `/clear` command record | 0 tasks, Background false | No error expected |
| Old task ends after `/clear` | the new transcript gains `completed` for `bk7q2x9` | still 0 (an end for an id not open is ignored) | No error expected |
| Old transcript grows after `/clear` | a new start appended to the old file | not read: still 0 | No error expected |
| Recap before `/clear` | recaps on, old transcript ends in a recap | the view's recap is "" after the switch | No error expected |
| New transcript not written yet | record names an id with no file | transcript dropped (existing `trErr` path), 0 tasks | logged once at debug, by kind |

</intent-contract>

## Code Map

- `internal/engine/engine.go` `readTranscript` (~line 2085) -- builds the path from `s.SessionID`; `restarted` from the tail resets `tasks`. Only its doc comment changes: name `/clear` as the case where another session id gives another path.
- `internal/procenv/transcript.go` `TailTranscript` -- restarts on `t.path != path`; already covered by `TestTailTranscriptRestart` ("other path"). Read-only.
- `internal/engine/transcript_test.go` -- rig `startTranscript` (`r.sess.recs[p1PID]` under `r.sess.mu`, `r.cfg`, `r.path`), `writeTranscript`, `awaitTasks`, `awaitRecap`, `recapsOn`, `trRecap`, `trDone`, `trSubStart`, `noLeak`, `readsOf`. Pattern for waiting on reads is in `TestTranscriptResumedStopped`.
- `docs/adr/0009-background-work-from-claude-code-transcripts.md` -- dated amendments sit under the "Open tasks" bullet (~line 70); add one for `/clear`.
- `docs/implementation/deferred-work.md` line ~72 -- the `/clear` entry; mark resolved.

## Tasks & Acceptance

**Execution:**
- `docs/research/claude-code-clear-background-tasks-2026-10-23.md` -- ALREADY WRITTEN during planning by the experimenter; do not rewrite, only fix a factual mismatch with the code -- AC 1.
- `internal/engine/transcript_test.go` -- add `TestTranscriptCleared` covering the matrix's first three rows (a `/clear` command record shaped `{"type":"user","message":{"content":"<command-name>/clear</command-name>..."}}`, text marked SECRET; the new session id added to the leak check) , `TestTranscriptClearedRecap` for the recap row and `TestTranscriptClearedNotWritten` for the last row -- surface proof at the view.
- `internal/engine/engine.go` -- `readTranscript` comment only -- the reset is explicit for a reader.
- `docs/adr/0009-background-work-from-claude-code-transcripts.md` -- amendment (2026-10-23, Story 5.3) -- decision text matches observed behaviour.
- `docs/implementation/deferred-work.md` -- resolve the `/clear` entry with the evidence and this spec.

**Acceptance Criteria:**
- Given the experiment, when the research record is read, then it names Claude Code 2.1.292 and 2026-10-23, and says that `/clear` changes the record's `sessionId` in the same process, that the background command survives, and that its completion goes to the new transcript.
- Given a Claude Agent whose record's session id changes, when the engine reads it, then the Board's view drops the old transcript's tasks and recap and does not read the old file again.
- Given the change, when `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race -count=20 -run 'TestTranscriptCleared' ./internal/engine` and `gofmt -l .` run, then all pass and gofmt prints nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 22 findings — high 0, medium 0, low 10, false 9, maybe-false 3
- findings:
  - `[low]` `[patch]` (blind) The old task's completion check ran at count 0 and could not fail — patched: a start in the new transcript first (count 1), then the old id's completion, still 1.
  - `[maybe-false]` `[defer]` (blind) A background subagent across /clear is untested and would vanish while running — would be medium; deferred (frontmatter and deferred-work.md), ADR amendment now says it was not observed.
  - `[false]` `[reject]` (blind) The process-check claim depends on herdr's Status after /clear — the amendment states the condition (Status done or idle) explicitly; herdr's Status handling is unchanged and pre-existing.
  - `[low]` `[patch]` (blind) Faked record status "idle" where "shell" was observed — patched: "shell" in clear() and TestTranscriptClearedNotWritten.
  - `[low]` `[patch]` (blind) The recap test never checks the realistic transcript starting with the /clear record, and its comment leaned on decoder internals — patched: two cases, comment no longer describes decoding.
  - `[low]` `[reject]` (blind) ADR does not say the /clear record starts a turn — decoder behaviour Story 5.14 is changing; documenting it here would go stale, and it changes no count.
  - `[low]` `[patch]` (blind) TestTranscriptClearedNotWritten never shows the new file picked up — patched: writes it, awaits 1 task, the debug entry still once.
  - `[low]` `[reject]` (blind) Resolution rests on one Claude Code version — the record, ADR amendment and resolution all name 2.1.292; guarding against a future change would need machinery for an unseen case.
  - `[false]` `[reject]` (blind) The research record prints session-id prefixes — they are ids of throwaway experiment sessions, not operator data; the leak check concerns the Daemon's log.
  - `[low]` `[reject]` (blind) ClassRank 1 magic number — same commented pattern as Story 5.2's test; deriving it adds plumbing for no everyday harm.
  - `[low]` `[patch]` (blind) The research record cites Story 5.2's method without a link — patched: linked.
  - `[low]` `[patch]` (edge) Same as the first blind finding (completion check cannot fail) — patched as above.
  - `[low]` `[reject]` (edge) With a new-session command open, a surviving pre-/clear shell adds nothing, so the count is one low — the Box is BACKGROUND either way; counting shells per id is new process-check machinery, pre-existing design.
  - `[maybe-false]` `[defer]` (edge) Subagent across /clear — same entry as the blind subagent finding.
  - `[low]` `[patch]` (edge) No proof the new file is picked up after the not-written window — patched as above.
  - `[low]` `[patch]` (edge) ADR said "only the process check counts it", but it adds one only when no open command accounts for a tool shell — patched: ADR and readTranscript comment reworded.
  - `[false]` `[reject]` (intent) A pre-/clear command still running shows on the Box — deliberate (Design Notes): it is real running work, counted by the process check, gone when it ends.
  - `[false]` `[reject]` (intent) Tests stop at the AgentView, not the Board — the API and Board serialize that view unchanged; existing httpapi tests cover the mapping.
  - `[false]` `[reject]` (intent) The reset is implicit, not an explicit session-id check — the outcome is tested at the engine, and the verification-gap reviewer saw the tests fail with the reset removed; procenv's TestTailTranscriptRestart pins the path rule.
  - `[maybe-false]` `[defer]` (intent) Subagents not observed — same entry as above.
  - `[false]` `[reject]` (intent) Fixture status differs from observation — duplicate of the blind finding, patched.
  - `[false]` `[reject]` (intent) Evidence gathered outside herdr — herdr plays no part in writing the session record or transcript; the engine path from herdr to the record is covered by existing tests.

## Design Notes

Why the surviving command is not kept on the Box through the transcript: its start is in a
file the record no longer names. Carrying it across would mean tracking session ids per pane and
reading two transcripts, which the story's goal ("only the new session's work") argues against.
While it runs it is still a tool-shell child of the Claude Code process, so the process check
counts it, and the Box reads BACKGROUND for real work. When it ends the shell goes and so does
the count. Its `completed` line in the new transcript names an id that is not open, and
`BackgroundTasks.Apply` ignores it.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./... && gofmt -l .` -- expected: all pass, gofmt prints nothing
- `go test -race -count=20 -run 'TestTranscriptCleared' ./internal/engine` -- expected: pass

## Auto Run Result

Status: done

**Summary.** The experiment (Claude Code 2.1.292, 2026-10-23) found that `/clear` changes the same
process's record to a new `sessionId` at once, starting a new transcript. A background command
keeps running and its completion goes to the new transcript. The new id names another path, so the
engine's tail already restarts there and open tasks and the recap start afresh. No production logic
changed; three engine tests prove it.

**Files changed.**
- `docs/research/claude-code-clear-background-tasks-2026-10-23.md`: frozen record of the experiment.
- `internal/engine/transcript_test.go`: `TestTranscriptCleared`, `TestTranscriptClearedRecap` (two cases), `TestTranscriptClearedNotWritten`; the leak check includes the new session id.
- `internal/engine/engine.go`: `readTranscript` doc comment only.
- `docs/adr/0009-background-work-from-claude-code-transcripts.md`: dated amendment for `/clear`.
- `docs/implementation/deferred-work.md`: the `/clear` entry resolved; one new unverified entry (subagent across `/clear`).

**Review.** 22 findings: 10 low (7 patched, 3 rejected as not worth the added machinery), 9 false
(rejected), 3 maybe-false (one deferred entry, medium unverified). Patched by verdict: high 0,
medium 0, low 7. Follow-up review: not recommended.

**Verification.** `go build ./...` ok, `go vet ./...` ok, `go test ./...` all packages ok,
`go test -race -count=20 -run TestTranscriptCleared ./internal/engine` 60/60 passed, `gofmt -l .`
empty, `node --test internal/httpapi/web/*.test.mjs` 75 passed. The verification-gap reviewer
disabled the reset and saw the new tests fail.

**Residual risks.** A background subagent across `/clear` was not observed (deferred). Behaviour
depends on Claude Code keeping the per-session transcript naming seen on 2.1.292.
