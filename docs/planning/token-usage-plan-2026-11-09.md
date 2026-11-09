# Token usage: implementation plan

**Status: proposal, 2026-11-09.** Nothing here is decided. It follows the research record
[`docs/research/token-usage-sources-2026-11-09.md`](../research/token-usage-sources-2026-11-09.md)
and its run folder. When the operator approves it, the plan becomes an ADR, a PRD amendment and
Epic 8 in `epics.md` and `sprint-status.yaml`, built story by story with `/bmad-build`.

## 0. The decision this needs first

**The PRD rules this out today.** §5, Non-Goals: "Not an orchestrator, scheduler, cost tracker or
usage dashboard." ADR-0003 makes the Board's restraint the differentiator. Tracking token usage
reverses a written non-goal, so the first step is the operator's decision, recorded as:

- **ADR-0017, "Token usage per Agent and per Profile"**, amending ADR-0001 (what herddash records),
  ADR-0003 (what the Board may show), ADR-0005 (a new table in the record) and ADR-0009 (more read
  from transcripts, including subagent files);
- **a PRD amendment**: §5's non-goal narrowed to "not a cost tracker or billing tool", with new
  FRs (below), dated as earlier amendments are.

The case for it: per-account attribution and durable history are ADR-0001's two surviving
differentiators, and usage is both. Transcripts are deleted after 30 days, and no tool found
attributes Claude Code usage to an account. The case against it is the non-goal: every new fact
on the Board competes with "which ones are waiting on me".

## 1. Scope

**In:** tokens per completed model response for Claude Code Agents, from their transcripts and
their subagents' transcripts, recorded with the Agent, the Profile, the model and the session;
totals per Agent in the view; totals per Profile and per day from a read-only API; a usage page.

**Out, each a later decision:** dollar costs (list-price estimates only; subscription plans are
not billed per token); the plan-limit gauge from `.claude.json` `cachedUsageUtilization`
(undocumented; watch its refresh cadence first); Codex (its Profile rules are PRD §8 open
questions); a statusline hook or OTLP receiver (both configure every Claude Code, against
"deliberately unmanaged"); usage on other hosts.

## 2. New requirements (PRD)

- **FR-18: Record token usage.** For each Claude Code Agent, every completed model response is
  recorded once, with its AgentKey, Profile, session id, subagent id (empty for the main thread),
  model, response time and its input, output, cache-write (5 min, 1 h) and cache-read tokens.
  Recording survives a Daemon restart and never counts a response twice, whatever file or read it
  is seen in. Retention sweeps it with the rest of the record.
- **FR-19: Show token usage.** The view carries each Agent's totals since its current Agent began;
  `GET /api/usage` returns totals per Profile, model and day over a window; a usage page shows them.
  The Board's Boxes change only after a design gate with the operator's live data.

## 3. Design

### 3.1 Counting rule (`internal/core`)

From the research, first-hand on 3,258 transcripts:

- A response is the set of assistant records sharing `message.id` and `requestId`; one record per
  content block, input and cache fields repeated, `output_tokens` non-decreasing.
- **A response is complete at its first record whose `message.stop_reason` is set.** In every one
  of 20,603 multi-record responses with a stop before the last record, that record already held the
  final `output_tokens`; 19,224 more ended at their last record. Later records repeat it.
- 432 responses (cldw) never had a stop reason: interrupted, or still running when read. Rule: such
  a response is recorded, with the largest values seen and `complete = false`, when a later
  response in the same file completes or the session's transcript path changes; if neither happens
  it stays pending in memory.
- Records with no `requestId` (111 of 164,045) key on `message.id` alone; `<synthetic>` records and
  zero-usage records are skipped.

`core.DecodeTranscriptLine` gains a usage marker: `TranscriptLine.Usage *ResponseUsage` with the
two ids, model, `stop_reason` set or not, the token fields and the record's `timestamp`. Only
these fields are decoded; message text is never kept, as ADR-0009 requires. A new pure
`core.UsageLedger` folds markers into completed responses (pending map bounded, as `earlyCloses`
is) and hands back the ones newly completed. No clock: times come from the record.

### 3.2 Reading (`internal/procenv`, `internal/engine`)

- The main transcript is already tailed for each Claude Code Agent (`readTranscript`). Usage rides
  on the same read.
- **New: subagent transcripts**, `<project dir>/<sessionId>/subagents/agent-*.jsonl`. Each read
  lists that directory and tails each file with its own `procenv.Tail`, bounded (a cap on files
  per session, oldest dropped). Their `agentId` names the subagent.
- **First read.** Today a first read starts at the last 8 MiB (`transcriptFirstBytes`). Usage
  before that bound is lost unless read. Proposal: usage only, a first read of a file's whole
  history in a separate, rate-limited pass, since the store deduplicates (3.3). Measure first: the
  largest transcripts here exceed 8 MiB (21 of 788).
- The engine stamps each completed response with the Agent's AgentKey and resolved Profile at the
  time it is recorded, and appends through the single writer, as Transitions are.

### 3.3 Storing (`internal/store`, migration 0008)

```sql
CREATE TABLE usage (
    id            INTEGER PRIMARY KEY,
    host TEXT NOT NULL, server TEXT NOT NULL, epoch INTEGER NOT NULL, pane_id TEXT NOT NULL,
    profile       TEXT NOT NULL,
    session_id    TEXT NOT NULL,
    agent_id      TEXT NOT NULL DEFAULT '',          -- subagent, '' for the main thread
    message_id    TEXT NOT NULL,
    request_id    TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL,
    responded_at  INTEGER NOT NULL,                  -- the record's timestamp, UTC ms
    input         INTEGER NOT NULL, output INTEGER NOT NULL,
    cache_write_5m INTEGER NOT NULL, cache_write_1h INTEGER NOT NULL, cache_read INTEGER NOT NULL,
    complete      INTEGER NOT NULL CHECK (complete IN (0, 1)),
    observed_at   INTEGER NOT NULL,
    UNIQUE (message_id, request_id)
);
```

`INSERT OR IGNORE` on the unique pair makes recording idempotent across re-reads, restarts, a
continued session's second file and subagent replays. Append-only like the rest of the record;
Retention sweeps by `responded_at`. Schema 8 is a one-way door: 0.10.x cannot open it, so
`docs/install.md`'s schema table and backup step gain a row.

### 3.4 Serving (`internal/httpapi`)

- `AgentView.usage` (always sent): totals since the current Agent began, by token type, plus the
  last response time. Derived from the record at seed, so it survives a restart.
- Changes arrive on the existing `agent` event, coalesced: at most one usage change per Agent per
  5 s, so a working Agent does not flood the stream.
- `GET /api/usage?from=&to=&by=profile|model|day|agent`: read-only, loopback, same guards as the
  other GET routes. No new listener, so no new network surface.
- `/api/agents/.../history` gains the Agent's responses only if the operator wants them there.

### 3.5 Showing

A usage page at `/usage`, linked from nowhere on the Board until a decision, with per-Profile and
per-model totals over today, 7 and 30 days. Whether a Box shows a number, and which one, is a
design gate with live data: totals are dominated by cache reads (billions here), so "tokens" alone
misleads.

### 3.6 Configuration

`claude_usage` (default open question, see §6) in the TOML file, no flag, like `claude_recaps`.

## 4. Stories (Epic 8, draft)

| # | Story | Done when |
|---|---|---|
| 8.1 | ADR-0017 and the PRD amendment | The operator accepts both. **Checkpoint.** |
| 8.2 | Decode usage and fold complete responses (`core`) | Table tests from real record shapes: multi-record, stop before last, no stop, no `requestId`, synthetic; no clock |
| 8.3 | The usage table (migration 0008) | Idempotent insert, sweep, rollback test failing mid-transaction, schema door documented |
| 8.4 | Record usage from main and subagent transcripts (`engine`, `procenv`) | Recorded once across two reads, a restart, a continued session and `/clear`; `-race -count=50`; replay of local transcripts matches an independent count |
| 8.5 | Serve usage (`httpapi`) | `AgentView.usage` and `/api/usage`; contract test through `order.mjs`; coalescing tested |
| 8.6 | The usage page, and the Board decision | Design rendered with live data, approved by the operator. **Checkpoint.** `board-browser` check |
| 8.7 | First-read history (optional) | Measured cost; usage before the 8 MiB bound recorded without delaying status |

## 5. Risks

- **Format drift.** Anthropic calls the transcript format internal. Mitigation: fixtures per
  observed shape, a test that fails on an unknown `usage` shape rather than silently dropping it,
  and a debug log counting skipped records.
- **Wrong totals look right.** The 2x over-count is silent. Mitigation: 8.4's replay against an
  independent count over every local transcript before merging.
- **Read cost.** 1.3 GB of transcripts in one config dir. Only active Agents' sessions are read;
  the first-read pass (8.7) is measured before it ships.
- **Board creep.** Mitigation: the page first, the Box only through 8.6's gate.

## 6. Open questions for the operator

1. Reverse the non-goal at all? (The rest assumes yes.)
2. On by default (numbers only, no text), or opt-in like recaps?
3. What a Box should show, if anything: output tokens, total, or nothing until asked.
4. Is a separate `/usage` page acceptable, given the Board is "one page"?
5. Record interrupted responses (`complete = false`) or drop them?
6. Should usage history start at the first read (8 MiB back) or include each transcript's whole
   history (story 8.7)?
