# Reconciliation: docs/prd.md (2026-10-05 draft) against the reworked PRD and addendum

Compared against `prd.md` and `addendum.md` in this folder, under ADR-0001 (option B) and ADR-0003
(the board is the product, read-only, no output). Only in-scope losses are listed as gaps.
`.memlog.md` was not read.

## Gaps

### G1. Backfill is demoted, but ADR-0001 says it became important. Severity: HIGH
Source (ADR-0001, Consequences, which ADR-0003 says stands): "Open question 3, whether to backfill
from the existing plugin's weeks of logged transitions, becomes important rather than optional.
That log is the only historical data that exists."
Old PRD OQ3: "has logged weeks of `blocked` and `done` transitions. Worth importing as initial
history, or not worth the code?"
New PRD §6.2 puts backfill "Out of Scope for MVP ... Deferred to a one-shot import command after
the recorder is trusted", and §8 OQ2 asks "Import ... or ignore?", which reopens what the ADR
settled.
Land: PRD §6.2 and §8 OQ2. Either state that backfill is committed work with a named milestone
(addendum §J, after milestone 2), leaving only the spurious-`done` handling open, or record an
explicit decision that overrides the ADR's wording. Addendum §D already holds the counts and the
pre-0.9.2 spurious-`done` caveat; keep those.

### G2. Open questions the ADR already resolved are shown as open. Severity: MEDIUM
Source (ADR-0001, Consequences): "Open question 4 resolves: that plugin is superseded by this
daemon and should be retired once it works, because they would record the same events twice."
"Open question 5 resolves by precedent. devswha keeps a finished-pane set beside a server
generation marker; the same shape works here."
New PRD §8 OQ3 says "ADR-0001 leans retire", which understates a resolution. OQ1
(acknowledgement) is open and §6.2 defers it, though the ADR said the devswha shape "works here".
Land: PRD §8 (move OQ3 to a recorded decision with the rationale "would record the same events
twice"). For OQ1, keep deferral as a scheduling choice but cite the ADR's resolved shape, and add
the plugin-retirement step to addendum §J milestone 5.

### G3. A Box gives no way to locate the pane in herdr beyond project directory. Severity: MEDIUM
Source (old §7.2): "where the pane lives, and its ids, for finding it in the terminal". Old §7.1
grouped "by workspace and tab".
New: Boxes group by workspace only (§4.1); FR-2 shows Profile, program, directory, class, wait,
Context Line. UJ-1 has Jason "switch to that pane in herdr", and with several panes in one project
directory nothing on the Box says which. The detail page is a deliberate cut, but the need to act
on what the Board shows survives it.
Land: PRD §4.1 FR-2 (tab label as a grouping header or in the Box) or an explicit decision under
§9 that directory plus Context Line is enough. Tab labels and `pane_count` are already in the
snapshot (addendum §C).

### G4. UJ-4 (phone, status widget) conflicts with loopback-only and loses its rationale. Severity: LOW-MEDIUM
Source (old §5): "A later phase adds the same operator on a phone over a private network, which is
why §9 exists now." Old §9: "The bind address is configurable so the private-network phase is a
configuration change rather than a rewrite, but shipping a non-loopback bind without
authentication is out of the question." "Authentication ... gets its own decision record before
any code."
New: FR-15 keeps a configurable bind address; FR-17 refuses non-loopback without auth; addendum §G
says "there is no non-loopback mode" in v1. UJ-4 names phone as a consumer without saying it is
blocked on the authentication ADR.
Land: PRD UJ-4 (one sentence: requires the auth ADR first) and addendum §G (why the bind address
is configurable at all).

### G5. Rationale for rejecting plugin hooks was swapped for a weaker one. Severity: LOW
Source (ADR-0001, rejected alternatives; old §8): "Rejected on capability, not taste.
Output-related events are deliberately excluded from plugin hooks, and a hook naming one gets a
warning and silently never fires."
New addendum §I gives only "fresh process per event" and "registry is Ansible-managed". Moot for
v1 since output is not read, but it is why a later output feature cannot move to a plugin.
Land: addendum §I, one clause.

### G6. Smaller context lost. Severity: LOW
- Toast limits: "Popups for the active tab are always suppressed. Hard-coded, no setting."
  "Only two agent transitions notify at all: entering `blocked`, and a background completion into
  `idle`." PRD §1 keeps suppression, detached silence, cap of 8 and 8 seconds, but drops
  "hard-coded, not configurable" and the two-transition limit. Land: PRD §1.
- Why `done` vs `idle` is untrustworthy: "each terminal client tracks viewed completions
  independently". Addendum §E keeps only the observed flip. Land: addendum §E.
- Old goal wording: "noticing a blocked agent sooner than the toast would have told them,
  including while fully detached." SM-2 measures the ten-minute miss but not detached operation
  (FR-6 does test it). Land: SM-2 validation note.
- Prior-art licence constraint: alecuba16's token auth, HTTPS and installers cannot be copied into
  an MIT project (AGPL). Addendum §I covers forking only. Land: addendum §G or §I.
- Title glyph shape (`◐ <task>`) from the old draft is superseded by the 2026-10-13 measurement of
  session-name titles; no action.

## Carried over intact (checked)
Protocol constraints (one request per connection, no global agent-status subscription, subscribe
before snapshot, all-or-nothing subscribe, two naming conventions, `events_lost` recovery with a
separate snapshot connection, reconnect forever, reconcile timer, ping gate); never call focus;
Host-namespaced ids; 0600 files and loopback default; finite Retention; Go and stack table; types
generated from `herdr api schema`; the reference plugin for attribution; longest-waiting-first
sort and blocked dominance; read-only API; milestone 1 scope.

## Intentionally dropped (scope cuts, audit trail)
- Old §7.1 "short excerpt of recent output" on each row: cut by ADR-0003 and PRD §5 (no output).
- Old §7.2 detail page (timeline, longer output window, `agent.explain`, ids): page deferred to v2
  (PRD §6.2); timeline survives as the FR-14 API; output and `agent.explain` cut.
- Old §8.2 activity capture and the unbounded-`recent` trap: moot because output is never read;
  trap kept as a conditional in addendum §B.
- Output retention clause (old §8.3): moot with no captured output.
- Old milestone 4 "Activity": removed; the Board became milestone 4.
- Old §10 "future write actions become ordinary POST endpoints": removed with the read-only stance.
- Old differentiator table and options A/B/C: now ADR-0001 only.
- Old note that 0.9.3 is a two-line hotfix: replaced by "0.9.2 or newer" plus the 0.8.2 advisory
  gate.
