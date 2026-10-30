---
title: 'Story 7.2: The log is rotated'
type: 'feature'
created: '2026-10-30'
status: 'done'
baseline_commit: '574224bf990a0c700223d24f4cd9837a81508b07'
route: 'dispatch'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `serve` appends JSON lines to `herddash.log` in the data directory forever (`cmd/herddash/serve.go:117-131`); nothing bounds it (Epic 4 retrospective R8; 212 KB after 9 days on the operator's Mac, about 23 KB a day, more at debug level or in a reconnect storm).

**Approach:** The Daemon rotates its own log by size with the standard library: when the next line would take `herddash.log` past 10 MiB, `herddash.log.1` becomes `herddash.log.2` (replacing any older `.2`), `herddash.log` becomes `herddash.log.1`, and a new `herddash.log` is opened owner-only. At most two earlier files are kept, so the log uses at most about 30 MiB. `herddash.stderr.log` (launchd's) and the journal (systemd) are left as they are and documented.

## Boundaries & Constraints

**Always:** Every file, current and rotated, is mode 0600 (FR-17), whatever the umask or an existing file's mode. A line is never split across files: rotate only between writes (slog's JSON handler writes one record per `Write`). The writer is safe for concurrent use (the engine, httpapi and `http.Server.ErrorLog` share the handler). A rotation that fails (rename or open error) never loses the line or stops the Daemon: keep writing to the file already open and report the failure once to stderr, then retry at the next limit crossing. The size counts the existing file's bytes at start, so a restart does not reset it. A single line larger than the limit is written whole to a fresh file.

**Never:** No new dependency, package or import edge (the writer lives in `cmd/herddash`); no config key (limit and count are fixed, listed with the other tunables); no compression, no time-based rotation; no change to stderr or stdout handling, the plist or the unit; no change to what is logged.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Below limit | writes totalling < 10 MiB | one file, all lines | N/A |
| Crossing | next line would pass the limit | `.log` → `.log.1`, new `.log` gets the line | N/A |
| Third rotation | `.1` and `.2` exist | old `.2` removed, `.1` → `.2`, `.log` → `.1` | N/A |
| Restart near limit | existing `.log` of 9.99 MiB | first write that crosses rotates | N/A |
| Loose mode | existing `.log` or new file under umask 022 | 0600 | N/A |
| Huge line | one write > limit into a non-empty file | rotate, then the whole line in the new file | N/A |
| Rotation fails | rename or open errors | line written to the current file; one stderr report | retried at next crossing |
| Concurrent | writers in parallel under `-race` | no torn or lost lines | N/A |

</frozen-after-approval>

## Code Map

- `cmd/herddash/serve.go:23-24,117-131` -- `logFileName`; opens the log `O_APPEND` 0600 and `Chmod`s it, then `slog.NewJSONHandler(logFile, nil)`. Replace with the rotating writer, keeping the open/Chmod failure exits (exit 1, "herddash: open log: …"). Note the store is opened before the log on purpose (a refused second serve must not write into the running Daemon's log); keep that order.
- `cmd/herddash/logfile.go` (new) -- the writer: path, limit, keep count, current size, mutex; `Write`, `Close`. Make the limit a parameter so tests use a few hundred bytes; `serve` passes the 10 MiB constant.
- `cmd/herddash/logfile_test.go` (new) -- the matrix rows, with a small limit; rotation failure via a read-only directory or an unremovable `.log.1` path (e.g. a directory named `herddash.log.1`).
- `cmd/herddash/serve_test.go` -- existing tests read `logFileName`; they must keep passing unchanged.
- `docs/install.md:340-348` ("Logs" table) -- say the log is rotated at 10 MiB with two earlier files, `herddash.log.1` and `.2`; stderr.log and the journal are not rotated by herddash.
- `docs/config.example.toml` (tunables list at the end) -- add the log limit and count under a fitting heading.
- `docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md:542` -- Deferred "Log rotation": strike through as done in Story 7.2, as other done items are.
- `docs/implementation/deferred-work.md` -- R8 entry: add `resolved:` naming Story 7.2.
- `CLAUDE.md` -- repository state: `cmd/herddash` rotates `herddash.log`.

## Tasks & Acceptance

**Execution:**
- [x] `cmd/herddash/logfile.go` -- rotating writer per the constraints -- R8
- [x] `cmd/herddash/logfile_test.go` -- one test per matrix row, `-race` -- matrix
- [x] `cmd/herddash/serve.go` -- use it for the logger; close it on exit -- R8
- [x] `docs/install.md`, `docs/config.example.toml`, spine, `deferred-work.md`, `CLAUDE.md` -- documents match code -- R8

**Acceptance Criteria:**
- Given `serve` running with the real constant, when it starts, then it logs exactly as before (existing `serve_test.go` passes unchanged).
- Given R8 in `deferred-work.md`, when the story is done, then it has a `resolved:` line naming Story 7.2.

## Implementation Notes

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race ./... && test -z "$(gofmt -l .)"` -- all pass, gofmt prints nothing
- `go test -race -count=50 ./cmd/herddash -run 'Log'` -- pass (new concurrency test)

## Review Triage Log

Pass 1 (Blind Hunter BH, Edge Case Hunter ECH, Verification Gap VG):

| # | Finding | Verdict | Evidence | Route |
|---|---|---|---|---|
| 1 | ECH1, BH1, BH2, VG1: when the reopen fails after both renames, lines go to the file now named `.log.1`, no `herddash.log` exists, and the retry renames that active `.1` onto `.2`, deleting a kept generation early; the open-failure branch has no test | medium | `rotate` returns after `os.Rename(l.path, .1)` succeeded and `openLogFile` failed, with no undo; `TestRotatingLogRotationFails` fails only the first rename | patch: undo the last rename on open failure; test through an unexported open seam |
| 2 | ECH2: herddash.log removed or moved by hand while serving keeps writing to the old inode | low | same as before this story (one `O_APPEND` handle for the run) | reject (pre-existing; fix adds a periodic stat) |
| 3 | ECH3: external truncation leaves `size` counting high, so rotation comes early | low | harmless early rotation; pre-existing kind of external tampering | reject |
| 4 | ECH4: a Write after Close that crosses the limit opens a file no one closes | low | only `serve`'s deferred Close, at exit, precedes such a write; fix adds a closed flag | reject (unlikely, adds a guard) |
| 5 | ECH5, BH4: a kept `.1`/`.2` that cannot be chmodded now stops `serve` from starting | medium | `openRotatingLog` returns the Chmod error; before, only the current log could stop a start | patch: report on stderr and continue |
| 6 | ECH6, BH7: `keep >= 1` not enforced | low | the only caller passes `logKeep = 2` | reject (unreachable) |
| 7 | ECH7, BH3: a failure that persists reports once per 10 MiB, not once, and the log outgrows ~30 MiB | low | `Write` sets `next = size + limit` and reports each failed crossing; install.md says "once" | patch: docs and comment state the real behaviour |
| 8 | ECH8: chmod failure now prints "open log:" not "log:" | low | the spec names "herddash: open log: …"; nothing matches the old text | reject |
| 9 | VG2: `logMaxBytes`/`logKeep` not pinned against the docs | low | no repo precedent pins `tunables.go` against `config.example.toml`; no named consumer | reject |
| 10 | BH5: the umask half of the loose-mode test cannot fail | low | a 0600 create cannot be loosened by any umask | patch: drop that half and the "whatever the umask" wording |
| 11 | BH6: R8 is closed while `herddash.stderr.log` is still unrotated | low | R8's summary names both files | patch: resolved line names herddash.log only; new deferred entry for stderr.log |
| 12 | BH8: R8's own evidence implies ~41 KB/day, not 23 | low | fix edits the frozen spec | reject |
| 13 | BH9: no acceptance criterion states the rotation | low | fix edits the spec; the matrix carries it | reject |
| 14 | BH10: nothing shows `serve` uses the rotating writer | low | `serve_test.go` would pass with a plain file | patch: a serve test that a loose `herddash.log.1` is tightened to 0600 |
| 15 | BH11: one comment line in `openRotatingLog` is badly wrapped | low | `logfile.go` comment | patch |
