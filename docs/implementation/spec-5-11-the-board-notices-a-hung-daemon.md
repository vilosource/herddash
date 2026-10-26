---
title: 'Story 5.11: The Board notices a hung Daemon'
type: 'bugfix'
created: '2026-10-23'
status: 'done'
baseline_revision: 'c264789f443bc89e02aa03ce8202261739b2d456'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0007-read-only-loopback-http-surface.md'
  - '{project-root}/docs/implementation/epic-3-retro-2026-10-23.md'
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** The Daemon's keepalive is an SSE comment the page never sees, and the Board marks the stream lost only on EventSource `error`, so a wedged or SIGSTOPped Daemon (or a half-open socket) leaves stale Boxes undimmed with no strip (Epic 3 retro D1). A `state` event that fails validation, an event `applyEvent` rejects, or a skipped view sequence also go unnoticed (D2, D3), the version strip says "herdr (version unknown) is below the supported minimum" when the version could not be read (D9), and no test replays real engine publications through the Board's `applyEvent` (D5, D6).

**Approach:** Send the keepalive as a named `keepalive` event (ADR-0012, amending ADR-0007 and the spine's SSE convention by adding one event type and nothing else). Teach `order.mjs`'s `streamDecision` to treat silence beyond a bound, a rejected event, and a sequence gap as a lost stream that it reopens (a reopened `/events` stream begins with exactly the `/api/state` body, which is the "fresh `/api/state` load" AD-7 allows). Reword the unknown-version strips. Add a Go test that runs the real engine behind the real SSE handler and replays every captured event through `order.mjs` in Node, comparing the Board model with every published View.

## Boundaries & Constraints

**Always:**
- The keepalive frame is exactly `event: keepalive\ndata: {}\n\n`: no `id:` line, so `id:` stays the view sequence of real changes. It is sent on the same 15 s ticker as today.
- Every input to `streamDecision` stays pure: time comes in as `now` on the input; `order.mjs` never reads the clock.
- Silence is measured from the last event of any name (keepalive included) on the current source, or from when that source was opened or (re)connected. `STREAM_SILENCE_MS` is 35000 and must stay above twice the Go `keepaliveInterval`, pinned by a Go test that reads the constant from the embedded `order.mjs`.
- A reopen for silence shows the lost strip at once (lostSince is the last sign of life, already past `LOST_GRACE_MS`). A reopen for a gap or a rejected event marks lostSince = now, so a reopen that heals within the grace period does not flash the strip.
- Every reopen goes through the existing backoff (`delay`, `nextReconnectDelay`, reset by a valid `state`), closes the current source, and is ignored while a reopen is already pending.
- Sequence checks: the valid `state` event's `id` sets the last sequence; a later handled event whose `id` parses as an integer must equal the last sequence or the last plus one. Anything else is a gap. An event whose `id` is empty or absent is not checked. Keepalive and resync are never sequence-checked.
- Re-arm `awaitingState` (and the silence clock) on EventSource's own `open` event, as well as on board.mjs's own open step.
- Conventional Commits, no AI attribution, no new dependency, no npm.

**Never:**
- No fetch of `/api/state` from the page (AD-7: the Board opens `/events` and fetches nothing first).
- No new route, no change to any other event's shape, no `Last-Event-ID` handling, no server-side event history.
- Do not change `core.DecideGate`, the engine, or ConnectionTruth's shape. Story 5.12 owns snapshot-failure handling.
- Do not try to recognise an unparsable but non-empty `herdr_version` in the page; only an empty or absent one is "could not be read".
- Do not touch `docs/implementation/sprint-status.yaml` or `docs/implementation/epic-5-context.md`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Keepalive keeps the stream alive | valid state, then only `keepalive` events every 15 s | not lost, no reopen | none |
| Hung Daemon | valid state, then nothing for > 35 s, tick arrives | lost strip at once, Boxes dimmed, current source closed, reopen after the current delay | repeated silence on the reopened source backs off (1 s, 2 s, 4 s ... 30 s) |
| Silence while a reopen is pending | pending true, tick past the bound | nothing more | none |
| Recovery | reopened source delivers a valid state | strip cleared, delay reset | none |
| Rejected `state` | current source, state data fails `applyEvent` | warning logged, lostSince = now, source closed, reopen after delay | backoff if it repeats |
| Rejected delta | valid state, then an `agent`/`transition`/`removed`/`connection` event `applyEvent` rejects | same as rejected state | same |
| Sequence gap | state id 7, then an event with id 9 (or 6) | lost from now, reopen after delay | same |
| Same publication | state id 7, then events with ids 8, 8, 9 | applied in order, no reopen | none |
| Version unreadable, refused | gate refused, `herdr_version` empty or absent | "herddash could not read herdr's version. It needs 0.9.5 or newer and is recording nothing." | none |
| Version unreadable, advisory | gate advisory, `herdr_version` empty or absent | "herddash could not read herdr's version; recording under the dogfooding flag." plus the existing resync sentence when `resync_guaranteed` is not true | none |

</intent-contract>

## Code Map

- `internal/httpapi/events.go:16-40` -- tunables and the event-name constants; add `eventKeepalive = "keepalive"`. `:255-270` the keepalive ticker writes `": keepalive\n\n"`; write the named frame instead (no id, data `{}`). `forward` (`:212`) skips publications with `Seq <= v.Seq` and drops unknown kinds at `:228`: unchanged, the client's gap check now catches a publication whose kinds were all dropped. Doc comment of `serveEvents` mentions events; add the keepalive.
- `internal/engine/view.go:20-22,187-225` -- read-only evidence: `Seq` rises by exactly one per publication and every publication carries at least one Change, so consecutive event ids differ by 0 or 1 unless something was skipped. A slow subscriber's channel is closed (stream ends), never thinned.
- `internal/httpapi/events_test.go:104-160` -- `sseEvent`, `eventsServer`, `openStream`, `stream.next`: reuse. `:466` `TestEventsKeepalive` expects the comment: change it to expect `{event: "keepalive", data: "{}"}` with empty id.
- `internal/httpapi/web/order.mjs:346` `EVENT_NAMES` (add `"keepalive"`), `:350` `HANDLED_EVENTS` (unchanged), `:361-400` `applyEvent` (unchanged), `:410-480` `streamState`/`streamDecision`/`streamLost` (extend: `lastEventAt`, `lastSeq`, inputs `open` and `connected` carry `now`, event input carries `id` and `now`, new `tick` input; reopening decisions return the source to close), `:497-560` `boardStatus` and `versionLabel` (unknown-version wording).
- `internal/httpapi/web/board.mjs:306-337` -- `step` closes `input.source` on `reopenAfter`; generalise to close the source the decision names. `open()` passes `now`, listens for EventSource `open` (input `connected`), passes `e.lastEventId` and `now` on events. `tick()` (`:290`) also feeds `{kind: "tick", now}` through `step`.
- `internal/httpapi/web/order.test.mjs:628-735` -- stream tests to extend; `:628` asserts `EVENT_NAMES` exactly; `:963-972` asserts the old unknown-version wording: replace with the matrix's wording.
- `internal/httpapi/web/check/board-browser.mjs:400-430` -- the "serve stopped" lost case; add a hung case before it: send SIGSTOP to the serve process (expose a `signal` alongside `stop` in `serve()`), wait for the lost strip and dimming (allow `STREAM_SILENCE_MS` imported from `../order.mjs` plus a margin), SIGCONT, wait for the strip to clear. Update the header comment to name the hung case.
- `internal/engine/fake_test.go:30-400` -- model for a minimal scripted `engine.Client` (`fakeSub`, `fakeClient`); the contract test needs its own small copy in package `httpapi` (test file). `internal/engine/engine_test.go` `TestStart:54`, `TestLiveChange:84`, `TestNewAgent:133`, `TestRemoval:208`, `TestRestarted:753` (epoch change: herdr restarts with new terminal ids), `internal/engine/view_test.go` `TestViewContextChange:516` show how each scenario is driven. `engine.Options` (`internal/engine/engine.go:24-70`): set `Environ`, `Children`, `Session`, `Transcript` to fakes so no real process is read; `Sink` must be non-nil (a no-op implementation of the seven methods in `internal/engine/sink.go:59-90`); `Store` nil.
- `docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:185-199,310` -- AD-7 change-events bullet and the SSE convention row name the event set.
- `docs/adr/0007-read-only-loopback-http-surface.md`, `docs/adr/0011-*.md` -- ADR style; `docs/adr/README.md` index.
- `docs/design/board-design.md:169-195` -- Strips section: advisory/refused wording and where to add the silent-Daemon paragraph.
- `docs/implementation/epic-3-retro-2026-10-23.md` -- D1, D2, D3, D5, D6, D9 and action items 1, 2, 5.

## Tasks & Acceptance

**Execution:**
- `docs/adr/0012-an-observable-keepalive-on-the-event-stream.md` -- write the ADR (Status: Accepted 2026-10-23, under the operator's Epic 5 delegation for a change that only adds an event type; Context, Decision, Consequences, Alternatives: comment kept plus a page timer is impossible because EventSource hides comments; reuse `connection` events would lie about ConnectionTruth changes; client polling is FR-4's rejected option). `docs/adr/README.md` -- add the row.
- `ARCHITECTURE-SPINE.md` -- add `keepalive` to AD-7's change-events bullet (as a liveness event that is not a change, sent with no `id:`) and to the SSE convention row, citing ADR-0012.
- `internal/httpapi/events.go` -- named keepalive frame and constant; doc comments.
- `internal/httpapi/events_test.go` -- adapt `TestEventsKeepalive`; add a test that reads `STREAM_SILENCE_MS` from the embedded `web/order.mjs` and fails unless it exceeds `2*keepaliveInterval`.
- `internal/httpapi/engine_contract_test.go` (new) -- run a real `engine.Engine` (scripted fake Client, no-op Sink, fake procenv funcs, injected clock not required) behind `New(...)` on an `httptest` server; subscribe to the engine itself as well to capture every `Publication`'s View. Drive: start with two Agents; a live Status change; an Agent removed (closed); a new Agent detected; a context change; herdr restarting with new terminal ids (epoch change: mass removal and re-add); herdr lost and back (connection events). Collect every SSE event (name, id, data) until the stream's last id equals the engine's last Seq. Write events and the captured Views to a temp file and run `node internal/httpapi/testdata/replay-board.mjs <file>`; the script feeds the events through `streamDecision` (so seq and rejection checks run too) and prints, for every sequence number, the model's agents (sorted by `keyId`) and connection after that sequence's last event, plus any warning or reopen. The Go test asserts no warning or reopen, and that for every captured View the model equals `View.Agents` (as a set, JSON-equal) and `View.Connection`. When `node` is not on PATH, skip, unless `CI` is set, then fail.
- `internal/httpapi/testdata/replay-board.mjs` (new) -- the replay script above; imports `../web/order.mjs` only, Node built-ins only.
- `internal/httpapi/web/order.mjs` -- the stream changes and the version wording in the matrix; doc comments describe every input and decision.
- `internal/httpapi/web/board.mjs` -- wire the new inputs and the close-then-reopen.
- `internal/httpapi/web/order.test.mjs` -- one test per matrix row (both version rows), plus: `connected` re-arms `awaitingState`; events from a non-current source never move the silence clock; a gap or rejection before the first valid state on a source is not checked (events are ignored until state).
- `internal/httpapi/web/check/board-browser.mjs` -- the hung (SIGSTOP/SIGCONT) case.
- `docs/design/board-design.md` -- Strips: the unknown-version wording, and a dated paragraph: a Daemon silent past the bound, a rejected event or a skipped sequence is a lost stream, shown as the lost strip and reopened.
- `docs/implementation/deferred-work.md` -- if it lists any of D1, D2, D3, D5, D6, D9, mark them resolved by Story 5.11 with where.

**Acceptance Criteria:**
- Given an open Board whose Daemon is SIGSTOPped, when more than `STREAM_SILENCE_MS` passes, then the Board shows "Lost contact with herddash. Reconnecting…" and dims every Box, and after SIGCONT the strip clears with no console warning (browser check).
- Given the real engine behind the real SSE handler, when its publications are replayed through `order.mjs` in Node, then after every publication the Board model equals that publication's View and no event was rejected or seen as a gap (`go test ./internal/httpapi`).
- Given the whole module, when the Verification commands run, then all pass.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 20 findings — high 0, medium 1, low 11, false 8, maybe-false 0 (the intent-alignment layer reported readings and divergences only, no defects; its notes are recorded under Auto Run Result)
- findings:
  - `[medium]` `[patch]` (verification-gap) Nothing checks that board.mjs passes `e.lastEventId` into streamDecision, so the gap check could be disabled silently — added a stubbed-EventSource case to `check/board-browser.mjs` (state id 1, agent id 3) asserting the "skipped from view 1 to 3" warning and a new `/events` source; the final warnings check ignores only that exact warning. The implementer confirmed the case fails with the id line removed.
  - `[low]` `[reject]` (verification-gap) `check("serve resumed …", true, s)` asserts nothing — grouped with the blind finding below on the same check; fixed there.
  - `[false]` `[reject]` (verification-gap) The hung check's lower bound `STREAM_SILENCE_MS - 15000` is weak — silence is measured from the last keepalive, which can precede SIGSTOP by up to the 15 s interval, so 20 s is the tightest bound the check can assert.
  - `[low]` `[reject]` (edge) Daemon down > 35 s: a silence reopen replaces EventSource's own 2 s retry with backoff up to 30 s — bounded by the same 30 s cap the existing 503 path already uses; the strip is shown throughout; the fix needs a new retrying flag in the stream state.
  - `[low]` `[reject]` (edge) Sleep/wake: the first tick after wake sees > 35 s of silence and reopens with the strip shown at once — after a sleep the Board's data is genuinely unconfirmed and the reopen delivers a fresh state within about a second; the fix needs tick-gap state.
  - `[low]` `[reject]` (edge) A resync with an id past lastSeq + 1 advances lastSeq without a gap check — needs a publication of only unknown kinds right before a resync; the spec's Always rule says resync is never sequence-checked, so changing it edits this build's spec.
  - `[false]` `[reject]` (edge) View sequence above 2^53 loses precision — one publication per visible change; 2^53 publications is unreachable.
  - `[low]` `[patch]` (edge) `await` in engine_contract_test.go panics on `vs[len(vs)-1]` when nothing was published — now fails with "no publication at all, waiting for: …".
  - `[false]` `[reject]` (edge) Rejected event while earlier trouble is outstanding keeps the earlier lostSince — the loss is continuous since that earlier trouble, so showing the strip by its age is correct; the matrix's "lostSince = now" is for a healthy stream.
  - `[low]` `[reject]` (blind) Resync can hide a skipped publication — same as the edge finding; same reason.
  - `[false]` `[reject]` (blind) Silence detection fails open when `open` has no `now` — board.mjs always passes `now`; only unit tests open without it.
  - `[low]` `[reject]` (blind) Sleep/wake and clock steps give a false lost — same as the edge finding; same reason.
  - `[false]` `[reject]` (blind) Browsers keep `lastEventId` across a keepalive, so tests model ids board.mjs never sends — keepalive and resync are excluded by name, not by id, so the browser's sticky id never reaches the check; the replay script reads raw wire ids, where a keepalive's id is genuinely empty.
  - `[low]` `[patch]` (blind) `await` panics when nothing was published — same as the edge finding; same fix.
  - `[low]` `[reject]` (blind) The contract test never produces a resync — resync changes no model and the replay already covers the kinds that do; not required by the story's criteria.
  - `[low]` `[reject]` (blind) No guard that every future engine Change kind maps to an event — pre-existing (`TestEventNames` pins today's kinds); a Go enum of kinds cannot be enumerated; the new gap check now makes such a drop visible instead of silent.
  - `[low]` `[patch]` (blind) The SIGCONT recovery check proves nothing — records the Box count before SIGSTOP and asserts no strip, no `board--stale` and the same count after recovery.
  - `[false]` `[reject]` (blind) Detection lower bound weak and hardcoded — same refutation as the verification-gap row.
  - `[false]` `[reject]` (blind) Browser-check header stale against Story 5.6 — this branch is cut from c264789, where the check is not in CI; reconciling the header and the CI job time is a merge concern, reported to the orchestrator.
  - `[false]` `[reject]` (blind) Verification commands use a machine-specific PATH inconsistently — the fix edits this build's spec; the commands ran with the 1.26.5 toolchain by absolute path.

## Design Notes

"A skipped sequence number or a rejected `state` event triggers a fresh `/api/state` load" is met by reopening `/events`: AD-7 makes a stream's first event exactly the `/api/state` body, forbids the page fetching before or alongside the stream, and ADR-0007 rejects fetch-then-subscribe because it loses changes in between. Reopening is the only fresh load that cannot lose one.

Expected stream decision shapes (illustrative):

```js
streamDecision(st, { kind: "tick", now })                       // -> { st } or { st, reopenAfter, close: st.source }
streamDecision(st, { kind: "event", source, name, data, id, now }) // gap/rejection -> { st, reopenAfter, close, warning }
```

## Verification

**Commands:**
- `PATH=$HOME/.local/share/mise/installs/go/1.26.5/bin:$PATH go build ./... && go vet ./... && go test ./...` -- expected: all pass
- `PATH=$HOME/.local/share/mise/installs/go/1.26.5/bin:$PATH go test -race -count=20 ./internal/httpapi` -- expected: pass
- `gofmt -l .` -- expected: prints nothing
- `node --test internal/httpapi/web/*.test.mjs` -- expected: all pass
- `go build -o ./herddash-check ./cmd/herddash && node internal/httpapi/web/check/board-browser.mjs ./herddash-check; rm -f ./herddash-check` -- expected: every line `ok`, exit 0

## Auto Run Result

Status: done

**Summary.** `/events` now sends its 15 s keepalive as a named `keepalive` event (data `{}`, no `id:`), recorded as ADR-0012 (Accepted under the operator's Epic 5 delegation: it only adds an event type) and in the spine's AD-7 and SSE convention. The Board's `streamDecision` gains a pure silence check (`STREAM_SILENCE_MS` 35 s, fed by a `tick` input), sequence-gap and rejected-event handling that reopen `/events` through the existing backoff, and re-arms `awaitingState` on EventSource's own `open`. The version strips say "herddash could not read herdr's version" when the version is empty or absent. A Go contract test runs the real engine behind the real SSE handler and replays every event through `order.mjs` in Node, comparing the model with every published View.

**Files changed.**
- `docs/adr/0012-an-observable-keepalive-on-the-event-stream.md` -- new ADR; `docs/adr/README.md` -- index row.
- `docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md` -- AD-7 keepalive bullet, SSE convention row.
- `internal/httpapi/events.go` -- named keepalive frame.
- `internal/httpapi/events_test.go` -- keepalive frame test; `TestStreamSilenceBound` pins `STREAM_SILENCE_MS > 2 × keepaliveInterval`.
- `internal/httpapi/engine_contract_test.go`, `internal/httpapi/testdata/replay-board.mjs` -- real-engine replay contract test (start, live change, removal, new Agent, context change, terminals replaced, epoch restart).
- `internal/httpapi/web/order.mjs` -- silence, gap, rejection, `connected`, version wording.
- `internal/httpapi/web/board.mjs` -- wiring: `now`, `lastEventId`, EventSource `open`, tick through `step`, close the named source.
- `internal/httpapi/web/order.test.mjs` -- one test per matrix row and the extra stream cases.
- `internal/httpapi/web/check/board-browser.mjs` -- hung (SIGSTOP/SIGCONT) case and skipped-publication case.
- `docs/design/board-design.md` -- unknown-version wording; silent-Daemon paragraph.

**Review.** 20 findings: 3 patched (1 medium: browser check for the id wiring; 2 low: contract-test panic, SIGCONT recovery assertion), 0 deferred, 17 rejected with reasons in the triage log. The intent-alignment layer found the diff implements the story with the "fresh `/api/state` load" met by reopening `/events` (AD-7), rejected deltas also reopening, and "unknown version" read as empty or absent; it noted model equality is asserted for `agents` and `connection` (the Board model holds no `headers`, `seq` or `run_id`).

**Follow-up review recommended:** false (one medium and two low patched on a first pass).

**Verification.** `go build ./...`, `go vet ./...`, `go test -count=1 ./...` all ok; `go test -race -count=10 ./internal/httpapi` ok; `gofmt -l .` empty; `node --test internal/httpapi/web/*.test.mjs` 88/88 pass; `board-browser.mjs` against a scratch build: 26/26 ok, exit 0 (binary deleted). `TestEngineContract` ran (not skipped).

**Residual risks.** Story 5.6 (CI runs the browser check) is on another branch: merging needs the check's header reconciled and the CI job's time budget checked, since the hung case adds roughly 25 to 50 s. A Daemon down for minutes can take up to the 30 s backoff cap to reconnect after it returns. A publication made only of unknown kinds immediately before a resync is not detected as a gap.
