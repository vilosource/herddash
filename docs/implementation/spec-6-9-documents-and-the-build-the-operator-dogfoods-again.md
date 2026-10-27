---
title: 'Story 6.9: Documents, and the build the operator dogfoods again'
type: 'chore'
created: '2026-10-27'
status: 'done'
baseline_revision: '2bcfa8d'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0015-seen-when-you-leave-the-pane.md'
  - '{project-root}/docs/research/herdr-focus-events-2026-10-27.md'
  - '{project-root}/docs/implementation/spec-6-5-documents-and-the-build-the-operator-dogfoods.md'
  - '{project-root}/docs/implementation/spec-6-6-read-herdr-s-focus-and-record-where-a-mark-came-from.md'
  - '{project-root}/docs/implementation/spec-6-7-the-engine-marks-a-finish-seen-when-the-operator-leaves-its-pane.md'
  - '{project-root}/docs/implementation/spec-6-8-the-board-shows-in-view-and-offers-mark-unseen.md'
warnings:
  - 'The install under launchd and the checks against the live Daemon are left to the orchestrator, which owns restarting the operator''s herddash; see "The installed build".'
deferred: []
---

<intent-contract>

## Intent

**Problem:** Stories 6.6 to 6.8 built Seen on leaving the pane (ADR-0015): the `pane.focused`
subscription, migration 0007's `source`, the engine's focus tracking and automatic mark,
`in_view`, the `● in view` tag and `Mark unseen`. The documents a fresh session or a herdr user
reads first still described Seen as a Board click only, with six migrations and the checkpoint
at Story 6.5, and `docs/install.md`'s schema table stopped at 6. The operator's dogfooding
checkpoint, moved to the end of this story by ADR-0015, needs a `-dev` build of the whole epic.

**Approach:** Edit `docs/how-herddash-works.md`, `CLAUDE.md`, `docs/install.md` and `README.md`
to state what the code at the branch does, each claim checked against ADR-0015 and the code;
confirm every review deferral of Stories 6.6 to 6.8 is in a story's criteria or
`deferred-work.md`; build a `0.8.0-dev` binary of the documents commit and check it, including a
migration of a consistent copy of the operator's database to schema 7.

## Boundaries & Constraints

**Always:** Every claim in the documents is checked against the code
(`internal/herdr/subscribe.go`, `internal/herdr/map.go`, `internal/engine/focus.go`,
`internal/engine/tunables.go`, `internal/core/acknowledgement.go`, `internal/core/view.go`,
`internal/httpapi/history.go`, `internal/httpapi/web/order.mjs`,
`internal/store/migrations/0007_acknowledgement_source.sql`). `docs/install.md` keeps every string
the deploy tests look for.

**Never:** Touch the live herdr server, call `pane.focus`, `agent.focus` or any herdr mutation,
make an acknowledgement on the operator's real Agents, or change code or behaviour. In this run,
also never stop, reinstall or restart the operator's launchd Daemon: the orchestrator does that.

</intent-contract>

## What was done

- `docs/how-herddash-works.md`: the opening says herddash also marks Seen when the operator reads
  a result in herdr and moves on; the herdr client's bullet says it follows herdr's focused pane
  by reading focus events and never sets focus; the history route's acknowledgements carry
  `source` (`board` or `herdr_focus`); a new subsection, "Seen when you leave the pane in herdr",
  gives the rule (3 s focused after the finish, then focus moves to another pane), the
  `● in view` tag, flicking marks nothing, prompt then leave, several turns watched, a finish
  into the focused pane while away, Needs you never and Background still first, `Mark unseen`
  and the operator's choice winning for that finish, what ends a mark, a restart keeping marks
  and missing moves made while it was down, that herddash only reads focus and why herdr's
  `done`/`idle` is not used, and the three caveats: `in view` on a detached herdr (never a mark by
  itself), several attached terminals, and another program's focus call. The never-list's "Never
  focus a pane" says reading focus changes nothing; contributors are pointed at ADR-0004 to
  ADR-0015.
- `CLAUDE.md`: 55 stories (9 for Epic 6); the open checkpoint says Stories 6.1 to 6.9 are done and
  the Story 6.9 `-dev` build (schema 7) is the one dogfooded; ADR-0015 listed with the spine's
  ADRs; `internal/herdr` gains the `pane.focused` kind, the dropped `workspace_focused` and
  `tab_focused` lines, `FocusedPaneID`, and still no focus method; `internal/engine` the in-memory
  focus and dwell, no snapshot from a focus event, the leave rule and its `herdr_focus` row;
  `internal/core` `Source` and `core.SeenOnLeave`; `internal/store` seven migrations, 0007's
  `source` and its one-way door (0.7.x and earlier refuse 5, 6 or 7; the Epic 6 `-dev` builds
  before Story 6.6 refuse 7); history items' `source`; the Board's `Mark unseen` and `● in view`;
  a "Seen on leave" entry with `in_view` and its caveats.
- `docs/install.md`: schema 7 in "Before a build with a new schema", refused by schema-6 builds
  including the first Epic 6 `-dev` builds; its example check prints `7`.
- `README.md`: the read-only bullet says herddash also marks Seen when the operator leaves the pane
  in herdr, reading focus and never setting it (ADR-0015).
- `docs/implementation/deferred-work.md`: unchanged. Story 6.6 deferred nothing. Story 6.7's one
  deferral (a redraw losing a keyboard-focused pill) was routed into Story 6.8's criteria in
  `epics.md` (8102984) and built there. Story 6.8's one deferral (the in-view tag's title
  overpromising for a finish that already has a row) is already an open `deferred-work.md` entry.
- `docs/planning/architecture/.../ARCHITECTURE-SPINE.md` already carried ADR-0015 (Stories 6.6 to
  6.8); `docs/config.example.toml` already lists the 3 s dwell. Neither changed.

## The installed build

1. Built at the documents commit `34d9daa1c35d`:
   `scripts/release-build.sh 0.8.0-dev 34d9daa1c35d <scratchpad>/dist-6-9`; all four checksums OK;
   `herddash_0.8.0-dev_darwin_arm64 version` prints `herddash 0.8.0-dev (34d9daa1c35d)`; `go
   version -m` shows `CGO_ENABLED=0`, `-trimpath=true`, `vcs.revision=34d9daa…`. It shows
   `vcs.modified=true` because `.vfkb/entries.jsonl`, the knowledge base's own file and no code,
   was modified in the working tree.
2. **Migration checked on a copy, not on the live database.** A consistent copy of the
   operator's database was taken with `sqlite3 -readonly … ".backup …"` while the installed
   Daemon kept running (schema 6, 3150 Transitions, 1 acknowledgement). The new binary ran
   `serve` on that copy, its own data directory, `--bind 127.0.0.1:7399` and a socket path that
   does not exist, with `HERDR_SOCKET_PATH` unset, so it never reached herdr: it migrated the
   copy to schema 7, 3150 Transitions unchanged, the existing row's `source` `board`,
   `integrity_check` ok. `/api/state` answered; a `POST /api/seen` without `Origin`, and one with
   `Origin: http://evil.example`, were both `403 origin_not_allowed`.
3. **Left to the orchestrator**, which owns the operator's launchd Daemon in this run: stop the
   agent (`launchctl bootout`, then wait until `pgrep -x herddash` finds nothing), back up the
   database beside itself as `~/.local/share/herddash/herddash.db.pre-0007.bak` per
   `docs/install.md`'s Backup section, keep the running binary (`0.8.0-dev`, `edaf0ab`) as a
   revert copy, install `herddash_0.8.0-dev_darwin_arm64` at `~/.local/bin/herddash` with
   `install -m 755`, `launchctl bootstrap` the agent, then check: `herddash version` prints
   `34d9daa1c35d`; `PRAGMA user_version` is 7 with the backup beside it; `/api/state` has `seen`
   and `in_view` on every Agent and exactly one `in_view` when herdr's focused pane holds an
   Agent; the log shows the lifecycle subscription started and no run of snapshot reads after
   focus events; a `POST /api/seen` without `Origin` is 403. No acknowledgement is made.

**To revert:** stop the agent and wait, restore `herddash.db.pre-0007.bak` per
`docs/install.md`'s "To restore", put the kept `edaf0ab` binary back at `~/.local/bin/herddash`,
and bootstrap the agent. Anything recorded since the upgrade is lost.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test ./...` -- all 12 packages pass
- `node --test internal/httpapi/web/order.test.mjs` -- 107 pass, 0 fail
- No concurrency test was added or changed, so no `-race -count=50` run was due.
- `node internal/httpapi/web/check/board-browser.mjs <dist>/herddash_0.8.0-dev_darwin_arm64` --
  exit 0, every check ok (78 `ok` lines), including the focus section, `● in view` and Mark
  unseen; it runs its own fake herdr and temporary data directory, and left no process behind.

**Not done here, by design:** the install under launchd and the live checks (above), and asking
the operator to dogfood. The sprint-status action item for Epic 6 stays open until the operator
has used Seen on leave on their live Board and said whether the epic ships; on their OK it ships
as one pull request and one release.

## Review Triage Log

### 2026-10-27 — Self-review pass
- `[low]` `[accept]` The binary is built at the documents commit; the commit adding this spec and
  the sprint status changes no code, so the binary's code is the branch's.
- `[low]` `[patch]` README.md's read-only bullet named only the Board click; it now names Seen on
  leave too, so it agrees with how-herddash-works and ADR-0015.
- `[low]` `[accept]` `epics.md`'s Story 6.9 criteria name the live checks; they are listed under
  "The installed build" for the orchestrator rather than claimed here.
