---
title: 'Story 5.10: A new Agent in a reused pane starts fresh'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: 'abb4666069cd16a72f26dabc4051d3e14222346c'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Two Agents that share an AgentKey (same host, server, epoch and pane id) fold together after a Daemon restart (Epic 2 retro F-1, Epic 1 retro #19). `core.Fold`/`FoldResult.Add` never start over, so a new Claude session in a pane whose previous Agent finished is rebuilt from the record as Finished with the old Agent's wait. The same happens when the pane id comes back on a new `terminal_id` while the Daemon was down: `apply` seeds the first sight from the old Agent's history because nothing compares the saved terminal. Retention keeps the key's latest `working` row even when it belongs to the earlier Agent, so `EverWorking` leaks across.

**Approach:** Decided (vfkb aa3dcbf096bc), not reopened: AgentKey and the stored schema stay. A Transition with no previous Status is an Agent's first row; the fold starts over there. The first snapshot of a connection does not seed a pane from the record when that pane's saved `terminal_id` differs from the one now shown, so its first sight is written as such a row. Retention keeps only the current Agent's latest working row. AD-3 (and AD-4's Retention sentence) document that one AgentKey can hold successive Agents separated by such a row.

## Boundaries & Constraints

**Always:** `internal/core` stays stdlib-only and clock-free. The engine's in-memory fold and the fold rebuilt from `Store.History` agree (both go through `FoldResult.Add`). A row with no previous Status is written only as an Agent's first row: `transitionAt` uses the fold's last Status as previous when `p.status` is not valid but the fold is known. The engine fake store (`internal/engine/fake_test.go`) applies the same Sweep rule as the real store. Backfill (epoch 0) behaviour is unchanged.

**Never:** No stored-schema change, migration, new marker or row type, AgentKey change or ADR. Do not touch the live herdr or service. Do not edit AD-3's stale "Nothing persists the evidence yet" bullet (Story 5.8 owns it). Do not edit `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Fold reset | rows: `->working`, `working->done`, `->idle` | Class working (never worked), Since = the `->idle` row, EverWorking false | none |
| Re-detected then restart | A working→done live; A released; B detected idle; restart with epoch kept, snapshot idle | B Working since its first row; nothing written | none |
| Terminal changed while down | saved terminals `w1:p1=t1`; restart snapshot shows `w1:p1` on `other`, same status as last row | not seeded, no history read for it; first sight row with no previous; fold from it | none |
| Saved terminal absent or empty | pane not in saved map, or either id empty | seeded as before (no evidence of a change) | none |
| Sweep, two Agents on a key | A `->working`@200d, `working->done`@199d; B `->idle`@150d, `idle->blocked`@120d; present Since=120d | only `blocked@120` remains; fold unchanged (needs_you, EverWorking false) | none |
| Sweep, current Agent worked | as above but B `idle->working`@140d before blocked | B's `working@140` and `blocked@120` remain | none |

</intent-contract>

## Code Map

- `internal/core/attention.go` -- `FoldResult.Add` (line ~68): if `t.Previous()` reports none, start from `FoldResult{Class: ClassUnknown}` before applying t. Update `Fold`/`Add` doc comments.
- `internal/core/attention_test.go` (or existing core test file for Fold) -- add reset cases.
- `internal/engine/engine.go` -- `onSnapshot` (~1143) overwrites `e.terminals` before `apply`; keep the previous map (e.g. local `prev := e.terminals` passed to apply, or a field) so `apply` (~1240-1270) sets `p.seedHistory = resubscribe && e.historic && !replaced && !changedTerminal`, where changedTerminal = previous map has a non-empty id for the pane, obs.TerminalID non-empty, and they differ. Log it at Info. `transitionAt` (~2235): previous fallback to `p.fold.Status` when `!p.status.Valid() && p.fold.Known`. `pane.seedHistory` comment (~346) and `firstSight` comment (~2267) mention the rule.
- `internal/store/sweep.go` -- `sweepTransitions`: the latest-working subquery is restricted to rows at or after the key's last row with `previous_status IS NULL` in History order `(observed_at, id)`; use SQLite row-value comparison. Update the const and `Sweep`/`Present` comments.
- `internal/engine/fake_test.go` -- `fakeStore.Sweep` (~971): same restriction, with History order = stable sort by ObservedAt over append index.
- `internal/store/sweep_test.go` -- helpers `appendAt`, `sweep`, `expectDays`, `folded` exist; add the two two-Agent cases.
- `internal/engine/attention_test.go` -- helpers `stored`, `storedIn`, `restartOn`, `restartedMatching`, `agentByPane`, `historyReads`, `twoAgents`, `firstP1`; add restart tests. `internal/engine/retention_test.go` -- `TestRetentionKeepsFoldAcrossRestart` pattern; add sweep-then-restart for both scenarios. Add a parity test running the same rows through `fakeStore.Sweep` and a real `store.Store` (temp dir) Sweep and comparing survivors.
- `cmd/herddash/serve_test.go` -- `startFakeHerdr` (terminals `i0-t1`/`i0-t2`, `f.blocked` makes snapshots blocked), `serveArgs`, `listenAddr`, `TestServeSweepsRetention` and `TestServeHistory` patterns; add one serve test for the history AC.
- `docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md` -- AD-3 (line ~97-147) and AD-4 Retention sentence (~155).

## Tasks & Acceptance

**Execution:**
- `internal/core/attention.go` -- fold starts over at a Transition with no previous Status -- the record's boundary between successive Agents.
- `internal/core/attention_test.go` -- tests for reset, incremental Add equal to Fold over a boundary -- pin the core rule.
- `internal/engine/engine.go` -- compare saved terminal before seeding; previous-status fallback in `transitionAt` -- second trigger, and keep "no previous" meaning first row.
- `internal/store/sweep.go` and `internal/engine/fake_test.go` -- restrict kept working row to the current Agent, identically -- stop `EverWorking` leaking after a sweep.
- `internal/store/sweep_test.go` -- the two sweep matrix rows.
- `internal/engine/attention_test.go`, `internal/engine/retention_test.go` -- restart tests for both triggers, each followed by a sweep and another restart; fake/real Sweep parity test.
- `cmd/herddash/serve_test.go` -- seed a real DB (epoch 1, saved terminals matching the fake herdr) with A's and B's rows aged past Retention; serve with `f.blocked` true; assert `/api/agents/testhost/test/1/w1%3Ap1/history` includes B's `idle->blocked` class-start row and not A's working row.
- `ARCHITECTURE-SPINE.md` -- AD-3: one AgentKey can hold successive Agents separated by a row with no previous Status; when such rows are written; the fold and Retention start over there; the undetectable case (quit and restart in the same terminal while the Daemon is down) joins. AD-4: latest working Transition of the current Agent.

**Acceptance Criteria:**
- Given an Agent quits and a new one starts in the same pane, and the Daemon restarts keeping the epoch, when Waiting Times are rebuilt, then the new Agent's Attention Class and Waiting Time come only from rows since its first row.
- Given a pane id comes back on a new terminal while the Daemon was down, when the Daemon restarts keeping the epoch, then the new Agent is not seeded from the old record and its first row has no previous Status.
- Given either scenario, when a Retention sweep runs and the Daemon restarts again, then the fold is unchanged, in the engine with the fake store and in the real store, which apply the same rule.
- Given a present Agent whose class began before the Retention cutoff, when the history API is read after a sweep, then it includes the Transition that began its current class.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- Reviewer note: the four layers (blind hunter, edge-case hunter, verification gap, intent alignment) were run by the build agent itself, in order. Subagent launches in this environment are detached, and their completion notices reach the orchestrator, not this run.
- verdicts: 7 findings — high 0, medium 0, low 3, false 4, maybe-false 0
- findings:
  - `[low]` `[patch]` `Store.Sweep` doc comment has one line past the file's wrap width (sweep.go) — rewrapped.
  - `[low]` `[patch]` Parity test names a variable `real`, shadowing the builtin (retention_test.go) — renamed `db`.
  - `[low]` `[reject]` A saved terminal map left stale by a failed `SaveServerState` can make a restart not seed a pane whose terminal changed during the earlier run, so its wait restarts as a lower bound — this only splits and never joins, and it needs a save failure followed by a terminal change. The direction is the one AD-3 already accepts. A guard would add state for a path never observed.
  - `[false]` A history read failure now writes a no-previous row, which permanently splits the Agent in the record — the engine already treats that first sight as new in memory ("recording the first sight as new"). The record now agrees with what the Board showed, and this split never joins two Agents.
  - `[false]` The fold reset breaks backfill history — backfill rows are epoch 0 and are never folded. `firstSight` reads History only for the live epoch, and `TestWaitingBackfillNotFolded` passes.
  - `[false]` The self-referencing DELETE could judge a boundary row it deletes in the same statement — SQLite evaluates the WHERE for every candidate row before deleting, and the statement already relied on that for `max(w.id)`. `TestSweepTwoAgentsOnOneKey` (boundary row deleted, A's working row gone) and the 200-round fake-versus-SQLite parity test pass.
  - `[false]` Intent alignment: the tests exercise the engine and store, not the Board surface the intent names — `TestServeHistoryAfterSweepReusedPane` runs the real binary path (serve, real SQLite, sweep, history API). The engine restart tests assert the Agent view (Class, WaitingSince) that `/api/state` and the Board render.

## Design Notes

Rows with no previous Status are written today only by `firstSight` when no history was folded (new Agent, terminal replaced in-run, unseeded first snapshot) and by `transitionAt` when `p.status` is invalid; the fallback closes the second. History order is `ORDER BY observed_at, id`; the boundary is the last null-previous row in that order:

```sql
AND w.new_status = 'working' AND (w.observed_at, w.id) >= (
  SELECT b.observed_at, b.id FROM transitions b WHERE <same key> AND b.previous_status IS NULL
  ORDER BY b.observed_at DESC, b.id DESC LIMIT 1)  -- no boundary row: no restriction
```

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- expected: pass
- `go test -race -count=20 ./internal/engine/ ./internal/store/ ./internal/core/` -- expected: pass
- `gofmt -l .` -- expected: no output

## Auto Run Result

Status: done

**Summary.** The Waiting Time fold starts over at a Transition with no previous Status, which is an Agent's first row. The first snapshot of a connection does not seed a pane from the record when its saved non-empty `terminal_id` differs from the one shown, so that pane's first sight becomes such a row. `transitionAt` uses the fold's last Status as previous, so only first rows lack one. Retention keeps only the current Agent's latest working row, in the store and the engine fake alike. AD-3 and AD-4 document successive Agents on one AgentKey. There is no schema change, no new row type and no ADR.

**Files.**
- `internal/core/attention.go`: `FoldResult.Add` resets at a row with no previous Status.
- `internal/core/attention_test.go`: reset cases, and a random Add-equals-Fold test with boundaries.
- `internal/engine/engine.go`: `apply` takes the terminals held before the snapshot and skips seeding a pane whose terminal changed; `transitionAt` falls back to the fold's Status for previous.
- `internal/engine/fake_test.go`: fake Sweep applies the same current-Agent working-row rule.
- `internal/engine/attention_test.go`: restart tests for a re-detected Agent, a terminal changed while the Daemon was down, a missing or empty saved terminal, and the previous-Status fallback.
- `internal/engine/retention_test.go`: restart, then sweep, then restart for both triggers; fake-versus-SQLite Sweep parity over 200 random rounds.
- `internal/store/sweep.go`: the latest working row must not be followed by a row with no previous Status in `(observed_at, id)` order.
- `internal/store/sweep_test.go`: two Agents on one key, with the current Agent working or not; boundary in History order.
- `cmd/herddash/serve_test.go`: after a sweep on a reused pane, the history API holds B's class start and none of A's rows.
- `ARCHITECTURE-SPINE.md`: AD-3 gains "Successive Agents on one AgentKey"; AD-4's Retention wording now says the current Agent's latest working row.

**Review.** 2 patches (low), 0 deferred, 1 low rejected and 4 false (reasons in the triage log).

**Follow-up review:** false. This pass patched no high or medium finding.

**Verification.** `go build ./...`, `go vet ./...` and `go test ./...` pass, and `gofmt -l .` prints nothing. The implementer ran `-race -count=20` on core, store and engine; engine needs `-timeout 60m`, about 17 minutes. After the merge with the epic branch, the checks are re-run (see the commit).

**Residual risk.** An Agent that quits and another that starts in the same terminal while the Daemon is down leave no row between them, so they still join. AD-3 documents this.

