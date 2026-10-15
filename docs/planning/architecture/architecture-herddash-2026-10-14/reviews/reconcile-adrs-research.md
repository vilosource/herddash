# Reconciliation: spine and ADR-0004..0007 against ADR-0001..0003 and the research records

Reviewed 2026-10-15. Findings only; nothing else edited. Severity: H = would produce a wrong or
unbuildable design, M = silent drop or unflagged claim that will bite in a milestone, L = tidy-up.

## Gaps

1. **H - Events as "invalidation only" contradicts recording every live Transition.** ADR-0006
   says `session.snapshot` is truth and events are "only invalidation signals"; AD-4/ADR-0005
   record `live`-provenance Transitions with herdr ordering. The research's invalidation rule is
   the `events_lost` recovery procedure, not the steady state. If steady state is also
   snapshot-after-event, any Transition shorter than a read round trip is lost and `live` is
   meaningless. The spine must say which: apply the `pane.agent_status_changed` payload as a delta
   in steady state, and use snapshot-as-truth only on start, resync and the timer. It also dropped
   the research rules "do not replay buffered event payloads onto a snapshot" and "refresh again if
   events arrive while a read is in flight"; "at most one resync" (AD-5, ADR-0006) is weaker than
   that.

2. **H - Attribution ignores the restored-pane hole and never says how pane maps to process.**
   herdr-on-this-machine.md: restored panes relaunch as a literal `claude --resume <id>` with no
   `CLAUDE_CONFIG_DIR`, which is why the wrapper profiles are not integrated, and the plugin reads
   the variable from the pane's *foreground process*. The spine has `internal/procenv` reading "a
   process's environment" but (a) never states the pane-to-pid step (the snapshot fields the
   research lists carry `foreground_cwd`, no pid), (b) has no rule for a restored pane, which will
   look like the stock `~/.claude` account unless an explicit "unattributed" Profile exists, and
   (c) does not note that attribution can be wrong, not just absent. ADR-0005 stores "Profile as
   resolved then" so a mis-attribution is permanent. Needs an Attribution rule in the spine
   (unknown beats guess) and a milestone-1 question on where the pid comes from.

3. **M - ADR-0006 asserts as fact what the research marks UNVERIFIED or contradicts.** Only a
   header sentence disclaims; the Decision and Consequences are unqualified, and the spine has no
   provenance at all (it cites only the research file in `sources`). Specifically: "the only design
   the protocol allows" for one connection per pane is wrong, since `events.subscribe` takes an
   array of subscriptions on one connection (the cost is all-or-nothing rejection, which is
   UNVERIFIED); "Subscriptions replay nothing" (0.9.0, UNVERIFIED, and ADR-0006 itself says 0.8.2
   replays); `events_lost` shape and closing (0.9.2, UNVERIFIED); no connection cap (UNVERIFIED);
   the never-focus and no-unbounded-read rules (UNVERIFIED, deliberately complied with, cost
   asymmetric, which is the actual justification and should be quoted); one request per connection
   verified on 0.8.2 only, with the research's "verify this first" for 0.9.3 dropped. There is also
   no plan for where the 0.9.x verification happens when the operator's machine cannot be upgraded
   (a side-by-side 0.9.x binary on a named session is the obvious route and is not mentioned, nor
   is the "do not install as a plugin" constraint it must respect).

4. **M - Protocol facts dropped.** Not in the spine or any ADR: the two push-envelope conventions
   (underscored `{event,data}` for lifecycle, dotted for the three pane-scoped streams, redundant
   `type` in `data`), which a decoder must handle; that `ping.protocol` is the binary-attach
   protocol and must not be gated on (AD-5 and ADR-0006 say only "version-gate on ping"); the
   `pane.agent_detected` / `pane.created` / `pane.closed` trio that actually drives the pane set
   (ADR-0006 says "global lifecycle subscriptions"); error `code` being an open set (spine's
   Errors row has it, ADR-0006 does not); `herdr-client.sock` is not the API socket; Server name
   derivation when the path comes from `HERDR_SOCKET_PATH` rather than a session name (AD-3 only
   defines it for the two default layouts); the `0600` mode of herdr's socket is never turned into
   a startup check. Also ADR-0006 says "about 15 Agent panes"; the research records 15 total panes
   and 10 agent panes.

5. **M - The spine restates ADRs and mis-cites one, against ADR-0002.** AD-8 cites ADR-0004, but
   ADR-0004 only chooses the two dependencies; config precedence, fail-fast validation and restart
   semantics are in no ADR. AD-4, AD-5, AD-6 reproduce ADR-0005/6/7 rule text (field list, routes,
   protocol steps) instead of pointing to them; AD-7 (SSE no replay) and AD-10 (migrations,
   stored-event ADR rule) duplicate ADR-0007 and ADR-0005 without citing them; AD-3 is a stored
   identity and belongs under ADR-0005; the Stack table repeats ADR-0004 versions. Go 1.26 in the
   spine conflicts with CLAUDE.md's "Go 1.25 or newer". ADR-0002 is missing from `sources`.
   ADR-0005..0007 are "Accepted" with no operator decision recorded (ADR-0004 and 0003 do record
   one); they should be Proposed until the operator accepts.

## Checked and fine

- ADR-0003 restraint: AD-1 (Board computes nothing), AD-6 (GET only, no CORS), ADR-0007 hold the
  read-only line. The spine never mentions ADR-0003 or SM-C1 as the guard, and "future surface"
  language in AD-1 is the only place scope could creep; low risk.
- Config ownership: no spine path edits `~/.config/herdr/config.toml`; deployment files are left to
  the provisioning repository; launchd unit sets no `HERDR_SOCKET_PATH` (matches research).
- Seen state / output rules: satisfied by absence of focus and pane-output methods. The
  `visible` or `recent` bounded by `viewport_rows` rule is not recorded anywhere, so a future ADR
  adding output reads would not find it; one line in ADR-0006 would do.
- Multi-host: one Daemon per Host and host-namespaced ids match herdr-multi-host.md; the research
  said aggregation gets its own decision record and the spine defers it as such.
- Socket resolution order is stated correctly in AD-8 and ADR-0006.
- Backfill: the research notes many pre-0.9.2 `done` events are probably spurious; ADR-0005 has the
  flag but neither it nor the importer entry says the importer sets it.
