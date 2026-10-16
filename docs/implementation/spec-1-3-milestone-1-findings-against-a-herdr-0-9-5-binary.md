---
title: 'Story 1.3: Milestone-1 findings against a herdr 0.9.5 binary'
type: 'feature'
created: '2026-10-16'
status: 'done'
baseline_commit: 'cee8a3366af6a3388189b6828f0b25ed4ef0bc4a'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/research/herdr-socket-api.md'
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The client and the record rest on four protocol claims checked only against 0.8.2 or
taken from documentation, and there are no Go wire types. Stories 1.4, 1.6 and 1.9 are blocked on
both.

**Approach:** Run herdr 0.9.5 in isolation, answer the spine's four open questions with recorded
evidence, commit the schema dump, and generate the wire types herddash uses into
`internal/herdr/gen` with an in-repo, standard-library-only generator run by `go generate`.

## Boundaries & Constraints

**Always:**
- **Isolation:** every herdr invocation goes through a wrapper equivalent to
  `env -i PATH=/opt/homebrew/bin:/usr/bin:/bin HOME=<scratch> TERM=xterm-256color
  /opt/homebrew/Cellar/herdr/0.9.5/bin/herdr …`. This session runs inside a live herdr pane, so
  the inherited `HERDR_SOCKET_PATH` points at the operator's live server and must never reach
  herdr. Keep `<scratch>` short (`/tmp/hd13/home`): macOS caps socket paths at 104 bytes.
- Stop the isolated server only through its own socket or by the exact PID you started.
- Drive agent status with herdr's own reporting call (`pane.report_agent`) or a scripted fake
  process, never a real AI agent.
- Each finding records its evidence (the request lines and responses, abridged) and amends
  `docs/research/herdr-socket-api.md` in this change. A finding that contradicts the spine or an
  ADR updates that `AD` or ADR in this change.
- Generated code imports only the standard library and covers the definitions herddash needs:
  ping's result, `session.snapshot` and its records, `events.subscribe` with `Subscription`,
  `pane.process_info`, the lifecycle event envelope and data, the pane-scoped subscription
  envelope, the error response, and `AgentStatus`.
- A test regenerates into a temp dir and fails if the output differs from the committed files.

**Never:**
- Connect to, signal, stop or restart the live server (PID 1560, `~/.config/herdr/herdr.sock`).
- Install a plugin, edit `~/.config/herdr/config.toml`, or call `pane.focus`, `agent.focus`,
  `pane.read` or `agent.read`.
- Add a module dependency, or generate request types for focus or pane-output methods.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Second request | two `ping` lines on one connection | recorded: answered, or broken pipe | evidence either way |
| Restart | snapshot, `server stop`, start, snapshot | recorded: pane ids kept or reassigned; field marking a new instance | evidence either way |
| Foreground | shell pane runs `sleep 300` | `pane.process_info` names it; the event (if any) reporting the change is recorded | "no event" is a valid finding |
| Ordering | three status reports on one pane | which of `state_change_seq`, `completion_seq`, `revision` rises per change | evidence |
| Regenerate | `go generate ./internal/herdr/gen` | byte-identical output | test fails on drift |

</frozen-after-approval>

## Code Map

- `/tmp/hd13/schema.json` -- already dumped from 0.9.5: JSON Schema 2020-12, `schemas.{request,success_response,error_response,event,subscription_event}`, each with `$defs`; refs are `#/schemas/<section>/$defs/<Name>`; nullable fields are `type: [T, "null"]` or `anyOf`
- `internal/herdr/doc.go` -- package doc; keep. `gen/` is part of the `herdr` node in `arch_test.go`, so a `package main` generator under `internal/herdr/gen/genwire` passes the import rules if stdlib-only
- `internal/core/types.go:108` -- `Transition.HerdrOrder *uint64`; the ordering finding names the field that feeds it
- Spine `ARCHITECTURE-SPINE.md` -- AD-3 (lines 97-111, epoch) and AD-14 (242-251, foreground) carry UNVERIFIED text; Open Questions (344-355) and Deferred "wire-type generator" (361) get resolved
- `docs/adr/0005-append-only-transition-record.md:28-35,54` and `0006-how-herddash-speaks-to-herdr.md:57` -- amend only if a finding contradicts them

## Tasks & Acceptance

**Execution:**
- [x] `internal/herdr/gen/herdr-api-0.9.5.schema.json` -- commit the dump verbatim -- generator input
- [x] `internal/herdr/gen/genwire/main.go` -- generator: object → struct (required → value, optional or nullable → pointer with `omitempty`), string enum → named type plus constants, `$ref` and `$defs` resolved across sections, discriminated unions → one struct per variant plus the discriminator constants, raw `json.RawMessage` where a union is decoded later -- repeatable generation
- [x] `internal/herdr/gen/doc.go`, `internal/herdr/gen/*.go` -- `//go:generate` line and the generated files with a `Code generated … DO NOT EDIT.` header
- [x] `internal/herdr/gen/gen_test.go` -- drift test, plus decoding a recorded 0.9.5 snapshot, ping, lifecycle event and pane-scoped event (fixtures in `internal/herdr/gen/testdata/`)
- [x] `docs/research/herdr-0.9.5-probe.sh` -- the isolated probes, re-runnable -- evidence is reproducible
- [x] `docs/research/herdr-socket-api.md` -- a dated 0.9.5 findings section; move confirmed items out of UNVERIFIED, correct any wrong ones, answer its Open items
- [x] Spine, ADRs, `CLAUDE.md` -- resolve Open Questions and the generator deferral; rewrite AD-3/AD-14 UNVERIFIED text; `CLAUDE.md` says the binary is 0.9.5 while the live server still runs 0.8.2 until restarted

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then the drift and decode tests pass and `arch_test.go` passes.
- Given `docs/research/herdr-socket-api.md`, when read, then each of the four questions has an answer with evidence and a date.

## Implementation Notes

- Matrix rows 1-4 are findings, not code paths: their covering check is `docs/research/herdr-0.9.5-probe.sh`, whose 2026-10-16 transcript is quoted in the socket API record; its recorded lines are the decode fixtures in `internal/herdr/gen/testdata/`. Row 5 is `TestGeneratedCodeIsCurrent`.
- The schema dump is excluded from the review diff: it is byte-identical to a fresh `herdr api schema --json` of 0.9.5 (checked with `cmp`).
- `state_change_seq` contradicted the spine's ordering convention and ADR-0005's "herdr's ordering"; both are amended. `done` never appeared with no terminal client attached; carried to Story 1.5 in the socket API record.
- `go-jsonschema` was tried during planning and fails on this schema; recorded in the socket API record.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH | AD-3 "Restored" branch keeps the epoch when a fresh server reissues the same pane ids | medium | Q1 shows a fresh server restarts ids at `w1:p1`; matching ids alone cannot tell restore from reissue, which is ADR-0005's exact hazard; patch: record signals and hazards, leave the rule to Story 1.6 |
| 2 | BH, ECH | AD-3 "Otherwise" branch splits every Agent when one pane closed during the outage | medium | Same root cause as 1 (rule designed past the evidence); patch with 1 |
| 3 | BH | No epoch behaviour before milestone 2 persists the map | low | Same root cause as 1; patch with 1 |
| 4 | BH, VG | `Subscription` roots omit `pane.moved` and `pane.exited` though their data types are generated and the record says the engine must follow moves | low | Confirmed in `roots`; direct list edit; patch |
| 5 | BH | `events_lost` absent from the 0.9.5 schema, unrecorded | low | `grep -c events_lost` on the dump is 0; patch: record it and that it is matched by raw error code |
| 6 | BH | Confirmations promoted without quoted evidence | medium | Spec requires evidence per finding; transcript lines 74-80 hold it; patch |
| 7 | BH | Q2 excerpt splices two subscriptions under one heading | low | Probe opens a second `LIFECYCLE` subscription for the `exec -a claude` step; patch |
| 8 | BH, ECH | Probe `python3 … \| tee` under `set -eu` masks Python failures | medium | POSIX sh pipeline status is `tee`'s; a failed guard would not stop server restarts; patch |
| 9 | BH, ECH | Re-run leaves stale fixtures; copy to testdata undocumented | low | `save()` writes only on match and `$OUT` is not cleared; patch |
| 10 | BH | sprint-status `in-progress` vs spec `in-review`; isolation action item open | false | Step 5 moves the story to review and the action item is closed at merge; workflow ordering, not a defect |
| 11 | BH | ADR-0006 Context edited in place, no amendment section or status note | low | ADR-0005 in the same change uses a dated amendment; inconsistent audit trail; patch |
| 12 | BH | Spec's `lsof` check is weak and hard-codes the PID; Code Map cites /tmp | low | Fix edits this spec; rejected |
| 13 | BH | CLAUDE.md should warn that plain `herdr` here is a 0.9.5 client against a 0.8.2 server | low | Real; agent-context file; defer |
| 14 | BH | Long unwrapped lines in CLAUDE.md and AD-14; AD-14 overclaims "only foreground change herdr reports" | low | Observed only sleep, cd and argv0; patch AD-14 wording and wrap; CLAUDE.md wrap deferred with 13 |
| 15 | BH, ECH | `TestNoForbiddenRequests` checks one identifier; `"pane.output_matched"` is in wire_gen.go | low | It is a `SubscriptionEventKind` decode constant, not a request, so the Never holds; but the test cannot catch a forbidden `RequestMethod`/`SubscriptionType` under another name; patch test to read those constants' values via go/ast |
| 16 | ECH | `forbidden` not applied to enum values | false | Spec forbids request types; an event-kind enum value builds no request; covered by 15 |
| 17 | VG | genwire's error guards (differing duplicates, forbidden and missing variants) never run in a test | medium | VG pre-verified: dropping the `put` comparison passes every test; patch with `genwire/main_test.go` |
| 18 | VG | stdlib-only not enforced for the herdr node | low | Widening `stdlibOnly` changes arch_test's set, which needs an ADR; defer |
| 19 | ECH | engine may import `internal/herdr/gen` directly | low | arch_test treats gen as the herdr node; tightening needs an ADR; defer |
| 20 | ECH | `pascal` collisions or invalid identifiers | low | Fails loudly at build; current schema clean; guard adds complexity; rejected |
| 21 | ECH | Nullable array items lose nullability | false | The 0.9.5 schema has no nullable `items` (checked) |
| 22 | ECH | Duplicate discriminator const overwrites a variant | low | Schema has none; guard adds complexity; rejected |
| 23 | ECH | Drift test skips if `go` is not on PATH | low | Direct replacement with `runtime.GOROOT()`; patch |
| 24 | ECH | `PaneProcessInfoParams.PaneID` nil queries another pane | false | No caller exists; the schema makes it nullable; Story 1.4's client sets it |
| 25 | ECH | `NewTransition` accepts HerdrOrder on live Transitions | low | The finding makes it snapshot-only, but enforcing adds a public sentinel to core; defer to Story 1.5 |
| 26 | ECH | Stale socket file makes the restart wait return early | maybe-false | The recorded run succeeded; would need `server stop` to leave the socket; low if true; rejected |
| 27 | ECH | PID fallback misses a daemonised server | false | `herdr server` ran in the foreground (`detached_server_daemon:false`) |
| 28 | ECH | Q4 conn B recv timeout crashes on the opposite result | low | Direct try/except; patch |
| 29 | ECH | `rpc` with no response line gives a bare traceback | low | Loud once 8 is fixed; rejected |
| 30 | ECH | "terminal ids changed" counts a missing pane as changed | low | Direct predicate fix; patch |
| 31 | ECH | `mv session.json` without an existence check | low | `set -e` aborts loudly; rejected |
| 32 | ECH | Required nullable field is a pointer without `omitempty` | false | Correct: a required key must be sent; the spec's wording is looser than the schema |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `go generate ./internal/herdr/gen && git diff --exit-code internal/herdr/gen` -- expected: no diff
- `lsof -p 1560 | grep -c hd13` -- expected: 0 (live server untouched)
