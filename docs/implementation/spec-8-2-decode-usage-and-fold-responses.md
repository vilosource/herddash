---
title: 'Story 8.2: Decode usage and fold Responses'
type: 'feature'
created: '2026-11-11'
status: 'done'
baseline_commit: '0744a1a83b978bbfd07a0093c349275d963f09b0'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/adr/0017-token-usage-per-agent-and-per-profile.md'
  - '{project-root}/docs/implementation/epic-8-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** herddash cannot read token usage: `core.DecodeTranscriptLine` takes no usage, and summing transcript records double-counts, because one Response is several records (ADR-0017).

**Approach:** The decoder gains, behind a new `usage` argument, a per-record usage marker carrying only ids, model, stop flag, five counts, timestamp and (sidechain) `agentId`. A new pure `core.UsageLedger` folds one file's markers in order into complete Responses. Nothing outside `core` changes behaviour: the engine passes `usage=false` until Story 8.4.

## Boundaries & Constraints

**Always:** `core` stays standard-library only and reads no clock (`time.Parse` is allowed). A Response is the records sharing `message.id` and `requestId`. It completes at its first record with a non-empty string `stop_reason` (taking that record's values and timestamp), or, with none, when a record of another key follows (taking its record with the largest `output_tokens`, the later on a tie, and that record's timestamp). An all-zero Response is returned. Integers are strict: a JSON integer ≥ 0, never a float, string or bool.

**Never:** keep or expose message text, `content`, or any field beyond those named; log from `core`; count a `<synthetic>` record, or an assistant record whose `message.usage` is absent or not an object (neither is a marker nor a problem); change `BackgroundTasks.Apply`'s behaviour; change the engine beyond passing `false`.

## I/O & Edge-Case Matrix

| Scenario | Input | Output |
|---|---|---|
| Multi-record | 3 records, one key, stop on the last | one Response at the last record's values |
| Stop before last | stop on record 2 of 3, record 3 equal | one Response, from record 2; record 3 ignored |
| Superseded | key A (no stop, outputs 5 then 9), then key B | A returned at output 9 when B's first record arrives |
| End of file | last key has no stop | nothing returned; `Pending()` true |
| No key | assistant usage record lacking `requestId` or `message.id` | `UsageIssue` = `UsageNoKey`; no marker |
| Unknown shape | usage object with a required count missing/non-integer/negative, `cache_creation` absent/not an object, split ≠ `cache_creation_input_tokens`, empty model, or unparseable `timestamp` | `UsageIssue` = `UsageUnknownShape`; no marker |
| Usage off | any line, `usage=false` | no marker, no issue; output equal to today's |

</frozen-after-approval>

## Code Map

- `internal/core/transcript.go` -- `TranscriptLine` (must stay comparable: `l != TranscriptLine{}` decides `ok`), `transcriptRecord`, `DecodeTranscriptLine(line, recaps)` and its `case "assistant"`; helpers `isObject`, `jsonString`, `stopReason` to reuse.
- `internal/core/transcript_test.go`, `transcript_local_test.go` -- ~25 call sites to update mechanically to `(…, recaps, false)`; their expectations must not change.
- `internal/engine/engine.go:2562` -- the only production caller; pass `false`.
- `arch_test.go` -- `clockFuncs`; must keep passing.
- Measured 2026-11-11 over 3,423 local transcripts, 176,788 assistant records: 133 synthetic; every other record has both ids, a timestamp, a model, the four counts as non-negative integers, a `cache_creation` object whose 5m + 1h equals `cache_creation_input_tokens`; every sidechain record has a top-level `agentId`.

## Tasks & Acceptance

**Execution:**
- [x] `internal/core/usage.go` -- new: `Usage{Input, Output, CacheWrite5m, CacheWrite1h, CacheRead int64}`; `UsageMarker{MessageID, RequestID, Model, AgentID string; Stopped bool; At int64 /* UTC ms */; Usage}`; `UsageIssue` (`UsageNone`, `UsageNoKey`, `UsageUnknownShape`); `Response` (the completed form: ids, model, `SubagentID`, `At`, `Stopped`, `Usage`); `UsageLedger` with `Add(UsageMarker) []Response` and `Pending() bool`, holding at most the current pending Response and the last completed key (later records of it are ignored) -- the counting rule, pure.
- [x] `internal/core/transcript.go` -- `DecodeTranscriptLine(line []byte, recaps, usage bool)`; `TranscriptLine` gains `Usage UsageMarker` and `UsageIssue UsageIssue`; decode only `requestId`, `timestamp`, `agentId` (sidechain only) and, inside `message`, `id`, `model`, `stop_reason`, `usage`; doc comment extended -- the decoder.
- [x] `internal/engine/engine.go` -- pass `false` -- no behaviour change.
- [x] `internal/core/usage_test.go` -- table tests for every matrix row plus: a sidechain marker carries `agentId`, a main one none; usage markers do not change `BackgroundTasks.Apply`; an end-turn assistant line yields both `TurnEnded` and a marker -- the rule's proof.
- [x] `internal/core/usage_golden_test.go` -- `//go:build golden`: replays every `*.jsonl` under `$HOME/.claude*/projects` (or `HERDDASH_GOLDEN_DIRS`) through decoder and ledger, first row per key across files kept, and compares with an independent count in the same file (last record per key across files, no ledger). Prints counts and totals only. Passes when the two differ by exactly the Responses pending at end of file -- the golden replay.

**Acceptance Criteria:**
- Given the golden replay on the operator's Mac, when run with `go test -tags golden -run TestGoldenUsage ./internal/core -v`, then it passes and its numbers are recorded in Implementation Notes.
- Given `go build ./... && go vet ./... && go test -race ./... && gofmt -l .`, when run, then all pass and gofmt prints nothing.

## Implementation Notes

**Golden replay, 2026-11-11, operator's Mac (`$HOME/.claude*/projects`): passes.**

- 3,431 files, 666,897 lines, 176,972 usage markers; no key 0, unknown shape 0.
- Ledger: 87,170 Responses (86,527 stopped, 643 superseded). Independent count: 87,202 keys.
  Pending at end of file: 32, completed in no file; independent minus those equals the ledger
  exactly: input 304,696, output 79,081,189, cache write 526,859,605 (5m 331,545,854 + 1h
  195,313,751), cache read 16,277,872,973.
- 19 keys have records in more than one file. One is a forked subagent's file (first record type
  `fork-context-ref`) starting with a sidechain copy of its parent's in-flight assistant record:
  same ids and timestamp, output 16, no stop reason; the main file holds the final record, output
  1,088, stopped. The first run failed on it because the independent count kept the last record
  per key; it now keeps the largest `output_tokens` per key (output never decreases within a
  Response), which is still independent of the ledger, and the ledger, which keeps the first file
  read (the main file sorts first), was right.
- **Consequence for Story 8.4, not this story:** with ADR-0017's insert-once rule, a fork copy
  read before the main file would record that Response at its partial counts under the subagent.
  Story 8.4's acceptance criteria now require the main file's final counts in that case (the
  `fork-context-ref` first record marks such files). Measured: 31 of 1,718 subagent files start
  with a Response that never stops, only one of them a fork copy, so skipping a subagent file's
  first incomplete Response would lose about 30 real ones.
- Files: `internal/core/usage.go` (new), `usage_test.go` (new), `usage_golden_test.go` (new,
  `//go:build golden`), `transcript.go`, `transcript_test.go`, `transcript_local_test.go` (call
  sites pass `false`; `TestTranscriptPrivacy`'s field list gains `Usage` and `UsageIssue`),
  `internal/engine/engine.go` (passes `false`).

## Spec Change Log

## Review Triage Log

Blind Hunter, Edge Case Hunter and Verification Gap, one pass:

- Golden replay never compiled in CI (`//go:build golden`) -- medium, real (verification gap): patched, `go vet -tags golden ./internal/core` in CI's first job.
- Fork copy not handled in 8.2; the 8.4 criterion reads as if storage alone solves it -- medium, real, routed: Story 8.4's criterion now says it is tested with the fork's file read first and may extend `core`. The live tail and history pass have no file order, so 8.4 owns it.
- Golden replay passes the fork case only by path sort order -- low, real: same 8.4 criterion; the replay is not evidence for it, as the criterion now says.
- `TranscriptLine` doc lost its "and" -- low, real: patched.
- Doc claims requestId/timestamp/agentId are not looked at with usage off, yet they are copied raw on every line -- low, real: patched wording (examined only with usage on).
- `Add` accepts a zero marker -- low, real only on misuse: patched the doc's precondition; no guard (the decoder never returns a marker with an empty id).
- The engine's hard-coded `false` has no pointer -- low, real: patched comment.
- WalkDir errors ignored; `unreadable` mixes open failures and part-way read failures -- low, real: patched, counted separately and failing the replay.
- Epic context says "largest values" and "last record per key" -- low, real: patched `epic-8-context.md`.
- Ledger remembers only the last completed key, so a late record re-emits -- false: records of two Responses never interleave within a file (measured over 87,170 Responses), and the store keeps the first row per key (ADR-0017).
- Sidechain records interleaved in a main file -- false: subagent records are only in subagent files (research record, finding 3).
- Sidechain record without `agentId` attributed to the main thread -- low; rejected: 0 of 176,972 records, and the fix adds a branch.
- Missing `cache_creation` split drops the whole Response -- low; rejected: every record has it (measured); such a record is reported as `UsageUnknownShape`, counted by 8.4, never silent, which is the frozen rule.
- Ids and model unbounded -- low; rejected: never observed, values come from Claude Code's own records, fix adds a guard.
- Spec missing from the diff -- false: the spec goes to the edge-case layer alone, by the workflow.
- Buffering every usage line once usage is on -- low; rejected here: usage is off in this story; 8.4 measures its reads.
- Spec says "last record per key" -- rejected: fixing it edits this build's spec; Implementation Notes record the change.

## Design Notes

A `usage` argument rather than always decoding keeps `claude_usage = false` free of usage parsing (ADR-0017) and keeps every existing expectation exact. Problems are returned, not logged, because `core` does no I/O; the engine counts and debug-logs them in Story 8.4. `At` comes from the record whose values are taken, so a read that started mid-Response (the live tail's 8 MiB bound) records the same `At` as the history pass's read from byte 0.

## Verification

`go` is not on this machine's default PATH: prefix commands with `PATH=/Users/jasonviloria/.local/share/mise/installs/go/1.27.2/bin:$PATH`.

**Commands:**
- `go build ./... && go vet ./... && go test -race ./... && test -z "$(gofmt -l .)"` -- all pass
- `go test -tags golden -run TestGoldenUsage ./internal/core -v` -- passes; numbers copied into Implementation Notes
