# ADR-0004: Runtime dependencies: a pure-Go SQLite driver, a TOML parser and x/sys

**Status: Accepted.** Decided by the operator 2026-10-15 during the architecture run.

## Context

herddash ships as one static binary per platform with no runtime on the target (PRD FR-15). Nothing
may need a C toolchain at build time, so cross-compiling to macOS and Linux on two architectures
stays one `go build` each.

The standard library lacks three things herddash needs. It needs a SQLite database for the record
(PRD §4.2), and a configuration file the operator edits by hand, whose largest part is the Profile
Map (FR-11). Profile resolution also reads another process's environment (FR-10). On Linux that is
`/proc/<pid>/environ`, which the standard library can read. On macOS it is the `kern.procargs2`
sysctl, which needs a raw sysctl call the standard library does not expose.

Versions were checked on 2026-10-15 against proxy.golang.org.

## Decision

- **`modernc.org/sqlite`**, v1.60.1. A pure-Go translation of SQLite with no cgo. It requires Go
  1.26 or newer, which sets herddash's own minimum.
- **`modernc.org/libc`**, pinned to exactly the version the driver's `go.mod` requires, v1.77.1 for
  driver v1.60.1. The driver's documentation requires this pairing. The two are upgraded together
  and never separately, so a blanket `go get -u` is not used in this repository.
- **`github.com/BurntSushi/toml`**, v1.6.0. The configuration file is TOML.
- **`golang.org/x/sys`**, v0.49.0, for the darwin process-environment reader only.

No other runtime dependency is admitted without a further ADR. HTTP, server-sent events, embedding
and logging use the standard library.

## Consequences

- `CGO_ENABLED=0` builds work for every target.
- The configuration format matches herdr's own `config.toml`, and it allows comments, which the
  Profile Map needs.
- Upgrading the SQLite driver is a deliberate change. Bump the driver, set `libc` to the version
  its `go.mod` names, and run the tests. A version bump of any dependency is not itself an ADR
  unless it changes the on-disk format.
- The SQLite driver is large and slow to compile. herddash writes a handful of Transitions a
  minute, so runtime performance is not a concern.

## Alternatives considered and rejected

- **`mattn/go-sqlite3`.** Needs cgo.
- **`ncruces/go-sqlite3`.** Pure Go by way of WebAssembly, with a smaller user base and a wasm
  runtime inside the binary.
- **An append-only JSON-lines file instead of a database.** FR-14's history reads and FR-9's
  Retention sweep want indexed queries and deletes.
- **JSON configuration with the standard library.** No comments, and hostile to hand-editing.
- **Shelling out to `ps eww` on macOS for process environments.** It is fragile to parse, and it
  runs a process for every resolution.
