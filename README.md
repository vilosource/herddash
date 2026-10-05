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
- Sorted by who has been blocked longest, because that is the actionable number.
- Click an agent for its state history and what it last said.

Status: **early.** Nothing is built yet.

- [docs/prd.md](docs/prd.md) is the product definition, including the prior art this has to justify
  itself against.
- [docs/research/](docs/research/) holds frozen notes on herdr's socket API and its multi-host
  model. Read them before writing client code.
- [CONTRIBUTING.md](CONTRIBUTING.md) is the contract for working on this.

## Requirements

- herdr 0.9.2 or newer
- Go 1.25 or newer, to build

## License

MIT. See [LICENSE](LICENSE).
