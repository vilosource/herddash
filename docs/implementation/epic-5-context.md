# Epic 5 Context: After 0.5.0: closing what dogfooding and reviews left open

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Close the loose ends that dogfooding, story reviews, the Epic 1 to 4 retrospectives and the open product questions left after the 0.5.0 release: fix the known ways a Box can show stale or wrong state (lingering background tasks, a reused pane inheriting an old Waiting Time, a hung Daemon or failing snapshots looking live, a manual `/recap` not appearing, mangled text and merged groups), retry transient Profile failures, tighten the architecture test, get CI to exercise the Board's browser check and macOS-only code, bring agent-facing documents in line with the code, and settle whether acknowledgement is needed using what the record shows. Nothing here adds a new product surface; it makes what exists trustworthy and keeps it from drifting.

## Stories

- Story 5.1: Retrospectives for Epics 1 to 4
- Story 5.2: A resumed session does not keep finished background tasks
- Story 5.3: `/clear` drops the previous session's background tasks
- Story 5.4: Retry a Profile lookup that failed transiently
- Story 5.5: Tighter import rules and Transition invariants (ADR-0011)
- Story 5.6: CI runs the Board's browser check
- Story 5.7: CI tests on macOS
- Story 5.8: Documents agents read match the code
- Story 5.9: Decide whether acknowledgement is needed
- Story 5.10: A new Agent in a reused pane starts fresh
- Story 5.11: The Board notices a hung Daemon
- Story 5.12: The engine treats repeated failures as a lost connection
- Story 5.13: Board text and grouping are exact
- Story 5.14: A manual `/recap` reaches the Board, and local commands are not turns
- Story 5.15: Bash-mode commands are not turns either

## Requirements & Constraints

Operator decisions taken in advance (do not ask again):
- Stories are built and installed locally as `-dev`. At epic end the operator is asked once; on their OK everything ships as one pull request and one release.
- A pane herdr moves stays a removal plus a new Agent; identity rules are unchanged, no ADR, only documented.
- ADR-0011 is pre-accepted provided it only tightens import rules.
- The acknowledgement question is decided by the record, not the operator, unless the record shows a problem. Before 2026-11-03 that story is skipped, not failed.
- Background-task experiments may run throwaway Claude Code sessions under `cldp`, outside herdr, in a scratch directory. Never experiment on the live herdr server or a live agent pane.
- Reused-pane handling (5.10) was decided without a schema change or ADR: the record's key stays host, server, epoch, pane id.

Other constraints:
- Retrospective action items become stories here or `deferred-work.md` entries with a reason; any needing a new operator decision is collected for one question at epic end.
- Experiment findings about Claude Code behaviour go in `docs/research/` with the Claude Code version and date. If no code change is needed, close the deferred entry with the evidence.
- Tests use record shapes copied from real transcripts, with no transcript text beyond what the test needs.
- Every closed `deferred-work.md` entry is marked resolved with where.
- New concurrency tests run with `-race -count=50`; record-derived state is tested across a Daemon restart.
- Conventional Commits, feature branches only, no AI attribution in commits or PRs.

## Technical Decisions

- **Background tasks (ADR-0009).** Open tasks come from tailing the session transcript (named by `sessions/<pid>.json`'s `sessionId` and `cwd`) for start markers and task-notification ends, plus live tool-shell children while Status is `done`/`idle`. Only task ids and status words are extracted; never keep, log or show message content. View-only (`background_tasks` on `AgentView`), never recorded. Transcript tail: last 8 MiB at first sight, then at most 1 MiB of complete new lines per read. A change in `sessionId` is the candidate signal to reset an Agent's tail and open tasks.
- **Session record (ADR-0008).** Reads only `pid`, `status`, `statusUpdatedAt`, `sessionId`, `cwd`; regular files only.
- **Recaps (ADR-0010).** Read only with `claude_recaps` consent (default off); off means no recap record content is read at all. Latest recap only, in memory, never recorded or logged; trimmed of whitespace and the trailing " (disable recaps in /config)", capped at 400 characters at a character boundary. A recap holds until the main thread's next turn start (a prompt, tool result or queued command). Today only `away_summary` is decoded. A manual `/recap` instead writes an `isMeta` caveat user record, a user record starting `<command-name>/recap</command-name>`, then a `system` record with subtype `local_command`, `commandRun.command` "recap" and `<local-command-stdout>` content; local commands such as `/usage`, `/compact`, `/model` must not count as turns, while skill commands (`<command-message>`) followed by an assistant turn do. A `<local-command-stderr>` result is not a turn either. Bash-mode records (`<bash-input>`, `<bash-stdout>`) *are* turn starts and clear the recap: Claude Code sends a `!cmd`'s output to the model, which answers it (verified on this machine's transcripts; see `docs/research/claude-code-bash-mode-2026-10-23.md`). Turn-start judgements are structural only, never by reading message text beyond the needed tags.
- **Waiting Time (one fold).** Waiting Time and Attention Class come from the record through `core.Fold`, never re-derived elsewhere. For 5.10 the fold starts over at an Agent's first row (no previous Status) and when the saved `terminal_id` for the pane changes; one AgentKey can hold successive Agents separated by such a row. The test fake must match the store.
- **Identity and epochs.** `AgentKey{Host, Server, Epoch, PaneID}`. The epoch and pane-to-`terminal_id` map are persisted (`server_state`) as engine state outside the append-only record.
- **Profile resolution.** Resolved via `internal/procenv` before an Agent's first Transition with a bounded timeout. Retry only transient failures (timeout, herdr call error), never rule outcomes. Retry counts, backoff and other bounds (e.g. 5.12's snapshot-failure bound) live in `internal/engine/tunables.go` and stay off the single-writer engine's critical path.
- **ConnectionTruth** is `{connected, last_snapshot_at, herdr_version, herdr_minimum, gate, resync_guaranteed}`; the Board's strips are driven by it. An unexpected but well-formed lifecycle event should resnapshot, not reconnect.
- **Event stream.** First event on every connection is `state` (same shape as `/api/state`, with run id and view sequence); `id:` is the view sequence; events are `state`, `agent`, `removed`, `transition`, `resync`, `connection`. Bounded per-stream buffer; on overflow the server closes and the client reconnects to a fresh `state`. Sequence numbers are never compared across Daemon restarts.
- **Import graph.** `arch_test.go` enforces the spine's diagram; changing its permitted set needs an ADR. `internal/herdr/gen` and `genwire` are not imported outside `internal/herdr`; those packages and `internal/core` stay stdlib-only; `internal/core` never reads the clock.
- **Transitions.** `core.NewTransition` is the single constructor. `HerdrOrder` (`state_change_seq`) exists only for `snapshot` Transitions; live and backfill reject it with a public sentinel error.
- **Acknowledgement**, if ever needed, becomes engine state stored through `store`.

## UX & Interaction Patterns

- A Box reads BACKGROUND (green, dashed edge, ranked just before Working) while `done`/`idle` with open tasks; `WORKING · N bg` while Working with tasks; Finished only when none are open.
- A non-empty recap takes the Context Line's row, clamped to two lines, full text in tooltip and accessible name; while any Box shows a recap all Boxes are one line taller.
- A lost Daemon (dropped or silent stream) shows the connection strip and dims the Board, then reconnects. When herdr's version is unknown the version strip says it could not be read, not "below the supported minimum".
- Box text: bidi controls neutralised and text isolated; only Claude Code's own glyph prefix is stripped (not a leading `-`, `#` or emoji). Workspaces with no known position each get their own named group, after positioned ones.
- The browser check (`board-browser.mjs`) covers strips, dimming, recap rows (`.board--recaps`), live updates and reconnect; in CI it runs against the runner's Chrome with no npm packages. Board event logic (`order.mjs`, `applyEvent`) is tested with `node --test`.

## Cross-Story Dependencies

- 5.1's action items fed several later stories (5.4, 5.7, 5.8 and 5.10 carry retro items) and the end-of-epic operator question.
- 5.2, 5.3, 5.14 and 5.15 all touch transcript tailing and the decoder's turn-start logic; keep them consistent. 5.14 also builds on ADR-0010's recap rules; 5.15 follows up 5.14 (its review findings and the stderr case) and was amended after verification refuted its bash-mode premise.
- 5.11 and 5.12 both feed the Board's connection strip and dimming; 5.11's Go test replays real engine publications through `applyEvent`.
- 5.10 changes the fold and its restart and Retention-sweep tests; history must include the Transition that began the current class.
- 5.8 documents the rest of the epic (tunables, CI changes, Epic 5's additions in CLAUDE.md), so it is best done late; 5.6 removes the "hand-run" wording from CLAUDE.md.
- 5.5 needs ADR-0011. 5.7 adds a macOS job (`TestLaunchdAgentLints`, procenv's darwin reader) and release-toolchain checks without changing existing ubuntu jobs.
- 5.9 is date-gated (2026-11-03) and may end at an operator decision.
- The epic ships as one PR and one release only after the operator's OK at the end.
