---
title: 'Story 8.3: The usage table'
type: 'feature'
created: '2026-11-11'
status: 'done'
baseline_commit: '3fc275de97545872a61652cd26aa2f67c99ba47f'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/adr/0017-token-usage-per-agent-and-per-profile.md'
  - '{project-root}/docs/implementation/epic-8-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Story 8.2 folds transcripts into complete `core.Response`s, but the record has nowhere to keep them, so usage cannot survive a restart, be summed per Agent, or be bounded by Retention (ADR-0017).

**Approach:** Migration 0008 adds the append-only `usage` table. `store.AppendUsage` inserts a batch with `INSERT OR IGNORE` in one transaction; the store reads one Agent instance's sums and hourly totals by Profile, model or Agent; `Sweep` deletes usage older than the cutoff except the present Agent's, in its transaction. No engine behaviour changes: nothing calls `AppendUsage` until Story 8.4.

## Boundaries & Constraints

**Always:** Columns exactly as ADR-0017 and the spine (AD-4) list: host, server, epoch, pane_id, `agent_since`, profile, session_id, `subagent_id` (default `''`), message_id, request_id, model, `responded_at` (UTC ms), input, output, `cache_write_5m`, `cache_write_1h`, `cache_read`, `stopped` (0/1), `observed_at`, plus an `id INTEGER PRIMARY KEY`. `UNIQUE (message_id, request_id)`, `CHECK (epoch >= 1)`, counts and `responded_at` CHECKed `>= 0`, `agent_since > 0`, ids non-empty. Indexes: by Agent instance, by `responded_at`, by (profile, `responded_at`). A Response seen again keeps the first row (its Agent and Profile). Store owns the row type (`store.UsageRow`, a `core.Response` plus AgentKey, `AgentSince`, Profile, SessionID, ObservedAt), as it owns `store.Present`. Hours are `responded_at - responded_at % 3600000`. The `/api/usage` cap is enforced by the caller's `limit`: more buckets than `limit` returns `store.ErrTooManyBuckets`, not a truncated list. Store calls stay under the caller's context (the engine already passes `WithoutCancel`).

**Never:** update or delete a usage row outside `Sweep`; touch the importer, `transitions` or `acknowledgements` tables; add a dependency or import edge; change the engine beyond logging the new sweep count and passing `Present.AgentSince` (0 until Story 8.4); add an HTTP route (Story 8.5).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected |
|---|---|---|
| Duplicate batch | same batch appended twice | one row per Response; second append inserts 0 |
| Overlap | batch sharing some keys with rows written | only new keys inserted; existing rows unchanged (first Agent kept) |
| Same key, other Agent | a Response already written under Agent A, appended under Agent B | row stays A's |
| Mid-transaction failure | trigger raises on the Nth insert, after earlier rows written | error; no row of the batch kept |
| Epoch 0 | raw INSERT with epoch 0 | database refuses (CHECK) |
| Empty batch | `AppendUsage(nil)` | no-op, nil error |
| Sweep, gone Agent | old rows of a key not in present | deleted, counted in `SweepResult.Usage` |
| Sweep, present Agent | old rows with the present key and its `AgentSince` | kept |
| Sweep, earlier Agent on a present key | old rows with the present key but an older `agent_since` | deleted |
| Sweep, `AgentSince` 0 | present key, since unknown | all its rows kept |
| Sums | `UsageTotals(key, agentSince)` | input, output, cache_write (5m+1h), cache_read, responses, last `responded_at`; zero value when none |
| Hourly | `UsageHourly(from, to, by, limit)`, `from <= responded_at < to` | one bucket per (hour, key), ordered by start then key |
| Too many buckets | more than `limit` buckets | `ErrTooManyBuckets` |
| Upgrade | schema-7 database with rows | opens at 8, earlier rows intact; failed 0008 rolls back to 7 |

</frozen-after-approval>

## Code Map

- `internal/store/migrations/0008_usage.sql` -- new; header comment in the style of 0005/0007 (one-way door: schema-7 builds refuse it).
- `internal/store/usage.go` -- new: `UsageRow`, `AppendUsage`, `UsageTotals`, `UsageBy` (`profile`, `model`, `agent`), `UsageBucket` (`Start`, `Profile`, `Model`, `Agent core.AgentKey`, `AgentSince`, five sums with `CacheWrite` = 5m+1h, `Responses`), `UsageHourly`, `ErrTooManyBuckets`. Follow `acknowledgements.go`: prepared statement in a tx, `toInt64("epoch", …)`, errors `store: …: %w`.
- `internal/store/sweep.go` -- add `Present.AgentSince`, `SweepResult.Usage`, a `sweepUsage` statement over the same `json_each` present set (add `"as"` to `presentRow`): delete by `responded_at < cutoff` except rows whose key matches a present entry and (`as` = 0 or `agent_since = as`). Run it in the existing transaction after acknowledgements.
- `internal/store/acknowledgements_test.go` -- reference for upgrade tests (`TestUpgradeFromSchema6`, `…RollsBack`) and the trigger rollback pattern (`CREATE TRIGGER boom`); `store_test.go`/others may assert schema 7 as highest: update to 8.
- `internal/engine/retention.go:69-89` -- add `"usage", r.Usage` to the sweep log; leave `AgentSince` unset.
- `callsite_test.go:66` -- add `"AppendUsage": {"records usage in the record", recordAcknowledgers}` (engine and store only), with its badtree case under `testdata/badtree/`.
- `docs/install.md:433-439` -- schema 8 row: Epic 8, token usage (ADR-0017); 0.10.x and earlier refuse it.
- `CLAUDE.md` "Repository state" -- seven migrations → eight, 0008 described; 0.10.x refuses schema 8.
- `docs/planning/epics.md` Story 8.4 -- add AC: the engine passes each present Agent's `agent_since` to `Sweep` (deferred from 8.3).
- `docs/implementation/sprint-status.yaml` -- 8-2 → done (PR #76); 8-3 → in-progress/review/done as the workflow moves.

## Tasks & Acceptance

**Execution:**
- [x] `internal/store/migrations/0008_usage.sql` -- create table, CHECKs, UNIQUE, three indexes.
- [x] `internal/store/usage.go` -- types and the three store methods.
- [x] `internal/store/sweep.go` -- usage sweep in the transaction, `Present.AgentSince`, `SweepResult.Usage`.
- [x] `internal/store/usage_test.go` -- every I/O matrix row; rollback forced after rows are written; reopen keeps rows and sums; upgrade from 7 and its rollback.
- [x] `internal/store/sweep_test.go` -- the four sweep rows of the matrix.
- [x] `internal/engine/retention.go` -- log the usage count.
- [x] `callsite_test.go` + `testdata/badtree/` -- `AppendUsage` is engine/store only.
- [x] `docs/install.md`, `CLAUDE.md`, `docs/planning/epics.md`, `sprint-status.yaml` -- as in the Code Map.

**Acceptance Criteria:**
- Given the epics' Story 8.3 criteria, when the story is done, then each holds and is tested.
- Given `go build ./... && go vet ./... && go test -race ./... && gofmt -l .`, when run, then all pass and gofmt prints nothing.

## Implementation Notes

- Checkpoint 1 pre-approved under the operator's standing "proceed autonomously" rule; spec kept whole at about 1,700 tokens (one goal: the table and its store API).

## Spec Change Log

## Review Triage Log

| # | Layer | Finding | Verdict | Evidence / route |
|---|---|---|---|---|
| 1 | edge, blind | One row breaking a CHECK (AgentSince 0, empty id, epoch 0) fails the whole batch | medium | Real: all-or-nothing like AppendAcknowledgements. Patch: AppendUsage doc states the contract; Story 8.4 AC added that the engine writes only rows with a known agent_since. |
| 2 | edge | Present treats AgentSince 0 as unknown while the table refuses agent_since 0 | medium | Same root cause as 1; grouped. |
| 3 | edge | A pre-1970 transcript timestamp fails the CHECK | low | Rejected: Claude Code timestamps are current; the fix adds a guard for an unseen input. |
| 4 | edge | limit = MaxInt overflows limit+1 | low | Rejected: the only caller (8.5) passes 50,000. |
| 5 | edge, blind | from >= to returns an empty list, not an error | false | An empty range has no buckets; Story 8.5 validates `from < to` before calling. |
| 6 | edge, blind | Spec names testdata/badtree/; the diff uses testdata/callsite/ | low | Rejected: the fix edits this spec; the code is right (callsite_test.go reads testdata/callsite/). |
| 7 | blind | A history pass may re-insert a swept Response under the present Agent, inflating its totals | medium | Real when retention_days is below the 30-day transcript life and a session is continued. Not caused here (no writer yet): Story 8.6 AC added to skip Responses before the cutoff; Design Note's "bounded" claim is superseded by that AC. |
| 8 | blind | No sweep test with Since non-zero and AgentSince 0 | medium | Patch: test added. |
| 9 | blind | observed_at has no CHECK | false | transitions and acknowledgements have none either; consistent. |
| 10 | blind | Schema-7 rollback test checks only acknowledgements | low | Patch: compares the record tables too. |
| 11 | blind | Upgrade test hard-codes 8 | low | Patch: uses schemaVersion. |
| 12 | blind | No hourly test of two Agent instances of one key in one hour | medium | Patch: test added. |
| 13 | blind | Spec status in-review vs sprint in-progress | false | The workflow moves sprint status at step 5. |
| 14 | blind | Operator docs say nothing of usage under Retention | low | Patch: config.example.toml; how-herddash-works.md is updated with Story 8.5 when usage is shown. |
| 15 | blind | Interim AD-4 gap recorded only in epics.md | low | Rejected: the Story 8.4 AC is the owner per the retro rule. |
| 16 | blind | toInt64 error unwrapped in AppendUsage | false | Same pattern as AppendAcknowledgements and Sweep. |
| 17 | blind | Sweep doc and CLAUDE.md wrapping | low | Patch: rewrapped. |
| 18 | verification-gap | session_id, subagent_id, stopped, observed_at never read back | medium | Pre-verified. Patch: TestAppendUsageRoundTrip. |

## Design Notes

The sweep keys usage by `responded_at`, the Response's own time, not `observed_at`: the history pass (8.6) writes old Responses with a fresh `observed_at`, and age must mean the Response's age. The present Agent is AgentKey plus `agent_since` because one pane holds successive Agents (AD-3); an earlier Agent's old rows go by age like a gone Agent's. With `retention_days` under Claude Code's 30-day transcript life, Story 8.6's history pass may re-insert a swept Response after a restart and the next sweep removes it again; that is bounded and accepted, not a double count.

## Verification

**Commands:**
- `go build ./... && go vet ./... && gofmt -l .` -- clean
- `go test -race ./...` -- pass
- `go test -race -count=50 ./internal/store/ -run 'Usage|Sweep'` -- pass
