# Token usage: where the numbers are, 2026-11-09

The operator asked, on 2026-11-09, whether herddash could track token usage per Agent and per
Profile. This record says what is on this machine and what is documented, as read that day. It
decides nothing. The full research run, with every source, the claims ledger and the options
table, is
[`docs/planning/research/technical-token-usage-tracking-for-herddash-2026-11-09/research.md`](../planning/research/technical-token-usage-tracking-for-herddash-2026-11-09/research.md).

Versions: Claude Code 2.1.220 to 2.1.311 (transcripts), herdr 0.9.5, Codex CLI 0.163.0.

## Provenance

Three kinds of evidence, kept apart as in the socket API record:

- **Confirmed first-hand** on the operator's Mac, by scripts that read field names, types and
  counts only, never message text: the four config dirs' transcripts (3,258 files, 164,045
  assistant records), their `.claude.json`, `settings.json`, `sessions/<pid>.json` and
  `stats-cache.json`; herdr's committed schema dump; `~/.codex/sessions`.
- **Documented** by Anthropic at code.claude.com and platform.claude.com, pages undated, citing
  versions up to 2.1.310. Not tested here.
- **Prior art**, from ccusage, tokscale and Claude-Code-Usage-Monitor: their code, READMEs and
  issues. Not tested here.

## Confirmed first-hand

1. **Every assistant record carries `message.usage`**: `input_tokens`, `output_tokens`,
   `cache_creation_input_tokens`, `cache_read_input_tokens`,
   `cache_creation.{ephemeral_5m_input_tokens, ephemeral_1h_input_tokens}`, `service_tier`,
   `inference_geo`, and in most records `output_tokens_details.thinking_tokens`,
   `server_tool_use.{web_search_requests, web_fetch_requests}`, `iterations` and `speed`.
   `message.model` names the model; `<synthetic>` records have zero usage.
2. **One response is several records.** Records sharing `message.id` and `requestId` (present on
   all but 111 of 164,045) are one API response: 80,869 responses. Input and cache fields repeat;
   `output_tokens` never decreases, and the last record holds the final value. Summing every record
   counts each response about twice: 2.15x for `~/.claude-cldw`, 2.02x for `~/.claude-cldp`.
   **The unit is the last record per (`message.id`, `requestId`).**
3. **Subagents are elsewhere.** Their records, all `isSidechain: true`, are only in
   `projects/<slug>/<sessionId>/subagents/agent-<agentId>.jsonl`; the main transcript holds none.
   herddash reads only the main transcript today.
4. **A few responses are in two files** (19: a session continued into a new file, and subagent
   files). Deduplicating by the key above, across files, handles them.
5. **Each config dir caches its account's plan usage**, undocumented: `.claude.json`
   `cachedUsageUtilization` = `{accountUuid, fetchedAtMs, utilization}`, the `accountUuid` equal to
   the file's `oauthAccount.accountUuid`. `utilization.five_hour` and `seven_day` are null or
   `{utilization, resets_at, used_dollars, limit_dollars, remaining_dollars, locked_reason}`;
   `utilization.limits[]` holds `{group, kind, scope, percent, resets_at, severity, is_active}`;
   many sibling keys have obfuscated names and were null. One account had a five-hour window and
   the other did not. When Claude Code refreshes it is not known.
6. **Not sources:** the session record has no usage fields; `stats-cache.json` was last written in
   October; no config dir sets a statusline; herdr 0.9.5's `tokens` properties are report-metadata
   string maps, not token counts.
7. **Transcripts do not last:** the oldest main transcript is 28 days old and no config dir sets
   `cleanupPeriodDays`.
8. **Codex** writes `event_msg` `token_count` with `info.total_token_usage` and
   `info.last_token_usage` `{input_tokens, cached_input_tokens, cache_write_input_tokens,
   output_tokens, reasoning_output_tokens, total_tokens}` and `rate_limits`
   `{primary, secondary: {used_percent, window_minutes, resets_at}, plan_type, ...}`, and
   `token_usage_record` lines with per-response, per-turn and per-thread usage.

## Documented, not tested here

- **The transcript format is internal:** "The entry format is internal to Claude Code and changes
  between versions, so scripts that parse these files directly can break on any release"
  (code.claude.com/docs/en/sessions). Transcripts are deleted after `cleanupPeriodDays`, default
  30 (claude-directory).
- **Statusline input** carries `rate_limits.five_hour` and `seven_day` `{used_percentage,
  resets_at}` for Pro, Max and Ultra subscribers, after the first response, plus an estimated
  `cost.total_cost_usd` (statusline).
- **OpenTelemetry**, with `CLAUDE_CODE_ENABLE_TELEMETRY=1`: `claude_code.token.usage` by `type`,
  `model` and `query_source`, and a `claude_code.api_request` event per request with token counts
  and `request_id`; attributes include `session.id` and `user.account_uuid` (monitoring-usage).
- **`/usage`** shows plan bars from a rate-limited usage endpoint and falls back to bars loaded in
  the past 60 minutes; its dollar figures are list-price estimates (costs). The endpoint is not
  in the pages read; third-party tools call it (anthropics/claude-code#34348 is a feature request
  about it).

## From prior art, not tested here

- ccusage over-reported 6.5x in one loader that skipped deduplication (#1728); missed subagent
  files were part of a 2.25x gap, most of which its reporter says was not subagents (#806);
  records without `requestId` broke its key (#1934). #1728 describes the repeated records as
  identical; here 26,530 of them differed in `output_tokens`, so take the last.
- No tool found attributes Claude Code usage to an account from transcripts (not checked
  repository-wide). Claude-Code-Usage-Monitor keeps an opt-in local history.

## What would need an ADR

Recording usage is a stored schema change; reading `.claude.json` is a new source; a statusline
hook would be configuration herddash owns; an OTLP receiver is a new network surface. The options
and a recommendation are in the research run linked above.
