---
title: 'Story 5.14: A manual /recap reaches the Board, and local commands are not turns'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: '9f5b4de19be1a87ab72bc2642983fd98c298000d'
review_loop_iteration: 0
followup_review_recommended: true
context:
  - '{project-root}/docs/adr/0010-claude-code-recap-on-the-board.md'
  - '{project-root}/docs/adr/0009-background-work-from-claude-code-transcripts.md'
warnings: []
deferred:
  - summary: >-
      Bash-mode input (`!cmd`) records (`<bash-input>`, `<bash-stdout>`, `<bash-stderr>`) and `<local-command-stderr>` results may still decode as turn starts and clear a recap.
    evidence: |-
      Reported by two review layers from knowledge of Claude Code, not from this machine's transcripts; the story names slash commands only. Settle by counting user records whose string content starts with those tags in ~/.claude-cldp/projects/*/*.jsonl and checking whether an assistant turn follows them.
    location: >-
      internal/core/transcript.go DecodeTranscriptLine case "user"
    severity: medium (unverified)
---

<intent-contract>

## Intent

**Problem:** Running `/recap` in a Claude Code pane does not update its Box: the decoder reads only `away_summary`, and the local command's own user records (the `isMeta` caveat and the `<command-name>` record) count as a turn start, which clears whatever recap the Box had and resets the turn state. `/usage`, `/compact`, `/model` and every other local command do the same damage.

**Approach:** Teach `core.DecodeTranscriptLine` the local-command records by structure (type, subtype, `isMeta`, `commandRun.command`, the leading tag of a string content), and teach `BackgroundTasks.Apply` that a `<command-name>` record is a *pending* command: its turn start is confirmed only when the next marker is not that command's local result. With recaps on, a `local_command` result whose `commandRun.command` is `"recap"` carries the recap. Amend ADR-0010 with a note naming the second source.

## Boundaries & Constraints

**Always:**
- Only structure is read: `type`, `subtype`, `isSidechain`, `isMeta`, `commandRun.command`, and whether a string `message.content` / `content` *starts with* one of `<local-command-caveat>`, `<command-name>`, `<local-command-stdout>`. The only text that may leave the decoder is a recap, and only with `recaps` true.
- With recaps off, a `local_command` record's `content` and `commandRun` are never looked at; the record is still a local-command result by `type`/`subtype` alone.
- A recap from `/recap` is trimmed exactly as `away_summary` (`recapOf`: trim, drop trailing `(disable recaps in /config)`, cap `MaxRecapRunes` at a rune boundary), after removing the `<local-command-stdout>`…`</local-command-stdout>` wrapper.
- A skill command (user record starting `<command-message>`) still starts a turn immediately, as today.
- `internal/core` stays stdlib-only and clock-free; `TranscriptLine` gains only bool fields (privacy test updated to the new field set).

**Never:**
- No change to background-task start/end handling (Story 5.2 is changing it concurrently), to the engine's read loop, to ADR-0009's tail bounds, or to the view/Board.
- No reading of the command name or args from the `<command-name>` record; no recap from a `local_command` without `commandRun.command == "recap"` (older Claude Code without `commandRun` is not supported for manual recaps).
- No operator transcript text in tests or docs; fixtures use placeholders.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Manual recap | recaps on; turn ended; caveat, `<command-name>`, `local_command` {commandRun recap, stdout "R."} | recap "R.", turn still ended | none |
| Manual recap replaces | recaps on; away_summary "X." then the /recap triple with "R." | recap "R." | none |
| Local command keeps recap | recaps on; recap "X."; caveat, `<command-name>`, `local_command` (usage) | recap "X.", turn ended unchanged | none |
| Old local result | `<command-name>` then user record starting `<local-command-stdout>` | no turn start, recap kept | none |
| Pending confirmed | `<command-name>` followed by a prompt, tool result, end_turn or any non-result marker | turn started (recap cleared) before that marker applies | none |
| Pending at end of read | `<command-name>` is the last marker | state unchanged; pending carried into the next read and Clone | none |
| Skill command | user record starting `<command-message>` | turn started, recap cleared | none |
| Recaps off | the /recap triple | no recap; local result is still a marker, content unread | none |
| Bad recap content | commandRun recap with non-string content, no stdout tags, empty stdout, sidechain | no recap; still a local result (not sidechain) | ignored |

</intent-contract>

## Code Map

- `internal/core/transcript.go` -- `TranscriptLine` (add `CommandPending`, `CommandResult` bools), `transcriptRecord` (add `IsMeta`, `CommandRun`), `DecodeTranscriptLine` `case "user"` (local records are not `TurnStarted`) and `case "system"` (`local_command`), `recapOf` (reuse after unwrapping stdout), `BackgroundTasks` (add `pending bool`), `Apply` (confirm/cancel pending at top), `Clone` (copy pending). Doc comment on `DecodeTranscriptLine` lists every marker; extend it.
- `internal/core/transcript_test.go` -- `TestTranscriptPrivacy` asserts the exact field set of `TranscriptLine` and that only `away_summary` lines yield a recap; `TestRecapMatrix`, `TestRecapOffChangesNothingElse` extend naturally. Helpers `fold`, `foldRecaps`, `recap`.
- `internal/core/testdata/` -- synthetic fixtures; add `claude-transcript-2026-10-23.jsonl` shaped on Claude Code 2.1.292 records (all text placeholders).
- `internal/engine/engine.go:2085` `readTranscript` -- decodes every line then `Apply`s markers in order; carries `BackgroundTasks` across reads, so pending survives. No change needed.
- `internal/engine/transcript_test.go:289-370` -- `trRecap`, `recapsOn`, `awaitRecap`, `recapNoLeak`, `TestTranscriptRecap`: pattern for an engine-level test that the view's `Recap` follows a manual `/recap`.
- `docs/adr/0010-claude-code-recap-on-the-board.md` -- add a dated amendment note naming the second source and that local commands are not turn starts.

Evidence (structure only, `~/.claude-cldp/projects/*/*.jsonl`, 2026-10-23): every one of 98 non-meta user records starting `<command-name>` was followed (attachments aside) by a local result — a `system`/`local_command` (2.1.220+) or a `user` record starting `<local-command-stdout>` (older and some commands). `commandRun {command,args}` appears from 2.1.273; 21 `recap` results, stdout wrapped in the tags, no suffix, 200-399 chars. Skill commands start `<command-message>` (29), followed by an `isMeta` text record. All contents are strings.

## Tasks & Acceptance

**Execution:**
- `internal/core/transcript.go` -- add the fields and decoding above; `Apply`: if `b.pending` and the line is not `CommandResult`, start the turn (turnEnded=false, recap="") then clear pending; `CommandResult` clears pending; `CommandPending` then sets it (so a second command after an unanswered one confirms the first) -- local commands must not touch turn state, the recap source must follow ADR-0010.
- `internal/core/testdata/claude-transcript-2026-10-23.jsonl` -- new synthetic fixture: a turn, its end, an away_summary, a `/usage` triple, a `/recap` triple, a skill command -- pins real shapes.
- `internal/core/transcript_test.go` -- fixture test, matrix rows from the I/O table, privacy test updated (new fields; recap allowed from `local_command` recap lines too), recaps-off test -- covers every matrix row.
- `internal/engine/transcript_test.go` -- engine test: recaps on, Done Agent with a recap; append a `/usage` triple (recap kept), then a `/recap` triple (view shows the new recap), log carries no recap text -- outermost surface (AgentView).
- `docs/adr/0010-claude-code-recap-on-the-board.md` -- amendment note.

**Acceptance Criteria:**
- Given recaps on and a Finished Agent, when the transcript gains a `/recap` triple, then the Agent's view `recap` becomes the stdout text within the usual transcript read.
- Given a recap on the view, when a `/usage` triple is appended, then the recap and `TurnEnded` are unchanged.
- Given recaps off, when the same lines are read, then no recap appears and every non-recap marker equals the recaps-on decode.
- Given `go build/vet/test ./...` and `gofmt -l .`, then all pass and print nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 26 findings — high 0, medium 5, low 9, false 9, maybe-false 3
- findings:
  - `[low]` `[patch]` (blind) ADR amendment says commandRun.command is read, then that the command's name is never looked at — reworded: compared with "recap" only, never kept; the `<command-name>` record's name and args never read.
  - `[medium]` `[patch]` (blind) any non-result marker (task notification, away_summary) confirms a pending command, so a /recap overlapping a notification reopens an ended turn and clears the recap — Apply now confirms only on TurnStarted/TurnEnded; pending cleared by those or a result; matrix rows updated, row "notification then result" added.
  - `[medium]` `[patch]` (blind) /compact's `isCompactSummary` user record still starts a turn although the ADR claims /compact is fixed (shape confirmed in this machine's transcripts: non-meta, lead text, before the caveat) — decoded `isCompactSummary`, no turn start; row "compact keeps recap" added; ADR amended.
  - `[maybe-false]` `[defer]` (blind) bash-mode `!cmd` records still start turns — not observed or checked here; deferred, medium unverified.
  - `[low]` `[reject]` (blind) a /recap that prints an error would show it as the recap — all 21 observed /recap results were 200-399 char recaps; filtering would add a guessed error-shape branch.
  - `[false]` `[reject]` (blind) command-first prompt shows stale state until the next marker — no `<command-name>` record was ever followed by anything but its local result (98 of 98); documented trade-off in Design Notes.
  - `[false]` `[reject]` (blind) manual recap during a running turn shows beside it — core.view.go sets recap only on Finished/Idle/Background (PRD FR-2 amendment), and the next tool result clears it.
  - `[low]` `[reject]` (blind) localUser decodes message before the sidechain check; isMeta stdout record untested — no behavioural harm; cosmetic.
  - `[low]` `[patch]` (blind) dead length guard in manualRecap — deleted.
  - `[low]` `[reject]` (blind) engine test misses recaps-off and a pending carried across engine reads — covered at core (TestRecapOffChangesNothingElse, TestLocalCommandPendingAcrossReads); engine reads carry BackgroundTasks unchanged (engine.go readTranscript).
  - `[low]` `[reject]` (blind) privacy test finds recap lines by raw substring — test-only, over fixtures the test itself writes.
  - `[false]` `[reject]` (blind) version claims lack a research record — the evidence is in this spec's Code Map and the ADR amendment, the same way ADR-0010 carries its own observation.
  - `[low]` `[patch]` (blind) docs that name the recap source not updated — spine procenv row and PRD FR-2 amendment each gained one clause naming the second source; how-herddash-works and the design doc do not name the record.
  - `[medium]` `[patch]` (edge) task notification between command and result confirms a turn — same root cause as the blind finding above; fixed there.
  - `[maybe-false]` `[defer]` (edge) bash-mode records — same as the deferred entry.
  - `[low]` `[reject]` (edge) /recap error or ANSI stdout shown as recap — none observed in 21 results; recap text is already rendered as text by the Board.
  - `[low]` `[patch]` (edge) output-less command followed by another local command confirms the first — fixed with the confirmation rule (CommandPending no longer confirms).
  - `[medium]` `[patch]` (edge) /compact summary record starts a turn — same as the blind finding; fixed there.
  - `[medium]` `[patch]` (edge) matrix pinned "confirmed by a notification" — row changed to "notification keeps pending".
  - `[maybe-false]` `[patch]` (verification-gap other) /compact record shape unverified — verified against transcripts (isCompactSummary, lead text, non-meta) and fixed as above.
  - `[false]` `[reject]` (intent) Board surface untested; tests stop at AgentView — the Board draws AgentView.recap unchanged since ADR-0010 (board-browser check covers `.board--recaps`), and /api/state serves the view verbatim; AgentView is the outermost surface this story changes.
  - `[false]` `[reject]` (intent) fixtures are synthetic, not copied — the AC asks for record shapes copied from real transcripts with no transcript text beyond need; field sets were taken from 2.1.292 records, text is placeholder.
  - `[false]` `[reject]` (intent) with recaps off the `<command-name>` record's leading tag is still read — the consent covers recap content; a leading-tag check is structural and needed for turn state regardless, like the existing `<task-notification>` check (ADR-0009); stated in the ADR amendment.
  - `[false]` `[reject]` (intent) the older user-record `<local-command-stdout>` result is beyond the story — it is the same local command's result in older releases (observed 2.1.220 to 2.1.292).
  - `[false]` `[reject]` (intent) retroactive turn start on a pending command is a design choice the story does not state — it is the story's "skill command that does start a model turn still clears the recap" case generalised; now limited to turn-shaped markers.
  - `[false]` `[reject]` (intent) skill command not exercised at engine level — decoder/fold tests cover it; the engine applies markers unchanged.

## Design Notes

A per-line decoder cannot see the next record, so the decision moves into the fold: `<command-name>` sets `pending`; the next marker either is the local result (cancel) or is anything else (confirm a turn start first, then apply). A command that never produces a result but starts a model turn (none observed) is therefore at worst one marker late. Markers that are not lines `DecodeTranscriptLine` reports (attachments other than `queued_command`) never reach `Apply`, so an attachment between a command and its result does not confirm it.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./... && gofmt -l .` -- expected: success, gofmt prints nothing
- `go test -race -count=20 ./internal/core/ ./internal/engine/ -run 'Transcript|Recap'` -- expected: pass

## Auto Run Result

Status: done

**Summary.** `core.DecodeTranscriptLine` now tells a local command's records apart by structure: the `isMeta` caveat is no marker, the `<command-name>` record is `CommandPending`, the result (`system`/`local_command`, or an older user record starting `<local-command-stdout>`) is `CommandResult`, and /compact's `isCompactSummary` record is no turn start. `BackgroundTasks.Apply` holds a pending command and confirms its turn start only if a turn starts or ends before its result. With recaps on, a `local_command` whose `commandRun.command` is "recap" carries the recap (unwrapped, then trimmed and capped as `away_summary`). Skill commands (`<command-message>`) start a turn as before.

**Files.**
- `internal/core/transcript.go` -- decoder and fold changes.
- `internal/core/transcript_local_test.go` -- fixture, I/O matrix and pending-across-reads tests.
- `internal/core/testdata/claude-transcript-2026-10-23.jsonl` -- synthetic fixture shaped on 2.1.292 records.
- `internal/core/transcript_test.go` -- privacy and recaps-off tests extended.
- `internal/engine/transcript_test.go` -- `TestTranscriptManualRecap` at the AgentView.
- `docs/adr/0010-claude-code-recap-on-the-board.md` -- amendment naming the second source.
- `docs/prd.md`, `ARCHITECTURE-SPINE.md` -- one clause each naming the second source.
- `docs/implementation/epic-5-context.md` -- recompiled (it predated Stories 5.10 to 5.14).

**Review.** 26 findings: 9 patched (3 medium root causes: pending confirmation, /compact summary; plus low ADR wording, dead guard, docs), 1 deferred (bash-mode records, unverified), the rest rejected with reasons in the triage log.

**Follow-up review recommended:** true. Two medium entries were patched (the pending-confirmation rule and the /compact summary record), 0 high. The named unverified risk: a real transcript placing a turn-shaped marker between a local command and its result, which none of the 98 observed commands did, would still confirm a turn.

**Verification.** `go build ./...` ok; `go vet ./...` ok; `go test -count=1 ./...` all 12 packages ok; `gofmt -l .` printed nothing; `go test -race -count=20 -run Transcript ./internal/core/ ./internal/engine/` ok; `-run LocalCommand` ok.

**Residual risks.** Story 5.2 changes `Apply` concurrently; a small merge there is likely. Older Claude Code without `commandRun` gets no manual recap.

**Deferred bash-mode entry: resolved by Story 5.15** (2026-10-23). Bash-mode records are turn starts on purpose: Claude Code sends a `!cmd`'s output to the model, which replies (54 of 55 observed commands; `docs/research/claude-code-bash-mode-2026-10-23.md`), so they stay turn starts, now pinned by tests. A user record starting `<local-command-stderr>` is now a local result, like `<local-command-stdout>`. The follow-up review of this story's code is in Story 5.15's triage log.
