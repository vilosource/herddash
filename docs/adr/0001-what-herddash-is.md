# ADR-0001: What herddash is, after evaluating the prior art

**Status: Accepted, option B.** Written 2026-10-05, decided 2026-10-08. This record existed to be
accepted or overturned deliberately, not inherited by default, because every option below implies
a different build.

## Context

herddash was started to answer one question: what is every AI coding agent doing right now, and
which ones are waiting on me? The motivation was concrete and is recorded in
[herdr-on-this-machine.md](../research/herdr-on-this-machine.md). herdr's own notification channel
suppresses the active tab, needs a foreground attached client so a detached session is silent,
queues at most eight alerts, and fires on only two transitions.

The first PRD claimed five differentiators over existing work. Two browser interfaces for herdr
were then installed and run rather than judged from their READMEs. The records are
[prior-art-herdr-web-ui.md](../research/prior-art-herdr-web-ui.md) and
[prior-art-alecuba16-herdr-webui.md](../research/prior-art-alecuba16-herdr-webui.md).

**Three of the five differentiators did not survive.**

| Claimed | devswha/herdr-web-ui | alecuba16/herdr-webui | Net |
|---|---|---|---|
| A daemon that works while detached | neutralised | neutralised | **gone** |
| Triage rather than remote control | largely neutralised | holds | **weak** |
| Persisted history | holds | holds | **holds** |
| Per-account attribution | holds narrowly | holds | **holds** |
| One static binary per host | holds | neutralised | **gone** |

The one that hurts most was the primary thesis. devswha's project holds its own herdr socket and
subscribes to the same events herddash would, so it needs no attached terminal client either. It
also ships a "Needs you" grouping, which takes most of the triage framing. alecuba16's project
ships a single Rust binary with checksummed tarballs and service installers, which takes the
deployment argument.

What genuinely remains:

1. **A persisted history of what agents did.** Neither project has a database or keeps any
   status-transition history. Both answer only "what is happening now".
2. **Attributing a pane to the account that owns it.** Several launchers exec the same `claude`
   binary, so herdr reports every pane as `claude`. Neither project resolves which subscription a
   pane belongs to. This matters only to an operator running one agent program under several
   accounts, which is exactly this operator.

That is a feature pair, not a product.

Two further constraints bear on the choice. devswha's project is **MIT**, so contributing to it is
possible. alecuba16's is **AGPL-3.0-or-later** with a licence-provenance question noted in its
record, so no code can be taken from it into an MIT project and contributing to it needs that
question resolved first.

## Decision

**Option B, the narrow complement, accepted 2026-10-08.** herddash is a headless recorder: it
subscribes to herdr, persists every agent status transition with per-account attribution under a
bounded retention policy, and exposes a deliberately minimal page of its own plus a read-only API.
It runs beside whichever browser interface the operator prefers and does not replace one. The
options are kept below as written, since the choice was made against them.

## Options

### A. Contribute the gap upstream to devswha/herdr-web-ui

Add status history and per-account attribution to the project that already does everything else
well, and stop here.

- **For:** no duplicated work, an existing user base, MIT so it is legally clean, and the operator
  gets the whole feature set rather than two pieces of it.
- **Against:** no control over acceptance, direction or release timing. A TypeScript and Bun
  codebase on every host, which the provisioning repository would rather not carry. The history
  feature is substantial and an unsolicited large contribution may not land.

### B. Build herddash as a narrow complement — *recommended*

A headless recorder: subscribe to herdr, persist every status transition with per-account
attribution, retain it under a bounded policy, and expose a small page of its own plus a read-only
API. It runs beside whichever browser interface the operator prefers rather than replacing it.

- **For:** keeps both surviving differentiators and the single-binary property that suits three
  machines. Honest about scope. Much smaller than the current PRD. Its data would be exactly what
  option A needs later, so this does not foreclose upstreaming.
- **Against:** two things to run. The page is the weakest part of the product and overlaps the
  existing interfaces most, so it must stay deliberately minimal or it drifts back into
  duplication.

### C. Build the full product as the PRD currently describes it

- **For:** one thing to run, entirely under our control, and the triage framing is still arguably
  better than what exists.
- **Against:** most of it knowingly duplicates two mature projects, one with several hundred stars.
  It is the largest build for the least unclaimed value.

## Consequences

If **B** is accepted:

- PRD sections 7 through 12 are rewritten around recording and attribution. The board becomes a
  secondary surface, not the product.
- The milestone list shortens. Milestone 1 stays as written, since protocol risk is unchanged.
- Open question 3, whether to backfill from the existing plugin's weeks of logged transitions,
  becomes important rather than optional. That log is the only historical data that exists.
- Open question 4 resolves: that plugin is superseded by this daemon and should be retired once it
  works, because they would record the same events twice.
- Open question 5 resolves by precedent. devswha keeps a finished-pane set beside a server
  generation marker; the same shape works here.
- Authentication stays deferred and loopback-only, which is easier to defend for a recorder with a
  minimal page than for a primary interface.

If **A** is accepted, this repository becomes the design record for a contribution and the Go and
single-binary decisions are discarded.

If **C** is accepted, the PRD stands as written and this record should say plainly that the
duplication is accepted knowingly.

## Alternatives considered and rejected

- **Fork alecuba16's project.** Rejected on licence. AGPL would relicense this work, and the
  provenance question in its record is unresolved.
- **Adopt one of the three unofficial Go socket clients as the foundation.** None is mature and all
  are version-0 with stated instability. Generating types from `herdr api schema` is the reusable
  idea; the libraries are not.
- **Use herdr plugin event hooks instead of a socket client.** Rejected on capability, not taste.
  Output-related events are deliberately excluded from plugin hooks, and a hook naming one gets a
  warning and silently never fires.
- **Rely on herdr's multi-host support for the three-machine view.** Rejected: it is client-side
  SSH federation with no machine dimension in any payload, and its cross-host bridge offers no
  event subscriptions. See [herdr-multi-host.md](../research/herdr-multi-host.md).
