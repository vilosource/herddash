---
title: 'Story 1.9: Know which profile each agent runs under'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: '9ab1b5b8b31c8d27f4278d94fb700defaab9004b'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/implementation/spec-1-8-refuse-unsupported-herdr-with-a-dogfooding-override.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Two `claude` panes on different subscriptions look identical. The Profile Map is
loaded but never used, and no Agent or Transition carries a Profile (FR-10, FR-11, AD-14).

**Approach:**
1. Resolve each Agent's Profile before it is first reported. For an Agent Program with a rule,
   read its foreground process from herdr's `pane.process_info`, then that process's environment
   through `internal/procenv`.
2. Apply the rule in `internal/core`.
3. Resolve again when herdr detects an agent in the pane or the Agent Program changes.

## Boundaries & Constraints

**Always:**
- **Rule (core, pure):** `core.ProfileRule{Variable, Absent string; Values map[string]string}`,
  plus `core.ResolveProfile(program, rule *ProfileRule, value string, set bool) string`.
  - No rule gives the program name.
  - Unset gives `Absent`.
  - Set gives the `Values` entry whose key equals the value after `filepath.Clean` on both sides.
    The keys arrive with `~/` already expanded by config.
  - Set but unmatched gives `core.ProfileUnresolved` ("unresolved"). config's constant becomes an
    alias of it.
  - Also add `core.Heading(profile, program)`. It gives the name once when they are equal, and
    `profile · program` otherwise.
- **procenv:** `Lookup(pid int, name string) (value string, set bool, err error)`.
  - linux reads `/proc/<pid>/environ`.
  - darwin reads `kern.procargs2` through `golang.org/x/sys/unix` v0.49.0 (ADR-0004), in a
    `_darwin.go` file. The format is argc, exec path, NUL padding, argc argv strings, then
    `NAME=value` strings.
  - Other OSes return an error. The parsing is a pure function, tested on byte fixtures.
  - procenv imports nothing internal (arch_test).
- **Engine:**
  - `Client` gains `ForegroundProcess(ctx, paneID) (core.ForegroundProcess, error)`.
  - `Options` gains `Profiles map[core.AgentProgram]core.ProfileRule` and `Environ` (a func
    defaulting to `procenv.Lookup`; tests inject a fake).
  - A pane about to be reported first is resolved in a goroutine bounded by
    `profileTimeout` (tunable, default 2 s). Its report waits for the result. Status changes
    meanwhile update the Status silently, so the first report carries the latest Status.
  - A failure gives `unresolved`. Failures include a timeout, no foreground process, an
    environment read error, and a rule-less read failure.
  - A program with no rule skips both reads.
  - Results carry the pane's generation tag, so a stale result is ignored.
- **Re-resolve:** on `pane_agent_detected` for a tracked pane, and when a snapshot or status event
  changes a known Agent's Agent Program. If the Profile differs, report
  `Sink.Profile(a Agent, previous string, observedAt)`.
- `Agent` and every Transition carry the Profile. serve's agent, transition, removed and profile
  lines print `profile=<p>`. cmd converts config's rules to core's.
- **Logging:** never log an environment value, except a value of the rule's variable that matched a
  Profile Map entry. Resolution outcomes log at debug with the Profile.

**Never:**
- Read pane output, or call focus.
- Store anything (Epic 2).
- Install anything.
- Touch a live herdr or a real process's environment in engine tests. procenv may read the test
  process's own environment.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Mapped | claude, env `CLAUDE_CONFIG_DIR=/h/.claude-cldw`, map `/h/.claude-cldw`→`cldw` | first line for the Agent says `profile=cldw` | none |
| Trailing slash | value `/h/.claude-cldw/` | `cldw` | none |
| Absent | claude, variable unset, absent `claude` | `profile=claude` | none |
| Unmatched | value `/h/.other` | `unresolved`; value not logged | none |
| Timeout | env read blocks past `profileTimeout` | Agent reported `unresolved` after the timeout | none |
| No foreground | process_info → no foreground process | `unresolved` | none |
| No rule | codex Agent | `profile=codex`, no process_info call; `Heading` gives `codex` | none |
| Status during resolve | live event `blocked` while resolving | one Agent line with `blocked`, no Transition | none |
| Re-detect | known claude pane; `pane_agent_detected`; env now maps to `cldp` | `profile` line `cldp` previous `cldw`; later Transitions carry `cldp` | none |
| Same profile | re-resolve gives the same Profile | nothing printed | none |
| darwin parse | procargs2 fixture with argc 2 and 3 env entries | Lookup finds, misses, handles `=` in value | malformed → error |
| linux parse | environ fixture | same | none |

</frozen-after-approval>

## Code Map

- `internal/core/` -- add `profile.go` and `profile_test.go`. Transition already has `Profile`.
- `internal/config/config.go` -- `ProfileUnresolved` aliases core's. Add a `ProfileRules() map[core.AgentProgram]core.ProfileRule` accessor for cmd. The existing `Profile`/`AgentPrograms` stay.
- `internal/procenv/` -- `procenv.go` (Lookup dispatch and pure parsers), `procenv_linux.go`, `procenv_darwin.go`, `procenv_other.go` and tests. `go.mod` gets `golang.org/x/sys v0.49.0`.
- `internal/engine/client.go` -- the `ForegroundProcess` method on the interface and on `herdrClient`; `herdr.Client.ForegroundProcess` exists.
- `internal/engine/engine.go`:
  - `report` is called from `apply` and `onStatus`. Gate it on resolution.
  - `onLifecycle`'s `LifecycleAgentDetected` tracked branch is the re-resolve trigger.
  - `transition` fills `Profile`.
  - Add a `profileMsg{paneID, gen, profile}` inbox message.
- `internal/engine/tunables.go`, `sink.go`, `fake_test.go`, `engine_test.go`.
- `cmd/herddash/serve.go`, `serve_test.go` -- the printer and the wiring.
- `arch_test.go` -- procenv already permits nothing internal. Do not change it.

## Tasks & Acceptance

**Execution:**
- [x] `internal/core/profile.go`, `profile_test.go` -- rule and Heading
- [x] `internal/procenv/*`, `go.mod`, `go.sum` -- Lookup with linux/darwin readers and fixture tests
- [x] `internal/config/config.go` and its test -- alias and accessor
- [x] `internal/engine/*` -- resolution before report, re-resolve, Profile on Agent and Transition, every matrix row
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- profile on lines, the profile line, wiring

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs on darwin, then every matrix row passes, arch_test passes, and Story 1.5 to 1.8 tests still pass.
- Given `GOOS=linux` and `GOOS=darwin` with `CGO_ENABLED=0`, when `go vet ./...` runs, then it passes and the darwin reader is excluded from linux builds.

## Implementation Notes

- **Generation tag.** A Profile result is tagged with a resolution tag drawn from the engine's
  `nextGen` counter and kept on the pane as `resolving`, not with the status subscription's
  `gen`: a resync resubscribing a pane mid-resolution would otherwise discard a valid result and
  leave the Agent unreported. A removed pane, a re-tracked pane id, or a newer resolution all make
  the old tag stale.
- **Reads run apart from the timeout.** The environment read cannot be cancelled, so
  `readProfile` runs both reads in an untracked goroutine with a buffered result channel and
  abandons it at `profileTimeout`. The bound uses the run context, not the connection's, so a
  reconnect does not turn a known Agent's re-resolve into `unresolved`.
- **Unreported panes respect `liveDuring`.** Because the first report now waits, a snapshot that
  was in flight when a live event arrived no longer overwrites that event's Status on a pane not
  yet reported.
- **Re-resolve triggers.** `pane_agent_detected` on a tracked, reported pane resolves again (the
  snapshot read it already caused stays). An Agent Program change from a snapshot or status event
  resolves again for a reported pane, and restarts a resolution in flight for an unreported one.
- **cmd may not import core**, so `config.ProfileRules()` returns core's map and cmd passes it
  straight to `engine.Options.Profiles`. `engine` re-exports `ProfileUnresolved`.
- `go.mod`'s directive became `go 1.26.0`, which `golang.org/x/sys` v0.49.0 itself declares. The
  module is imported only from `procenv_darwin.go`.
- serve's lines print `profile=-` for an empty Profile, as for the other fields.
- **An empty environment read is an error, not "unset".** On linux an empty
  `/proc/<pid>/environ` (zombie or exiting process) and on darwin a `kern.procargs2` buffer that
  is empty or has no environment entries return `procenv.ErrNoEnvironment`, so the Agent is
  `unresolved` rather than the rule's `absent`. Found while testing: macOS withholds an Apple
  platform binary's environment (for example `/bin/sleep`) from a non-root caller, so its
  `procargs2` holds the arguments only. Claude Code is not a platform binary, so its
  environment is readable. serve's end-to-end test uses its own test binary as the child process
  for the same reason.
- **Status before program.** For a pane not yet reported, `onStatus` and `apply` take the new
  Status before `setProgram`, because a change to a program with no rule settles the Profile and
  reports the Agent at once. A pane that `setProgram` just reported is not reported again, and
  gets no Transition.
- **Terminal replacement applies to unreported panes too**, so a resolution read from the old
  terminal's process is dropped with the pane.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH | `setProgram` runs before the Status update, so a program change to a no-rule program mid-resolution reports a stale Status and then a Transition | medium | `onStatus`/`apply` call `setProgram` first; `resolve`'s no-rule branch settles and reports synchronously; patch |
| 2 | BH | Transitions keep the old Profile while a known pane's re-resolve is in flight | low | A new agent in a pane normally follows `pane_agent_released`, which removes the pane, so it is resolved before its first report; a re-detect without release is rare and the window is at most `profileTimeout`; holding Transitions adds state; rejected |
| 3 | BH, ECH | `readProfile`'s inner goroutine is untracked and can outlive `Run` | low | Documented abandonment of an uncancellable read; `ForegroundProcess` is context-bound; rejected |
| 4 | BH | Every reconnect re-resolves every pane at once | false | `disconnect` keeps known panes and their Profile; only panes new to the engine are resolved |
| 5 | BH, ECH | `parseProcargs2` skips every NUL after the exec path, eating empty leading arguments | low | Same skip as Apple's own `ps`; an empty argv[0] under herdr is unobserved; a bounded skip needs the kernel's alignment rule; rejected |
| 6 | VG, BH | The darwin reader never runs in CI | medium | VG pre-verified: CI tests on ubuntu only; adding a macOS job is a CI change; defer |
| 7 | BH | `TestServeResolvesProfile` depends on the real environment and may log a token | medium | Picks the first usable variable; patch: a child process with a controlled environment |
| 8 | BH | `TestProfileRedetectedSame` can assert before the second result arrives | low | Direct test edit; patch |
| 9 | VG, BH | The `apply` change for unreported panes (`liveDuring`) is untested | medium | VG pre-verified: deleting the guard fails no test; patch |
| 10 | VG, BH | Nothing pins that a reconnect during a re-resolve leaves the Profile intact | medium | VG pre-verified; patch |
| 11 | BH | The frozen text lists a "rule-less read failure", and the tag source and conversion differ from it | false | Fixing it means editing this build's frozen spec; the implementation's choices are recorded in Implementation Notes |
| 12 | BH | Spec and tracker statuses disagree | false | Workflow ordering |
| 13 | BH | `runServe` doc comment badly wrapped | low | Direct reflow; patch |
| 14 | BH | CLAUDE.md "Repository state" is stale (dependencies, procenv) | low | Agent-context file; defer |
| 15 | BH | `Heading("", program)` gives " · program" | low | Direct fix; patch |
| 16 | ECH | No retry after a transient resolution failure, so the Agent stays `unresolved` | medium | maybe-false: whether `process_info` or the env read fails transiently in practice is unobserved; defer, settled by dogfooding logs |
| 17 | ECH | A failed re-resolve overwrites a good Profile with `unresolved` | false | The spec says a failure gives `unresolved`; after a re-detect the old Profile may itself be wrong, so `unresolved` is the honest value |
| 18 | ECH | Terminal replaced during the first resolution keeps the old process's Profile | low | `apply`'s replaced check requires `known`; direct correction; patch |
| 19 | ECH | An empty environment read gives `Absent` instead of `unresolved` | low | Direct guard on a zero-length read; patch |
| 20 | ECH | Non-absolute `Values` keys that collide after `Clean` choose nondeterministically | low | Keys are paths with `~/` expanded; relative keys are not a configured case; rejected |
| 21 | ECH | An Agent with an empty Agent Program gets an empty Profile | low | herdr names the program on every agent record observed; rejected |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
- `GOOS=linux go test -c -o /dev/null ./internal/procenv` -- expected: compiles
