# Digest: Claude Code documentation (web researcher), round 1

Accessed 2026-11-09. Pages are undated; each cites the Claude Code versions noted.

D1 https://code.claude.com/docs/en/monitoring-usage (cites v2.1.214 to v2.1.310)
D2 https://code.claude.com/docs/en/statusline (cites up to v2.1.295)
D3 https://code.claude.com/docs/en/costs (cites up to v2.1.305)
D4 https://code.claude.com/docs/en/sessions
D5 https://code.claude.com/docs/en/claude-directory
D6 https://github.com/anthropics/claude-code/issues/34348 (2026-03-14 to 16)
D7 https://platform.claude.com/docs/en/about-claude/pricing

## Findings

- OTel: CLAUDE_CODE_ENABLE_TELEMETRY=1; OTEL_METRICS_EXPORTER otlp|prometheus|console|none; OTEL_LOGS_EXPORTER; OTEL_EXPORTER_OTLP_PROTOCOL grpc|http/json|http/protobuf, no default; Prometheus on localhost:9464/metrics. D1, high, API surface.
- OTel metrics every 60 s, logs every 5 s by default; temporality defaults to delta. D1, high.
- claude_code.token.usage {type input|output|cacheRead|cacheCreation, model, query_source main|subagent|auxiliary, attribution attrs}; claude_code.cost.usage (USD, + speed, effort). D1, high.
- claude_code.api_request event: model, cost_usd, cost_usd_micros, input/output/cache_read/cache_creation tokens, request_id, duration_ms, speed, effort, query_source; 5m/1h cache split from v2.1.300; cost_usd "Estimated". D1, high.
- Standard attributes: session.id (replaced by /clear), organization.id, user.account_uuid, user.account_id, user.email, user.id, terminal.type; OTEL_RESOURCE_ATTRIBUTES keys included; events carry prompt.id, agent_id, parent_agent_id. No config-dir attribute documented. D1, high.
- Statusline stdin: session_id, transcript_path, model, cost.total_cost_usd (client estimate at list price, reset by /clear), context_window{total_input_tokens,total_output_tokens,context_window_size,used/remaining_percentage,current_usage{...}}, exceeds_200k_tokens. D2, high.
- Statusline rate_limits.five_hour / seven_day {used_percentage, resets_at epoch s}: only for claude.ai Pro/Max/Ultra subscribers (or a gateway with spend limit); after the first API response; windows can be absent; spend_limit v2.1.251, USD fields v2.1.284; no introduction version given for five_hour/seven_day. D2, high.
- /usage: per-session tokens by model and a local list-price dollar figure; resets on /clear since v2.1.211; subscribers get plan usage bars from a "usage endpoint" that is often rate limited, falling back to bars loaded in the last 60 min (v2.1.208); local 24h/7d attribution "approximate"; history in ~/.claude/stats-cache.json. D3, D5, high.
- Transcripts: "The entry format is internal to Claude Code and changes between versions, so scripts that parse these files directly can break on any release." Deleted after cleanupPeriodDays (default 30). Subagent transcripts in <session>/subagents/. D4, D5, high.
- GET https://api.anthropic.com/api/oauth/usage is undocumented; used by third parties with the OAuth token and anthropic-beta: oauth-2025-04-20; returns five_hour/seven_day {utilization, resets_at} and extra_usage; 429s reported. D6 and community sources, medium.
- Enterprise Analytics API and the Claude Code Analytics API (Admin key) exist; neither is local or per-session. D3, high.
- Prices (USD per MTok, input / 5m write / 1h write / hit / output): Opus 5.5 4/5/8/0.20/20; Opus 5, 4.8-4.5 5/6.25/10/0.50/25; Sonnet 5.5 2/2.50/4/0.10/10; Sonnet 5 2/2.50/4/0.20/10; Sonnet 4.6, 4.5 3/3.75/6/0.30/15; Haiku 5.5 0.10/0.125/0.20/0.01/0.50 (<=100k prompt), 0.50/0.625/1/0.05/2.50 (>100k); Haiku 4.5 1/1.25/2/0.10/5; Fable or Mythos 5.1 10/12.50/20/0.25/50; Fable or Mythos 5 10/12.50/20/1/50. US-only inference 1.1x on 4.6+; fast mode Opus 5.5 8/40. D7, high, pricing.
- Claude Code's cost: list price, 1.1x residency, managed `modelPricing` override (v2.1.242+); always an estimate. D3, high.

## Leads
- CHANGELOG for when rate_limits five_hour/seven_day arrived; `claude -p --output-format json` usage; /context output.

## Not found
- The Messages API usage schema verbatim; whether /cost exists; docs for /api/oauth/usage; an OTel attribute for config dir or plan; stats-cache.json schema.
