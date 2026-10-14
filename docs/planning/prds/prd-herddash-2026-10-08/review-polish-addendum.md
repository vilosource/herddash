# Polish review: addendum.md

Finalize "Polish" step, `skill:bmad-review lenses=structure,prose`, run 2026-10-14 against
`addendum.md` only. `prd.md` was read for context: §3 Glossary, the FR set (FR-1 to FR-17, with
FR-2a and FR-9a), and §8 Open Questions 1 to 4.

**Purpose read.** This document exists to give architecture and story work the technical "how"
behind the PRD: protocol constraints, data flow, stack and rejected alternatives, condensed from
the frozen research records.

**Structure model.** Reference / technical addendum: lettered, independently readable sections
that are looked up rather than read through. The shape is sound; no section is out of scope and
none needs to move. C2 stays lettered as it is, by instruction.

**Reader.** Humans (default), calibrated for precise terms because architecture work consumes it.

**Baseline.** 2,034 words before this pass.

## Findings

| # | Pass | Original Text | Revised Text | Changes | Disposition |
|---|---|---|---|---|---|
| 1 | structure | §D, second paragraph (~200 words): resolution order, restore behaviour, Profile Map drift and record-at-write in one block | CONDENSE: split after "not what the operator launched." into its own paragraph on drift and recording | Two topics in one block; the split makes the FR-11 point findable. 0 words | Applied |
| 2 | structure | §D, "Its log is the backfill candidate" | "The plugin's log is the backfill candidate" | "Its" pointed back across a paragraph about the Profile Map; antecedent was ambiguous | Applied |
| 3 | structure | §B, "Version-gate on `ping`" bullet | Add "(FR-16)" | Missing cross-reference; every other bullet with a matching FR carries one | Applied |
| 4 | structure | §D, opening sentence on Profile resolution | Add "(FR-10)" | Resolution order restates FR-10; no pointer to it | Applied |
| 5 | structure | §B "No global agent-status subscription" consequences vs the next bullet "Subscribe before snapshotting"; §B "Reconcile on a timer" vs §A data flow; §B "Never read pane output" vs §G | PRESERVE | Related, not identical: each restatement serves a different reader entry point | Kept |
| 6 | structure | §J milestone 2 | Consider noting PRD open question 3 belongs here, as milestone 1 does for question 2 | PRD §8 places question 3 in milestone 2; the addendum does not say so | Deferred (adds a statement) |
| 7 | structure | Cross-references checked: FR-2a, FR-7, FR-8, FR-9a, FR-11, FR-15, FR-16, FR-17; open questions 1, 2, 4 | — | All resolve to the right PRD item under the 1 to 4 numbering | No change |
| 8 | prose | §C2 "a harness that sets no usable pane title" | "…no usable Pane Title" | Glossary term | Applied |
| 9 | prose | §G "directory and terminal title only; the terminal title is the one field" | "directory and Pane Title only; the Pane Title is the one field" | Glossary term. §C's `terminal_title` field name is code and stays | Applied |
| 10 | prose | §D "a default that returns the program name"; "no entries for the program at all, the program name" | "…the Agent Program name" (both) | Glossary term, matches FR-10 and FR-11 | Applied |
| 11 | prose | §G "The data directory is `0700` and database and log `0600`" | "…and the database and log are `0600`" | Gapped verb read as a list item | Applied |
| 12 | prose | §H "Entirely UNVERIFIED by direct test; from 0.9.3 documentation and source." | "All of this is UNVERIFIED by direct test; it comes from 0.9.3 documentation and source." | Fragment with no subject; UNVERIFIED marker kept verbatim | Applied |
| 13 | prose | §J "4. **The Board.** Consuming only milestone 3." | "Consumes only what milestone 3 serves." | Dangling participle; milestone 3 is a set of routes, not a thing consumed | Applied |
| 14 | prose | §D first paragraph, §D third and fourth paragraphs, §G, §H: lines up to 140 characters, others broken short | Rewrapped near 100 characters | Inconsistent wrapping from earlier edits | Applied |
| 15 | prose | §I "the Daemon's continuous subscription is the property that makes it better than the toasts" | Consider: name what "the toasts" are (the plugin's notifications?) | Term appears nowhere else in the addendum or the Glossary; defining it needs the author | Deferred (needs a definition the text does not give) |
| 16 | prose | §E table, "no Waiting emphasis" | Consider: "no Waiting Time emphasis"? | "Waiting" capitalised alone is not a Glossary term; which emphasis is meant is the author's call | Deferred (possible meaning change) |
| 17 | prose | Introduction, "this condenses them for the recorder" | Consider: "for herddash"? | "the recorder" is ADR-0001's word, not a Glossary term; replacing it could narrow or widen what is meant | Deferred (possible meaning change) |

Style notes preserved: plain declarative voice, British spellings (serialise, behaviour,
parameterised), imperative bullet heads in §B, UNVERIFIED and confirmed markers verbatim.

## Summary

- 17 findings: 11 applied (1 to 4, 8 to 14), 1 preserved, 1 check with no change, 4 deferred.
- Word change if applied as here: about +15 words (cross-references and the §H subject), under
  1% of 2,034. Polish was the aim, not reduction.
- No protocol constraint, provenance marker, version number, decision or rejected-alternative
  reason was changed. Section lettering, including C2, is unchanged.
