# ADR-0017: Token usage per Agent and per Profile

**Status: Accepted** by the operator on 2026-11-11 ("i accept"), as Story 8.1. The operator
decided on 2026-11-09 to reverse PRD §5's non-goal "Not an orchestrator, scheduler, cost tracker
or usage dashboard" so herddash can track token usage (vfkb, "the operator reverses the
usage-dashboard non-goal"), and on 2026-11-11 answered the plan's two remaining design questions:
recording is on by default, and each transcript's earlier Responses are read by a history pass.
It amends [ADR-0001](0001-what-herddash-is.md), [ADR-0003](0003-the-board-is-the-product.md),
[ADR-0005](0005-append-only-transition-record.md),
[ADR-0007](0007-read-only-loopback-http-surface.md) and
[ADR-0009](0009-background-work-from-claude-code-transcripts.md). It leaves
[ADR-0004](0004-runtime-dependencies.md) unchanged: no new dependency.

## Context

ADR-0001 kept two differentiators after the prior art took the rest: a durable history, and each
Agent attributed to its Profile, which usually means its subscription. Token usage is the question
those two answer best that nothing else on this machine does. The research of 2026-11-09
([record](../research/token-usage-sources-2026-11-09.md), with its
[run folder](../planning/research/technical-token-usage-tracking-for-herddash-2026-11-09/research.md))
found, first-hand:

- **Usage is on disk per response.** Every assistant record of a Claude Code transcript carries
  `message.usage`: input, output, cache-write (5 min and 1 h) and cache-read tokens, with the
  model.
- **One response is several records.** Records sharing `message.id` and `requestId` are one API
  response; summing every record counts each about twice. Subagents' records are only in
  `<sessionId>/subagents/agent-*.jsonl`, which herddash does not read today.
- **Transcripts do not last.** Claude Code deletes them after `cleanupPeriodDays`, 30 by default.
- **No tool found attributes Claude Code usage to an account.** herddash already resolves the
  Profile of every Agent whose transcript it tails (ADR-0008, ADR-0009).

The implementation plan, revised for 14 findings of an independent review, is
[token-usage-plan-2026-11-09.md](../planning/token-usage-plan-2026-11-09.md). This record decides
what it proposes. The transcript format is internal to Claude Code and may change in any release
(Anthropic's sessions documentation, not tested here).

## Decision

### What is recorded

- **A Response** is the set of transcript records sharing `message.id` and `requestId`. Each
  completed Response of a Claude Code Agent whose transcript herddash reads is recorded once, with
  the Agent's AgentKey and start, its Profile, session id and subagent id, its model and response
  time, and its five token counts. A record with no `requestId` is not counted, and is counted in
  a debug log. `<synthetic>` records and records whose `usage` is missing or not an object are
  skipped. A Response whose counts are all zero is still recorded.
- **Complete** means its first record with `message.stop_reason` set, whose fields are final
  (measured: 81,358 of 81,358 Responses), or, with no stop reason, a record of another Response
  following it in the same file (records of two Responses never interleave within a file,
  measured), at its largest values.
- **What is not recorded**, and only this: a file's last Response while it has no stop reason, and
  any Response in a file the Agent stops reading before then. Measured on 2026-11-09: 32 of 82,035
  Responses (0.04%). An unknown `usage` shape is reported by the decoder and counted, never dropped
  silently.
- **No message text is kept.** The decoder takes only the ids, the model, whether a stop reason is
  set, the token counts, the record's timestamp and, in a subagent file, `agentId`.
- **Codex, Antigravity and other programs are not recorded.** Codex's Profile rule is still PRD §8
  Q3.

### Where it is read

- **The live tail.** The main transcript's existing tail (ADR-0009) also yields usage. Each
  session read also lists `<sessionId>/subagents/` and tails each subagent file whose size changed,
  with its own offset and ledger, within the session read's timeout, resuming where it stopped. A
  path change (`/clear`, a new session record, a new file) ends that file's ledger.
- **The history pass.** The live tail starts at a file's last 8 MiB. The first time the engine
  sees a transcript path, main or subagent, one worker off the actor reads that file from byte 0
  to the live tail's start, one file at a time, capped at 256 MiB per file, and never takes the
  session read's busy flag, so Status reads never wait on it. It hands complete Responses to the
  actor in batches. A cap hit or read failure marks the Agent's usage `partial` and logs it once.
  After a Daemon restart every file is read again; the store ignores what it already holds.

### How it is kept

- **A second append-only table, `usage`** (migration 0008), one row per Response, inserted once,
  complete, never updated, under a unique key of (`message_id`, `request_id`): the Response's own
  identity, so a Response seen twice (a continued session, a restart, the history pass, a
  subagent replay) is one row, and it keeps the attribution of the Agent that first recorded it.
- **An Agent instance** is its AgentKey and `agent_since`, the `observed_at` of the key's last
  Transition with no previous Status (AD-3). A new Agent in a reused pane starts at zero.
- **Epoch 0 never appears** (a CHECK on `epoch >= 1`); the plugin-log importer is untouched and
  there is no usage backfill.
- **Profile.** Usage rows join the pane's Profile hold (AD-14): while its Transitions are held,
  its usage rows are too, released with the same Profile.
- **Retention.** The sweep deletes usage rows older than the cutoff, in its transaction, except
  the present Agent's, so present totals never shrink. Transcripts are deleted after 30 days, so
  the record becomes the only copy of anything older.

### How it is shown

- **`AgentView.usage`**, always sent: the present Agent's totals, response count, last response
  time and `partial`; `null` when usage is off. It is summed from the record at seed, so it
  survives a restart, and changes at most once per transcript read or history batch, published as
  that Agent's ordinary `agent` event: no new event name.
- **`GET /api/usage`** (and HEAD): hourly UTC totals by Profile, model or Agent between `from` and
  `to` (at most 31 days, at most 50,000 buckets), from a `store` read, under the read routes'
  guards. No write route is added.
- **`GET /usage`**: one embedded page showing today, 7 days and 30 days by Profile and by model.
  How it is reached is decided when it is designed (Story 8.7); a link in the title bar would
  amend ADR-0013 then.
- **The Board's Boxes do not change.** A Box shows a usage figure only after a design gate with
  the operator's live data (Story 8.8), which would amend ADR-0003 again.

### Configuration

`claude_usage` in the TOML file, no flag, **default true**. False means nothing is decoded, read or
recorded for usage, and `AgentView.usage` is `null`.

### Out of scope, each a later decision

Dollar costs (list-price estimates, and subscriptions are not billed per token); the plan-limit
gauge in `.claude.json` (undocumented); a statusline hook or OpenTelemetry receiver (each would
configure every Claude Code, or open a network surface); transcripts no live Agent owns; usage on
other Hosts.

## What this amends

- **ADR-0001:** herddash records token usage beside Transitions, per Agent and per Profile.
  The Daemon still gathers only what its surfaces show.
- **ADR-0003:** the Daemon serves a second page, `/usage`. The Board stays the product and gains
  nothing without a design gate.
- **ADR-0005:** a second append-only table with its own unique key and sweep rule.
- **ADR-0007:** two GET routes, `/usage` and `/api/usage`. Still loopback, no CORS, no new write.
- **ADR-0009:** transcripts are no longer read for the view only: usage numbers, never text, are
  recorded, and subagent transcripts are read too.

## Consequences

- PRD §5's non-goal narrows to "not an orchestrator, scheduler, billing or cost tool"; FR-18 and
  FR-19 are added; SM-C1 notes that the Board is unchanged by this; §8 closes the question. The
  spine amends AD-4, AD-6, AD-7, AD-8, AD-10 and AD-14 and its capability map. The import graph is
  unchanged: `core` decodes and folds, `procenv` reads, `engine` decides and writes, `store` keeps,
  `httpapi` serves.
- Schema 8 is a one-way door: 0.10.x and earlier cannot open it. `docs/install.md` gains the row
  and the backup step applies.
- Each Daemon start reads every live transcript once more. Story 8.6 measures the cost on the
  operator's Mac before the history pass ships.
- The transcript format may drift. Fixtures for each observed shape, the unknown-shape count and a
  golden replay against an independent count (run by hand before each usage story merges) make
  drift visible rather than silent.
- A Response from a session that continues into a new file under a different Agent stays with the
  Agent that recorded it first. That is accepted: the pair is a global identity, and a Response
  is never counted twice.
- Built as Epic 8, one pull request per story into `develop`, with checkpoints at 8.1 (this
  record), 8.7 (the page, with live data) and 8.8 (any Box figure).
