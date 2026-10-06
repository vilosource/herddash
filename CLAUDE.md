# herddash

A dashboard for [herdr](https://herdr.dev), the terminal workspace manager for AI coding agents.
The question it answers: what is every agent doing, and which ones are waiting on me?

**Nothing is built yet. Do not start writing code — see "What blocks the build" below.**

## Read these first, in this order

| Document | Why |
|---|---|
| [docs/adr/0001-what-herddash-is.md](docs/adr/0001-what-herddash-is.md) | **Open and blocking.** What this project even is, after the prior art took three of its five differentiators. |
| [docs/prd.md](docs/prd.md) | The product definition. Section 3 is the honest scorecard. Sections 7 to 12 still describe a larger product than survives and need rewriting once the ADR lands. |
| [docs/research/herdr-on-this-machine.md](docs/research/herdr-on-this-machine.md) | The setup this project grew out of, none of which is visible from this repository. |
| [docs/research/herdr-socket-api.md](docs/research/herdr-socket-api.md) | How to talk to herdr. Read before any client code; it contains traps that look like free choices. |
| [docs/research/herdr-multi-host.md](docs/research/herdr-multi-host.md) | Why several machines means several daemons. |
| [docs/research/prior-art-herdr-web-ui.md](docs/research/prior-art-herdr-web-ui.md) | devswha's client, evaluated by running it. Works today. |
| [docs/research/prior-art-alecuba16-herdr-webui.md](docs/research/prior-art-alecuba16-herdr-webui.md) | alecuba16's project. Evaluation **parked**, needs herdr 0.9. |

The `docs/research/` files are **frozen records**, dated and written against specific versions. If
one turns out to be wrong, fix the code and amend the record in the same change rather than leaving
them to disagree.

## What blocks the build

**ADR-0001 is Proposed, not Accepted.** Two browser interfaces for herdr already exist and were
evaluated hands-on. Between them they took three of the five differentiators this project claimed,
including its primary thesis. What survives is a persisted history of agent activity and
attributing a pane to the account that owns it — a feature pair, not a product.

The ADR names three options and recommends the second, a narrow complement. **Each implies a
different build, so decide it before writing code.** Accepting it also resolves three of the PRD's
open questions, which the ADR lists.

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
  live agent work. Current stable is 0.9.3. Target 0.9.2 or newer in design, but expect to test
  against 0.8.2.
- **herdr's configuration is owned by an Ansible repository, not by hand.** Do not edit
  `~/.config/herdr/config.toml`. Changes belong in that repository.
- **Do not install anything as a herdr plugin on this machine.** The plugin registry is managed and
  already carries a plugin that matters. Run things from their own directory instead.
- **Reading pane output is not passive.** An unbounded `recent` read drives the agent's own scroll
  interface and fails outright on a working or blocked pane. Read `visible`, or `recent` bounded by
  the pane's `viewport_rows`.
- **Never call `pane.focus` or `agent.focus`.** They mutate herdr's seen state and would silently
  clear a completion the operator has not looked at. Reads do not.
- **There is no global agent-status subscription.** Status must be subscribed per pane, with the
  pane set tracked from the global pane lifecycle events.

## Build and test

Requires Go 1.25 or newer. There is no code yet, so these are the intended commands:

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .      # must print nothing
```

No npm, no build step for assets. Web assets are embedded in the binary.

## Repository state

`main` and `develop` are level. Four documentation commits, no code. The remote is
`git@github.com:vilosource/herddash.git`, public, MIT. This repository's git identity is pinned
locally to the GitHub noreply address rather than the machine's global work email.
