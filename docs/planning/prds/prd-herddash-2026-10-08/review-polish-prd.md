---
title: Polish review of prd.md (structure, prose)
reviewed: docs/planning/prds/prd-herddash-2026-10-08/prd.md
date: 2026-10-14
lenses: [structure, prose]
---

# Polish review: prd.md

Run as the BMAD PRD finalize "Polish" step (`skill:bmad-review lenses=structure,prose`).
Structure lens ran first; prose lens ran on top of its findings. Style guide: Microsoft Writing
Style Guide, overridden where it conflicts with the document's own voice (plain, declarative,
British spellings kept). Reader: humans.

**Read:** this document exists to help the operator, and the architecture, epic and story work
derived from it, know exactly what herddash v1 does and does not do.

**Structure model:** requirements specification (definitions first, then features with testable
requirements, scope, metrics, open questions). The document already follows it; findings are local.

**Word count before:** 4,985. Net change is small (a duplicate sentence removed, a few
cross-references added); no length target was set.

Rules applied: meaning unchanged; no FR, consequence, number, name, decision or scope statement
added, removed or re-decided; FR and open-question numbering untouched (FR-2a, FR-9a stable).

## Findings

| # | Pass | Original Text | Revised Text | Changes | Disposition |
|---|------|---------------|--------------|---------|-------------|
| S1 | structure | §3 Glossary: **Host** defined near the end, used from the first entry on | MOVE Host directly after herdr | Dependency-first: define before use | Applied |
| S2 | structure | §3 Glossary: **Board** defined after Box, Context Line, Attention Class, which all refer to it | MOVE Board directly before Box | Dependency-first | Applied |
| S3 | structure | §4.1 "The Context Line is derived by the Daemon, per Agent Program, with one global default." | CONDENSE to "The Context Line's derivation is FR-2a." | Duplicated §3 and FR-2a verbatim in substance; pointer keeps the paragraph's measured facts in context | Applied |
| S4 | structure | `### 4.4 Read-only API and event stream`, `### 4.5 Running it` | `Read-Only API and Event Stream`, `Running It` | Feature headings 4.1 to 4.3 and section headings are title case | Applied |
| S5 | structure | §6.2 Acknowledgement "See §8." | "See §8, question 1." | Cross-reference points at the specific open question | Applied |
| S6 | structure | §6.1 "with the dogfooding gate for 0.8.2" | append "(FR-16)" | Every other in-scope line cites its FRs | Applied |
| S7 | structure | §4.1 two-zone layout paragraph, restated in FR-3 and the §3 Board entry | PRESERVE | Description gives the design and rationale, FR-3 the testable form, glossary the definition; reinforcement, not redundancy | Kept |
| S8 | structure | §9 "Boxes are a responsive card grid grouped by workspace, with a flat Needs-you-first top section." | "Boxes are a responsive card grid: a flat Needs-you-first top zone above the Boxes grouped by workspace." | Summary read as if all Boxes were grouped by workspace; aligned with §4.1, which it records | Applied |
| P1 | prose | §1 "one Box per agent", "which agents", "longest-waiting agent", "every status transition with the Profile that owns the agent", "bounded retention", "which profile" | Agent, Agents, Transition, "with the Agent's Profile", Retention, Profile | §0 says §3 vocabulary is used exactly everywhere | Applied |
| P2 | prose | §2.1 "which agents", "other agent programs", "each running agent", "several profiles" | Agents, Agent Programs, Agent, Profiles | Glossary terms | Applied |
| P3 | prose | UJ-4 "asks the daemon" | "asks the Daemon" | Glossary term | Applied |
| P4 | prose | §3 Board "a flat top section"; §9 "top section" | "top zone" | §4.1 and FR-3 say "top zone"; one term for one concept | Applied |
| P5 | prose | §3 Waiting Time "taken from the earliest evidence, a backfilled or recorded Transition or else Daemon start, and is shown with `≥`" | "taken from the earliest evidence (a backfilled or recorded Transition, else Daemon start) and is shown with `≥`" | Appositive read as a list of three; parentheses make it one aside | Applied |
| P6 | prose | §3 Daemon "and nothing because it is merely available" | "and nothing merely because it is available" | Misplaced modifier | Applied |
| P7 | prose | FR-4 "Closing and reopening the Daemon" | "Stopping and restarting the Daemon" | A process is stopped, not closed; matches FR-8's wording | Applied |
| P8 | prose | FR-7 "results in connection on herdr's return and a snapshot-derived Status for every Agent, with each marked" | "results in the Daemon connecting when herdr returns and a snapshot-derived Status for every Agent, each marked" | Nominalisation removed | Applied |
| P9 | prose | FR-9a "record of agent activity" | "record of Agent activity" | Glossary term | Applied |
| P10 | prose | FR-10 "A program with no rule resolves to its program name"; FR-11 "their program name"; §8 Q4 "an Agent of each program" | "Agent Program" in each | Glossary term; FR-10's own consequences already say "Agent Program name" | Applied |
| P11 | prose | FR-15 "A fresh host" | "A fresh Host" | Glossary term | Applied |
| P12 | prose | Lines of 103 to 144 characters in §1, §4.1, FR-2, FR-17, §5 | Rewrapped near 100 | File convention | Applied |
| D1 | prose | §3 Context Line "beneath the status" | Consider: "beneath the Attention Class"? | The Box body shows Attention Class, not Status (§3 Box, FR-2), and lowercase "status" is neither term. Choosing which label the line sits under is a layout decision | Deferred |
| D2 | prose | FR-3 "with no Waiting emphasis" | Consider naming the emphasis meant (Needs you or Finished emphasis, or top-zone placement)? | "Waiting emphasis" is not a defined term; which emphasis is withheld is the author's call | Deferred |
| D3 | prose | "harness" in FR-2a, §4.3, FR-11, §8 Q3 and Q4 | Consider: add "harness" to §3 as a synonym of Agent Program, or replace it? | Unglossaried near-synonym of Agent Program; either fix adds or changes vocabulary | Deferred |
| D4 | structure | §8 Q2 "milestone 1", Q3 "milestone 2" | Consider defining milestones or citing where they are defined? | Milestones are not defined anywhere in the PRD; a definition is new content | Deferred |
| D5 | prose | FR-14 heading "History per Agent" vs body "A request for one pane" | Consider one word for the key? | Whether the history is keyed by pane id or by Agent is a design detail for the author | Deferred |
| D6 | prose | FR-2 "Waiting Time is derived from the recorded Transition, not from page load or Daemon start" vs the next consequence and §3, which allow Daemon start as lower-bound evidence | Consider: "…not from page load, and not from Daemon start when the Transition was observed"? | Reads as a contradiction; rewording a testable consequence is a meaning change | Deferred |

## Summary

- Recommendations: 26 (8 structure, 18 prose rows including deferrals).
- Applied: 19. Kept by design: 1 (S7). Deferred: 6 (D1 to D6), each needing an author decision
  that would change or add meaning.
- Estimated reduction: about 15 words (S3); the rest is substitution and rewrapping.
- Comprehension trade-offs: none. No reinforcing summary or example was cut.
