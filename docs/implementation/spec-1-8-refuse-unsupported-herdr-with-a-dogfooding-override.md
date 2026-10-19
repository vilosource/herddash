---
title: 'Story 1.8: Refuse unsupported herdr, with a dogfooding override'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: '88fb175434dbce0e185c915dc9c0874c7258c628'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/implementation/spec-1-7-resync-when-the-stream-cannot-be-trusted.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The engine records from any herdr that answers `ping`. Below 0.9.5 herdr
under-reports `blocked`, and below 0.9.0 it replays stale events and has no lost-events signal.
A Board built on such a server is silently wrong (FR-16, ADR-0006, AD-11).

**Approach:** At every connect, the engine gates on the version string `ping` returns: `ok` for
0.9.5 or newer, `refused` below it, and `advisory` below it when `allow_unsupported_herdr` is set.
A refused server is observed by nothing. An advisory one is observed normally, with a warning.

## Boundaries & Constraints

**Always:**
- **Core rule:** `internal/core` gets `Gate` (`ok`, `advisory`, `refused`), `HerdrMinimum`
  ("0.9.5"), and a pure `DecideGate(version string, allowUnsupported bool) (Gate, resyncGuaranteed bool)`.
  - It parses `MAJOR.MINOR.PATCH`, tolerating a leading `v` and ignoring any `-` or `+` suffix.
  - An empty or unparsable version cannot be shown to be supported, so it is treated as below the
    minimum.
  - `resyncGuaranteed` is false below 0.9.0 or when the version is unparsable.
  - The version comes only from `ping`'s `version`, never from a `protocol` number.
- **Where:** the gate is decided in the connect attempt, after `ping` and **before** the lifecycle
  subscribe.
  - **Refused:** no lifecycle subscription, no snapshot, no status subscriptions. Every tracked
    Agent is removed with reason `herdr refused`. The engine checks again every
    `refusedRecheckInterval`, a named tunable defaulting to 30 s that tests lower. It retries
    forever, like a failed start, but does not count into the reconnect backoff. Only the run
    context ends it.
  - **Advisory or ok:** the connect continues exactly as today.
- **Reporting:** `Sink.Gate(gate core.Gate, herdrVersion string, observedAt int64)` is called on a
  connect whose outcome or version differs from the last one reported, including the first.
  Nothing is reported for an unchanged repeat. serve prints
  `gate       herdr=<v> minimum=0.9.5 gate=<gate>`.
- **Logging:**
  - Refused: a warn naming `herdr_version` and `herdr_minimum` when the outcome first becomes
    refused, and debug on rechecks.
  - Advisory: one warn per connect. It names the version and the minimum, and when
    `resyncGuaranteed` is false it also says the resync guarantee does not hold below 0.9.0.
- **Transitions:** every Transition keeps the herdr version, as now. serve's transition line gains
  `herdr=<v>`.
- `engine.Options` gains `AllowUnsupportedHerdr bool`, which serve fills from
  `cfg.AllowUnsupportedHerdr()`.

**Never:**
- Read the version from anything but `ping`, or call any new herdr method.
- Store anything, or build ConnectionTruth or a Board strip. Those belong to Epics 2 and 3.
- Touch a live herdr in tests.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Supported | ping `0.9.5` (also `0.10.0`, `1.0.0`, `v0.9.6`) | `gate … gate=ok`; Agents and Transitions as before | none |
| Refused | ping `0.9.4`, flag off | `gate … gate=refused`; no lifecycle subscribe, no snapshot, no Agent or Transition line; warn log names 0.9.4 and 0.9.5 | none |
| Refused, then upgraded | ping `0.8.2`, later `0.9.5` | rechecks at the interval; then `gate … gate=ok` and a normal start | none |
| Refused rechecks | ping `0.8.2` repeatedly | one gate line; ping again at each interval; no backoff growth | none |
| Advisory | ping `0.8.2`, flag on | `gate … gate=advisory`; Transitions print `herdr=0.8.2`; warn says resync guarantee does not hold below 0.9.0 | none |
| Advisory 0.9.x | ping `0.9.2`, flag on | advisory; warn without the resync clause | none |
| Unparsable | ping `""` or `dev` | treated as below the minimum: refused, or advisory with resync not guaranteed | none |
| Prerelease | ping `0.9.5-rc1` | parsed as 0.9.5: ok | none |
| Downgraded across reconnect | was ok with Agents; reconnect pings `0.8.2`, flag off | gate line; every Agent removed `herdr refused` | none |

</frozen-after-approval>

## Code Map

- `internal/core/` -- add `gate.go` and `gate_test.go` (table tests, no I/O). Core stays standard-library-only and never reads the clock (arch_test).
- `internal/engine/engine.go`:
  - `connect`: decide the gate between `Ping` and `SubscribeLifecycle`, put it on `connectMsg`, and skip the subscribe when refused.
  - `onConnect`: report and log the gate. Refused means remove every tracked Agent (`remove`), then `awaitConnect(refusedRecheckInterval)` without touching `failures`.
  - Add the last-reported gate and version beside `herdrVersion`.
- `internal/engine/tunables.go` -- `defaultRefusedRecheckInterval` and its package variable.
- `internal/engine/sink.go` -- the `Gate` method. `engine.Options` gets `AllowUnsupportedHerdr`.
- `internal/engine/fake_test.go` -- the fake `Ping` must be scriptable per call (a version sequence). The `recorder` gains `Gate`.
- `cmd/herddash/serve.go`, `serve_test.go` -- the gate line, `herdr=` on the transition line, and the flag passed through.
- `internal/config` -- `AllowUnsupportedHerdr()` already exists. Do not change it.

## Tasks & Acceptance

**Execution:**
- [x] `internal/core/gate.go`, `gate_test.go` -- Gate, HerdrMinimum, DecideGate, with the parse cases from the matrix
- [x] `internal/engine/engine.go`, `tunables.go`, `sink.go`, `fake_test.go`, `engine_test.go` -- gate in connect, refused recheck loop, reporting, logging, every matrix row
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- printer lines and flag plumbing

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes, arch_test passes, and Story 1.5 to 1.7 tests still pass.

## Implementation Notes

- **cmd may not import core**, so `engine` re-exports `Gate` as an alias (as it does `Transition`)
  with the three outcomes and `HerdrMinimum`. `Sink.Gate` takes `core.Gate`, which the alias satisfies.
- `core` also exports `HerdrResyncMinimum` ("0.9.0"), so the advisory warning's clause and
  `DecideGate` share one constant.
- **Refusal warn vs debug.** The warn is logged whenever the refused outcome is reported, so a
  refused herdr whose version changes (0.8.2, then 0.9.4) warns again; an unchanged recheck logs at
  debug.
- A refused connect clears `failures` (the refused herdr was reachable, so a later failed start
  backs off from the start), calls `disconnect`, removes every tracked Agent, and waits
  `refusedRecheckInterval`. The warnings say the version is "below the supported minimum or could
  not be read", which is also true of an empty or unparsable version. The epoch's `terminal_id` map
  is kept, so an upgrade after a refusal goes through Story 1.6's epoch rule like any reconnect.
- The serve transition line prints `herdr=-` when a Transition carries no version.
- The test recorder keeps gate reports out of its lines unless `gateLines` is set, so the Story
  1.5 to 1.7 tests are unchanged except `TestLiveChange`, whose clock readings now start after the
  gate report's.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH | ok → refused → ok with the same terminal ids keeps the epoch, so removed Agents return under the same key | false | AD-3 keeps the epoch on terminal-id evidence and forbids a new epoch without evidence of reassignment; a real version change needs a restart, which changes every terminal_id and increments the epoch. Forcing a new epoch would split one Agent across two keys |
| 2 | VG | ok → refused → ok recovery is untested | medium | VG pre-verified: clearing terminals or bumping the epoch in `refused` fails no test; patch: tests for kept and incremented recovery |
| 3 | BH, ECH | A refusal leaves `failures`, so an upgrade backs off from a stale count | low | The refused herdr was reachable; direct fix; patch |
| 4 | VG, BH | A version change under the same outcome is never exercised | medium | VG pre-verified: dropping the version comparison in `reportGate` fails no test; patch |
| 5 | BH | The warnings say "older than the minimum" for an unreadable version | low | False statement in the log; direct rewording; patch |
| 6 | ECH | `0.9.5-rc1` passes as ok, though a prerelease may predate 0.9.5's fixes | false | The frozen matrix says a prerelease of 0.9.5 parses as 0.9.5 and is ok |
| 7 | ECH, BH | `V0.9.5` or a version with whitespace is refused | low | herdr returns the version from a JSON field; no evidence of either form; adds branches; rejected |
| 8 | BH | Spec and tracker statuses disagree; the triage log is empty | false | Workflow ordering: in-review precedes triage and the tracker sync at finalize |
| 9 | BH | deferred-work lacks the ConnectionTruth gate and Board strip, and the new tunable | false | The gate's ConnectionTruth and the Board strip are Epic 3 stories; the Story 1.7 deferred entry already covers documenting every engine tunable |
| 10 | BH | `TestGateRefusedRechecks` upper bound of 500 ms can flake | low | Direct test edit; patch |
| 11 | BH | `TestServeRefusesUnsupportedHerdr` does not show nothing was subscribed or read, nor the log content | medium | The epic's AC names the log; direct test edit; patch |
| 12 | BH | Session notes in `.vfkb/entries.jsonl` ride in the feature commit | low | No effect on the release changelog, which reads the commit message; rejected |
| 13 | BH | `parseVersion`'s digit check is redundant with `ParseUint` | low | Direct deletion; patch |
| 14 | BH | `DecideGate` reparses its constants on each call | low | Negligible cost, guarded by a test; rejected |
| 15 | BH | The `runServe` doc comment overruns the width | low | Direct reflow; patch |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
