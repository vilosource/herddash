# Review: docs/how-herddash-works.md (structure, prose)

Date: 2026-10-15. Lenses: structure, then prose (bmad-review). Reader: humans. Style guide: Microsoft
Writing Style Guide. Facts cross-checked against `docs/prd.md` and `ARCHITECTURE-SPINE.md` only.

**Read:** this document exists to help herdr users decide whether to run herddash, by showing what
it does, how it works, what it never does, and its limits.

**Structure model:** explainer (overview, then mechanism, then guarantees, then reference and
limits). The document already follows it. Starting length: 992 words.

## Findings

| Pass | Original Text | Revised Text | Changes | Disposition |
| --- | --- | --- | --- | --- |
| structure | §What it is for, last sentence of para 2 ("survives restarts") and §Why the waiting time can be trusted | PRESERVE | The repeat is an overview-then-detail reinforcement, not redundancy | Kept |
| structure | §Where things live: the platform and requirements sentence | Rename the section "Where things live, and what it needs" | The sentence is about requirements, which the old heading hid (0 words) | Applied |
| structure | §For contributors | PRESERVE | Short pointer for a secondary reader; correctly last | Kept |
| structure | Both mermaid diagrams | PRESERVE | Visual anchors for the two mechanism sections | Kept, unchanged |
| prose | "sorted so the agent that has been waiting on you longest is at the top" | "Agents blocked on a question come first, then agents that have finished, each with the longest-waiting at the top." | **Factual correction**: PRD FR-3 orders Needs you before Finished, each by Waiting Time; a long-finished agent does not outrank a blocked one | Applied |
| prose | "Everything else asks it or tells it, so there is exactly one version of the truth." | "Everything else goes through it, so no two parts can disagree about an agent's state." | Replaces a stock phrase with the concrete consequence | Applied |
| prose | "One binary each is built for macOS and Linux, on Apple silicon, Intel and ARM." | "It ships as a single binary for macOS (Apple silicon or Intel) and Linux (Intel/AMD or ARM)." | The original read as Linux on Apple silicon; now matches the spine's four targets | Applied |
| prose | "A view across several machines would read each daemon's API, and every record already carries its machine's name so that can be added later." | "A view across several machines can be added later: it would read each daemon's API, and every record already carries its machine's name." | Unclear antecedent of "that"; point first | Applied |
| prose | "**Your browser only.**" | "**A browser on the same machine only.**" | "Your browser" did not say what the limit is | Applied |
| prose | "rejects requests that do not name localhost" | Consider: "rejects requests addressed to any name other than localhost"? | Spine AD-6 accepts any loopback name; current wording is close enough for this reader | Deferred |
| prose | "through DNS tricks" | Consider naming DNS rebinding? | Would add jargon the audience brief rules out | Deferred |

## Summary

- Applied: 6 (1 structure, 5 prose). Preserved: 3. Deferred: 2.
- Length change: about +10 words; no cuts were warranted in a document already this short.
- Unchanged: every "never" guarantee, the lower-bound waiting-time explanation, paths, herdr 0.9.5,
  90 days, both mermaid diagrams. No em dashes present.

## Fact check

Contradictions with the PRD or spine: one, the sort order (fixed above). Everything else checked
consistent: notification limits (PRD §1), event stream starts with whole state and reconnects
(AD-7), no herdr call per page load (AD-6), lower bound and backfill (PRD §3, AD-12), focus and
output-read absence (AD-5), loopback bind refused otherwise (FR-17), Host header check (AD-6), paths
and owner-only files (AD-8, AD-9), 90-day Retention (FR-9), build targets (spine Deployment), herdr
0.9.5 (spine Stack), Claude Code-only Profiles (FR-10), Host on every record (AD-3).
