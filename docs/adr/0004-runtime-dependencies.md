# ADR-0004: Two runtime dependencies, a pure-Go SQLite driver and a TOML parser

**Status: Accepted.** Decided by the operator 2026-10-15 during the architecture run.

## Context

herddash ships as one static binary per platform with no runtime on the target (PRD FR-15). That
rules out anything needing a C toolchain at build time, because cross-compiling from one machine to
macOS and Linux on two architectures has to stay trivial.

It needs two things the Go standard library does not provide. It needs a SQLite database for the
record (PRD §4.2). It also needs a configuration file the operator edits by hand, whose largest
part is the Profile Map (FR-11).

Versions were checked on 2026-10-15 against proxy.golang.org.

## Decision

- **`modernc.org/sqlite`**, v1.60.1 at the time of writing. A pure-Go translation of SQLite, with
  no cgo.
- **`github.com/BurntSushi/toml`**, v1.6.0 at the time of writing. The configuration file is TOML.

No other runtime dependency is admitted without a further ADR. HTTP, server-sent events, embedding
and logging use the standard library.

## Consequences

- `CGO_ENABLED=0` builds work for every target, so the release matrix is one `go build` per
  OS and architecture pair.
- The configuration format matches herdr's own `config.toml`, so the operator writes both in the
  same idiom. It allows comments, which the Profile Map needs to stay readable.
- The SQLite driver is large and its build is slower than a cgo driver's. Performance on herddash's
  write rate (a handful of Transitions a minute) is not a concern.
- Both are pinned in `go.mod`. An upgrade is an ordinary dependency bump, not an ADR, unless it
  changes the on-disk format.

## Alternatives considered and rejected

- **`mattn/go-sqlite3`.** Needs cgo, which breaks trivial cross-compilation.
- **`ncruces/go-sqlite3`.** Pure Go by way of WebAssembly. It is close to upstream SQLite but has a
  smaller user base, and it carries a wasm runtime inside the binary.
- **No database, an append-only JSON-lines file.** Rejected because FR-14's history-per-Agent reads
  and FR-9's Retention sweep want indexed queries and deletes.
- **JSON configuration with the standard library.** No dependency, but no comments and a hostile
  hand-editing experience for the one file the operator maintains.
