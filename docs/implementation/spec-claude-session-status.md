---
title: 'Needs you from Claude Code''s session record (ADR-0008)'
type: 'feature'
created: '2026-10-21'
status: 'in-progress'
baseline_commit: '4e9e44c'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/adr/0008-claude-code-session-record-as-status-authority.md'
  - '{project-root}/docs/adr/0005-append-only-transition-record.md'
  - '{project-root}/docs/implementation/spec-background-tasks.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** On 2026-10-21 the Agent in `6: DNS` waited on a Claude Code question dialog (the
side-by-side form with a preview) and the Board showed it Finished: herdr's screen detection
reported `done`, while Claude Code's own `<config dir>/sessions/<pid>.json` said
`status: "waiting"` from the same second.

**Approach:** Build ADR-0008 (accepted by the operator 2026-10-21) exactly: read only `pid`,
`status` and `statusUpdatedAt` from that record every 2 s for each `claude` Agent with a known pid;
while it says `waiting`, the Agent's Status is `blocked`, recorded as a live Transition with a new
`observed_by = 'claude_session'` column; otherwise herdr's Status stands.

## Boundaries & Constraints

**Always:**
- **Reader (`internal/procenv` or a new small adapter allowed by the spine; check
  `arch_test.go` and keep to the permitted graph — if a new package would be needed, put the file
  read in `internal/procenv` as `ClaudeSession(configDir string, pid int)`):** reads exactly
  `<configDir>/sessions/<pid>.json` (never any other file there; never a `.key` file), size-bounded
  (e.g. 64 KiB), decoding only `pid` (must equal), `status` (string), `statusUpdatedAt` (int ms).
  Missing file, bad JSON, mismatched pid: an error, never a guess.
- **Config dir:** the Agent process's `CLAUDE_CONFIG_DIR` (read through the existing environment
  lookup on the pid Profile resolution found), `~/.claude` (the Daemon's home) when unset. `~/`
  expanded as the Profile rules do.
- **Core (AD-1):** a pure function deciding the effective Status from herdr's Status and the
  session status: `waiting` → `blocked`; anything else (or unreadable) → herdr's Status. Only
  for Agent Program `claude`.
- **Engine:** per `claude` pane with a known pid, a session watch like the background watch
  (off the actor, timeout, busy flag so a hung read is not joined, generation tag, stopped on
  removal / program change / epoch change / shutdown; tunables `sessionInterval` 2 s,
  `sessionTimeout`). The pane tracks herdr's latest Status separately from the effective one.
  When the effective Status changes because of the session record, record a Transition through
  the normal path with provenance `live` and `observed_by = claude_session`; when `waiting` ends,
  record the return to herdr's current Status with `observed_by = claude_session`. A herdr status
  event while `waiting` holds updates herdr's Status only (no Transition unless the effective
  Status changes). `observed_at` = `statusUpdatedAt` if not in the future and not before the
  Agent's previous Transition, else the read time.
- **Store:** migration `0003` adds `observed_by TEXT CHECK (observed_by IS NULL OR observed_by IN
  ('claude_session'))` with `ALTER TABLE transitions ADD COLUMN`; no table rebuild. Core
  `Transition` gains `ObservedBy` (empty = herdr). Read paths return it; the history API includes
  `observed_by` (omitted or null when herdr). Existing rows unchanged.
- **Docs:** PRD FR-2/FR-6 area gets a dated amendment citing ADR-0008; the spine notes the new
  source in its adapter table; `docs/research/herdr-socket-api.md` "Other observations" records
  the missed dialog as a herdr detection gap (layout not recognised), with the times above.

**Never:**
- Open any file in the sessions directory but `<pid>.json`; log, store or show any field but
  `status` (and `statusUpdatedAt` as a time); map `busy`/`idle`/`shell` to anything.
- Change herdr's Status for non-`claude` Agents, or rebuild the transitions table.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Missed question | herdr `done`, record `waiting` since T | Transition `blocked` at T, `observed_by=claude_session`; view needs_you | test fails |
| Question answered | record leaves `waiting`, herdr `working` | Transition `working`, `observed_by=claude_session` | test fails |
| herdr also blocked | herdr `blocked`, record `waiting` | no extra Transition | test fails |
| herdr moves during waiting | herdr `done`→`idle` while `waiting` | no Transition; on end, return to `idle` | test fails |
| Record missing / bad / pid mismatch | any | herdr Status alone, debug log once | test fails |
| Non-claude | codex, record `waiting` | ignored, not read | test fails |
| statusUpdatedAt in future / before last | T bad | observed_at = read time | test fails |
| Migration | existing DB at 0002 | 0003 applies, old rows `observed_by` NULL | test fails |
| Never `.key` | sessions dir holds `<pid>.<hash>.key` | not opened (reader opens one path only) | test fails |

</frozen-after-approval>

## Code Map

- `docs/adr/0008-…` (the decision), `internal/procenv` (reader), `internal/core` (effective Status, `Transition.ObservedBy`), `internal/engine` (`engine.go` watch, `tunables.go`, Profile resolution's pid and environment lookup), `internal/store` (`migrations/0003_*.sql`, insert/select), `internal/httpapi` (history JSON), `arch_test.go` (permitted graph), docs listed above.

## Tasks & Acceptance

**Execution:**
- [x] reader + tests (temp dir fixtures incl. a `.key` file present)
- [x] core effective-Status + Transition field + tests
- [x] store migration + tests
- [x] engine watch + tests with an injected reader
- [x] history API field + test
- [x] docs

**Acceptance Criteria:**
- Given the module, when `go build ./... && go vet ./... && go test -race ./...`, `gofmt -l .` and `node --test internal/httpapi/web/*.test.mjs` run, then all pass and every matrix row has a test.
- Given a dev build installed locally and a Claude Code session showing a question dialog, then its Box is Needs you within ~2 s.

## Implementation Notes

- **Reader.** `procenv.ClaudeSession(configDir, pid)` in `internal/procenv/session.go` (no new
  package, so `arch_test.go` is unchanged). It opens exactly `<configDir>/sessions/<pid>.json`,
  requires an absolute config dir and a regular file, reads at most 64 KiB, and decodes only `pid`,
  `status` and `statusUpdatedAt` (pid and status required; a missing `statusUpdatedAt` is 0, which
  core treats as unknown). Every failure wraps `procenv.ErrSession` and names the path and the
  kind of failure, never the decoding error, so no record content reaches a log. The `.key` test
  makes the sessions directory search-only (not listable) and every `.key` file mode 0: the record
  still reads, so only the one path was opened. A fixture pins the shape observed on 2026-10-21
  (fields named in ADR-0008 only, values invented).
- **Core.** `core/session.go`: `Observer` (`""` herdr, `claude_session`), `ReadsSession`
  (claude only), `EffectiveStatus`, `SessionObservedAt` (statusUpdatedAt when > 0, not after the
  read time and not before the previous Transition), and `ClaudeConfigDir` (`~`/`~/` expanded
  against the Daemon's home, `<home>/.claude` when unset or empty, relative paths refused).
  `TransitionInput.ObservedBy`/`Transition.ObservedBy()`; `NewTransition` rejects an unknown
  observer and a non-herdr observer on anything but a live Transition (`ErrInvalidObserver`).
- **Store.** `0003_observed_by.sql` is one `ALTER TABLE ... ADD COLUMN` with the CHECK. Append and
  History carry the column; `AppendBackfill` also refuses a backfill row with an observer. The
  upgrade test checks `transitions`' root page is unchanged (no rebuild) and old rows read NULL.
- **Engine.** `Options.Session` (default `procenv.ClaudeSession`; every engine test harness
  injects a fake or a temp-dir reader). The pane keeps `herdr` (latest valid herdr Status) apart
  from `status` (effective); every herdr path (status event, snapshot, agent release) goes through
  `herdrSays`, which updates `herdr` and returns `core.EffectiveStatus`, so a herdr change while
  `waiting` holds records nothing. The session watch mirrors the background watch (generation
  tag, own context, busy flag, timeout, debug log once per pane) but reads at once and then every
  `sessionInterval` (2 s) whatever the Status; `sessionTimeout` is 1 s. The config dir is read from
  the process's `CLAUDE_CONFIG_DIR` through `Environ` once per process and kept for the watch.
  `followSession` runs on report, process change and program change; it first drops the old
  verdict and, if that verdict had made the Status `blocked`, records the return to herdr's
  Status (observed by `claude_session`, read time). Removal (including an epoch increment) stops
  the watch without recording; shutdown ends it through the run context. `lastAt` (the previous
  Transition's `observed_at`, from the record at first sight) feeds `SessionObservedAt`.
  Only the record's `status` is logged (`claude session`, debug, on change).
- **History API.** `wireTransition.ObservedBy` (`observed_by`, omitted for herdr), so the SSE
  `transition` event carries it too.
- **Docs.** PRD FR-6 amendment (with FR-2/FR-3 references), the spine's procenv adapter row, and
  the socket API record's "Other observations" entry for the missed dialog (times from ADR-0008).
- **Judgment calls / known gaps.** A Daemon restart while an Agent is `waiting` was a known gap
  (first sight followed herdr, then the first read re-blocked); review closed it, see the Review
  Triage Log. A Status core does not know never becomes herdr's Status. The acceptance criterion's live check (a dev build
  showing a real question dialog) was not run here.

## Spec Change Log

- **2026-10-21, matrix row "statusUpdatedAt in future / before last".** The frozen row says
  observed_at is the read time in both cases. "Before last" now clamps to the Agent's previous
  Transition instead (`core.SessionObservedAt`: `max(statusUpdatedAt, previous)`), because Claude
  Code stamps `waiting` a few milliseconds before herdr reports the same moment, and discarding
  that stamp lost the true start. ADR-0008's Decision was amended to say so (commit `dbb1b78`);
  `TestSessionObservedAtBad` "before the last transition" expects the clamp. A stamp in the future,
  or none, is still the read time.
- **2026-10-21, failure rule.** ADR-0008's "Failure is quiet and safe" was amended: a passing
  failure keeps the verdict; only a missing record, a pid mismatch, or three failures in a row end
  it (Review Triage Log, item 1).

## Review Triage Log

Review of the session-status watch, 2026-10-21. All eight findings accepted and fixed; each fix
has a test that fails with it reverted (checked by reverting each fix alone).

1. **A transient read failure cleared `waiting`** (one timeout or a record caught mid-write ended
   a wait, recording a return and a reblock). Now `onSession` keeps the verdict on a passing
   failure and ends it only on a lasting one (`fs.ErrNotExist`, or the new
   `procenv.ErrSessionOtherProcess` for a pid mismatch) or `sessionFailLimit` (3) failures in a
   row. Tests: `TestSessionTransientFailureKeepsWaiting`, `TestSessionThreeFailuresClear`,
   `TestSessionLastingFailureClears`, `procenv.TestClaudeSessionLastingKinds`.
2. **Daemon restart while waiting wrote herdr's Status first.** `deferSight`: for a `claude`
   Agent whose stored history ends `blocked` observed by `claude_session` (and herdr does not
   itself say blocked), the first sight waits for the first session read (bounded by
   `sessionTimeout`; any failure falls back to herdr). Still waiting: unchanged since the record,
   nothing written, Waiting Time continuous from the stored start. History is read once
   (`historyOnce`). Tests: `TestSessionRestartWhileWaiting`, `TestSessionRestartAfterWaiting`.
3. **Agent release while waiting left the record at Needs you.** The release stops the session
   watch first, then records FinalStatus observed by herdr (with no valid FinalStatus, the
   session's Status is settled back instead). Test: `TestSessionReleasedWhileWaiting`.
4. **Program change while waiting wrote two rows; a new pid wrote a return and reblock.**
   `onStatus` and `apply` set herdr's Status from the event before `setProgram`, so the settle
   returns straight to it and the herdr path finds nothing to record. `followSession(p, keep)`:
   a re-resolution to another process of the same program keeps the verdict until the new
   process's first read decides it. Tests: `TestSessionProgramChangedOneRow`,
   `TestSessionNewProcessStillWaiting`.
5. **No valid herdr Status when waiting ends left it stuck blocked.** `settleSession` falls back
   to `preSession` (the Status before the record made it blocked, or the stored row's previous
   after a deferred first sight), then `unknown`. Test: `TestSessionWaitingEndsHerdrUnknown`.
6. **The reader followed symlinks and would block on a FIFO.** `os.Lstat` first (regular file
   only), open with `O_NONBLOCK|O_NOFOLLOW` on unix (`session_unix.go`; `session_other.go` adds
   none), then `f.Stat` again: regular and `os.SameFile` with the Lstat. Tests:
   `TestClaudeSessionFIFO`, `TestClaudeSessionSymlink` (`session_unix_test.go`).
7. **Migration 0003's CHECK does not tie `observed_by` to provenance `live`.** Not edited (applied
   to the operator's live database). Comments in `core.NewTransition` and `store/record.go`, and a
   sentence in ADR-0008, say the rule is enforced in code.
8. **Spec drift.** Spec Change Log entries above. Also: the pre-existing flake in
   `TestSessionObservedAtBad` "before the last transition" (a snapshot read in flight saw the
   event's Status first, recording it as snapshot provenance; it failed on the unchanged engine
   too) is fixed by settling before the event.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- all pass
- `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs` -- all pass
- `test -z "$(gofmt -l .)"` -- exit 0
