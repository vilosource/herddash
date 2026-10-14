# Reconcile: prior art vs the PRD and addendum

Inputs: `docs/research/prior-art-herdr-web-ui.md` (devswha, run live on 0.8.2) and
`docs/research/prior-art-alecuba16-herdr-webui.md` (alecuba16, AGPL, PARKED, source and artifacts
only). Targets: `prd.md`, `addendum.md`, `extract-prior-art.md`.

Severity: H = changes a requirement, claim or non-goal; M = should be cited or tightened; L = polish.
"Landing" is where the fix goes.

Method note: the PRD cites prior art in only five places (§2.2 non-users, §5 not-a-client, §8 Q1,
addendum E and G, addendum I). Everything below is what the records say that those places omit or
misstate.

## 1. Differentiation claims the records do not support

### 1.1 devswha already does notification, and the PRD never says so (H)

Record: "Its own description is a browser and phone client for herdr: chat and live terminal for
every agent pane, remote hosts over SSH, and **web push alerts**." And: "It ships a 'Needs you'
grouping and a 'Needs input' view, so it is more than a terminal in a browser." The scorecard marks
"Triage rather than remote control" as "Largely neutralised".

PRD: §2.2 "Anyone who wants to drive agents from a browser. devswha/herdr-web-ui does that." and §5
"devswha/herdr-web-ui exists for that and is MIT." This frames devswha as only a driver. §1 sells
herddash as a "notification center" whose fix is the toast's failings (suppressed on the active tab,
silent with no client attached, capped at eight, gone in eight seconds). devswha's push alerts
and server-held socket address at least the "no client attached" part (the record says so for the
socket: "it does not need an attached herdr terminal client").

Landing: §1 or a new short "Why not devswha" paragraph; §2.2; extract-prior-art.md already lists
push and Needs-you but the PRD dropped them. The honest claim is "herddash is the board without the
driver, plus history and Profile", not "nothing else notifies". The record never says whether
devswha's push covers `blocked` promptly or how its Needs-you list is ordered (no waiting time
recorded), so the defensible differentiator is ordering by Waiting Time and the recorded Transition
behind it, and the PRD should say that is untested against devswha's UI.

### 1.2 "Goes further" on security is thinner than the addendum says (M)

Addendum G: herddash "matches the loopback-only default and goes further in one way: it refuses to
start with a non-loopback bind and no authentication (FR-17)". But PRD §5 says there is no
authentication in v1 at all, so the refusal is vacuous: there is no configuration in which a
non-loopback bind is legal. Record: both projects' only default protection is loopback; alecuba16
"the machinery exists; it is simply not on". The real, supportable advantage is different and
unclaimed: devswha reports role `drive` (anyone reaching the port can send text and keys to live
panes); herddash is structurally read-only and never reads pane output. That is a materially lower
blast radius if loopback is breached, and it is the security lesson the records actually teach.

Landing: addendum G; FR-17 rationale. Severity M.

### 1.3 "Neither keeps any history" rests on one running evaluation (M)

Record: devswha was run live ("No database and no status-transition history"). alecuba16 was
"PARKED ... the product itself could not be exercised"; its history verdict ("only a settings file,
no database, no transition history") comes from source and artifacts, and the record itself says
"what is missing is any judgement of the interface itself." extract-prior-art.md reports the combined
verdict without that qualifier. Also: Not evaluated at all are DebugZHY/Herdr-Dash and the three Go
clients. PRD should say "no persisted history found in the one project run live and in the source of
the other", not state the gap as settled.

Landing: addendum G or a "Prior art" appendix; extract-prior-art.md "What neither project does".
Severity M.

### 1.4 Single binary is a deployment property, not a differentiator (L)

Record: alecuba16 "single Rust binary, checksummed tarballs, service installers"; only devswha needs
Bun and Node. PRD FR-16 correctly does not claim it as unique; keep it that way. Note in the PRD
that it is a parity feature versus alecuba16 and a real gap only versus devswha. Severity L.

## 2. Lessons and precedents the PRD should cite but does not

### 2.1 Needs-you grouping: PRD cites nothing, and the record gives no mapping (M)

Record: "ships a 'Needs you' grouping and a 'Needs input' view". Two names, not one; the record does
not say which statuses feed either, whether they are the same list, or how they order. PRD §3 and
addendum E define Needs you as `blocked` only and note nothing about devswha using the same label
(or whether "Needs input" is the `blocked` view and "Needs you" something broader, such as blocked
plus finished). The shared name is a precedent worth citing, and an unverified gap worth closing
cheaply: open the running devswha UI (it works on 0.8.2) and record the mapping before ADR/architecture
treats "Needs you = blocked only" as settled. Landing: addendum E; open question 1. Severity M.

### 2.2 Finished-pane set + generation marker: PRD copies a shape it has seen empty (H)

Record: file `~/.config/herdr-web-ui/completions.json`, sample
`{"herdr":"16777232:799919","finished":[],"finishedAgents":{}}`. Prose: "A server generation marker,
plus a set of finished panes and finished agents." The record's own wording is "a working precedent
for the concept", observed in one **empty** sample.

PRD §8 Q1 and addendum E say herddash "can copy that shape if needed" and describe it as "a server
generation marker beside a set of finished pane ids". What the record does not establish, and the
PRD should not imply:

- how entries get in (operator click, automatic on `pane.focused`, on transition to `done`) and how
  they leave (the record lists `pane.focused` among its subscriptions, which suggests focus is an
  input; that would be a seen-state coupling herddash forbids by FR/§5 "Never calls focus");
- what the marker value is (`"16777232:799919"` looks like a pid or boot id plus a number, an
  inference only) and what happens on mismatch (presumably: discard the set because pane ids may be
  reused after a server restart; unverified);
- that `finishedAgents` is a map, not a set, and the PRD drops it entirely (the addendum says only
  "a set of finished pane ids").

Lesson that is supportable: pane ids are not stable across herdr server restarts, so any
persisted per-pane flag needs a server-identity key. That bears on herddash independent of
acknowledgement, because Agent ids in the recorder and in Host-namespaced ids must survive herdr
restarts and handoffs (FR-8). Landing: addendum E, FR-8 and the Agent identity section; OQ1 should
cite the generation marker as the reason, not just the file shape. Also: to read the semantics,
exercise devswha's UI against the live session and watch `completions.json` change; do not read the
code as a template without checking the licence (MIT, so permitted). Severity H, because OQ1 is the
open question most likely to force a schema change and the schema is an ADR trigger.

### 2.3 Reconnect and events_lost: the records give no precedent (M)

Neither record says anything about reconnect, backoff, or `events_lost`. What they do give:

- devswha ran fully on 0.8.2 with its subscription set (`pane.agent_status_changed`, `pane.created`,
  `pane.closed`, `pane.exited`, `pane.focused`) and `session.snapshot`. That is a first-hand precedent
  that the event and snapshot calls work on 0.8.2, which supports the dogfooding gate for 0.8.2 and
  the §2.2 non-user line "tested against 0.8.2".
- It uses `session.snapshot` plus lifecycle subscriptions, which is the same snapshot-then-events
  shape as FR-7. No evidence it reconciles on a timer or handles `events_lost` (which is 0.9.2+ and
  UNVERIFIED in the addendum). Do not say the prior art validates resync.
- The pane lifecycle list is the only evidence on how a per-pane status subscription set is kept
  current (project `CLAUDE.md`: no global status subscription). devswha's list contains the lifecycle
  events but the record does not say it subscribes per pane; that is inference.
- alecuba16 shows a failure mode worth designing against: on a version mismatch it "silently started
  its own embedded multiplexer", wrote `"backend_mode": "builtin"` to its settings file, "overriding
  the mode passed on the command line and persisting that override", and its `/api/versions` kept
  reporting `external-herdr`. Lessons for herddash: (a) a disconnected or unsupported state must be
  visible on the Board, which FR-5/unsupported-version strip already covers, so cite it as the reason;
  (b) never persist a fallback decision; (c) any health or version endpoint must derive from the
  state actually in use, not from configuration (the "API probe was misleading" finding); (d) the
  integrated check the record recommends is "the listed session's backend must read external-herdr",
  i.e. test the whole path, which is a test case for milestone 1.

Landing: FR-7/FR-8 rationale and the unsupported-version strip requirement; addendum milestone 1 test
list. Severity M.

### 2.4 pane.process_info is the precedent for Profile resolution (M)

Record: devswha "leans on `pane.process_info` heavily, which is how it inspects what is actually
running in a pane." Addendum D says "`pane.process_info` or equivalent". The record upgrades that
from guess to precedent, but it also notes a scope boundary: devswha reads `CLAUDE_CONFIG_DIR` and
scans `~/.claude-*` sibling directories "to find credentials for more than one account", for usage
panels. Two lessons: (1) herddash's Profile resolver sits close to credential-adjacent data, so FR-17
and the data-minimisation note (G) should state it records only the launcher name and never the
config directory contents; (2) devswha already enumerates `~/.claude-*` directories, so
"attribution" is not a novel capability of reading the env, only of putting it on each Transition.
Landing: addendum D and G; §4.3. Severity M. Also note the extract's claim "reads several Claude config
directories, but only to aggregate plan usage" is in the record; the PRD should keep that wording
because it bounds the claim.

### 2.5 Security bar: state directory and listening port (M)

Record: devswha state directory is `0700`; it writes `bridges/` and `completions.json` under it.
PRD FR-17 and addendum G specify the database and log as `0600` but say nothing about the
containing directory; a `0600` file in a `0755` directory leaks existence and permits replacement.
Specify the directory as `0700`. Also devswha serves `127.0.0.1:7317` and alecuba16 `8788` in the
spike; if herddash runs "beside" devswha (ADR-0003) a default port that collides is a first-run
failure. Pick a default distinct from 7317 and say so. Landing: FR-17; FR-16 configuration.
Severity M.

### 2.6 Co-existence on one herdr server (M)

extract-prior-art.md states "herddash will run its own, separate socket connection beside this one
on the same server." The record does not claim this, and the PRD open question 4 says one request per
connection is "unverified against a 0.9.3 binary". devswha does hold a long-lived connection while the
record was written, and it was run alongside the operator's session, so concurrency of two long-lived
subscribers with the operator's own clients is plausible but the record never tested a second
subscriber. Mark as unverified and add to milestone 1. Landing: addendum A/J, OQ4. Severity M.

### 2.7 devswha's plugin version gate is soft (L)

Record: the 0.9.0 declared minimum "is a packaging assertion rather than a runtime requirement";
herdr refuses to link a plugin newer than the binary, but running the server directly bypasses it. The
PRD's "herdr 0.9.2 or newer, with dogfooding gate for 0.8.2" has the same shape; cite it as
evidence that a hard version refusal is a choice (alecuba16 chose it, to the user's cost) and the
unsupported-version strip as a warning is the better choice. Landing: FR on version strip. Severity L.

### 2.8 Not installing as a plugin (L)

Both spikes were run from clones, deliberately not as herdr plugins. Consistent with project rule.
Nothing to change in the PRD, but the "Backfill from the existing plugin's log" (OQ2) is about a
different, machine-owned plugin: do not conflate the two when citing prior art. Severity L.

## 3. PRD statements the records contradict or that overreach

| # | PRD / extract statement | Record says | Fix | Sev |
|---|---|---|---|---|
| C1 | §2.2 devswha is where you go to "drive agents from a browser" (implied only) | also has Needs-you grouping, Needs-input view, web push | see 1.1 | H |
| C2 | addendum E / §8 Q1: shape is "a server generation marker beside a set of finished pane ids" | marker plus `finished` set plus `finishedAgents` map; one empty sample | see 2.2 | M |
| C3 | addendum G: "goes further ... refuses to start with a non-loopback bind" | v1 has no non-loopback mode at all; real difference is read-only vs role `drive` | see 1.2 | M |
| C4 | extract-prior-art.md: "Needs Bun and Node on every host" (devswha) | record: "needs Bun and Node on every host" in the scorecard, but the setup section says only Bun 1.4.2 was installed, Node not mentioned | soften to "Bun (record also says Node)" | L |
| C5 | extract-prior-art.md: herddash "will run its own, separate socket connection beside this one" | not in record, never tested | see 2.6 | M |
| C6 | extract-prior-art.md: "Not evaluated: ... three unofficial Go clients" used as rejected alternative "All version-0 with stated instability" (addendum I) | record only says they are "interesting for schema-generated types" and "not evaluated"; the version-0 claim comes from the socket API research, not the prior-art records | cite the correct source | L |
| C7 | PRD header: "Section 3 is the honest scorecard" is the old PRD's wording (CLAUDE.md); new PRD §3 is vocabulary | scorecard now lives only in the two records | add a prior-art summary somewhere so the PRD is not silent | M |

## 4. Suggested additions, in priority order

1. A short "Prior art" section or addendum paragraph in the PRD that states, with the record's
   quotes: devswha = driver plus triage plus push, MIT, runs on 0.8.2, no history, no Profile
   labelling; alecuba16 = AGPL replacement, parked, not evaluable here. Then the explicit claim:
   herddash is read-only, Waiting-Time-ordered, recorded, Profile-attributed (H).
2. Rewrite OQ1 to cite what is and is not known about `completions.json` and the generation marker
   as an identity key, and add a milestone-1 task: watch devswha's file change under real use (H).
3. FR-17 and addendum G: `0700` directory, default port distinct from 7317, read-only as the
   security differentiator, minimal Profile data recorded (M).
4. Add alecuba16's silent-fallback failure to the rationale for the disconnected and
   unsupported-version strips and to milestone 1's end-to-end test (M).
5. Mark the second-subscriber coexistence assumption UNVERIFIED (M).
