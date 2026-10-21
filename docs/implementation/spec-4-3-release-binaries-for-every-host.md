---
title: 'Story 4.3: Release binaries for every Host'
type: 'feature'
created: '2026-10-21'
status: 'done'
baseline_commit: '829af674b5b466087e075dcfebbc57600ec46626'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A release produces only a tag and a changelog, so the second and third machines must
build herddash themselves (NFR8).

**Approach:** A separate release workflow builds a static binary for each of darwin/arm64,
darwin/amd64, linux/amd64 and linux/arm64 with the version stamped in, and attaches them with
SHA-256 checksums to the GitHub release release-please created. The build is a script that runs the
same way locally. Merging the held release-please PR #2 stays the operator's decision.

## Boundaries & Constraints

**Always:**
- **Trigger:** the release-please workflow creates its tag and release with `GITHUB_TOKEN`, and
  GitHub does not start other workflows from events that token causes, so a workflow on `push:
  tags` or `release: published` would never run. The release workflow therefore runs `on:
  workflow_run` of the release-please workflow (completed, success, on `main`), finds a `v*` tag
  pointing at that run's head commit with a GitHub release, and does nothing when there is none.
  It also runs on `workflow_dispatch` with a `tag` input, for re-running a release.
- **Separation:** the release-please workflow still never publishes artifacts (its comment is
  corrected to name the workflow_run mechanism); the release workflow never creates tags or
  releases, only attaches assets to an existing release.
- **Build script** `scripts/release-build.sh <version> <commit> <outdir>`: for each target, `GOOS`/
  `GOARCH` set, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X main.version=<version>
  -X main.commit=<commit>"`, output `herddash_<version>_<os>_<arch>` (version without the `v`),
  then one `SHA256SUMS` file covering every binary. It fails on any error and writes nothing
  outside `<outdir>`.
- **Toolchain:** Go 1.27.2 per the spine's Stack (needs macOS 13 or newer), via the SHA-pinned
  `actions/setup-go` already used in CI; `actions/checkout` likewise pinned. No new third-party
  action: assets are uploaded with `gh release upload --clobber` using `GITHUB_TOKEN` with
  `contents: write` only.
- **Verification in the workflow:** before uploading, run the linux/amd64 binary's `version` and
  require it to print the release version and commit, and check `sha256sum -c SHA256SUMS`.
- **Docs:** `docs/install.md`'s download path describes the real asset names and checksum file;
  CLAUDE.md's release and `version` text says the release workflow exists and what it does.
- **Tests:** a Go test runs `scripts/release-build.sh` into a temp dir for the host target only
  (or all four when not short), checks the four names and the checksum file, and that the host
  binary prints the stamped version and commit. Workflow files are checked structurally: the
  release workflow's triggers, permissions (`contents: write` only), pinned actions, Go version,
  and that it never creates a tag or release.

**Never:**
- Merge release-please PR #2 or create a tag or release while building this. That is the
  operator's checkpoint (sprint-status action item for Story 4.3): only after their day of
  dogfooding, after checking the PR proposes a 0.x version, and with their explicit approval.
- Use a personal access token, add a dependency, or sign or notarise binaries.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Local build | `scripts/release-build.sh 0.1.0 abc1234 out` | four binaries `herddash_0.1.0_<os>_<arch>` and `SHA256SUMS` | non-zero on failure |
| Stamped | the host binary's `version` | `herddash 0.1.0 (abc1234)` | test fails |
| Release run | release-please run on main created `v0.1.0` | assets attached to release `v0.1.0` | job fails |
| Ordinary push | release-please run with no new tag | workflow ends with nothing to do | none |
| Re-run | `workflow_dispatch` with `tag: v0.1.0` | assets rebuilt and replaced | job fails if no such release |

</frozen-after-approval>

## Code Map

- `.github/workflows/release-please.yml` -- release-please action v5.0.0 pinned by SHA; its comment claims a tag-triggered workflow, which `GITHUB_TOKEN` would never start; correct it
- `.github/workflows/ci.yml` -- pinned `actions/checkout` (`3d3c42e5aac5ba805825da76410c181273ba90b1`, v7.0.1) and `actions/setup-go` (`b7ad1dad31e06c5925ef5d2fc7ad053ef454303e`, v7.0.0) to reuse
- `release-please-config.json` -- `include-component-in-tag: false`, so tags are `vX.Y.Z`
- `cmd/herddash/main.go` -- `version` (default `dev`) and `commit` are the ldflags targets; `herddash version` prints `herddash <version> (<commit>)`
- `docs/install.md` -- the "download a release binary" path written in Story 4.2, marked as pending this story
- `CLAUDE.md` "Releases" and "Build and test" -- say no artifact workflow exists and that stamping is not wired; update both
- Spine Stack: release toolchain Go 1.27.2 (1.27.3 is also published; the pin is the spine's)
- `deploy_test.go` (module root) -- where repository-level file checks live; add the release checks beside it
- Push over SSH: the `gh` token lacks the `workflow` scope needed to push workflow changes over HTTPS

## Tasks & Acceptance

**Execution:**
- [x] `scripts/release-build.sh` -- the build and checksum script
- [x] `.github/workflows/release.yml` -- workflow_run and workflow_dispatch, build, verify, upload
- [x] `.github/workflows/release-please.yml` -- correct the comment
- [x] `release_test.go` (module root) -- script and workflow checks
- [x] `docs/install.md`, `CLAUDE.md` -- describe the real release

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then the release checks pass and the script builds all four targets with the stamped version.
- Given release-please PR #2 merged by the operator, when its release-please run completes, then the release workflow attaches four binaries and `SHA256SUMS` to the release (checked after the operator's merge).

## Implementation Notes

- Acceptance criterion 2 (assets attached to the first real release) can only be checked after the operator merges release-please PR #2; the story stays `review` in sprint-status until then. `actionlint` v1.7.7 passes on all three workflows.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH | One concurrency group: a newer pending run cancels the pending run that carries the tag | medium | GitHub keeps one pending run per group; the release would get no binaries; patch: a group per commit or tag |
| 2 | BH | Any `gh release view` failure (token, rate limit, network) reads as "no release, nothing to do" | medium | `>/dev/null 2>&1` hides the cause; patch: only not-found means no release, anything else fails |
| 3 | BH | Requiring release-please's run to succeed skips a release it created before a later step failed | medium | The lookup already decides; patch: run on any completed conclusion |
| 4 | ECH | A release whose tag is on an earlier commit than the run's head is never found | low | Needs a release created by a later run; patch the docs: the manual rebuild is the recovery |
| 5 | BH, ECH | Only the linux/amd64 binary's stamping is verified | medium | Patched with a deviation: `-trimpath` (required) makes Go omit `-ldflags` from build info, so every binary is checked instead for GOOS, GOARCH, `CGO_ENABLED=0`, `-trimpath`, the tagged commit as `vcs.revision` and `vcs.modified=false` (workflow: `go version -m`; test: `debug/buildinfo`); the stamped version string is proven by running linux/amd64 (workflow) and the host binary (test), all four built from one ldflags value |
| 6 | BH | A manual rebuild silently replaces assets and their checksums | medium | Toolchain drift changes hashes users verified; patch: refuse to replace existing assets unless a `force` input is set |
| 7 | ECH | A partial `--clobber` upload leaves old and new assets mixed | low | Patch: upload `SHA256SUMS` last, in its own call |
| 8 | BH, ECH | The guide's `shasum -c --ignore-missing` can pass having checked nothing | medium | Patch: check the one matching line, which fails when absent |
| 9 | BH | The download path needs an authenticated `gh` | low | Public repository; patch: a plain `curl` alternative |
| 10 | BH | The guide offers downloads before any release has binaries | low | Patch: say they start with the first release, and to build until then |
| 11 | VG | The tag-finding and gating logic runs in no test | medium | It decides whether the one-shot first release gets binaries; patch: move it to a script tested against a temporary git repository with a stub `gh` |
| 12 | VG | The unsafe-commit test fails for the linker, not the guard | medium | VG pre-verified: the linker rejects `abc 1234` anyway; patch: inputs the linker accepts (`abc/1234`, `abc -X main.version=evil`) |
| 13 | BH, ECH | Script and tag validation disagree (`v` alone, `..`, a leading `-`, `+`); a `+` may be renamed in asset names | low | Patch: one rule, `vX.Y.Z` with an optional `-prerelease`, no `+`, in both |
| 14 | ECH | `HERDDASH_TARGETS` that is blank or lists a target twice | low | Blank hashes stdin into a bogus `SHA256SUMS`; patch: reject both |
| 15 | ECH | A stale `SHA256SUMS` survives a failed rebuild in the same directory | low | Patch: remove it before building |
| 16 | ECH | Rejected-argument cases reuse an existing outdir | low | Patch: a fresh outdir that must not be created |
| 17 | BH | The workflow test does not assert the `push` event guard and is easy to bypass | low | Patch: assert it |
| 18 | ECH | "Static" is untrue for macOS, which links libSystem | low | Patch the wording: built with `CGO_ENABLED=0` |
| 19 | BH | Every CI run builds all four release binaries | low | Cost accepted: it keeps the cross-target build tested; rejected |
| 20 | BH | Spec/tracker status mismatch; acceptance 2 is checked only after the operator's merge | false | Workflow ordering; the acceptance criterion says so itself |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `scripts/release-build.sh 0.0.0-test deadbee /tmp/hd-release && (cd /tmp/hd-release && shasum -a 256 -c SHA256SUMS)` -- expected: four binaries, checksums OK
