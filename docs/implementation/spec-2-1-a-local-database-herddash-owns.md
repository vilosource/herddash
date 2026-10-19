---
title: 'Story 2.1: A local database herddash owns'
type: 'feature'
created: '2026-10-19'
status: 'done'
baseline_commit: '0f38886fbd06ecb217d8846461ad5bfe5a28c6ce'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-2-context.md'
  - '{project-root}/docs/adr/0005-append-only-transition-record.md'
  - '{project-root}/docs/adr/0004-runtime-dependencies.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** herddash keeps nothing. Before anything can be recorded, there must be a single
owner-only database that only one process can write. Its schema has to be fixed by the record
decision, because changing the transitions table later needs an ADR (AD-9, AD-10, ADR-0005).

**Approach:** Build `internal/store`'s `Open` and `Close` on `modernc.org/sqlite`. It takes an
exclusive `flock`, turns on WAL, applies embedded numbered migrations in one transaction, and
refuses a schema newer than the binary. Make `herddash serve` open the store, so a second Daemon
on the same database fails at once.

## Boundaries & Constraints

**Always:**
- **Dependencies:**
  - `modernc.org/sqlite v1.60.1`, with `modernc.org/libc` pinned to exactly `v1.77.1` (ADR-0004).
  - Indirect modules only as the driver requires. No `go get -u`. The build stays `CGO_ENABLED=0`.
- **`store.Open(path string) (*Store, error)`:**
  - **Lock:** open or create `<path>.lock` and take `syscall.Flock(LOCK_EX|LOCK_NB)`. If the lock
    is held, return an error wrapping exported `ErrLocked` that names the lock path. The OS
    releases the lock when the process dies. `Close` releases it.
  - **Owner-only parent:** create the database's parent directory `0700` when missing. Refuse a
    parent writable by group or others. This also covers a `--database` outside `data_dir`.
  - **WAL:** turn it on and confirm it took.
  - **Migrations:** embed `internal/store/migrations/NNNN_name.sql` and apply every one above
    `PRAGMA user_version` in one transaction, which also sets `user_version`.
  - **Newer schema:** a `user_version` above the binary's highest migration is refused with
    exported `ErrSchemaNewer`, naming both versions, before anything is written.
- **Migration 0001:**
  - `transitions`:
    - `id INTEGER PRIMARY KEY` (the recording order);
    - `host`, `server`, `epoch`, `pane_id` (NOT NULL);
    - `agent_program`, `profile` (NULL when unknown);
    - `previous_status` (NULL), `new_status` (NOT NULL);
    - `state_change_seq INTEGER NULL` (herdr's per-server counter, Story 1.3);
    - `herdr_version` (NULL);
    - `provenance` NOT NULL, CHECK in (`live`, `snapshot`, `backfill`);
    - `possibly_spurious INTEGER NOT NULL DEFAULT 0` CHECK 0/1;
    - `observed_at INTEGER NOT NULL` (UTC ms);
    - `import_key TEXT NULL UNIQUE`;
    - an index on (host, server, epoch, pane_id, observed_at, id).
  - `resync_markers`:
    - `id`;
    - `host`, `server`, `epoch` (NOT NULL);
    - `observed_at`;
    - `reason` (NOT NULL);
    - an index on observed_at.
  - Unknown values are NULL, never a placeholder string.
- **File modes:** the database, WAL, SHM and lock files end up `0600`. That holds under the umask
  main sets, and `Open` also chmods the lock and database files it creates.
- **serve:** opens the store at `cfg.Database()` after the data directory and log are ready, and
  closes it on exit. A failure prints `herddash: <err>` and exits 1. Nothing is written to the
  store yet (Story 2.2).
- Store tests use `t.TempDir()` only. A second-process test re-executes the test binary to hold
  the lock from another process.

**Never:**
- Write Transitions or resync markers, or add read APIs (Stories 2.2 to 2.5).
- Make the store import anything internal but `core` (arch_test).
- Touch `~/.local/share/herddash` in tests.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Fresh | empty temp dir | db created, WAL on, `user_version` = 1, both tables with the listed columns | none |
| Reopen | Open, Close, Open | no migration re-run; same version | none |
| Second process | child holds the lock | Open fails at once with `ErrLocked` naming `<db>.lock` | error |
| Same process twice | Open twice without Close | the second fails with `ErrLocked` (flock is per open file) | error |
| Crashed holder | child killed while holding the lock | the next Open succeeds | none |
| Newer schema | `user_version` set to 99 | `ErrSchemaNewer` naming 99 and 1; nothing changed | error |
| Modes | after Open plus a write-free checkpoint, umask 077 | db, -wal, -shm and .lock are `0600`; a parent dir created by Open is `0700` | none |
| Loose parent | parent dir `0755` | Open refuses, naming the directory | error |
| Columns | PRAGMA table_info | exact column names, nullability and the `import_key` unique index as listed | none |
| serve twice | serve running; second serve on the same data dir | second exits 1 naming the lock | none |

</frozen-after-approval>

## Code Map

- `internal/store/store.go` (new) -- `Store`, `Open`, `Close`, `ErrLocked`, `ErrSchemaNewer`, the lock, the parent check, WAL, migrate.
- `internal/store/migrations/0001_initial.sql` (new) -- embedded through `embed.FS`.
- `internal/store/store_test.go` (new) -- matrix, using a re-exec helper for the second process (as `cmd/herddash/serve_test.go`'s `TestHelperSleep` does).
- `go.mod`, `go.sum` -- the driver and libc pins; the modules are in the local module cache.
- `cmd/herddash/serve.go`, `serve_test.go` -- open and close the store; the serve-twice test. `cmd → store` is permitted.
- `cmd/herddash/main.go` -- `syscall.Umask(0o077)` is already there. Do not change it.
- `docs/implementation/deferred-work.md` -- Story 1.2's two Story 2.1 entries (0600 files, owner-only parent) are what this closes. Leave the file as is.

## Tasks & Acceptance

**Execution:**
- [x] `go.mod`, `go.sum` -- dependencies at the pinned versions
- [x] `internal/store/*` -- Open/Close, migration 0001, tests for every matrix row
- [x] `cmd/herddash/serve.go`, `serve_test.go` -- open and close the store; the second serve fails

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then every matrix row passes and arch_test passes.
- Given `go list -m all`, then `modernc.org/sqlite v1.60.1` and `modernc.org/libc v1.77.1` appear exactly.

## Implementation Notes

- **Owner-only parent.** The Always list says "refuse a parent writable by group or others", but the
  matrix's Loose parent row refuses `0755`, which no one else can write. The parent check follows
  the matrix and `config.EnsureDataDir`: any group or other bit (`perm & 077`) is refused, and so
  is a directory owned by another uid. Store tests therefore put the database in a `0700`
  directory under `t.TempDir()`, which is itself `0755`.
- **WAL and SHM modes.** `Open` creates the database file `0600` before SQLite opens it; SQLite
  gives `-wal` and `-shm` the database file's mode, so they are `0600` even under umask `022`
  (tested under both `077` and `022`).
- **Newer schema before any write.** `user_version` is read before `journal_mode = WAL`, so a
  refused database is byte-identical afterwards and gets no `-wal` file.
- The DSN is a `file:` URI with the path escaped, so `?`, `#` or `%` in a `--database` path is a
  file-name character rather than driver parameters (tested).
- One connection (`SetMaxOpenConns(1)`): herddash is a single writer. Story 2.4/3.x may revisit
  this if httpapi reads need to run beside the engine's writes.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH | The epoch lives in memory, so after a Daemon restart new epoch-1 rows are indistinguishable from the previous run's | high | Real: Story 1.6 keeps the epoch in memory and no Epic 2 story persists it; it is not a schema change to `transitions` (a later migration can add a table), so it is not this story's; defer, to be built with Story 2.2's recording |
| 2 | ECH, BH | A foreign SQLite file at `user_version` 0 is switched to WAL and gets herddash's tables | medium | `prepare` treats version 0 as fresh; patch: refuse a non-empty unversioned database before WAL |
| 3 | ECH | A negative `user_version` panics on `ms[current:]` | low | Direct guard; patch |
| 4 | BH | Record invariants (epoch 0 ⇔ backfill, import_key only and always on backfill, `state_change_seq` only on snapshot) are not CHECKed | medium | Adding them later touches `transitions` and needs an ADR; patch in 0001 |
| 5 | BH | `resync_markers` cannot hold an error code or detail | low | Cheap now; patch: nullable `detail` |
| 6 | BH | No index serves the Retention sweep by age | medium | Adding it later touches `transitions`; patch |
| 7 | BH | Existing `-wal`/`-shm` files keep a loose mode | low | Direct chmod; patch |
| 8 | ECH, BH | A refused second serve writes into the running Daemon's log | low | Direct reorder; patch |
| 9 | ECH | Symlinked database paths give two lock files, or chmod an unrelated target | low | Requires deliberately symlinking the database; adds checks; rejected |
| 10 | ECH, BH | A refused newer database still gets its mode tightened and possibly `-wal`/`-shm` created | low | Owner-only tightening is harmless; nothing in the database changes; rejected |
| 11 | ECH | `TestFileModes` changes the process umask | false | The store package has no parallel tests, so no concurrent test observes it |
| 12 | BH | Migration rollback untested; numbering test hard-codes one migration | low | Test edit; patch |
| 13 | BH | Parent rule in the frozen text ("writable by group or others") differs from the matrix and code (owner-only) | false | The frozen matrix row refuses `0755`, so the frozen intent itself requires owner-only; the Always wording is the looser half of an internal contradiction, resolved toward FR-17 and Story 1.2's deferred item |
| 14 | BH | Spec and tracker statuses disagree | false | Workflow ordering |
| 15 | BH | Story 1.2's two deferred entries remain listed | low | Append-only file; rejected; noted in the PR |
| 16 | BH | Misleading connection comment; no `busy_timeout`/`synchronous` | low | Comment fix; patch. `synchronous` stays at its default, which favours durability of the record; one connection needs no busy timeout |
| 17 | VG, BH | The sqlite/libc pins are not checked automatically | medium | VG pre-verified; patch: `TestDependencyPins` |
| 18 | VG | serve is never run with `--database` | medium | VG pre-verified; patch |
| 19 | VG | Nothing checks serve closes the store | low | One assertion in `TestServeTwice`; patch |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=3 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...; done` -- expected: all pass
- `go list -m modernc.org/sqlite modernc.org/libc` -- expected: v1.60.1 and v1.77.1
