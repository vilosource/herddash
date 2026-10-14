# Reconciliation: herdr-on-this-machine.md against PRD and addendum

Input: `docs/research/herdr-on-this-machine.md` (recorded 2026-10-05, frozen).
Targets: `prd.md`, `addendum.md`, `extract-operator-context.md`.
Scope: ADR-0001 and ADR-0003 (read-only board plus recorder with Profile attribution). Out-of-scope features are not listed.

Severity: H = wrong or contradicts a decision; M = in-scope fact dropped that will matter to build or acceptance; L = nuance.

## G1. Spurious `done` on 0.8.2 is not carried into the advisory-gate design (H)

Quote: "herdr 0.9.2 fixed false `done` notifications on startup and restored sessions, and this machine predates that fix."
PRD handles this only as a backfill concern (§8 Q2, addendum D). FR-16 lets the Daemon record from 0.8.2 during dogfooding, and SM-2, SM-3, UJ-3 ("He trusts it") and FR-6 assume recorded Transitions are true. On the operator's only machine, `done` Transitions after herdr start or session restore may be false, so Waiting Time and Finished emphasis can be wrong.
Landing spot: FR-16 consequences (advisory mode records on a known-defective server); SM-3 caveat; a Transition provenance marker such as `server_version` alongside FR-6's live/snapshot-derived marker.

## G2. Backfill demoted to deferred; prior instruction said it was important (H)

Quote: "its log is the backfill candidate in PRD open question 3." CLAUDE.md records that ADR-0001 made backfill "important rather than optional."
PRD §6.2 defers it to a "one-shot import command after the recorder is trusted," and §8 Q2 offers "ignore" as an option. The log schema the import must accept is not stated: the plugin logs only `blocked` and `done` (not `working` or `idle`), carries no previous Status and no herdr sequence, and no log path is recorded anywhere. Counts (1627 done, 288 blocked) are as of 2026-10-05 only.
Landing spot: §6.2 and §8 Q2 (reconcile with ADR-0001); addendum D (log shape and the record fields backfilled rows cannot supply, FR-6 provenance value "backfilled"); a path-discovery item for the architecture phase.

## G3. No explicit constraint that herddash must not require herdr or Ansible-owned config changes (M)

Quotes: "Do not hand-edit herdr's configuration on this machine. Changes belong in the provisioning repository." "That repository is mid-migration... herdr configuration changes have no home today." Config is `onboarding = false` and `[ui.toast] delivery = "system"`; the operator owns `[keys]`.
PRD §5 has "Not installed as a herdr plugin" and "Does not replace herdr's toasts," but nothing says herddash must work with herdr's config untouched, nor that it must not edit `config.toml`. Plugin retirement (§8 Q3) is an Ansible-repo change with no current home, and ADR-0001 only "leans retire"; the PRD omits that cost. Deployment ownership (provisioning repo installs it on three machines) appears only as FR-15's rationale.
Landing spot: §5 Non-Goals (herddash neither edits nor requires changes to herdr config); §8 Q3 (retirement lives in the provisioning repo, which has no home for it yet); FR-15.

## G4. Profile Map source of truth is duplicated and the profile facts are thin (M)

Quotes: plugin maps `CLAUDE_CONFIG_DIR` "through a rendered profile map"; wrapper profiles are "deliberately not integrated, because herdr restores a pane with a literal `claude --resume <id>` and no `CLAUDE_CONFIG_DIR`, which would relaunch Claude Code under the wrong subscription."
- The plugin's map is rendered by Ansible; herddash's Profile Map is a second hand-maintained copy (FR-11, §4.3). Nothing says they must agree, or that herddash's map should be rendered by the same provisioning step, or that the plugin map can seed it. Without this the plugin and Daemon can disagree during the period both run.
- Restored panes show `claude` (absent case) because the restore command drops `CLAUDE_CONFIG_DIR`. That is correct for what runs, but the Board will show a restored pane under a different Profile than before the restore. FR-10 does not call this out. It is a likely UJ-2 surprise and an SM-4 false failure.
- Profile names `cldp`, `cldw`, `claude`, `oneio` appear only in the addendum, not the PRD, and the facts that distinguish them (`cldp` skips permissions, `cldw` prompts, different subscriptions, `~/.claude-oneio`) are absent. The shipped-config requirement (FR-10) does not list the four entries.
Landing spot: FR-10 and FR-11 consequences; addendum D; shipped default Profile Map content.

## G5. Notification-failure facts only partly carried into the vision (M)

Quotes: popups for the active tab suppressed; foreground attached client needed; "at most **8** notifications queue, oldest dropped"; "only **two** transitions notify: entering `blocked`, and a background completion into `idle`"; "clicking a notification does not focus the waiting pane" (osascript fallback because `terminal-notifier` is deliberately absent); "none of them configurable."
PRD §1 carries the first three limits (the "eight seconds" figure is from the old docs/prd.md, not this input). Missing: only two transitions ever notify (so a `working` to `blocked` or `done` seen on the active tab, or any other transition, produces nothing), and the click-does-not-focus defect. Also missing that these are not configurable, which is the justification for building rather than tuning herdr.
Landing spot: §1 Vision; a one-line problem-evidence note for ADR traceability.

## G6. Workload sizing not stated as a requirement (M)

Quote: "A snapshot... a normal working state rather than a peak: 10 agent panes, 15 total panes, 9 tabs, 3 workspaces" across several Claude Code subscriptions plus Codex.
No NFR or sizing target in the PRD. The per-pane subscription model (one connection each, addendum B) and the 1-second repaint (FR-4) are untested against any stated scale; addendum C2's "15 agents" is a different count on a different date and is all Claude Code, whereas this input says Codex is present.
Landing spot: new NFR in §4.4 or §4.5 ("sized for about 15 panes, with headroom"); addendum A.

## G7. Smaller items (L)

- 0.8.2 constraint: the PRD gives the reason (the server hosts live agent work) only implicitly. The route `herdr update --handoff` / `herdr server live-handoff` and the fact that the version gate flag is the operator's workaround are absent. Add to FR-16 note or §2.2.
- Agent integration is session-identity only, so Claude Code status always comes from screen detection. This explains why `done` vs `idle` is untrustworthy (addendum E) but is not cited there.
- "Three machines feel identical" is captured via FR-15 but not the Ubuntu desktop/laptop and Mac mix; relevant to the `/proc/<pid>/environ` vs macOS sysctl split in addendum D, which is there. No action beyond a cross-reference.
- Spike leftovers (Bun 1.4.2 unmanaged, devswha state directory kept) are out of scope for the PRD; intentionally not carried.
