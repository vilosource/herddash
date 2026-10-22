---
title: "Claude Code's session recap on the Board (ADR-0010)"
type: 'feature'
created: '2026-10-22'
status: 'ready-for-dev'
route: 'dispatch'
review_loop_iteration: 0
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
- **View (core, AD-1):** `AgentView.Recap` (`json:"recap"`, always present, "" when none),
  the state's recap for every class. Sort key, class, Waiting Time and background are unchanged.
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
- [ ] config + example + serve wiring, with tests
- [ ] core: decoder parameter, recap extraction and bounds, state, view field, fixture, with tests covering the matrix
- [ ] engine: option, decoder flag, debug log without text, with tests (on, off, cleared, no leak)
- [ ] Board: row, clamp, label, height rule; node tests; browser-check variant
- [ ] docs: PRD, design doc, spine

**Acceptance Criteria:**
- Given the module, when `go build ./... && go vet ./... && go test -race ./...`, `gofmt -l .`, `node --test internal/httpapi/web/*.test.mjs` and `node internal/httpapi/web/check/board-browser.mjs <binary>` run, then all pass and every matrix row has a test.
- Given a dev build installed with `claude_recaps = true`, when a Claude Agent's transcript has a fresh recap, then its Box shows it, and Boxes with none keep their Context Line.

## Implementation Notes

## Spec Change Log

## Review Triage Log

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- all pass
- `test -z "$(gofmt -l .)"` -- exit 0
- `node --test internal/httpapi/web/*.test.mjs` -- all pass
- `go build -o /tmp/hd ./cmd/herddash && node internal/httpapi/web/check/board-browser.mjs /tmp/hd` -- all pass
