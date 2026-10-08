# Architecture decision records

Numbered, immutable records of the structural decisions this project has made. Nygard format:
**Context, Decision, Consequences, Alternatives Considered.**

An ADR is written *before* the code it governs. Open one for a new dependency, a change to how
herdr is spoken to, a change to the stored event schema, or a new network surface. Small fixes,
documentation corrections and behaviour-preserving refactors do not need one.

A record is never edited once accepted, except to change its status. To reverse a decision, write
a new ADR that supersedes it and say so in both.

| Status | Meaning |
|---|---|
| Proposed | written, not yet decided |
| Accepted | in force |
| Superseded by NNNN | replaced; the newer record says why |

| ADR | Title | Status |
|---|---|---|
| [0001](0001-what-herddash-is.md) | What herddash is, after evaluating the prior art | Accepted, option B |
| [0002](0002-adopt-bmad-method.md) | Adopt the BMAD Method, and where its records live | Accepted |
