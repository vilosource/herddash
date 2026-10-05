# Contributing to herddash

herddash follows a few conventions that aren't GitHub defaults. This document is the contract: it
tells you what a maintainer expects, so a PR doesn't stall on something you had no way to guess.

## Branch and PR — never `main`

All work lands on a topic branch and ships as a pull request. Nobody, maintainers included, commits
or pushes directly to `main`.

- Branch from a fresh `main`. Use `feat/`, `fix/`, `docs/` or `chore/` prefixes.
- Keep the PR scoped to one change. Small, reviewable diffs merge faster than large ones.
- CI must be green before merge.

## Conventional Commits

Commit messages, and PR titles since squash-merge uses them, follow
[Conventional Commits](https://www.conventionalcommits.org/): `feat: …`, `fix: …`, `docs: …`,
`chore: …`, `ci: …`. This isn't cosmetic. release-please parses these prefixes to generate the
changelog and pick the next version, so a mislabeled commit produces a wrong entry in a real
release.

## No AI attribution — hard rule

Commits and PR descriptions in this repo **must not** carry AI-attribution trailers or markers: no
`Co-Authored-By: Claude` or any other AI tool or assistant, no "Generated with …" footers, no 🤖,
no `noreply@anthropic.com` or equivalent. This holds regardless of what tooling you used to help
write the change.

If your PR's commits carry this kind of trailer, you'll be asked to reword them before merge. This
isn't a judgment on how you worked, it's just keeping the history clean.

## Decisions before code

Architecture-level changes start as a written decision record, not as a PR. Numbered, immutable
records live in [`docs/adr/`](docs/adr/) in Nygard format: Context, Decision, Consequences,
Alternatives Considered.

Open one before the code if you're proposing something structural: a new dependency, a change to
the herdr protocol handling, a change to the stored event schema, or a new network surface. Small
fixes, docs corrections and behaviour-preserving refactors don't need this.

## Build, test, run

Requires **Go >= 1.25**.

```bash
go build ./...
go test ./...
go vet ./...
```

Keep changes consistent with the surrounding code style. `gofmt` output is the formatting standard
and CI checks it.

## Reporting bugs and requesting features

Use the issue templates. They ask for the minimum a maintainer needs to triage: what you expected,
what happened, and how to reproduce it.

Include your herdr version. herddash talks to herdr's socket API over a versioned protocol, and
most surprising behaviour turns out to be a version difference.

## Security issues

Do not open a public issue for a security vulnerability. See [SECURITY.md](SECURITY.md).

herddash reads terminal output from your agent panes, which can contain credentials. Anything that
could expose that to a party who shouldn't see it is a security issue, not a bug.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). Participation implies
agreement to it.
