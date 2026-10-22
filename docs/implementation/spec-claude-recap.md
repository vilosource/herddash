---
title: "Claude Code's session recap on the Board (ADR-0010)"
type: 'feature'
created: '2026-10-22'
status: 'done'
route: 'dispatch'
baseline_commit: '950eee6f1bd435a767614214d074dcf8c1fb2280'
review_loop_iteration: 1
context:
  - '{project-root}/docs/adr/0010-claude-code-recap-on-the-board.md'
  - '{project-root}/docs/implementation/spec-transcript-background.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The operator wants "a recap in the boxes": a Finished or Idle Box shows where an
Agent is and its title, not what it did or will do next. Claude Code already writes that as a
two-sentence recap in the session transcript herddash tails (ADR-0009).

**Approach:** Build ADR-0010 (accepted 2026-10-22). When the opt-in config key `claude_recaps` is
true, the transcript decoder also takes a non-sidechain `system` record with `subtype`
`away_summary` and a string `content`. The per-Agent transcript state keeps only the newest one,
cleared when a main turn starts. `AgentView.recap` carries it, and the Board shows it in place of
the Context Line, clamped to two lines. Operator decisions: privacy accepted for the operator;
future users must consent (the opt-in key); two lines; "recaps should be up to date as often as
possible" (the newest recap always wins, and a started turn clears it).

## Boundaries & Constraints

**Always:**
- **Config:** `claude_recaps` (bool, default false), file only (no flag), in `config.example.toml`
  with a comment that it shows Claude Code's own recap text on the Board; `herddash config` prints
  it. `serve` passes it to `engine.Options.ClaudeRecaps`.
- **Decoder (core, pure):** `DecodeTranscriptLine` gains a `recaps bool` parameter. With false, an
  `away_summary` record is no marker, exactly as today. With true, `TranscriptLine.Recap` is the
  content, `strings.TrimSpace`d, then with a trailing `(disable recaps in /config)` removed and
  trimmed again, then cut to at most 400 runes (never a split rune). An empty result, a non-string
  content, a sidechain record or any other subtype gives no recap. Every other field of the record
  is still never read.
- **State:** the transcript state (`BackgroundTasks`, or a renamed wrapper if clearer; keep
  `Clone`) holds the newest recap. `TurnStarted` clears it, and a recap line sets it, newest wins.
  It is in memory only and goes in no store row. It is lost on restart until re-read; the first
  read's 8 MiB window re-finds it.
- **View (core, AD-1):** `AgentView.Recap` (`json:"recap"`, always present, "" when none), the
  state's recap only when the Box is Finished, Idle or Background; a Working, Needs you or
  Unknown Box gets "" (operator decision 2026-10-22, at review: herdr reports a new turn before
  the next transcript read, so a Working Box must never show the last turn's recap). Sort key,
  class, Waiting Time and background are unchanged.
- **Logging:** never the text. When a recap appears or clears, log at debug only `pane` and
  `recap` true/false.
- **Board:** a Box with a non-empty `recap` shows it in the Context Line's row (class `recap`),
  clamped to two lines with an ellipsis. `boxLabel` (accessible name and tooltip) uses the full
  recap in place of the Context Line. While any Box on the Board has a recap, every Box is one
  line taller and the context row two lines high. With none, the layout is exactly as today.
- **Docs:** PRD FR-2 ADR-0010 amendment (dated) with testable consequences, the design doc
  (Context Line row, height rule), the spine's procenv/core row naming the one extra decoded
  record, `config.example.toml`.

**Never:**
- Read, keep or log any other transcript text, or a recap when the key is off.
- Store a recap in the database, or add a route.
- Change the Board for Agents without a recap when no Box has one.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Recap after turn | end_turn, then away_summary "X. (disable recaps in /config)", key on | recap "X." | test fails |
| Key off | same lines, key off | recap "", line not a marker | test fails |
| New turn | recap, then a prompt / tool result / queued command | recap "" | test fails |
| Newest wins | two recaps | the second | test fails |
| Bounds | 1,000-rune content with multibyte runes | 400 runes, valid UTF-8 | test fails |
| Not a recap | sidechain away_summary; other subtype; content not a string; empty after trim | no recap | test fails |
| Privacy | recap on, lines with prompts and outputs | only the recap text leaves the decoder; log has no recap text | test fails |
| View | recap set on a Finished Agent | `recap` in the AgentView JSON; other fields unchanged | test fails |
| View, hidden | recap set; Status working (not Background), blocked, or unknown | `recap` "" | test fails |
| Board, recap | one view with recap | context row shows the recap, two-line clamp, accessible name has the full text, all Boxes one line taller | test fails |
| Board, none | no view with recap | same layout and heights as before | test fails |

</frozen-after-approval>

## Code Map

- `internal/core/transcript.go` -- `TranscriptLine`, `transcriptRecord` (add `Subtype`; `Content` already decoded), `DecodeTranscriptLine`, `BackgroundTasks.Apply`/`Clone`; tests in `transcript_test.go` (matrix table, `TestTranscriptPrivacy`) and the dated fixture `internal/core/testdata/claude-transcript-2026-10-21.jsonl` (add an `away_summary` line, or a new dated fixture).
- `internal/core/view.go` -- `AgentViewInput.Transcript`, `AgentView` (next to `ContextLine`), `NewAgentView`. The golden fixture `internal/core/testdata/board-order.json` gains `recap` on every view, regenerated from `NewAgentView`'s output as before; add one recap input, and update its description.
- `internal/config/config.go` -- follow `AllowUnsupportedHerdr` (file struct, `Config` field and accessor, JSON print, `pick` with no flag); tests in `config_test.go`; `docs/config.example.toml`.
- `cmd/herddash/serve.go:~138` -- pass `ClaudeRecaps` into `engine.Options`.
- `internal/engine/engine.go` -- `Options` (beside `Transcript`), `readTranscript` (~2072) passes the flag to the decoder; `onTranscript` (~2175) logs recap appeared/cleared by pane only; the view input already takes `p.tasks`. Tests in `internal/engine/transcript_test.go` (rig `startTranscript`, `noLeak`).
- `internal/httpapi/web/order.mjs` -- `boxLabel` (~252); tests in `order.test.mjs`. `board.mjs` `fillBox` (~80, the `ctx` line) adds `recap` to the Box; `board.css` `.box` grid (~138), `.context` (~190). `internal/httpapi/web/check/board-browser.mjs` -- add a stand-in variant (as the task-count variants do) checking the recap row, clamp, label and one height.
- Do not change: `procenv` (no new file read), `store`, `arch_test.go`.

## Tasks & Acceptance

**Execution:**
- [x] config + example + serve wiring, with tests
- [x] core: decoder parameter, recap extraction and bounds, state, view field, fixture, with tests covering the matrix
- [x] engine: option, decoder flag, debug log without text, with tests (on, off, cleared, no leak)
- [x] Board: row, clamp, label, height rule; node tests; browser-check variant
- [x] docs: PRD, design doc, spine

**Acceptance Criteria:**
- Given the module, when `go build ./... && go vet ./... && go test -race ./...`, `gofmt -l .`, `node --test internal/httpapi/web/*.test.mjs` and `node internal/httpapi/web/check/board-browser.mjs <binary>` run, then all pass and every matrix row has a test.
- Given a dev build installed with `claude_recaps = true`, when a Claude Agent's transcript has a fresh recap, then its Box shows it, and Boxes with none keep their Context Line.

## Implementation Notes

- **Config.** `claude_recaps` follows `allow_unsupported_herdr` with no flag (`pick[bool](nil, ...)`);
  `Config.ClaudeRecaps()`, printed by `herddash config` as `claude_recaps`; documented in
  `docs/config.example.toml` (so `TestExampleDocumentsEveryKey` holds). `serve` passes it to
  `engine.Options.ClaudeRecaps`.
- **Core.** `DecodeTranscriptLine(line, recaps)`; `transcriptRecord` gains `Subtype`. A `system`
  record is looked at only when `recaps` is true, not a sidechain and `subtype` is
  `away_summary`; `recapOf` trims, drops the trailing `(disable recaps in /config)`, trims again
  and keeps at most `MaxRecapRunes` (400) whole runes. `TranscriptLine.Recap`; `BackgroundTasks`
  (name kept, doc widened) gains `recap`, set by a recap line, cleared by `TurnStarted`, kept by
  `Clone`, read by `Recap()`. An empty-after-trim recap is no marker, so it leaves an earlier
  recap in place (judgment call: only a started turn clears). `AgentView.Recap` (`json:"recap"`,
  never omitted), set by `shownRecap` only when the Box is Background, class finished, or class
  working with a Status other than working (the "View, hidden" row); "" otherwise. New dated fixture `testdata/claude-transcript-2026-10-22.jsonl` (two turns, each
  with `turn_duration` and an `away_summary`); the 2026-10-21 one is unchanged. Golden
  `board-order.json`: `recap` on every view, a `recap` input on w1:p2 (Finished), description
  updated; regenerated by script and checked by `TestGoldenBoard`.
- **Engine.** `Options.ClaudeRecaps` -> `e.recaps`, passed to the decoder in `readTranscript`.
  `onTranscript` logs `claude recap` with `pane` and `recap` true/false when one appears or clears;
  `dropTranscript` became a method so a recap lost with the rest of the transcript state is logged
  the same way.
- **Board.** `order.mjs`: `recapOf`, `contextText` (recap else Context Line), `anyRecap`;
  `boxLabel` uses `contextText`. `board.mjs` draws the row as `context recap` (no `line` class,
  so it wraps) and toggles `board--recaps` on `#board` from `anyRecap` of the model's views.
  `board.css`: `.recap` clamps to two lines (`-webkit-line-clamp: 2`); `.board--recaps .box` is
  `calc(var(--box-h) + 1.5rem)` high with rows `2rem 1.25rem 3rem 1.5rem`.
- **Serve.** `TestServeRecap` runs serve with a child process whose `CLAUDE_CONFIG_DIR` holds a
  session record and transcript (turn ended, subagent open, recap last). It needs a claude Profile
  rule in the config, since the Agent's process is only read for a program with one. Deleting the
  wiring in `serve.go` makes it fail.
- **Not done here.** The acceptance criterion's live check (a dev build installed with
  `claude_recaps = true`) is the operator's / orchestrator's.

## Spec Change Log

- 2026-10-22, review loop 1. Trigger: blind and edge-case findings that a Box reading Working (herdr
  sees the new prompt before the 2 s transcript read) or Needs you could show the previous
  turn's recap. Amended (frozen block, by the operator's answer "Hide on Working/Needs you"):
  the View rule and a "View, hidden" matrix row. Known-bad state avoided: a stale recap beside a
  running turn. KEEP: everything else as built (decoder, state, config, engine, Board, docs);
  the code was not reverted because re-deriving it from the amended spec changes only the view
  rule, which the patch applies.

## Review Triage Log

Review of 2026-10-22 (three layers on the diff from `950eee6`).

| # | Layer | Finding | Verdict | Evidence | Route |
|---|-------|---------|---------|----------|-------|
| 1 | blind, edge ×3 | Working / Needs you Box can show the last turn's recap (≤ 2 s after a prompt; any class gets it) | low | herdr flips to working before the next transcript read; view copied the recap for every class | intent_gap → operator: hide on Working/Needs you; patch view |
| 2 | blind | PRD still says no transcript text appears in any API response | medium | FR-2 ADR-0009 consequence left unqualified | patch: PRD |
| 3 | blind | Privacy test's state assertion can never fail | low | `m.Recap = ""` before `Apply` | patch: test |
| 4 | blind | No test that recaps off decodes every other line identically | low | fixture and matrix tests switched to `true` | patch: test |
| 5 | verification-gap | `serve` wiring of `claude_recaps` untested | low | deleting the line left `go test ./cmd/...` green | patch: `TestServeRecap` |
| 6 | verification-gap | Recap layout only in the hand-run browser check | low | CI runs node tests only | defer (browser check in CI) |
| 7 | blind | Accessible name drops the pane title when a recap shows | false | The label carries what the Box shows; the Context Line is not shown then | reject |
| 8 | blind, edge | 400-rune cut has no ellipsis and can split a grapheme | low | Observed recaps are 162-329 chars; Claude Code caps at 400 | reject |
| 9 | blind | No provenance for the record shape or the cap | false | ADR-0010's Context gives dates, counts and the docs page | reject |
| 10 | blind | Board height jumps when the first recap appears or the last clears | low | The uniform-height rule the operator approved from the mock | reject |
| 11 | blind | Taller grid duplicates the base row template | low | dev-only, two lines of CSS | reject |
| 12 | blind | Transient drops flicker a recap | low | followSession runs on a process change; read failures are rare | reject |
| 13 | blind | Wrong-typed `claude_recaps` untested | low | the shared pick/TOML path is tested for other keys | reject |
| 14 | blind | Browser check exact height compare; ellipsis not asserted | low | line-clamp draws its own ellipsis; exact compare is the existing check's | reject |
| 15 | blind | Nothing marks the text as model-written | low | design as approved from the mock | reject |
| 16 | blind | Spec shows done while live check is open; install docs | false | fix edits this spec; the key is in config.example.toml | reject |
| 17 | blind | Log-attribute count check is opaque | low | readability only | reject |
| 18 | edge | Recap arriving mid-turn should be ignored | false | the 9 of 863 recaps not after a turn end follow a local command or prompt with no model turn; guarding would drop them | reject |
| 19 | edge | Recap kept while session reads fail transiently | low | rare; ends after three failures | reject |
| 20 | edge | An empty newer recap leaves the older one | false | no observed recap is under 162 chars | reject |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- all pass
- `test -z "$(gofmt -l .)"` -- exit 0
- `node --test internal/httpapi/web/*.test.mjs` -- all pass
- `go build -o /tmp/hd ./cmd/herddash && node internal/httpapi/web/check/board-browser.mjs /tmp/hd` -- all pass

Run 2026-10-22 on the implementation: all four commands pass; the browser check's five new recap
checks (row and clamp, label, other Box's Context Line, one height taller, and back to the usual
height with none) pass.
