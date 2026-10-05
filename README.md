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

Status: **early.** See [docs/prd.md](docs/prd.md) for the product definition and
[CONTRIBUTING.md](CONTRIBUTING.md) for how to work on it.

## Requirements

- herdr 0.9.3 or newer
- Go 1.25 or newer, to build

## License

MIT. See [LICENSE](LICENSE).
