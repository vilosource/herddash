# Epic 4 Context: Running unattended on all three machines

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Turn the dogfooded Daemon into something that runs by itself on each of the operator's three Hosts. It must stop cleanly when signalled, so a logout or restart never corrupts the record. It must start with the operator's session, under launchd on macOS and a systemd user unit on Linux. And each tagged release must publish ready-to-run binaries, so the second and third machines install herddash without building it. Epics 1 to 3 are done and merged: the herdr client, engine, store, read-only loopback API and the Board at `/` with its strips. The live herdr on the operator's Mac now runs 0.9.5. The held release-please PR #2, which proposes 0.1.0, is merged in this epic, and only with the operator's approval after a day of dogfooding.

## Stories

- Story 4.1: Clean shutdown
- Story 4.2: Start with my session on macOS and Linux
- Story 4.3: Release binaries for every Host

## Requirements & Constraints

- **Clean shutdown, on SIGINT and SIGTERM, in order:** stop accepting requests, close open SSE streams, let the engine finish its current write, close the database and release the lock. Then exit zero within a few seconds. A restart straight afterwards must start cleanly and write its `daemon_start` resync marker.
- **Service units:** a launchd agent and a systemd user unit start herddash as the operator's own user.
  - There is no root, no system service and no container.
  - The units set no `HERDR_SOCKET_PATH`. The Daemon finds the socket by herdr's own resolution order, and the config file can pin the path.
  - It must run as the same user as herdr, because the socket is `0600`.
  - The Daemon logs to `herddash.log` in the data directory.
  - It restarts on failure, but not in a tight loop.
- **Install docs:** installation and removal are documented step by step for both platforms, without root. They must never touch herdr's Ansible-owned configuration or its plugin registry, and must never install herddash as a herdr plugin. The first release also documents backup: a copy of the database taken while the Daemon is stopped. Deploying to the three machines belongs to the provisioning repository; nothing here installs or configures a machine.
- **Release binaries:** one static binary for each of darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64, built with `CGO_ENABLED=0` and needing no runtime on the Host. The version is stamped in, and the binaries are attached to the GitHub release with SHA-256 checksums.
- **Release PR gate:** before PR #2 is merged, check that its proposed version is 0.x (expected 0.1.0), because a published 1.0.0 cannot be withdrawn. The operator must also have confirmed a day of dogfooding and approved the merge. This is an open operator action item.
- **Tests and CI:** tests never touch a live herdr. `go build`, `go vet`, `go test -race` and `gofmt -l .` stay green.

## Technical Decisions

- **Lock (AD-9):** `store.Open` takes an exclusive advisory `flock`, which the OS releases if the process dies, so a crash never leaves a stale lock that blocks the next start under launchd or systemd. Umask `077` keeps the data directory `0700` and the database, WAL, SHM, lock and log files `0600`.
- **Writes and markers:** the single engine goroutine is the only writer (AD-2), so a clean shutdown means letting its in-flight `store.Append` finish. Each Daemon start writes a `daemon_start` marker (AD-4).
- **Signal handling:** `cmd/herddash` already ends a context on SIGINT and SIGTERM and then restores default handling, so a second Ctrl-C force-quits a shutdown that hangs. Story 4.1 builds on that rather than adding a second mechanism.
- **Defaults (AD-8):** the config file is `~/.config/herddash/config.toml`. The data directory is `$XDG_DATA_HOME/herddash`, falling back to `~/.local/share/herddash`, on both OSes. Config changes apply only on restart, and a flag beats the file, which beats the defaults.
- **Version stamping:** release builds pass `-ldflags "-X main.version=... -X main.commit=..."`. `scripts/release-build.sh` wires it, for the release workflow and locally (Story 4.3).
- **Toolchain:** `go.mod` requires Go 1.26. Releases are built with Go 1.27.2, which needs macOS 13 or newer. If an older Mac must be supported, build releases with 1.26.x.
- **Release workflow:**
  - It is a separate workflow (`release.yml`) that runs on `workflow_run` of release-please and acts on the tag that run created; a tag or release trigger would never fire, because release-please creates both with `GITHUB_TOKEN`. The release-please workflow itself must never publish artifacts.
  - release-please targets the default branch (`main`).
  - `docs` commits count towards a release here.
  - `initial-version` is pinned to 0.1.0.
  - The repository setting that lets Actions create pull requests must stay on.
- **Restraint:** this epic adds no new dependency, network surface or herdr call. Any of those needs an ADR first.

## Cross-Story Dependencies

- Story 4.2's restart-on-failure policy assumes Story 4.1's shutdown exits zero, plus the flock that the OS releases after a crash.
- Story 4.3's binaries are what the Story 4.2 install docs install on the second and third Hosts.
- Merging PR #2 creates the first tag, so Story 4.3's release workflow must exist on `main` before that merge or the first release ships with no binaries.
- After the epic, retiring the existing herdr plugin happens in the provisioning repository once the Daemon is trusted. It is not done here.
