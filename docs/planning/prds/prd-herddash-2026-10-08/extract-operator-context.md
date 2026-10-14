# Operator context extract

Sourced from `docs/research/herdr-on-this-machine.md` and `docs/research/herdr-multi-host.md`
(both recorded 2026-10-05, frozen records). Quoted where precision matters. No recommendations.

## 1. Operator's setup

Three machines, eventually: an Ubuntu desktop, an Ubuntu laptop, and this Mac — provisioned so
"the three feel identical" (source of herddash's single-binary requirement).

This Mac: herdr **0.8.2** (Homebrew, unpinned); current stable is 0.9.3, "four releases behind."
Upgrade "deliberately deferred: the herdr server hosts live agent work, and restarting it is
disruptive" — `herdr update --handoff` / `herdr server live-handoff` are "the route to take when
the time comes."

Working-state snapshot: 10 agent panes, 15 total panes, 9 tabs, 3 workspaces, "span[ning] several
Claude Code subscriptions plus Codex, which is why per-account attribution matters here."

Four launcher profiles: `cldp` (`CLAUDE_CONFIG_DIR=~/.claude-cldp`, adds
`--dangerously-skip-permissions`), `cldw` (`~/.claude-cldw`, different subscription, prompts for
permissions), `claude` (`~/.claude`, default), `~/.claude-oneio` (additional profile). Agent
integration installed "only for the stock `~/.claude`"; wrappers "deliberately **not** integrated,
because herdr restores a pane with a literal `claude --resume <id>` and no `CLAUDE_CONFIG_DIR`,
which would relaunch Claude Code under the wrong subscription." Integrations "only provide
session identity" — status "always comes from screen detection."

## 2. Existing plugin (reference implementation / backfill candidate)

An Ansible-deployed herdr workflow plugin, linked and enabled, fires on
`pane.agent_status_changed`, meant to POST `blocked`/`done` to a webhook. "It has no endpoint
configured, so it posts nothing and only logs" — deliberately, URL kept out of git. Logging for
weeks: as of 2026-10-05, 1627 `done` and 288 `blocked` events, each carrying "the agent, the
resolved launcher profile, the status, the pane id and the workspace id." Attribution: "by reading
`CLAUDE_CONFIG_DIR` from the pane's foreground process and mapping it through a rendered profile
map" — the reference implementation. Caveat: many logged `done` events "are probably spurious";
0.9.2 fixed false `done` on startup/restore, and "this machine predates that fix."

## 3. Install/edit constraints

herdr config is "not hand-managed" — owned by an Ansible repo; "Do not hand-edit herdr's
configuration on this machine" (repo "mid-migration," no current home for new changes). The
managed plugin registry already carries the plugin above; prior-art spike servers ran unmanaged
from their own directories — "Neither project was installed as a herdr plugin."

## 4. Multi-host

Multi-host is "client-side SSH federation, not server aggregation": each host runs its own
server; "No herdr server ever learns that another exists." No machine dimension in any API
payload; ids "collide across machines." The one bridge (`herdr --machine`, 0.9.1+) is CLI-only,
polling-only, one machine at a time, no cross-machine subscriptions. Decision: "One daemon
instance per host, each on its own local socket, aggregated by the web tier," ids namespaced by
host; "Aggregation across daemons is out of scope for v1."

## 5. Dated / UNVERIFIED

Both files recorded 2026-10-05. `herdr-multi-host.md` is **entirely UNVERIFIED by direct test** —
drawn from 0.9.3 docs/source, since multi-host postdates the 0.8.2 server here and "there is no
second host configured to try it against"; two points confirmed first-hand (no `machine_id` in
local snapshots; no `machine` subcommand at 0.8.2). `herdr-on-this-machine.md` is first-hand
observation.
