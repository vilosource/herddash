# Validation: epics.md against the PRD and the architecture spine

Validated 2026-10-16 against `docs/prd.md` (final, 2026-10-14) and `ARCHITECTURE-SPINE.md`
(final, 2026-10-15). Severity: High means a dev agent cannot finish the story, or the build
contradicts the PRD or spine, without a decision first. Medium means a gap or ambiguity that will
likely be resolved wrongly. Low means polish.

| Category | High | Medium | Low | Total |
| --- | --- | --- | --- | --- |
| 1. FR and testable-consequence coverage | 1 | 6 | 2 | 9 |
| 2. Forward or unowned dependencies | 3 | 4 | 2 | 9 |
| 3. AD-1..AD-14 honoured | 0 | 4 | 6 | 10 |
| 4. Story size | 3 | 4 | 0 | 7 |
| 5. Testability of ACs | 1 | 3 | 4 | 8 |

Every FR has at least one story. The gaps are in individual consequences. AD-1 to AD-14 are each
carried by some story and no AC flatly contradicts a rule. Two come close (A-1, A-2).

---

## 1. FR and testable-consequence coverage

Consequences with no AC, by FR:

**C1-1 (High) FR-1, all three consequences, Board side.**
No AC says a Box appears, is removed, or is gained by a later-detected pane, and nothing says
non-Agent panes get no Box.
- Story 1.5 covers detection and printing only. Its only closing-pane AC is "A pane that closes
  between snapshot and subscribe".
- Story 3.3 names a `removed` event but nothing produces it.
- The engine behaviour "pane closed, so the Agent leaves the present set" has no story. That set
  also drives the Retention floor (2.4: "Agent still present").
- Fix: add to 1.5 "Given an Agent pane closes, or a pane stops being an Agent / When herdr reports
  it / Then the engine drops it from the pane set, cancels its subscription, and prints a removal."
  Add to 3.6 "Given an `agent` event for an unseen key / Then a Box appears with no reload. Given a
  `removed` event / Then its Box disappears." Add a 3.1 or 3.2 AC "a non-Agent pane never appears in
  `/api/state`".

**C1-2 (Medium) FR-3.** No AC for:
- "A Transition into `working` moves that Box out of the top zone into its workspace group
  immediately".
- "A `done` Agent becoming `idle` keeps its place and its Waiting Time" on the Board. 2.3 proves
  only the fold.
- "An `idle` Agent never seen `working` sits with Working, with no Waiting Time emphasis".
- "Order within a class changes as Waiting Times grow, without reload".
- "Needs you is visually dominant". 3.6 never says the Board applies 3.5's emphasis.

Fix: add 3.6 ACs for working leaving the top zone, `done` to `idle` not moving the Box, and the
never-prompted idle Box showing no Waiting Time. Add "Needs you Boxes use the approved emphasis
tokens, checked by a class on the Box in the DOM test".

**C1-3 (Medium) FR-2.** No Box-level AC for "An Agent whose Profile cannot be resolved shows
`unresolved`, never a blank" or "Two Agents with the same Agent Program under different Profiles
show different Profile labels". 1.8 resolves them and 3.5's fixture merely contains `unresolved`.
Fix: add both as 3.6 Given/Then, using the fixture.

**C1-4 (Medium) FR-5.**
- Nothing says the engine sets `connected` to false, or maintains `last_snapshot_at`. 3.2 only
  "returns ConnectionTruth".
- "Stopping herdr produces the disconnected strip within the Daemon's reconnect interval" has no
  time bound.
- Fix: 1.6 gains "Given the herdr connection drops / Then ConnectionTruth `connected` is false
  within one backoff step, and `last_snapshot_at` advances on each completed snapshot". 3.7 gains
  the same bound.

**C1-5 (Medium) FR-6.** "Two Transitions on one Agent are ordered consistently with herdr's own
sequence" has no AC. 2.1 stores "herdr ordering" and 3.4 says "in order", but no test inserts
out-of-clock-order events. Also unspecified: the fallback to `observed_at` when herdr's ordering is
unknown (spine Time convention). Fix: add to 2.1 "Given two Transitions whose ordering values
disagree with their observed_at / When history is read / Then they are returned by herdr ordering;
with NULL ordering, by observed_at".

**C1-6 (Medium) FR-11.**
- "Adding a rule for a new Agent Program changes no Board or API code": only Context Line rules get
  this check (3.1). Nothing checks that a new env-var Profile rule is added by configuration alone.
- 1.2 never says the TOML schema for the Profile Map (rule type and parameters per Agent Program,
  absent case) is parsed and validated. It only ships an example.
- Fix: 1.2 gains "Given a Profile Map entry selecting the env-var rule for `codex` with
  `CODEX_HOME` / Then it resolves with no Go change". It also gains a validation AC for an unknown
  rule type.

**C1-7 (Medium) FR-13.** "The stream carries enough to update one Box without a second request" has
no AC. 3.3 lists event names but not their bodies. Fix: "the `agent` event body is a full
`AgentView`; the `transition` event carries the full Transition and the key; `removed` carries the
key".

**C1-8 (Low) FR-15.**
- "A fresh Host with the binary and a config containing only the Profile Map runs and serves the
  Board": no end-to-end AC.
- "No runtime, package manager or build step on the Host": 4.3 only builds with `CGO_ENABLED=0`.
- "The config can pin the herdr socket path": 1.2 has no such AC.
- Database path and bind address (PRD FR-15 list) are not in 1.2.
- Fix: add one AC to 1.2 for those config keys, and one to 4.3 or 3.6 that the Board is served from
  embedded assets with an empty working directory.

**C1-9 (Low) FR-6.** "A Status change observed while no browser and no terminal client is attached
is in the record" is implicit in 2.1 only. Add one explicit sentence to 2.1.

(Covered, for the record: FR-2a, FR-4, FR-7, FR-8, FR-9, FR-9a, FR-10, FR-12, FR-14, FR-16 and
FR-17 map to ACs in 1.2, 1.7, 1.8, 2.1 to 2.5 and 3.1 to 3.7. FR-17's log-file mode `0600` is
asserted only in 2.1.)

---

## 2. Forward or unowned dependencies

**F-1 (High) Stories 1.6 and 1.7 need the store, which arrives in 2.1.**
- 1.6: "replaces its state, recording any disagreement as a snapshot-derived Transition" and "snapshots
  and records any disagreement".
- 1.7: "recording proceeds".
- The Epic 1 header says "Nothing is stored yet".
- Fix: reword the 1.6 and 1.7 ACs to "emits" or "prints a snapshot-derived Transition", with
  persistence added in 2.1 and 2.2. Alternatively state that 2.1's recording hook is the
  integration point.

**F-2 (High) The engine's published view has no owning story.**
- AD-2 requires an immutable view with a change list, a view sequence and a run id after every
  change.
- 3.2 ("Agents in the engine's published view", "the run id and the view sequence") and 3.3 ("the
  engine publishes a view with a change list") consume it.
- 1.5 only requires "the engine is the only goroutine holding the pane set and current Status".
- Fix: add a story, or an AC to 3.1, owning view construction and publication. For example "Given
  any state change / Then the engine publishes a new immutable view with an incremented sequence,
  the run id, and the list of changed keys, and no reader locks engine state".

**F-3 (High) Story 1.8 needs herdr calls that 1.4 does not build.**
- 1.8 re-resolves "when a pane's foreground process changes".
- The spine says which call yields the foreground process, and which event reports a change, is
  milestone 1's finding. A change event may need a new subscription kind.
- 1.4's scope is ping, snapshot and the two subscription envelopes. Per AD-5 and AD-11 only
  `internal/herdr` may speak to herdr.
- Fix: add to 1.4, after 1.3, "the client exposes the foreground-process lookup and the change
  signal found in 1.3". Alternatively state in 1.8 that it extends `internal/herdr` for them.

**F-4 (Medium) 3.1's AgentView needs data nothing captures.**
- AgentView carries "the shortened directory, the workspace and tab labels", the Pane Title, and the
  workspace's position in herdr's snapshot (the sort key `group`).
- 1.4 and 1.5 decode and track only pane, Agent Program and Status.
- Fix: add to 1.4 or 1.5 "snapshot and status events deliver cwd, workspace id, label and order,
  tab label and pane title into core types". Without it 3.1 reopens Epic 1 packages.

**F-5 (Medium) 2.5 needs schema columns 2.1 does not name.**
- 2.5 needs a unique `import_key` and a possibly-spurious flag.
- 2.1's AC lists columns without them.
- AD-10 says "A migration touching the transitions table needs an ADR first", so adding them in 2.5
  is a process violation.
- Fix: list `import_key` (nullable, unique) and `possibly_spurious` in 2.1's column AC.

**F-6 (Medium) Core domain types have no owning story.**
- AgentKey, Status, Transition and `core.NewTransition` are first needed by 1.4 ("decoded events
  as core types") and named in 2.1.
- Fix: add an AC to 1.1 or 1.4, "`internal/core` defines AgentKey, Status, Transition and
  `NewTransition`, table-tested, with unknown stored as absent".

**F-7 (Medium) 3.6 cannot start without a human approval.** 3.5 ends "which the operator reviews and
approves before Story 3.6 starts". A `bmad-build-auto` loop or a single dev session stalls on it.
Fix: make approval an explicit sprint gate in the story status, not an AC of 3.5. Alternatively let
3.6 start against the fixture and re-skin.

**F-8 (Low) 1.5 prints Agents without a Profile.** Epic 1's goal says each is "labelled with its
Profile", and AD-14 says resolve before the first Transition. 1.5 prints Host, pane, Agent Program
and Status. 1.8 retrofits ("before printing") by changing 1.5's output. Fix: order 1.8's AC as an
amendment to 1.5's line format, or move Profile into the 1.5 output as `unresolved` until 1.8.

**F-9 (Low) 1.7 uses the dogfooding flag that 1.2 does not declare.** 1.2 rejects unknown keys, so
the key must exist by 1.7. Fix: list the flag in 1.2's config key AC.

---

## 3. AD-1..AD-14 check

Each AD is carried by at least one story: AD-1 (3.1, 3.6), AD-2 (1.5, 4.1), AD-3 (1.2, 1.6, 2.5,
3.4), AD-4 (2.1, 2.4), AD-5 (1.4, 1.5), AD-6 (3.2, 3.4), AD-7 (3.3, 3.6), AD-8 (1.2), AD-9 (2.1,
2.5), AD-10 (2.1), AD-11 (1.6, 1.7), AD-12 (2.3), AD-13 (2.1, 2.5), AD-14 (1.8). Findings:

**A-1 (Medium) AD-1, Board does only three things beyond rendering.**
- 3.6: "showing the name once when they are equal". That is a derivation (Profile == Agent Program)
  done by the Board.
- 3.7: "names the version and the minimum". `ConnectionTruth` in the spine has no minimum field, so
  the Board would hardcode 0.9.5.
- Fix: AgentView carries `heading` (or a flag), computed in core and tested with 1.8's case.
  ConnectionTruth gains `min_version`, or the strip text comes from the view.

**A-2 (Medium) AD-5 versus 1.4's "no exported identifier refers to focus or output reads".**
- AD-5 puts generated wire types in `internal/herdr/gen`. Generated from `herdr api schema --json`,
  they will include pane focus and pane read request types.
- The 1.4 test would fail unless scoped.
- Fix: scope the test to the handwritten API of `internal/herdr` and add to the import-graph test
  "only `internal/herdr` imports `internal/herdr/gen`". Alternatively filter those methods out in
  the generator and say so in 1.3.

**A-3 (Medium) AD-3 and AD-13: the importer's identity.** 2.5 gives epoch 0 and Profile but does not
require Host and Server from the same `internal/config` function the Daemon uses. Imported rows
would not join. Fix: add "Then each row's Host and Server equal the values `serve` would resolve
with the same config".

**A-4 (Medium) AD-10 and CLAUDE.md "ADR before stored-schema change".** The spine defers the
transitions columns and indexes "to milestone 2 after the open questions", and ADR-0005 "fixes only
the invariants". No story amends ADR-0005 with the columns. Fix: add to 2.1 "the column list is
recorded in ADR-0005 (or a superseding ADR) in the same change, using the 1.3 findings".

**A-5 (Low) AD-4, no UPDATE.** No AC asserts rows are never updated. Fix: a test that fails if any
non-test SQL in `store` contains UPDATE, or a trigger rejecting it.

**A-6 (Low) AD-12.** 2.3 covers restart. "At start and after each resync, the engine loads each
Agent's history and passes it to `core.Fold`" is not asserted for the resync case.

**A-7 (Low) Logging convention.** "Pane Titles only at debug" has no AC. The log file's `0600` is
asserted only in 2.1, although logging starts in 1.5.

**A-8 (Low) Import-graph rule.** 1.1's test covers the arrows. The spine's two extra rules ("httpapi
may call only store's read functions; only engine and importer call `store.Append`") are enforced
only by 3.4's last AC. Fix: extend 1.1's test, or add a symbol-level check in 2.1.

**A-9 (Low) AD-7 run id.** 3.3 puts the run id in `state`, but 3.6 has no AC using it. The spine's
reason is not comparing sequence numbers across restarts. Fix: "the Board resets its last sequence
on a changed run id".

**A-10 (Low) Go version.** 1.1 declares `go 1.26` (per the spine). `CLAUDE.md` "Build and test" says
"Go 1.25 or newer". Fix: amend CLAUDE.md in 1.1's change.

---

## 4. Stories too big for one dev-agent session

**S-1 (High) Story 2.1.** It bundles `store.Open` with flock, WAL, migrations, version refusal and
modes. It also bundles `core.NewTransition`, `store.Append`, the schema decision, engine integration,
the `daemon_start` marker and Profile Map edit semantics. Split into 2.1a (store: open, lock,
migrations, modes, version refusal) and 2.1b (Transition model, Append, engine recording, marker,
Profile-at-the-time).

**S-2 (High) Story 3.6.** It bundles the SSE client with reconnect, rendering all Box fields with
truncation, hover and equal height, ordering and workspace headers, the live clock, the cross-language
golden test and the blocked-move behaviour. Split into 3.6a (page shell, `/events` client,
state/apply, reconnect), 3.6b (Box rendering, design tokens) and 3.6c (ordering, headers, ticking,
golden test).

**S-3 (High) Story 1.6.** It bundles the resync decision, the one-at-a-time guard and minimum
interval, the re-read loop, backoff, the reconcile timer, and epoch detection. Split into 1.6a
(reconnect and backoff, reconcile timer) and 1.6b (resync guard, re-read, epoch).

**S-4 (Medium) Story 1.8.** It bundles the rule type in core, Profile Map config consumption, the
linux and darwin env readers (separate files, separate OS APIs), timeout, re-resolution and
logging. Split off `internal/procenv` (both OS readers) as its own story.

**S-5 (Medium) Story 1.3.** It is a spike of four investigations that need a real 0.9.5 binary,
plus schema dump, generator choice and generated code, plus research-record amendments. Split
findings from the generator story.

**S-6 (Medium) Story 1.4.** The client has two request types, two subscription envelopes, health
signals, the foreground lookup (F-3), the fake socket server and the anti-focus test. Consider
splitting the fake-server test harness (reused by 1.5 to 1.7) from the client.

**S-7 (Medium) Story 2.3.** It bundles `core.Fold`, exactness, never-prompted class, restart
semantics and the terminal output. It is borderline. Split the terminal output off, or accept it
as one if 2.1 is already split.

---

## 5. ACs that are not testable as written

**T-1 (High) 3.6, "checked by a test that runs the same fixture through Go and the Board's
JavaScript".**
- Nothing in the stack runs JavaScript. CI in 1.1 is Go tooling only. The stack says "no npm, no
  build step". ADR-0004 says "Nothing else" for dependencies.
- The same gap hits "Every Box is the same height", "within one second", and truncation with
  ellipsis, which need a layout engine.
- Fix: decide the Board test method before 3.6. For example a Node-only unit test file with no
  packages, or a pure-JS ordering module run via `node --test` (and add Node to CI). Or an ADR for
  a Go JS interpreter. State layout checks as DOM class or computed-style assertions, or as an
  operator checklist.

**T-2 (Medium) 3.5.** "defines the palette, type and spacing tokens…" and "the operator reviews and
approves" are subjective. NFR14 "readable from across a desk" has no measure. Fix: add measurable
criteria (minimum Box heading size in px, WCAG contrast ratio in both themes, Needs you
distinguishable under greyscale) and a checklist of the delivered artefacts.

**T-3 (Medium) 2.5, first AC.** "Given the plugin log on the operator's machine / When the story
starts" is a process step. Fix: "Given a committed anonymised sample / Then the parser test parses
every line, and a malformed line is skipped with a count reported".

**T-4 (Medium) 4.3, second AC.** "has run on the operator's machine for at least a day" and "the
operator is asked before merging" are process, not verifiable. Fix: move to a release checklist, or
make it "the release PR's version is `0.x`, checked by a workflow step".

**T-5 (Low) 4.1 and 4.2.** "within a few seconds" needs a number (for example 5 s). "not in a tight
loop" needs concrete values (`RestartSec`, launchd `ThrottleInterval`). "documented step by step"
is a review item.

**T-6 (Low) Unspecified constants.** The reconcile interval, minimum resync interval, backoff cap,
Profile timeout and Retention sweep interval are "named constants chosen when first built"
(spine Deferred). 1.6, 1.8 and 2.4 ACs cannot be asserted without them. Fix: allow injection in the
test, and state "defaults documented with the config" in each AC.

**T-7 (Low) 1.5 and 3.1.**
- 1.5 "prints one line per Agent" does not give a format.
- 3.1 "touches only `internal/core` and its documentation" is checkable only by review. Fix: a test
  registers a rule from a table entry alone.

**T-8 (Low) 1.3.** "Given a finding that contradicts the spine or an ADR" is conditional, so it may
never be exercised. Reword as a task checklist item.

## Disposition (2026-10-16)

- **High, all fixed.**
  - C1-1: Board ACs in Story 3.8, and pane removal in 1.5.
  - F-1: Epic 1 prints, it does not record.
  - F-2: new Story 3.2 builds the published view.
  - F-3: the foreground-process lookup is in 1.4.
  - S-1: 2.1 split into 2.1 and 2.2.
  - S-2: 3.6 split into 3.7 and 3.8.
  - S-3: 1.6 split into 1.6 and 1.7.
  - T-1: `node --test` with no npm packages, plus a Go timing test.
- **Medium, fixed:** C1-2 to C1-7, F-4 to F-7, A-1 to A-4. A-1 also amended the spine: ConnectionTruth gains `herdr_minimum`, and AgentView gains `heading`.
- **Medium, deferred to sprint planning:** S-4 to S-6, the size notes on 1.9 (was 1.8), 1.3 and 1.4. Sprint planning may split them if a single session proves too small.
- **Low:** left as polish.
