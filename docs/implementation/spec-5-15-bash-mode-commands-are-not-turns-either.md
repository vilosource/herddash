---
title: 'Story 5.15: Bash-mode commands are not turns either'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: 'fc4573c12bd4713e580e10c93fc4921e1d2c160c'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0010-claude-code-recap-on-the-board.md'
  - '{project-root}/docs/implementation/spec-5-14-a-manual-recap-reaches-the-board.md'
warnings:
  - 'oversized'
  - 'ac-premise-refuted: the verification AC 1 requires shows bash mode starts a model turn; bash records stay turn starts (see Design Notes)'
deferred: []
---

<intent-contract>

## Intent

**Problem:** Story 5.14 left two record shapes unverified: bash-mode input (`!cmd`: user records starting `<bash-input>` then `<bash-stdout>`, with `<bash-stderr>`) and `<local-command-stderr>` results. Both decode as turn starts today, which would wrongly clear a recap if they are local actions. 5.14's code also owes a follow-up review.

**Approach:** Settle each shape against this machine's transcripts by structure only. Verified (2026-10-23, 473 transcripts, Claude Code 2.1.267 to 2.1.292): all 56 `<bash-input>` and 56 `<bash-stdout>` records are non-meta, non-sidechain string user records; `<bash-stderr>` never leads a record, it sits inside every `<bash-stdout>` string; and 54 of 55 bash commands with a later main-thread assistant record were answered by a real model turn whose parent chain reaches the `<bash-stdout>` record, with no prompt between (the 55th had a prompt between). `<bash-stdout>` records from 2.1.286 on carry `turnOrigin: "human"`. So bash mode **is** a turn on this Claude Code: keep its records as turn starts, pin that with tests, record the evidence, amend ADR-0010. `<local-command-stderr>` was seen 0 times; treat a user record starting it as the older-form local result, the twin of `<local-command-stdout>` (a `local_command` system record is already a result by subtype, whatever its content). Then review 5.14's code (commit 0d1bc68) and fix or defer its findings.

## Boundaries & Constraints

**Always:** structure only (type, isMeta, isSidechain, leading tag of a string content); no transcript text in the repo, fixtures use placeholders; `internal/core` stays stdlib-only and clock-free; `TranscriptLine` field set unchanged.

**Never:** do not make bash-mode records non-turns (refuted above); no change to the engine read loop, ADR-0009 tail bounds, view or Board; no reading of a `<local-command-stderr>` record's text.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Bash command answered | recap X, turn ended; `<bash-input>`, `<bash-stdout>` (stderr inside), assistant end_turn | recap "", turn ended | none |
| Bash command, reply running | recap X, turn ended; `<bash-input>`, `<bash-stdout>` | recap "", turn not ended | none |
| Prompt after bash | bash pair, end_turn, prompt | turn started | none |
| Old stderr result | `<command-name>` then user record starting `<local-command-stderr>` | CommandResult; recap and turn kept, pending cleared | none |
| local_command with stderr | `<command-name>` then local_command `<local-command-stderr>`, commandRun recap | CommandResult, no recap | none |
| Recaps off | the bash pair | same turn state as recaps on | none |

</intent-contract>

## Code Map

- `internal/core/transcript.go:72-78` -- local tags; add `tagStderr = "<local-command-stderr>"`. `localUser` (201-221) returns tagStdout for both result tags (or a result marker); `DecodeTranscriptLine` case "user" (133-141) maps it to `CommandResult`. Bash records fall to `default` (TurnStarted) -- unchanged. Doc comment (80-122): name `<local-command-stderr>` and state bash mode is a turn start, with the evidence.
- `internal/core/transcript_local_test.go` -- `TestLocalCommandMatrix` rows; constants `caveat`, `cmdName`, `oldResult`; helpers `foldRecaps`, `recap`, `prompt`, `endTurn` (in `transcript_test.go`). Add `bashInput`, `bashStdout` shapes (placeholder text; stdout string holds `<bash-stdout>…</bash-stdout><bash-stderr>…</bash-stderr>`) and a `TestBashModeIsATurn`.
- `internal/engine/transcript_test.go:447` `TestTranscriptManualRecap` -- pattern (`startTranscript`, `writeTranscript`, `awaitRecap`, `recapNoLeak`) for an engine test that a `!cmd` answered by the model clears the recap on the AgentView.
- `docs/research/` -- new `claude-code-bash-mode-2026-10-23.md`, frozen record (version, date, method, counts).
- `docs/adr/0010-claude-code-recap-on-the-board.md:9-28` -- extend the 5.14 amendment with a dated Story 5.15 note.
- `docs/implementation/spec-5-14-a-manual-recap-reaches-the-board.md` -- its `deferred` bash-mode entry is resolved here (note in that spec's Auto Run Result).
- Review target for 5.14 follow-up: `git show 0d1bc68 -- internal/` (transcript.go, transcript_local_test.go, transcript_test.go, engine/transcript_test.go).

## Tasks & Acceptance

**Execution:**
- `internal/core/transcript.go` -- add tagStderr as an older-form local result; doc comment names it and bash mode as a turn start -- the AC's stderr half, and stating the verified rule where the next reader looks.
- `internal/core/transcript_local_test.go` -- bash-mode shapes, matrix rows above, decode assertions (both bash records `{TurnStarted: true}`, stderr records `{CommandResult: true}`) -- pins verified shapes.
- `internal/engine/transcript_test.go` -- subtest: Finished Agent with recap; append bash pair + end_turn; recap cleared, Finished; log carries no text -- outermost surface.
- `docs/research/claude-code-bash-mode-2026-10-23.md` -- evidence record.
- `docs/adr/0010-claude-code-recap-on-the-board.md` -- amendment note.
- `docs/implementation/spec-5-14-a-manual-recap-reaches-the-board.md` -- mark its deferred bash-mode finding resolved by this story.

**Acceptance Criteria:**
- Given a Finished Agent showing a recap, when the transcript gains a `!cmd` pair and the model's reply, then the view's recap is "" (bash mode starts a model turn, verified against real shapes).
- Given a pending local command, when a user record starting `<local-command-stderr>` follows, then the recap and turn state are unchanged.
- Given a prompt after bash-mode records, then a turn starts.
- Given Story 5.14's code, when reviewed, then every finding is fixed or deferred with a reason in this spec's triage log.
- Given `go build/vet/test ./...` and `gofmt -l .`, then all pass and print nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Follow-up review of Story 5.14's code (commit 0d1bc68, `internal/`)
- verdicts: 7 findings — high 0, medium 1, low 4, false 2
- findings:
  - `[medium]` `[patch]` bash-mode records and `<local-command-stderr>` (5.14's deferred entry) — bash records are turn starts by evidence (Design Notes, research record), pinned by `TestBashModeIsATurn` and the engine subtests; a user record starting `<local-command-stderr>` is now `CommandResult`.
  - `[low]` `[patch]` `localUser`'s doc and `DecodeTranscriptLine`'s marker list named only `<local-command-stdout>` and did not say what bash mode is — both updated.
  - `[low]` `[patch]` `TestRecapOffChangesNothingElse` did not cover the `isCompactSummary` record, so recaps-off parity for it was untested — added, with the stderr and bash shapes.
  - `[low]` `[reject]` `TestLocalCommandFixture` slices the fixture at fixed positions (`lines[:7]`, `len-2`) — test-only; the fixture is synthetic and fixed, and a change to it fails the marker comparison first.
  - `[low]` `[reject]` a recap replaced by a manual recap logs nothing (the engine logs only presence changes) — by design (ADR-0010: logs say only that a recap was seen, by pane).
  - `[false]` `[reject]` a `CommandResult`-only line now returns ok and reaches `Apply` — the engine only folds markers (`readTranscript`); a result must reach `Apply` to clear `pending`.
  - `[false]` `[reject]` an `isMeta` user record starting `<command-name>` starts a turn — intended and pinned by the matrix row "meta command-name starts"; only a non-meta one was observed as a command.

(The entry above is the implementer's own pass over 5.14's code; the independent four-layer review below covered this story's diff and 5.14's code commit 0d1bc68 together.)

### 2026-10-23 — Review pass
- verdicts: 25 findings — high 0, medium 0, low 17, false 7, maybe-false 1
- findings:
  - `[low]` `[patch]` (verification-gap other) the story's title and planning text state the premise the build refuted — dated "Amended 2026-10-23 (built)" note added under Story 5.15 in `docs/planning/epics.md`; title kept for traceability.
  - `[low]` `[reject]` (edge) a `!cmd` the model never answers leaves the turn open with the recap cleared — pre-existing behaviour, identical to an interrupted prompt; 1 of 56 commands, at end of file; now stated in the research record. A guard would need a turn signal the decoder lacks.
  - `[maybe-false]` `[reject]` (edge) leading whitespace before a local-command tag decodes as a turn start — no observed record has leading whitespace (98 commands in 5.14, 56 bash pairs here); if true only low.
  - `[false]` `[reject]` (edge) array-form content with a local-command tag — every observed local-command and bash record has string content (5.14 evidence; 56/56 here).
  - `[false]` `[reject]` (edge) an older-form `/recap` result as a user record gives no recap — by design: ADR-0010's 5.14 amendment says Claude Code without `commandRun` gets no manual recap.
  - `[low]` `[patch]` (edge claim) the rule reads as always true though 2 of 56 commands were not answered by an unprompted reply — research record now gives the 54/55/56 split and the unanswered case.
  - `[low]` `[patch]` (blind) planning text still states the refuted premise — same root as the first row; fixed there.
  - `[low]` `[patch]` (blind) the 56th command is never explained — same as the edge claim; fixed there.
  - `[false]` `[reject]` (blind) the privacy test does not cover bash or stderr lines — `TestBashModeIsATurn` and the matrix decode loop assert exact equality with `{TurnStarted: true}` / `{CommandResult: true}`, so no text can be kept.
  - `[low]` `[patch]` (blind) research provenance omits fields actually read — field list completed (subtype, timestamp, promptId, turnPosition, message.model, stop_reason, content block types) and "real model turn" defined.
  - `[low]` `[patch]` (blind) `<local-command-stderr>` stated as fact with no source — marked UNVERIFIED (defensive assumption by symmetry) in the research record and ADR-0010.
  - `[low]` `[patch]` (blind) the sample was misdescribed — it was 318 transcripts in `~/.claude-cldp` (7 commands) and 155 in `~/.claude-cldw` (49), not cldp alone; research record and ADR corrected, other config dirs named as untested.
  - `[false]` `[reject]` (blind) ADR-0009 not amended — ADR-0009's rule (a non-sidechain user record starts a turn) is unchanged by this story; bash records already fall under it.
  - `[low]` `[reject]` (blind) no sidechain, isMeta or leading-`<bash-stderr>` bash cases — none of these shapes was observed; sidechain user records are covered generically.
  - `[low]` `[patch]` (blind) "prompt after bash" proved nothing from empty state — now starts from a recap and an ended turn.
  - `[low]` `[patch]` (blind) the engine subtest never observed the recap cleared while the reply runs — now writes the pair, awaits recap "", then the end_turn.
  - `[low]` `[reject]` (blind) `localUser` returns `tagStdout` for a stderr record — cosmetic; its doc comment says so.
  - `[low]` `[reject]` (blind) bash shapes not in the shared fixture file — the inline shapes carry every field the decoder reads, including `turnOrigin`; a fixture adds nothing the decoder sees.
  - `[low]` `[patch]` (blind) decoder doc comment hard-codes statistics — removed; the rule and the research pointer remain.
  - `[false]` `[reject]` (intent) the bash half inverts AC 1 — the story's stated source is 5.14's deferred finding, whose evidence says to settle it by "checking whether an assistant turn follows them"; that check answers yes (54/55), and AC 1 itself requires verification against real records. Correction to Design Notes: the "record and close" pattern is spelled out per story (5.2, 5.3), not as an epic-wide rule; the decision rests on the source's settlement test and ADR-0010.
  - `[false]` `[reject]` (intent) tests stop at AgentView, not the Board — the Board draws `AgentView.recap` unchanged and `/api/state` serves the view verbatim; same ruling as 5.14.
  - `[low]` `[reject]` (intent) stderr covered only in core and its shape is synthetic — the engine folds markers unchanged; the shape is now marked UNVERIFIED.
  - `[low]` `[patch]` (intent) no test of a prompt after a stderr result — matrix row "prompt after stderr result" added.
  - `[low]` `[patch]` (intent) parent-chain evidence is weak — research record now gives the chain breakdown: direct 40, attachments only 13, one isMeta system-reminder 1, a typed prompt 1.
  - `[false]` `[reject]` (intent) the 5.14 follow-up review is not visible in the diff — it is in this spec's triage log, committed with the story, and this pass reviewed 0d1bc68 directly.

## Design Notes

AC 1 of the story reads "neither clear the recap nor mark a turn as started, judged by structure only and verified against real record shapes". The verification it requires refutes the premise for bash mode: Claude Code here sends `!cmd` output to the model, which replies (often with tool use). Treating the pair as no turn would keep a recap through a real model turn, the stale-recap bug ADR-0010 exists to avoid. The epic's rule for Claude Code findings applies: record the evidence in `docs/research/`, and where no code change is needed close the deferred entry with it. The stderr half of AC 1 holds and is implemented.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./... && gofmt -l .` -- expected: success, gofmt prints nothing
- `go test -race -count=20 -run 'Transcript|Recap|LocalCommand|BashMode' ./internal/core/ ./internal/engine/` -- expected: pass

## Auto Run Result

Status: done

**Summary.** The verification AC 1 requires refuted its premise for bash mode: in 473 transcripts (`~/.claude-cldp` and `~/.claude-cldw`, Claude Code 2.1.267 to 2.1.292) 54 of 56 `!cmd` pairs were answered by a real model turn with no prompt between. Bash-mode records (`<bash-input>`, `<bash-stdout>` with `<bash-stderr>` inside) therefore stay turn starts and clear the recap, now pinned by tests; no decoder change for them. A user record starting `<local-command-stderr>` (never observed; UNVERIFIED, by symmetry) is now a local result, so it keeps the recap and turn state, and a prompt after it still starts a turn. Story 5.14's code got its follow-up review (implementer's pass plus the four-layer pass above); nothing high or medium was found in it.

**Files.**
- `internal/core/transcript.go` -- `tagStderr` as an older-form local result; decoder doc names it and bash mode as a turn.
- `internal/core/transcript_local_test.go` -- bash shapes, `TestBashModeIsATurn`, stderr matrix rows and decode checks.
- `internal/core/transcript_test.go` -- recaps-off parity covers `/compact` summary, stderr and bash records.
- `internal/engine/transcript_test.go` -- subtests "bash mode is a turn" and "bash mode reopens the turn".
- `docs/research/claude-code-bash-mode-2026-10-23.md` -- new frozen evidence record.
- `docs/adr/0010-claude-code-recap-on-the-board.md` -- Story 5.15 amendment.
- `docs/implementation/spec-5-14-a-manual-recap-reaches-the-board.md` -- deferred bash-mode entry marked resolved.
- `docs/planning/epics.md` -- dated note under Story 5.15 that its premise was refuted.

**Review.** 25 findings: 12 low patched (docs accuracy, three test tightenings, one test row, the epics note), 0 deferred, 13 rejected (7 false, 1 maybe-false, 5 low) with reasons in the triage log.

**Follow-up review recommended:** false. No high or medium entry was patched (0 high, 0 medium, 12 low patched).

**Verification.** `go build ./...` ok; `go vet ./...` ok; `go test -count=1 ./...` all 12 packages ok; `gofmt -l .` printed nothing; `go test -race -count=20 -run "Transcript|Recap|LocalCommand|BashMode" ./internal/core/ ./internal/engine/` ok.

**Residual risks.** The story's AC 1 is met only for the stderr half; for bash mode it is deliberately inverted on evidence, which the orchestrator should accept or overrule. The `<local-command-stderr>` user-record shape is assumed, not observed. A `!cmd` the model does not answer leaves the turn open, as an interrupted prompt already does.
