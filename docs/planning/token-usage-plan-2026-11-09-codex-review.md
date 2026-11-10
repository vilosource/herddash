# Codex review of the token usage plan, 2026-11-09

Reviewer: Codex CLI 0.163.0 through `codexw exec -s read-only`, run on 2026-11-09 against commit
`aee8354` (the first version of `token-usage-plan-2026-11-09.md`). Its prompt named the plan, its
research, CLAUDE.md, the PRD, the ADRs, the spine and the code, and forbade edits, herdr commands
and reads outside this repository. Kept verbatim below; the plan's "Review response" section says
what each finding changed.

---

1. **§3.3 — the unique key cannot identify an Agent instance.** `UNIQUE (message_id, request_id)` collides across panes, profiles, sessions, epochs, and subagents. It also cannot distinguish successive Agents reusing the same `AgentKey`, which the architecture explicitly permits.

   Evidence: plan lines 89–108; AD-3 permits successive Agents on one key and says epoch 0 is reserved for backfill ([ARCHITECTURE-SPINE.md:187-216](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:187)); `AgentKey` is the full identity, not pane ID alone ([ARCHITECTURE-SPINE.md:121-134](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:121)).

   Suggested change: define a durable Agent-instance identity/generation and include Host, Server, Epoch, PaneID, session ID, subagent ID, and the transcript response key in the uniqueness rule. Explicitly test pane reuse, epoch changes, `/clear`, backfill epoch 0, and subagent/main collisions.

2. **§3.1/§3.3 — immutable incomplete rows contradict append-only storage.** The plan records a response with `complete = false`, then uses `INSERT OR IGNORE`. If a later read finds the completed record or larger token totals, the later value is ignored forever. This loses or undercounts completed responses.

   Evidence: plan lines 59–64 and 101–109; ADR-0005 requires append-only rows ([0005-append-only-transition-record.md:22-47](docs/adr/0005-append-only-transition-record.md:22)); the research says the last record holds the final value, not that an early incomplete value is final ([token-usage-sources-2026-11-09.md:32-41](docs/research/token-usage-sources-2026-11-09.md:32)).

   Suggested change: either persist only once a response is known complete, or design an append-only lifecycle with separate immutable observations and a deterministic fold. Do not use an immutable `complete=false` row that later requires an update.

3. **§3.1 — the completion rule is stronger than the evidence supports and may count the wrong record.** The research supports “last record per response key,” while the plan changes this to “first record whose `stop_reason` is set.” The cited research files do not establish that this rule is stable across Claude Code versions or all content-block shapes.

   Evidence: plan lines 54–64; source evidence says “the last record holds the final value” ([token-usage-sources-2026-11-09.md:32-36](docs/research/token-usage-sources-2026-11-09.md:32)); transcript format is documented as internal and changeable ([research.md:24-37](docs/planning/research/technical-token-usage-tracking-for-herddash-2026-11-09/research.md:24)).

   Suggested change: make “last observed record for a response key after a proven response boundary” the normative rule, or add fixture evidence and a precise state machine proving why the first stop record is final. Define how later records, missing stops, malformed usage, and changed models are handled.

4. **§3.2/§4 — the plan explicitly permits data loss while FR-18 promises every response.** Subagent files may be capped with “oldest dropped,” and full-history recovery is optional Story 8.7. Both contradict “every completed model response.”

   Evidence: plan lines 76–81 and 143; FR-18 at lines 39–43; the research confirms subagent usage is in separate files ([token-usage-sources-2026-11-09.md:37-41](docs/research/token-usage-sources-2026-11-09.md:37)).

   Suggested change: either weaken FR-18 to a documented best-effort guarantee or make discovery, tail persistence, file caps, and historical backfill mandatory. Define behavior when the cap or read budget is exceeded, including an operator-visible gap marker.

5. **§3.2 — `/clear`, continued sessions, and transcript-path changes are underspecified.** Existing code treats a changed session ID as a new transcript and drops old in-memory transcript state. The usage ledger needs explicit boundaries: continued sessions across files should deduplicate, `/clear` should not merge unrelated response IDs, and a new Agent/session must not inherit pending state.

   Evidence: `readTranscript` comments that `/clear` names a new session and drops the old transcript state ([internal/engine/engine.go:2544-2553](internal/engine/engine.go:2544)); `TailTranscript` restarts when a path or inode changes ([internal/procenv/transcript.go:100-113](internal/procenv/transcript.go:100)); plan lines 107–109 claim the schema handles these cases without defining the boundary identity.

   Suggested change: specify a transcript-source identity and ledger reset/merge rules for `/clear`, continued files, inode replacement, and session-record changes. Add tests for each transition, including responses split across files.

6. **§3.2/§4 — Profile attribution can race the existing Profile hold rules.** The plan says usage is stamped with the Profile “at the time it is recorded,” but AD-14 holds rows while Profile resolution is pending and releases them later. Recording immediately can associate usage with the previous or wrong Profile.

   Evidence: plan lines 83–84; AD-14 requires held rows to wait for Profile resolution and forbids pairing a program with a Profile from another program ([ARCHITECTURE-SPINE.md:395-420](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:395)).

   Suggested change: state that usage observations participate in the same Profile hold/release mechanism as transitions, and specify the fallback when the hold deadline expires. Test program changes, reconnects, and Profile changes while responses are pending.

7. **§3.3/§4 — retention is not designed for usage totals.** Saying “Retention sweeps by `responded_at`” is insufficient. Existing sweep logic preserves rows needed by the present Agent’s current class, including special handling for successive Agents. If old usage rows are swept, current-Agent totals silently decrease; if rows are retained by `AgentKey` alone, a later Agent inherits them.

   Evidence: plan lines 107–110; existing sweep preserves rows based on the present Agent and its class start ([internal/store/sweep.go:11-19](internal/store/sweep.go:38-62)); ADR-0005 defines the present-Agent retention invariant ([0005-append-only-transition-record.md:24-32](docs/adr/0005-append-only-transition-record.md:24)).

   Suggested change: define usage retention in terms of the durable Agent-instance boundary, include usage in the same transaction and present-set rules, add usage counts to `SweepResult`, and test current, replaced, absent, and epoch-0 Agents.

8. **§3.3 — backfill epoch 0 is not addressed.** The plan claims deduplication across all reads but does not say whether imported/backfilled usage exists, how it is keyed, or how it avoids suppressing a live row. The proposed unique key omits epoch, making this especially unsafe.

   Evidence: AD-3 reserves epoch 0 for backfill and says it never joins a live Agent ([ARCHITECTURE-SPINE.md:131-134]); AD-13 requires importer rows to use epoch 0 and the shared constructor/insert path ([ARCHITECTURE-SPINE.md:375-391]); plan lines 103–109 omit epoch from uniqueness.

   Suggested change: explicitly choose one: usage is not backfilled, or the importer has a defined usage import with epoch-0 identity and separate deduplication. Add an acceptance test proving epoch-0 rows cannot collide with live rows.

9. **§3.2 — the full-history pass threatens engine responsiveness and has no operational design.** Current transcript reads run off the actor, have a timeout, and abandoned reads may continue because the underlying reads cannot be cancelled. A whole-file pass over 1.3 GB of transcripts needs concurrency, scheduling, cancellation, memory, and status guarantees.

   Evidence: `readSession` uses a timeout while the underlying read continues and is abandoned ([internal/engine/engine.go:2495-2541](internal/engine/engine.go:2495)); the plan proposes whole-file reads but only says “rate-limited” ([token-usage-plan-2026-11-09.md:79-82](docs/planning/token-usage-plan-2026-11-09.md:79)).

   Suggested change: specify bounded worker concurrency, per-file and total budgets, interaction with the existing busy flag, failure recovery, and an operator-visible incomplete state. Acceptance must prove status updates are not delayed beyond the project’s existing timeout guarantees.

10. **§3.4 — event coalescing may violate the existing event-stream contract.** AD-7 publishes each view with the change list that produced it and requires clients to observe contiguous view sequences. “At most one usage change per Agent per 5 s” is safe only if intermediate changes are merged into a later complete view without dropping other changes or producing unusable sequence gaps.

    Evidence: plan lines 114–119; AD-7 requires publication in change order and reconnect on skipped sequence numbers ([ARCHITECTURE-SPINE.md:269-291](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:269)).

    Suggested change: define coalescing at the actor publication boundary, preserving all non-usage changes and one final immutable view per sequence. Add a contract test with usage, status, removal, and reconnect events interleaved.

11. **§0/§3.4/§3.6 — the required ADR and architecture amendments are incomplete.** The plan names ADR-0001, 0003, 0005, and 0009, but the change also affects the loopback route contract, configuration rules, migration rules, event payloads, and the architecture capability/import documentation.

    Evidence: AD-6 enumerates the allowed routes and methods ([ARCHITECTURE-SPINE.md:249-267](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:249)); AD-8 governs TOML configuration ([ARCHITECTURE-SPINE.md:293-303](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:293)); AD-10 governs numbered migrations and one-way compatibility ([ARCHITECTURE-SPINE.md:315-330](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:315)); the import graph is enforced by `arch_test.go:22-35`.

    Suggested change: require amendments to ADR-0004/0007 and the relevant AD-6/AD-7/AD-8/AD-10 capability-map entries, or explicitly justify why each is unchanged. Define `/usage` routing, CSP, response schemas, query validation, indexes, and migration compatibility.

12. **§2/§4 — the PRD amendment is incomplete.** Adding FR-18 and FR-19 is not enough: §5, §6 scope, success metrics, terminology, API requirements, and likely §8 open questions must be updated consistently. The plan also leaves “on by default,” interrupted responses, and history start unresolved while presenting FR-18 as unconditional.

    Evidence: current PRD places the usage-dashboard prohibition in §5 ([docs/prd.md:646-664](docs/prd.md:646)); MVP scope enumerates FR-1 through FR-17 ([docs/prd.md:670-678](docs/prd.md:670)); plan lines 156–164 leave material requirements open.

    Suggested change: make Story 8.1 acceptance require a complete PRD diff, including explicit guarantees, defaults, exclusions, operator-visible gaps, and testable semantics—not merely acceptance of the ADR title and two FR numbers.

13. **§3.4/§3.5 — the usage page has no testable navigation or API contract.** A page at `/usage` “linked from nowhere” is not an implementable user story. The plan does not define the HTML route, links, empty/error states, time-window boundaries, timezone/day semantics, pagination or limits, or how `by=agent` identifies a successive Agent.

   Evidence: plan lines 118–127; the current Board exposes only `/`, assets, state, history, and events (AD-6); current HTML has only the Board entry point ([internal/httpapi/web/index.html:7-19](internal/httpapi/web/index.html:7)).

   Suggested change: specify route registration, navigation decision, API JSON schema, UTC/day semantics, bounded query parameters, empty/error behavior, and browser acceptance tests. If the page is intentionally inaccessible initially, defer its implementation rather than calling it complete.

14. **§3.1 — malformed and missing usage records are not safe enough.** “Records with no `requestId` key on `message.id` alone” can merge separate requests if Claude Code reuses message IDs, and “zero-usage records are skipped” needs a distinction between synthetic records and a legitimate zero-valued response.

   Evidence: plan lines 63–68; prior art documents failures when `requestId` is absent ([research.md:101-105](docs/planning/research/technical-token-usage-tracking-for-herddash-2026-11-09/research.md:101)); the source record only establishes that 111 records lack `requestId`, not that `message.id` alone is globally unique ([token-usage-sources-2026-11-09.md:32-36](docs/research/token-usage-sources-2026-11-09.md:32)).

   Suggested change: define a fallback key with transcript/session/source identity, document whether missing-key records are counted, quarantined, or marked uncertain, and test repeated message IDs without request IDs.

What the plan gets right:

- It correctly recognizes that the current PRD explicitly excludes usage tracking and that an operator decision plus ADR/PRD work must precede implementation.
- It chooses the existing transcript source, preserving the project’s deliberately unmanaged/read-only stance.
- It identifies the central 2× counting hazard and the need to include subagent files.
- It keeps parsing in `internal/core`, transcript access in `internal/procenv`, orchestration in `internal/engine`, persistence in `internal/store`, and serving in `internal/httpapi`, which is broadly compatible with the existing import graph.
- It preserves the Board design gate and proposes replay tests against independent counts.
- It remembers migration documentation, rollback testing, restart testing, and race testing.

Overall verdict: **needs rework**. The source choice and high-level direction are promising, but identity, immutable storage semantics, retention, transcript lifecycle, and the “every response” guarantee need to be resolved before this can become an ADR and Epic.