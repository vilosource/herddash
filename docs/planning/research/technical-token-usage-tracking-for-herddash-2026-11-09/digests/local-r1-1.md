# Digest: local, first-hand (lead), round 1

Accessed 2026-11-09 on the operator's Mac. Structure only: field names, counts and types; no message
text was read. The scripts are in the session scratchpad; each claim is reproducible.

Source L1 = Claude Code transcripts under the four config dirs (`~/.claude-cldp`, `~/.claude-cldw`,
`~/.claude`, `~/.claude-oneio`), `projects/**/*.jsonl`: 3,258 files, written by Claude Code 2.1.220
to 2.1.311. Source L2 = `<config dir>/.claude.json`. Source L3 = `<config dir>/sessions/<pid>.json`.
Source L4 = herdr 0.9.5's schema dump committed at `internal/herdr/gen/herdr-api-0.9.5.schema.json`.
Source L5 = `~/.codex/sessions/**/rollout-*.jsonl` (Codex CLI 0.163.0, 10 files).

## Findings

- {claim: every `type: "assistant"` record (164,045) carries `message.usage` with `input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `cache_creation.{ephemeral_5m_input_tokens, ephemeral_1h_input_tokens}`, `service_tier` and `inference_geo`; most also carry `output_tokens_details.thinking_tokens`, `server_tool_use.{web_search_requests, web_fetch_requests}`, `iterations`, `speed` and `fallback_credit` (null in every record), source: L1, confidence: high, class: format}
- {claim: `message.model` names the model per record; `<synthetic>` records (124) have zero usage, source: L1, confidence: high, class: format}
- {claim: one API response is written as several assistant records sharing `message.id` and `requestId` (163,934 of 164,045 records have a `requestId`); 164,045 records are 80,869 responses. Within a response the input and cache fields repeat and `output_tokens` never decreases, and the last record always holds the largest, source: L1, confidence: high, class: behaviour}
- {claim: summing every record over-counts about 2x (naive / last-record-per-response: 2.15 for cldw, 2.02 for cldp); taking the first record per response under-counts output only slightly (first 9.321e9 vs last 9.331e9 total tokens for cldw). The correct unit is the last record per (`message.id`, `requestId`), source: L1, confidence: high, class: behaviour}
- {claim: subagent work is in separate files, `projects/<slug>/<sessionId>/subagents/agent-<agentId>.jsonl`, all `isSidechain: true` (80,105 records); the main transcript holds no sidechain records (83,961 main records). A per-Agent total must add the session's `subagents/` files, which herddash does not read today, source: L1, confidence: high, class: format}
- {claim: 19 responses appear in two files: 12 between two main transcripts (a session continued into a new file) and 7 between subagent files, 1 main/subagent. Deduplicating by (`message.id`, `requestId`) across files removes them, source: L1, confidence: high, class: behaviour}
- {claim: 148 records are compaction summaries (`isCompactSummary: true`); compaction does not rewrite earlier usage, source: L1, confidence: medium, class: behaviour}
- {claim: `.claude.json` in each config dir holds `cachedUsageUtilization` = `{accountUuid, fetchedAtMs, utilization}`, its `accountUuid` equal to that file's `oauthAccount.accountUuid`. `utilization.five_hour` and `utilization.seven_day` are null or objects `{utilization, resets_at, used_dollars, limit_dollars, remaining_dollars, locked_reason}`; `utilization.limits[]` items are `{group, kind, scope, percent, resets_at, severity, is_active}`; also `seven_day_breakdown`, `extra_usage`, `spend` and many obfuscated feature keys (all null here). Both files were fetched at 2026-11-09 13:23 local, source: L2, confidence: high for shape, low for stability (undocumented, obfuscated keys), class: format}
- {claim: per config dir this is per account: subscription usage for a Profile is readable from a file herddash can already locate, with no network call, source: L2, confidence: high, class: behaviour}
- {claim: `.claude.json` `projects.<path>.lastModelUsage.<model>` holds `inputTokens`, `outputTokens`, `cacheReadInputTokens`, `cacheCreationInputTokens`, `webSearchRequests`, `costUSD` (some `thinkingTokens`): the last session's totals per directory, source: L2, confidence: medium (meaning inferred from the name), class: format}
- {claim: the session record has no usage fields (keys: cwd, entrypoint, kind, messagingSocketPath, name, nameSince, nameSource, peerFeatures, peerProtocol, pid, pidDomain, procStart, sessionId, startedAt, status, statusUpdatedAt, updatedAt, version, optional waitingFor), source: L3, confidence: high, class: format}
- {claim: no config dir sets `statusLine`, so no statusline input was observed, source: L2 and settings.json, confidence: high, class: behaviour}
- {claim: herdr 0.9.5 has no LLM token or cost field. Its `tokens` properties (PaneInfo, WorkspaceInfo, AgentInfo, report_metadata params) are string maps of up to 32 keys `^[A-Za-z0-9_-]{1,32}$`, report metadata, not token counts, source: L4, confidence: high, class: API surface}
- {claim: Codex CLI 0.163.0 writes `event_msg` / `token_count` with `info.total_token_usage` and `info.last_token_usage` `{input_tokens, cached_input_tokens, cache_write_input_tokens, output_tokens, reasoning_output_tokens, total_tokens}` and `model_context_window`, plus `rate_limits` `{primary, secondary: {used_percent, window_minutes, resets_at}, credits, plan_type, ...}`; and `token_usage_record` with `usage`, `turn_token_usage`, `thread_token_usage` and ids; `session_meta` has `creator_account_id`, source: L5, confidence: high for this version only, class: format}
- {claim: transcript volume: `projects/` is 1.3 GB (cldp) and 1.1 GB (cldw); a full Python scan of all 3,258 files takes minutes, source: L1, confidence: high, class: behaviour}

- {claim: no config dir sets `cleanupPeriodDays`, and the oldest main transcript is dated 2026-10-12 (cldw) and 2026-10-13 (cldp), about 28 days back, consistent with a 30-day default cleanup: transcripts are not a lasting history, source: L1 and settings.json, confidence: medium (the default is from D5 in the docs digest), class: behaviour}
- {claim: `stats-cache.json` exists in both dirs with `dailyModelTokens[] {date, tokensByModel}`, `modelUsage`, `dailyActivity`, but was last written 2026-10-01 (cldp) and 2026-10-09 (cldw): stale, not a live source, source: L2 dir, confidence: high, class: behaviour}

## Leads

- The usage cache's refresh cadence (who fetches it, how often, from which endpoint).
- Whether `iterations[]` can hold more than one entry (all had 0 or 1 here, equal to the top level).
- Whether a five-hour window is null for one plan type and present for another (cldp null, cldw present).

## Looked for, not found

- Usage in the session record or in herdr. A statusline input sample.
