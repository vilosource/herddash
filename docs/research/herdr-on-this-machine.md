# The herdr setup this project grew out of

**Recorded 2026-10-05.** Frozen notes on the author's Mac as it actually is. herddash exists
because of this setup, and none of it is visible from the herddash repository, so it is written
down here rather than rediscovered.

## Versions and who owns them

herdr **0.8.2**, installed by Homebrew, unpinned. Current stable is 0.9.3, so the machine is four
releases behind.

The upgrade is **deliberately deferred**: the herdr server hosts live agent work, and restarting it
is disruptive. `herdr update --handoff` and `herdr server live-handoff` exist for exactly this and
are the route to take when the time comes.

herdr's configuration is **not hand-managed**. It is owned by an Ansible repository, which writes
exactly two keys into `~/.config/herdr/config.toml` with `ini_file` rather than templating the
file, because herdr writes `onboarding` itself and the operator owns the keybindings:

```toml
onboarding = false

[ui.toast]
delivery = "system"
```

The `[keys]` block in that file is the operator's own. **Do not hand-edit herdr's configuration on
this machine.** Changes belong in the provisioning repository.

That repository is mid-migration. herdr currently has no role in the active repo and appears only
as a row in its tools catalogue; the designed home is a step that is not yet built. So herdr
configuration changes have no home today, which is worth knowing before suggesting one.

## How notifications work today, and why that is not enough

herdr classifies each agent pane by reading its screen, then raises its own toast. Delivery is
`system`, which on macOS tries `terminal-notifier` first and falls back to `osascript`.
`terminal-notifier` is deliberately absent, because Homebrew re-signs it ad hoc and recent macOS
then refuses it notification authorization, so every alert takes the `osascript` path. A
consequence: clicking a notification does not focus the waiting pane.

The limits that motivated this project, none of them configurable:

- popups for the **active tab** are always suppressed
- delivery needs a **foreground attached client**; a detached session is silent
- at most **8** notifications queue, oldest dropped
- only **two** transitions notify: entering `blocked`, and a background completion into `idle`

## There is already an event recorder on this machine

The same Ansible repository deploys a herdr workflow plugin, linked and enabled, firing on
`pane.agent_status_changed`. It would POST to a webhook for the `blocked` and `done` states. **It
has no endpoint configured, so it posts nothing and only logs**, deliberately: the URL is kept out
of git.

Its log has been accumulating for weeks and is a real dataset:

| Logged state | Count as of 2026-10-05 |
|---|---|
| done | 1627 |
| blocked | 288 |

Each line carries the agent, the resolved launcher profile, the status, the pane id and the
workspace id. **It already solves the per-account attribution problem** that is one of herddash's
two surviving differentiators, by reading `CLAUDE_CONFIG_DIR` from the pane's foreground process
and mapping it through a rendered profile map. That plugin is the reference implementation for that
feature, and its log is the backfill candidate in PRD open question 3.

Note that many of those `done` events are probably spurious. herdr 0.9.2 fixed false `done`
notifications on startup and restored sessions, and this machine predates that fix.

## Agent integrations

The herdr agent integration is installed only for the stock `~/.claude`, reporting `current (v8)`.
The three wrapper profiles are deliberately **not** integrated, because herdr restores a pane with a
literal `claude --resume <id>` and no `CLAUDE_CONFIG_DIR`, which would relaunch Claude Code under
the wrong subscription.

Those integrations only provide session identity in any case. Claude Code sits in herdr's
session-identity tier, not the lifecycle-authority tier, so its status always comes from screen
detection.

## The workload herddash is sized for

A snapshot taken while writing the PRD, which is a normal working state rather than a peak:

| Dimension | Count |
|---|---|
| agent panes | 10 |
| total panes | 15 |
| tabs | 9 |
| workspaces | 3 |

Those span several Claude Code subscriptions plus Codex, which is why per-account attribution
matters here and would not matter to a single-account user.

## Three machines, eventually

The provisioning repository manages an Ubuntu desktop, an Ubuntu laptop and this Mac, and its whole
purpose is that the three feel identical. So anything herddash ships will eventually be installed
by that repository on all three, which is where the single-binary requirement comes from.

Cross-host aggregation is **not** something herdr gives us. See
[herdr-multi-host.md](herdr-multi-host.md): one daemon per host, aggregated by our own web tier.

## Spike leftovers on this machine

From evaluating the prior art:

| Item | State |
|---|---|
| `~/GitHub/herdr-web-ui` | devswha's client, cloned and built. Works against 0.8.2. |
| `~/GitHub/herdr-webui` | alecuba16's Rust project, cloned and compiled. Needs 0.9.0. |
| `~/.config/herdr-web-ui` | devswha's small state directory, kept |
| `~/.config/herdr-webui` | **removed**, because its persisted `backend_mode` was a trap |
| Bun 1.4.2 | installed via Homebrew to build devswha's project. **Unmanaged machine state.** |

Neither project was installed as a herdr plugin, so the managed plugin registry is untouched. Both
spike servers are stopped.
