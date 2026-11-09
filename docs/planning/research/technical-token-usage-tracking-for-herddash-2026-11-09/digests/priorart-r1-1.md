# Digest: prior art (web researcher), round 1

Accessed 2026-11-09.

P1 ccusage/ccusage (GitHub releases, API, code `rust/adapters/claude/src/lib.rs`, `paths.rs`, `rust/adapters/codex/src/parser.rs`, `.github/workflows/update-pricing.yaml`, `docs/guide/blocks-reports.md`)
P2 ccusage issues #1728, #1934, #806, #313, #539, #1505, #1818, #876, #1288, #1370, #1970, #2046, #4, #1722, #222, #1987, #1968
P3 https://github.com/junhoyeo/tokscale README (v4.21.0)
P4 https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor README (v4.0.0)
P5 snipeship/ccflare, chiphuyen/sniffly (GitHub API)
P6 openai/codex `codex-rs/protocol/src/protocol.rs` (rust-v0.169.0-alpha.3, 2026-11-07)
P7 https://code.claude.com/docs/en/statusline

## Findings
- ccusage: v20.0.48 2026-11-02, active; org repo ccusage/ccusage; Rust core with adapters for claude, codex, gemini and ~17 more; licence reported NOASSERTION (MIT believed, unverified). P1, high, ecosystem.
- tokscale: MIT, v4.21.0 2026-11-02; reads ~/.claude/projects and transcripts, ~/.codex/sessions and archived_sessions (incl. .jsonl.zst, dedups copies), Gemini chats. P3, high.
- Claude-Code-Usage-Monitor: MIT, v4.0.0 2026-06-27; v4 uses statusline rate_limits as "live source of truth when fresh", else labelled local estimate; scans several dirs without merging accounts' 5-hour windows. P4, high (method medium).
- ccflare is a proxy (not a file reader), no releases, pushed 2026-04-19; sniffly stale (2025-08-08). P5, high.
- Claude Code writes one line per content block, each repeating message-level usage; ccusage's session path once skipped dedup and over-reported 6.5x (#1728, closed 2026-09-16). P2, high.
- ccusage dedup key: hash(message.id, requestId), plus session_id when requestId missing, plus timestamp when id is not `msg_`; comment calls the msg_ test a heuristic. P1, high.
- Gateway lines without requestId and with `resp_` ids were counted per block; totals ~doubled in 20.0.19-20.0.36 (#1934, closed 2026-10-19). P2, high.
- Sidechain: ccusage filters is_sidechain in some session counts and has a replay dedup (message_id, session_id) because "/btw sidechain logs can replay parent messages with new request IDs". P1, high.
- Subagent files were once missed: 2.25x under-count (#806, 2026-01-15, Claude Code 2.1.7); #313; both closed May 2026 (fix unverified). P2, medium.
- /resume replayed context into counts (#539, closed 2025-12-22). P2, medium.
- ccusage reads CLAUDE_CONFIG_DIR as a comma-separated list; setting it replaces the defaults; #1505 proposed extra named dirs (closed 2026-07-27, shipped unverified). P1, P2, high/medium.
- No tool attributes Claude Code usage to a subscription account from transcripts (transcripts carry no account field; unverified). Codex: per-account by rate_limits reset times or session_meta.creator_account_id (#1818, closed 2026-10-02). P2, medium.
- Pricing: ccusage embeds LiteLLM and models.dev snapshots refreshed nightly, with overrides; tokscale LiteLLM with 1 h cache, OpenRouter fallback, --offline. P1, P3, high.
- ccusage 5-hour blocks are a local model with a user-supplied limit; Usage-Monitor's plan limits are guesses (Team "estimate / unverified"). P1, P4, high that it is so, low that numbers are accurate.
- Codex TokenCountEvent {info: TokenUsageInfo {total_token_usage, last_token_usage, last_usage_includes_reasoning, model_context_window}, rate_limits: RateLimitSnapshot}; Codex re-emits unchanged token_count events, so ccusage uses last_token_usage only when the total advanced (#876, #1288); forks replay parent history (#1370), a subagent first response dropped (#1970), fork counted zero (#2046, open 2026-11-09). P1, P2, P6, high.
- Format drift: costUSD dropped in Claude Code 1.0.9 (#4, 2025-06-02); `"model": null` in usage.iterations dropped usage (#1722, 2026-09-15). P2, high.
- Local files cover one machine only; account-wide figures differ (#222, open). P2, high.
- Statusline rate_limits five_hour/seven_day {used_percentage, resets_at}, subscribers only, after first response; cost.total_cost_usd "May differ from your actual bill". P7, high.

## Leads
- Statusline hook per config dir as the official per-session live source; Codex rate_limits; whether #1505/#1818 shipped; ccusage gemini adapter.

## Not found
- A first-hand report of tool vs /usage disagreement; account attribution from Claude transcripts; Gemini field names.
