# Epic 8 Context: Token usage per Agent and per Profile

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Claude Code writes every model Response's token counts into its transcripts, deletes those transcripts after 30 days, and nothing on the Host says which subscription spent them. The Daemon already tails each Claude Code Agent's transcript and knows its Profile, so this epic records each Response once with its Agent and Profile, keeps it under Retention, serves totals through the view and a read-only API, and draws them on a separate `/usage` page. The operator reversed the "no usage dashboard" non-goal for this. herddash records and shows token usage; it does not price it, budget it or act on it. The Board stays the product and its Boxes do not change unless a later design gate approves it.

## Stories

- Story 8.1: The decisions: ADR-0017, the PRD and the spine (done, accepted)
- Story 8.2: Decode usage and fold Responses
- Story 8.3: The usage table
- Story 8.4: Record live usage
- Story 8.5: Serve usage
- Story 8.6: The history pass
- Story 8.7: The usage page
- Story 8.8: Decide whether a Box shows usage

## Requirements & Constraints

- **Response** = all transcript records sharing `message.id` and `requestId`. Each is counted once, at its final counts. Summing every record double-counts.
- **Complete** = its first record with `message.stop_reason` set (all fields final from there on, measured 81,358/81,358). With no stop reason, it is complete when a record of another Response follows it in the same file (records never interleave within a file), and it takes the values of its record with the largest `output_tokens` (the later on a tie).
- **Accepted loss, and only this:** a file's last Response while it has no stop reason, plus any Response in a file the Agent stops reading before then (measured at 32 of 82,035, 0.04%).
- Skip `<synthetic>` records and records whose `usage` is missing or not an object. A record with no `requestId` is not counted, only debug-logged. Record an all-zero Response. Report and count an unknown `usage` shape; never drop it silently.
- **No message text is kept or logged.** The decoder takes only the two ids, the model, the stop-reason flag, the token counts with the cache split (input, output, cache write 5m, cache write 1h, cache read), the record timestamp, and `agentId` in subagent files.
- Claude Code only. Out of scope: Codex/Antigravity, dollar figures, plan-limit gauges, statusline hooks or OTel receivers, transcripts that no present Agent owns, and other Hosts.
- `claude_usage` is a TOML key with no flag, **default true**. When false, nothing is decoded, read or recorded, every `AgentView.usage` is `null`, and `/api/usage` returns no buckets.
- Usage reading, the history pass included, never delays a Status read or a Board update.
- One pull request per story into `develop`. Operator checkpoints are at 8.7 (the page, rendered with live data) and 8.8 (any Box figure).
- **Golden replay:** a test helper that is not shipped. It replays every local transcript through the decoder and ledger and compares the totals with an independent count (per key across files, the record with the largest `output_tokens`, the later on a tie). They must agree except for the documented end-of-file loss. Run it by hand before 8.2, 8.4 and 8.6 merge, and put its numbers in that story's spec.

## Technical Decisions

- **No new dependency, no new import edge.** `core` decodes and folds (standard library only, no clock), `procenv` reads, `engine` decides and writes, `store` keeps, `httpapi` serves. `arch_test.go` must still pass.
- **`core.DecodeTranscriptLine`** gains a usage marker. **`core.UsageLedger`** is new and pure: it folds one file's markers in order, returns the Responses that became complete, holds at most the current pending Response, and has no clock.
- **Live tail:** usage rides on the main transcript's existing tail (last 8 MiB at first sight, then up to 1 MiB of new lines per read, about every 2 s within the 1 s session timeout). Each read also lists `<sessionId>/subagents/agent-*.jsonl` and tails every file whose size changed, each with its own offset and ledger. When the session timeout is hit, the read stops, keeps every offset, and resumes there next time. No subagent file is dropped. A path change (`/clear`, a new session record, a new file) ends that file's ledger and drops its pending Response.
- **History pass:** the first time the engine sees a transcript path (main or subagent), one worker off the actor reads that file from byte 0 to the live tail's start, one file at a time. Each file is capped at 256 MiB, with a pause between files. The worker never takes the session read's busy flag and hands complete Responses to the actor in batches. A cap hit or read failure sets the Agent's `usage.partial` and logs it once. After a Daemon restart everything is re-read, and the store ignores duplicates.
- **Storage:** migration 0008 is the only schema change. It adds an append-only `usage` table with one row per complete Response, inserted once and never updated (`INSERT OR IGNORE`, one transaction per batch).
  - Columns: AgentKey (host, server, epoch, pane_id), `agent_since`, profile, session_id, subagent_id (default `''`), message_id, request_id, model, `responded_at` (UTC ms), the five counts, `stopped` (0/1), `observed_at`.
  - `UNIQUE (message_id, request_id)` and `CHECK (epoch >= 1)`. There is no usage backfill, and the plugin-log importer is untouched.
  - Indexes: by Agent, by `responded_at`, and by (profile, `responded_at`).
- **Agent instance** = AgentKey + `agent_since`, the `observed_at` of the key's last Transition with no previous Status. A new Agent in a reused pane starts at zero. A Response seen again under another Agent (a continued session) keeps the Agent that recorded it first.
- **Profile hold (AD-14):** usage rows queue with the pane's Transitions. While those are held for a Profile lookup, usage is held too and released with the Profile decided at release. A usage row never carries a Profile resolved for another program.
- **Retention:** in the sweep's transaction, delete usage rows older than the cutoff except the present Agent's, and count them in `SweepResult`.
- **Callers and schema:** only the engine calls `store.AppendUsage` (enforced by `callsite_test.go`). Schema 8 is a one-way door: 0.10.x and earlier refuse it. `docs/install.md`'s schema table gains that row.
- **`AgentView.usage`** is always sent: `{input, output, cache_write, cache_read, responses, last_response_at, partial}`, or `null` when usage is off. It is summed from the record at seed, so it survives a restart, and is advanced by each written batch. It changes at most once per transcript read or history batch and is published as the Agent's ordinary `agent` event, with no throttle and no new event name.
- **`GET /api/usage`** (and HEAD) is served under the read routes' guards: loopback Host, no CORS, no new write route.
  - Parameters: `from` and `to` in UTC ms, with `from < to` and at most 31 days apart; `by=profile|model|agent`; `bucket=hour`.
  - Body: `{from, to, by, buckets:[{start, key, input, output, cache_write, cache_read, responses}]}`. A `by=agent` key is `{host, server, epoch, pane_id, agent_since}`.
  - More than 50,000 buckets returns 400 `range_too_large`. Malformed parameters return 400 with `{"error":{"code","message"}}`. JSON fields are `snake_case`.
- **`GET /usage`** is an embedded page: no npm and no build step.

## UX & Interaction Patterns

- `/usage` shows today, the last 7 days and the last 30 days, per Profile and per model. It groups the API's UTC hours into local days.
- When nothing is recorded it says "No usage recorded yet". When `/api/usage` fails it shows an error line.
- `board-browser.mjs` checks the page's drawing, once with recorded data and once with empty data.
- How the page is reached (a title-bar link, which would amend ADR-0013; a link at the Board's foot; or its URL alone) is decided at Story 8.7's design gate, using the operator's live data.
- The Board's Boxes are unchanged. Cache reads dominate the totals (billions), so which figure a Box would show, if any, is Story 8.8's gate, and that would amend ADR-0003 and SM-C1.

## Cross-Story Dependencies

- Each story builds on the one before: 8.2 (decoder and ledger in `core`), then 8.3 (table and store API), 8.4 (engine and procenv live recording, which uses both), 8.5 (view and API from the store), 8.6 (history pass feeding the same actor path and store key), 8.7 (page over `/api/usage`), and 8.8 (optional Board change).
- 8.4 reuses the Profile hold (Epic 5, Stories 5.4 and 5.18) and the successive-Agent boundary (Stories 5.10 and 5.17) to determine `agent_since`.
- 8.4 needs concurrency tests run with `-race -count=50`. 8.4 and 8.5 need state tested across a Daemon restart.
- 8.5 must keep `engine_contract_test.go` replaying through `order.mjs` equal to the published view, with usage changes interleaved with Status changes, a removal and a reconnect.
- 8.6's time and memory cost on the operator's Mac is measured and recorded before it merges.
