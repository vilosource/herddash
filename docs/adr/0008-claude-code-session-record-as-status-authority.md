# ADR-0008: Claude Code's session record as a second authority for Needs you

**Status: Accepted** by the operator on 2026-10-21. Split out of the first draft of this ADR,
whose transcript part is now [ADR-0009](0009-background-work-from-claude-code-transcripts.md)
(Proposed).

## Context

On 2026-10-21 the Claude Code Agent in tab `6: DNS` (pane `wB:pE`, Profile `cldw`) asked the
operator a question with Claude Code's question dialog, in its side-by-side form with a preview
panel. The Board showed it Finished; the operator noticed only by looking at the pane.

- herdr reported that pane `blocked` three times earlier the same afternoon (14:52:01, 14:59:28,
  15:00:59), for Claude Code's ordinary prompts. For this dialog it reported `done` at 15:13:15
  and `idle` at 15:17:15. herdr derives a Claude Code Agent's Status by screen detection, and it
  evidently does not recognise this dialog's layout. That is a herdr defect, to be reported
  upstream; herddash cannot fix herdr's detection.
- Claude Code itself keeps a small record per running process, `<config dir>/sessions/<pid>.json`,
  holding among other fields `pid`, `sessionId`, `cwd`, `status` and `statusUpdatedAt`. For that
  pane's process it read `status: "waiting"` with `statusUpdatedAt` 15:13:15, the same moment herdr
  said `done`, and still read `waiting` at 15:32. Values seen across this machine's twelve live
  records: `busy`, `idle`, `waiting`, `shell`.
- The record holds no conversation. The same directory also holds `<pid>.<hash>.key` files, which
  are secrets.
- Needs you is what herddash exists to show (ADR-0003, FR-3). Missing it is the worst failure the
  Board can have.

## Decision

- **Source.** For each `claude` Agent whose process is known (the pid Profile resolution already
  reads), the Daemon reads `<config dir>/sessions/<pid>.json`, where the config dir is the
  process's `CLAUDE_CONFIG_DIR` (read as Profile resolution reads environment), or `~/.claude`
  when it is unset. It decodes only `pid`, `status` and `statusUpdatedAt`, ignores every other
  field, and requires `pid` to match. It never opens a `.key` file or any other file in that
  directory.
- **Only `waiting` is used.** While the record says `waiting`, the Agent's Status is `blocked`,
  whatever herdr reports. Otherwise herdr's Status stands, unchanged. herdr remains the authority
  for every other Status; the record's other values are not mapped.
- **Recorded, with its observer.** A Status change the record causes is a real Transition in the
  record (ADR-0005): `blocked` when `waiting` begins, and back to herdr's current Status when it
  ends. Its provenance is `live`. A new nullable column `observed_by` says who observed it:
  `claude_session` for these, NULL for herdr as before. Migration 0003 adds the column with
  `ALTER TABLE … ADD COLUMN` and a CHECK, so no table is rebuilt and existing rows are untouched.
  Its `observed_at` is the record's `statusUpdatedAt` when that is not in the future, raised to
  the Agent's previous Transition when it is earlier (Claude Code stamps a few milliseconds
  before herdr reports the same moment), else the time it was read.
- **Polling.** Every 2 s per `claude` Agent with a known pid; a read is a small local file read,
  off the actor, bounded by a timeout, skipped while the previous one runs.
- **Failure is quiet and safe.** A missing or unreadable record, a `pid` mismatch, or a shape that
  no longer decodes means herdr's Status alone, logged once at debug level. It never guesses.
- **Surfaces.** The Board needs no change: the Agent is Needs you. The history API includes
  `observed_by`.

## Consequences

- A Claude Code question that herdr's screen detection misses still reaches Needs you, within
  about 2 s, with its true start time.
- herddash depends on one Claude Code internal file and three of its fields. A Claude Code
  release can change or remove it; herddash then falls back to herdr alone, as before this ADR.
  A fixture pins the shape observed on 2026-10-21.
- The stored record gains one nullable column. Old Daemons cannot open a migrated database
  (as with every migration); new ones read old rows as observed by herdr.
- Only Claude Code is covered.
- **Unverified:** that `waiting` is set for every Claude Code prompt that needs the operator
  (permission prompts, plan approval) and for nothing else. It was observed for the question
  dialog only. Dogfooding must watch for both a miss and a false Needs you.

## Alternatives considered and rejected

- **Display only.** Show NEEDS YOU on the Box without recording a Transition. No schema change,
  but the record, Waiting Time and history would say Finished while the operator was being asked.
- **A new provenance value.** `provenance` is CHECK-constrained; changing it means rebuilding the
  append-only table. The observation is live; who observed it is a separate fact.
- **Waiting for a herdr fix.** Right to report, but the Board would keep missing questions until
  then and after any future layout change.
- **Reading the transcript.** Would also work, but carries the conversation; that is ADR-0009's
  separate decision.
