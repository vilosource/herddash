---
title: 'Story 1.2: Configuration, data directory and socket discovery'
type: 'feature'
created: '2026-10-16'
status: 'done'
baseline_commit: '08acd8778ad83c943a34048b7a186f088a6b85a9'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-1-context.md'
  - '{project-root}/docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md'
  - '{project-root}/docs/adr/0004-runtime-dependencies.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** herddash has no configuration. Later stories need to know where herdr's socket is,
what this Host and herdr server are called, where data lives, and how each Agent Program's Profile
is resolved (FR-11, FR-15, FR-17, AD-3, AD-8).

**Approach:** `internal/config` parses one TOML file into one validated, immutable struct, with
command-line flags beating the file and the file beating defaults. It resolves the socket path,
Server and Host in one function. It creates the data directory owner-only. A new
`herddash config` subcommand prints the effective configuration as JSON.

## Boundaries & Constraints

**Always:**
- **Dependency:** `github.com/BurntSushi/toml` v1.6.0 is the only new dependency (ADR-0004).
  `internal/config` imports `internal/core` only.
- **File keys:**

  | Key | Default |
  |---|---|
  | `host` | short OS hostname, before the first dot |
  | `session` | — |
  | `socket_path` | — |
  | `server` | — |
  | `bind` | `127.0.0.1:7318` |
  | `data_dir` | `$XDG_DATA_HOME/herddash`, else `~/.local/share/herddash` |
  | `database` | `<data_dir>/herddash.db` |
  | `retention_days` | 90; must be ≥ 1 |
  | `allow_unsupported_herdr` | false |

  The config file defaults to `~/.config/herddash/config.toml`, and a missing default file is not
  an error. An explicit `--config` path that is missing is an error.
- **Profile Map:** `[profiles.<agent program>]` with `rule = "env"`, `variable`, `absent`, and a
  `[profiles.<agent program>.values]` table mapping a variable value to a Profile name. Values and
  names must be non-empty, and a leading `~/` in a value is expanded to the home directory. `env`
  is the only rule type for now.
- **Flags:** `--config`, `--host`, `--session`, `--socket`, `--server`, `--bind`, `--data-dir`,
  `--database`, `--retention-days`, `--allow-unsupported-herdr`.
- **Socket path resolution**, in order:
  1. `--socket` or `socket_path`
  2. `--session` or `session`, which gives `~/.config/herdr/sessions/<name>/herdr.sock`
  3. `HERDR_SOCKET_PATH`
  4. `HERDR_SESSION`, as in step 2
  5. `~/.config/herdr/herdr.sock`
- **Server** is `default` for `~/.config/herdr/herdr.sock` and `<name>` for
  `sessions/<name>/herdr.sock`. Any other path needs `server` set, or it is an error naming
  `server`. An explicit `server` always wins.
- **Validation:** unknown keys and invalid values are errors naming the key, reported before
  anything else happens.
- **Non-loopback bind is refused:** its error says authentication does not exist yet (FR-17).
- **Data directory:** `cmd/herddash` sets umask `077` at process start, and `config.EnsureDataDir`
  creates the data directory with mode `0700`.
- **Example config:** `docs/config.example.toml` documents every key and ships the Claude Code
  Profile Map: `claude`, rule `env` on `CLAUDE_CONFIG_DIR`, absent case `claude`, with commented
  example values. A test parses it.

**Never:**
- Read pane environments, dial the socket, or open the database. Those belong to later stories.
- Reload configuration while running. Log environment values.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| No file | default path absent | defaults, as in Always | none |
| Flag beats file | file `bind=127.0.0.1:9000`, flag `--bind 127.0.0.1:9001` | bind 127.0.0.1:9001 | none |
| Unknown key | `colour = "red"` | error naming `colour` | exit 1 |
| Bad value | `retention_days = 0` | error naming `retention_days` | exit 1 |
| Non-loopback | `bind = "0.0.0.0:7318"` | error naming `bind` | exit 1 |
| Default socket | no socket settings, no env | `~/.config/herdr/herdr.sock`, Server `default` | none |
| Session | `HERDR_SESSION=work` | `…/sessions/work/herdr.sock`, Server `work` | none |
| Env path | `HERDR_SOCKET_PATH=…/sessions/ops/herdr.sock` | Server `ops` | none |
| Odd path | `socket_path=/tmp/x.sock`, no `server` | error naming `server` | exit 1 |
| Precedence | `--session a` and `HERDR_SOCKET_PATH` set | session `a` wins | none |
| Profile Map | `values = {"~/.claude-cldw" = "cldw"}` | value expanded to `<home>/.claude-cldw` | none |
| Bad profile | `rule = "magic"` or empty `absent` | error naming the profile key | exit 1 |
| Data dir | fresh path | directory created `0700` | error if creation fails |
| Example file | `docs/config.example.toml` | parses with no error | test fails otherwise |

</frozen-after-approval>

## Code Map

- `cmd/herddash/main.go` -- `run(args, stdout, stderr)` dispatches subcommands; add `config`, and set umask in `main` only (keep `run` testable)
- `internal/config/doc.go` -- package doc exists; keep it
- `internal/core/types.go` -- `AgentProgram` type to key the Profile Map
- `arch_test.go` -- enforces imports; config → core only, third-party allowed outside core

## Tasks & Acceptance

**Execution:**
- [x] `go.mod`, `go.sum` -- add `github.com/BurntSushi/toml v1.6.0` -- ADR-0004
- [x] `internal/config/config.go` -- `Config` struct, `Load(Flags, Env)` with injected environment and home directory, defaults, precedence, validation and typed errors naming keys -- AD-8
- [x] `internal/config/socket.go` -- socket path and Server resolution, plus Host -- AD-3
- [x] `internal/config/datadir.go` -- `EnsureDataDir` creating `0700` -- FR-17
- [x] `internal/config/config_test.go` -- table tests for every matrix row using temporary files and an injected environment -- no real home directory touched
- [x] `cmd/herddash/main.go`, `main_test.go` -- `herddash config [flags]` prints the effective Config as indented JSON, or the error on stderr with exit 1; `syscall.Umask(0o077)` in `main` -- operator can check config
- [x] `docs/config.example.toml` -- documented example as in Always -- shipped example

**Acceptance Criteria:**
- Given `docs/config.example.toml` copied to a temporary config path, when `herddash config --config <path>` runs, then it exits 0 and prints JSON containing the `claude` profile with absent case `claude`.
- Given the module, when `go test -race ./...` runs, then `arch_test.go` still passes with config importing only core internally.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `go run ./cmd/herddash config --config docs/config.example.toml` -- expected: JSON, exit 0

## Implementation Notes

- Default Host on this machine resolves to `localhost` because its hostname is `localhost.localdomain`; the operator should set `host` before dogfooding.
- An existing data directory that group or others can access is refused, not tightened.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | VG | Six flags never passed through `run` | medium | VG pre-verified: no test passes them; patch |
| 2 | VG, BH | MarshalJSON positional literal; four JSON fields unasserted | medium | VG pre-verified: swapping data_dir/database passes; patch with keyed literal |
| 3 | VG | umask in `main` untested | low | No file depends on it yet; defer to Story 2.1, which creates the database |
| 4 | BH, ECH | Values losing on precedence never validated | medium | `resolveTarget` validates only the winning session; spec says invalid values error naming the key; patch |
| 5 | BH | `herddash config` hides an overridden `session` | low | Real, but showing every source is a larger design; rejected |
| 6 | BH | `config_file` omitted when no file was read | low | Operator can infer the default; rejected |
| 7 | BH, ECH | `--config` not ~-expanded; empty `--config=` silently defaults | low | Confirmed in readFile; direct fix; patch |
| 8 | BH, ECH | Hostname or home lookup failure aborts even when unused | low | Both are always available under launchd and systemd user units; fix adds lazy plumbing; rejected |
| 9 | BH | Errors print HERDR_SOCKET_PATH / HERDR_SESSION values | false | The no-logging rule covers Agents' process environments (secrets); herdr socket paths are not secret |
| 10 | BH, ECH | EnsureDataDir ignores owner | low | Confirmed; one-field guard; patch |
| 11 | BH, ECH | `localhost` case-sensitive; resolves via resolver | low | Case confirmed; patch with EqualFold; resolver risk needs a hostile /etc/hosts, rejected |
| 12 | BH | Profile program typos silent | low | herdr's full program list is not known; rejected |
| 13 | BH, ECH | Test assertions that cannot fail | medium | Every error starts with `config:`; patch |
| 14 | BH | `server` error lacks the socket's source | low | Direct fix; patch |
| 15 | BH | Example file claims all keys have defaults | low | False for session/socket_path/server; patch plus key-coverage test |
| 16 | BH | Help text hardcodes defaults | low | Direct fix from constants; patch |
| 17 | BH, ECH | Bare `~` not expanded | low | Confirmed; patch |
| 18 | BH | Example tested twice; no key-coverage test | low | Patch adds key-coverage test |
| 19 | BH | Diff omits go.sum and spec | false | Deliberate: the spec reaches the edge-case layer as claims_file |
| 20 | ECH | Dangling default config symlink loads defaults | low | Direct Lstat check; patch |
| 21 | ECH | database outside data_dir gets no owner-only parent | medium | Real for FR-17, but the store creates and opens the database; defer to Story 2.1 |
| 22 | ECH | retention_days unbounded, later overflow | low | Direct cap; patch |
| 23 | ECH | `unresolved` accepted as a Profile name | medium | Would make a configured Profile indistinguishable from the sentinel; patch |
| 24 | ECH | Program key whitespace; variable with `=`/space/NUL | low | Direct validation; patch |
| 25 | ECH | Literal profile values not cleaned | medium | `~/` values cleaned, literal not, so `/opt/x/` never matches; patch |
| 26 | ECH | Symlinked home breaks server derivation | low | macOS homes are not symlinked; rejected |
| 27 | ECH | Socket path over darwin sun_path limit | low | Direct guard; patch |
| 28 | ECH | Names allow Unicode whitespace and `?#%` | low | Would corrupt API path segments; patch |
| 29 | ECH | MkdirAll parents depend on umask | low | `main` sets umask 077; rejected |
| 30 | ECH | Tests use real hostname | low | CI runners have valid names; rejected |
| 31 | ECH | EnsureDataDir has no caller | false | By design: the spec says serve calls it in a later story |
