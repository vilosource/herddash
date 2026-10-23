---
title: 'Story 5.5: Tighter import rules and Transition invariants (ADR-0011)'
type: 'chore'
created: '2026-10-23'
status: 'done'
baseline_revision: 'dc975c819bb99df65a8f184b3aae4f8a484639d5'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
warnings: ['oversized']
deferred:
  - summary: >-
      The architecture spine still says only core is standard-library-only, and AD-5 does not point to ADR-0011's gen confinement.
    evidence: |-
      ARCHITECTURE-SPINE.md line 68 and the AD-5 paragraph (around line 168) predate ADR-0011; the spine is a document agents read first, so it belongs to Story 5.8 (documents agents read match the code).
    location: >-
      docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:68
    severity: low
  - summary: >-
      CLAUDE.md's single sentence listing arch_test.go's rules is now long enough to read better as a list.
    evidence: |-
      The Build and test paragraph packs four rules into one clause list after Story 5.5; agent-context file, cosmetic, for Story 5.8.
    location: >-
      CLAUDE.md:187
    severity: low
---

<intent-contract>

## Intent

**Problem:** `arch_test.go` treats `internal/herdr/**` as one node and lists only `core` as standard-library-only, so `internal/engine` (or anything allowed to import `herdr`) could import the wire types in `internal/herdr/gen` directly, and `internal/herdr` could take a third-party dependency, without CI noticing (deferred-work items from Story 1.3). Separately, `core.NewTransition` accepts a `HerdrOrder` on a live or backfill Transition, although herdr's `state_change_seq` is known only from a snapshot (Finding Q3; the store's migration 0001 CHECK already enforces it at the database).

**Approach:** Write ADR-0011 (accepted in advance by the operator, vfkb 1e89662f83af), which only tightens the import rules: `internal/herdr/gen` and `genwire` importable only from `internal/herdr` (and its subpackages), and `internal/herdr` plus its gen packages standard-library-only, enforced in `arch_test.go`. Add the sentinel `core.ErrInvalidHerdrOrder` and have `NewTransition` reject a `HerdrOrder` unless provenance is snapshot.

## Boundaries & Constraints

**Always:** The ADR says only what the operator accepted; no permitted edge in `permitted` changes. Module-internal imports from `internal/herdr` (e.g. `core`) stay governed by the existing edge rules, not by the stdlib rule. `_test.go` files stay exempt, as today. Rejection is a `*TransitionError` wrapping the new sentinel, matched with `errors.Is`, like every other NewTransition rejection. Every production `NewTransition` call site keeps working (engine passes `nil` for live, `p.seenOrder` only for snapshot; importer sets none; store reads only rows its CHECK allows).

**Never:** Widen the ADR or the permitted set; add a dependency; change the stored schema; edit `docs/implementation/sprint-status.yaml` beyond the 5-5 line; touch the frozen research records.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| snapshot with order | Provenance snapshot, HerdrOrder set | Transition built, HerdrOrder() returns it | none |
| live with order | Provenance live, HerdrOrder set | rejected | `*TransitionError{Field: "herdr order"}`, `errors.Is(err, ErrInvalidHerdrOrder)` |
| backfill with order | Provenance backfill, HerdrOrder set | rejected | same sentinel |
| any provenance, no order | HerdrOrder nil | unchanged behaviour | none |
| gen leak | non-test file outside `internal/herdr` imports `.../internal/herdr/gen` or a path under it | arch test violation naming file and import | test fails |
| herdr imports non-stdlib | non-test file under `internal/herdr/**` imports `example.com/x` | violation "herdr may import only the standard library" | test fails |
| herdr imports gen | `internal/herdr/client.go` imports gen | no violation | none |

</intent-contract>

## Code Map

- `arch_test.go` -- `stdlibOnly` (add `"herdr": true`; `nodeOf` already maps gen and genwire to node `herdr`, so they are covered); in `checkTree`'s module-import branch add the gen confinement check: import path `modulePath+"/internal/herdr/gen"` or under it, from a file whose dir is not `internal/herdr` or under it, is a violation. `TestCheckTreeCatchesViolations` holds the `want` list for the fixture.
- `testdata/badtree/` -- fixture tree; add `internal/engine/leak.go` (imports `internal/herdr/gen`; engine->herdr is a permitted edge, so only the gen rule fires), `internal/herdr/bad.go` (imports `example.com/notstd` and `internal/herdr/gen`, the latter must NOT be reported), `internal/herdr/gen/genwire/bad.go` (imports a non-stdlib path, proving subpackages are covered).
- `internal/herdr/**` -- verified 2026-10-23: every non-test import is standard library or `internal/core`/`internal/herdr/gen`; genwire (package main, the generator) imports only stdlib incl. `go/format`. No scoping exception needed.
- Importers of `internal/herdr/gen` outside `internal/herdr`: none (grep; only `arch_test.go` names it as text).
- `internal/core/types.go:72-81` -- sentinel var block; `NewTransition` at ~168, doc list of rejections above it; `TransitionInput.HerdrOrder` comment at ~110.
- `internal/core/types_test.go` -- `TestTransitionRoundTrip` (backfill WITH order: must drop the order and the HerdrOrder assertion moves to a snapshot case), `TestTransitionCopiesPointers` (live WITH order: switch to snapshot), table of rejection tests to extend.
- `internal/httpapi/events_test.go:410` `TestWireTransitionFull` -- backfill WITH order; split so `herdr_order` is asserted on a snapshot Transition.
- `internal/store/record_test.go:181` `TestAppendBackfillRefusesOtherRows` -- its `"order"` case can no longer be built; remove it (core now refuses it earlier). Keep `AppendBackfill`'s `hasOrder` guard in `internal/store/record.go:126` as defence in depth.
- Production call sites (read-only evidence): `internal/engine/engine.go:948,1000,1019,1956` pass nil with live; `:1308` passes `StateChangeSeq` with snapshot; `:2314` sets order only when `prov == ProvenanceSnapshot`; `internal/store/record.go:234` reads rows the 0001 CHECK limits to snapshot; `internal/importer` never sets it.
- `docs/adr/0006..0010` -- style; `docs/adr/README.md` -- index table.
- `docs/implementation/deferred-work.md:14-21` -- the three items to mark resolved.
- `CLAUDE.md` "Build and test" paragraph on `arch_test.go` -- describe the two new rules.

## Tasks & Acceptance

**Execution:**
- `docs/adr/0011-tighter-import-rules-for-herdr.md` -- write ADR (Context, Decision, Consequences, Alternatives Considered), Status Accepted 2026-10-23, "accepted in advance by the operator during Epic 5 planning" -- required before the structural test change.
- `docs/adr/README.md` -- add the 0011 row.
- `arch_test.go` -- add the herdr stdlib rule and gen confinement; extend the fixture `want` list and the must-not-report checks.
- `testdata/badtree/internal/engine/leak.go`, `testdata/badtree/internal/herdr/bad.go`, `testdata/badtree/internal/herdr/gen/genwire/bad.go` -- fixtures, one rule broken each.
- `internal/core/types.go` -- `ErrInvalidHerdrOrder`, the check (after the spurious check), doc comments.
- `internal/core/types_test.go` -- rejection test for live and backfill, acceptance for snapshot; fix the two existing tests.
- `internal/httpapi/events_test.go`, `internal/store/record_test.go` -- adapt as in the Code Map.
- `CLAUDE.md`, `docs/implementation/deferred-work.md` -- describe the new rules; mark the three items resolved by Story 5.5 with where.

**Acceptance Criteria:**
- Given the module as built, when `go test .` runs, then `TestImportGraph` passes and `TestCheckTreeCatchesViolations` reports the gen leak from `internal/engine/leak.go`, the non-stdlib imports in `internal/herdr/bad.go` and `internal/herdr/gen/genwire/bad.go`, and does not report `internal/herdr/bad.go`'s gen import.
- Given a TransitionInput with a HerdrOrder and provenance live or backfill, when `NewTransition` is called, then it returns an error for which `errors.Is(err, core.ErrInvalidHerdrOrder)` holds.
- Given the whole module, when `go build ./... && go vet ./... && go test ./...` run and `gofmt -l .` runs, then all pass and gofmt prints nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 22 findings — high 0, medium 0, low 16, false 6, maybe-false 0
- findings:
  - `[low]` `[patch]` (edge) No fixture for a package under internal/herdr importing gen — added the gen import to `testdata/badtree/internal/herdr/gen/genwire/bad.go` and asserted it is not reported.
  - `[low]` `[patch]` (edge) AppendBackfill's `hasOrder` guard is unreachable and its test was removed — kept the guard with a comment that core refuses it first (defence in depth).
  - `[low]` `[defer]` (blind) Spine line 68 and AD-5 do not mention herdr's new rules — the spine is a document agents read first (agent-context file), and Story 5.8 ("Documents agents read match the code") owns that sweep.
  - `[low]` `[patch]` (blind) ADR-0011 says the fixture "breaks each rule once" though the stdlib rule is broken twice — reworded to "at least once".
  - `[low]` `[patch]` (blind) No fixture proves `_test.go` files stay exempt from the gen rule — added `testdata/badtree/internal/engine/leak_test.go` and asserted it is not reported.
  - `[low]` `[patch]` (blind) No fixture for an import of a package under gen from outside internal/herdr — grouped with the verification-gap finding below; same fix.
  - `[low]` `[reject]` (blind) Negative assertion for `internal/herdr/bad.go` is narrow — a differently worded spurious violation is unlikely in everyday use and an exact-set comparison adds test machinery; the positive `want` list already pins the expected messages.
  - `[low]` `[patch]` (blind) AppendBackfill guard unreachable — same root cause as the edge finding; same fix.
  - `[false]` `[reject]` (blind) No engine-level test shows the engine never produces a rejected order — `internal/engine/engine_test.go:96` asserts a live Transition has no order and `:291`, `:1052` assert snapshot ones carry it; a live caller passing an order would drop the Transition and fail those tests.
  - `[false]` `[reject]` (blind) Spec, context and sprint status not in the change — the spec and epic context are committed at finalization; sprint-status.yaml is left to the orchestrator by instruction.
  - `[low]` `[reject]` (blind) ADR README gains the missing ADR-0010 row, outside scope — a correct one-line index fix; named in the commit body.
  - `[low]` `[reject]` (blind) `resolved:` field in deferred-work.md has no stated convention — the orchestrator asked for items to be marked resolved with where; a convention header is outside this story.
  - `[low]` `[defer]` (blind) CLAUDE.md's arch_test sentence is long — agent-context file, cosmetic; folded into the spine deferral for Story 5.8.
  - `[low]` `[patch]` (verification-gap) The `HasPrefix(ipath, genPath+"/")` branch has no fixture — added `internal/herdr/gen/genwire` to `testdata/badtree/internal/engine/leak.go` with its expected violation.
  - `[low]` `[patch]` (intent) No fixture imports genwire from outside internal/herdr — same as the verification-gap finding; same fix.
  - `[low]` `[reject]` (intent) The stdlib rule is not fixtured in `internal/herdr/gen` itself — nodeOf maps every directory under internal/herdr to one node and genwire (deeper) is fixtured; no harm a developer would meet.
  - `[low]` `[patch]` (intent) Store guard unreachable — same as the edge finding; same fix.
  - `[false]` `[reject]` (intent) A stored row breaking the rule would now fail history reads — migration 0001's `CHECK (state_change_seq IS NULL OR provenance = 'snapshot')` makes such a row impossible.
  - `[low]` `[patch]` (intent) No `_test.go` fixture for the gen rule — same as the blind finding; same fix.
  - `[false]` `[reject]` (intent) `herdr -> core` still allowed is untested — `TestImportGraph` runs on the real tree, where `internal/herdr/client.go` imports core, and passes.
  - `[false]` `[reject]` (intent) Docs changes outside the criteria (CLAUDE.md, deferred-work, README) — CLAUDE.md and deferred-work were required by the orchestrator; none widens the permitted set.
  - `[false]` `[reject]` (intent) Only the HerdrOrder invariant was added though the title says "invariants" — the acceptance criteria name only that one.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- expected: all pass
- `gofmt -l .` -- expected: no output
- `go test -run 'TestImportGraph|TestCheckTree' -v .` -- expected: PASS

## Auto Run Result

Status: done

**Summary.** ADR-0011 (accepted in advance by the operator during Epic 5 planning) confines `internal/herdr/gen` and every package under it to `internal/herdr`, and holds `internal/herdr` and its gen packages to the standard library; `arch_test.go` enforces both. `core.NewTransition` rejects a `HerdrOrder` on a live or backfill Transition with `core.ErrInvalidHerdrOrder`. No production Go code changed behaviour: every caller already obeyed both rules.

**Files changed.**
- `docs/adr/0011-tighter-import-rules-for-herdr.md` -- new ADR.
- `docs/adr/README.md` -- index rows for 0011 and the missing 0010.
- `arch_test.go` -- `herdr` in `stdlibOnly`; gen confinement check; fixture expectations.
- `testdata/badtree/internal/engine/leak.go`, `leak_test.go`, `testdata/badtree/internal/herdr/bad.go`, `testdata/badtree/internal/herdr/gen/genwire/bad.go` -- fixtures.
- `internal/core/types.go` -- sentinel and check; doc comments.
- `internal/core/types_test.go` -- live and backfill rejected, snapshot accepted; existing tests adapted.
- `internal/httpapi/events_test.go` -- `herdr_order` asserted on a snapshot Transition.
- `internal/store/record_test.go` -- removed the now-unbuildable backfill-with-order case; `internal/store/record.go` -- comment on the defence-in-depth guard.
- `CLAUDE.md`, `docs/implementation/deferred-work.md` -- new rules described; three Story 1.3 items marked resolved.

**Review.** 22 findings: 5 patch entries applied (all low: gen-subpackage fixture, herdr-subpackage-may-import-gen fixture, `_test.go` exemption fixture, ADR wording, store guard comment); 2 deferred (spine and CLAUDE.md wording, to Story 5.8); the rest rejected with reasons in the triage log (6 false, 5 low not worth the complexity or outside the criteria).

**Follow-up review recommended:** false -- patched: high 0, medium 0, low 5.

**Verification.** `go build ./...` OK; `go vet ./...` OK; `go test -count=1 ./...` all 12 packages ok; `gofmt -l .` empty; `TestImportGraph` and `TestCheckTreeCatchesViolations` pass.

**Residual risks.** None known. A Transition with an order on a non-snapshot provenance is now dropped with a Warn log by the engine rather than recorded; no current caller produces one.
