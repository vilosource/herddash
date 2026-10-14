# PRD Quality Review — herddash (prd-herddash-2026-10-08)

Reviewed 2026-10-14 against `.claude/skills/bmad-prd/assets/prd-validation-checklist.md`, reading
`prd.md`, `addendum.md`, ADR-0001 and ADR-0003. Calibration: internal tool, one operator, target
5 to 8 pages, chain-top (feeds architecture, epics and stories).

## Overall verdict

The PRD has a real thesis and keeps to it. "A notification center for herdr agents", made
trustworthy by a recorder, follows ADR-0003 closely. The non-goals are sharp, the counter-metric
enforces restraint, and nearly every FR has testable consequences. The risk sits in the Board's
own display semantics, which is the part ADR-0003 says the product is measured by. The order of
the Boxes (global sort against workspace grouping), the Attention Class of an `idle` Agent, and
the meaning of Waiting Time when no Transition was observed are each either contradictory or
undefined. Stories written from the PRD as it stands would encode those contradictions. The PRD
also reopens two questions the governing ADRs settled, and defers a third that ADR-0001 promoted,
without saying so.

## Decision-readiness — adequate

Most decisions are stated as decisions, with what was given up: no pane output in v1 (§5), no
authentication and so no exposure (§5), an empty global-default Context Line "rather than, say,
the project directory again" (FR-2a), and Retention of 90 days (FR-9). The open questions in §8
are genuinely open, and most say when or how they get answered ("Decide after two weeks of
dogfooding", "milestone 1 proves it"). The one `[NOTE FOR PM]` (§6.2) marks a real future
pressure, not a safe checkpoint.

The weakness is how the PRD relates to its governing ADRs. It declares
`governed_by: [ADR-0001, ADR-0003]` and then quietly diverges from consequences both ADRs record.

### Findings
- **high** PRD reopens or demotes decisions the governing ADRs settled (§8 Q1, §8 Q3, §6.2
  "Backfill") — ADR-0001 says open question 5 (acknowledgement) "resolves by precedent", and the
  devswha generation-marker shape "works here". PRD §8 Q1 turns that back into "Decide after two
  weeks of dogfooding". ADR-0001 says the plugin "should be retired once it works", and ADR-0003
  repeats "the existing plugin is retired once the daemon works". PRD §8 Q3 asks "Retire it … or
  keep it as an independent push path? ADR-0001 leans retire". The ADR does not lean. It decides.
  ADR-0001 also made backfill "important rather than optional", and ADR-0003 says "backfill
  matters", yet §6.2 defers it past MVP and never acknowledges the ADRs. An epic writer gets two
  authoritative sources that disagree, and plugin retirement, which needs a story, is lost.
  *Fix:* close Q3 by citing the ADRs and add plugin retirement to §6.1 or milestone 5. For Q1 and
  backfill, either align with the ADRs or state plainly "this PRD departs from ADR-0001's
  consequence X because Y". Re-deciding ADR-0001's acknowledgement consequence may need its own
  amending ADR, which is how ADR-0003 handled the board.
- **medium** SM-C1 sets a weaker gate than ADR-0003 (§7 SM-C1) — the PRD says an added control
  or excerpt is "a product change requiring this PRD to be updated". ADR-0003 says such a request
  "needs a superseding ADR, not a story". *Fix:* quote ADR-0003's gate in SM-C1.

## Substance over theater — strong

There are no personas beyond the one operator, and the user journeys are concrete vignettes that
carry real values (`cldw · claude · ~/GitHub/herddash · Needs you · 14m`). The Vision could not be
swapped into another PRD: it names the specific failures of herdr's toasts ("suppressed for the
active tab, silent when no terminal client is attached, capped at eight, and gone in eight
seconds"). The NFRs are product-specific: no mutating routes, no on-demand herdr proxying, `0600`.
The Builder job in §2.1 ("publish it for other herdr operators") is the only near-furniture item,
and §6.2 defers it honestly.

### Findings
- **low** Measurement data inside the requirements (§4.1 ¶3) — "Measured on the operator's
  machine on 2026-10-13, Claude Code Pane Titles run 7 to 45 characters" repeats addendum §C2 and
  dates the PRD. *Fix:* keep it in the addendum only.

## Strategic coherence — strong

The thesis is explicit and the rest of the PRD follows from it: the Board is the product, and
the recorder exists so that "waiting 40 minutes" is a fact (§1). Feature order (§4.1 first) and
the primary metrics (SM-1 and SM-2 both measure the Board) match ADR-0003. Each success metric
names the FRs it validates. The counter-metrics are real: SM-C1 bounds clutter and SM-C2 bounds
herdr load. The MVP scope is the problem-solving kind, and the deferrals in §6.2 fit it.

### Findings
- **medium** The success metrics will be measured on a server that cannot exercise key FRs (§7
  SM-1 to SM-3, FR-16, §2.2) — dogfooding runs on 0.8.2 behind the advisory gate. `events_lost`
  is 0.9.2+ (addendum §B), so FR-7's main trigger never fires. 0.8.2 also predates the fix for
  spurious `done` (addendum §D), which pollutes Finished and Waiting Time, so SM-2 and SM-3 are
  measured against noise the target version does not have. *Fix:* state that the 30-day window
  starts once the Host runs 0.9.2 or newer, or say which SMs are valid on 0.8.2 and which are
  provisional.

## Done-ness clarity — thin

Taken one by one, the FRs are better than usual. Almost every one has two or more testable
consequences, and some are excellent ("never `codex · codex`", "Database file mode is `0600`",
"no Transition older than N days remains after the next sweep"). But three of the Board's
defining semantics are contradictory or undefined. These are the concepts every Board story will
cite, and ADR-0003 says the Board is what the product is measured by. The visual requirements are
adjectives in a PRD whose product is a visual surface.

### Findings
- **high** The order of the Boxes is contradictory (§4.1 ¶2, FR-3, FR-12) — FR-3 orders all Boxes
  globally: "Needs you, Finished, Working, Unknown, and within each class by Waiting Time
  descending". §4.1 ¶2 lays them out "grouped by workspace" except for "the page's top section,
  which is flat and Needs-you first". FR-3's consequences then refer to "the top sections"
  (plural). Nothing defines which classes form the top section, whether Finished is in it, or how
  Boxes are ordered inside a workspace group. FR-12 returns the list "already sorted as the Board
  sorts" but includes no workspace field to group by. *Fix:* define the layout exactly: which
  classes are flat at the top, how the order works inside each workspace group, and whether a Box
  appears once or twice. Then make FR-3, FR-12 and §9's confirmed assumption say the same thing.
- **high** The Attention Class of `idle` contradicts itself across three places (§3 "Attention
  Class", §8 Q1, addendum §E) — the glossary already decides that `idle` "reached from `working`
  or `done`" is Finished. Q1 asks whether that same `done`→`idle` change should "stop showing an
  Agent as Finished". Addendum §E adds a fifth case the PRD lacks ("`idle` with no prior `working`
  in the record … Working-adjacent calm, shown as Finished with no Waiting emphasis"). That case
  also depends on how much record exists, which differs on first start and after Retention. On
  top of this, Waiting Time is "time since the Transition that put an Agent into its current
  Status", so a `done`→`idle` change resets the clock while the Box stays Finished. Viewing a
  pane in the terminal would then silently drop it down the sort. *Fix:* put a single,
  authoritative Status-to-Attention-Class table in the PRD covering every case in addendum §E,
  and state whether `done`→`idle` resets Waiting Time. Make Q1 ask only what is genuinely still
  open.
- **high** Waiting Time is undefined when no Transition was observed (§3 "Waiting Time", FR-2,
  FR-7, UJ-3) — FR-2 says Waiting Time is "not from page load or Daemon start", and UJ-3 promises
  "not from when … herdr was restarted". On the Daemon's first run, after an `events_lost`
  resync, or when the Daemon starts while herdr is down (FR-7's second consequence), the only
  Transition is snapshot-derived. Its time is the Daemon's observation time, not when the Status
  actually began, and addendum §C lists no timestamp in the snapshot. The PRD never says what the
  Box shows then. A precise-looking "2h 10m" that is really "since Daemon start" is exactly the
  distrust SM-3 counts. *Fix:* add an FR-2 consequence saying how a snapshot-derived Waiting Time
  appears (for example "≥ 12m", or a marker), and narrow UJ-3's promise to Transitions observed
  live.
- **medium** The visual requirements are adjectives (§4.1 ¶1 "readable from across a desk",
  "visually dominant", "clearly marked", "calm"; FR-3 "Needs you is visually dominant") — UJ-1
  implies red for Needs you and amber for Finished, but no FR binds them, and no size or distance
  is set. *Fix:* bound them, for example a minimum text size or reading distance, one colour per
  Attention Class, and Needs you as the only saturated colour. Alternatively state that a UX
  document will set them and name it as an input to the Board epic.
- **medium** Detection time is unbounded (FR-5, FR-8) — FR-5 says the strip appears "within the
  Daemon's reconnect interval", and FR-8 reconnects "forever, with backoff" with no ceiling. Both
  the strip appearing and the recovery after herdr returns can therefore take arbitrarily long.
  *Fix:* set a maximum backoff and a bound on the strip, for example "strip within 5 s of loss;
  refreshed within 30 s of herdr's return".
- **medium** Agent identity across a herdr restart is unspecified (§3 "Agent", FR-1, FR-8) — an
  Agent is "a herdr pane", keyed by Host and pane id. FR-8 then expects "no Transition is recorded
  for an Agent whose Status is the same before and after" a herdr restart. That only holds if pane
  ids survive the restart or handoff, which nothing in the PRD or addendum establishes. If they do
  not, every restart orphans the history and resets every Waiting Time. *Fix:* state the identity
  key (pane id, `terminal_id`, or `agent_session`) and its survival as an assumption for
  milestone 1 to verify, alongside Q4.
- **low** FR-1 does not say what happens when an Agent exits but its pane stays open (for
  example, it drops back to a shell). *Fix:* add a consequence.
- **low** Retention can delete the Transition that anchors a Box (FR-9, FR-14) — an Agent idle
  for longer than 90 days loses the Transition its Waiting Time depends on, which contradicts
  FR-14's "contains the Transition that defines its current Waiting Time". *Fix:* exempt each
  Agent's latest Transition from the sweep.
- **low** FR-16 does not say whether Boxes still render below the minimum version, only that a
  strip shows and "records nothing". *Fix:* say whether the Board is empty or shows a live
  snapshot without recording.

## Scope honesty — adequate

§5 is a model of non-goals that do real work, each with a reason, and "Never calls focus" is
correctly framed as "a correctness rule, not a scope choice". The `[NON-GOAL for MVP]` on
per-Agent history and the §6.2 list are explicit. §9 records the confirmed assumptions, so no
`[ASSUMPTION]` tags remain open. The open-items density (6 questions and 1 note) is appropriate
for a draft one operator will build from. Two omissions could still be assumed silently.

### Findings
- **medium** A "notification center" that never notifies (§1, §5) — the Vision promises a
  notification center, and UJ-1 has the operator "heads-down in one pane for twenty minutes".
  Nothing says whether the Board ever pulls attention: a browser notification, a sound, or a
  count in the tab title or favicon. A story writer could reasonably add one, and it is unclear
  whether that would breach SM-C1. *Fix:* add an explicit non-goal ("the Board never alerts; it is
  glanced at") or a scoped FR (for example, a Needs-you count in the document title).
- **low** Phantom authentication (FR-17, §5) — FR-17 refuses "a non-loopback bind address without
  authentication configured", but v1 has no way to configure authentication. *Fix:* say "any
  non-loopback bind address is refused at startup in v1".

## Downstream usability — adequate

There is a glossary, it is used consistently in capitalised form, and §0 commits the PRD to it.
FR, UJ and SM IDs resolve, and every FR names the UJ it realises. The addendum is cleanly
separated as the "how", and its cross-references (FR-7, FR-11, FR-17, open questions 1, 2 and 4)
resolve. The gaps are in the contracts that architecture will lift directly.

### Findings
- **medium** FR-12's field list cannot drive the Board or FR-14 (FR-12) — it lists Profile, Agent
  Program, project directory, Status, Attention Class, Waiting Time, Pane Title, Context Line and
  Host. It omits the workspace (needed for §4.1's grouping), the pane id (needed to call FR-14's
  `/api/panes/{host}/{pane}/history` and to match FR-13 events to a Box), and the version-gate
  state FR-16 must show. *Fix:* add workspace label, the Host-qualified pane id and
  version-gate status to FR-12.
- **medium** The recorded fields disagree (FR-6, addendum §G) — FR-6 records Host, pane id, Agent
  Program, Profile, the two Statuses, ordering information and time. Addendum §G says "the
  recorded data is Status, Profile, directory and terminal title". CLAUDE.md requires an ADR to
  change the stored event schema, so this needs settling before architecture freezes it. *Fix:*
  make FR-6 the single list and correct §G, including whether the directory and the Pane Title are
  persisted at all, which matters for the prompt-text concern in §G.
- **low** The re-resolution trigger is unspecified (FR-10) — "re-resolving when the pane's
  foreground process changes", but no herdr event is named that signals this, and addendum §D
  repeats the phrase without a mechanism. *Fix:* name the signal, or say "re-resolved on every
  Transition", which FR-6's "Profile at the time" implies anyway.

## Shape fit — strong

The shape suits a single-operator internal tool. It is a capability spec with four short
vignette user journeys, and three of those carry acceptance-relevant detail rather than ceremony.
UJ-4 is explicitly "to shape the API, not to be built now". The success metrics are operational
self-report, justified in a sentence ("a tool for one person does not need telemetry"). The
addendum keeps protocol detail out of the requirements.

### Findings
- **low** Slightly over length — `prd.md` is about 4,500 words, roughly 9 to 10 pages against a
  5 to 8 page target. Trimming §4.1 ¶3 (measurement data), the protocol detail in §8 Q4 and Q5,
  and the duplicated rationale in FR-10 and FR-11 (already in addendum §D) would bring it within
  range.

## Mechanical notes

- **IDs:** FR-1 to FR-17 are contiguous, with an inserted FR-2a. Downstream tooling that expects
  integer IDs may stumble, so consider renumbering before epics. UJ-1 to UJ-4, SM-1 to SM-4 and
  SM-C1 to SM-C2 are contiguous and unique.
- **Cross-references:** §4.1 to §6.2, §6.2 to §8, and §0 to §9 resolve. ADR-0003's references to
  PRD §4.1, §7, SM-1, SM-2, SM-C1 and addendum §J resolve. Addendum §G cites "open question 6" for
  the prompt-text risk in terminal titles, but PRD Q6 is about other harnesses' title behaviour,
  not privacy. It is related but not the same question.
- **Glossary drift:** the glossary defines the Profile Map as "a configured mapping from process
  environment values to Profile names", but FR-11 says it "selects a rule per Agent Program and
  supplies that rule's parameters". These are different shapes. "Profile rule", "connection
  strip", "resync marker", "snapshot", "workspace" and "top section" are used as terms of art but
  are not in §3. Waiting Time is applied to Working Agents, where "waiting" reads oddly. Consider
  saying so explicitly in the glossary.
- **Assumptions roundtrip:** no inline `[ASSUMPTION]` tags remain. All six §9 entries map to text
  in §4 that says the same thing. The §4.1 entry inherits the grouping ambiguity flagged above.
- **Required sections:** all present for the stakes. No section is missing.
