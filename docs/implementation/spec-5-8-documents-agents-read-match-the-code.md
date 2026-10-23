---
title: 'Story 5.8: Documents agents read match the code'
type: 'chore'
created: '2026-10-23'
status: 'done'
baseline_revision: 'b095da655d2e8725a95514b762c029acb0640bcb'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/docs/implementation/epic-5-context.md'
warnings: ['oversized']
deferred:
  - summary: >-
      Stories 5.4 and 5.10 merged into feat/epic-5-after-0.5.0 while this story was built, and a
      Story 5.16 was added, so CLAUDE.md's Repository state, the story count (42/15), the Fixed
      timings block (no defaultProfileRetries or defaultProfileRetryDelay; the Profile timeout's
      "records unresolved" note) and deferred-work.md's Profile-retry entry describe the baseline,
      not the branch this merges into.
    evidence: |-
      Intent and blind reviewers saw 9324497 (Story 5.4 done), 59638d6 (Story 5.10 done) and a
      "Story 5.16" heading on the epic branch. The orchestrator's brief forbids describing 5.4 or
      5.10 as done in this pass and schedules a final pass after they merge; the spine's AD-3 and
      AD-4 also changed there, so expect a merge overlap with this story's AD-3 edits.
    location: >-
      CLAUDE.md "How the build runs" and "Repository state"; docs/config.example.toml Fixed
      timings; docs/implementation/deferred-work.md spec-1-9 Profile retry entry; spine AD-3
    severity: medium
---

<intent-contract>

## Intent

**Problem:** CLAUDE.md, the architecture spine, ADR-0005, the PRD's Board wording,
`docs/install.md` and `deferred-work.md` disagree with the code in places a fresh session reads
first: a stale "live 0.8.2" checkpoint, a wrong `version` output, Epic 4 described as in progress,
AD-3 saying nothing persists the epoch, AD-1 and the PRD describing a flat Needs you and Finished
top zone, a JSON rule saying absent values are omitted, undocumented engine tunables, shell blocks
that install after a failed checksum, and resolved deferred entries that read as open.

**Approach:** Edit each document to state what the code at the baseline does, mark every closed
`deferred-work.md` entry `resolved:` with where, add the retrospectives' low-severity items as new
entries with reasons, and record the retrospectives' process lessons in CLAUDE.md, where every
session reads them. Only one code file changes, `TestPaneMoved`'s comment.

## Boundaries & Constraints

**Always:** Every claim checked against the code or a command run at the baseline. Prose plain,
wrapped near 100 columns, PRD §3 vocabulary. `docs/install.md` keeps every string
`TestInstallGuideMatchesUnits` looks for; `config.example.toml` stays a valid config whose keys
the config tests find documented; the spine keeps its "Go toolchain for releases"
row (`release_test.go`). Superseded PRD text keeps its dated amendments; only the glossary and the
§4.1 description are rewritten, with a note.

**Never:** Describe Story 5.4 (Profile retry) or 5.10 (reused pane) as done or document their
tunables; mark their deferred entries resolved. Rewrite a `docs/research/` record (append a dated
correction only, and none is needed here). Touch the live herdr, the running herddash, or
`sprint-status.yaml`. Change behaviour, add a test or a dependency.

</intent-contract>

## Code Map

- `CLAUDE.md:34-54` -- "27 stories", "starting with Story 1.1", checkpoint "never the live 0.8.2
  server", ADR-0004 to 0007 only. `:173-229` Build and test: arch_test sentence (5.5 deferral:
  make it a list), `version` paragraph (wrong: `go run` prints `herddash dev (unknown)`, a plain
  `go build` prints `herddash dev (<12-hex>[-dirty])`; release ldflags are `-s -w -X ... -X ...`,
  `scripts/release-build.sh:82`). `:231-255` Repository state: "Epic 4 in progress", no procenv,
  no x/sys, no `go 1.26.0`, no Epic 5 additions.
- Epic 5 additions at baseline: ADR-0011 + `arch_test.go` + `core.ErrInvalidHerdrOrder` (5.5);
  `board-browser`, `macos`, `release-toolchain` CI jobs, `HERDDASH_REQUIRE_TOOLS` (5.6, 5.7);
  ADR-0012 keepalive, Board silence bound, `engine_contract_test.go` + `testdata/replay-board.mjs`
  (5.11); `defaultSnapshotFailuresBeforeLost`, unexpected lifecycle line -> resnapshot (5.12);
  bidi cleaning and `<bdi>`, `UnpositionedGroup` (5.13); recap from `away_summary` and a manual
  `/recap`'s `local_command` record, local-command and bash-mode records are not turns (5.14,
  5.15, ADR-0010 amendment); `stopped` closes a background task, `/clear` restarts the tail (5.2,
  5.3, ADR-0009 amendment, three `docs/research/claude-code-*-2026-10-23.md` records).
- `ARCHITECTURE-SPINE.md:68` (herdr rules missing), `:76-89` AD-1 zones (stale: zone 0 is
  Needs you alone, zone 1 every other class by workspace; `internal/core/view.go:248-275`),
  `:108` "OS hostname" (code: up to the first dot, `internal/config/socket.go`), `:139-152` AD-3
  hazard 3 "Nothing persists the evidence yet" (persisted: `server_state`,
  `internal/store/migrations/0002_server_state.sql`), no moved-pane rule, `:169-173` AD-5 (no
  ADR-0011), `:314` JSON row (AgentView strings are always sent, `""` when unknown; omitted are
  `waiting_since`, `last_snapshot_at`, `herdr_version`, `gate`, `previous`, `herdr_order`,
  `observed_by`, `detail`; owners: `engine.View`, core's `AgentView`/`AgentKey`/`SortKey`/
  `ConnectionTruth`, httpapi's SSE, history and error shapes), `:411-432` Deferred (drop the
  columns seed and the "specific values" item).
- `docs/adr/0005-append-only-transition-record.md` -- add a dated amendment: `server_state`.
- `docs/prd.md:112` glossary Board, `:152-155` §4.1 description, `:686` §9 -- flat top zone.
- `docs/planning/epics.md:218` -- Story 1.2 "the OS hostname".
- `internal/engine/tunables.go` -- the values to document; `internal/httpapi/events.go`
  keepalive 15 s.
- `docs/config.example.toml` -- gets a closing comment block of fixed values (no `key =` lines).
- `docs/install.md:23-24` stale sentence, `:49-51` checksum not chained, `:160-161`,
  `:291-292`, `:337-338` launchd waits, `:328` empty `$DB`.
- `internal/engine/engine_test.go:519-520` -- `TestPaneMoved` "Today a move is ...".
- `docs/implementation/deferred-work.md` -- entries to resolve: 1.1 version (5.8), 1.2 umask/0600
  and database parent (Story 2.1: `store.go:78-198`, `TestFileModes`, `TestOpenRefusesLooseParent`),
  1.3 plain `herdr` vs 0.8.2 (obsolete, live herdr 0.9.5 since 2026-10-20), 1.5 rejection loop
  (Story 1.7, `88fb175`, `TestResyncPersistentRejection`), 1.5 moved pane (decided, AD-3), 1.6
  lost herdr (Story 3.9's strip and 5.12), 1.7 tunables (5.8), 1.9 CLAUDE.md (5.8), 2.1 epoch
  persistence (Story 2.2, `b72f192`), 2.2 daemon_start epoch (Story 2.3, `store.ReasonConnect`,
  `engine.go:1189`), 3.7 non-200 and focus (Story 3.8, `6198ea8`), 3.8 Board lost the Daemon
  (Story 3.9, `6fce275`, `LOST_TEXT`), 4.1 CLAUDE.md 0.8.2 (5.8). Stays open: 1.9 Profile retry
  (5.4), the 5.2 and 5.3 unverified entries.
- Retrospective items to add: Epic 1 L8, L9, untested Umask; Epic 2 F-2, F-4, F-5, F-6, F-7,
  F-8; Epic 3 D7, D10, A3, A4; Epic 4 R5, S1, R8. Spec 5.13's recap bidi and 5.6's no required
  checks are in their spec frontmatter only; add both to the ledger.

## Tasks & Acceptance

**Execution:**
- `CLAUDE.md` -- story count and order; checkpoints with Story 1.3 closed; ADR range; arch_test
  rules as a list; version paragraph exact; `-s -w`; Repository state with Epics 1 to 4 done,
  Epic 5 in progress and its merged additions, procenv, x/sys v0.49.0, `go 1.26.0`; a
  "Lessons from the retrospectives" list.
- `ARCHITECTURE-SPINE.md` -- line 68, AD-1 zones, AD-3 host/persistence/moved pane, AD-5,
  JSON row, Deferred; `updated:` date.
- `docs/adr/0005-...md` -- amendment 2026-10-23.
- `docs/prd.md`, `docs/planning/epics.md` -- Board wording and short hostname.
- `docs/config.example.toml`, `docs/how-herddash-works.md` -- fixed timings block; a pointer.
- `docs/install.md` -- `&&` after the checksum, `[ -n "$DB" ]` guard, `pgrep -x herddash`
  waits after `bootout`, drop the stale sentence.
- `internal/engine/engine_test.go` -- `TestPaneMoved` comment.
- `docs/implementation/deferred-work.md` -- resolutions and new entries.

**Acceptance Criteria:**
- Given CLAUDE.md, when read, then `version` output matches `go run` and a plain `go build`, the
  Story 1.3 checkpoint is closed with no "live 0.8.2", x/sys and `go 1.26.0` are named, and Epic
  5's merged additions are described without 5.4 or 5.10.
- Given the spine, when read, then tunables are not in Deferred, AD-3 names `server_state` and the
  moved-pane rule, Host is the short hostname, AD-1 matches `core.SortKey`, and the JSON row
  matches the tags; ADR-0005 agrees.
- Given `docs/install.md`'s blocks pasted whole, then a failed checksum installs nothing, an empty
  `$DB` stops before copying, and launchd blocks wait for the process to exit.
- Given `deferred-work.md`, then every closed entry has `resolved:` with where, and every
  retrospective low item routed to it is present with its reason.
- Given the repository, then `go build ./...`, `go vet ./...`, `go test ./...` pass and gofmt
  prints nothing.

## Spec Change Log

## Review Triage Log

### 2026-10-23 — Review pass
- verdicts: 23 findings — high 0, medium 2, low 19, false 2, maybe-false 0
- findings:
  - `[medium]` `[defer]` (intent) Diff measured at b095da6; 5.4 and 5.10 have since merged on the epic branch (reading B) — the orchestrator's brief forbids describing them as done here and schedules a final pass; recorded in frontmatter `deferred`.
  - `[low]` `[patch]` (intent) Nothing keeps the Fixed timings list following the code (reading C) — added comments in `tunables.go`, `serve.go`, `events.go` and `order.mjs` pointing back to the list; a test was outside the spec ("Never: add a test").
  - `[low]` `[patch]` (edge) Restore block fails silently when `DB` or `BACKUP` is missing — now `if … else echo "not restored: …" >&2`.
  - `[low]` `[patch]` (edge) Backup block fails silently on an empty `DB` — now `if … else echo "no backup taken: …" >&2`.
  - `[low]` `[patch]` (edge) A database path with `&`, `<`, `>` is JSON-escaped by `herddash config`, so the `sed` extraction yields a wrong path — verified (`json.MarshalIndent` escapes HTML characters); the guide now says to set `DB` by hand for such a path.
  - `[low]` `[patch]` (edge) `pgrep -x herddash` waits on another user's or another of the operator's herddash processes forever — scoped to `-u "$(id -u)"` and the prose names a `-dev` build, `serve` in a terminal and `import-plugin-log` (two such processes were running on this machine when checked).
  - `[low]` `[patch]` (edge) Upgrade block lacked the hand-started-herddash warning — the first wait's note now says every launchd block below waits the same way.
  - `[low]` `[patch]` (edge) Plain "Stop it" block did not wait for the process — wait line added.
  - `[medium]` `[defer]` (blind) Out of date with the integration branch (5.4, 5.10) — same root as the first intent row; same frontmatter entry.
  - `[low]` `[defer]` (blind) Story counts 42/15 wrong on the epic branch (Story 5.16 added there) — true only after merging the epic branch; same frontmatter entry, final pass.
  - `[low]` `[patch]` (blind) Fixed timings omitted the Board's reconnect backoff, request-header timeout, stream write timeout and shutdown timeout — added under "HTTP and the Board".
  - `[low]` `[patch]` (blind) No back-pointers from the constants to the list — same fix as the intent C row.
  - `[low]` `[patch]` (blind) Spine JSON row said `waiting_since` was both omitted and always carried, and missed `heading` and a Transition's `profile` — reworded and both added.
  - `[low]` `[reject]` (blind) JSON row omits `engine.View`'s `run_id`, `seq`, `headers` — they are always sent and need no omission rule; the row names `engine.View` as their owner.
  - `[low]` `[patch]` (blind) pgrep too broad / unbounded — same as the edge pgrep row.
  - `[low]` `[patch]` (blind) Backup silent on empty `DB`; sed JSON escapes — same as the edge rows.
  - `[low]` `[patch]` (blind) Backfilled `resolved:` lines undated, and two cite line ranges — dated from each story's commit, line ranges replaced by function names, each marked "marked by Story 5.8".
  - `[false]` `[reject]` (blind) Moved-pane resolution says "no ADR since AD-3 is unchanged" while adding an AD-3 paragraph — the identity rule is unchanged, the paragraph only states it; reworded to say so, and the decision now cites `epics.md`'s Epic 5 preamble, an in-repo record. Same-workspace moves: AD-3 now says "whatever the new id".
  - `[low]` `[reject]` (blind) Deferred list deletes two items but strikes others — the acceptance criteria ask for those two to be dropped; the struck items record work done. The backup half of the rotation bullet is split into its own struck bullet (patched). The default port is documented in `config.example.toml`'s `bind` line.
  - `[low]` `[reject]` (blind) CLAUDE.md's "Epic 5 in progress" and "so far" go stale after release, and dependency versions copy `go.mod` — Repository state is a dated status section updated by each epic, as before; versions match `go.mod` and the spine's Stack table at this commit.
  - `[low]` `[patch]` (blind) The deferral lesson read two ways — reworded: a deferral naming a story changes that story's criteria in the same commit; otherwise it goes to the ledger naming none.
  - `[low]` `[patch]` (blind) ADR-0005 amendment silent on stale `server_state` rows and on being after the fact — both stated.
  - `[false]` `[reject]` (blind) Verification cannot catch the install blocks — the blocks were extracted and checked with `bash -n` and `zsh -n`, and the checksum and empty-`DB` paths were run in a scratch directory (failed checksum installed nothing; empty `DB` copied nothing); recorded under Auto Run Result.
  - verification-gap layer: no gaps found.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test ./...` -- all pass
- `gofmt -l $(git ls-files '*.go')` -- prints nothing
- `grep -n "0.8.2" CLAUDE.md` -- no checkpoint line saying "live 0.8.2"

## Auto Run Result

Status: done

**Summary.** Documents agents read first now state what the code at `b095da6` does: CLAUDE.md
(build order, checkpoints, ADR range, `arch_test` rules as a list, exact `version` output and
`-s -w`, Repository state with Epics 1 to 4 done and Epic 5's merged stories, the Claude Code
features, a deferred-work convention and "Lessons from the retrospectives"); the spine (import
rules for herdr, AD-1's zone 0 / zone 1, short hostname, AD-3's `server_state` and moved-pane rule,
AD-5, the JSON row and its owners, Stack, Deployment, Deferred); ADR-0005's amendment; the PRD's
Board glossary and §4.1; Story 1.2's wording; the install guide's shell blocks; the fixed timings
beside the configuration; `TestPaneMoved`'s comment; `deferred-work.md`.

**Files.**
- `CLAUDE.md` -- as above.
- `docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md` -- as above.
- `docs/adr/0005-append-only-transition-record.md` -- 2026-10-23 amendment: `server_state` is engine state.
- `docs/prd.md` -- glossary Board, §4.1 description, §9 note: Needs you alone on top.
- `docs/planning/epics.md` -- Story 1.2: short hostname.
- `docs/config.example.toml` -- "Fixed timings (not configurable)" block.
- `docs/how-herddash-works.md` -- keepalive, silence bound, pointer to the timings.
- `docs/install.md` -- checksum `&&` chain, no inline comments in code, `DB` guards with messages, `pgrep -u` waits after every `bootout`, stale sentence dropped.
- `internal/engine/engine_test.go` -- `TestPaneMoved` comment.
- `internal/engine/tunables.go`, `cmd/herddash/serve.go`, `internal/httpapi/events.go`, `internal/httpapi/web/order.mjs` -- comments pointing to the timings list.
- `docs/implementation/deferred-work.md` -- 14 entries resolved with where; 16 added.
- `docs/implementation/epic-5-context.md` -- regenerated (planning had changed since it was compiled).

**Review.** 23 findings: 15 patch rows (all low), 3 deferred rows sharing one root (5.4, 5.10
and 5.16 on the epic branch; final pass), 5 rejected (2 false, 3 low by design). Patched counts
by verdict: high 0, medium 0, low 15.

**Follow-up review:** not recommended (no high or medium patched).

**Verification.** `go build ./...`, `go vet ./...`, `go test ./...` pass (Go 1.26.5); `gofmt -l .`
prints nothing; `node --test` in `internal/httpapi/web` 88 pass. Install blocks: all ```bash
blocks extracted and checked with `bash -n` and `zsh -n`; the checksum chain run in a scratch
directory with a wrong and a right hash (wrong: exit 1, nothing installed; right: installed), and
the empty-`DB` guard (nothing copied). `go run ./cmd/herddash version` printed
`herddash dev (unknown)`; a plain `go build` printed `herddash dev (<12 hex>-dirty)`.

**Residual risk.** The final pass after 5.4, 5.10 and 5.16 (frontmatter `deferred`). AD-3 was also
edited by 5.10 on the epic branch, so the merge needs a look there.

**Final pass (after 5.4, 5.10, 5.16, 5.17 and 5.18).** CLAUDE.md counts 45 stories (18 in Epic
5), lists the five in "Repository state" and says the store has migrations 0001 to 0004, with 0.5.0
refusing a schema-4 database. `docs/config.example.toml`'s "Fixed timings" gains the Profile
retries (3, first after 2 s, doubling) and the hold limit (4 s, 2 x the Profile timeout), and its
Profile timeout note no longer says running out always means `unresolved`; every value checked
against `internal/engine/tunables.go`, and `defaultSnapshotFailuresBeforeLost` was already listed.
`docs/how-herddash-works.md` says what a retry and a hold do. The spine's AD-3 adds 5.17's
lost-save residual (its released-pane paragraph and quit-and-restart residual were already there
from 5.17), AD-10 lists the migrations with 0002 and 0004 as engine state only, and AD-14 gains
the 5.4 and 5.18 retry and hold rules. `deferred-work.md` marks the Story 1.9 retry entry resolved
by 5.4, adds 5.4's documentation deferral (resolved here) and 5.16's resume-across-`/clear`
question (open); 5.10, 5.17 and 5.18 deferred nothing.

