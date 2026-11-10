---
title: 'technical research: token usage tracking for herddash'
type: 'technical'
topic: 'Token usage tracking for herddash, per Agent and per Profile'
decision: 'Whether and how herddash tracks token usage per Agent and per Profile, and what needs an ADR'
source: 'native run: first-hand reads on the operator Mac, Claude Code docs, prior-art repositories'
status: complete
preset: 'standard'
validation: 'normal'
created: '2026-11-09'
updated: '2026-11-09'
claims_verified: 6
claims_unverified: 2
claims_disputed: 1
---

# technical research: token usage tracking for herddash

**Decision this research serves:** whether herddash should track token usage per Agent and per
Profile, from which source, and which parts need an ADR.

## Executive summary

**Track it, from the transcripts herddash already tails, and record it.** Every Claude Code
response's token counts are on disk in the transcript, per model and per token type [1]. herddash
already knows each pane's transcript and Profile. Two things decide the design:

1. **Counting is easy to get wrong by 2x.** Claude Code writes one record per content block, each
   repeating the response's usage, with `output_tokens` growing to its final value. The unit is the
   last record per (`message.id`, `requestId`); summing all records over-counts about 2.0 to 2.15x
   on this machine [1][14]. Subagents' usage is in separate files under
   `<session>/subagents/`, not the main transcript [1][10].
2. **Only a record lasts.** Claude Code deletes transcripts after 30 days by default [10]; the
   oldest here is 28 days old [1]. No tool found attributes Claude Code usage to an account [13]
   (unverified repository-wide);
   herddash's Profile resolution already does. Persisted usage per Profile is the per-account
   attribution ADR-0001 kept as herddash's reason to exist.

Separately, each config dir's `.claude.json` caches its account's plan usage (five-hour and
weekly utilization with reset times) [2], which would give a per-Profile limit gauge with no
network call. It is undocumented and partly obfuscated, so it is a candidate, not a foundation.

**Biggest caveat:** the transcript format is internal to Claude Code and "changes between
versions" [9], and has broken usage tools before [17][18]. herddash already lives with this for
background work (ADR-0009); usage adds more fields to depend on.

## Sources herddash could read

**Transcripts (first-hand) [1].** All 164,045 assistant records in 3,258 transcripts (Claude Code
2.1.220 to 2.1.311) carry `message.usage`: `input_tokens`, `output_tokens`,
`cache_creation_input_tokens`, `cache_read_input_tokens`, the cache split
`cache_creation.{ephemeral_5m,ephemeral_1h}_input_tokens`, and usually
`output_tokens_details.thinking_tokens`, `server_tool_use.{web_search,web_fetch}_requests`,
`service_tier`, `speed` and `inference_geo`; `message.model` names the model. 80,869 responses
span those records. Within a response, input and cache fields repeat and `output_tokens` never
decreases: the last record is complete, the first nearly so. In 26,530 repeated records the
`output_tokens` differed from the earlier record of the same response; ccusage #1728 describes the
repeats as identical [14], which holds for the other 56,533. `<synthetic>` records have zero usage.
19 responses appear in two files (a continued session, subagent files); deduplicating across files
by the same key removes them. Subagent records (80,105, all `isSidechain`) live only in
`subagents/agent-*.jsonl`.

**Plan usage cache (first-hand, undocumented) [2].** `.claude.json` holds `cachedUsageUtilization`
`{accountUuid, fetchedAtMs, utilization}`. `utilization.five_hour` and `seven_day` are null or
`{utilization, resets_at, used_dollars, limit_dollars, remaining_dollars, locked_reason}`, plus
`limits[] {group, kind, scope, percent, resets_at, severity, is_active}`. Its `accountUuid` equals
the file's own `oauthAccount.accountUuid`. On this machine one account showed a five-hour window
and the other did not. Claude Code's docs say `/usage` loads plan bars from a rate-limited usage
endpoint and falls back to those loaded in the past 60 minutes [8], consistent with this being
that cache; the refresh cadence is unknown. The endpoint is not in the pages read; third-party
tools call it, and [11] is a feature request about what it returns. `projects.<path>.lastModelUsage` also holds per-model
tokens and `costUSD` for the last session in a directory [2]. `stats-cache.json` exists but was last
written in October [2]: not live.

**Not sources.** The session record has no usage fields [3]. herdr 0.9.5 has none: its `tokens`
properties are report-metadata string maps [4]. No config dir sets a statusline [2].

**Official, but needs configuring each Claude Code.**
- *Statusline input* [7][20]: per session, `rate_limits.five_hour` and `seven_day`
  `{used_percentage, resets_at}` for Pro, Max and Ultra subscribers or behind a Claude apps
  gateway that sets a spend limit, after the first response;
  `cost.total_cost_usd` (a client estimate at list price, reset by `/clear`); and
  `context_window` figures for the latest response. Getting it means installing a statusline
  command in every config dir.
- *OpenTelemetry* [6]: `claude_code.token.usage` by `type`, `model` and `query_source`
  (main, subagent, auxiliary), and a per-request `claude_code.api_request` event with token counts,
  `request_id` and an estimated `cost_usd`. Attributes include `session.id` (replaced by `/clear`)
  and `user.account_uuid`. Delta temporality, 60 s default interval, OTLP gRPC or HTTP or a
  Prometheus endpoint. Getting it means setting environment variables in every wrapper and
  receiving OTLP.
- *Messages API price list* [12], current 2026-11-09, for a list-price estimate. Claude Code's own
  dollar figures are estimates [8].

**Codex (first-hand and source) [5][21][23].** `token_count` events carry cumulative
`total_token_usage` and per-response `last_token_usage`, and `rate_limits` with
`primary`/`secondary` `used_percent`, `window_minutes` and `resets_at`; Codex re-emits unchanged
events, so a counter takes `last_token_usage` only when the total advanced [23].

## What the prior art teaches

ccusage (active, v20.0.48 on 2026-11-02) is the reference [13]. Its history is a list of the
mistakes to avoid: counting every content-block record (6.5x over-report in one loader, #1728) [14]; missing
subagent files, part of a 2.25x gap whose reporter says most of it was not subagents (#806) [15]; gateway records without `requestId` counted per
block (#1934) [16]; a field removed (#4) [17] and records dropped after a format change (#1722) [18]. Its key is a hash of
`message.id` and `requestId`, with fallbacks [13]. tokscale (MIT, active) reads Claude Code,
Codex and Gemini files [19]. Claude-Code-Usage-Monitor treats statusline `rate_limits` as the
live truth, labels other figures `official`, `local_estimate`, `experimental` or `unknown`, and has
an opt-in local usage warehouse with durable history [20]. None attributes Claude Code usage to an
account from transcripts [13]. All are limited to one machine's files (#222, open) [22]: the same
limit herddash's one-daemon-per-host design has.

## Cross-dimension insights

- **Durability is herddash's to give.** Claude Code deletes the evidence after 30 days [10];
  most prior art re-reads what is left [13][19], and one tool keeps an opt-in history [20]. A
  record in herddash keeps history beside the Profile and Agent it already records, which no
  other tool has.
- **Attribution is herddash's to give.** Transcripts carry no account [13]; OpenTelemetry does,
  but only where every wrapper is configured [6]. herddash maps each pane to a Profile already, and
  each Profile's `.claude.json` names its account [2].
- **Official live sources conflict with "deliberately unmanaged".** The statusline and
  OpenTelemetry need a change to every Claude Code's configuration [6][7]; the files need none but
  are unofficial [9]. The file path matches how herddash already works.

## Options, and what needs an ADR

| Option | What the operator gets | Cost and risk | ADR |
|---|---|---|---|
| **A. Live per Agent, in memory** | Session tokens on each Box, from the transcript tail plus `subagents/` | New fields from an unstable format; reading subagent files is new | ADR-0009 amendment; a Board design decision (the title bar and Box carry no new fact without one) |
| **B. Recorded per response** | Usage history per Agent, Profile and model, past transcript cleanup, across restarts | Migration (schema 8, a one-way door), retention, a backfill from existing transcripts | **New ADR:** stored schema change; PRD amendment (new FR) |
| **C. Plan gauge per Profile** | Five-hour and weekly utilization per account, with reset times | Undocumented, obfuscated keys; cadence unknown; reads a file that also holds account details | New ADR: a new source; read one key only |
| **D. Statusline hook** | Official per-session limits and cost | Installs a command in every config dir and needs a hand-off to the Daemon | New ADR: configuration herddash would own, maybe a new surface |
| **E. OTLP receiver** | Official tokens per request, with account | Env in every wrapper; a new network surface; OTLP decoding | New ADR: new network surface, likely a dependency |
| **F. Codex** | Tokens and limits for Codex Agents | Profile rules for Codex are still open (PRD §8) | Part of A/B's ADR |

## Recommendations

1. **Take A and B together as one decision** (high confidence in the source and the counting rule,
   [1][14][15]): record one row per completed response (key `message.id` + `requestId`, last
   record wins) with Agent, Profile, model and the token types, and show session totals on the
   Box. Feeds: a PRD amendment and a new ADR for the schema change, then an epic.
2. **Show tokens, not dollars, as the measure** (medium): subscription plans are not billed per
   token, and every dollar figure is a list-price estimate [8][12]. Offer list-price cost only as
   labelled.
3. **Treat C as a separate, later decision** (low: one first-hand source, undocumented) [2]. It is
   the cheapest way to answer "how close is each account to its limit", but should first be
   watched to learn when Claude Code refreshes it.
4. **Do not pursue D or E now** (medium): they contradict "deliberately unmanaged" for data the
   files already hold [6][7].
5. **Pin the format in tests**, as ADR-0009 does: a fixture per observed shape, including
   multi-record responses, subagent files and a missing `requestId` [16].

## Open questions

- When does Claude Code refresh `cachedUsageUtilization`? Watching `fetchedAtMs` against `/usage`
  use for a day would answer it.
- Is the 8 MiB first-read bound acceptable for a backfill, or does B need a separate full read?
- How should a response be attributed when its transcript moves (a continued session, `/clear`)?
  The cross-file duplicates [1] suggest deduplicating by key, not by file.
- Codex's Profile rules and pane titles (PRD §8) block F until one Codex pane is observed.

## Source appendix

| n | Supports | Publisher | Pub date | Accessed | Confidence |
|---|---|---|---|---|---|
| 1 | Usage fields, multi-record responses, subagent files, counts | First-hand: Claude Code transcripts on the operator Mac (2.1.220 to 2.1.311) | 2026-11 | 2026-11-09 | high |
| 2 | Plan usage cache, lastModelUsage, stats-cache, no statusline set | First-hand: `<config dir>/.claude.json`, `settings.json` | 2026-11 | 2026-11-09 | medium |
| 3 | Session record has no usage | First-hand: `<config dir>/sessions/<pid>.json` | 2026-11 | 2026-11-09 | high |
| 4 | herdr has no token counts | First-hand: `internal/herdr/gen/herdr-api-0.9.5.schema.json` | 2026-10 | 2026-11-09 | high |
| 5 | Codex token_count and token_usage_record | First-hand: `~/.codex/sessions` (Codex 0.163.0) | 2026-11 | 2026-11-09 | high |
| 6 | OpenTelemetry metrics, events, attributes | [Anthropic, monitoring-usage](https://code.claude.com/docs/en/monitoring-usage) | undated (v2.1.310) | 2026-11-09 | medium |
| 7 | Statusline input, rate_limits | [Anthropic, statusline](https://code.claude.com/docs/en/statusline) | undated (v2.1.295) | 2026-11-09 | high |
| 8 | /usage, cost estimates, usage endpoint fallback | [Anthropic, costs](https://code.claude.com/docs/en/costs) | undated (v2.1.305) | 2026-11-09 | high |
| 9 | Transcript format is internal | [Anthropic, sessions](https://code.claude.com/docs/en/sessions) | undated | 2026-11-09 | high |
| 10 | Retention, subagent file location | [Anthropic, claude-directory](https://code.claude.com/docs/en/claude-directory) | undated | 2026-11-09 | high |
| 11 | Usage endpoint is called by tools; a feature request about it | [GitHub, anthropics/claude-code #34348](https://github.com/anthropics/claude-code/issues/34348) | 2026-03 | 2026-11-09 | medium |
| 12 | Model prices | [Anthropic, pricing](https://platform.claude.com/docs/en/about-claude/pricing) | current | 2026-11-09 | high |
| 13 | ccusage design, dedup key, no account attribution | [ccusage/ccusage](https://github.com/ccusage/ccusage) | 2026-11 | 2026-11-09 | high |
| 14 | One record per content block; 6.5x over-report | [ccusage #1728](https://github.com/ccusage/ccusage/issues/1728) | 2026-09 | 2026-11-09 | high |
| 15 | Subagent files missed, part of a 2.25x gap | [ccusage #806](https://github.com/ccusage/ccusage/issues/806) | 2026-01 | 2026-11-09 | medium |
| 16 | Records without requestId | [ccusage #1934](https://github.com/ccusage/ccusage/issues/1934) | 2026-10 | 2026-11-09 | high |
| 17 | costUSD dropped from transcripts | [ccusage #4](https://github.com/ccusage/ccusage/issues/4) | 2025-06 | 2026-11-09 | high |
| 18 | Records dropped after a null model in iterations | [ccusage #1722](https://github.com/ccusage/ccusage/issues/1722) | 2026-09 | 2026-11-09 | high |
| 19 | tokscale sources | [junhoyeo/tokscale](https://github.com/junhoyeo/tokscale) | 2026-11 | 2026-11-09 | high |
| 20 | Statusline limits as truth; estimates labelled | [Claude-Code-Usage-Monitor](https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor) | 2026-06 | 2026-11-09 | high |
| 21 | Codex TokenCountEvent | [openai/codex protocol.rs](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs) | 2026-11 | 2026-11-09 | high |
| 22 | Local files cover one machine | [ccusage #222](https://github.com/ccusage/ccusage/issues/222) | 2025-06 | 2026-11-09 | high |
| 23 | Codex re-emits token_count | [ccusage Codex parser](https://github.com/ccusage/ccusage/tree/main/rust/adapters/codex) | 2026-11 | 2026-11-09 | high |

## Staleness map

Every claim here is in a one-month class (versions, formats, API surface, prices). Computed by
`recon_kit.py staleness` with one-month windows: all re-check on **2026-12-01**, the earliest
date. The transcript format and the plan cache are the most likely to move.
