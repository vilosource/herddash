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
- Read-only apart from one thing: you can mark a finished agent's result as seen, which
  herddash records for itself and never tells herdr
  ([ADR-0014](docs/adr/0014-acknowledging-a-finished-agent.md)); it also marks it for you when
  you read it in herdr and leave the pane, by reading herdr's focus, never setting it
  ([ADR-0015](docs/adr/0015-seen-when-you-leave-the-pane.md)). No terminal, no prompts, no pane
  control: herdr already does those.

Status: **pre-release, being dogfooded.** The recorder, the API and the Board work; the API is
read-only except for the acknowledgement routes the Board's Seen buttons use, which accept
requests only from the Board's own page. Two browser interfaces for herdr already exist, so
herddash is deliberately narrow: a notification board backed by a recorder, running beside them,
whose only control is marking a finished result seen. See [ADR-0001](docs/adr/0001-what-herddash-is.md) and
[ADR-0003](docs/adr/0003-the-board-is-the-product.md).

- [docs/how-herddash-works.md](docs/how-herddash-works.md) explains the design in plain
  language: what it does, what it stores, and what it will never do.
- [docs/prd.md](docs/prd.md) is the product definition, and the
  [architecture spine](docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md)
  the rules the build follows. The build runs story by story from
  [docs/planning/epics.md](docs/planning/epics.md).
- [docs/install.md](docs/install.md) explains how to install it yourself to start with your
  session, under launchd on macOS or a systemd user unit on Linux, without root, and covers logs
  and backup. Nothing in this repository installs anything.
- [docs/research/](docs/research/) holds frozen notes on herdr's socket API, its multi-host
  model, and hands-on evaluations of the two closest existing projects. Read them before
  writing client code.
- [docs/adr/](docs/adr/README.md) holds the decision records.
- [CONTRIBUTING.md](CONTRIBUTING.md) is the contract for working on this, and
  [CLAUDE.md](CLAUDE.md) is the orientation for an agent session.

## Requirements

- herdr 0.9.5 or newer
- Go 1.26 or newer, to build

## License

MIT. See [LICENSE](LICENSE).
