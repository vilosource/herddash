# Token usage: implementation plan

**Status: proposal, revision 2, 2026-11-09.** The operator decided on 2026-11-09 to reverse PRD
§5's non-goal ("Not an orchestrator, scheduler, cost tracker or usage dashboard") so herddash can
track token usage. Revision 1 was reviewed by Codex the same day
([review](token-usage-plan-2026-11-09-codex-review.md)), which found 14 problems; §8 says what
each changed. Evidence: [the research record](../research/token-usage-sources-2026-11-09.md) and
its run folder. Once the open questions in §9 are answered, this becomes ADR-0017, a PRD
amendment, spine amendments and Epic 8, built story by story with `/bmad-build`.

## 1. Scope

**In:** tokens for each model response of a Claude Code Agent, read from its main transcript and
its subagents' transcripts. Each response is recorded once, with its Agent and Profile. Each
Agent's totals appear in its view, totals per Profile, model and hour come from a read-only API,
and a usage page shows them.

**Out, each a later decision:**
- **Dollar costs.** Every figure would be a list-price estimate, and subscription plans are not
  billed per token.
- **The plan-limit gauge** from `.claude.json`. It is undocumented, and when Claude Code refreshes
  it is unknown.
- **Codex.** Its Profile rules are open questions in PRD §8.
- **A statusline hook or OTLP receiver.** Both would configure every Claude Code.
- **Importing transcripts of sessions no live Agent owns.**
- **Usage on other hosts.**

## 2. Decisions to record (story 8.1)

- **ADR-0017, "Token usage per Agent and per Profile".** It amends:
  - **ADR-0001:** what herddash records.
  - **ADR-0003:** what the Board may show; the Board itself gains nothing without §6's gate.
  - **ADR-0005:** a second append-only table and its sweep rule.
  - **ADR-0007:** new GET routes.
  - **ADR-0009:** more read from transcripts, including subagent files.
  - **ADR-0013:** only if the title bar gains a link (§9 Q4).

  It leaves ADR-0004 unchanged: no new dependency.
- **Spine amendments:**
  - **AD-4:** the usage table is append-only, with its sweep.
  - **AD-6:** the routes `/usage` and `/api/usage`.
  - **AD-7:** `AgentView.usage` rides on `agent` events, with no new event name.
  - **AD-8:** the `claude_usage` key.
  - **AD-10:** migration 0008.
  - **AD-14:** usage rows join the Profile hold.
  - **The Capability → Architecture map:** FR-18 and FR-19.

  The import graph is unchanged: `core` decodes, `procenv` reads, `engine` decides, `store` keeps
  and `httpapi` serves.
- **PRD amendment, as one diff:**
  - **§3 Glossary:** Response, Usage.
  - **§4.6:** FR-18 and FR-19 (§3 below).
  - **§5:** the non-goal narrowed to "not an orchestrator, scheduler, billing or cost tool".
  - **§6:** scope.
  - **§7:** SM-C1 amended, and a usage counter-metric added.
  - **§8:** a new question closed.

  Story 8.1's acceptance is that whole diff, accepted by the operator.

## 3. Requirements

- **FR-18: Record token usage.**
  - **What is recorded:** each completed Response of a Claude Code Agent whose transcript herddash
    reads, recorded once with:
    - its AgentKey and the Agent's start (§4.4);
    - its Profile, session id and subagent id;
    - its model and response time;
    - its input, output, cache-write (5 min, 1 h) and cache-read tokens.
  - **Completed** means its first record with a stop reason, or, with none, superseded by a later
    Response in the same file.
  - **The only Responses not recorded** are a file's last Response while it has no stop reason,
    and any Response in a file the Agent no longer reads before that happens. Measured on
    2026-11-09: 32 of 82,035 Responses (0.04%).
  - **History:** each newly seen transcript's earlier Responses are recorded by a separate pass
    (§4.3). A file past that pass's size cap marks the Agent's usage `partial`.
  - **Restarts and Retention:** recording survives a Daemon restart, and Retention sweeps usage by
    age except the present Agent's (§4.4).
- **FR-19: Show token usage.**
  - **The view:** `AgentView.usage` carries the present Agent's totals, its `partial` flag and its
    last Response time.
  - **The API:** `GET /api/usage` returns hourly totals by Profile, model or Agent over a bounded
    window.
  - **The page:** `/usage` draws them.
  - **The Board:** it gains nothing without §6's design gate.

## 4. Design

### 4.1 Counting rule (`internal/core`)

Measured on 2026-11-09 over every transcript in the four config dirs, 82,035 Responses:

- **A Response is the records sharing `message.id` and `requestId`.**
  - Every non-synthetic record has both.
  - No `message.id` has two `requestId`s.
  - The pair is the key. A record with no `requestId` is not counted, and is counted in a debug
    log.
- **From its first record with `message.stop_reason` set onward, every field is final.** This held
  for all 81,358 Responses that have a stop reason:
  - every field is equal on every later record, and that record has the largest `output_tokens`;
  - input, cache and model fields are equal on the earlier records too.
- **Records of two Responses never interleave within a file.** So a Response with no stop reason
  is over once a record of another Response follows. 645 such Responses, all interrupted, are
  recorded then, at their largest values.
- **What is skipped:** `<synthetic>` records, and records whose usage is missing or not an object.
  A Response with every count zero is still recorded.

**Code changes:**
- **`core.DecodeTranscriptLine`** gains a usage marker carrying only:
  - the two ids;
  - the model;
  - whether a stop reason is set;
  - the token fields, with the cache split;
  - the record's timestamp;
  - `agentId` in a subagent file.

  Message text is never kept (ADR-0009).
- **`core.UsageLedger`, new and pure**, folds one file's markers in order and returns the
  Responses that became complete. It holds at most one pending Response per file, the current one,
  because records do not interleave. It has no clock.
- **An unknown `usage` shape fails a test.** It never drops a Response silently: the decoder
  reports it, and the engine counts it.

### 4.2 Reading the live tail (`internal/procenv`, `internal/engine`)

- **Main transcript:** usage rides on the existing tail in `readTranscript`, which runs every 2 s
  within the 1 s session timeout.
- **Subagent transcripts:**
  - Files are at `<project dir>/<sessionId>/subagents/agent-*.jsonl`, up to 394 per session here.
  - Each read lists the directory and tails only files whose size changed since the last read,
    each with its own `procenv.Tail` and ledger.
  - A subagent file is never dropped while its session is read.
  - If listing and tailing exceed the session timeout, the read stops at the timeout with the
    files' offsets kept, and resumes there next time.
- **A path change** (`/clear`, a new session record, a new inode) ends that file's ledger, and its
  pending Response is dropped, which is the loss FR-18 names. The new file starts a fresh ledger.
  - **A session continued into a new file** repeats earlier Responses. The store's unique key
    ignores them, and they keep the attribution of the Agent that first recorded them.

### 4.3 The history pass (`internal/engine`, `internal/procenv`)

- **Why it exists:** a first read starts at the last 8 MiB. The largest transcript here is 42 MB,
  and 21 exceed 8 MiB.
- **Scheduling:**
  - The first time the engine sees a transcript path (main or subagent), it queues that file for a
    usage-only read from byte 0 up to the live tail's start offset.
  - One worker, off the actor, reads one file at a time, with a per-file size cap (default 256
    MiB) and a pause between files.
  - It never takes the session read's busy flag, so status reads are never delayed.
  - It hands complete Responses to the actor in batches. The actor writes them, and the unique key
    makes overlap with the live tail harmless.
- **Failure:** a cap hit or a read failure marks the Agent's usage `partial` in its view, and logs
  it once.
- **Restarts:** after a restart, every transcript is new to the engine again. The pass re-reads
  it, and the store ignores every Response already recorded, so only the read is repeated. That
  cost is measured in story 8.6 before it ships.

### 4.4 Identity, storing and Retention (`internal/store`, migration 0008)

```sql
CREATE TABLE usage (
    id             INTEGER PRIMARY KEY,                       -- recording order
    host TEXT NOT NULL, server TEXT NOT NULL,
    epoch          INTEGER NOT NULL CHECK (epoch >= 1),       -- never backfill's epoch 0
    pane_id        TEXT    NOT NULL,
    agent_since    INTEGER NOT NULL,   -- observed_at of the Agent's first Transition (its start)
    profile        TEXT    NOT NULL,
    session_id     TEXT    NOT NULL,
    subagent_id    TEXT    NOT NULL DEFAULT '',
    message_id     TEXT    NOT NULL,
    request_id     TEXT    NOT NULL,
    model          TEXT    NOT NULL,
    responded_at   INTEGER NOT NULL,   -- the record's timestamp, UTC ms
    input          INTEGER NOT NULL, output INTEGER NOT NULL,
    cache_write_5m INTEGER NOT NULL, cache_write_1h INTEGER NOT NULL, cache_read INTEGER NOT NULL,
    stopped        INTEGER NOT NULL CHECK (stopped IN (0, 1)), -- 0: superseded with no stop reason
    observed_at    INTEGER NOT NULL,
    UNIQUE (message_id, request_id)
);
CREATE INDEX usage_agent ON usage (host, server, epoch, pane_id, agent_since);
CREATE INDEX usage_responded_at ON usage (responded_at);
CREATE INDEX usage_profile ON usage (profile, responded_at);
```

- **Identity.**
  - An Agent instance is its AgentKey plus `agent_since`, the `observed_at` of the key's last
    Transition with no previous Status: the same boundary AD-3 and the sweep use.
  - A new Agent in a reused pane gets a new `agent_since`, so it never inherits usage. An epoch
    change is a new AgentKey.
  - The unique key is the Response's own global identity. It deduplicates a Response seen twice,
    whether in a continued session, after a restart, by the history pass or by a subagent replay.
    It never merges two Responses: the pair was unique across every transcript here.
- **Append-only.** Rows are inserted once, complete, and never updated (`INSERT OR IGNORE`).
  Nothing is written while a Response is pending.
- **Profile hold (AD-14).** Usage rows queue with the pane's Transitions. While the pane's rows are
  held for a Profile lookup, its usage rows are held too, and they are released with the Profile
  decided at release. A row never pairs a Response with a Profile resolved for another program.
- **Retention.** The sweep deletes usage rows older than the cutoff, except the present Agent's
  rows (its AgentKey and `agent_since`). That rule mirrors acknowledgements, and keeps present
  totals from ever shrinking. It runs in the sweep's transaction, and `SweepResult` gains a usage
  count.
- **Backfill.** None. The plugin-log importer is untouched, and epoch 0 never appears in `usage`,
  which the CHECK enforces.
- **Schema door.** Schema 8 is refused by 0.10.x. `docs/install.md`'s table and backup steps gain
  a row.

### 4.5 Serving (`internal/httpapi`, `internal/engine`)

- **`AgentView.usage`, always sent.** It carries `{input, output, cache_write, cache_read,
  responses, last_response_at, partial}` for the present Agent.
  - At seed and after a restart it is summed from the record. The engine then adds each written
    batch.
- **Live changes.** The view changes at most once per transcript read (every 2 s) or per history
  batch. It is published as that Agent's `agent` event in the usual change list: no throttle, no
  new event name, so AD-7 is untouched. A contract test interleaves usage with status, removal and
  a reconnect.
- **`GET /api/usage`** (and HEAD).
  - **Parameters:**
    - `from` and `to` in UTC ms: required, at most 31 days apart, `from < to`;
    - `by=profile|model|agent`;
    - `bucket=hour`.
  - **Response:** `{"from", "to", "by", "buckets": [{"start", "key", "input", "output",
    "cache_write", "cache_read", "responses"}]}`.
  - **Rules:** UTC hour buckets only; the page groups them into local days. `by=agent` keys are
    `{host, server, epoch, pane_id, agent_since}`. At most 50,000 buckets per response, or 400
    `range_too_large`. Malformed parameters get 400 with the JSON error shape.
  - It is built from a store read under the same GET guards (loopback `Host`, no CORS).
- **`GET /usage`** serves the embedded page.

### 4.6 The page, and the Board

- **The page** at `/usage` shows today, 7 days and 30 days per Profile and per model, drawn from
  `/api/usage`.
  - **Empty state:** "No usage recorded yet".
  - **API failure:** an error line.
  - It is checked in `board-browser.mjs`.
- **How it is reached** is §9 Q4.
- **The Board's Boxes change only through a design gate** rendered with the operator's live data
  (Epic 1-4 retrospective). Totals are dominated by cache reads (billions here), so which number a
  Box would show is the gate's question.

### 4.7 Configuration

`claude_usage` lives in the TOML file with no flag, like `claude_recaps`. Its default is §9 Q2.
When it is false, nothing is decoded, read or recorded for usage, and `AgentView.usage` is
`null`.

## 5. Stories (Epic 8, draft)

| # | Story | Done when |
|---|---|---|
| 8.1 | ADR-0017, the spine amendments and the PRD diff | The operator accepts all three. **Checkpoint.** |
| 8.2 | Decode usage and fold Responses (`core`) | Fixtures from real shapes: multi-record, stop before last, no stop then superseded, no stop at end of file, synthetic, all-zero, unknown shape; no clock |
| 8.3 | The usage table (migration 0008) | Idempotent insert; sweep keeps the present Agent's rows; rollback test failing after rows are written; epoch 0 refused; schema door documented |
| 8.4 | Record live usage, main and subagent files (`engine`, `procenv`) | Profile hold covers usage; recorded once across two reads, a restart, a continued session, `/clear` and a reused pane; `-race -count=50`; a replay of every local transcript equals the independent count within the documented loss |
| 8.5 | Serve usage (`httpapi`) | `AgentView.usage` from the record after a restart; `/api/usage` contract and limits; event-stream contract test through `order.mjs` |
| 8.6 | The history pass | Measured on this Mac (time, memory) before merge; status reads never wait on it; `partial` shown on cap or failure |
| 8.7 | The usage page | Rendered with live data and approved. **Checkpoint.** `board-browser` checks it |
| 8.8 | The Board decision | Only if the operator wants a Box figure: design gate with live data. **Checkpoint.** |

The order is 8.1 to 8.8, with one pull request per story into `develop`.

## 6. Tests that prove the counting

- **Golden replay (8.4).** A test helper, not shipped, replays every local transcript through the
  decoder and ledger. It compares the result with an independent Python count, last record per key
  across files. The two must agree except for the documented end-of-file loss. It runs by hand
  before each usage story merges, and its numbers go into the story's spec.
- **Restart (8.4, 8.5).** Record, stop the Daemon, append Responses, start it again. Totals must
  equal one uninterrupted run, and no row is duplicated.
- **Successive Agents (8.4).** A new Agent in a reused pane starts at zero, and the old Agent's
  rows keep their `agent_since`.

## 7. Risks

- **Format drift.** Anthropic calls the transcript format internal. Fixtures per observed shape,
  the unknown-shape test and the debug count make drift visible rather than silent.
- **Wrong totals look right.** §6's golden replay guards against that.
- **Read cost.** Up to 394 subagent files per session, and a 42 MB transcript. The live tail stats
  before reading, and the history pass is one worker with caps. Story 8.6 measures both.
- **Board creep.** The page comes first, and the Board changes only through 8.8's gate. SM-C1 is
  amended to say so.

## 8. Review response

| # | Codex finding | Change |
|---|---|---|
| 1 | Unique key cannot identify an Agent instance | Agent instance = AgentKey + `agent_since`; unique key stays the Response's global identity, measured unique (§4.4) |
| 2 | Immutable `complete=false` rows contradict append-only | Only complete Responses are written; nothing is written while pending (§4.1, §4.4) |
| 3 | Completion rule stronger than evidence | Re-measured across all four config dirs: 81,358 of 81,358; stated as measured, with fixtures (§4.1) |
| 4 | "Every response" contradicted by caps | FR-18 states exactly what is not recorded (0.04%); no subagent file is dropped; the history pass is mandatory with a visible `partial` (§3, §4.2, §4.3) |
| 5 | `/clear` and continued sessions underspecified | Per-file ledger; a path change drops its pending Response; continued files deduplicate by key (§4.2) |
| 6 | Profile hold race | Usage rows join the AD-14 hold (§4.4) |
| 7 | Retention breaks present totals | Present Agent's rows are never swept; same transaction; `SweepResult` count (§4.4) |
| 8 | Epoch 0 not addressed | No usage backfill; CHECK `epoch >= 1` (§4.4) |
| 9 | History pass has no operational design | One worker, caps, never the busy flag, batches to the actor, measured in 8.6 (§4.3) |
| 10 | Coalescing vs AD-7 | No throttle: one change per read or batch, ordinary `agent` events, contract test (§4.5) |
| 11 | ADR and spine amendments incomplete | Listed per ADR and AD rule, ADR-0004 unchanged and why (§2) |
| 12 | PRD amendment incomplete | One diff across §3 to §8, accepted at 8.1 (§2) |
| 13 | Page has no contract | Route, API schema, limits, UTC buckets, empty and error states, browser check (§4.5, §4.6) |
| 14 | Missing-key and zero records | No `requestId` is not counted (none seen); all-zero Responses are recorded; unknown shapes fail a test (§4.1) |

## 9. Open questions for the operator

1. **The non-goal.** Decided 2026-11-09: reverse it.
2. **Default.** Should `claude_usage` be on by default (numbers only, no text), or opt-in like
   recaps? Recommendation: on.
3. **The history pass.** Keep it (recommended), or record from the first read onward only?
4. **Reaching `/usage`.** A link in the title bar (amends ADR-0013), a link at the Board's foot, or
   no link (reached by its URL)? Recommendation: the title bar, decided at 8.7.
5. **The Board.** Should a Box ever show a usage figure? Recommendation: decide after living with
   the page (8.8).
