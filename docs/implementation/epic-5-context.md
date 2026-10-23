# Epic 5 Context: After 0.5.0: closing what dogfooding and reviews left open

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Close the loose ends that dogfooding, story reviews and the open product questions left after the 0.5.0 release: review Epics 1 to 4 against their evidence, fix the known ways Claude Code background tasks can linger on a Box, retry Profile lookups that failed transiently, tighten the architecture test, get CI to exercise the Board's browser check and the macOS-only code, bring agent-facing documents in line with the code, and settle whether acknowledgement is needed using what the record shows. Nothing here adds a new product surface; it makes what exists trustworthy and keeps it from drifting.

## Stories

- Story 5.1: Retrospectives for Epics 1 to 4
- Story 5.2: A resumed session does not keep finished background tasks
- Story 5.3: `/clear` drops the previous session's background tasks
- Story 5.4: Retry a Profile lookup that failed transiently
- Story 5.5: Tighter import rules and Transition invariants (ADR-0011)
- Story 5.6: CI runs the Board's browser check
- Story 5.7: CI tests on macOS
- Story 5.8: Documents agents read match the code
- Story 5.9: Decide whether acknowledgement is needed (PRD §8 Q1)

## Requirements & Constraints

Operator decisions taken in advance (do not ask again):
- Stories are built and installed locally as `-dev`. At epic end the operator is asked once; on their OK everything ships as one pull request and one release.
- A pane herdr moves stays a removal plus a new Agent. Identity rules are unchanged and need no ADR; the behaviour is only documented.
- ADR-0011 is pre-accepted provided it only tightens import rules.
- The acknowledgement question is decided by the record, not the operator, unless the record shows a problem. Before 2026-11-03 that story is skipped, not failed.
- Background-task experiments may run throwaway Claude Code sessions under `cldp`, outside herdr, in a scratch directory. Never experiment on the live herdr server or a live agent pane.

Other constraints:
- Retrospective action items become stories in this epic, or `deferred-work.md` entries with a reason; any that needs a new operator decision is collected for one question at epic end.
- Experiment findings (Claude Code behaviour on resume and `/clear`) go in `docs/research/` with the Claude Code version and date. If the experiment shows no code change is needed, close the deferred entry with the evidence.
- Every closed `deferred-work.md` entry is marked resolved with where.
- Conventional Commits, feature branches only, no AI attribution in commits or PRs.

## Technical Decisions

- **Background tasks (ADR-0009).** Open tasks come from tailing the session transcript (located via `sessions/<pid>.json`'s `sessionId` and `cwd`) for start markers and task-notification ends, plus live tool-shell children while Status is `done`/`idle`. Only task ids and status words are extracted; never keep, log or show message content. Failure falls back silently to the process check. Tasks are view-only (`background_tasks` on `AgentView`); no stored schema change. Transcript re-read is bounded (8 MiB). Today nothing ages out a task whose end never arrives, and the process table only adds to the count.
- **Session record (ADR-0008).** Reads only `pid`, `status`, `statusUpdatedAt`; never opens `.key` files; regular files only. A change in `sessionId` on `/clear` would be the signal to reset an Agent's transcript tail and open tasks.
- **Profile resolution.** Resolved through `internal/procenv` before an Agent's first Transition with a bounded timeout yielding `unresolved`; re-resolved on agent detection. Retries apply only to transient failures (timeout, herdr call error), never to rule outcomes (unmatched, absent). Retry counts and backoff live in `internal/engine/tunables.go` and must stay off the engine actor's critical path (single-writer engine).
- **Import graph.** `arch_test.go` enforces the spine's import diagram; changing its permitted set needs an ADR (ADR-0011 here). Wire types live in `internal/herdr/gen` and must not be imported outside `internal/herdr`; `internal/herdr` and its gen/genwire packages should be standard-library only. `internal/core` stays stdlib-only and never reads the clock.
- **Transitions.** `core.NewTransition` is the single constructor for every Transition. `HerdrOrder` (`state_change_seq`) exists only for `snapshot` Transitions and is meaningful only within one herdr instance; live and backfill must reject it with a public sentinel error.
- **Identity.** `AgentKey{Host, Server, Epoch, PaneID}`; a moved pane (new pane id) is a removal plus a new Agent, which the spine's identity rule and `TestPaneMoved` should both state.
- **Docs facts to reconcile.** `go run` prints `herddash dev (unknown)`; release builds stamp version and commit via ldflags. Dependencies include `golang.org/x/sys`; `go.mod` declares the exact `go` directive. Engine tunables are to be documented with the configuration and dropped from the spine's Deferred list. The live herdr runs 0.9.5; the Story 1.3 checkpoint is closed.
- **Acknowledgement.** If ever needed, it becomes engine state stored through `store`. The evidence query: Agents that stayed `done` without a following `idle` while the operator was active, or `idle`/`done` disagreeing.

## UX & Interaction Patterns

- A Box reads BACKGROUND (green, dashed edge, ranked just before Working) while `done`/`idle` with open tasks; `WORKING · N bg` while Working with tasks; Finished only when none are open. Stories 5.2 and 5.3 exist so a Box never stays BACKGROUND because of a task from an earlier process or a cleared session.
- The Board's browser check covers strips, dimming, recap rows (`.board--recaps`), live updates and reconnect. In CI it must run against the runner's Chrome with no npm packages; removing the `.board--recaps` rule must fail it.

## Cross-Story Dependencies

- 5.1's action items may add stories to this epic or entries to `deferred-work.md`, and feed the single end-of-epic operator question.
- 5.8 documents work from the rest of the epic (tunables from 5.4, CI changes from 5.6/5.7, Epic 5's additions in CLAUDE.md), so it is best done after them. It also removes the "hand-run" wording that 5.6 must remove from CLAUDE.md.
- 5.2 and 5.3 both touch the engine's transcript tailing and open-task state; keep their changes consistent.
- 5.5 needs ADR-0011 written (pre-accepted if it only tightens rules).
- 5.7's macOS job makes the launchd plist lint (`TestLaunchdAgentLints`) and procenv's darwin reader run in CI; existing ubuntu jobs stay unchanged.
- 5.9 is date-gated (2026-11-03) and may end at an operator decision.
- The epic ships as one PR and one release only after the operator's OK at the end.
