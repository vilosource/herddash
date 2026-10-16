---
title: 'Story 1.1: Project skeleton, import rules and CI'
type: 'chore'
created: '2026-10-16'
status: 'done'
baseline_commit: '184609e42c1bb12acef49f2b7cb0e6c740289efb'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** There is no code. Every later story needs a module laid out as the architecture spine
says, a shared core vocabulary, and CI that stops a story from breaking the import rules.

**Approach:** Create the Go module and its eight internal packages plus `cmd/herddash`. Define the
core domain types every story will share. Add a test that enforces the spine's import graph, and a
CI workflow that builds, vets, tests, checks formatting and cross-compiles.

## Boundaries & Constraints

**Always:**
- Module `github.com/vilosource/herddash`, `go 1.26`, no dependencies in `go.mod`.
- Packages: `cmd/herddash` and `internal/{core,engine,herdr,procenv,store,httpapi,importer,config}`.
- Permitted imports follow the spine's diagram exactly:
  - cmd → engine, httpapi, importer, config, store
  - engine → core, herdr, procenv, store
  - httpapi → core, engine, store
  - importer → core, store, config
  - store, herdr and config → core
  - core and procenv → no internal package
- `internal/core` imports only the standard library and never calls `time.Now()`.
- GitHub Actions are pinned by commit SHA, as in `release-please.yml`. Use `actions/checkout`
  v7.0.1 (`3d3c42e5aac5ba805825da76410c181273ba90b1`) and `actions/setup-go` v7.0.0
  (`b7ad1dad31e06c5925ef5d2fc7ad053ef454303e`).
- No AI attribution in commits or PR text. Conventional Commits.

**Never:**
- Write any behaviour beyond the core types and `herddash version`. There is no herdr client,
  store or HTTP yet.
- Add a dependency, npm, or a build step.
- Touch release-please's workflow or config.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Version | `herddash version` | prints `herddash <version> (<commit>)`; version is `dev` and commit comes from build info when not stamped | none |
| Unknown command | `herddash frobnicate` | usage on stderr | exit 2 |
| Valid Transition | live provenance, epoch ≥ 1, valid new Status | Transition returned | none |
| Backfill epoch | backfill provenance with epoch ≠ 0, or live/snapshot with epoch 0 | error | typed error from `core` |
| Bad Status | new Status not one of the five | error | typed error |
| Unknown previous Status | previous not known | stored as absent (nil), never the string `unknown` | none |

</frozen-after-approval>

## Code Map

- `.github/workflows/release-please.yml` -- existing; SHA-pin style to copy, do not modify
- `CLAUDE.md` -- "Build and test" and "Repository state" sections still say there is no code; update them
- `docs/implementation/epic-1-context.md` -- Epic 1 constraints, already compiled

## Tasks & Acceptance

**Execution:**
- [x] `go.mod` -- create the module, `go 1.26` -- module root
- [x] `cmd/herddash/main.go` -- `version` subcommand, usage, exit 2 on unknown -- smallest real CLI; later stories add `serve` and `import-plugin-log`
- [x] `internal/{engine,herdr,procenv,store,httpapi,importer,config}/doc.go` -- package doc naming its spine role -- packages exist for the graph test
- [x] `internal/core/doc.go`, `internal/core/types.go` -- AgentKey{Host, Server, Epoch, PaneID}, Status with its five constants and validation, AgentProgram, Provenance (`live`, `snapshot`, `backfill`), Transition with nullable previous Status, herdr ordering and import key, and `NewTransition` enforcing the matrix -- shared vocabulary (AD-1, AD-3, AD-13)
- [x] `internal/core/types_test.go` -- table tests for the matrix rows -- core is table-tested with no I/O
- [x] `arch_test.go` (module root, package `herddash_test`) -- parse every non-test `.go` file under `cmd/` and `internal/` with `go/parser`, map imports of this module to packages, and fail on any edge outside the permitted set or any non-stdlib import in core -- enforces the spine diagram
- [x] `.github/workflows/ci.yml` -- on pull_request and push to `develop`/`main`: gofmt check, vet, test, then a cross-compile matrix for darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64 with `CGO_ENABLED=0` -- Story 1.1 CI AC
- [x] `CLAUDE.md` -- Build and test, and Repository state, describe the skeleton -- keep orientation true

**Acceptance Criteria:**
- Given a fresh clone, when `go build ./...`, `go vet ./...` and `go test ./...` run, then all pass, and `gofmt -l .` prints nothing.
- Given a temporary edit adding a forbidden import (e.g. `internal/core` importing `internal/store`), when `go test ./...` runs, then `arch_test.go` fails naming the edge.
- Given the CI workflow, when it runs on this branch's pull request, then every job passes.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/herddash; done` -- expected: all build
- `go run ./cmd/herddash version` -- expected: `herddash dev (...)`

## Implementation Notes

- `NewTransition` also carries Profile, herdr version and observation time (UTC Unix ms), which the PRD lists as recorded on every Transition.
- `herddash help`, `-h`, `-help` and `--help` print usage to stdout and exit 0.
- `go run` embeds no VCS info, so `version` prints `herddash dev (unknown)` there; a built binary prints the commit.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | ECH, BH, VG | Clock rule misses Since/Until/After/Tick/NewTimer/NewTicker/AfterFunc/Sleep and dot-import of time | medium | VG reproduced `time.Since` passing in a scratch copy; patch |
| 2 | ECH, BH | Walk covers only cmd/ and internal/ | medium | `TestImportGraph` iterates only those roots, so a pkg/ dir is never parsed; patch |
| 3 | ECH | `_test.go` files unchecked | low | Exempting tests is a deliberate rule (they need fakes); patch is a one-line comment |
| 4 | ECH | `isStdlib` dot heuristic misclassifies dotless replace paths | low | Needs a `replace` to a dotless path, which nothing here uses; fix needs a stdlib list; rejected |
| 5 | ECH, BH | `herddash version extra` exits 0 | low | Confirmed in `run`; direct fix; patch |
| 6 | ECH | stdout write error ignored | low | Negligible for a version print; fix adds a branch; rejected |
| 7 | ECH, BH, VG | NewTransition accepts empty key fields, ObservedAt <= 0, backfill without ImportKey, live with ImportKey | medium | Confirmed in `NewTransition`; AD-3 and AD-13 invariants the single constructor exists to hold. The new sentinels are public surface (strictly bad_spec), but the package has no consumers yet and re-derivation would produce the same code, so routed as patch |
| 8 | ECH, BH | Zero-value `core.Transition{}` constructible | low | Go cannot forbid it; patch is a doc note that the zero value is invalid |
| 9 | ECH | `Previous=&StatusUnknown` accepted | false | `unknown` is one of herdr's five Statuses; the matrix forbids encoding not-known as `unknown`, which `NewTransition` cannot detect and never does itself |
| 10 | BH | darwin code paths never vetted | low | No darwin-only code yet, but procenv adds some in Story 1.9; direct CI line; patch |
| 11 | BH, VG | Import-graph checker has no negative test | medium | VG pre-verified: inverting the edge condition still passes; patch with a testdata fixture |
| 12 | BH, VG | Five Transition fields never round-trip tested | medium | VG pre-verified: deleting `profile:` passes; patch |
| 13 | BH, VG | `resolveCommit` untested beyond prefix | medium | VG pre-verified; patch with an extracted pure function |
| 14 | BH | No-args exits 2 though the spine makes `serve` the default | low | Provisional until Story 1.5 adds `serve`, which changes it; rejected |
| 15 | BH | CLAUDE.md version wording ambiguous; stamping not wired | low | Real; an edit to an agent-context file routes to defer |
| 16 | BH | CI lacks -race, timeout, concurrency, mod tidy check | low | -race matters for the AD-2 actor; direct workflow lines; patch (-race, timeout, concurrency); mod tidy check rejected while go.mod has no requires |
| 17 | BH | Diff omits docs/implementation files | false | Deliberate: the spec reaches the edge-case layer as claims_file, not in the diff |
