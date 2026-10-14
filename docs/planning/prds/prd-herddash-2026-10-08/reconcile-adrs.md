# Reconciliation extract: ADR-0001 and ADR-0003 against the PRD rework

Inputs: `docs/adr/0001-what-herddash-is.md` (accepted, option B), `docs/adr/0003-the-board-is-the-product.md`
(amends only ADR-0001's board consequence). Target: `prd.md` (updated 2026-10-14) and `addendum.md` in this
directory. Section numbers below are the PRD's unless marked "Add." (addendum).

Severity: **High** = PRD re-decides or contradicts a settled decision. **Medium** = decision honoured in
spirit but weakened, ambiguous, or internally inconsistent. **Low** = omission or wording drift.

## Summary table

| # | Topic | Status | Severity |
|---|---|---|---|
| 1 | Plugin retirement | Re-opened as open question | High |
| 2 | Acknowledgement precedent | Re-opened as open question and deferred | High |
| 3 | Backfill importance | Contradicted (deferred, "ignore" offered) | High |
| 4 | Restraint (SM-C1, escalation path) | Weakened, inconsistent with PRD's own Board | Medium |
| 5 | Plugin-hooks rejection rationale | Different reason recorded than the ADR's | Medium |
| 6 | No-AGPL-code | Only "forking" rejected; no constraint on taking code | Medium |
| 7 | Runs beside a full browser client | Stated only as a non-goal, no requirement | Low |
| 8 | Loopback-only and no auth | Honoured; one internal wobble in FR-17 | Low |
| 9 | Rejected alternatives (A, C, multi-host) | Partly carried, option A/C and upstream note missing | Low |
| 10 | Open question numbering | ADR numbers do not map to PRD section 8 | Low |
| 11 | Board is the product; bounded retention; read-only API; per-account attribution | Honoured | none |

## 1. Plugin retirement (High)

- ADR-0001 Consequences: "Open question 4 resolves: that plugin is superseded by this daemon and should
  be retired once it works, because they would record the same events twice."
- ADR-0003 Consequences: "ADR-0001's other consequences stand: backfill matters, the existing plugin is
  retired once the daemon works..."
- PRD §8 Open Question 3: "Retire it when the Daemon works, so there is one recorder, or keep it as an
  independent push path? ADR-0001 leans retire."
- Problem: the ADR says resolved; the PRD says "leans" and asks the question again, offering keep-as-push-path
  as a live option. That silently reopens a settled decision. It is also a double-recording hazard if both run.
- Also omitted: no FR, scope item (§6.1/§6.2) or milestone (Add. J) carries retirement. Add. D calls the plugin
  "the reference implementation" and the "backfill candidate" but never says it is retired.
- Landing spot: remove from §8; add to §6.1 or §6.2 a line "the existing plugin is retired once the Daemon is
  trusted on that Host"; add to Add. J milestone 5 (or after milestone 2 for the recording overlap); note in §9
  that this is ADR-settled. Retaining the plugin's push channel as a *separate* concern would need its own
  decision, not an open question.

## 2. Acknowledgement precedent (High)

- ADR-0001 Consequences: "Open question 5 resolves by precedent. devswha keeps a finished-pane set beside a
  server generation marker; the same shape works here."
- PRD §8 Open Question 1: "...herddash can copy that shape if needed. Decide after two weeks of dogfooding."
- PRD §6.2: "Acknowledgement. ... Deferred; the Attention Class model relies on herdr's own Transitions instead."
- Add. E: "Open question 1 decides whether herddash needs its own acknowledgement set, copying devswha's
  `completions.json` shape".
- Problem: the ADR settled the mechanism (finished-pane set plus server generation marker). The PRD
  converts "resolves" into "if needed" and makes the existence of the mechanism contingent on a later
  evaluation, while simultaneously making the Board's Finished class depend on herdr's `done`/`idle`,
  which the PRD itself calls untrustworthy (Add. E). The residual genuine open question is only *when*
  (MVP or later) and the exact UX trigger, since the ADR is silent on both and ADR-0003 says the operator
  does not interact with the Board.
- Landing spot: restate §8 Q1 as "mechanism decided (ADR-0001); remaining: whether it is MVP and what
  clears a Finished Box, given the Board takes no input"; reflect in §6.2 and Add. E; if it is in MVP it
  needs an FR and a schema decision (stored state needs an ADR per CLAUDE.md "stored event schema").
  Note tension to surface: acknowledgement implies an operator input, which sits against ADR-0003's
  "read-only... glance at it and does not interact with it".

## 3. Backfill importance (High)

- ADR-0001 Consequences: "Open question 3, whether to backfill from the existing plugin's weeks of logged
  transitions, becomes important rather than optional. That log is the only historical data that exists."
- ADR-0003: "backfill matters".
- PRD §6.2: "Backfill from the existing plugin's log. Deferred to a one-shot import command after the
  recorder is trusted."
- PRD §8 Open Question 2: "Import, import with a flag marking them as backfilled, or ignore?"
- Add. J: no milestone carries backfill.
- Problem: the PRD treats backfill as optional-and-deferrable and offers "ignore" as a candidate, which the
  ADR ruled out as an equal option. Deferral is not itself forbidden, but "important" implies a requirement,
  not an out-of-scope bullet with an ignore branch. The spurious-`done` concern (pre-0.9.2) is a legitimate
  *how* question and can stay open.
- Landing spot: add a FR in §4.2 (import from the plugin log, marked as backfilled, distinguishable like
  FR-6's live vs inferred), put it in Add. J (after milestone 2, before the Board is trusted), narrow §8 Q2
  to "import all vs filter pre-0.9.2 `done`", and drop "ignore". Also add the provenance value to the
  stored schema decision (ADR needed for schema change). Check that retention interacts sensibly: weeks of
  history is inside the 90-day default, so no conflict.

## 4. Restraint (Medium)

- ADR-0003 Decision: "no terminal, no chat, no prompt input and no pane control, and it never will within
  this product." Consequences: "A request to add pane control, output streaming or prompt input to the
  board is a request for a different product and needs a superseding ADR, not a story." And: "SM-C1 holds the
  board to boxes and the connection strip."
- ADR-0001: "it must stay deliberately minimal or it drifts back into duplication."
- PRD §7 SM-C1: "Any proposal to add a control, an excerpt, or a second data type per Box is a product change
  requiring this PRD to be updated." Weaker escalation: a PRD edit, not a superseding ADR.
- PRD §4.1 / §9: Boxes "grouped by workspace, with the workspace label as a thin header" and FR-16's
  unsupported-version strip. SM-C1 says the Board "carries only Boxes and the connection strip". Both a
  workspace header and the version strip are other Board elements, so the PRD's own Board fails the PRD's own
  counter-metric as written. Also ADR-0003's Box is "status, the profile... and how long it has been waiting";
  the PRD Box adds project directory and a Context Line, so "a second data type per Box" is already breached
  or at least undefined.
- Landing spot: §7 SM-C1 (name the superseding-ADR route for control/output/input; define the permitted
  element set explicitly: Boxes, workspace headers, connection and version strips; define the Box data types),
  §5 Non-Goals (state "never within this product" for pane control, output and prompt input), §4.1.
  Fine as-is: §4.1 "Out of Scope for the Board" and §5 do forbid actions, output, focus.

## 5. Plugin-hooks rejection rationale (Medium)

- ADR-0001 Alternatives: "Rejected on capability, not taste. Output-related events are deliberately excluded
  from plugin hooks, and a hook naming one gets a warning and silently never fires."
- Add. I: "Hooks fire a fresh process per event, and the Daemon must hold subscriptions continuously... Also,
  the machine's plugin registry is Ansible-managed and already occupied."
- Problem: the addendum records different reasons (process-per-event model, registry occupancy) and drops
  the ADR's capability reason entirely. That is a re-decision of rationale, and it is weak given the PRD never
  reads output in v1, which makes the ADR's reason look moot and the addendum's look like the whole case.
- Landing spot: Add. I; cite the ADR's reason and mark the additional reasons as added by the PRD.

## 6. No-AGPL-code (Medium)

- ADR-0001 Context: "alecuba16's is AGPL-3.0-or-later with a licence-provenance question noted in its
  record, so no code can be taken from it into an MIT project and contributing to it needs that question
  resolved first." Rejected: "Fork alecuba16's project. Rejected on licence. AGPL would relicense this work".
- PRD: Add. I "Forking alecuba16/herdr-webui. AGPL-3.0-or-later with an unresolved provenance question."
  Nothing in §5 or Add. states the broader rule (no code taken, only ideas), nor that herddash is MIT, nor
  that devswha (MIT) is the only permitted source of copied shape (Q1 copies devswha's `completions.json`
  shape, which is fine but is the sole precedent source worth naming).
- Landing spot: §5 Non-Goals or a constraints line; Add. I. Low effort.

## 7. Runs beside a full browser client (Low)

- ADR-0001 Decision: "It runs beside whichever browser interface the operator prefers and does not replace
  one." ADR-0003: "herddash still runs beside a full browser client rather than replacing one."
- PRD §2.1: "keep working in herdr's own interface"; §5: "devswha/herdr-web-ui exists for that and is MIT";
  §1: "beside the terminal". Coexistence is a stance, not a requirement. Unaddressed: coexisting with devswha's
  own herdr socket connection and herdr's one-request-per-connection load (SM-C2 partly covers), default
  port non-collision, no dependency on devswha being installed.
- Landing spot: §2.2 / §5, optionally an NFR under §4.5.

## 8. Loopback-only, no auth (Low)

- ADR-0001: "Authentication stays deferred and loopback-only." ADR-0003: "authentication stays deferred
  with loopback-only binding."
- PRD §5 and FR-17 honour it. Wobble: FR-17 "Setting a non-loopback bind address without authentication
  configured is refused" implies an authentication option exists, while Add. G says "in v1 there is no
  authentication, so there is no non-loopback mode". Say plainly: non-loopback bind is refused outright
  in v1. Landing spot: FR-17.

## 9. Rejected alternatives (Low)

- ADR-0001 rejected: fork alecuba16, unofficial Go clients, plugin hooks, herdr multi-host federation; and
  options A (upstream contribution) and C (full product).
- PRD carries the first three (Add. I) and multi-host as reasoning (Add. H, §5) but not as a rejected
  alternative, and omits A and C. ADR-0001 also says option B's "data would be exactly what option A needs
  later, so this does not foreclose upstreaming", a design property the API/schema should preserve and the
  PRD does not mention. ADR-0003's three rejected alternatives are reflected only implicitly (SM-C1, §5).
- Landing spot: Add. I (add A, C, multi-host, and ADR-0003's "board as full client"), §6.2 or Add. for
  the upstream-not-foreclosed note.

## 10. Open question numbering (Low)

- ADR-0001 refers to "open questions 3, 4 and 5" of the old PRD; CLAUDE.md says 1, 4 and 5 were resolved
  and 3 made important. PRD §8 renumbered: Q1 acknowledgement, Q2 backfill, Q3 plugin. The mapping is
  undocumented and the numbers invert. The old `docs/prd.md` was not re-read for this extract, so the old
  numbering is as the ADRs cite it.
- Landing spot: a one-line mapping in §8 or §9.

## 11. Honoured, no action

- **Board is the product** (ADR-0003): §1, §4.1 lead, SM-1/SM-2 measure the Board, Add. J builds the Board
  fourth, `governed_by` lists both ADRs. ADR-0001's "secondary surface" consequence is not repeated.
- **Recorder makes the board trustworthy** (ADR-0003): §1 second half, FR-6 to FR-9, FR-8 Waiting Time
  survives restart.
- **Bounded retention** (ADR-0001 Decision): FR-9, default 90 days, §9 records confirmation.
- **Per-account attribution** (ADR-0001): §4.3, FR-10, FR-11, SM-4. The PRD says "Profile" and notes it
  "usually correspond[s] to subscriptions"; the ADR says "which subscription a pane belongs to". A
  deliberate, reasonable softening, but the term differs.
- **Read-only API and event stream**: §4.4, FR-12 to FR-14, "no POST, PUT or DELETE routes".
- **Milestone 1 stays as written** (ADR-0001): Add. J milestone 1 holds all protocol risk.
- **No pane control, output, focus**: §4.1 out of scope, §5.
