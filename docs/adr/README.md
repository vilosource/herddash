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
| [0003](0003-the-board-is-the-product.md) | The board is the product, and the recorder is what makes it trustworthy | Accepted; amends one consequence of 0001 |
| [0004](0004-runtime-dependencies.md) | Runtime dependencies: a pure-Go SQLite driver, a TOML parser and x/sys | Accepted |
| [0005](0005-append-only-transition-record.md) | The stored record is append-only, keyed by Host, Server, server epoch and pane | Accepted |
| [0006](0006-how-herddash-speaks-to-herdr.md) | How herddash speaks to herdr | Accepted |
| [0007](0007-read-only-loopback-http-surface.md) | A read-only HTTP surface on loopback | Accepted |
| [0008](0008-claude-code-session-record-as-status-authority.md) | Claude Code's session record as a second authority for Needs you | Accepted |
| [0009](0009-background-work-from-claude-code-transcripts.md) | Background work from Claude Code's session transcripts | Accepted |
| [0010](0010-claude-code-recap-on-the-board.md) | Claude Code's session recap on the Board | Accepted; amends one rule of 0009 |
| [0011](0011-tighter-import-rules-for-herdr.md) | Tighter import rules for herdr's wire types | Accepted |
| [0012](0012-an-observable-keepalive-on-the-event-stream.md) | An observable keepalive on the event stream | Accepted; amends 0007 |
| [0013](0013-a-title-bar-on-the-board.md) | A title bar on the Board | Accepted; amends 0003 |
| [0014](0014-acknowledging-a-finished-agent.md) | Acknowledging a Finished Agent ("Seen") | Accepted; amends 0003, 0005, 0007 |
