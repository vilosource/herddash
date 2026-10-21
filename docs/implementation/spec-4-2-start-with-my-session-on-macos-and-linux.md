---
title: 'Story 4.2: Start with my session on macOS and Linux'
type: 'feature'
created: '2026-10-20'
status: 'done'
baseline_commit: 'c3fd7d073aaf938f77450d5483ca4efb5f1d2a49'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** herddash runs only while the operator keeps a terminal open for it, so the Board is
missing whenever they forget to start it (FR-15).

**Approach:** Ship a launchd agent for macOS and a systemd user unit for Linux that start
`herddash serve` as the operator's own user with their session, restart it on failure without a
tight loop, and set no `HERDR_SOCKET_PATH`. Document installation, removal, logs and backup step
by step for both, without root. Nothing in the repository installs anything.

## Boundaries & Constraints

**Always:**
- **Files:** `deploy/launchd/io.github.vilosource.herddash.plist` and
  `deploy/systemd/herddash.service`, plus `docs/install.md`. The repository only ships them; the
  operator, or the provisioning repository, installs them (CLAUDE.md: nothing here configures a
  machine).
- **Both units** run `<binary> serve` with no other arguments, so configuration comes from
  `~/.config/herddash/config.toml` exactly as when run by hand. They set no `HERDR_SOCKET_PATH`
  and no `HERDR_SESSION`: the Daemon finds the socket by herdr's resolution order, and the config
  file can pin `socket_path` or `session`. The binary path is `~/.local/bin/herddash`.
- **Restart policy:** restart only on a non-zero exit, never after a clean exit (a SIGTERM at
  logout exits 0, Story 4.1), and never in a tight loop.
  - launchd: `RunAtLoad` true, `KeepAlive` with `SuccessfulExit` false, `ThrottleInterval` 30.
  - systemd: `Restart=on-failure`, `RestartSec=30`, a start limit that stops a crash loop.
- **Paths:** launchd does not expand `~` or variables, so the plist uses a single placeholder,
  `__HOME__`, that the install steps replace with the absolute home directory (one `sed` line);
  systemd uses `%h`. Standard output, which repeats what the log records, goes nowhere
  (`/dev/null`, `StandardOutput=null`). Standard error goes to the data directory under launchd
  (`__HOME__/.local/share/herddash/herddash.stderr.log`) and to the journal under systemd. The
  JSON log stays `herddash.log` in the data directory.
- **`docs/install.md`**, step by step, without root and for both platforms: build or download the
  binary into `~/.local/bin`; write the config (pointing at `docs/config.example.toml` and
  `herddash config` to check it); install, start, check, stop and remove the unit (`launchctl
  bootstrap`/`bootout gui/$(id -u)`; `systemctl --user daemon-reload`, `enable --now`, `disable
  --now`, and `loginctl enable-linger` only as an option to run without a login); where the logs
  are; how to open the Board; and backup: stop the Daemon, copy `herddash.db`, start it again.
  It says plainly never to touch herdr's configuration or plugin registry, and that herddash is
  never installed as a herdr plugin.
- **Tests** (Go, module root, next to `arch_test.go`): parse the plist (`encoding/xml`) and the
  unit file and assert every rule above: the program and `serve` argument, `RunAtLoad`,
  `KeepAlive`/`SuccessfulExit` false, `ThrottleInterval` ≥ 10, no `HERDR_*` variables, the output
  paths; and for systemd `ExecStart`, `Restart=on-failure`, `RestartSec` ≥ 10, a start limit, no
  `HERDR_*`, `StandardOutput=null`. Where `plutil` exists (macOS), `plutil -lint` the plist with
  `__HOME__` substituted; where `systemd-analyze` exists, verify the unit.

**Never:**
- Install, load or start anything on this machine from a test or a script in the repository.
- Touch `~/.config/herdr/`, herdr's plugin registry, or the operator's live herdr.
- Add a dependency, an `install` subcommand, or root-level (system) units.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Login | unit installed and enabled | herddash serve starts as the operator | none |
| Clean stop | logout or SIGTERM, exit 0 | not restarted | none |
| Crash | exit non-zero | restarted after 30 s, not sooner | none |
| Crash loop | config error, exit 1 every start | throttled (launchd) or start-limited (systemd), never tight | none |
| Socket | no `HERDR_SOCKET_PATH` in the unit | default herdr socket, or the config's | none |
| Placeholder | plist with `__HOME__` replaced | `plutil -lint` passes | test skips without plutil |

</frozen-after-approval>

## Code Map

- `internal/config/socket.go` -- socket resolution order (flag/config, session, `HERDR_SOCKET_PATH`, `HERDR_SESSION`, `<home>/.config/herdr/herdr.sock`); no change
- `internal/config/config.go` -- data directory `$XDG_DATA_HOME/herddash` else `~/.local/share/herddash`; `DefaultConfigFile` `~/.config/herddash/config.toml`
- `cmd/herddash/serve.go` -- exit codes: 0 on a signal, 1 on config, data directory, database and listen errors (Story 4.1 proves the clean path)
- `docs/config.example.toml` -- the documented config to point at
- `arch_test.go` -- module-root test package `herddash_test`; a new root test file joins it (its import rules apply to non-test files only)
- `README.md` -- add one line pointing at `docs/install.md`

## Tasks & Acceptance

**Execution:**
- [x] `deploy/launchd/io.github.vilosource.herddash.plist` -- the launchd agent template
- [x] `deploy/systemd/herddash.service` -- the systemd user unit
- [x] `docs/install.md`, `README.md` -- install, removal, logs, Board, backup, both platforms
- [x] `deploy_test.go` (module root) -- the rule checks, plus `plutil`/`systemd-analyze` where present

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` runs, then the unit checks pass (and `plutil -lint` on macOS).
- Given `docs/install.md`, when followed on macOS, then herddash runs under launchd and the Board is at `http://127.0.0.1:7318/` (checked by the operator while dogfooding; never loaded by a test).

## Implementation Notes

- Both units add one thing the spec did not list: a `077` umask (`Umask` 63 in the plist,
  `UMask=0077` in the unit), matching AD-9, so the launchd-created stderr log is owner-only too.
  The Daemon already sets its own umask. The plist also sets `ProcessType` `Background`.
- Review fix: the systemd unit also carries `UnsetEnvironment=HERDR_SOCKET_PATH HERDR_SESSION`,
  because a user manager can import them from a herdr pane and the service would inherit them.
  launchd has no equivalent, so `docs/install.md` checks `launchctl getenv` instead. The test also
  requires `RestartSec x (StartLimitBurst - 1) < StartLimitIntervalSec`, parses systemd time spans,
  and ties the guide's label, unit name, timings and stderr path to the unit files.
- systemd start limit: `StartLimitBurst=5` within `StartLimitIntervalSec=600`, in `[Unit]`.
- launchd does not create `StandardErrorPath`'s directory, and the Daemon refuses a data
  directory with group or other bits, so `docs/install.md` creates `~/.local/share/herddash`
  with `mkdir -m 700` before `bootstrap`.
- `deploy_test.go` parses the plist with a small `encoding/xml` decoder (dict, array, string,
  integer, bool) and the unit with a line parser. The `HERDR_` and `~`/`$` checks run over
  parsed keys and values, not raw bytes, so the explanatory comments can name them.
- `systemd-analyze verify` needs `ExecStart`'s binary to exist, so the test writes a stand-in
  into a temporary home and substitutes `%h`; it runs in system mode (no `--user`), which needs
  no user manager. It skips on macOS. It was exercised by hand in an `ubuntu:24.04` container
  (systemd 255): `verify` exits 0. CI's Ubuntu runner will run it as a test.
- `docs/install.md` notes two service-environment traps: launchd passes no `XDG_DATA_HOME`, and
  `herddash config` run inside a herdr pane sees the pane's `HERDR_SOCKET_PATH`, which a service
  does not.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | ECH | A systemd user manager can carry `HERDR_SOCKET_PATH` (e.g. after `import-environment` from a herdr pane), which the service would inherit | medium | Contradicts "the units set no `HERDR_SOCKET_PATH`" in effect; patch: `UnsetEnvironment=HERDR_SOCKET_PATH HERDR_SESSION`, tested |
| 2 | ECH | launchd agents inherit anything set with `launchctl setenv` in the gui domain | low | Patch the guide: check `launchctl getenv HERDR_SOCKET_PATH` is empty |
| 3 | BH, ECH | Restore copies a backup over `herddash.db` with a newer `-wal`/`-shm` beside it | medium | SQLite would replay the stale WAL onto the older file; patch the guide: remove `-wal` and `-shm` before restoring, and copy them with a backup when present |
| 4 | BH, ECH | Backup and log paths are hard-coded, ignoring `data_dir`, `database` and Linux's `XDG_DATA_HOME` | medium | Patch the guide: take the paths from `herddash config` |
| 5 | BH, VG, ECH | `Umask`/`UMask`, `ProcessType` and `Type` are asserted by no test | medium | VG pre-verified: deleting `Umask` passes every test; patch the tests |
| 6 | BH, ECH | The start-limit check accepts values that never stop a crash loop | medium | Only positivity is checked; patch: assert `RestartSec × (StartLimitBurst − 1) < StartLimitIntervalSec` |
| 7 | BH | The `seconds` helper parses a count and rejects valid systemd time spans | low | Patch: parse the burst as a count and accept systemd spans |
| 8 | ECH | `ProgramArguments` as one string would pass the test | low | `fmt.Sprint` renders both alike; patch: compare elements |
| 9 | BH | Nothing ties the guide's label, unit name and timings to the units | low | Patch: a test that the guide names the values parsed from the units |
| 10 | BH, ECH | A persistent failure under launchd retries every 30 s forever and grows the stderr log | low | launchd has no start limit; patch the guide: say so and how to stop it |
| 11 | BH, ECH | No upgrade path for unit files; `bootstrap` fails when already loaded | low | Patch the guide: `bootout` before `bootstrap`, and re-copy plus `daemon-reload` on Linux |
| 12 | BH, ECH | The `sed` substitution breaks on `|` or `&` in `$HOME` | low | Patch the guide: a grep that the installed plist names the real binary path |
| 13 | ECH | `mkdir -p` keeps an existing directory's broader mode, so herddash refuses it | low | Patch the guide: `chmod 700` after `mkdir` |
| 14 | ECH | The backup may copy before the Daemon has exited; same-day backups overwrite | low | Patch the guide: wait until the job has no pid; timestamped names |
| 15 | BH | The guide's download-and-checksum path needs Story 4.3's binaries | low | Patch: mark it as available once release binaries are published (Story 4.3) |
| 16 | BH | README says the guide "installs it", and "Epics come next" is stale | low | Patch the wording |
| 17 | BH | No Linux end-to-end acceptance | low | `systemd-analyze verify` plus the guide; the operator's Linux Hosts confirm it when deployed; rejected |
| 18 | ECH | `systemd-analyze verify` may fail without a running systemd | false | It passed in an `ubuntu:24.04` container with no systemd running (Implementation Notes) |
| 19 | BH | Spec/tracker status mismatch | false | Workflow ordering |
| 20 | VG | `plutil -lint` never runs in CI (Linux only) | low | The strict Go decoder covers most of it; a macOS runner is a CI change; defer |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `sed "s|__HOME__|$HOME|g" deploy/launchd/io.github.vilosource.herddash.plist | plutil -lint -` -- expected: OK (macOS)
