# Extract: prior art, for requirements

Source records: `docs/research/prior-art-herdr-web-ui.md`,
`docs/research/prior-art-alecuba16-herdr-webui.md`.

## devswha/herdr-web-ui

MIT. Runs fully on herdr 0.8.2 despite declaring a 0.9.0 minimum — the version gate is a plugin
packaging assertion, bypassed by running its server directly rather than installing it as a plugin.
Already does, and herddash must not rebuild: live terminal and chat per pane (xterm.js), a "Needs
you" grouping and "Needs input" view (triage, not just remote control), remote hosts over SSH, web
push alerts. Needs Bun and Node on every host — no single-binary deployment. Server holds its own
herdr socket connection, so no attached terminal client is required.

## alecuba16/herdr-webui

**AGPL-3.0-or-later, with a commercial option.** "No code, and no closely-derived structure, can be
taken from this project without relicensing herddash. It can be read for understanding; it cannot
be copied from." Cannot be evaluated against 0.8.2: it enforces `MIN_BACKEND_VERSION` 0.9.0 and,
pointed at 0.8.2, "silently started its own embedded multiplexer" and showed an empty session while
the top-level API still reported `external-herdr` — status PARKED. From source/release artifacts
(stand regardless of the version block): single static Rust binary with per-platform checksummed
tarballs and service installers for macOS/Linux — herddash's single-binary property is not a
differentiator here. Also ships a terminal/file-browser UI with no triage view.

## What neither project does

Record's own scorecard: "Persisted history — holds[, i.e. gap survives]. There is no database and
no status-transition history. Its only persistence is configuration and the finished-pane set. No
timeline exists" (devswha). alecuba16: "holds; only a settings file, no database, no transition
history." Per-account attribution: devswha "reads several Claude config directories, but only to
aggregate plan usage. Panes are not labelled by the subscription that owns them" — holds narrowly;
alecuba16 "no awareness of `CLAUDE_CONFIG_DIR` at all" — holds. Combined verdict: "Two of five
survive: a persisted history of what agents did, and attributing a pane to the account that owns
it. Neither existing project keeps any status history, and neither labels a pane by subscription."

## The ack precedent (devswha)

Store at `~/.config/herdr-web-ui/completions.json`: `{"herdr":"16777232:799919","finished":[],
"finishedAgents":{}}` — a server generation marker (`herdr` key) paired with a set of finished pane
ids and a set of finished agent ids. "A working precedent for the concept herddash said it would
have to invent."

## How devswha's project talks to herdr

Its server holds the herdr socket connection directly (no attached terminal client needed).
Subscribes to: `pane.agent_status_changed`, `pane.created`, `pane.closed`, `pane.exited`,
`pane.focused`. Calls: `session.snapshot`, `agent.get`, `pane.process_info`, `pane.read`,
`pane.send_text`, `pane.send_keys`. herddash will run its own, separate socket connection beside
this one on the same server.

## Security posture (sets a bar)

devswha: "No authentication by default," role reported as `drive`; loopback bind
(`127.0.0.1:7317`) is the only protection in the default configuration; state dir created `0700`;
reaches external provider APIs (Anthropic, OpenAI, GitHub Copilot, Cursor, xAI) for optional usage
panels, and scans `CLAUDE_CONFIG_DIR`-sibling directories for credentials. alecuba16 has
session-token auth with expiry and constant-time comparison, plus HTTPS modes — but "the shipped
default is `localhost_no_auth: true`, so loopback is unauthenticated out of the box, the same
posture as devswha's project. The machinery exists; it is simply not on."

## Parked / UNVERIFIED / version-dependent

alecuba16 evaluation is explicitly **PARKED**: "requires herdr 0.9.0 and this machine runs 0.8.2,
so the product itself could not be exercised"; its interface was never judged, only source and
release artifacts. Its LICENSE text's "dual-licensed"/commercial-contact claim pointing at herdr's
own domain is flagged as an unresolved provenance question, not fact. Not evaluated at all:
DebugZHY/Herdr-Dash, and three unofficial Go socket clients.
