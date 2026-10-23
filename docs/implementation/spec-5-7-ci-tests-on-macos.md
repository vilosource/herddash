---
title: 'Story 5.7: CI tests on macOS'
type: 'chore'
created: '2026-10-23'
status: 'done'
baseline_revision: '55b0bcf1acab51bd6ca9475aefa1172a75cf9335'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred:
  - summary: >-
      The macos and release-toolchain jobs have not yet run on GitHub's runners; that -race, procenv's darwin reader, plutil, systemd-analyze verify and the Go 1.27.2 release build all pass there is unproven.
    evidence: |-
      Unverified (would be medium: a job would be red on its first run, visibly, not silently green). Every run so far was local on macOS (Go 1.26.5 and 1.27.2; CI=true on a clean tree). Settled by the first pull-request run of the epic branch.
    location: >-
      .github/workflows/ci.yml macos and release-toolchain jobs
    severity: medium (unverified)
---

<intent-contract>

## Intent

**Problem:** CI runs only on ubuntu with go.mod's minimum Go, so procenv's darwin reader and `TestLaunchdAgentLints` (plutil) never run in CI, the release toolchain (Go 1.27.2) is first exercised by a release, `TestReleaseBuild` never checks the commit or clean-tree build settings, and `TestSystemdUnitVerifies` may be silently skipping on ubuntu (Epic 4 retro #1-#3, #6).

**Approach:** Add two self-contained CI jobs beside the untouched `check`, `board-browser` and `cross-compile` jobs: `macos` (macos-latest, go.mod's Go, `go test -race ./...` with plutil required) and `release-toolchain` (ubuntu-latest, Go 1.27.2: vet, `go test -race ./...` with systemd-analyze required, then `scripts/release-build.sh` with its outputs checked). A test-side env var `HERDDASH_REQUIRE_TOOLS` turns the tool-missing skips into failures; `TestReleaseBuild` asserts `vcs.revision` = HEAD and `vcs.modified` matching the tree (false required under `CI`); a new release_test.go test pins the new jobs' properties.

## Boundaries & Constraints

**Always:** Reuse the existing checkout/setup-go SHAs (one SHA per action; CI pins = release pins). The release toolchain version in ci.yml equals the spine's "Go toolchain for releases" row, read the same way `TestReleaseWorkflow` reads it. A test needing a tool named in `HERDDASH_REQUIRE_TOOLS` fails naming the variable; unlisted, it skips as today. Local runs without the variable behave as today.

**Never:** Edit the `check`, `board-browser` or `cross-compile` jobs; add a third-party action; add a dependency; change `scripts/release-build.sh`'s behaviour; touch the live herdr or the running herddash; edit `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| tool present | plutil / systemd-analyze on PATH | test runs as today | none |
| tool missing, not required | env unset | `t.Skip` as today | none |
| tool missing, required | `HERDDASH_REQUIRE_TOOLS` lists it | `t.Fatal` naming the tool and the variable | test fails |
| clean tree | git repo, nothing modified/untracked | each binary: `vcs.revision`=HEAD, `vcs.modified=false` | none |
| dirty tree, local | uncommitted change, `CI` unset | `vcs.modified=true` accepted (matches tree) | none |
| dirty tree under CI | `CI` set, tree modified | fails: binaries would not be clean-tree builds | test fails |
| no git / not a repo | git missing, `CI` unset | vcs assertions skipped with a log line | under `CI`, fails |

</intent-contract>

## Code Map

- `.github/workflows/ci.yml` -- jobs `check` (ubuntu, go.mod, `go test -race`), `board-browser` (Story 5.6), `cross-compile`; pins checkout `3d3c42e5…` v7.0.1, setup-go `b7ad1dad…` v7.0.0. New jobs go after `board-browser`, before `cross-compile`, no `needs`.
- `.github/workflows/release.yml:825-868` -- house pattern for `go-version: '1.27.2'` and the build-info/`version` verification loop (`go version -m`, `sha256sum -c`).
- `release_test.go:47-166` `TestReleaseBuild` -- builds all four targets via `scripts/release-build.sh v0.1.0 abc1234 <dir>` from the repo root, reads `buildinfo.ReadFile` settings; add `vcs.revision`/`vcs.modified` there. `topBlock`, `codeLines`, `readText`, `pinnedActions`, spine regex at 583 are reusable.
- `deploy_test.go:236-251` `TestLaunchdAgentLints`, `:384-410` `TestSystemdUnitVerifies` -- the two `exec.LookPath` skips to route through a shared helper.
- `scripts/release-build.sh` -- read-only; `-trimpath`, `-ldflags "-s -w -X …"`; Go keeps vcs settings under `-trimpath` (release.yml already asserts them).
- `CLAUDE.md` "Build and test" -- describes CI's jobs; add the two new ones and the env var.
- `docs/implementation/deferred-work.md` -- Story 4.2 #20 entry (plutil lint in CI on macOS) to mark resolved if present.
- Local: Go 1.27.2 installed via mise at `$HOME/.local/share/mise/installs/go/1.27.2/bin`.

## Tasks & Acceptance

**Execution:**
- `deploy_test.go` -- add `requireTool(t, name, why) string` (LookPath; missing → Fatal if listed in space-separated `HERDDASH_REQUIRE_TOOLS`, else Skip); use it in both tests; unit-test the list parsing.
- `release_test.go` -- in `TestReleaseBuild`, determine HEAD and tree cleanliness with git from the repo root before building; assert every binary's `vcs.revision`==HEAD and `vcs.modified` == (tree dirty); under `CI` require a clean tree and git; add `TestCIWorkflow` checking ci.yml: jobs `check`/`board-browser`/`cross-compile` still use `go-version-file: go.mod`; a `macos` job on `macos-latest` with `go-version-file: go.mod`, `go test -race ./...` and `HERDDASH_REQUIRE_TOOLS: plutil`; a `release-toolchain` job on ubuntu-latest with `go-version: '<spine row>'`, `go vet ./...`, `go test -race ./...` with `HERDDASH_REQUIRE_TOOLS: systemd-analyze`, and `scripts/release-build.sh`.
- `.github/workflows/ci.yml` -- add the `macos` and `release-toolchain` jobs (timeout 15); release-toolchain's build step runs `scripts/release-build.sh 0.0.0-ci "$(git rev-parse --short=12 HEAD)" "$RUNNER_TEMP/dist"`, then `sha256sum -c`, four lines, and the linux/amd64 binary's `version` output.
- `CLAUDE.md` -- Build and test: the macOS and release-toolchain jobs, `HERDDASH_REQUIRE_TOOLS`, and TestReleaseBuild's vcs checks.
- `docs/implementation/deferred-work.md` -- mark entries this closes resolved (Story 5.7).

**Acceptance Criteria:**
- Given a pull request, when CI runs, then a `macos-latest` job runs `go test ./...` (with -race) and `TestLaunchdAgentLints` cannot skip there.
- Given ci.yml, when diffed against the baseline, then the `check`, `board-browser` and `cross-compile` jobs are byte-identical.
- Given CI, when it runs, then a job vets, tests and runs `scripts/release-build.sh` under the spine's release Go (1.27.2), and the existing jobs still do so under go.mod's minimum.
- Given `TestReleaseBuild` in a clean checkout, when it runs, then each binary's `vcs.revision` equals HEAD and `vcs.modified` is `false`.
- Given the ubuntu release-toolchain job, when systemd-analyze is missing, then `TestSystemdUnitVerifies` fails.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 23 findings — high 0, medium 2, low 16, false 4, maybe-false 1
- findings:
  - `[maybe-false]` `[defer]` (intent-alignment A) Every AC is "when CI runs", but only ci.yml's text is tested; no run shows the jobs passing on GitHub — cannot be settled without a GitHub run; deferred as medium (unverified), settled by the first PR run.
  - `[medium]` `[patch]` (intent-alignment B) requireTool's fail branch has no automated test — same root as the verification-gap row; fixed there.
  - `[low]` `[reject]` (intent-alignment C) `check`'s behaviour changes: under CI, TestReleaseBuild now fails on a dirty tree or missing git — that is AC4 working as intended in every job that runs the suite; ci.yml's existing jobs are byte-identical, which is what "unchanged" protects.
  - `[low]` `[reject]` (intent-alignment D) In `check`, TestSystemdUnitVerifies can still skip — the ubuntu `release-toolchain` job requires systemd-analyze on the same image, so a missing tool fails CI; requiring it in `check` too would edit an existing job.
  - `[low]` `[reject]` (intent-alignment E) vcs.modified=false is asserted only under CI — a developer's dirty tree cannot produce clean-tree binaries; locally the setting is still checked against the tree, and CI (where releases come from) requires false.
  - `[low]` `[reject]` (intent-alignment F) `-race` instead of the literal `go test ./...`; go.mod's release build is covered via TestReleaseBuild in `check` — -race is a superset matching `check`; TestReleaseBuild runs release-build.sh for all four targets under go.mod's Go.
  - `[false]` `[reject]` (intent-alignment G) AC3-AC5 exceed the title — they are in the story's own acceptance criteria in epics.md.
  - `[low]` `[patch]` (edge-case) `CI=false`/`CI=0` counted as CI — now `strconv.ParseBool(os.Getenv("CI"))`; CLAUDE.md says "with `CI` true".
  - `[low]` `[reject]` (edge-case) Tree changing between `git status` and the build — only a concurrent writer could do it; none of the suite's tests writes into the repo (a CI=true clean-tree run stayed clean); a re-read adds logic for an unseen case.
  - `[low]` `[patch]` (edge-case) A future go.mod go/toolchain line above 1.27.2 would make GOTOOLCHAIN=auto silently switch Go in release-toolchain — job-level `GOTOOLCHAIN: local`, required by TestCIWorkflow.
  - `[low]` `[patch]` (edge-case) vet's position in release-toolchain is not pinned — `run: go vet ./...` heads TestCIWorkflow's ordered list.
  - `[low]` `[patch]` (edge-case) A failing git call does not say which one — errors are wrapped with the command (`git rev-parse HEAD: …`).
  - `[low]` `[reject]` (blind) The CI release step lacks release.yml's `go version -m` loop — TestReleaseBuild asserts the same settings (target, cgo, trimpath, vcs.revision, vcs.modified) on Go 1.27.2-built binaries in the same job; extracting release.yml's shell into a shared script is a refactor beyond a direct correction.
  - `[low]` `[patch]` (blind) `-eq 4` is a literal — TestCIWorkflow builds the expected line from `len(releaseTargets)`.
  - `[false]` `[reject]` (blind) A typo in HERDDASH_REQUIRE_TOOLS silently disables the safeguard — TestCIWorkflow requires the exact `HERDDASH_REQUIRE_TOOLS: plutil` / `: systemd-analyze` lines (now built from the constant), so a typo in CI fails; locally the variable is unset by design.
  - `[low]` `[reject]` (blind) TestReleaseBuild's bash check still plain-skips — bash is on every runner image and every developer machine; moving it adds a required-tools entry for a case nobody meets.
  - `[low]` `[patch]` (blind) TestCIWorkflow flattens steps, so the env line could sit under vet — it now requires the env line immediately before `run: go test -race ./...` in both jobs.
  - `[low]` `[patch]` (blind) CI=false counts as CI — same root as the edge-case row; same fix.
  - `[false]` `[reject]` (blind) deferred-work entries resolved before CI evidence — both `resolved:` lines state the local evidence and that the first PR run proves it, the convention Story 5.6 settled.
  - `[low]` `[patch]` (blind) TestJudgeTree lacks a tracked modification under CI and a not-a-repository error — rows added (fatal under CI; skip locally, fatal under CI).
  - `[low]` `[reject]` (blind) gofmt/go build not on the release toolchain — go vet and go test compile every package under 1.27.2; gofmt's rules are stable across these releases; not asked by the story.
  - `[false]` `[reject]` (blind) Unweighed CI cost — TestReleaseBuild's four cross-compiles take ~5 s locally; the whole -race suite took ~1 min on macOS, far inside 15 minutes.
  - `[medium]` `[patch]` (verification-gap) requireTool's fail-instead-of-skip branch never runs in a test, and ci.yml's variable name is not tied to `requireToolsEnv` — new TestRequireToolFailsWhenListed re-runs the test binary with an empty PATH: with the variable it must exit non-zero naming it, without it must SKIP; TestCIWorkflow builds the lines from `requireToolsEnv`.

## Design Notes

The systemd requirement goes on the new ubuntu `release-toolchain` job rather than `check`, so the existing ubuntu jobs stay unchanged while ubuntu still proves the unit verifies. `CI` (set by GitHub on every runner) gates the clean-tree requirement, so a dirty local tree still passes while CI cannot ship a "modified" binary unnoticed.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./... && test -z "$(gofmt -l .)"` (Go 1.26.5) -- all pass.
- Same vet/test with Go 1.27.2, and `scripts/release-build.sh 0.0.0-ci <sha> <scratch>` with Go 1.27.2 -- pass; four binaries + SHA256SUMS.
- `HERDDASH_REQUIRE_TOOLS=systemd-analyze go test -run TestSystemdUnitVerifies .` on macOS -- fails naming the variable; `HERDDASH_REQUIRE_TOOLS=plutil go test -run TestLaunchdAgentLints -v .` -- passes, not skipped.
- `CI=true go test -run TestReleaseBuild .` on a clean tree -- passes; with an untracked file -- fails.
- `node --test internal/httpapi/web/*.test.mjs` -- pass; `ruby -ryaml -e 'YAML.load_file(".github/workflows/ci.yml")'` -- parses.
- `git diff 55b0bcf -- .github/workflows/ci.yml` -- additions only, between `board-browser` and `cross-compile`.

## Auto Run Result

Status: done

**Summary:** CI gains two self-contained jobs beside the untouched `check`, `board-browser` and `cross-compile`: `macos` (macos-latest, go.mod's Go, `go test -race ./...` with `HERDDASH_REQUIRE_TOOLS: plutil`) and `release-toolchain` (ubuntu-latest, Go 1.27.2 with `GOTOOLCHAIN: local`: vet, `go test -race ./...` with `HERDDASH_REQUIRE_TOOLS: systemd-analyze`, then `scripts/release-build.sh` with checksums, line count and `version` checked). Tests needing plutil or systemd-analyze fail instead of skipping when that variable lists the tool. `TestReleaseBuild` asserts every binary's `vcs.revision` equals HEAD and `vcs.modified` matches the tree, requiring a clean tree and git when `CI` is true. `TestCIWorkflow` pins the new jobs.

**Files changed:**
- `.github/workflows/ci.yml` -- new `macos` and `release-toolchain` jobs (additions only).
- `deploy_test.go` -- `requireTool`/`toolRequired`, used by `TestLaunchdAgentLints` and `TestSystemdUnitVerifies`; `TestToolRequired`, `TestRequireToolFailsWhenListed` (subprocess).
- `release_test.go` -- vcs checks in `TestReleaseBuild` via `releaseTreeState`/`judgeTree` (`TestJudgeTree`); `spineReleaseGo`; `workflowJobs`; `TestCIWorkflow`.
- `CLAUDE.md` -- Build and test describes the two jobs, `HERDDASH_REQUIRE_TOOLS` and the vcs checks.
- `docs/implementation/deferred-work.md` -- Story 1.9 (darwin reader on macOS CI) and Story 4.2 (plutil lint in CI) entries resolved.

**Review findings:** 23 findings (high 0, medium 2, low 16, false 4, maybe-false 1). Patched 8 entries: one medium (requireTool's fail branch untested; ci.yml's variable name untied to the constant) and lows (CI parsed as bool, GOTOOLCHAIN local, vet order pinned, git error names its command, checksum count from releaseTargets, env line bound to the test step, extra judgeTree rows). Deferred 1 (jobs unproven on GitHub runners, medium unverified). Rejected 9 lows and 4 false, each with its reason in the triage log.

**Follow-up review recommendation:** false — patched entries: high 0, medium 1, low 7 (by entry); a first pass needs a high or two mediums.

**Verification:** Go 1.26.5 and Go 1.27.2: `go build ./...`, `go vet ./...`, `go test ./...` ok; `gofmt -l .` empty; `node --test internal/httpapi/web/*.test.mjs` 75/75; ci.yml parses with jobs check, board-browser, macos, release-toolchain, cross-compile; diff of ci.yml against the baseline adds lines only. `scripts/release-build.sh 0.0.0-ci <sha> <scratch>` under Go 1.27.2 (GOTOOLCHAIN=local): four binaries, checksums OK, 4 lines, `herddash 0.0.0-ci (<sha>)`, build info carries vcs.revision=HEAD. `HERDDASH_REQUIRE_TOOLS=systemd-analyze` fails TestSystemdUnitVerifies on macOS naming the variable; `HERDDASH_REQUIRE_TOOLS=plutil` runs TestLaunchdAgentLints (PASS, not skipped). `CI=true HERDDASH_REQUIRE_TOOLS=plutil go test -race ./...` on the committed, clean tree: see the commit's check below.

**Residual risks:** The two jobs have not run on GitHub's runners (deferred; settled by the first PR run). Under `CI`, any future test that writes an untracked file into the repository fails TestReleaseBuild in every job.
