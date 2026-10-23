---
title: 'Story 5.4: Retry a Profile lookup that failed transiently'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: 'fc4573c12bd4713e580e10c93fc4921e1d2c160c'
review_loop_iteration: 0
followup_review_recommended: true
context:
  - '{project-root}/docs/implementation/epic-5-context.md'
  - '{project-root}/docs/implementation/epic-1-retro-2026-10-23.md'
warnings: ['oversized']
deferred:
  - summary: >-
      No user-facing document yet describes the Profile retry tunables or how rows are held while a Profile is resolved again.
    evidence: |-
      Found by the review of Story 5.4. Story 5.8 already documents the engine tunables with the configuration, so this belongs there.
    location: >-
      docs/how-herddash-works.md; internal/engine/tunables.go
    severity: low
---

<intent-contract>

## Intent

**Problem:** A Profile lookup that fails on a timeout or a herdr call error leaves the Agent
`unresolved` until herdr next detects it. Four neighbouring faults from the Epic 1 retrospective
share the code: a row written right after an Agent Program change carries the old Profile (L1, #1);
`agent_detected` for a new pane drops its program, so a status event naming none reports an Agent
with an empty program (L5, #4); a reconnect that keeps the epoch never re-resolves Profiles (L6,
#5); Profile Map values equal after `filepath.Clean` both pass validation (L7, #6).

**Size of the problem (operator log, read-only, 2026-10-21 11:04 to 2026-10-23 13:24):** 0
`profile resolved` debug entries with an error, because the service logs at Info and never writes
debug entries. The Info proxy agrees: 334 `agent seen` lines, none with Profile `unresolved`
(`cldw` 240, `cldp` 88, `claude` 3, `codex` 2, `oneio` 1), 0 `profile changed`. Transient failures
have not been observed; the retry is insurance, so it stays small and logs at Info when it acts.

**Approach:** A transient failure is retried off the actor, a bounded number of times with a
doubling wait from `tunables.go`; rule outcomes never are. A known Agent whose Profile is being
resolved again for a program change, a redetection or an epoch-keeping reconnect holds its new
rows until that resolution settles (at most `profileTimeout`), so each row carries the Profile
resolved for its program.

## Boundaries & Constraints

**Always:** retries and reads never block the engine goroutine (AD-2): a timer goroutine sends a
message, the read runs in a goroutine as today. New tunables live in `internal/engine/tunables.go`.
Rows keep the `observed_at`, provenance and previous Status they were observed with; only their
Profile is decided at settle. No row pairs a program with a Profile resolved for another program.
Matched values only are logged (FR-11 privacy rule unchanged).

**Never:** retry a rule outcome (absent, unmatched, no rule) or an environment-read error (lasting:
permission, no environment). Hold rows during a retry (only the first settle after a trigger ends a
hold). Change the stored schema, the Sink interface or `arch_test.go`'s permitted set. Touch
snapshot-failure or connection-loss handling (Story 5.12 owns it).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| First lookup times out, retry succeeds | new Agent, env read blocks once | reported `unresolved`, then Profile change `cldw previous unresolved` | Info log names attempt and wait |
| No foreground process, every retry fails | `procs` empty | reported `unresolved`; exactly 1 + `profileRetries` lookups; then stops | Warn once when giving up |
| Environment unreadable / unmatched / absent | EPERM, `/h/.other`, unset | one lookup, never retried | as today |
| Re-resolution of same program fails transiently | known `cldw`, redetect, env times out | keeps `cldw`, no Profile change; retry may change it | — |
| Program change on known Agent | `claude`(cldw) → status event `codex` + new Status | Profile change first, then the row `codex ... profile=codex` | — |
| Program change, rule program, rows during lookup | `codex` → `claude`, env held, two Status events | no row until settle; then both rows with the new Profile, original times | pane removed or Run ends first: rows flushed (`unresolved` for a program the held Profile was not resolved for) |
| agent_detected new pane, status event without program | detected `claude`, event program `""` | `agent w1:p4 claude blocked` | — |
| Epoch-keeping reconnect | Agent's env changed while disconnected | Profile change after the reconnect | transient failure keeps the old Profile |
| Profile Map `x` and `x/` | config | load fails naming `values."x/"` | `config.Error` |

</intent-contract>

## Code Map

- `internal/engine/engine.go` -- `pane` struct (~l.361 `profile/resolved/resolving`): add `profileProgram`, `profileTries`, `retryGen`, `holdRows`, `held []core.TransitionInput`. `profileMsg` (~l.504): add `transient bool`. `handle` (~l.916): add `profileRetryMsg`. `onLifecycle` new-pane branch (~l.970): store `ev.AgentProgram`. `apply` (~l.1292): re-resolve a known pane on `resubscribe` when its program did not change. `remove` (~l.1572): flush held rows first. `resolve` (~l.1654): trigger entry (resets tries/retry, sets hold for a known pane on the async path); split the read start into `startResolve`. `readProfile` (~l.1676): mark timeout and `ForegroundProcess` errors transient. `onProfile` (~l.1712): retry/keep logic. `settleProfile` (~l.1724): set `profileProgram`, then flush after the Profile change. `transitionAt` (~l.2228): validate, then hold or commit. `Run` defer (~l.603): flush held rows before `stopPublishing`.
- `internal/engine/tunables.go` -- add `defaultProfileRetries = 3`, `defaultProfileRetryDelay = 2s`, vars, `profileRetryWait(n)` (doubling, like `reconnectDelay`).
- `internal/engine/fake_test.go` -- `TestMain`: `profileRetryDelay = 10ms`.
- `internal/engine/engine_test.go` -- `TestProfileFailures` (l.1769): transient cases now assert 1+retries lookups and a Warn; environment-unreadable asserts one lookup after the retry window.
- `internal/engine/profile_test.go` -- new tests for this story (kept apart from 5.12's edits).
- `internal/config/config.go` (~l.576) -- clean every expanded value before the duplicate check; `config_test.go` l.234 table gains a relative case.
- Read-only: `internal/core/profile.go` (`ResolveProfile` cleans keys), `internal/engine/sink.go` (`Sink.Profile`: "Transitions after it carry the new one").

## Tasks & Acceptance

**Execution:**
- `internal/engine/tunables.go` -- add retry tunables and `profileRetryWait` -- bounds in one place.
- `internal/engine/engine.go` -- implement transient marking, retry timer + message, keep-on-transient for a same-program re-resolution, row hold/flush, detection program, reconnect re-resolve; update the Profile doc comment (~l.127) -- the story's behaviour.
- `internal/config/config.go` -- `filepath.Clean` every expanded value -- L7.
- `internal/engine/profile_test.go`, `engine_test.go`, `fake_test.go`, `internal/config/config_test.go` -- tests for every matrix row; `TestDetectedProgramKept` already written and failing before the fix (`agent w1:p4  blocked`).

**Acceptance Criteria:**
- Given any matrix row, when its test runs (also `-race -count=20`), then it passes.
- Given a held row and an engine shutdown, when `Run` returns, then the row was appended to the Store.
- Given the full suite, when `go build`, `go vet`, `go test ./...` and `gofmt -l .` run, then all pass and gofmt prints nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 28 findings — high 0, medium 3, low 15, false 10, maybe-false 0
- findings:
  - `[low]` `[reject]` (intent) "No foreground process" is treated as transient, not as a definitive herdr answer — lookups run only for Agent panes, where no process means a process swap is under way; at most 1+3 lookups and one Warn. A small, bounded cost against a real chance it passes.
  - `[low]` `[reject]` (intent) The hold also covers redetection and reconnect and freezes the view for at most `profileTimeout` — this is the spec's Approach; the latency is bounded and further limited by the E3 patch.
  - `[false]` (intent) Clause #5 took re-resolve rather than a documenting test — the AC allows either reading.
  - `[false]` (intent) Tests run against fakes, not the real herdr client or SQLite — the AC is at the engine level, and every engine test uses these fakes by design.
  - `[false]` (intent) The held-row guarantees come only from the spec — they are asserted by `TestProfileProgramChangeHeldRows` and `TestProfileHeldRowRecordedAtShutdown`, so no defect.
  - `[low]` `[reject]` (blind) The Profile change is printed with a later time than the held rows it governs — a Profile change is not a row in the record, only a printed line, and the rows keep their true observation times, as AD-4 requires.
  - `[low]` `[patch]` (blind) `release`'s comment says each row is reported "with the Agent as it was after it", but program and Profile are the current ones — the comment now says the Status, Class and Waiting Time are per row and the row's own program and Profile are on the Transition. The printer sink reads only `waiting(a)` from the Agent.
  - `[low]` `[patch]` (blind) `Agents()` shows a held Status while the published view stays frozen — documented on `pane.holdRows`; `TestProfileHeldView` asserts the view's behaviour.
  - `[false]` (blind) A hold started in `transitionAt` has nothing to bound it — every path that changes `p.program` goes through `setProgram`, which calls `resolve` for a known Agent and so guarantees a settle; the guard covers only the `followSession` row recorded before that call.
  - `[false]` (blind) `onLifecycle` sets `p.program` directly instead of calling `setProgram` — for an unreported pane with no lookup in flight, `setProgram`'s `followBackground` and `followSession` return early (`!p.known`, pid 0) and it starts no resolve, so the direct assignment is equivalent.
  - `[low]` `[patch]` (blind) The config duplicate message says "once expanded" when only cleaning made the values equal — the message now reads "once expanded and cleaned".
  - `[false]` (blind) A cancelled context at shutdown arms a retry and logs at Info — after `runCtx` ends, `send` returns false, so `onProfile` never runs.
  - `[low]` `[reject]` (blind) When reconnects flap, each pane can log one Warn per reconnect — it is bounded per pane per reconnect, and flapping is Story 5.12's subject.
  - `[low]` `[patch]` (blind) Missing tests for `profileRetryWait`, stale retries and the reset — added `TestProfileRetryWait` and `TestProfileRetryReset` (grouped with the verification-gap items).
  - `[low]` `[reject]` (blind) Tests depend on timing — `-race -count=20` passed for all Profile and Detected tests.
  - `[low]` `[defer]` (blind) The spec's logs were empty and no user-facing document describes the retry or hold — the logs fill at this stage; documenting the tunables and the hold belongs to Story 5.8 (deferred).
  - `[low]` `[reject]` (edge) After a redetect then a program change, the old program's rows are emitted after `sink.Profile` — this needs a program change within `profileTimeout` of a redetection; the rows carry the correct Profiles, and only the order of printed lines differs.
  - `[low]` `[patch]` (edge) `Agents()` shows a Status that is not yet recorded — grouped with the blind item on `Agents()` above; documented.
  - `[medium]` `[patch]` (edge) Repeated triggers supersede the lookup in flight, so a hold can outlast `profileTimeout` — an epoch-keeping reconnect no longer re-resolves a pane with a lookup in flight (`p.resolving == 0` guard in `apply`). `TestProfileReconnectDuringRedetect` now asserts no extra lookup. Repeated `agent_detected` still supersedes, since a new detection can mean a new process.
  - `[low]` `[reject]` (edge) Rows released by keep or removal while naming an unresolved program stay `unresolved` even if a later retry resolves it — the record is append-only, and the spec rule is never to pair a row with a Profile that was not resolved for its program.
  - `[low]` `[patch]` (edge) The Agent passed per row names the new program and Profile — same as the blind `release` comment item; comment corrected.
  - `[medium]` `[patch]` (edge) The spec's "a hold ends within `profileTimeout`" fails under repeated triggers — same root cause as the medium edge item above; patched there.
  - `[low]` `[patch]` (verification) `noteView`'s early return during a hold was untested — added `TestProfileHeldView`.
  - `[medium]` `[patch]` (verification) `release`'s old-program branch was never exercised — added `TestProfileHeldOldProgramRow`.
  - `[medium]` `[patch]` (verification) `resolve`'s reset of tries and retry, and `onProfileRetry`'s stale drop, were untested — added `TestProfileRetryReset` ("after giving up" and "stale retry").
  - `[low]` `[patch]` (verification) `profileRetryWait`'s doubling was unasserted — added `TestProfileRetryWait`.
  - `[low]` `[patch]` (verification) `TestProfileReconnectDuringRedetect`'s doc comment was stale — the E3 guard makes it true again; it now says so and asserts it.
  - `[low]` `[patch]` (verification) `e.agents()` and the published View disagree during a hold — documented on `pane.holdRows`, the same as the blind `Agents()` item.

## Design Notes

Retry: `onProfile` with `m.transient` increments `profileTries`; while `tries <= profileRetries` it
arms `time.NewTimer(profileRetryWait(tries))` in a `wg` goroutine that sends
`profileRetryMsg{paneID, gen}`; a matching `retryGen` starts `startResolve` without resetting tries
or holding rows. Any trigger (`resolve`) clears `retryGen` and tries, so a stale timer is ignored.
Success or a non-transient outcome resets tries.

Keep: `p.resolved && p.profileProgram == p.program` on a transient failure means the Agent already
holds a Profile for this program; a failure is no evidence it changed, so the Profile and pid stay.
This makes the reconnect re-resolve (L6) safe on a flaky herdr.

Hold rationale: the AC reads "resolved again before the next row is written"; writing `unresolved`
meanwhile (the retro's alternative) would put a false Profile in the append-only record. A hold
ends within `profileTimeout` because every lookup ends with a message.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- expected: pass
- `gofmt -l .` -- expected: no output
- `go test -race -count=20 -run 'Profile|Detected' ./internal/engine` -- expected: pass

## Auto Run Result

Status: done

**Summary.** Transient Profile lookup failures are now retried: a timeout or a failed herdr
foreground-process call gets up to 3 retries, waiting 2s, then 4s, then 8s (`tunables.go`). A timer
goroutine sends a message for each retry, so nothing waits on the engine goroutine. Rule outcomes
and an unreadable environment are never retried. If an Agent already holds a Profile resolved for
its program, a transient failure leaves that Profile in place.

While a reported Agent's Profile is being resolved again, its new rows are held until the lookup
settles, so every row carries the Profile resolved for its own program. This covers a program
change, a redetection and an epoch-keeping reconnect. The four Epic 1 retro items are done:
- #1: rows are held as described above.
- #4: `agent_detected` stores the new pane's program. The test was written first and reproduced
  the fault (`agent w1:p4  blocked`).
- #5: a reconnect that keeps the epoch re-resolves every Agent that has no lookup in flight.
- #6: every Profile Map value is cleaned before the duplicate check.

**Operator log sizing** (read-only, 2026-10-21 to 2026-10-23): 0 `profile resolved` entries with
an error. The service logs at Info, so debug entries are never written. As an Info-level proxy:
of 334 `agent seen` lines, 0 had Profile `unresolved`, and there were 0 `profile changed` lines.

**Files**
- `internal/engine/engine.go`: the retry and its message, keeping the Profile on a transient
  failure, holding and releasing rows, the detected program, and re-resolving on reconnect.
- `internal/engine/tunables.go`: `profileRetries`, `profileRetryDelay`, `profileRetryWait`.
- `internal/engine/view.go`: the published view is not updated while rows are held.
- `internal/config/config.go`: every Profile Map value is cleaned before the duplicate check, and
  the error message now says so.
- `internal/engine/profile_test.go`: new tests for every row of the matrix, plus the review gaps.
- `internal/engine/engine_test.go`: `TestProfileFailures` covers retries;
  `TestProfileReconnectDuringRedetect` asserts the reconnect starts no extra lookup.
- `internal/engine/fake_test.go`: `TestMain` shortens the retry delay for tests.
- `internal/config/config_test.go`: a case for a relative duplicate.

**Review.** 28 findings:
- 3 medium: patched.
- 15 low: 10 patched, 5 rejected with reasons in the triage log.
- 10 false.
- 1 low deferred: user-facing documentation, which belongs to Story 5.8.

What the patches did:
- An epoch-keeping reconnect no longer supersedes a lookup already in flight, so it cannot restart
  a hold.
- Comments now say what `release` and the hold actually report.
- The config error message mentions cleaning.
- New tests: `TestProfileRetryWait`, `TestProfileHeldView`, `TestProfileHeldOldProgramRow`,
  `TestProfileRetryReset`.

**Follow-up review recommended: true.** Patched entries by verdict: high 0, medium 3, low 10. The
unverified risk is how holds and retries interact under repeated `agent_detected` triggers. Each
one supersedes the lookup in flight, so a hold can outlast `profileTimeout` while they keep
arriving.

**Verification.** Run after merging `feat/epic-5-after-0.5.0` (a58b1e3, which includes 5.12):
- `go build ./...`, `go vet ./...`, `go test ./...` and `go test -race ./...` all pass.
- `gofmt -l .` prints nothing.
- `go test -race -count=20 -run 'Profile|Detected' ./internal/engine` passes (240s).

**Residual risks**
- While rows are held, the Board shows an Agent's last released state for up to `profileTimeout`
  (2s). After a reconnect this applies to every Agent with a rule.
- `Agents()` shows the held Status before the view does.
- If a redetection is followed by a program change within `profileTimeout`, the Profile-change
  line is printed before the old program's held row.
