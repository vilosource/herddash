---
title: 'Story 5.6: CI runs the Board''s browser check'
type: 'chore'
created: '2026-10-23'
status: 'done'
baseline_revision: 'fca738fdb4141789ca26654a93a213096041c7a6'
review_loop_iteration: 0
followup_review_recommended: false
context: []
warnings: ['oversized']
deferred:
  - summary: >-
      The board-browser job has not yet run on an ubuntu-latest runner; whether the runner's google-chrome starts headless with its sandbox (Ubuntu 24.04's AppArmor userns restriction) is unproven.
    evidence: |-
      Unverified (would be medium: the job would be red on its first run, visibly, not silently green). Every run so far was local on macOS. Settle it with the first pull-request run of the epic branch; if Chrome refuses to start, the error now carries Chrome's stderr and exit code, and the fix is a runner-side setting or a CI-only flag decided then.
    location: >-
      .github/workflows/ci.yml board-browser job
    severity: medium (unverified)
  - summary: >-
      No branch protection or ruleset makes CI checks required, so a red board-browser job (like any red CI job) does not block a merge into develop or main.
    evidence: |-
      Reported by the verification-gap reviewer: `gh api repos/vilosource/herddash/branches/develop/protection` returns 404 "Branch not protected" and the rulesets list is empty. Pre-existing repository setting covering every job, outside this story; an operator decision if wanted.
    location: >-
      GitHub repository settings (vilosource/herddash)
    severity: low
---

<intent-contract>

## Intent

**Problem:** `internal/httpapi/web/check/board-browser.mjs` is the only test of the Board's drawing (strips, dimming, recap rows, live updates, reconnect), and it runs only by hand, so removing the `.board--recaps` CSS rule or breaking a strip leaves CI green (deferred-work entries from Stories 3.9 and the recap spec).

**Approach:** Add a self-contained `board-browser` job to `.github/workflows/ci.yml` on ubuntu-latest that builds `./cmd/herddash` and runs the existing check against the runner's preinstalled Google Chrome with the runner's Node (no npm, no new action), then stop calling the check hand-run in CLAUDE.md, the check's header and code comments, and close the two deferred entries.

## Boundaries & Constraints

**Always:** Reuse the exact `actions/checkout` and `actions/setup-go` SHAs the `check` job pins (release_test.go's `pinnedActions` fails on two SHAs for one action, and `TestReleaseWorkflow` compares the release workflow's pins against CI's). The job is its own `jobs:` entry so Story 5.7 can add a macOS job beside it without editing it. The step fails loudly, naming the cause, when Node is older than 22 / has no global `WebSocket`, or when no Chrome is found. The check keeps running only against its own fake herdr with its own HOME; the scratch binary goes under `$RUNNER_TEMP`.

**Never:** Add `actions/setup-node`, a browser-install action or any npm package; change the existing `check` or `cross-compile` jobs; embed `web/check/` in the binary; add `--no-sandbox` or other Chrome flags pre-emptively; touch the live herdr or the running herddash (port 7318) when verifying locally; edit `sprint-status.yaml`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| healthy Board | current tree, runner Node 22.x, `google-chrome` on PATH | every check prints `ok`, step exits 0 | none |
| recap rule removed | `.board--recaps .box` rule deleted from `board.css`, binary rebuilt | "while a Box has a recap, every Box is one line taller" prints FAIL, script exits 1, job fails | job red |
| Node too old | `node` major < 22 or no global WebSocket | step prints the version and why, exits 1 | job red |
| no Chrome | `google-chrome` not on PATH and `CHROME` unset | step prints that no Chrome was found, exits 1 | job red |
| check throws | e.g. Chrome never prints a DevTools endpoint | script prints the error, exits 1 (existing `main().then` handler) | job red |

</intent-contract>

## Code Map

- `.github/workflows/ci.yml` -- jobs `check` (gofmt/build/vet/`go test -race`/board node tests) and `cross-compile`; pins: checkout `3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1`, setup-go `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0` with `go-version-file: go.mod`. The "board tests" step shows the house style for a Node guard (`node -p 'process.versions.node.split(".")[0]'`).
- `internal/httpapi/web/check/board-browser.mjs` -- header lines 1-14 say "Hand-run ... Not embedded, not run in CI"; `CHROME` env var defaults to the macOS path (line 27); usage `node board-browser.mjs <herddash binary>`; exits 1 on any FAIL or thrown error. Runs in ~9 s locally (23 checks).
- `internal/httpapi/web/board.css:199` -- `.board--recaps .box { height: …; grid-template-rows: … }`; CSS is embedded (`internal/httpapi/board.go` `//go:embed`), so the deliberate-failure proof needs a rebuild after the edit.
- `internal/httpapi/board.go:11-13`, `internal/httpapi/board_test.go:166-168` -- comments say "hand-run checks" for `web/check/`; reword (comments only).
- `CLAUDE.md:186-190` ("Build and test": what CI runs) and `:224-227` ("Repository state": "a hand-run headless-Chrome check") -- update both.
- `docs/implementation/deferred-work.md` -- entries with source `spec-3-9-connection-and-version-strips.md` ("Run the Board's headless-browser check … in CI") and `spec-claude-recap.md` ("The recap row's layout … run it in CI") get a `resolved:` line like Story 5.2's.
- `release_test.go:495-511,566-580` -- read-only constraint: every `uses:` pinned to a 40-hex SHA, one SHA per action across a file, CI pins equal release pins.
- Evidence: CI run 39784117354 (2026-10-22, image ubuntu-24.04 20261018) printed Node `v22.23.3`; ubuntu runner images ship Google Chrome as `google-chrome`.

## Tasks & Acceptance

**Execution:**
- `.github/workflows/ci.yml` -- add job `board-browser` (ubuntu-latest, timeout-minutes 10, no `needs`): checkout and setup-go with the same pins; one step that sets `-eu`, checks Node ≥ 22 with a global `WebSocket`, resolves Chrome (`CHROME` or `command -v google-chrome`), prints both versions, `go build -o "$RUNNER_TEMP/herddash" ./cmd/herddash`, then `CHROME=… node internal/httpapi/web/check/board-browser.mjs "$RUNNER_TEMP/herddash"`; comment that it is the Board's drawing check and that removing `.board--recaps` fails it.
- `internal/httpapi/web/check/board-browser.mjs` -- header: run by CI's `board-browser` job on ubuntu and by hand; keep usage; mention `CHROME=$(command -v google-chrome)` on Linux. No behaviour change.
- `internal/httpapi/board.go`, `internal/httpapi/board_test.go` -- reword "hand-run checks" to "browser check(s)".
- `CLAUDE.md` -- Build and test: CI's `board-browser` job runs the check; Repository state: drop "hand-run".
- `docs/implementation/deferred-work.md` -- mark the two entries resolved, Story 5.6, naming the job.

**Acceptance Criteria:**
- Given a pull request, when CI runs on ubuntu-latest, then the `board-browser` job runs the browser check against the runner's Chrome with no npm packages and no new action.
- Given the `.board--recaps .box` rule removed and the binary rebuilt, when the job's command runs, then it exits non-zero with a FAIL line for the recap height check.
- Given CLAUDE.md, when read, then it no longer calls the check hand-run.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 19 findings — high 0, medium 2, low 13, false 2, maybe-false 2
- findings:
  - `[low]` `[patch]` (intent-alignment) CI claims (recap rule fails the job) are proven only by a local macOS run, yet deferred-work states them as CI fact — reworded both `resolved:` lines to say the job's step was run locally (Chrome 156, Node 22: 23 ok; rule removed: FAIL, exit 1) and that the first PR run proves it on ubuntu-latest.
  - `[low]` `[reject]` (intent-alignment) No test pins the board-browser job in ci.yml — real, but deleting the job is not an everyday mistake and the fix adds a new structural test; not a direct correction.
  - `[low]` `[defer]` (intent-alignment) "Cannot break with CI green" depends on CI being a required check — no branch protection or ruleset exists; pre-existing repository setting for every job, deferred.
  - `[maybe-false]` `[defer]` (verification-gap) The Linux path, incl. Chrome's sandbox under Ubuntu 24.04 AppArmor, has never run — cannot be settled without a GitHub run; deferred as medium (unverified), settled by the first PR run.
  - `[medium]` `[patch]` (verification-gap) A Chrome start-up failure shows only "timed out waiting for DevTools endpoint", dropping Chrome's stderr — `chrome()` now appends Chrome's exit code/signal and stderr to that error; proven with a fake Chrome exiting 3 with a sandbox message.
  - `[low]` `[defer]` (verification-gap) CI does not block merges (no branch protection) — same root as the intent-alignment required-check row; deferred.
  - `[medium]` `[patch]` (edge-case) Chrome exiting early yields only a 20 s timeout with stderr discarded — same root and fix as the verification-gap stderr row.
  - `[false]` `[reject]` (edge-case) `CHROME` set to a missing path exits on a generic shell error — the step's `"$chrome" --version` fails under `set -e` with the shell's "No such file or directory" naming the path, which names the cause; CI never sets CHROME.
  - `[low]` `[patch]` (blind) No CI run backs the claims; deferred-work states outcomes as fact — same root and fix as the first intent-alignment row.
  - `[maybe-false]` `[defer]` (blind) Linux Chrome sandbox risk not addressed — same as the verification-gap Linux row; deferred.
  - `[low]` `[patch]` (blind) Chrome failure hard to diagnose (stderr never printed) — same fix as the stderr rows.
  - `[low]` `[reject]` (blind) No guard against zero checks running — `main()` runs every check unconditionally in sequence and any throw exits 1; skipping needs a code change to the check itself; the guard would add parsing logic for an unlikely refactor.
  - `[low]` `[reject]` (blind) No test pins the new job — same as the intent-alignment row; rejected for the same reason.
  - `[low]` `[patch]` (blind) CLAUDE.md "Build and test": the new sentence took the colon introducing the Go command block, so the block read as the browser job's commands — restored the Go sentence's colon, moved the board-browser paragraph after the block and added the local invocation (with the Linux CHROME prefix). CLAUDE.md is agent context, but the orchestrator's brief and this spec mandated editing this paragraph, so the defect this diff introduced was corrected in place rather than deferred.
  - `[low]` `[reject]` (blind) CHROME should default by `process.platform` — CI and the header name the variable; changing the default adds a branch for convenience only.
  - `[false]` `[reject]` (blind) Other deferrals waiting on 5.6 (spec-5-13's bdi gap) are left open — spec-5-13's frontmatter `deferred` list is append-only and already names Story 5.6 as its closer; the ledger is deferred-work.md, which has no bdi entry; the check's "sits in a <bdi>" assertion now runs in CI.
  - `[low]` `[reject]` (blind) Coverage lists in the job comment / resolved line are not exhaustive (layout, bg counts, bdi, refused hiding) — the lists are not wrong; "refused: red strip …, no Boxes" covers refused hiding; cosmetic.
  - `[low]` `[reject]` (blind) Node guard has a redundant WebSocket check — harmless belt-and-braces with its own named message; removing it gains nothing a user meets.
  - `[low]` `[reject]` (blind) Child serve processes are not stopped when a check throws — pre-existing in the check; on CI the runner is discarded, locally the processes exit when their socket or data dir goes; low and unlikely to bite.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./... && test -z "$(gofmt -l .)"` -- expected: all pass (release_test.go's pin checks included).
- `node --test internal/httpapi/web/*.test.mjs` -- expected: pass.
- The job's script run locally with `RUNNER_TEMP` set to a scratch dir inside the worktree and `CHROME` set to the macOS Chrome -- expected: 23 `ok`, exit 0.
- Same, after deleting the `.board--recaps .box` rule (then restoring it, `git diff --exit-code internal/httpapi/web/board.css`) -- expected: FAIL line, exit 1.
- `ruby -ryaml -e 'YAML.load_file(".github/workflows/ci.yml")'` (or equivalent) -- expected: parses.

## Auto Run Result

Status: done

**Summary:** CI gains a self-contained `board-browser` job (ubuntu-latest, 10 min, no `needs`) that builds `./cmd/herddash` into `$RUNNER_TEMP` and runs `internal/httpapi/web/check/board-browser.mjs` against the runner's `google-chrome` with the runner's Node (≥ 22, global WebSocket), with no npm and no new action; it reuses the `check` job's checkout and setup-go SHAs. The check, CLAUDE.md and code comments no longer call it hand-run, and the two deferred-work entries it closes are marked resolved.

**Files changed:**
- `.github/workflows/ci.yml` -- new `board-browser` job with Node/Chrome guards that name the cause.
- `internal/httpapi/web/check/board-browser.mjs` -- header says CI runs it; a DevTools-endpoint failure now reports Chrome's exit code and stderr.
- `internal/httpapi/board.go`, `internal/httpapi/board_test.go` -- comments: "browser check" instead of "hand-run checks".
- `CLAUDE.md` -- Build and test describes the job and the local invocation; Repository state drops "hand-run".
- `docs/implementation/deferred-work.md` -- Story 3.9 and recap-spec entries resolved (local evidence; first PR run proves ubuntu).

**Review findings:** 19 findings. Patched 4 entries (one medium: Chrome stderr on start-up failure; lows: deferred-work wording, CLAUDE.md colon/placement — the low rows sharing roots are listed in the triage log). Deferred 2 (Linux runner unproven, medium unverified; no required checks, low pre-existing). Rejected 9 lows / 2 false, each with its reason in the triage log.

**Follow-up review recommendation:** false — patched entries: high 0, medium 1, low 2 (by entry); first pass needs a high or two mediums.

**Verification:** `go build ./...`, `go vet ./...`, `go test ./...` all ok (incl. release_test.go pin checks); `gofmt -l .` empty; `node --test internal/httpapi/web/*.test.mjs` 75/75; ci.yml parses. The job's step, extracted from ci.yml and run with RUNNER_TEMP in the worktree and CHROME set to macOS Chrome 156 / Node 22.23.2: 23 ok, exit 0. With the `.board--recaps .box` rule deleted (binary rebuilt by the step): FAIL "recap Box … clamped to two lines" and FAIL "while a Box has a recap, every Box is one line taller", exit 1; board.css restored byte-identical. Guards: fake Node 20 → "node 20 is older than 22 …", exit 1; no Chrome → "no Chrome found …", exit 1; fake Chrome exiting 3 → error includes exit code and stderr, exit 1.

**Residual risks:** The job has not run on ubuntu-latest; Chrome's sandbox there is the main unknown (deferred, settled by the first PR run). CI checks are not required for merges (repository setting).
