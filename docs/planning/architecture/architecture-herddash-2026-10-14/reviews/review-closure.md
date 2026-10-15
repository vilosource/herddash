---
review: closure
lens: 'Verify, adversarially, that the revised spine and ADR-0004..0007 close every Critical/High finding of the five reviews; then retry the incompatible-pair attack one level down'
target: docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md (updated 2026-10-15, working tree)
context: [docs/adr/0004-runtime-dependencies.md, docs/adr/0005-append-only-transition-record.md, docs/adr/0006-how-herddash-speaks-to-herdr.md, docs/adr/0007-read-only-loopback-http-surface.md, docs/prd.md, docs/planning/prds/prd-herddash-2026-10-08/addendum.md, reviews/*.md]
date: '2026-10-15'
---

# Closure review: revised spine and ADR-0004..0007

## Verdict

**Close, not closed.** 20 findings were in scope: every Critical (adversarial "Blocking") and High
finding, plus the rubric's two Medium-High findings (F3, F4), counted because their own label
includes High.

| Result | Count |
| --- | --- |
| CLOSED | 12 |
| PARTIAL | 7 |
| OPEN | 1 |

The revision fixed the big structural holes: one fold, one transition constructor, one writer, a
state-first stream, a sort key, an engine that owns all decisions, a flock, and an epoch in the key.
What remains sits at the edges of those fixes. The importer has no epoch rule. The Retention floor
protects the wrong Transition. The engine publishes views but not the events that SSE needs. And the
new epoch contradicts one of FR-8's testable consequences. The new pass found five incompatible pairs:
N1 to N4 in §2 and N5 in the note at the end of §4.

## 1. Finding-by-finding

### review-rubric.md

| Finding | Result | Evidence |
| --- | --- | --- |
| F1 High: identity and state across restarts | **PARTIAL** | Seeding is closed by AD-12: "at start and after each resync, the engine loads each Agent's history through one `store` read and passes it to `core.Fold`". Exactness is closed by AD-12's exact/lower-bound bullets. A server-generation component is in AD-3 as `AgentKey{Host, Server, Epoch, PaneID}`. **Still open:** the fix's third part, "make the same decision for how backfilled rows join to live AgentKeys". AD-13 "Same identity" gives the importer Host and Server only. No text says what Epoch an imported row carries (pair N2). |
| F2 High: import graph forbids the history route | **CLOSED** | The graph has `httpapi -->\|history reads\| store`, and the Rule says "`httpapi` may call only `store`'s read functions; only `engine` and `importer` call `store.Append`." |
| F3 Medium-High: per-Agent SSE vs "Board computes nothing" | **PARTIAL** | AD-1 now gives a sort key, "(zone, class rank, start instant, workspace label, AgentKey)", and permits the Board to sort by it. **Still open:** "workspace group order in the lower zone is also unspecified". The tuple as written does not keep workspace groups contiguous (pair N1). |
| F4 Medium-High: operational envelope silent | **PARTIAL** | Mostly closed. Default config and data paths are in AD-8. WAL, refusal of a newer schema and single-transaction migrations are in AD-10. AD-9 covers umask `077`, `flock` and release by the OS. Deployment covers SIGINT/SIGTERM. Deferred covers backup, rotation, port and units. **Still open:** where the log goes. AD-9 says "the log" is `0600`, which implies a file, but no AD gives its path or says whether it is stderr. That is the "two stories pick stderr and a file" divergence the finding named. |

### review-adversarial.md

| # | Finding | Result | Evidence |
| --- | --- | --- | --- |
| 1 | Blocking: reconcile and resync have two owners | **CLOSED** | AD-11: "`internal/herdr` provides dial, ping, snapshot, subscribe, unsubscribe and stream-health signals, and nothing that decides. The engine owns: the pane set and the reconcile timer; the decision to resync, a guard allowing one resync at a time…". ADR-0006 Decision bullet 1 matches. |
| 2 | Blocking: SSE sequence drops events; fetch-then-subscribe gap | **CLOSED** | AD-7: "the Board opens `/events` and fetches nothing first… first event is `state`… The client applies events in arrival order. A dropped stream is reopened and begins again with `state`." The run id stops sequence numbers being compared across restarts. ADR-0007 also has "no `Last-Event-ID` handling". |
| 3 | Blocking: Box order cannot travel in one Agent's event | **CLOSED** for the pair as posed | AD-1 defines a sort key on the start instant, so order never changes without an event, and adds a golden fixture. The grouping defect in the key is a new pair, N1. |
| 4 | Blocking: Waiting Time needs history the engine lacks | **PARTIAL** | 4a is closed by "one `store` read… `core.Fold`" (AD-12). 4c is closed by "Exact only when that Transition was observed `live` and the Status seen on reconnect is unchanged". 4b (`ever_working`) is closed by AD-4: the sweep "never removes an Agent's latest Transition or its latest `working` Transition". **Still open:** the fix asked the sweep to keep "the one that defines its current class entry". AD-4 protects the latest Transition and the latest `working` Transition instead, so the class-entry Transition can be swept (pair N3). That also breaks FR-14's "contains the Transition that defines its current Waiting Time". |
| 5 | Blocking: importer and engine build different Transitions | **PARTIAL** | 5a is closed: "Unknown means NULL" (AD-13, ADR-0005). 5b is closed: "importer gets Host and Server from the same `internal/config` function", with the `importer --> config` arrow. 5c is closed: "unique `import_key`". 5e is closed: "The operator supplies the date the Host reached herdr 0.9.2". **Still open:** 5d, which Profile an imported row carries (the log's own label, or re-resolution through today's Profile Map), is not stated anywhere. The new Epoch field also has no importer rule (N2). |
| 6 | Blocking: pane ids not stable across herdr restarts | **PARTIAL** | Epoch is in AgentKey (AD-3, ADR-0005), and its detection is Open Question 1, owned by milestone 1 before the columns freeze. That is a legitimate deferral. **Still open:** the same finding's importer half ("`p3` in March and `p3` in October are different Agents under one key"). The plugin log spans many herdr restarts, and no rule assigns epochs to imported rows (N2). The epoch also collides with FR-8 (N4). |
| 7 | High: Profile resolution sync or async | **CLOSED** | AD-14: "resolves the Profile… before recording an Agent's first Transition, with a bounded timeout that yields `unresolved`". The re-resolution trigger is Open Question 2, with an owner. |
| 8 | High: lock mechanism undefined | **CLOSED** | AD-9: "`store.Open` takes an exclusive advisory `flock`… for serve and import alike… The operating system releases it when the process dies." |
| 9 | High: socket resolution and Server naming have two owners | **CLOSED** | AD-3 says `internal/config` "resolves both in one function used by serve and import alike". AD-8 says it "owns socket-path resolution". ADR-0006: "resolved by `internal/config` and handed to `internal/herdr`". Deferred: "v1 observes one server". Residual: no text names the Server for a pinned path that fits neither pattern. Only one function decides it, so this is a gap rather than a two-unit divergence. |
| 10 | High: ConnectionTruth has no shape | **PARTIAL** | The shape is closed: "`{connected, last_snapshot_at, herdr_version, gate: ok\|advisory\|refused, resync_guaranteed}`" (Conventions). **Still open:** the folded-in pair 16. Under `gate: refused`, does the engine publish AgentViews? FR-16 says it "records nothing", but the spine never says whether in-memory state and Boxes are published. One engine unit shows ten Boxes under the strip, another shows none. |
| 11 | High: removal, who diffs, and what a removal carries | **OPEN** | AD-2 publishes "an immutable view… holding the current `AgentView`s, the ConnectionTruth, a view sequence number and the run id", with no change list. AD-7 requires `agent`, `removed`, `transition` and `resync` events "in publish order". `transition` and `resync` cannot be derived by diffing two views. A Transition that leaves the AgentView unchanged, or a resync that changes nothing, produces no diff. Nothing says who produces them, or what happens to a slow SSE client that skips views (bounded queue then close, or drop). The fix, publishing each view with its delta and closing a client on overflow, was not adopted. The payload shapes of `transition`, `resync` and `removed` are also unfixed. |

### review-tech-currency.md

| Finding | Result | Evidence |
| --- | --- | --- |
| F-1 High: libc pin, upgrades not ordinary | **CLOSED** | ADR-0004: "`modernc.org/libc`, pinned to exactly the version the driver's `go.mod` requires… upgraded together and never separately, so a blanket `go get -u` is not used". "It requires Go 1.26 or newer, which sets herddash's own minimum." The optional CI check is not adopted, which is acceptable. |

### reconcile-prd.md

| Finding | Result | Evidence |
| --- | --- | --- |
| H1: Daemon restart loses Waiting Time | **CLOSED** | AD-12, as for rubric F1. The PRD glossary was amended to match: "A restart across which the Status did not change keeps the exact time (FR-8)." |
| H2: lower-bound Waiting Time has no provenance rule | **CLOSED** | AD-12: "Exact only when that Transition was observed `live`… `backfill` and possibly-spurious starts are always lower bounds." The review asked that a possibly-spurious entry never define a start. The spine chose "always a lower bound", which meets UJ-3's "never a smaller number claimed as exact". |

### reconcile-adrs-research.md

| Finding | Result | Evidence |
| --- | --- | --- |
| H1: events as invalidation vs recording live Transitions | **CLOSED** in spine and ADR | AD-11 Event handling: "a status event is recorded at once as a `live` Transition… The snapshot is the truth for which panes exist… Buffered events are never replayed onto a snapshot. Events that arrive during a snapshot read cause another read." ADR-0006 matches. Addendum §B still says the opposite (see §3). |
| H2: restored panes; pane-to-pid step | **CLOSED** | AD-14: "A pane herdr restored without `CLAUDE_CONFIG_DIR` resolves to the absent case, which is the true Profile." The pid source is Open Question 2, owned by milestone 1. |

## 2. New incompatible pairs, one level down

Each pair below obeys every AD to the letter and still integrates wrongly. Pairs already reported
above as still open (refused-gate publishing, view-without-delta) are not repeated.

### N1: core sort key vs the Board's workspace groups (core / Board JS)

FR-3 says: "Below it, Working then Unknown, grouped by workspace." AD-1 fixes the key as "(zone,
class rank, start instant, workspace label, AgentKey)". The workspace label is fourth.

- **Core, built literally:** in the lower zone it orders all Working Boxes across every workspace
  by start instant, then all Unknown Boxes, with the workspace label only breaking ties.
- **Board, built literally:** "orders Boxes by the sort key" and renders a workspace label per
  group. Either it draws a header for every contiguous run, giving the same workspace several
  times, or it gathers each workspace into one group, which reorders and breaks "orders by the
  sort key".

AD-1's golden fixture passes in both cases, because it compares order and not grouping. The order
of the workspace groups is also undecided.

**Fix:** within the grouped zone, the key is (zone, workspace label, class rank, start instant,
AgentKey), and the AD names the group order (by workspace label, say).

### N2: importer vs engine on Epoch (importer / engine)

AD-13 "Same identity" covers Host and Server. AD-3 makes Epoch part of every store row's identity
and has the engine set it "when the engine detects a new herdr server instance". The importer
never talks to herdr, so it cannot detect instances.

- **Importer A** writes one constant epoch (0, or the current one). March's `p3` and October's
  `p3` become one Agent, and if it uses the current epoch, an imported row joins a live Agent
  wrongly.
- **Importer B** writes NULL, or a per-import epoch. No backfilled row ever shares an AgentKey with
  a live Agent, so the glossary's lower bound "from the earliest evidence (a backfilled… Transition)"
  silently never applies.

Both obey AD-13. **Fix:** an AD-13 bullet that says what epoch imported rows carry, and whether
backfill can ever join a live AgentKey. If it cannot, say so, and drop "backfilled" from the
glossary's evidence list.

### N3: Retention sweep vs core fold (store / core)

AD-4 keeps "an Agent's latest Transition or its latest `working` Transition". Take an Agent that
goes `working`(T0) → `done`(T1, live) → `idle`(T2, live) and is never prompted again. Once T1 is
older than Retention, the sweep obeys AD-4 and deletes the `done` row, keeping `idle` (the latest)
and `working` (the latest `working`). After the next restart, `core.Fold` sees `working → idle`,
so the Agent is Finished with its start at T2, and provenance is `live`. AD-12 then calls the
Waiting Time **exact**, but it is short by T2 − T1.

That is the "smaller number claimed as exact" which UJ-3 and FR-2 forbid. FR-14's "contains the
Transition that defines its current Waiting Time" also fails. Retention set to 1 day for testing
reproduces it in a day.

**Fix:** the sweep keeps the Transition that began the current Attention Class. Alternatively,
when that Transition is missing, the fold reports a lower bound.

### N4: epoch detection vs FR-8's herdr-restart consequence (herdr client and engine / engine recording)

FR-8 says: "herdr restarted: no Transition is recorded for an Agent whose Status is the same
before and after, other than the resync marker." AD-3 says: "Epoch changes when the engine detects
a new herdr server instance". A herdr restart is a new server instance.

- **The epoch-detection unit** bumps the epoch on restart (say, on a changed instance identity
  from `ping`). Every Agent becomes a new AgentKey.
- **The recording unit** records each new AgentKey's first Transition (`snapshot` provenance),
  which is correct per AD-11 and AD-14. Every Box is removed and re-added, and every Finished
  Agent's Waiting Time falls back to a lower bound.

FR-8's test then fails. ADR-0005's line that "if… herdr keeps pane ids across restarts, the epoch
still costs nothing: it simply changes less often" suggests a different intent: the epoch changes
only when ids are not kept. AD-3 does not say that.

**Fix:** state in AD-3 that the epoch changes only when continuity cannot be established, with
the test settled by Open Question 1. Alternatively, amend FR-8's consequence to accept one
`snapshot` Transition per Agent on a herdr restart.

### Unenforceable rule, not a pair

AD-1's "shared golden fixture tests that the Board's order equals core's" has no runner. `go test`
cannot execute the Board's JavaScript without a JS engine, and the stack has "no npm, no build
step". ADR-0004 forbids new dependencies without an ADR. Decide where the Board half of this test
runs, or drop it.

## 3. Agreement check: spine, ADRs, PRD (and addendum)

| Topic | Agreement | Disagreements |
| --- | --- | --- |
| herdr minimum 0.9.5 | Spine Stack, ADR-0006 (Context, version gate, wire types), PRD §2.2 and §6.1, CLAUDE.md and README agree. | (a) The spine Stack says "0.8.2 **only** behind FR-16's dogfooding flag". ADR-0006 says "Older servers are refused unless FR-16's dogfooding flag is set", which admits any older version. A gate builder reading the Stack refuses 0.9.3 under the flag; one reading the ADR admits it. (b) PRD §8 Q2 and CLAUDE.md say one-request-per-connection is checked "on a 0.9.3 binary". Spine Open Questions say all four are answered "against a herdr 0.9.5 binary". (c) Addendum §F says types are generated from "a real 0.9.3 binary". ADR-0006 says "of a 0.9.5 binary". |
| Go 1.26 | Spine Stack, ADR-0004, addendum §F, CLAUDE.md, CONTRIBUTING.md and README agree. The PRD is silent. | None. |
| History route shape | Spine AD-3 and AD-6, ADR-0007 and addendum §A all give `/api/agents/{host}/{server}/{epoch}/{pane}/history`. | PRD FR-14 "identified by its pane" is loose but not contradictory. Epoch type and path encoding (a `%2F` in host or server, per tech-currency F-6) are unfixed. |
| SSE state-first | Spine AD-7, ADR-0007 and addendum §A ("opens /events (first event is the whole state)") agree. | PRD FR-12 consequence: "The Board renders from **this response** [`/api/state`] alone on first load." AD-7 says "the Board… fetches nothing first". Neither text says the `state` event's `data` is the `/api/state` body, so the FR-12 test has nothing to bind to. Either state that equality in AD-7, or amend FR-12. |
| Retention exceptions | Spine AD-4 and ADR-0005 agree word for word: the latest Transition and the latest `working` Transition survive. | PRD FR-9 consequence: "With Retention set to N days, **no Transition older than N days remains** after the next sweep." The exceptions contradict it, and the PRD was not amended. The exception set is also the wrong one (N3). |
| AgentKey with epoch | Spine AD-3, ADR-0005 (title and Decision), ADR-0007's route, addendum §A and the ADR README title agree. | (a) PRD FR-8's herdr-restart consequence conflicts with epoch-per-instance (N4). (b) There is no importer epoch rule (N2). (c) PRD FR-6's field list and FR-12's "Host and herdr server" omit the epoch. That is additive and acceptable, but FR-12 should list it, since a client needs it to call the history route. |
| Other drift found on the way | | Addendum §B still says "events are invalidation signals, never deltas". That contradicts AD-11 and ADR-0006 ("Status events are recorded as live observations"). It is the drift that reconcile-adrs-research H1 warned about, now confined to the addendum. |

## 4. What would close it

1. AD-13: say what epoch an imported row carries, whether backfill can join a live AgentKey, and
   which Profile an imported row stores (N2, adversarial 5d).
2. AD-4 and ADR-0005: the Retention floor is the Transition that began the current Attention
   Class. Amend PRD FR-9's consequence to name the exception (N3).
3. AD-3: the epoch changes only when continuity cannot be established, or amend FR-8 (N4).
4. AD-2 and AD-7: the engine publishes each view with its change list (`changed`, `removed`,
   `transitions`, `resync`). Each SSE client has a bounded queue and is closed on overflow. Fix
   each event's payload shape (adversarial 11).
5. AD-2: say what is published under `gate: refused` (adversarial 10).
6. AD-1: put the workspace label before class rank in the grouped zone, name the group order, and
   say where the Board half of the golden test runs (N1).
7. AD-6: name the Board's static asset routes, or require the HTML to be inline (see the note).
8. AD-7: the `state` event's `data` equals the `/api/state` body (FR-12).
9. One line on where the log goes (rubric F4). Align the Stack's "0.8.2 only" with ADR-0006.
   Amend addendum §B and §F, and PRD §8 Q2, to 0.9.5 and to live-event recording.

**Note on AD-6 and Board assets (a fifth pair, N5).** AD-6 and ADR-0007 list the routes
exhaustively: "`/` (root only), `/api/state`, the history route and `/events`". The Stack says the
Board is "hand-written HTML, CSS and JavaScript, embedded", and the seed says
"`web/` holds embedded Board assets". A Board epic that ships `board.js` and `board.css` as files,
and an httpapi epic that serves exactly the AD-6 routes, integrate into a Board whose scripts and
styles return the JSON 404. Tech-currency F-6 recommended "`GET /{$}` plus explicit asset routes";
the revision kept the first half and dropped the second.
