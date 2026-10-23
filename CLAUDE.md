# herddash

A dashboard for [herdr](https://herdr.dev), the terminal workspace manager for AI coding agents.
The question it answers: what is every agent doing, and which ones are waiting on me?

**Planning is complete; the build runs story by story. See "How the build runs" below.**

herddash is a **notification board backed by a recorder**: one read-only page with one box per
agent, longest-waiting first, plus a daemon that persists every agent status transition with the
profile that owns it and serves a read-only API. It runs beside the existing browser interfaces,
not instead of them. That is ADR-0001 (accepted 2026-10-08) as amended by ADR-0003: the board is
the product, and the recorder is what makes it trustworthy.

## Read these first, in this order

| Document | Why |
|---|---|
| [docs/adr/0001-what-herddash-is.md](docs/adr/0001-what-herddash-is.md) | **Accepted, option B.** What this project is, after the prior art took three of its five differentiators. Everything else is scoped by it. |
| [docs/adr/0003-the-board-is-the-product.md](docs/adr/0003-the-board-is-the-product.md) | Amends one ADR-0001 consequence: the board is what herddash is measured by, and its restraint (read-only, no terminal, no pane control) is the differentiator. |
| [docs/prd.md](docs/prd.md) | The product definition, **final** as of 2026-10-14, since amended in place by dated operator decisions. §3 is the vocabulary, used exactly everywhere; §4 holds FR-1 to FR-17 plus FR-2a and FR-9a. Its [addendum](docs/planning/prds/prd-herddash-2026-10-08/addendum.md) holds the protocol constraints, stack and build order. |
| [docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md) | **Final.** The rules every story must obey: package boundaries, the single-writer engine, identity, the record, the event stream. [How herddash works](docs/how-herddash-works.md) is the plain-language version. |
| [docs/research/herdr-on-this-machine.md](docs/research/herdr-on-this-machine.md) | The setup this project grew out of, none of which is visible from this repository. |
| [docs/research/herdr-socket-api.md](docs/research/herdr-socket-api.md) | How to talk to herdr. Read before any client code; it contains traps that look like free choices. |
| [docs/research/herdr-multi-host.md](docs/research/herdr-multi-host.md) | Why several machines means several daemons. |
| [docs/research/prior-art-herdr-web-ui.md](docs/research/prior-art-herdr-web-ui.md) | devswha's client, evaluated by running it. Works today. |
| [docs/research/prior-art-alecuba16-herdr-webui.md](docs/research/prior-art-alecuba16-herdr-webui.md) | alecuba16's project. Evaluation **parked**, needs herdr 0.9. |

The `docs/research/` files are **frozen records**, dated and written against specific versions. If
one turns out to be wrong, fix the code and amend the record in the same change rather than leaving
them to disagree.

## How the build runs

**Planning is complete.** The PRD, the architecture and the 42 stories in
`docs/planning/epics.md` (27 for Epics 1 to 4, 15 for Epic 5) are final, and
`docs/implementation/sprint-status.yaml` tracks them. Build one story at a time with
`/bmad-build`, in the order the tracking file lists; it names the next one. Do not write code that
no story asks for.

The tracking file's `action_items` name the operator checkpoints. Those of Epics 1 to 4 are closed:
- **Story 1.3** ran against an isolated herdr 0.9.5. That rule still holds for any herdr
  experiment, but the live server itself has run 0.9.5 since 2026-10-20 (see below).
- **Story 2.6** had its sample of the plugin log.
- **Story 3.6's design** was approved before Story 3.7.
- **Story 4.3** had its day of dogfooding before release-please PR #2 was merged, at 0.1.0.

Two are open:
- **When Epic 5 is done**, the operator is asked once; on their OK it ships as one pull request
  and one release. Until then its stories are built and installed locally as `-dev` builds.
- **Story 5.9** runs on or after 2026-11-03, and asks the operator only if the record shows
  Finished Agents that never cleared.

The architecture spine at
`docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md` is binding on
every story: its `AD` rules, its import graph and its conventions. ADR-0004 to ADR-0007 carry the
structural decisions behind it; ADR-0008 to ADR-0012 added Claude Code's session record, background
work, recaps, the tighter import rules and the event stream's keepalive. Its four open questions
were answered by Story 1.3 against an isolated herdr 0.9.5 binary; the evidence is in the socket
API record's "Findings against herdr 0.9.5".

The PRD's remaining open questions (§8) are answered during the build, not before it: whether
acknowledgement is needed at all (Story 5.9, from the record), and Profile rules and Pane Titles
for Codex and Antigravity once one pane of each has been observed.

## Working method

Planning and delivery run on the [BMAD Method](https://bmadcode.com/), installed into this
repository as the 29 `bmad-*` skills under `.claude/skills/`. Start with `/bmad-help`; it reads
`docs/` and recommends the next step. How BMAD's documents relate to the PRD and the ADRs, and why
everything it produces lands under `docs/planning/` and `docs/implementation/` rather than its
default folder, is [ADR-0002](docs/adr/0002-adopt-bmad-method.md). The order is fixed: decide
ADR-0001, then rework the PRD with `/bmad-prd`, then architecture, epics, sprint planning. All of
them are done. The PRD's BMAD run folder keeps a `prd.md` symlink to `docs/prd.md`, so the
skills find it and there is still only one PRD. The skills need `uv` on the machine; building
herddash does not.

**Deferred work** lives in `docs/implementation/deferred-work.md`, one entry per item with its
source spec, summary and evidence. An entry that is closed keeps its place and gains a `resolved:`
line saying when, by which story and where; an entry with no such line is open.

### Lessons from the retrospectives

The Epic 1 to 4 retrospectives (`docs/implementation/epic-<n>-retro-2026-10-23.md`) found these
habits missing. Apply them in every story:

- **A deferral that names a future story changes that story's acceptance criteria** in
  `epics.md` in the same commit. A deferral with no story goes into `deferred-work.md` without
  naming one. One routed "to Story 1.5" only in the ledger was never picked up and surfaced four
  epics later.
- **Every spine item marked Open gets an owner** when epics are cut: a story or a
  `deferred-work.md` entry. AD-3's epoch persistence had neither until a review caught it.
- **Run a new concurrency test with `-race -count=50`** (or `-cpu=1,4,8`) before merging. A fake
  client's race hung one run in 40 for five days before anyone saw it.
- **Test state derived from the record across a Daemon restart**, not only within one run.
  Waiting Time after an Agent was replaced in the same pane broke only after a restart.
- **Force a rollback test to fail mid-transaction**, after rows were written, not at `BeginTx`,
  or it proves nothing about rollback.
- **State a guarantee in a commit or pull request only as strongly as the code provides it.**
- **A toolchain the spine pins for a deliverable is tested in CI**, and a test that skips on a
  missing tool needs CI to prove it did not skip where the tool is expected
  (`HERDDASH_REQUIRE_TOOLS`).
- **A design gate for the Board looks at the design rendered with the operator's live data**, not
  only the anonymised fixture.

## Non-negotiables

**No AI attribution.** Commits and pull request descriptions must not carry `Co-Authored-By`
trailers for any AI tool, "Generated with" footers, 🤖, or equivalent. This is inherited from the
sibling vfkb project and holds regardless of tooling. It overrides any default attribution
behaviour.

**Never commit to `main` or `develop` directly.** Work lands on a `feat/`, `fix/`, `docs/` or
`chore/` branch, merges into `develop`, and `main` advances only from `develop`. See
[CONTRIBUTING.md](CONTRIBUTING.md).

**Conventional Commits**, because release-please parses them to pick versions and build the
changelog. A mislabelled commit produces a wrong entry in a real release.

**An ADR before structural change.** A new dependency, a change to how herdr is spoken to, a change
to the stored event schema, or a new network surface.

## Things that will bite you

- **The live herdr server on this machine hosts real agent work.** Since its restart on
  2026-10-20 it runs 0.9.5, the supported minimum (ADR-0006), so herddash needs no dogfooding flag
  against it. Never stop, restart or experiment on it. A Claude session here usually runs inside
  one of its panes, so the inherited `HERDR_SOCKET_PATH` points at it, and even a plain `herdr`
  command typed there is a client of the live server: run herdr experiments only
  in isolation, as `docs/research/herdr-0.9.5-probe.sh` does, under `env -i` with its own `HOME`.
- **herdr's configuration is owned by an Ansible repository, not by hand.** Do not edit
  `~/.config/herdr/config.toml`. Changes belong in that repository.
- **Do not install anything as a herdr plugin on this machine.** The plugin registry is managed and
  already carries a plugin that matters. Run things from their own directory instead.
- **Reading pane output is not passive.** An unbounded `recent` read drives the agent's own scroll
  interface and fails outright on a working or blocked pane. Read `visible`, or `recent` bounded by
  the pane's `viewport_rows`. *Sourced from herdr's documentation, not tested here: testing it
  means experimenting on a live agent pane. Complying costs nothing, so comply.*
- **Never call `pane.focus` or `agent.focus`.** They mutate herdr's seen state and would silently
  clear a completion the operator has not looked at. Reads do not. *Same provenance, same
  asymmetry: if the claim is wrong, not calling focus costs nothing.*
- **There is no global agent-status subscription.** Status must be subscribed per pane, with the
  pane set tracked from the global pane lifecycle events. *Confirmed first-hand against the
  installed binary's own schema: 27 subscription variants, exactly three of which require a pane
  id, and agent status is one of the three.*

On that last distinction generally: the socket API record has a **Provenance** section separating
what was confirmed against the running server from what was taken from herdr's published
documentation and marked UNVERIFIED. Read it before a claim from it decides a design.

## Releases, and the setting that is not in this repository

Releases are release-please's job. It watches `main`, maintains a standing release pull request,
and on merge tags the version and cuts the GitHub release. It never publishes artifacts.

**Binaries are `.github/workflows/release.yml`'s job.** It runs `scripts/release-build.sh` with Go
1.27.2 (the spine's release toolchain), which builds `herddash_<version>_<os>_<arch>` with
`CGO_ENABLED=0` and `-trimpath` for darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64, the
version and 12-character commit stamped in, plus one `SHA256SUMS`. It checks the checksums, each
binary's build info (`go version -m`: target, `CGO_ENABLED=0`, `-trimpath`, the tagged commit,
clean tree; Go records no `-ldflags` under `-trimpath`) and that the linux/amd64 binary's `version`
prints the release's version and commit. Then it attaches the binaries, and `SHA256SUMS` last, to
the existing release with `gh release upload --clobber`. It never creates a tag or a release, and
holds only `contents: write`.

It runs `on: workflow_run` of release-please, for any completed run on `main` from a push, **not**
on a tag or release event: release-please creates both with `GITHUB_TOKEN`, and GitHub starts no
workflow from events that token causes. It does not require release-please's run to succeed, since
a release can exist even when a later step failed. `scripts/release-find.sh` decides: it looks for
a `v*` tag with a GitHub release on the run's head commit and ends the run with nothing to do when
there is none, which is every ordinary push. Only gh's "release not found" counts as no release;
any other lookup failure, a tag that is not `vX.Y.Z[-prerelease]`, or a tag not on the checked-out
commit fails the job, and so does a release that already has assets. Runs are grouped per head
commit or tag, so a newer run never cancels the one carrying a release. A `workflow_run` workflow
only fires once it is on the default branch, so it must be on `main` before a release PR is merged.
To build a release's assets by hand, for example one created by a later release-please run, run it
with the tag: `gh workflow run release.yml -f tag=v0.1.0`; add `-f force=true` to replace assets
the release already has. `release_test.go` tests both scripts, `release-find.sh` against a
temporary repository and a stub `gh`.

**Two things about it are not obvious from the workflow file.**

First, the job declaring `pull-requests: write` is **not sufficient.** There is a separate
repository setting, *Allow GitHub Actions to create and approve pull requests*, which is off by
default. Without it release-please does all its work, pushes its release branch, and then fails on
the last step with:

```
release-please failed: GitHub Actions is not permitted to create or approve pull requests.
```

That happened here on the first two runs. It is fixed, by setting
`can_approve_pull_request_reviews` to true on `repos/vilosource/herddash/actions/permissions/workflow`.
If release-please starts failing that way again, check that setting before the workflow.

Second, **release-please targets the repository's default branch**, not the branch that triggered
the run. When this repository was created, `develop` was briefly the default because it was pushed
first, so an early run built a release branch against `develop`. That stray branch was deleted. If
you ever see a `release-please--branches--develop--*` branch, the default branch is wrong.

Third, **`docs` commits do trigger a release here**, because `docs` is configured visible in
`changelog-sections` rather than hidden. Only `chore`, `ci`, `test` and `refactor` are hidden. The
first successful run duly proposed a release containing nothing but documentation.

And it proposed **1.0.0**, for a repository with no code. release-please defaults a first release
to 1.0.0 when it finds no previous release, and a manifest of `0.0.0` with no matching tag reads as
no previous release. `bump-minor-pre-major` does not help, because that governs how a breaking
change behaves while already pre-1.0. The config now pins `initial-version` to `0.1.0` for the
package. **Check the version a release pull request proposes before merging it** — the first one
is the only chance to get it right, and a published 1.0.0 cannot be withdrawn.

## Build and test

Requires Go 1.26 or newer (the SQLite driver sets that floor; ADR-0004). CI
(`.github/workflows/ci.yml`) runs these on every pull request and on pushes to `develop` and
`main` (tests with `-race`), then builds `./cmd/herddash` and vets the module with
`CGO_ENABLED=0` for darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64:

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .      # must print nothing
```

Beside them, CI's `board-browser` job builds `./cmd/herddash` and runs
`internal/httpapi/web/check/board-browser.mjs` against the runner's preinstalled Google Chrome with
the runner's Node (22 or newer, no npm), so breaking the Board's drawing, such as removing the
`.board--recaps` rule, turns CI red. To run it locally (on Linux, prefix the second command with
`CHROME=$(command -v google-chrome)`):

```bash
go build -o /tmp/herddash ./cmd/herddash
node internal/httpapi/web/check/board-browser.mjs /tmp/herddash
```

Two more CI jobs run beside those. `macos` runs `go test -race ./...` on macos-latest with go.mod's
Go, so procenv's darwin reader and `TestLaunchdAgentLints` (`plutil -lint`) run in CI.
`release-toolchain` runs on ubuntu-latest with the spine's release Go (1.27.2, which
`release_test.go` holds equal to the spine's row): `go vet ./...`, `go test -race ./...`, then
`scripts/release-build.sh 0.0.0-ci <commit> "$RUNNER_TEMP/dist"` with `sha256sum -c`, four
checksum lines and the linux/amd64 binary's `version` output checked. Tests that need a tool
(`plutil`, `systemd-analyze`) skip when it is missing, unless `HERDDASH_REQUIRE_TOOLS`, a
space-separated list, names it; then they fail. The `macos` job sets it to `plutil` and
`release-toolchain` to `systemd-analyze`; locally, leave it unset.

`arch_test.go` at the module root enforces the spine's import graph over every non-test Go file in
the module. It fails, naming the edge or the call, on:

- any import between internal packages that the spine's diagram does not draw;
- any package outside the spine;
- any non-standard import in `internal/core`, or in `internal/herdr` and the packages under it
  (`gen`, `genwire`) (ADR-0011);
- any import of `internal/herdr/gen` or a package under it from outside `internal/herdr`
  (ADR-0011);
- `internal/core` reading or waiting on the clock (`time.Now`, `Since`, `Sleep` and the like, or a
  dot-import of `time`).

`testdata/badtree/` is the fixture that proves each rule fails. Changing the permitted set is a
structural change and needs an ADR.

`herddash version` prints `herddash <version> (<commit>)`. Without stamping, the version is `dev`
and the commit comes from Go's embedded VCS info: a plain `go build` in a clone prints
`herddash dev (<12 hex digits>)`, with `-dirty` appended when the tree had uncommitted changes,
and `go run ./cmd/herddash version` embeds none, so it prints `herddash dev (unknown)`. Release
builds stamp `-ldflags "-s -w -X main.version=<version> -X main.commit=<commit>"`, which
`scripts/release-build.sh <version> <commit> <outdir>` does, so a release binary prints e.g.
`herddash 0.5.0 (<12 hex digits>)`; `-s -w` strips the symbol table and DWARF data.
`release_test.go` runs that script (only the host's target under `-short`) and checks the release
workflows' triggers, concurrency, permissions, pinned actions and Go version, and CI's job set.
`TestReleaseBuild` also checks every binary's `vcs.revision` equals `git rev-parse HEAD` and
`vcs.modified` matches the tree (`git status --porcelain`), so a dirty local tree still passes;
with `CI` true, as GitHub sets it, it requires git and a clean tree and fails otherwise. Outside
CI it skips both checks in a linked git worktree (`.git` is a file, as under `.claude/worktrees/`):
Go 1.26 stamps such a worktree's binaries with the enclosing checkout's commit and state.

No npm, no build step for assets. Web assets are embedded in the binary.

## Repository state

**Epics 1 to 4 are done, released as v0.1.0 to v0.5.0, and Epic 5 (closing what dogfooding and
reviews left open after 0.5.0) is in progress** on `feat/epic-5-after-0.5.0`.
`docs/implementation/sprint-status.yaml` is the authority on which story is next; each story's
spec, with its review triage log, is `docs/implementation/spec-<key>.md`. `go.mod` declares
`go 1.26.0` and three direct dependencies: `modernc.org/sqlite` v1.60.1,
`github.com/BurntSushi/toml` v1.6.0 and `golang.org/x/sys` v0.49.0 (ADR-0004). What exists:

- `cmd/herddash`: `serve` (the default), `config`, `import-plugin-log` and `version`.
- `internal/config`: the TOML file, flags, validation, and socket, Host and Server resolution.
- `internal/herdr`: the socket client (one request per connection, per-pane status
  subscriptions, stream-health signals). `internal/herdr/gen` holds the herdr 0.9.5 wire types,
  generated from the committed schema dump by `go generate ./internal/herdr/gen`; a test fails if
  they drift. A lifecycle line that decodes as JSON but is not an event it can deliver arrives
  flagged unexpected, and the engine answers it with a fresh snapshot rather than a reconnect
  (Story 5.12).
- `internal/procenv`: reads one process's environment and its direct children (linux `/proc`,
  darwin `kern.procargs2` through `golang.org/x/sys`), Claude Code's session record
  `sessions/<pid>.json`, and that session's transcript, tailed in memory (ADR-0008, ADR-0009).
- `internal/engine`: the single-writer actor. It reconnects forever, detects server epochs
  (persisted in `server_state`), resyncs, applies the version gate, resolves Profiles through
  `internal/procenv`, records Transitions and publishes immutable views. Snapshot reads that keep
  failing after start, `defaultSnapshotFailuresBeforeLost` (3) in a row, count as herdr lost, so
  the connection strip says so (Story 5.12). Its fixed timings are in
  `internal/engine/tunables.go`, listed for operators at the end of `docs/config.example.toml`.
- `internal/core`: the pure rules: Attention Class, Waiting Time fold, sort key, Context Line,
  Profile rules, and the transcript decoder. `core.NewTransition` refuses a `HerdrOrder` on a
  `live` or `backfill` Transition (`core.ErrInvalidHerdrOrder`).
- `internal/store`: the SQLite record (`modernc.org/sqlite`, WAL, flock-owned), plus the engine's
  `server_state`, which is engine state, not the append-only record.
- `internal/httpapi`: the read-only loopback API: `/api/state`, `/events` (SSE) and
  `/api/agents/{host}/{server}/{epoch}/{pane}/history`. Every 15 s a stream sends a `keepalive`
  event with no `id:` (ADR-0012).
- `internal/importer`: the plugin-log backfill.

- The Board at `/`, with its files under `internal/httpapi/web/` embedded in the binary: the
  ordering and event logic is `order.mjs`, tested with `node --test` in CI, and
  `internal/httpapi/web/check/board-browser.mjs` is a headless-Chrome check of its drawing,
  run by CI's `board-browser` job. Its approved design is `docs/design/board-design.md`.
- `deploy/`: a launchd agent and a systemd user unit, installed by hand as `docs/install.md`
  describes.

What the Board shows beyond herdr's Status, all for Claude Code Agents:

- **Needs you from the session record** (ADR-0008): Claude Code's own `sessions/<pid>.json`
  saying it waits on the operator is a second source of `blocked`, recorded with
  `observed_by = claude_session`.
- **Background** (ADR-0009): a done or idle Agent with background shells or tasks still open,
  from its child processes and its transcript's task markers. A resumed session's `stopped`
  notifications close the tasks the earlier process left open, and `/clear` starts a new
  transcript, so the old tasks go with it (Stories 5.2 and 5.3; the research records are
  `docs/research/claude-code-resume-background-tasks-2026-10-23.md` and
  `docs/research/claude-code-clear-background-tasks-2026-10-23.md`).
- **Recaps** (ADR-0010, only with `claude_recaps = true`): the newest recap, from an
  `away_summary` record or from the output of a `/recap` run by hand (Story 5.14), held in memory
  and never recorded. A local command such as `/usage`, with its caveat and its output
  (`<local-command-stdout>` or `<local-command-stderr>`), does not start a turn, so it neither
  clears the recap nor changes the turn state (Story 5.14). A skill command that starts a model
  turn does, and so does a `!command` in bash mode, because Claude Code answers it with a model
  turn (Story 5.15, `docs/research/claude-code-bash-mode-2026-10-23.md`).

Epic 5 has also made these exact so far:

- The Board treats a stream that sends nothing, keepalive included, for 35 s
  (`STREAM_SILENCE_MS`) as a lost Daemon: it dims, says so and reconnects. A skipped sequence
  number or a rejected `state` event makes it load the whole state again. With herdr's version
  unknown, the version strip says it could not be read (Story 5.11).
  `internal/httpapi/engine_contract_test.go` drives the SSE handler from the real engine and
  replays the publications through `order.mjs` (`internal/httpapi/testdata/replay-board.mjs`),
  asserting the Board's model equals the published view.
- Context Lines lose Unicode bidi controls (other format characters, such as an emoji's
  zero-width joiner, stay), render inside `<bdi>`, and lose only Claude Code's own status glyph
  prefix, so a session name starting with `-`, `#` or an emoji keeps it. A workspace herdr gives
  no position gets its own group and name, after the positioned ones (Story 5.13).
- CI's `board-browser`, `macos` and `release-toolchain` jobs (Stories 5.6, 5.7; see "Build and
  test").
- `arch_test.go`'s ADR-0011 rules (Story 5.5).

`main` and `develop` are kept level; if they are not, a `develop` into `main` pull request is open
or overdue.

The remote is `git@github.com:vilosource/herddash.git`, public, MIT. This repository's git identity
is **pinned locally** to the GitHub noreply address rather than the machine's global work email, so
commits here do not leak it. Check with `git config --local user.email` before the first commit of
a session.

Deliberately unmanaged: nothing in this repository installs or configures anything on a machine.
Deployment to the operator's three machines is the provisioning repository's job, and that is a
later concern than anything blocking here.
