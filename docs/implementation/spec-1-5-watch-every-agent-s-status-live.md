---
title: "Story 1.5: Watch every Agent's Status live"
type: 'feature'
created: '2026-10-16'
status: 'done'
baseline_commit: 'f1bfaf58587a956d3f461869d427f2dc0db2af1d'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/research/herdr-socket-api.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** herddash can talk to herdr but does nothing with it. The operator cannot yet see
agents finish or block without a terminal client attached.

**Approach:** Add the engine actor and a `herddash serve` command. The engine subscribes to
lifecycle events, snapshots, keeps one status subscription per Agent pane, and prints every Agent
and every Status change to stdout as it happens.

## Boundaries & Constraints

**Always:**
- **One writer (AD-2):** only the engine goroutine holds the pane set, each Agent's current Status
  and its tracked context. Subscription readers and herdr calls run in other goroutines and reach
  the engine only by message, each tagged so a stale message from a closed subscription is ignored.
- **Order (AD-11):** at start, lifecycle subscription first, then snapshot, then one status
  subscription per Agent pane. On `pane.agent_detected` for an untracked pane: subscribe its status,
  then read its state with a snapshot. At most one snapshot in flight; an event arriving while one
  is in flight causes one more read after it. Buffered events are never replayed onto a snapshot.
- **Printing:** one line per Agent when first seen, one per Transition, one per removal. A line
  carries Host, pane id, Agent Program and Status, and a Transition line its previous Status and
  provenance (`live` for status events, `snapshot` when a snapshot disagrees with current state).
  Transitions are built with `core.NewTransition` (Server from config, Epoch fixed at 1 until Story
  1.6, Profile empty until Story 1.9, herdr version from `ping`, `observed_at` from an injected
  clock). A live status event is printed within one second.
- **Tracked context**, from the snapshot and refreshed by later snapshots: project directory (the
  pane's `foreground_cwd`, else `cwd`), workspace label, tab label, the workspace's position in
  herdr's order, and the Pane Title. The lifecycle subscription also takes `workspace.renamed`,
  `workspace.moved`, `workspace.reordered`, `tab.renamed`, `tab.moved` and `pane.updated`, each only
  as a signal to snapshot again. Context is not printed (Pane Titles only at debug, per the logging
  convention).
- **A rejected status subscribe** fails only that pane: the engine snapshots again and leaves other
  subscriptions alone.
- **Removal:** `pane.closed`, `pane.exited`, `agent_released`, or a snapshot without the pane or its
  agent closes that pane's subscription, drops it and prints its removal.
- **Serve:** `herddash serve` (and `herddash` with no arguments) loads config like `herddash
  config`, ensures the data directory, logs JSON with `log/slog` to `herddash.log` there, and stops
  cleanly on SIGINT or SIGTERM. A lifecycle stream health signal or a failed start ends serve with
  exit 1 naming the cause; reconnecting is Story 1.6.
- Tunables (snapshot-again debounce if any, call timeouts) are named constants in one place.

**Never:**
- Reconnect, gate on version, detect epochs, resolve Profiles, persist anything, or serve HTTP.
- Touch a live herdr in tests: the engine is tested against a fake client behind an interface.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Start | snapshot: 2 agent panes, 1 shell | lifecycle subscribed before snapshot; 2 status subs; 2 Agent lines | none |
| Live change | status event working→blocked | one Transition line, `live`, previous `working` | none |
| Repeat | status event with unchanged Status | no line | none |
| New agent | `agent_detected` w1:p4 | status subscribe before the snapshot that reads it; Agent line | none |
| Close race | status subscribe for w1:p4 rejected | snapshot again; other subscriptions untouched | no exit |
| Removal | `pane_closed`, `agent_released`, or pane gone from snapshot | subscription closed, removal line | none |
| Disagreement | snapshot shows w1:p1 `idle`, engine has `working` | Transition line, `snapshot` | none |
| During read | event arrives while a snapshot is in flight | exactly one more snapshot after it | none |
| Context | `tab.renamed` event | snapshot again; tracked tab label updated | none |
| Lifecycle lost | lifecycle health `closed` | serve exits 1 naming it | exit 1 |
| Signal | SIGINT | subscriptions closed, exit 0 | none |

</frozen-after-approval>

## Code Map

- `internal/herdr/client.go`, `subscribe.go`, `map.go` -- Story 1.4 client: `Ping`, `Snapshot`, `SubscribeLifecycle`, `SubscribeStatus`, `Subscription[T].C`/`Close`, `*Health`; extend `SubscribeLifecycle` and `core.LifecycleKind` with a context-changed kind for the six context kinds, and `mapSnapshot` with the tracked context fields
- `internal/herdr/gen/genwire/main.go` `roots` -- add the six context subscription variants (and their `EventData` variants only if decoded; a context-changed event needs no data), regenerate with `go generate ./internal/herdr/gen`
- `internal/core/observe.go` -- extend `PaneObservation` with project directory, workspace and tab labels, workspace position, Pane Title; snapshot workspaces/tabs give labels and order
- `internal/core/types.go` -- `NewTransition`, `AgentKey`, `Status`
- `internal/engine/doc.go` -- package doc; engine imports core and herdr only here
- `cmd/herddash/main.go` -- `run(args, stdout, stderr)`, `configFlags`, `runConfig`; add `serve` and no-args → serve; keep `run` testable (signals and umask in `main`)
- `internal/config` -- `Load`, `EnsureDataDir`, `Config.Host/Server/SocketPath/DataDir`
- `arch_test.go` -- cmd → engine, config; engine → core, herdr

## Tasks & Acceptance

**Execution:**
- [x] `internal/core/observe.go`, test -- tracked context fields and the context-changed lifecycle kind
- [x] `internal/herdr/gen/genwire/main.go`, `wire_gen.go`, `internal/herdr/map.go`, `subscribe.go`, tests -- context subscriptions and snapshot context mapping
- [x] `internal/engine/engine.go` -- the actor: inbox, pane set, ordering, snapshot-in-flight guard, Transitions, removal -- AD-2, AD-11
- [x] `internal/engine/sink.go` -- the output interface the engine reports Agent, Transition and removal lines to
- [x] `internal/engine/engine_test.go` -- a fake herdr client injecting every signal; a test clock; every matrix row except Signal; run with `-race`
- [x] `cmd/herddash/serve.go`, `main.go`, test -- `serve`, default command, line printer, slog file, signals -- Signal and Lifecycle-lost rows

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes and arch_test passes.

## Implementation Notes

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, ECH | A snapshot failure after start ends serve | medium | `onSnapshot` returns the error; the spec makes only a lifecycle health signal or a failed start fatal; patch: log and retry after a named delay |
| 2 | BH, ECH | Every `pane.updated` (and status events during a read) triggers back-to-back snapshots with no minimum interval | medium | `requestSnapshot` coalesces only in-flight reads; title updates could keep herdr snapshotting continuously; patch: named minimum interval between reads (status subscriptions stay live, so the one-second promise is unaffected) |
| 3 | BH, ECH, VG | Persistent subscribe rejection loops snapshot → subscribe → reject | low | Needs herdr to list a pane it refuses; bounding it is Story 1.7's minimum resync interval; defer |
| 4 | BH, ECH, VG | `pane.moved` prints a removal plus a new Agent; move untested | low | AD-3 keys identity on pane id, so whether a moved pane keeps its Agent is an unsettled design question; defer the question; patch a test pinning today's behaviour |
| 5 | BH, ECH | `agent_released`'s `FinalStatus` is ignored | medium | `onLifecycle` removes at once; a last change carried only by the release is never printed; patch: live Transition to a valid differing final Status before the removal |
| 6 | BH, ECH | `transition` updates `p.status` before `NewTransition` may reject it | low | A rejected (non-standard) Status becomes the next Transition's previous without ever being reported; patch: update only on success |
| 7 | BH, VG | A program change in a tracked pane prints nothing | low | Not required by the spec; no defined line for it; rejected |
| 8 | BH | Pane Title debug log unreachable at the default level | low | The convention is satisfied (titles only at debug); a level setting is a feature no story asks for; rejected |
| 9 | BH, VG | observed_at test passes with the wall clock; Agent/Removed observedAt unasserted | medium | VG pre-verified; patch with exact test-clock values |
| 10 | BH | Spec vs tracker status mismatch | false | Workflow ordering |
| 11 | BH | SIGTERM untested; no end-to-end Transition through the real client in serve tests | low | SIGTERM shares SIGINT's path; patch only the end-to-end Transition line through the fake socket |
| 12 | BH, ECH | nil Sink/Client panic, Run-once unenforced, Agents before Run blocks, early start errors not in the log, one long doc line | low | Programmer errors or cosmetic; rejected |
| 13 | VG | `agent_detected` for an already tracked pane untested | medium | VG pre-verified: dropping the early return passes; patch |
| 14 | ECH | Rejected subscribe of an unknown pane during a read lets that read re-track it | low | `onSubscribed` deletes it without marking `removedDuring`; one-line patch |
| 15 | ECH | An event-detected pane can be reported by a read that predates its subscription | medium | `apply` reports any `!known` pane; spec: subscribe before reading its state; patch: such a pane is reported only by a read started after its subscription was confirmed |
| 16 | ECH | A second Ctrl-C cannot force-quit a hung shutdown | low | Direct fix: restore default handling after the first signal; patch |
| 17 | ECH | An existing `herddash.log` keeps broader permissions | low | OpenFile's mode applies only on create; direct chmod; patch |
| 18 | ECH | SIGPIPE on `herddash \| head` kills serve | low | Standard Unix behaviour for a closed stdout; rejected |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
