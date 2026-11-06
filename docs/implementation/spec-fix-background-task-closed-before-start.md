---
title: 'A background command whose completion is written before its start does not stay open'
type: 'bugfix'
created: '2026-11-06'
status: 'done'
route: 'oneshot'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Claude Code can write a background command's `<task-notification>` to the transcript before the `toolUseResult.backgroundTaskId` record that starts it. `core.BackgroundTasks.Apply` ignores the close, because nothing is open, then opens on the start, and nothing closes it again, so the Board shows Background for work that has ended. The operator saw this on agent-core (task `bzk3pwk70`, lines 446 and 449 of its transcript); 8 of 708 background commands across the 788 local transcripts do it.

**Approach:** Remember closes for ids that are not open, bounded, and when a background command's start names a remembered id, consume it instead of opening the task. The operator chose a `fix/` branch off `develop`, separate from Story 7.5. Only a background command's start is cancelled: a resumed subagent reuses its id, and must still open after an earlier close. Tests cover this record order, one read and across two, and a Daemon restart.

</frozen-after-approval>

## Implementation Notes

- `core.BackgroundTasks` gains `earlyCloses`, up to 256 ids closed while not open, in memory only, copied by `Clone`; a background command's start naming one opens nothing. `internal/core/transcript.go`, with tests there and in `internal/engine/transcript_test.go`. ADR-0009 amended.
- **Departure from the Intent, from review:** the Intent says the start consumes the remembered close. It keeps it instead: a command's id is not used again, so consuming could only let a start written twice reopen a finished command, and it needed a mutating helper inside `Apply`'s condition. The start still opens nothing.
- Surprise while planning: the rule as first proposed, for every start, would have cancelled 5 resumed subagents in real transcripts, so it is limited to background commands. Two limits remain, both in `deferred-work.md` and ADR-0009.
- Verification: a temporary test, not committed, replayed all 788 local transcripts through the real decoder with and without the rule. Exactly 8 changed, each by one fewer open task, including agent-core's `7517737e` from 1 to 0. With the rule disabled, `TestTranscriptClosedBeforeStart` fails in `core` and in both `engine` subtests. `go test ./internal/engine/ -run TestTranscriptClosedBeforeStart -race -count=50` passes.
- The engine's restart coverage is a fresh Engine reading the transcript: open tasks are in memory only and never in the record, so nothing else survives a restart. That test uses a new temporary directory rather than stopping a rig and starting another on the same file.

## Review Triage Log

- The "two reads" test usually did not split the reads: medium, patched. A subagent start in the first batch, awaited, proves that batch was read.
- The "restart" test restarted nothing: medium, patched. It is now named a fresh Engine, one read, and its comment says why that stands for a restart.
- The 8 MiB bound can hide a close: medium, deferred and recorded in ADR-0009. The code path is as described; no real transcript hits it.
- A launch is excluded for a reason only a resume has: low, patched in the ADR. A line does not tell the two apart, and no launch inversion was seen.
- A subagent closed before its start stays open, unrecorded: medium, deferred and recorded in ADR-0009.
- Consuming the close contradicts the rationale, and a duplicated start reopens: medium, patched. The close is kept and the tests changed.
- The ADR evidence is thin: low, patched. Mechanism marked inferred; version, transcript count and the 165 peak added.
- The evidence omits that all closes were `completed`, and has unclear wording: low, patched in the ADR.
- The Story 5.3 sentence and `Apply`'s comment are stale: low, patched. CLAUDE.md's Background bullet: low, rejected; it lists behaviour at a summary level and ADR-0009 holds this rule.
- `closedEarly` mutates inside a condition: low, patched by removing it. Subagent closes occupy slots unasserted: low, rejected; the cap test bounds them.
- A tail restart or `/clear` dropping remembered closes is untested: low, rejected; `restarted` resets the whole value to its zero, and a close and start split across `/clear` was not observed.
- The spec is unfinished: false for status, notes and triage log, which this step writes; the oneshot route deletes the other sections by design. A real-shape fixture: low, rejected; the engine test uses the same marker shapes as its neighbours.
