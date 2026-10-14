# herddash

A web dashboard for [herdr](https://herdr.dev), the terminal workspace manager for AI coding
agents.

herdr knows the state of every agent pane it manages. Its own way of telling you is a desktop
notification, which has two limits you cannot configure away: it suppresses popups for the tab you
are looking at, and it only delivers through a foreground attached client. Detach your session to
leave agents running and you go silent exactly when you most want to know.

herddash is a small daemon that connects to herdr's socket API and serves one page answering a
different question: **what is every agent doing right now, and which ones are waiting on me?**

- Live, no refresh, no attached terminal required.
- Sorted by who has been waiting on you longest, because that is the actionable number.
- Each agent labelled with the profile it runs under, so two `claude` panes on different
  subscriptions are told apart.
- Every status change recorded, so "waiting 40 minutes" is a fact that survives a restart.
- Read-only. No terminal, no prompts, no pane control: herdr already does those.

Status: **early.** Nothing is built yet. Two browser interfaces for herdr already exist, so
herddash is deliberately narrow: a read-only notification board backed by a recorder, running
beside them. See [ADR-0001](docs/adr/0001-what-herddash-is.md) and
[ADR-0003](docs/adr/0003-the-board-is-the-product.md).

- [docs/prd.md](docs/prd.md) is the product definition, final as of 2026-10-14. Architecture
  and epics come next.
- [docs/research/](docs/research/) holds frozen notes on herdr's socket API, its multi-host
  model, and hands-on evaluations of the two closest existing projects. Read them before
  writing client code.
- [docs/adr/](docs/adr/README.md) holds the decision records.
- [CONTRIBUTING.md](CONTRIBUTING.md) is the contract for working on this, and
  [CLAUDE.md](CLAUDE.md) is the orientation for an agent session.

## Requirements

- herdr 0.9.2 or newer
- Go 1.25 or newer, to build

## License

MIT. See [LICENSE](LICENSE).
