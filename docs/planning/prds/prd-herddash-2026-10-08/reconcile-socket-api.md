# Reconcile: socket API and multi-host research vs PRD and addendum

Inputs: `docs/research/herdr-socket-api.md` (SA), `docs/research/herdr-multi-host.md` (MH).
Targets: `prd.md`, `addendum.md`, `extract-socket-api.md`.
Severity: H = will mislead architecture or break a testable requirement; M = real gap, cheap fix;
L = polish. "Landing" is where the fix belongs.

## 1. Gaps: constraints in the research that the PRD/addendum lack

### G1 (H). 0.8.2 behaves differently from the 0.9.x the design assumes, and the dogfood gate runs on 0.8.2
- SA: "Since 0.9.0, a new lifecycle subscription starts with live events and no longer replays
  retained history. Earlier versions flooded a new subscriber with stale events."
- SA: `events_lost` is "Added in 0.9.2"; "not testable against 0.8.2".
- PRD FR-16 makes the gate advisory on 0.8.2 so the Board can run there. Addendum B states the
  0.9.x behaviours but never says what the Daemon must do when `ping` reports < 0.9.0.
- Consequences not written down: on 0.8.2 a new subscription may replay stale events, which FR-6
  would record as "observed live"; `events_lost` can never fire, so only the reconcile timer
  protects FR-7; FR-8's "herdr restarted: no Transition recorded" test and FR-7's "after herdr
  signals it fell behind" test cannot pass or run on 0.8.2; the 0.9.2 spurious-`done` fix
  (herdr-on-this-machine.md line 69, not in either input) is absent, so restarts on 0.8.2 emit
  false `done`.
- Landing: addendum B (a "behaviour by version" table: <0.9.0, 0.9.0-0.9.1, >=0.9.2); PRD FR-16
  consequence (what the 0.8.2 flag degrades) and FR-6 (mark replayed/early events as not-live).
  Milestone 1 in addendum J.

### G2 (H). Namespace key is Host only, but ids are scoped to a herdr server, and a host can run several
- SA: sockets are `~/.config/herdr/herdr.sock` (default) and `.../sessions/<name>/herdr.sock`
  (named session); resolution is `--session`, `HERDR_SOCKET_PATH`, `HERDR_SESSION`, default.
- MH: "workspace, tab and pane ids, and agent names, are scoped to one server"; "One profile
  targets one remote session, not a whole host."
- PRD glossary: "herdr ... Runs one herdr server per host"; "Every id herddash stores is
  namespaced by Host." API path is `/api/panes/{host}/{pane}/history`.
- Gap: two named sessions on one machine both have `w1:p1`. The PRD's unit of namespacing should be
  Host plus herdr server/session, or the one-server-per-host assumption stated as a v1 scope limit
  with the schema keyed so it can widen. Config (FR-15) says "socket location", which hints at it
  but never ties it to identity.
- Landing: PRD glossary (Host definition), FR-6 record fields, addendum A path shape; ADR trigger
  per CLAUDE.md ("change to the stored event schema").

### G3 (M). Socket-path resolution under a service manager
- SA: "A process launched inside a herdr pane is given `HERDR_SOCKET_PATH` and friends... A daemon
  started by launchd or systemd is not, and must resolve the path itself."
- PRD FR-15 says "defaulting to herdr's own discovery" and milestone 5 runs it as a service unit.
  Not stated: the service has no env, must apply the SA resolution order itself, and the socket is
  `0600`, so it must run as the same user (the addendum says same user for process environments but
  not for the socket mode).
- Landing: addendum B or a new bullet beside FR-15.

### G4 (M). Fan-out topology consequences: connection count, resync storms, per-pane race
- SA: `events_lost` uses a shared history, "so it can fire even when the evicted events would not
  have matched your filters, and it can fire while subscriptions are still initialising."
- SA: "Whether any hard concurrent-connection cap exists. None found" (open item, UNVERIFIED).
- Addendum A: "per-pane subscriptions ... (one connection each)." Not drawn out: with N panes plus
  the global stream, one slow period can drop many connections at once, each needing a resync;
  refreshes must be serialised across all of them (SA: "Serialise refreshes"), not per connection.
  Also a new pane needs subscribe-then-read-its-status (the subscribe-before-snapshot rule applies
  per pane, not only at connect), and a pane closing between `pane.created` and its subscribe is an
  expected failure, not a fault.
- The addendum's one-connection-per-pane choice avoids all-or-nothing blast radius, but SA's
  example batches subscriptions into one request. The choice is sound; the reason should be stated.
- Landing: addendum B.

### G5 (M). Error codes are an open set
- SA: "`error.code` is an unconstrained string with no enumerated list. Treat the set as open."
- Neither the PRD nor the addendum says unknown codes must be tolerated, nor which are expected
  and benign (`pane_not_found`, `not_found`) versus resync (`events_lost`) versus fatal.
- Landing: addendum B.

### G6 (L). 5 second / 1 MiB initial-line limits
- SA: "reads a single initial request line with a 5 second timeout and a 1 MiB cap."
- Relevant to a client that dials and writes late, and to subscribe requests carrying many
  entries. Landing: addendum B, one clause.

### G7 (L). Seen-state independence between terminal clients
- SA: "Each terminal client tracks viewed completions independently ... `pane.focused` events fire
  on any client's manual selection and the payload does not say which client."
- Addendum E notes `done` vs `idle` is untrusted but not this cause. Strengthens open question 1.
  Landing: addendum E, PRD open question 1.

### G8 (L). Open items from SA not carried
- Whether 0.9.3 `ping` advertises capabilities beyond `live_handoff` and
  `detached_server_daemon`; schema re-dump from a real 0.9.3 binary before freezing types
  (addendum F carries the second, not the first). Landing: addendum F / open questions.

## 2. Already covered, no action (verified against text)
One request per connection (add. A, B, OQ4); no global agent-status subscription (add. B); never
call focus (add. B, PRD non-negotiables); no output reads in v1 and the bounded-`recent` rule
(add. B); `events_lost` recovery and the no-sequence-boundary rule (add. B, FR-7); subscribe before
snapshot; all-or-nothing; two envelope conventions; version gate on `ping` (add. B); MH
conclusion (add. H, PRD non-goals). Host dimension in the record: preserved (FR-6 lists Host, API
path carries `{host}`, glossary namespaces ids); only the Host-vs-server granularity in G2 is open.

## 3. PRD/addendum states as fact what the research marks UNVERIFIED, without saying so

| Where | Statement | Research status | Severity | Landing |
|---|---|---|---|---|
| add. C | "The snapshot rolls status up to tab and workspace with labels and pane counts, which is the Board's grouping." | Not in SA at all. SA lists snapshot members as `workspaces`, `tabs`, `panes`, `layouts`, `agents`; no roll-up is mentioned. | M | Mark as unsourced here or cite the other record; confirm against a real snapshot. |
| add. D | Daemon "obtains the foreground process from herdr's `pane.process_info`" (later hedged: "or equivalent") | Method not mentioned in SA, which confirmed only `ping`, snapshot, subscribe. Provenance unknown from the inputs. | M | Mark UNVERIFIED or point to a source; it gates FR-10 and milestone 2. |
| prd 2.2, FR-16, OQ4 | "herddash targets 0.9.2 or newer" | SA is written against 0.9.3 docs and 0.9.2-era probing; 0.9.x server never tested first-hand. | L | State that no 0.9.x server has been exercised. |
| add. B | "Reconnect indefinitely. herdr restarts, hands off to a new server" | Live handoff is only visible as a `ping` capability (`live_handoff`); behaviour of subscriptions across handoff is not described in SA. | L | Say handoff behaviour is untested. |
| add. B | "Never read pane output in v1 ... (UNVERIFIED)" | Correctly labelled. | none | none |
| prd 4.2 FR-6 | "herdr's ordering information" | SA names `revision` and `state_change_seq` but does not say they are monotonic per pane or survive a herdr restart. | M | Add to add. C: ordering semantics unverified. |
| extract-socket-api.md | Item 1 states `state_change_seq` as the change-detection field | SA says "Use `revision` and `state_change_seq` for change detection" but marks no verification; both are listed only under the confirmed snapshot field list, so field presence is first-hand, semantics are not. | L | Clarify in extract. |
| prd line 96/addendum | Status enum `working, blocked, done, idle, unknown` | SA says enums were read from the schema but does not list them; values come from elsewhere. | L | Cite source. |

## 4. Stated incorrectly or in tension with the research

- **add. A**, "per-pane subscriptions: pane.agent_status_changed (one connection each)" is a design
  choice presented as protocol shape. SA's own example batches into one request. Not wrong, but a
  reader may take it as forced. (L)
- **add. B**, "Version-gate on `ping`, not on a protocol number" is correct. **FR-16** says only
  "herdr's reported version", fine, but should say `ping.result.version` because the snapshot also
  carries `version` and `protocol`, and SA warns that `protocol` is a different protocol. (L)
- **PRD glossary**, "herdr ... Runs one herdr server per host" contradicts SA/MH named-session
  sockets (see G2). (M, tied to G2)
- **extract-socket-api.md section 3**, "Confirmed first-hand: exactly 3 of 27" is accurate, but the
  header paragraph says confirmed facts were verified against 0.8.2 while the "27" count is of the
  0.8.2 bundled schema; 0.9.x may add kinds. Note it. (L)
- **add. H**, "Entirely UNVERIFIED by direct test" overstates slightly; MH says two points were
  checked first-hand (no `machine_id` in a live snapshot, no `machine` subcommand at 0.8.2). (L)

## 5. Host dimension check (multi-host deferred)
Preserved: Host in the Transition record (FR-6), Host in the API path and response (FR-14), Host
namespacing stated as non-negotiable (PRD non-goals, glossary, add. H). MH's decision (one Daemon
per host, aggregation its own record) matches. The one hole is granularity (G2): key on herdr
server identity (Host + session/socket), not Host alone.
