# Prior art: devswha/herdr-web-ui

**Evaluated 2026-10-05 by installing and running it against a live herdr session.** This is the
project herddash has to justify itself against. It is why open question 1 in the PRD exists, and
this record is the evidence for answering it.

Read this before writing herddash code. Three of the five differentiators the PRD claimed are
weaker than they looked, and this project has already solved a problem the PRD listed as open.

The companion record is [prior-art-alecuba16-herdr-webui.md](prior-art-alecuba16-herdr-webui.md),
which takes a further differentiator. The combined scorecard is in that file and in PRD §3.

## What it is

| | |
|---|---|
| Repository | https://github.com/devswha/herdr-web-ui |
| Version evaluated | 0.3.49 |
| Licence | MIT |
| Stars | about 472 |
| Activity | pushed the same day it was evaluated |
| Stack | Bun, TypeScript, React 18, Vite, xterm.js |
| Declared herdr minimum | 0.9.0, in `herdr-plugin.toml` |

Its own description is a browser and phone client for herdr: chat and live terminal for every agent
pane, remote hosts over SSH, and web push alerts. It installs as a herdr plugin and serves on
`127.0.0.1:7317`.

## The local setup

Cloned, built and run on the author's Mac. The clone lives at `~/GitHub/herdr-web-ui`.

```bash
git clone https://github.com/devswha/herdr-web-ui.git ~/GitHub/herdr-web-ui
cd ~/GitHub/herdr-web-ui
bun install
bun run build
bun run server        # serves http://127.0.0.1:7317
```

Prerequisite installed for this: **Bun 1.4.2 via Homebrew.** That is unmanaged machine state, not
provisioned by configuration management.

**It was deliberately not installed as a herdr plugin.** The documented route is
`herdr plugin install devswha/herdr-web-ui`, which registers it in the user's global herdr plugin
registry. Running the server straight from the clone leaves that registry untouched, which matters
on a machine where the registry is owned by configuration management and already carries another
plugin.

Running it **does** write to `~/.config/herdr-web-ui`, mode `0700`, containing a `bridges/`
directory and a `completions.json`. Removing that directory and the clone undoes the spike, leaving
only the Bun installation.

## It works on herdr 0.8.2, despite declaring 0.9.0

This was the useful surprise. herdr refuses to link a plugin whose `min_herdr_version` exceeds the
running binary, so the plugin route is hard-blocked below 0.9.0. **Running its server directly
bypasses that gate**, and on 0.8.2 it reported:

```json
{"ok":true,"herdr":{"version":"0.8.2","protocol":20,"terminal_attach":true},
 "auth":{"required":false,"authenticated":true,"via":"local","role":"drive"}}
```

and served the complete session: 3 workspaces, 9 tabs, 15 panes, 10 agents, with per-pane status
and terminal titles. So the prior art is evaluable without upgrading herdr, and that version gate
is a packaging assertion rather than a runtime requirement.

## How it talks to herdr

The same architecture herddash's PRD arrived at independently. Its server holds the herdr socket
connection and subscribes to events.

| Subscribes to | `pane.agent_status_changed`, `pane.created`, `pane.closed`, `pane.exited`, `pane.focused` |
|---|---|
| Calls | `session.snapshot`, `agent.get`, `pane.process_info`, `pane.read`, `pane.send_text`, `pane.send_keys` |

Because the server owns that connection, **it does not need an attached herdr terminal client.**
That is the property herddash's PRD claimed as its primary reason to exist.

It leans on `pane.process_info` heavily, which is how it inspects what is actually running in a
pane.

## It already solved our open question 5

The PRD listed "what does the operator click to mark an agent as dealt with" as unresolved, because
herdr's own seen state is tracked per terminal client and cannot be borrowed.

This project keeps its own store at `~/.config/herdr-web-ui/completions.json`:

```json
{"herdr":"16777232:799919","finished":[],"finishedAgents":{}}
```

A server generation marker, plus a set of finished panes and finished agents. That is a working
precedent for the concept herddash said it would have to invent.

## Security posture

Assessed because it holds a connection that can drive live agents.

- **No authentication by default**, with the role reported as `drive`. Anything able to reach the
  port can send text and keys to real agent panes. Binding to loopback is the only protection in
  the default configuration.
- **No telemetry.** No analytics or crash-reporting library in the server or the client, and no
  live outbound connections observed from the running process.
- **It does reach provider APIs for optional features.** Usage panels query Anthropic, OpenAI,
  GitHub Copilot, Cursor and xAI, and voice transcription posts to OpenAI with the user's own key.
  The usage code reads `CLAUDE_CONFIG_DIR` and scans `~/.claude-*` sibling directories to find
  credentials for more than one account.
- Its state directory is created `0700`.

## Honest scorecard against the PRD's five differentiators

| Claimed differentiator | Verdict |
|---|---|
| A daemon that works while you are detached | **Neutralised.** Its server holds its own socket and subscribes to the same events. No attached client needed. |
| Triage rather than remote control | **Largely neutralised.** It ships a "Needs you" grouping and a "Needs input" view, so it is more than a terminal in a browser. |
| Persisted history | **Holds.** There is no database and no status-transition history. Its only persistence is configuration and the finished-pane set. No timeline exists. |
| Telling several subscriptions of the same agent apart | **Holds, narrowly.** It reads several Claude config directories, but only to aggregate plan usage. Panes are not labelled by the subscription that owns them. |
| One static binary, no per-host runtime | **Holds.** It needs Bun and Node on every host. |

Two and a half of five survive contact.

## What this means

"A web interface for herdr" is comprehensively taken, and taken well. The honest remaining gap is
narrower than the PRD assumed, and is specifically:

1. **A history of what agents did**, rather than only what they are doing now. No existing project
   keeps one.
2. **Attribution of a pane to the subscription that owns it**, which matters only to an operator
   running one agent program under several accounts.
3. **Deployability as a single binary** by configuration management across several hosts.

That is a real but much smaller product than the PRD describes, and it is closer to a complement to
this project than a replacement for it. A defensible outcome is to narrow herddash to the history
and attribution gaps, or to contribute them upstream there instead.

**That decision is not made in this file.** It belongs in an ADR, with the PRD rewritten to match.
