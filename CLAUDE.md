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
| [docs/prd.md](docs/prd.md) | The product definition, **final** as of 2026-10-14. §3 is the vocabulary, used exactly everywhere; §4 holds FR-1 to FR-17 plus FR-2a and FR-9a. Its [addendum](docs/planning/prds/prd-herddash-2026-10-08/addendum.md) holds the protocol constraints, stack and build order. |
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

**Planning is complete.** The PRD, the architecture and the 27 stories in
`docs/planning/epics.md` are final, and `docs/implementation/sprint-status.yaml` tracks them.
Build one story at a time with `/bmad-build`, in the order the tracking file lists, starting with
Story 1.1. Do not write code that no story asks for.

The tracking file's `action_items` name the operator checkpoints:
- **Story 1.3** needs an isolated herdr 0.9.5, never the live 0.8.2 server.
- **Story 2.6** needs a sample of the plugin log.
- **Story 3.6's design** needs approval before Story 3.7.
- **Story 4.3** needs a day of dogfooding before release-please PR #2 is merged.

The architecture spine at
`docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md` is binding on
every story: its `AD` rules, its import graph and its conventions. ADR-0004 to ADR-0007 carry the
structural decisions behind it. Its four open questions are answered in milestone 1 against a
herdr 0.9.5 binary.

The PRD's remaining open questions (§8) are answered during the build, not before it: whether
acknowledgement is needed at all after two weeks of dogfooding, one-request-per-connection on a
0.9.5 binary in milestone 1, and Profile rules and Pane Titles for Codex and Antigravity once one
pane of each has been observed.

## Working method

Planning and delivery run on the [BMAD Method](https://bmadcode.com/), installed into this
repository as the 29 `bmad-*` skills under `.claude/skills/`. Start with `/bmad-help`; it reads
`docs/` and recommends the next step. How BMAD's documents relate to the PRD and the ADRs, and why
everything it produces lands under `docs/planning/` and `docs/implementation/` rather than its
default folder, is [ADR-0002](docs/adr/0002-adopt-bmad-method.md). The order is fixed: decide
ADR-0001, then rework the PRD with `/bmad-prd`, then architecture, epics, sprint planning. The
first two are done. The PRD's BMAD run folder keeps a `prd.md` symlink to `docs/prd.md`, so the
skills find it and there is still only one PRD. The
skills need `uv` on the machine; building herddash does not.

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

- **herdr on this machine is 0.8.2 and cannot be upgraded right now**, because its server hosts
  live agent work. Current stable is 0.9.5, and herddash supports 0.9.5 or newer (ADR-0006).
  Expect to test against 0.8.2 behind the dogfooding flag.
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
and on merge tags the version and cuts the GitHub release. It never publishes artifacts; that would
be a separate workflow keyed off the tag, and none exists yet.

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

Requires Go 1.26 or newer (the SQLite driver sets that floor; ADR-0004). There is no code yet, so
these are the intended commands:

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .      # must print nothing
```

No npm, no build step for assets. Web assets are embedded in the binary.

## Repository state

**There is no code yet** — documentation and project setup only. `main` and `develop` are kept
level; if they are not, a `develop` into `main` pull request is open or overdue.

The remote is `git@github.com:vilosource/herddash.git`, public, MIT. This repository's git identity
is **pinned locally** to the GitHub noreply address rather than the machine's global work email, so
commits here do not leak it. Check with `git config --local user.email` before the first commit of
a session.

Deliberately unmanaged: nothing in this repository installs or configures anything on a machine.
Deployment to the operator's three machines is the provisioning repository's job, and that is a
later concern than anything blocking here.
