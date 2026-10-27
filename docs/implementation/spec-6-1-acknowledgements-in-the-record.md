---
title: "Story 6.1: Acknowledgements in the record"
type: 'feature'
created: '2026-10-27'
status: 'done'
baseline_revision: '8614682655ea6c37a2d6422c5c609f4506c67e09'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0014-acknowledging-a-finished-agent.md'
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: []
deferred: ['Story 6.2: the engine retention log line reports SweepResult.Acknowledgements']
---

<intent-contract>

## Intent

**Problem:** The operator decided PRD §8 Q1 directly (vfkb 9aee0fc69560, ADR-0014): a Finished Box
must be markable Seen once its result is read. Whether a finish is seen has to be a fact the record
keeps, derived the same way after any Daemon restart, before the engine, the write routes or the
Board can use it.

**Approach:** ADR-0014's record layer only. A second append-only table, `acknowledgements`
(migration 0005), each row naming one exact finish: the AgentKey and the instant its Finished
class began (`core.FoldResult.Since`, the view's `waiting_since`), with `seen` 1 or 0 (an undo is a
further row). `internal/core` owns the `Acknowledgement` type, its validating constructor, and the
pure rule `core.Seen(fold, acks)`: true exactly when the fold is known and finished with a positive
`Since`, and the last row (recording order) naming that `Since` is seen. `AgentView` gains `seen`,
and a seen, non-Background Agent ranks 2 (Idle's) in the sort key. `internal/store` writes a batch
in one transaction, reads one Agent's rows in recording order, and sweeps them with Retention in
the existing sweep transaction.

## Boundaries & Constraints

**Always:** The table and indexes exactly as ADR-0014 gives them. Rows are only inserted; the sweep
in `sweep.go` is the only delete (`TestOnlyDelete`). `AppendAcknowledgements` callable only from
`internal/engine` and `internal/store` (`callsite_test.go`). No new import edge (`arch_test.go`).
`internal/core` stays pure: no I/O, no clock. `attention_class` stays `finished` on a seen view.

**Never:** No engine, HTTP or Board change (Stories 6.2 to 6.4). No change to the transitions,
resync_markers or server_state tables. No call to herdr of any kind. Do not touch the live herdr or
the running service.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior |
|----------|--------------|---------------------------|
| Seen | fold finished since S; rows `[S seen]` | `core.Seen` true; view `seen`, class finished, rank 2, start `-S` |
| Undone | rows `[S seen, S unseen]` | false |
| Seen again | rows `[S seen, S unseen, S seen]` | true |
| done to idle | fold `done` then `idle` in one finish (Since unchanged) | still true |
| New turn, Needs you, new Agent | fold class or Since moves | false, with no row written |
| Older finish only | rows name an earlier Since | false |
| Not finished | Needs you, Idle (class working), Working, Unknown, fold not known, Since 0 | false; view `seen` false, sort key unchanged |
| Background | seen input, finished, background work open | rank 3 (Background wins) |
| Batch fails mid-way | second of three inserts aborts after the first was written | error; no row of the batch kept |
| Sweep | rows older than the cutoff | present key: rows with `finished_since >= Since` kept (all when Since 0); others swept; counted in `SweepResult.Acknowledgements` |
| Sweep fails | acknowledgements delete aborts after the transitions and markers deletes ran | error; every transition, marker and acknowledgement kept |
| Schema 4 database | 0.7.x database with rows | migrated to 5 in the one migration transaction, rows unchanged; a schema-4 binary then refuses it (`ErrSchemaNewer`) |

</intent-contract>

## Code Map

- `internal/store/migrations/0005_acknowledgements.sql` -- new: the table and its two indexes.
- `internal/core/acknowledgement.go` -- new: `ErrInvalidAcknowledgement`, `AcknowledgementInput`,
  `Acknowledgement` with accessors, `NewAcknowledgement`, `Seen`.
- `internal/core/view.go` -- `AgentViewInput.Seen`, `AgentView.Seen` (`json:"seen"`, never
  omitted), `newSortKey` gains `seen` (rank 2 after Background's 3 is decided).
- `internal/store/acknowledgements.go` -- new: `AppendAcknowledgements` (one prepared `INSERT`,
  one transaction, no-op for none), `Acknowledgements` (ordered by `id`, the recording order, not `observed_at`, which a stepped clock can reorder; each row
  rebuilt through the constructor).
- `internal/store/sweep.go` -- `sweepAcknowledgements`, run third in `Sweep`'s transaction;
  `SweepResult.Acknowledgements`.
- `callsite_test.go`, `testdata/callsite/` -- `AppendAcknowledgements` guarded to
  `internal/engine` and `internal/store`; fixtures for cmd, httpapi and importer violations and the
  engine's permitted call.
- `internal/core/testdata/board-order.json` -- every view gains `seen`; workspace `docs` (w4)
  holds Finished w4:p1, seen w4:p2 and older Idle w4:p3, in that order; headers
  `[3,8,12,16,19,20,21]`.
- `internal/httpapi/web/order.test.mjs` -- an explicit test of the new workspace's order.

## Tasks & Acceptance

**Execution:** all done, as mapped above, with these tests:

- `internal/core/acknowledgement_test.go`: `TestNewAcknowledgement` (each refusal wraps
  `ErrInvalidAcknowledgement`), `TestSeen` (table, no I/O), `TestSeenAcrossTransitions` (real folds:
  done to idle keeps it; new turn, Needs you, new Agent's row, next finish end it),
  `TestAgentViewSeen` (rank 2 with Idle's sort key, class stays finished, non-finished classes
  unaffected, Background keeps 3).
- `internal/core/view_test.go`: the golden loader passes `seen`; `TestAgentViewJSON` requires
  `"seen":false`.
- `internal/store/acknowledgements_test.go`: `TestUpgradeFromSchema4` (rows of transitions,
  resync_markers and server_state byte-for-byte unchanged; schema-4 `prepare` with `ms[:4]` refuses
  with `ErrSchemaNewer`), `TestUpgradeFromSchema4RollsBack` (0005 plus a failing 0006 leaves schema
  4 and no acknowledgements objects), `TestAcknowledgementChecks`, `TestAppendAcknowledgements`
  (column mapping, recording order by `id` even when a later row's `observed_at` is earlier, epoch isolation),
  `TestAppendAcknowledgementsRollsBack` (a trigger aborts the second insert only when the first
  row is already in the table, so the failure proves a mid-transaction rollback),
  `TestAcknowledgementsSurviveReopen` (the story's seven cases plus done to idle, before and after
  close and reopen).
- `internal/store/sweep_test.go`: `TestSweepAcknowledgements`, `TestSweepAcknowledgementsUnknownSinceKeepsAll`,
  `TestSweepAcknowledgementsRollsBack` (the abort trigger fires only after witness triggers saw
  both earlier deletes run).
- `internal/store/store_test.go`: schema version 5, the table's columns and indexes, the migration
  list.

**Acceptance Criteria:** every criterion of Story 6.1 in `docs/planning/epics.md` is met by the
tests above.

## Spec Change Log

## Review Triage Log

### 2026-10-27 — Self-review pass
- `[low]` `[defer]` The engine's `retention sweep` log line does not report the new
  `SweepResult.Acknowledgements` — the story forbids engine changes; added to Story 6.2's
  description and criteria in `epics.md` in this commit.
- `[low]` `[patch]` The first rollback test's AFTER-INSERT witness table was rolled back with the
  batch, so it proved nothing — replaced by an abort conditioned on the first row being present.
- `[false]` `[reject]` A seen view is still drawn as Finished by `order.mjs` (`boxKind`) and
  counted as finished — by design here: the Board is Story 6.4's, whose criteria already make
  `boxKind` return `seen`. The Board orders by the Daemon's sort key, so the order is already right.
- `[false]` `[reject]` `AgentView.Seen` is set on a seen Background view — the story sets it
  whenever the input says so and the class is finished; Background only takes the rank, so the
  view still says what the record says.

### 2026-10-27 — Review pass
- `[low]` `[patch]` (two reviewers, same finding) `store.Acknowledgements` ordered by
  `observed_at`, then `id`, so a wall clock stepped back between a Seen and its Undo put the undo
  first and, after a restart, `core.Seen` read the opposite of the operator's last action. Now
  ordered by `id` alone, the single writer's append order; the test asserts a later row with an
  earlier clock stays last, and ADR-0014, migration 0005's comment and Story 6.1's criteria say so.

### 2026-10-27 — Review triage, round 1
- Fixed: the clock-ordering defect above (one finding, reported by two reviewers, merged). The
  query fix landed in 33a06ec; this round adds
  `TestAcknowledgementUndoSurvivesClockStepAcrossRestart`, which records a Seen and then its Undo
  at an earlier wall clock in separate batches, closes and reopens the store, and checks that
  `core.Seen` over the rows read back is false. It and `TestAppendAcknowledgements` both fail
  with the old `ORDER BY observed_at, id` and pass with `ORDER BY id`.
- Deferred: none.
- Rejected: none.

## Design Notes

The acknowledgements sweep runs after the transitions and markers deletes, inside the same
transaction, so its failure rolls all three back. Its key match mirrors `sweepTransitions`
(`json_each` over the present set, `since = 0` keeps all), which keeps an acknowledgement in force
(it names exactly the present `Since`) from ever being swept. `CLAUDE.md`, the spine and
how-herddash-works are brought up to date in Story 6.5, as Epic 6 plans; migration 0005 is a
one-way door for 0.7.x binaries.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test -race ./...` -- all 12 packages pass
- `go test -race -count=20 ./internal/store/ ./internal/core/` -- pass
- `node --test internal/httpapi/web/order.test.mjs` -- 91 pass, 0 fail
- `node internal/httpapi/web/check/board-browser.mjs <built binary>` -- all checks ok

## Auto Run Result

Status: done

**Summary.** Acknowledgements are a second append-only table in the record (migration 0005),
written in one all-or-nothing transaction by `store.AppendAcknowledgements` (engine-only), read in
recording order by `store.Acknowledgements`, and swept with their Agent's rows by Retention.
`core.Seen` derives whether the current finish is seen from the fold and the rows, so the answer is
the same after a reopen. `AgentView` carries `seen`, and a seen, non-Background Agent sorts with
the Idle ones. No engine, HTTP or Board code changed.
