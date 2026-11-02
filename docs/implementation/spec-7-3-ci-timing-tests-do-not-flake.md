---
title: 'Story 7.3: CI''s timing tests do not flake'
type: 'chore'
created: '2026-11-02'
status: 'done'
route: 'oneshot'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Three tests failed once each on a Linux runner under `-race` and passed on rerun, costing reruns on PR #64 (deferred-work entry from Story 7.1). Root causes, found by stress and fault injection on 2026-11-02, are test races, not production defects:

1. `TestProfileHoldDeadline/transient_failure_after_the_deadline` (`internal/engine/profile_test.go:557-582`): the last redetection's lookup times out after the package's 200 ms `profileTimeout` (`fake_test.go:933`) and the "kept" branch of `onProfile` (`engine.go:2067-2071`) releases the held row; the test must assert the row is held within those 200 ms. An injected 250 ms stall reproduces the CI message 3/3.
2. `TestAckRefusedDuringProfileHold` (`acknowledge_test.go:~221-226`) and its sibling `TestAckUndoRefusedDuringProfileHold` (`~257`): a redetection also requests a snapshot (`engine.go:1160-1163`); when it applies after the test reads `seq`, `LastSnapshotAt` changes and `publish` bumps the view seq. 10/1200 natural failures under CPU load, 5/5 forced with `h.fake.holdNext()`.
3. `TestNoArgumentsServes` (`cmd/herddash/serve_test.go:~615-660`): its 300 ms context covers store open with seven migrations under `-race`, so it can expire before the first connect; the CI log shows `starting`, `listening`, `stopped` and no attempt.

**Approach:** Test-only changes that keep every assertion: (1) `setProfileTimeout(t, time.Second)` at the top of that subtest, as its siblings at `profile_test.go:534,586` do; (2) in both ack tests, note `View().Connection.LastSnapshotAt` before `env.hold()` and, after `awaitProcessCalls(2)`, wait (with the package's existing wait/deadline helper) until it changes before reading `seq`; (3) run `run` in a goroutine with a cancellable context, poll the log for "herdr not reachable" (5 ms, 10 s deadline), then cancel and assert the exit code, the line and its level as now. No production code changes. Then mark the deferred-work entry `resolved:`.

</frozen-after-approval>

## Implementation Notes

- Root causes came from a read-only investigation on 2026-11-02 that stress-ran and fault-injected each test in a scratch copy: (1) a 250 ms `time.Sleep` after the subtest's Status event gave "a row was not held" 3/3 before the fix and passed 3/3 after; (2) `h.fake.holdNext()` on the redetection's snapshot gave "view seq 5, want 4" 5/5 before, and the fixed tests had 0 failures in 1200 runs under 30 CPU hogs (10/1200 before, 60 hogs); (3) a 320 ms sleep before `store.Open` gave the CI log (starting, listening, stopped) 2/2 before and passed 5/5 after. Nothing injected was committed.
- Found a fourth instance of (2): `TestAckUndoRefusedDuringProfileHold` failed the same way (6 of those 10 failures); fixed with it through one helper, `awaitSnapshotAfter` (`acknowledge_test.go`). The package's publication watcher needs a subscription these tests do not open, so it was not reused.
- (3) uses `serve_test.go`'s `waitFor` (5 s, fails naming what it waited for) rather than a bare loop.
- Stress on macOS after the fix: `TestProfileHoldDeadline/transient` `-race -count=20 -cpu=1,4,8` (60 runs, 247 s; each run is ~4 s with the 1 s timeout), both ack tests and `TestNoArgumentsServes` `-race -count=50 -cpu=1,4,8` (150 runs each). All passed. The Linux evidence is CI's own run of this pull request; no separate Linux stress run was made.
- Files: `internal/engine/profile_test.go`, `internal/engine/acknowledge_test.go`, `cmd/herddash/serve_test.go`; docs `deferred-work.md`, `epics.md` (Story 7.3's wording now admits widening a timeout the test sets), `CLAUDE.md` (story count).

## Verification

- `go test -race -count=50 -cpu=1,4,8 ./internal/engine -run 'TestProfileHoldDeadline|TestAckRefusedDuringProfileHold|TestAckUndoRefusedDuringProfileHold'` and `go test -race -count=50 -cpu=1,4,8 ./cmd/herddash -run TestNoArgumentsServes` pass.
- Each fixed test now passes with its race forced (a 250 ms stall in (1), a `holdNext()` snapshot in (2), a 320 ms slow start in (3)) — checked once by hand, not committed. That each still fails when the behaviour it covers is broken was not checked by injection: every assertion is unchanged, and the added waits fail loudly on their own deadline (UNVERIFIED by a mutation run).
- Full `go build/vet/test -race ./...`, `gofmt -l .` empty.

## Review Triage Log

Blind Hunter, one pass:

- Widening the timeout is not "waiting for the event or driving the timer" as Story 7.3 says — low, real: patched the story text to admit widening a timeout the test itself sets.
- The 1 s value depends on the package's 200 ms in another file — low; rejected: the comment names it and the run time was measured.
- Duplicated wait loop ignores existing helpers — low, real: patched into one helper; the watcher needs a subscription these tests lack.
- `TestNoArgumentsServes` hand-rolls a loop that times out silently and rebuilds the path — low, real: patched to `waitFor` and one `logPath`.
- Verification commands lack `-timeout` — low; rejected: the runs used `-timeout 20m`; fixing it edits the frozen spec's commands only cosmetically.
- No Linux stress run — low; rejected: root causes were shown by injection; CI's Linux jobs on the pull request are the Linux evidence, noted above.
- Proof that each test still catches a failure is not recorded — low, real: recorded in Implementation Notes.
- The `resolved:` line claims more than shown — low, real: reworded to what was reproduced.
- `CLAUDE.md` story count stale — low, real: updated to 60 and 5 for Epic 7.
- Stories 7.4 and 7.5 leave out routes, an observable stall, the timeout values, and catch-up edge cases — low; rejected here: carried into those stories' specs when they are built.
