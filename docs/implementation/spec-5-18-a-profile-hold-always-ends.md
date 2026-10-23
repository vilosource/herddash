---
title: 'Story 5.18: A Profile hold always ends'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: '8a0519c43431afcb395ae27a09925782b6ab2ec6'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/implementation/epic-5-context.md'
  - '{project-root}/docs/implementation/spec-5-4-retry-a-profile-lookup-that-failed-transiently.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Story 5.4 holds a reported Agent's new rows while its Profile is resolved again, and
assumed a hold ends within `profileTimeout`. It does not: every `agent_detected` or program change
calls `resolve`, which supersedes the lookup in flight, so while triggers keep arriving no lookup
settles, `holdRows` stays set and `p.held` grows (the follow-up review's reproduction: 200 ms
timeout, a blocked environment read, a redetection every 50 ms, still held after 3 s and 61
lookups). Separately, `readProfile`'s inner reading goroutine is neither tracked nor guarded, so a
permanently hung environment read leaves one goroutine per lookup (1 + `profileRetries` per trigger).

**Approach:** A hold gets a hard deadline, set when it starts and never moved: a timer goroutine
sends a message after `profileHoldLimit()` (`profileHoldFactor` × `profileTimeout`, factor 2), and
the engine then releases the held rows with the best Profile it knows. A pane whose hold ran out
holds nothing more until one of its lookups settles. A per-pane one-slot semaphore lets at most one
lookup's reads run per pane; a lookup that cannot get the slot within `profileTimeout` fails as a
timeout (transient).

## Boundaries & Constraints

**Always:** AD-2: the timer only sends a message; all state changes on the engine goroutine. The
new tunable lives in `internal/engine/tunables.go`. No row pairs a program with a Profile resolved
for another program: a row released or recorded for a program whose lookup has not settled carries
`core.ProfileUnresolved`; a row of the program the held Profile was resolved for carries it. Rows
keep their Status, previous Status, provenance and `observed_at`. Story 5.4's behaviour (retries,
keep on transient, release at settle/removal/run end) is unchanged when lookups settle in time.

**Never:** Move a hold's deadline on a later trigger. Track the inner reading goroutine in `e.wg`
(a hung read would then hang `Run`'s shutdown). Touch restart seeding, first-sight or
`server_state` code (Story 5.17 owns it). Change the stored schema, the Sink interface or
`arch_test.go`'s permitted set.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Repeated redetection, hung read (reviewer) | known `claude`/cldw, env blocked, `agent_detected` every 50 ms, a Status row | row released within ~`profileHoldLimit()` of the hold's start, `profile=cldw`; one environment read in flight | Warn "profile hold ran out" once |
| Further rows after the deadline, lookup still unsettled | same pane, new Status event, more triggers | row recorded at once, no new hold | — |
| Program change, lookup hangs | `codex` → `claude` (rule), env blocked, two rows | at the deadline both rows released `profile=unresolved`; view shows `claude`, latest Status | — |
| Lookup settles after the deadline | env unblocked later | Profile change reported as usual; later rows carry it; holding works again | — |
| Single trigger, lookup settles in time | as Story 5.4 tests | unchanged | — |
| Hung read, more lookups for the pane | env blocked, triggers and retries | no second environment read starts; waiting lookups fail at `profileTimeout` as transient | Info/Warn retry logs as today |

</intent-contract>

## Code Map

- `internal/engine/tunables.go` -- add `defaultProfileHoldFactor = 2`, var `profileHoldFactor`, func `profileHoldLimit()` = factor × `profileTimeout`.
- `internal/engine/engine.go` -- `pane` (~l.391-410): add `holdGen uint64` (deadline timer tag, 0 when none), `holdExpired bool`, `profileSlot chan struct{}` (cap 1, created lazily in `startResolve`). Message types (~l.545-565): add `profileHoldMsg{paneID, gen}`; `handle` (~l.975) dispatches it to `onProfileHold`. `resolve` (~l.1765): replace `p.holdRows = true` with `e.hold(p)`. `startResolve` (~l.1781): pass the slot to `readProfile`. `readProfile` (~l.1798): acquire the slot under the timeout ctx before starting the inner goroutine, which frees it on return. `onProfile` (~l.1834): clear `holdExpired` after the stale check. `settleProfile` (~l.1905): clear `holdExpired`. `release` (~l.1950): clear `holdGen`. `transitionAt` (~l.2496): on a program mismatch, `e.hold(p)`; when the hold ran out, the row carries `unresolved`. Profile doc comment (~l.143-156): state the deadline and the slot.
- `internal/engine/fake_test.go` -- `fakeEnviron` gains a read counter.
- `internal/engine/profile_test.go` -- the reviewer's reproduction as `TestProfileHoldDeadline` plus the matrix rows. The reproduction is at `/private/tmp/claude-501/-Users-jasonviloria-GitHub-herddash/fff4672d-5559-480a-a694-9955071e114b/scratchpad/rv54f/internal/engine/zz_review_test.go` (`TestReviewHoldUnbounded`); copy its scenario, do not add that file.
- Read-only: reconnect guard (~l.1396, `p.resolving == 0`), `setProgram` (~l.1754), `readBackground`/`readSession` busy pattern (~l.2048, 2281), `view.go` `noteView` (holds freeze the view).

## Tasks & Acceptance

**Execution:**
- `internal/engine/tunables.go` -- add the hold factor and `profileHoldLimit` -- bounds in one place.
- `internal/engine/engine.go` -- `hold` helper arming the deadline timer once per hold; `onProfileHold` releasing with `release(p, "", "")`, setting `holdExpired`, logging Warn, noting the view; slot in `readProfile`; `transitionAt` change; doc comments -- the story.
- `internal/engine/fake_test.go`, `internal/engine/profile_test.go` -- tests for every matrix row; the reviewer's test made real.

**Acceptance Criteria:**
- Given any matrix row, when its test runs (also `-race -count=20`), then it passes.
- Given the full suite, when `go build`, `go vet`, `go test ./...` and `gofmt -l` run, then all pass and gofmt prints nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 26 findings — high 0, medium 1, low 15, false 10, maybe-false 0
- findings:
  - `[medium]` `[patch]` (blind) Superseded lookups queued on the pane's slot still take it and read, delaying the current lookup — a per-pane `profileLatest` generation; a lookup that takes the slot after a later one started frees it at once and returns without reading.
  - `[low]` `[reject]` (blind) Waiting for the slot uses the lookup's own timeout budget — this is the spec's Approach (a lookup that cannot get the slot within `profileTimeout` fails as a timeout); with the stale-waiter patch only a genuinely slow earlier current read can delay it.
  - `[low]` `[reject]` (blind) The slot is per pane, so a permanently hung read blocks later programs' lookups on that pane — the AC mandates at most one environment read per pane; the slot-wait error ("an earlier lookup's read still running") appears in the retry logs and the give-up Warn, and the rows are never held past the deadline.
  - `[low]` `[patch]` (blind) `e.agent(p)` pairs the new program with the old program's Profile while the view shows `unresolved` — one helper `shownProfile` used by `viewOf` and `agent`; `TestProfileProgramChangeHeldRows/pane removed` now expects `removed ... profile=unresolved`.
  - `[low]` `[reject]` (blind) The spec's Code Map lists `view.go` read-only and the Matrix does not state that a transient failure clears `holdExpired` — fix is a spec edit; noted under Auto Run Result.
  - `[low]` `[reject]` (blind) The spec cites a scratchpad path and a machine-local Go path — fix is a spec edit; the scenario lives on in `TestProfileHoldDeadline`.
  - `[low]` `[patch]` (blind) `TestProfileFailures` timeout case no longer proves retries happen — transient cases assert exactly `profileRetries` retry INFO entries.
  - `[low]` `[patch]` (blind) "only the first settle ends the hold" synchronises by sleep and its bound no longer catches a holding retry — bound tightened to `profileTimeout/2` (grouped with the verification gap below).
  - `[low]` `[reject]` (blind) Untested paths: stale hold message, removal or run end with a deadline pending, `transitionAt`-started hold, slow-not-hung environment — each is guarded by the `holdGen`/pane-gone check or by the existing release paths that 5.4's tests cover; adding tests for each is more than a direct correction.
  - `[low]` `[reject]` (blind) The deadline goroutine lingers for the full limit after an early release — bounded to one per hold, each needing a trigger, ends on `runCtx`; stopping it would add state for no observed harm.
  - `[false]` (blind) `profileHoldFactor` is an unchecked integer — it is a constant default of 2 changed only by tests, like every other tunable in `tunables.go`.
  - `[medium]` `[patch]` (edge) Superseded lookups queued on the slot read after a hung read returns — same root cause as the blind medium; patched there.
  - `[low]` `[patch]` (edge) Channel sends are not FIFO, so the newest lookup can starve — same root cause; stale waiters now drop the slot at once.
  - `[low]` `[reject]` (edge) A permanently hung read stops the pane's Profile ever being re-resolved — same as the blind per-pane item; rejected there.
  - `[low]` `[patch]` (edge) `agent()` uses the old program's Profile after a hold ran out — same as the blind `agent` item; patched there.
  - `[low]` `[reject]` (edge) A pane removed and re-tracked under the same id while the old read hangs gets a new slot, so a second read can start — bounded to one per pane instance; removal and re-tracking during a hung read is rare and the leak does not grow per trigger.
  - `[low]` `[reject]` (edge) claim: `holdExpired` is cleared by a transient failure too, so a later trigger can hold again — intended: a lookup that ran to its end has settled; the new hold then ends within `profileTimeout` (the slot-wait timeout) and never moves its deadline; now pinned by a test (verification gap below).
  - `[false]` (edge) claim: "No row pairs a program with a Profile resolved for another program" — rows are guarded (the claim concedes it); the Agent passed alongside is the separate `agent()` item, patched.
  - `[low]` `[patch]` (verification) No test pins that a transient failure after a deadline lets the pane hold again — added `TestProfileHoldDeadline/transient failure after the deadline`.
  - `[low]` `[patch]` (verification) "only the first settle ends the hold" cannot see a retry that holds rows — bound tightened to `profileTimeout/2`.
  - `[false]` (intent) The Board surface is checked only through `engine.View()` — the engine's published view is what `/api/state` and `/events` serve unchanged; 5.4's triage settled the engine level as the AC surface.
  - `[false]` (intent) Repeated bounded freezes after a transient reset are untested — now tested; each freeze is bounded and its deadline fixed.
  - `[false]` (intent) Goroutines are not counted — the AC's mechanism is one environment read per pane, asserted by `fakeEnviron.reads()`; the remaining lookup goroutines are tracked and end within `profileTimeout`.
  - `[false]` (intent) The tunable is not an operator setting — the epic context puts bounds in `tunables.go`.
  - `[false]` (intent) Two existing Profile tests were edited — the slot changes how many herdr calls retries behind a hung read make; their behavioural assertions stayed and were strengthened.
  - `[false]` (intent) The test accepts release between 1× and 2× the limit — the upper bound is test slack for a 50 ms polling loop under `-race`; the deadline itself is armed once at the hold's start.

## Design Notes

Deadline 2 × `profileTimeout` (4 s by default): one lookup always ends within `profileTimeout`, and
one trigger superseding it just before it ends still settles within 2 × from the hold's start, so
only an unending stream of triggers or a lookup that cannot run reaches the deadline. After the
deadline the old Profile is still the best known for rows of its program (a lookup that failed is no
evidence it changed, as in 5.4's keep rule); rows of a new program get `unresolved`.

`holdExpired` stops the freeze recurring: without it, each further trigger would start a new hold and
the Board would freeze for 4 s at a time. It clears when any of the pane's lookups settles.

## Verification

Go is `$HOME/.local/share/mise/installs/go/1.26.5/bin` (prepend to PATH).

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- expected: pass
- `gofmt -l $(git ls-files '*.go')` -- expected: no output
- `go test -race -count=20 -run 'Profile|Detected' ./internal/engine` -- expected: pass

## Auto Run Result

Status: done

**Summary.** A Profile hold now always ends. A hold's deadline, `profileHoldLimit()` = 2 ×
`profileTimeout` (4 s by default), is armed by a timer goroutine when the hold starts and is never
moved by a later trigger. At the deadline the held rows are released in order with the best Profile
known (the held one for rows of the program it was resolved for, `unresolved` otherwise), the view is
noted, and a Warn "profile hold ran out" is logged. The pane then holds nothing more until one of its
lookups ends; rows of a program whose lookup has not ended are recorded at once as `unresolved`. A
per-pane one-slot semaphore lets at most one lookup's reads run per pane, so a hung environment read
gains no company; a lookup that cannot take the slot within `profileTimeout` fails as a transient
timeout, and one superseded while waiting drops the slot without reading. The view and `Agents()`
show `unresolved` while the held Profile belongs to another program.

Deadline choice: factor 2, because a lookup ends within `profileTimeout` and one trigger superseding
it just before it ends still settles within 2× from the hold's start; only an unending stream of
triggers or a lookup that cannot run reaches it.

**Files**
- `internal/engine/tunables.go`: `defaultProfileHoldFactor`, `profileHoldFactor`, `profileHoldLimit()`.
- `internal/engine/engine.go`: `hold`, `profileHoldMsg`/`onProfileHold`, `holdGen`/`holdExpired`, the slot and `profileLatest` in `startResolve`/`readProfile`, `transitionAt`'s hold and `unresolved` row, `agent()` via `shownProfile`, doc comments.
- `internal/engine/view.go`: `shownProfile`, used by `viewOf` (this file was listed read-only in the Code Map; the change came from the review).
- `internal/engine/fake_test.go`: `fakeEnviron.reads()` counts reads started and running.
- `internal/engine/profile_test.go`: `TestProfileHoldDeadline` (four subtests, the first the reviewer's reproduction); "only the first settle ends the hold" bound tightened.
- `internal/engine/engine_test.go`: `TestProfileFailures` counts environment reads and retry entries.

**Review.** 26 findings: 1 medium and 9 low patched (grouped into 6 fixes), 7 low rejected with
reasons above, 10 false, none deferred. Follow-up review recommended: false (patched entries by
verdict: high 0, medium 1, low 9).

**Verification** (after the patches)
- `go build ./...`, `go vet ./...`, `go test ./...`: pass.
- `gofmt -l .`: prints nothing.
- `go test -race -count=20 -run 'Profile|Detected' ./internal/engine`: ok (361.9 s).
- `go test -race ./internal/engine ./internal/httpapi`: ok.

**Residual risks**
- A permanently hung environment read leaves that pane's Profile lookups failing at `profileTimeout` for the pane's lifetime (logged as retries and one Warn per trigger); its rows are never held past the deadline.
- After a hold ran out with a hung read, each later trigger burst can hold rows again for up to `profileTimeout`.
- A removal now reports `profile=unresolved` when the Agent's program changed and its lookup had not settled.

