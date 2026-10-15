# Input reconciliation: PRD and addendum against the spine and ADR-0004..0007

Reviewed 2026-10-15. Inputs: `docs/prd.md` (final), the PRD addendum. Targets: `ARCHITECTURE-SPINE.md`, ADR-0004 to 0007.
Severity: H = a testable FR or a trust property is unowned or contradicted; M = a requirement has no concrete owner or an under-specified rule; L = wording or drift.

## Top gaps

1. **H - Daemon restart loses Waiting Time; no owner seeds engine state from the record (FR-8, FR-2, SM-3, UJ-3).**
   ADR-0005 rejects storing current state ("held in memory by the engine and rebuilt from a snapshot"). But FR-8 requires that after a
   Daemon restart "Waiting Times equal those before" and that a herdr restart records no Transition for an unchanged Status. Both
   need the engine to load each AgentKey's last recorded Status and Attention-Class entry time from `store` at start and diff the first
   snapshot against it. Neither AD-2 nor the Capability map says so. Also unowned: "idle with no `working` in the record" (FR-3) and
   "`done`->`idle` keeps Waiting Time" both need that history lookup, which AD-1 ("pure, takes state and a clock") does not provide.
2. **H - Lower-bound Waiting Time has no provenance rule (FR-2, UJ-3, FR-9a, Glossary).**
   AD-1 mentions "its lower-bound flag" but nothing says which evidence yields an exact figure and which yields `>=`. PRD: exact only
   when the Daemon observed the moment (`live`); a `snapshot`, `backfill`, or possibly-spurious entry, or Daemon start, is a lower
   bound. ADR-0005 says Waiting Time "can prefer" live over snapshot over backfill: a permission, not a rule. As written, a backfilled
   (and possibly spurious, pre-0.9.2) `done` could anchor an exact-looking Waiting Time, which is what UJ-3 forbids ("never a smaller
   number claimed as exact"). Needs an AD (or an AD-1 sentence): exact iff the defining Transition is `live`; otherwise lower bound;
   possibly-spurious never defines a Waiting Time start.
3. **M - FR-13 event content: SSE names `agent`/`removed`/`connection` only (AD-7, Consistency Conventions).**
   FR-13 says the stream pushes "each recorded Transition, each resync, and each connection change". There is no transition event
   and no resync event; a resync is not visible to clients. Either amend FR-13's reading (a changed `AgentView` is the Transition
   signal; resync rides on `connection`) or add events. Related: the timing NFR "within one second" (FR-4, FR-13, UJ-1) appears in no
   AD, ADR or test convention; neither does the SM-C2 bound that snapshot frequency is limited by the reconcile timer (ADR-0006 lets
   every rejected subscribe or unknown code trigger a resync, serialised but not rate-limited, so a flapping pane can storm snapshots).
4. **M - Connection-truth payload and strips (FR-5, FR-16, UJ-1 edge, addendum B "expect 0.8.2 to differ").**
   AD-7's `connection` event is "the connection truth", never defined. It must carry: connected or not, last snapshot time, and the
   version-gate state (unsupported, or advisory warning with "resync is not guaranteed"), because FR-5 and FR-16 require both strips
   and dimmed-not-hidden Boxes. FR-16's dogfooding config flag (default off) is absent from AD-8; the log message on refusal and
   "records nothing" below the minimum have no engine rule. Disconnect must surface "within the reconnect interval", yet ADR-0006
   says "backoff" with no cap; the strip must not wait for the next retry.
5. **M - Pane-id lifetime across herdr restarts (addendum E, FR-8, AD-3).**
   Addendum E: the server generation marker "matters to FR-8, since pane ids may not survive a herdr restart". AD-3 keys on
   (Host, Server, PaneID) with no generation or instance component, and the spine's Deferred section ties the generation marker only
   to the (optional) acknowledgement set. A herdr restart that reuses `p_1` for a different agent would splice two Agents' histories
   and Waiting Times. Needs a decision: generation in the key, or a resync marker that closes all open Agents.

## Everything else found

### FR consequences
- **FR-9a idempotence and nulls (M).** "Running the import twice adds nothing" has no dedupe key in ADR-0005. "Previous Status and
  herdr sequence recorded as unknown" needs those columns nullable; ADR-0005 lists "previous and new Status" unqualified.
  "Entries older than Retention are not imported" and the rule for flagging `done` "before the Host ran 0.9.2" (by log date? by
  version?) have no owner. AD-2/AD-9 force the Daemon to be stopped for import; PRD calls it a one-shot, not a downtime.
- **FR-10 re-resolution trigger (M).** "Re-resolving when the foreground process changes" has no event source (herdr offers no
  foreground-change subscription). `pane.process_info` (UNVERIFIED, addendum D) is not named in AD-5, ADR-0006 or the Deferred
  list; yet it is a herdr call, so by AD-5 it must live in `internal/herdr`.
- **FR-10 shipped defaults (L).** The Claude Code absent-case entry ("a fresh install never shows a stock pane as unresolved") and a
  config holding "only the Profile Map" (FR-15) imply built-in defaults for retention (90 days), bind, port, path. AD-8 only says
  "defaults". Heading dedupe (`codex`, not `codex . codex`) is a derived display value; AD-1 says the Board computes nothing, but
  AgentView is not stated to carry a ready heading.
- **FR-2 / FR-12 fields (L).** FR-12 requires Pane Title (raw), Context Line, workspace and tab labels, Host and server in the
  response; AgentView is never enumerated. Addendum C marks the snapshot roll-up of workspace and tab labels UNVERIFIED; the spine's
  Deferred list omits that check from milestone 1.
- **FR-3 / Board zones (M).** FR-12 requires the API "already ordered as the Board orders it". Orders are in AD-1, but the zone
  split (flat top zone vs. Working/Unknown grouped by workspace, workspace label, grouping never reordering the top zone, order of
  workspaces) is not stated as a core or API concern. Risk: the Board derives grouping itself, contradicting AD-1.
- **FR-2 UI rules (L, correctly deferred).** One-line Context Line with ellipsis, hover text, reserved empty space, `unresolved` token,
  dim-not-hide: all fall under "Board visual design deferred", but no UX hand-off item records them as binding.
- **FR-17 (L).** Port number is deferred to milestone 3 (ADR-0007) while FR-17 requires "distinct from 7317"; acceptable if the
  ADR amendment is a milestone-3 exit criterion. No Host-header check (DNS rebinding) beyond "no CORS"; not a PRD requirement.
- **FR-14 (L).** History includes resync markers? PRD says Transitions only; spine silent. History must contain the Transition that
  defines current Waiting Time, which the retention sweep may have removed for a long wait (>90 days); out-of-retention behaviour
  undefined.

### NFRs, restraint and counter-metrics
- **SM-C1 / ADR-0003 restraint (M).** The Board containing "only Boxes, workspace labels, connection and version strips", and "any new
  element needs an ADR superseding ADR-0003", is not an invariant in the spine. AD-1 even lets `AgentView` carry anything. Suggest
  adding the restraint to the Board row of the Capability map and a guard on AgentView growth.
- **SM-C2** is covered by AD-6 for page-view reads; see gap 3 for reconcile and resync rate.
- **Addendum A race (M).** Addendum A and AD-7 both say fetch `/api/state` first, then follow events. As written, events published
  between the fetch and the stream opening are lost. Safe order: open `/events`, buffer, fetch state, apply events with a higher
  sequence number. Also the sequence restarts at zero on Daemon restart, which only works if the client refetches state on every
  reconnect (AD-7 says so; keep it as a test).
- Retention default (90 days), SM-1..4 (operator judgement, no telemetry): nothing in the spine contradicts; no telemetry is
  introduced.

### Non-goals and never-rules (all landed)
Never focus and never read output are enforced by absence (AD-5, ADR-0006). No write routes, no authentication, no network exposure
(AD-6, ADR-0007). Not a plugin, no aggregation (AD-3, Deferred). Addendum G data minimisation: spine stores neither directory nor
Pane Title and logs Pane Title only at debug: consistent. Addendum B items missing from ADR-0006 (L): the two envelope conventions
(underscored lifecycle vs. dotted pane-scoped), "refresh again if events arrive mid-read", and stale-event replay on 0.8.2 (no
dedupe rule beyond the version flag).

### Drift against the PRD/addendum text
- **Go version:** spine pins `go` 1.26 and a 1.27.2 release toolchain; CLAUDE.md and addendum F say "Go 1.25 or newer". Raising the
  floor is allowed but should be stated and CLAUDE.md amended.
- **herdr version:** spine and ADR-0006 say 0.9.4 is latest and types are generated from a "0.9.2-or-newer" binary; addendum F says a
  real 0.9.3 binary. Pick one and amend the addendum.

## Spine vs. addendum: API path

| | Path |
|---|---|
| Addendum A (and PRD FR-14 wording "identified by its pane") | `GET /api/panes/{host}/{pane}/history` |
| Spine AD-3/AD-6 and ADR-0007 | `GET /api/agents/{host}/{server}/{pane}/history` |

**The spine and ADR-0007 are authoritative.** The addendum's path omits the herdr server, which the PRD glossary says namespaces every id
(a Host can run several named sessions), and "panes" contradicts the PRD's "Agent" vocabulary. Amend addendum §A (the data-flow
diagram) and §J item 3 (`/api/.../history` is fine as written). FR-14 needs no change in meaning. Open sub-issues: the encoding of
a Host name or session name containing `/` or spaces in a path segment (not specified in AD-6 or ADR-0007), and whether the
`AgentView` identity fields emit the path ready-made.

Also now stale in the addendum: §A's "fetches /api/state once, holds /events" (see race above); §F "Go 1.25+" and "0.9.3 binary";
§E's generation marker (decide per gap 5); §J milestone 1's list should include verifying `pane.process_info` (gap list, FR-10).
