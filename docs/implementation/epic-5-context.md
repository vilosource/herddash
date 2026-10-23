# Epic 5 Context: After 0.5.0: closing what dogfooding and reviews left open

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Close the gaps that dogfooding 0.5.0, the Epic 1 to 4 retrospectives and the code reviews left open, so the Board can be trusted without anyone checking it against herdr. That means Boxes that leave BACKGROUND when the work is over, Profiles and Waiting Times that stay correct across reused panes and restarts, a Board that notices a hung or failing Daemon, exact text and grouping, stricter CI and import rules, and documents that match the code. The items come from `docs/implementation/deferred-work.md`, the retrospectives, the operator's planning answers and the PRD's open question on acknowledgement. The operator decided them in advance, so no story asks them again.

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
- Story 5.10: A new Agent in a reused pane starts fresh
- Story 5.11: The Board notices a hung Daemon
- Story 5.12: The engine treats repeated failures as a lost connection
- Story 5.13: Board text and grouping are exact

## Requirements & Constraints

- **Delivery.** Each story is built and installed locally as a `-dev` build. When the epic is done the operator is asked once, and on their OK everything ships as one pull request and one release. Do not open release PRs per story.
- **Pre-decided, do not re-ask:** a pane herdr moves stays a removal plus a new Agent (AD-3 unchanged, documented only, no ADR). ADR-0011 is accepted in advance provided it only tightens import rules. The acknowledgement question is decided by what the record shows, and goes to the operator only if the record shows a problem; before 2026-11-03 that story is skipped, not failed.
- **Experiments.** Background-task and `/clear` experiments may run throwaway Claude Code sessions under `cldp`, outside herdr, in a scratch directory. Findings go in `docs/research/` with the Claude Code version and date. Never experiment on the live herdr server or its panes.
- **Retrospective follow-through.** An action item that is code or documentation work becomes a story here, or goes into `deferred-work.md` with a reason. Items needing a new operator decision are collected for one question at the end of the epic.
- **Standing non-functional rules still bind:** read-only GET surface on loopback only; no `pane.focus`/`agent.focus`; no pane-output reads; a Transition reaches an open Board within one second; reconnect forever (herdr absence or restart is normal); bounded herdr load (reconcile timer plus a minimum resync interval); Board restraint (only Boxes, workspace labels with counts, the Needs you heading, the connection and version strips; anything more needs an ADR); every Box the same height; tests never touch a live herdr; `go build`, `go vet`, `go test` (with `-race` in CI) and `gofmt -l .` stay clean.
- **Process lessons** that apply to every story: copy a deferral into the criteria of the story it targets; give every spine Open item an owner; run new concurrency tests with `-race -count=50`; test state derived from the record across a Daemon restart; make rollback tests fail mid-transaction.

## Technical Decisions

- **Package boundaries.** Hexagonal: pure `internal/core` (standard library only, never reads the clock; time comes in as a parameter), one engine actor that alone owns current state and calls `store.Append`, and the adapters `herdr`, `procenv`, `store`, `httpapi` and `importer`. `arch_test.go` enforces the import graph. Changing the permitted set is structural and needs an ADR. ADR-0011 tightens it further: `internal/herdr/gen` and `genwire` may be imported only from inside `internal/herdr`, and `internal/herdr` and its gen packages may import only the standard library.
- **Identity.** An AgentKey is (Host, Server, Epoch, PaneID). The epoch changes only on `terminal_id` evidence. A tracked pane whose non-empty `terminal_id` changes is removed (`terminal replaced`) and comes back as a new Agent. Epoch 0 is reserved for backfill. For reused panes the decision is: no schema change and no ADR. The key stays as it is, and the Waiting Time fold starts over at an Agent's first row (a row with no previous Status) and whenever the saved `terminal_id` for the pane changes. AD-3 must document that one AgentKey can hold successive Agents separated by such a row. The epoch and the terminal map are persisted (`server_state`) as engine state outside the append-only record.
- **Record.** Transitions are append-only. Retention is the only delete, and it keeps everything from the Transition that began the current Attention Class onward, plus the latest `working`. `core.NewTransition` is the only constructor (it must reject a `HerdrOrder` on live or backfilled Transitions with a sentinel error), and one INSERT writes every row. Unknown values are stored as NULL. A migration touching the transitions table needs an ADR.
- **Waiting Time** comes from the record through `core.Fold` at start and after each resync. It is exact only when it starts from a `live` Transition whose Status is unchanged; otherwise it is a lower bound.
- **Profile** is resolved through `procenv` before an Agent's first Transition, with a bounded timeout that yields `unresolved`. It is re-resolved on `pane.agent_detected`. Rule outcomes (absent, unmatched) are final. Only transient failures (a timeout or a call error) may be retried, with bounded attempts and backoff, off the engine goroutine's critical path.
- **Tunables.** Engine timing values (reconcile, resync interval, backoff, timeouts, failure bounds) are named constants in `tunables.go`, documented beside the configuration.
- **Claude Code sources** are read through `procenv` only. The source of Background is `sessions/<pid>.json` (`pid`, `status`, `statusUpdatedAt`, `sessionId`, `cwd`) plus the session transcript, tailed from an in-memory offset. Background tasks feed the view only and are never recorded. Recaps require `claude_recaps` consent and are never recorded or logged.
- **Engine owns decisions.** `internal/herdr` only does things: dial, ping, snapshot, subscribe and health signals. The engine owns resync, the reconnect and lost-connection decisions, ConnectionTruth (`connected`, `last_snapshot_at`, `herdr_version`, `herdr_minimum`, `gate`, `resync_guaranteed`) and the version gate. An unknown herdr error code is a resync, never a crash.
- **Event stream.** `/events` opens with a `state` event that has the `/api/state` body, the run id and the view sequence. Then come `agent`, `removed`, `transition`, `resync` and `connection` events. `id:` is the view sequence. Each stream has a bounded buffer, and on overflow the server closes it and the client reconnects to a fresh `state`. The server keeps no event history.
- **JSON** fields are `snake_case`, and errors are `{"error":{"code","message"}}`. Omitted values must match what the API actually sends: it sends empty strings, and the packages that own the wire shapes are named in the spine. Logging is `slog` JSON. Environment values are never logged, and Pane Titles only at debug.
- **Toolchains.** `go.mod` is at the 1.26 minimum, and releases build with Go 1.27.2 (`CGO_ENABLED=0`, `-trimpath`, four targets). Dependencies are pinned, and a new dependency needs an ADR.

## UX & Interaction Patterns

- The Board orders Boxes by core's sort key `[zone, group, class_rank, start, agent_key]`. The top zone (Needs you, Finished) is flat, and the lower zone (Working, Unknown) is grouped by workspace position. Workspaces with no known position each get their own group under their own name, ordered after positioned ones.
- The Board's logic is `order.mjs`, tested with `node --test` with no npm. The drawing check is `board-browser.mjs`, run against headless Chrome, and this epic moves it into CI.
- When the stream drops or goes silent beyond the keepalive allowance, the Board shows the connection strip, dims the Boxes and reconnects. A skipped sequence number or a rejected `state` event triggers a fresh `/api/state` load. When herdr's version is unknown, the version strip says it could not be read rather than "below minimum".
- Box text: neutralise bidi controls and isolate the text. Strip only Claude Code's own glyph prefix, never a leading `-`, `#` or emoji.

## Cross-Story Dependencies

- 5.1's retrospectives feed 5.4, 5.7, 5.8, 5.10 and 5.11, and may add stories or deferred entries.
- 5.8 documents what other stories change: the tunables (5.4, 5.12), AD-3's moved-pane and reused-pane notes (5.10), and Epic 5's additions in CLAUDE.md. It should land after them or be updated with them. Once 5.6 is done, CLAUDE.md must stop calling the browser check hand-run.
- 5.2 and 5.3 both change how the engine handles open background tasks from `procenv` and the transcript tail. Their code paths overlap.
- 5.11's Go test that replays engine publications through `applyEvent` relies on the SSE contract that 5.12's connection changes also feed.
- 5.9 is date-gated (2026-11-03) and may stop for an operator decision. If acknowledgement is needed, it becomes engine state stored through `store`.
- The release at the end of the epic depends on every story being done and on the operator's single OK.
