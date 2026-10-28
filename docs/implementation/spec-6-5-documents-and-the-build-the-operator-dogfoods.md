---
title: 'Story 6.5: Documents, and the build the operator dogfoods'
type: 'chore'
created: '2026-10-27'
status: 'done'
baseline_revision: '4154742'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/adr/0014-acknowledging-a-finished-agent.md'
  - '{project-root}/docs/implementation/spec-6-1-acknowledgements-in-the-record.md'
  - '{project-root}/docs/implementation/spec-6-2-the-engine-acknowledges-a-finish.md'
  - '{project-root}/docs/implementation/spec-6-3-write-routes-guarded-against-cross-site-requests.md'
  - '{project-root}/docs/implementation/spec-6-4-the-board-s-seen-control-bulk-mark-and-undo.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Stories 6.1 to 6.4 built acknowledgement ("Seen", ADR-0014): migration 0005, the
engine's decision, four guarded `POST` routes and the Board's controls. The documents a fresh
session or a herdr user reads first still described herddash as read-only throughout, with four
migrations and Epic 5 in progress, and `docs/install.md` said nothing about a schema upgrade being
a one-way door. The operator's dogfooding checkpoint needs a `-dev` build of the epic running under
launchd.

**Approach:** Edit `docs/how-herddash-works.md`, `CLAUDE.md` and `docs/install.md` to state what
the code at the branch does, each claim checked against ADR-0014 and the code; confirm every Epic 6
review deferral is in a story's criteria or `deferred-work.md`; then build a `0.8.0-dev` binary of
the documents commit, back the database up with the Daemon stopped, install it at
`~/.local/bin/herddash`, start the launchd agent and check it.

## Boundaries & Constraints

**Always:** The installed Daemon is stopped and its process gone before the database is copied.
The previous release binary is kept beside the new one for a revert. Every claim in the documents
is checked against the code (`internal/httpapi/ack.go`, `internal/store/migrations/`, `v0.7.1`'s
migration list). `docs/install.md` keeps every string the deploy tests look for.

**Never:** Touch the live herdr server, call `pane.focus` or `agent.focus`, or make an
acknowledgement on the operator's real Agents (the guard check uses a key that names no Agent and
is refused before its body is read). Change code or behaviour.

</intent-contract>

## What was done

- `docs/how-herddash-works.md`: the opening no longer says nothing is built; "It is read-only"
  becomes "It changes nothing in herdr", with Seen as its one control; the web server's bullet
  names the four write routes; the history route's `acknowledgements`; a new section "Marking a
  finished agent Seen" saying what a mark does (one exact finish, until the next change out of it,
  `done` to `idle` keeps it, recorded append-only and shown in history, survives a restart, refused
  with recording off, broadcast as the agent's updated box) and does not do (never tells herdr,
  changes no recorded status, only a finished, non-background box can be marked); the never-list's
  "Never accept a change over the web" becomes "Never accept a change over the web but an
  acknowledgement, and only from its own page", with the guard in plain words; contributors are
  pointed at ADR-0004 to ADR-0014.
- `CLAUDE.md`: the opening summary says read-only but for acknowledgement; the open checkpoint
  says Epic 6 is built and installed and waits on the operator's dogfooding; ADR-0013 and ADR-0014
  are listed with the spine's ADRs; the repository state says Epics 1 to 5 are released (v0.1.0 to
  v0.7.1) and Epic 6 is built on `feat/epic-6-acknowledgement`; `internal/core` gains the
  acknowledgement rule; `internal/store` says five migrations, 0005's table and its one-way door
  (0.7.x and earlier refuse schema 5, checked against `git ls-tree v0.7.1`); `internal/httpapi`
  lists the four `POST` routes and their guard in order (loopback `Host`, same-origin `Origin`,
  `Sec-Fetch-Site`, `X-Herddash-Write: 1`, JSON, 4 KiB or 64 KiB body, one JSON object, no CORS,
  `OPTIONS` 405) and the engine's error codes; the Board's only controls; and a "Seen" entry for
  every Agent: `seen` always sent, `attention_class` stays `finished`, rank 2 with Idle, counted
  `N seen`, derived from the record across a restart.
- `docs/install.md`: "Upgrading the unit files" points at a new Backup subsection, "Before a build
  with a new schema", with the schema table (4 from 0.6.0, 5 from Epic 6, refused by 0.7.x and
  earlier), the order (stop, copy, install while stopped, start) using the section's existing
  commands, a backup beside the database as an alternative location, and a `PRAGMA user_version`
  check.
- `docs/implementation/deferred-work.md`: unchanged. Epic 6's reviews deferred one item, Story
  6.1's `retention sweep` log line, and it named Story 6.2, whose criteria in `epics.md` already
  carry it and which built it (`internal/engine/retention.go`, `TestAckRetentionLogsAcknowledgements`).
  Stories 6.2 to 6.4 deferred nothing; their `[accept]` findings are decisions, not open work.

## The installed build

1. Built at the documents commit `5bcf6cb90b6b`, tree clean:
   `scripts/release-build.sh 0.8.0-dev 5bcf6cb90b6b <scratchpad>/dist-6-5`; all four checksums
   OK; `go version -m` shows `CGO_ENABLED=0`, `vcs.revision=5bcf6cb…`, `vcs.modified=false`.
2. `launchctl bootout gui/$(id -u)/io.github.vilosource.herddash`, then waited until `pgrep -x`
   found no `herddash` process. The clean stop folded the write-ahead log into the database.
3. Backed up per `docs/install.md`'s Backup section to
   `~/.local/share/herddash/herddash.db.pre-0005.bak`, beside the database as the earlier
   `herddash.db.pre-0003.bak` is: byte-identical to the database (`cmp`), schema 4,
   `integrity_check` ok, 2842 Transitions.
4. Kept the 0.7.1 release binary as `~/.local/bin/herddash-0.7.1`, installed
   `herddash_0.8.0-dev_darwin_arm64` at `~/.local/bin/herddash` with `install -m 755`, and loaded
   the agent with `launchctl bootstrap`. It was `state = running` at once (no codesigning
   refusal this time, since the job was booted out rather than kickstarted).

**To revert:** stop the agent and wait as above, restore `herddash.db.pre-0005.bak` per
`docs/install.md`'s "To restore", `cp -p ~/.local/bin/herddash-0.7.1 ~/.local/bin/herddash`, and
bootstrap the agent. Anything recorded since the upgrade is lost.

## Verification

**Commands** (Go 1.26.5 at `$HOME/.local/share/mise/installs/go/1.26.5/bin`, prepended to PATH):
- `go build ./... && go vet ./...` -- pass
- `gofmt -l .` -- no output
- `go test ./...` -- all 12 packages pass
- `node --test internal/httpapi/web/order.test.mjs` -- 102 pass, 0 fail
- No concurrency test was added or changed, so no `-race -count=50` run was due.
- `node internal/httpapi/web/check/board-browser.mjs <dist>/herddash_0.8.0-dev_darwin_arm64` --
  exit 0, every check ok (54 `ok` lines), including the cross-origin page refused and a seen Box
  still seen after a restart; it runs its own fake herdr and temporary data directory, and left no
  process behind.

**The installed Daemon** (2026-10-27, 12:37 local):
- `~/.local/bin/herddash version` -- `herddash 0.8.0-dev (5bcf6cb90b6b)`; the running process is
  `/Users/jasonviloria/.local/bin/herddash serve` under launchd.
- `sqlite3 -readonly ~/.local/share/herddash/herddash.db 'PRAGMA user_version'` -- `5`;
  `acknowledgements` holds 0 rows. The backup sits beside it.
- `GET http://127.0.0.1:7318/api/state` -- 10 Agents, every one carrying `seen` (all false), the
  connection `connected`, herdr 0.9.5, gate `ok`; the log shows the epoch kept across the restart
  (`reconnected`, `epoch 2`, `epoch_kept true`). `GET /` -- 200.
- `POST /api/seen` with `X-Herddash-Write: 1` and JSON but no `Origin` -- `403
  {"error":{"code":"origin_not_allowed",…}}`; with `Origin: http://evil.example` -- the same 403;
  `OPTIONS /api/seen` -- `405`, `Allow: POST`, no `Access-Control-*` header. The table still holds
  0 rows afterwards.

**Not done here, by design:** asking the operator to dogfood. The sprint-status action item for
Epic 6 stays open until the operator has used the Seen control on their live Board and said
whether the epic ships; on their OK it ships as one pull request and one release.

## Review Triage Log

### 2026-10-27 — Self-review pass
- `[low]` `[accept]` The installed binary is built at the documents commit; the commit that adds
  this spec and the sprint status changes no code, so the binary's code is the branch's.
- `[low]` `[accept]` `sqlite3 -readonly` on a stopped WAL database whose `-shm` is gone fails
  (`unable to open database file`); the backup was checked with `file:…?immutable=1` instead.
  `docs/install.md`'s schema check runs while the Daemon is up, when the `-shm` exists, and
  `-readonly` works there (checked against the running 0.7.1 before the upgrade, which printed 4).

### 2026-10-27 — Epic 6 final review (documents)
- `[medium]` `[patch]` `README.md` still called herddash, its API and its Board read-only. It now
  says read-only apart from marking a finished result seen, whose routes accept requests only from
  the Board's own page (ADR-0014).
- `[low]` `[patch]` Other stale "read-only" text: `docs/config.example.toml`'s `bind`, the systemd
  unit's `Description`, the `cmd/herddash` package comment and `runServe`'s comment now say
  read-only apart from acknowledgement, or drop the claim.
- `[low]` `[patch]` CLAUDE.md's "Repository state" now describes the title bar (Story 5.19,
  ADR-0013: text logo, a summary that restates the Needs you count and herdr's connection, no
  control, not a live region), heads the Epic 5 list as done, lists Story 5.19 in it and says what
  v0.7.1 carried (the launchd start-delay note in `docs/install.md`).
- `[low]` `[patch, partly rejected]` "With recording turned off, marking is refused" in
  how-herddash-works and ADR-0014. The review said the shipped Daemon never reaches that mode;
  that part is rejected: `restore` (`internal/engine/recorder.go`) sets `e.store = nil` for the
  run when the saved state fails to load, so the shipped binary can answer 503 `recording_off`
  (deferred-work's retro F-6 entry). What is true is that no setting turns recording off and that
  with herdr refused by the version gate a mark gets 404 `unknown_agent`; both documents now say
  so, and CLAUDE.md says which run gives the 503.
- `[low]` `[patch]` The schema-6 migration the final review's engine fix needed is reflected in
  CLAUDE.md, `docs/install.md`'s schema table, the spine's AD-10 and how-herddash-works. The
  installed `-dev` database is at schema 5 and is migrated to 6 by the next `-dev` build;
  `TestUpgradeFromSchema5` covers that path. `epics.md`'s Story 6.1 and 6.5 criteria, which name
  schema 5, are the plan as it was cut and are left as they are.
