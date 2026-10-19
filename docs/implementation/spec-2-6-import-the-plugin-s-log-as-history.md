---
title: "Story 2.6: Import the plugin's log as history"
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: 'ffab32cac9891bb8054a8833474082845994f256'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-2-context.md'
  - '{project-root}/docs/implementation/spec-2-2-record-every-transition.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The only history that exists before herddash is the agent-webhook plugin's
`hook.log`: 3,157 entries since 2026-09-10. herddash cannot load it (FR-9a, AD-13).

**Approach:** Add `herddash import-plugin-log <file> --herdr-0.9.2-since <YYYY-MM-DD>`. It runs
with no Daemon. It parses the log into `backfill` Transitions under epoch 0 and writes them in
one transaction, idempotently, through the store.

## Boundaries & Constraints

**Always:**
- **Format.** This was observed 2026-10-19 and is documented in the importer's package doc and
  tests. Each line is `YYYY-MM-DD HH:MM:SS <message>`, in local time with no zone. There are two
  message forms, and any other line is counted as unparsed and skipped:
  - `posted <status> for <pane_id> -> <http code>`: no Agent Program, no Profile.
  - `no url configured; would have sent {json}`: the JSON carries `agent`, `status` and
    `pane_id`, and optionally `profile` (a string or null), `workspace_id` and `title`.
- **Row.** Each kept line becomes a `core.NewTransition` with:
  - Host and Server from the same `config.Load` serve uses;
  - epoch 0, the log's `pane_id`, and the Agent Program from `agent` (NULL if absent);
  - the Profile from `profile` (NULL if absent or null);
  - previous Status NULL, new Status the logged status, `state_change_seq` NULL and herdr
    version NULL;
  - provenance `backfill`, and `observed_at` = the line's time in `time.Local`, as UTC ms;
  - an `import_key`: `plugin-log:` + hex SHA-256 of the raw line + `#` + the line's occurrence
    index among identical lines. It is stable across re-runs and across a log that has grown.
- **Possibly spurious.** `done` entries whose time is before local midnight of the given date are
  flagged possibly spurious. On this machine the live server still answers ping `0.8.2`, so the
  operator passes today's date and every `done` entry is flagged.
  - `core.TransitionInput` gains `PossiblySpurious bool`, with an accessor. `NewTransition`
    rejects it unless provenance is `backfill`.
  - `store.Append` writes it.
- **Retention.** Entries older than `now − retention_days` are skipped and counted.
- **Writing.** `store` gains `AppendBackfill(ctx, []core.Transition) (added int, err error)`.
  - It runs in one transaction with `INSERT … ON CONFLICT(import_key) DO NOTHING`, so a re-run
    adds nothing.
  - Only the importer calls it. Add it to the call-site guard as importer and store only.
- **Lock.** It opens the store with `store.Open`. A running Daemon makes it fail at once with
  `ErrLocked`, naming the lock: exit 1, `herddash: <err>`.
- **Command.**
  - The `--herdr-0.9.2-since` flag is required and must be a valid date; otherwise exit 2.
  - It accepts the same config flags as serve.
  - It prints one summary line:
    `imported <added> of <parsed> entries (<duplicates> already present, <old> older than retention, <spurious> flagged possibly spurious, <unparsed> unparsed)`.
- **Tests.** Fixtures under `internal/importer/testdata/` are trimmed from the real log, with
  Profiles anonymised (`cldw`→`work`, `cldp`→`personal`, `cldo`→`other`) and pane ids kept.

**Never:**
- Write epoch ≠ 0, previous Status, `state_change_seq` or herdr version for a backfill row.
- Run while a Daemon holds the lock.
- Modify the plugin or its log.
- Commit the real log.
- Let imported rows become a live Agent or a live fold. Epoch 0 is never live, and a test proves
  it.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| JSON line | `... would have sent {"agent":"claude","profile":"work","status":"blocked","pane_id":"wA:p3",...}` | row: claude, work, blocked, epoch 0, backfill, previous NULL | none |
| No profile key | early JSON without `profile` | profile NULL | none |
| Null profile | `"profile": null` | profile NULL | none |
| Posted form | `posted done for w1:pC -> 204` | row: program NULL, done | none |
| Spurious | done before the since-date | `possibly_spurious` 1; blocked never flagged | none |
| Old | an entry older than retention | skipped, counted | none |
| Garbage | an unparseable line | skipped, counted unparsed | none |
| Re-run | import the same file twice | second run: 0 added, all duplicates | none |
| Grown log | lines appended, then re-import | only the new lines added | none |
| Identical lines | two identical lines | two rows (occurrence index differs) | none |
| Daemon running | lock held | exit 1 naming the lock, nothing written | error |
| Bad flag | missing or invalid `--herdr-0.9.2-since` | exit 2 | error |
| Host/Server | config with host `mac` | rows' host and server equal what `config.Load` resolves | none |
| Not live | serve started after an import | no Agent from epoch-0 rows; History of a live key excludes them | none |

</frozen-after-approval>

## Code Map

- `internal/importer/` -- `importer.go` (`Parse(r io.Reader, opts) (Result, []core.Transition)` with the format, keys and flags), package doc with the format, tests and `testdata/`. Importer imports core, store and config.
- `internal/core/types.go` -- `PossiblySpurious` on the input and on Transition, with its accessor and validation; `types_test.go`.
- `internal/store/record.go` -- `Append` writes `possibly_spurious`; `AppendBackfill`; `History` reads it back; tests.
- `cmd/herddash/main.go` -- the `import-plugin-log` subcommand, reusing the config flags (`configFlags`); new `import.go` and `import_test.go`.
- `callsite_test.go` -- guard `AppendBackfill`.

## Tasks & Acceptance

**Execution:**
- [x] `internal/core` -- the PossiblySpurious field
- [x] `internal/store` -- writing the flag, AppendBackfill, tests
- [x] `internal/importer` -- the parser, keys, testdata, tests
- [x] `cmd/herddash` -- the command, flags, summary, lock refusal, tests
- [x] `callsite_test.go` -- the guard

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes and earlier tests still pass.

## Implementation Notes

- **Parse signature.** `Parse(r io.Reader, opts Options) (Result, []core.Transition, error)`: the
  error is only a failure to read `r`. `Options` holds Host, Server, `Since` (the instant),
  `Cutoff` (UTC ms) and an optional `Location` (nil is `time.Local`; tests use a fixed zone).
  `Import(ctx, *store.Store, r, config.Config, since, now)` derives the cutoff from
  `retention_days` with the engine sweep's arithmetic and calls `AppendBackfill`; that is the
  importer's use of `config` and `store`. `ParseDate` reads the flag as local midnight.
- **Counts.** `parsed` is every understood entry, old ones included, so `added + already present
  + older than retention = parsed`. `spurious` counts flagged entries among those kept (not old),
  on every run, re-runs included. A line whose JSON lacks `status` or `pane_id`, whose status is
  not one of the five, or which has trailing data after the JSON, is unparsed. Blank lines are
  unparsed. The posted form's HTTP code is not checked.
- **Import key** hashes Host + `\x00` + Server + `\x00` + the line (no newline, a trailing `\r`
  dropped), so an import under a wrong `--host` or socket does not block a corrected one. A line
  over 1 MiB is read to its end, discarded and counted unparsed. Occurrence indexes count every line, unparsed and old ones too, so they stay stable as
  entries age out.
- **AppendBackfill** returns `(added, flagged int, err error)`, one more value than the spec's
  signature, so the summary counts only flagged rows among those added (a re-run reports 0). It
  refuses (writing nothing) any row that is not backfill, not epoch 0, has no import key, or
  carries a previous Status, herdr order or herdr version, enforcing the Never list at the store; `core` still
  allows them on backfill input.
- **Call-site guard.** `AppendBackfill` is allowed in importer and store only. The checker used to
  skip every file in a directory allowed both to write and to sweep (engine, store); it now skips a
  directory only when it may call every guarded method, so engine files are read and an engine
  `AppendBackfill` is caught (fixture `testdata/callsite/internal/engine/backfill.go`).
- **Command.** Flags may come before or after the file; after `--` every argument is a file. A
  log that is missing or not a regular file exits 1 before the data directory is created. The
  import's store work runs under `context.WithoutCancel`, so a signal cannot leave a rollback
  running after Close. Usage errors (missing or invalid
  `--herdr-0.9.2-since`, not exactly one file, an unknown flag) exit 2 before touching the data
  directory. The log is opened before the store; the store before anything is written, so a
  running serve fails it with `ErrLocked` naming the lock.
- **"Every done entry is flagged."** Only done entries *before* local midnight of the given date
  are flagged, as the rule says. Run on 2026-10-19 with `--herdr-0.9.2-since 2026-10-19`, the real
  log (3,175 lines) gave 2,661 flagged of 2,782 done entries: the other 121 were logged on
  2026-10-19 itself.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH | `AppendBackfill` is a second write path against AD-13's "one insert" | medium | It shares the single INSERT statement; resolved by amending spine AD-13 in this change to name the batch form |
| 2 | BH | Host and Server are not in the import key, so a mis-keyed first import cannot be redone | medium | Every later run reports all rows present; patch: key includes host and server |
| 3 | BH | `AppendBackfill` does not refuse epoch ≠ 0 or an empty import key | low | Direct; patch |
| 4 | VG, BH | The rollback test fails at `BeginTx`, never mid-transaction | medium | Patch: a trigger-forced failure of a later insert |
| 5 | ECH | Ctrl-C during the import leaves a background rollback after Close | low | The SQLITE_BUSY gotcha; direct; patch |
| 6 | ECH, BH | A line longer than 1 MiB aborts the whole import | low | Contradicts "other lines are skipped"; patch |
| 7 | BH | `TestParseCRLF` never feeds `\r\n` | low | Patch |
| 8 | VG | Local-midnight reading of the since-date is unpinned | low | Patch: a `ParseDate(valid, nil)` assertion |
| 9 | BH | Passing today's date leaves today's `done` entries unflagged; nothing says to pass a later date | medium | The rule matches the epic's AC; patch the usage text and docs |
| 10 | BH | Re-run summary repeats the spurious count | low | Patch: count among added rows |
| 11 | ECH, BH | `--` handling re-parses later arguments as flags | low | Patch |
| 12 | ECH, BH | A directory log path creates the data directory first; no missing-file test | low | Patch |
| 13 | BH | Help text column broken; flags, time zone and date rule undocumented | low | Patch |
| 14 | ECH, BH | DST repeated hour reads ambiguously | low | One hour a year in a local-time log the plugin wrote; rejected (documented with the time zone note) |
| 15 | ECH | `Parse` with empty Host/Server counts every line unparsed | low | Only `Import` calls it, with config values; rejected |
| 16 | BH | Lock refusal names the lock, not the Daemon | low | `ErrLocked`'s message asks whether serve or import is running; rejected |
| 17 | BH | Spec numbers disagree; manual check outdated | low | Non-frozen Verification updated below; the log grows daily |
| 18 | VG | Retention cutoff arithmetic duplicated in importer and engine | low | Two one-line computations; rejected |
| 19 | BH | No vfkb decision for this story | low | Added (acf0eabd7f2f) |
| 20 | BH | Spec and tracker statuses disagree | false | Workflow ordering |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass

**Manual checks:**
- With a throwaway `--data-dir` in the scratchpad, import the real log with `--herdr-0.9.2-since` set to a date after its last entry: every line parses (the log grows daily; 3,175 on 2026-10-19), every done entry is flagged, and a second run adds 0.
