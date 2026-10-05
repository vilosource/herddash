# Prior art: alecuba16/herdr-webui

**Evaluated 2026-10-05 by building it from source and running it against a live herdr session.**
The companion record is
[prior-art-herdr-web-ui.md](prior-art-herdr-web-ui.md) for devswha's project. Between them, they
answer PRD open question 1.

This one matters for a different reason than the first. It is not really a herdr client, it ships
the thing herddash claimed as its last structural advantage, and its licence makes borrowing from
it impossible.

## What it is

| | |
|---|---|
| Repository | https://github.com/alecuba16/herdr-webui |
| Version evaluated | 0.2.99 |
| Licence | **AGPL-3.0-or-later**, with a commercial option offered |
| Stars | about 34 |
| Activity | pushed the same day it was evaluated |
| Stack | Rust, Axum, Tokio, ratatui, portable-pty, rustls |
| External-backend floor | 0.9.0, as `MIN_BACKEND_VERSION` in source |

**It is a herdr replacement, not a dashboard.** By default it starts its own embedded terminal
multiplexer and needs no herdr binary at all. Its own README says so: one process, the Axum server
plus an embedded PTY backend. It emulates herdr's terminal server protocol at version 22, carries
its own agent detection, and ships a second binary that is a first-party terminal client. Talking
to a real herdr daemon is an optional compatibility mode.

So it competes with herdr itself as much as it complements it. That is a different product
category from herddash and from devswha's client.

## The local setup

Clone at `~/GitHub/herdr-webui`. No new prerequisite: Rust 1.98 was already present via Homebrew,
and the frontend assets are vendored, so no Node step is needed.

```bash
git clone https://github.com/alecuba16/herdr-webui.git ~/GitHub/herdr-webui
cd ~/GitHub/herdr-webui
cargo build
./target/debug/herdr-webui --https off --bind 127.0.0.1:8788 \
  --backend-mode external-herdr \
  --api-socket ~/.config/herdr/herdr.sock \
  --client-socket ~/.config/herdr/herdr-client.sock
```

Built from source deliberately rather than taking the published tarball, since the project has few
stars and the binary would hold a connection able to drive live agents.

It writes `~/.config/herdr-webui/webui-settings.json`, and, as the next section explains, it
also creates its own multiplexer sockets under `~/.config/herdr-webui/builtin/` even when told
to use an external backend. Removing that directory and the clone undoes the spike.

Default mode is `builtin`, which would have started its own multiplexer and shown none of the real
agents. The explicit `external-herdr` mode plus socket paths is what points it at the running
herdr.

## It cannot be evaluated against herdr 0.8.2, and it fails quietly

This is the finding that matters, and it was missed on a first pass because probing the HTTP API
makes it look as though the thing works.

Pointed at the live 0.8.2 server with an explicit `--backend-mode external-herdr` and explicit
socket paths, it started, served, and reported the version mismatch:

```json
{"backend":"0.8.2","backend_mode":"external-herdr","backend_protocol_version":20,
 "compatibility":{"compatible":false,"status":"protocol_mismatch",
   "message":"backend direct attach protocol does not match this WebUI build"},
 "herdr_install":{"available":true,"compatible":false,"path":"herdr","version":"0.8.2"}}
```

Its data endpoints then returned the real session at full fidelity, because they proxy the socket
paths given on the command line: 3 workspaces, 9 tabs, 15 panes and 10 agents, each with status,
working directory and terminal title.

**But the browser shows none of it.** The session list offers exactly one session, and it is not
the herdr one:

```json
{"herdr_available":true,"herdr_compatible":false,"herdr_version":"0.8.2","running_count":1,
 "sessions":[{"backend":"builtin","backend_label":"built-in","name":"default","running":true,
   "api_socket":"~/.config/herdr-webui/builtin/default/herdr.sock"}]}
```

Because `herdr_compatible` is false, it **silently started its own embedded multiplexer and
selected that instead**, creating `~/.config/herdr-webui/builtin/default/herdr.sock` and its
client socket. It also wrote `"backend_mode": "builtin"` into
`~/.config/herdr-webui/webui-settings.json`, overriding the mode passed on the command line and
persisting that override for the next run.

So the user-visible result on 0.8.2 is an empty built-in session. Nothing announces the fallback
in the interface, and the top-level `backend_mode` field still reports `external-herdr`, which is
what made the API probe misleading.

**Verdict: this project requires herdr 0.9.0 or newer, enforced as `MIN_BACKEND_VERSION` in
source, and there is no partial mode worth using below it.** Unlike devswha's client, which ran
fully against 0.8.2, this one cannot be assessed as a herdr dashboard without the upgrade. The
scorecard rows below that concern packaging and licensing stand regardless, since they come from
its release artifacts and source rather than from running it.

The underlying split is still the one the socket API research predicted: the JSON API is
version-tolerant, while direct terminal attach rides the length-prefixed binary protocol that
needs an exact version match. A dashboard that never attaches a terminal would be unaffected by
that mismatch. This project chose to treat it as disqualifying for the whole external backend.

## It already has what the PRD deferred

herddash's PRD puts authentication and transport security in a later phase and ships v1 on
loopback with neither. This project has both already:

- Session-token authentication with an expiry policy and constant-time credential comparison.
  Note the shipped default is `localhost_no_auth: true`, so loopback is unauthenticated out of
  the box, the same posture as devswha's project. The machinery exists; it is simply not on.
- HTTPS with `off`, `auto`, `self-signed`, `files` or `both`, generating its own certificate.
- Service installation and update for macOS and Linux, from the binary itself.
- Published per-platform tarballs with SHA-256 checksums alongside them.

## The licence is a hard blocker for reuse

**AGPL-3.0-or-later.** herddash is MIT. No code, and no closely-derived structure, can be taken
from this project without relicensing herddash. It can be read for understanding; it cannot be
copied from.

There is also a provenance oddity worth knowing before relying on it. Its `LICENSE` opens with
"Herdr is dual-licensed", describes a commercial option, and directs commercial enquiries to
`hey@herdr.dev`, which is herdr's own domain. The repository is not a GitHub fork of
`herdrdev/herdr` and states no relationship to it, yet it reimplements herdr's wire protocol and
its release notes describe mirroring upstream herdr semantics. Either the licence file was copied
from herdr carelessly, or the project is a derivative. **Resolve that before depending on it, and
certainly before contributing to it.**

## Scorecard: what survives after both evaluations

| Claimed differentiator | devswha | alecuba16 | Net |
|---|---|---|---|
| A daemon that works while detached | neutralised | neutralised | **gone** |
| Triage rather than remote control | largely neutralised | holds; this is a terminal and file-browser UI with no triage view | **weak** |
| Persisted history | holds | holds; only a settings file, no database, no transition history | **holds** |
| Per-account attribution | holds narrowly | holds; no awareness of `CLAUDE_CONFIG_DIR` at all | **holds** |
| One static binary, deployable per host | holds | **neutralised**; single Rust binary, checksummed tarballs, service installers | **gone** |

**Two of five survive: a persisted history of what agents did, and attributing a pane to the
account that owns it.** Neither existing project keeps any status history, and neither labels a
pane by subscription.

## What this means

That is a feature pair, not a product. The honest readings are:

1. **Contribute upstream.** Add history and per-account attribution to devswha's project, which is
   MIT and already does everything else well. The licence permits it and the user base exists.
2. **Build herddash as a narrow complement.** A headless history recorder with per-account
   attribution, exposing a small page of its own, running beside whichever browser interface the
   operator prefers. This keeps the MIT licence and the single-binary property, and it is a much
   smaller build than the current PRD.
3. **Build the full product anyway**, accepting that most of it duplicates mature work.

The second is the most defensible, and it is a materially different project from the one §7 through
§12 of the PRD describes. **This decision belongs in an ADR before milestone 1.**

Not evaluated: DebugZHY/Herdr-Dash, and the three unofficial Go socket clients. The Go clients
remain interesting for schema-generated types regardless of the product decision, subject to their
own licences.
