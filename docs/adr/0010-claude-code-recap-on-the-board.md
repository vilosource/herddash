# ADR-0010: Claude Code's session recap on the Board

**Status: Proposed** on 2026-10-22, for the operator's approval with a Board mock built from the
live state. It amends one rule of [ADR-0009](0009-background-work-from-claude-code-transcripts.md):
that no transcript text is ever kept, sent or shown.

## Context

The operator asked whether Claude Code's recap could be shown in the Boxes: "the idea is to have
a recap in the boxes". A Finished or Idle Box today says where an Agent is (workspace, tab,
directory) and its Pane Title as the Context Line, which for Claude Code is a short session title
such as "Resume work". It does not say what the Agent did or what it will do next, which is what
the operator needs when coming back to it.

Claude Code has a **session recap**. Its documentation (code.claude.com/docs/en/interactive-mode,
"Session recap", read 2026-10-22) says it "generates in the background once at least three
minutes have passed since the last completed turn and the terminal is unfocused", only once the
session has at least three turns and "never twice in a row"; it is on by default and turned off
under `/config` → Session recap; `/recap` asks for one by hand; output is capped at 400
characters. The documentation does not say the recap is stored. It is. Observed on 2026-10-22 across the
184 transcripts written on this machine in the previous 14 days, in four config directories:

- It is a transcript record of its own: `type: "system"`, `subtype: "away_summary"`, a
  `timestamp` and a string `content`, alongside the usual envelope fields (`sessionId`, `cwd`,
  `uuid`, `isSidechain` false, `isMeta` false).
- 863 records in 78 transcripts. 854 of them follow the turn's `system`/`turn_duration` record,
  182 to 185 s later (p10 to p90): one recap about three minutes after a turn ends.
- `content` is 162 to 329 characters, two sentences in the first person, what is happening and
  "Next I'll …", always ending with " (disable recaps in /config)".

The other session summaries Claude Code writes were considered and are weaker: `ai-title` and
`custom-title` records and the session record's `name` are titles, which the Context Line already
approximates; hooks and the status line would mean installing into Claude Code, which herddash
never does.

ADR-0009 lets herddash tail exactly this transcript, and decodes from it only ids, a status word
and turn boundaries. It promises that "message text, tool inputs and outputs, prompts and results
are never kept, logged, stored, sent or shown". A recap is text, written by the model about the
operator's work, and can name repositories, pull requests, tickets and hosts. Showing it is a
privacy decision, which is why this is an ADR.

## Decision

- **Source.** The transcript ADR-0009 already tails; no new file is opened, no new watch. The
  decoder recognises one more record by fixed field names: a non-sidechain `system` record whose
  `subtype` is `away_summary` and whose `content` is a string.
- **Only the latest, only in memory.** The Daemon keeps, per Agent, the latest recap's text and
  its timestamp, trimmed of whitespace and of the trailing " (disable recaps in /config)", capped
  at 400 characters, Claude Code's documented cap (cut at a character boundary). Nothing else from the record is kept. It is never written
  to the record, never logged (logs say only that a recap was seen, by pane), and is lost on a
  Daemon restart until the transcript is read again.
- **Fresh only.** A recap applies from its record until the main thread's next turn starts (the
  ADR-0009 turn-start markers: a prompt, a tool result or a queued command). After that the Agent
  has none until Claude Code writes another. A Working Agent therefore never shows one, and a
  stale recap is never shown beside a new turn.
- **View.** `AgentView` gains `recap` (string, empty when there is none). Every other derived
  value is unchanged: the record, the Attention Class, the Fold, Waiting Time and the order.
- **Board.** When `recap` is not empty it takes the Context Line's row, clamped to two lines, with
  the full text in the Box's tooltip and accessible name; otherwise the Context Line shows as
  today. A Box grows by at most one line.
- **Surface.** It is served where AgentViews already are, the read-only loopback API and its event
  stream (ADR-0007); no new route, no new network surface.
- **Failure is quiet.** A Claude Code release that stops writing the record, or recaps turned off,
  leave `recap` empty and the Box as today.

## Consequences

- A Finished or Idle Claude Code Box says, in two sentences, what the Agent did and what it means
  to do next, without the operator switching to its pane.
- herddash now sends conversational text to the browser for the first time. It is Claude Code's
  own summary, not the conversation, and it stays on loopback in memory, but anything on the
  operator's machine that can read `127.0.0.1` sees it, as it already sees Pane Titles.
- The recap is Claude Code's undocumented on-disk format, as ADR-0009's markers are; a fixture
  pins the shape observed on 2026-10-22.
- Not every Agent has one: sessions of fewer than three turns, sessions whose terminal stayed
  focused, and every program but Claude Code keep the Context Line. How "unfocused" is judged
  for a pane inside herdr (the terminal window, or herdr's own pane focus) is UNVERIFIED; 78 of
  184 recent transcripts hold a recap.
- herddash cannot ask for a recap: typing `/recap` into a pane would break the Board's read-only
  rule (ADR-0003).
- The PRD's FR-2 and the design doc gain the recap; the spine's procenv adapter row names the one
  extra record decoded.

## Alternatives considered and rejected

- **Session titles (`ai-title`, `custom-title`, the session record's `name`).** Already close to
  the Context Line; they say what a session is about, not where it stands.
- **Hooks or the status line.** Would carry the last assistant message, but need installing into
  every Claude Code config dir, which herddash never does.
- **Summarising the transcript ourselves.** Means reading the conversation and calling a model:
  far more data read and a new dependency, for what Claude Code already writes.
- **Tooltip only.** No visible change to the Board, but hides the one thing the operator asked to
  see at a glance.
- **Keep recaps in the record** for history. Persisting model-written text about the operator's
  work in a database that outlives the session is a larger privacy step than showing the latest
  one; not needed for the Board.
