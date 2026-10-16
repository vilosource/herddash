# herdr socket API — implementation notes

**Recorded 2026-10-05 against herdr 0.9.3 documentation, the v0.9.3 source, and live probing of a
0.9.2-era server.** This is a frozen research record. It is the reference herddash's client is
built from. Where it is wrong, fix the code and amend this file in the same change.

0.9.3 is a two-line hotfix over 0.9.2 affecting terminal escape-sequence key handling, with no API
changes. Everything here applies equally to 0.9.2.

**Amended 2026-10-16 against a herdr 0.9.5 binary, run in isolation (Story 1.3).** The section
[Findings against herdr 0.9.5](#findings-against-herdr-095) answers the architecture spine's four
open questions with evidence, and the provenance lists below have been updated to match. Where an
older statement in this file was wrong, it is corrected in place and the correction is marked.

## Provenance of these claims

Not all of this is first-hand, and the difference matters because some of it is load-bearing.
Treat anything labelled UNVERIFIED as a thing to confirm before it decides a design, not as
established fact.

**Confirmed directly against the installed herdr 0.8.2 on this machine:**

- The socket path, its `0600` mode, and the sibling client socket.
- One request per connection. A second request on the same socket raised a broken pipe. See the
  section below.
- The request and response envelopes, and the `ping` payload including `version`, `protocol` and
  `capabilities`.
- The subscription set: exactly 27 variants in the bundled schema's `Subscription` request type, of
  which exactly three require a `pane_id` — `pane.output_matched`,
  `pane.agent_status_changed` and `pane.scroll_changed`. **This is the basis of the
  no-global-agent-status finding and it is first-hand.**
- The snapshot method, its result shape, and the agent and pane record fields, read from a live
  snapshot.
- The `AgentStatus` and other enums, read from `herdr api schema --json`.

**Confirmed against an isolated herdr 0.9.5 on 2026-10-16** (evidence in the findings section,
re-runnable with [herdr-0.9.5-probe.sh](herdr-0.9.5-probe.sh)):

- One request per connection, still.
- Subscriptions are all or nothing: one unknown `pane_id` fails the whole request with an error
  echoing the request id, and the server closes the connection.
- The two push-envelope conventions, underscored for lifecycle events and dotted for the
  pane-scoped streams.
- A new subscription delivers live events only: subscriptions opened after earlier pane, agent and
  status events received none of them, only `subscription_started` and what followed.
- Error responses echo the request id, including for subscription setup errors.
- The subscription set: still 27 kinds, the same three pane-scoped, in the 0.9.5 schema.

**UNVERIFIED, sourced from a research pass over herdr's published 0.9.3 documentation and its
v0.9.3 source** (`herdr.dev/llms-full.txt` and the `herdrdev/herdr` repository at tag `v0.9.3`),
not reproduced by hand here:

- That reading pane output is not always passive, that an unbounded `recent` read drives a
  recognised agent's own scroll interface, and that such a read returns `agent_not_idle` for a
  working, blocked or unknown agent. **This was deliberately not tested, because the only way to
  test it is against a live agent pane belonging to real work.** It is treated as true in the
  design because the failure mode if it is true is severe and the cost of complying is nil.
- The `events_lost` error, its shape and its recovery procedure. Still not reproduced: forcing it
  means outrunning the server's retained history, which the 0.9.5 probes did not attempt. The
  0.9.5 schema dump does not mention it anywhere, so it is matched on the raw `error.code`.
- ~~The 0.9.0 change to live events only~~, ~~all-or-nothing subscribe~~ and ~~the two envelope
  conventions~~: confirmed on 0.9.5, see above.
- The 0.9.3 seen-state semantics, including that reads do not mark a pane seen while focus calls
  do. **The hard rule never to call focus rests on this.** The underlying risk is asymmetric: if
  the claim is wrong, not calling focus costs nothing.
- The absence of any concurrent-connection cap.

The schema has been re-dumped from a real 0.9.5 binary and committed verbatim as
`internal/herdr/gen/herdr-api-0.9.5.schema.json`; the Go wire types are generated from it.

## Socket discovery

```
~/.config/herdr/herdr.sock                       default session
~/.config/herdr/sessions/<name>/herdr.sock       named session
```

Mode is `0600`, user only. A sibling `herdr-client.sock` exists and is **not** the API socket.
Windows uses a named pipe.

Resolution order: an explicit `--session` name, then `HERDR_SOCKET_PATH`, then `HERDR_SESSION`,
then the default session socket. A process launched inside a herdr pane is given
`HERDR_SOCKET_PATH` and friends in its environment. A daemon started by launchd or systemd is not,
and must resolve the path itself.

## Framing, and the fact that shapes the whole client

Newline-delimited JSON, one request per line.

**A connection carries one request and one response, then closes.** Verified: a second request on
the same socket gets a broken pipe. The server reads a single initial request line with a 5 second
timeout and a 1 MiB cap. Connections are thread-per-connection with no observed cap, and multiple
concurrent clients are fully supported.

Only `events.subscribe` and the wait methods keep a connection open.

**Consequence for herddash:** a fresh dial per remote procedure call, plus one long-lived
connection per subscription. There is no connection pool to reuse, and no multiplexing.

## There is no version negotiation

The JSON API has no handshake and no protocol number of its own. The `protocol` integer that
appears in responses belongs to the separate length-prefixed binary protocol used for terminal
attach and live handoff, not to this API. The documented contract is that clients ignore unknown
fields and treat unsupported methods as ordinary errors.

`ping` is the de facto discovery call:

```json
{"id":"req_1","method":"ping","params":{}}
{"id":"req_1","result":{"type":"pong","version":"0.9.3","protocol":22,
 "capabilities":{"live_handoff":true,"detached_server_daemon":true}}}
```

herddash gates on `result.version` from `ping`, not on a protocol number.

## Request and response envelopes

A request requires a top-level string `id`, a `method`, and `params`. `params` is required even
when empty. There is no `type` field on a request.

Success is `{id, result}` with `result.type` as the discriminator. Failure is `{id, error}` with
`error.code` and `error.message`.

*Corrected 2026-10-16:* on 0.9.5 an unknown method is `invalid_request` (the message lists every
method), not `unknown_method`. An unknown pane is `pane_not_found`.

**`error.code` is an unconstrained string with no enumerated list.** Treat the set as open and
match on the codes you care about. Codes seen in the docs and source include `not_found`,
`pane_not_found`, `invalid_request`, `invalid_params`, `unknown_method`, `internal_error`,
`timeout`, `events_lost`, `agent_blocked`, `agent_not_ready`, `agent_not_idle`,
`agent_prompt_stalled`, `platform_unsupported`, `plugin_disabled`.

Since 0.9.2 an error response echoes the original request id whenever the line reached JSON
decoding, including for subscription setup errors. The id is empty only when there was no
unambiguous top-level string id.

## Subscriptions

27 subscribable kinds. **Only three require a `pane_id`; the other 24 are global.**

| Group | Kinds | `pane_id` |
|---|---|---|
| workspace | created, updated, metadata_updated, renamed, moved, reordered, closed, focused | no |
| worktree | created, opened, removed | no |
| tab | created, closed, focused, renamed, moved | no |
| pane | created, closed, updated, focused, moved, exited, agent_detected | no |
| layout | updated | no |
| pane, scoped | output_matched, also needs `source` and `match` | **yes** |
| pane, scoped | agent_status_changed, optional `agent_status` filter | **yes** |
| pane, scoped | scroll_changed | **yes** |

```json
{"id":"sub_1","method":"events.subscribe","params":{"subscriptions":[
  {"type":"pane.agent_status_changed","pane_id":"w1:p1","agent_status":"blocked"}]}}
```

The acknowledgement is `{"id":"sub_1","result":{"type":"subscription_started"}}`.

### There is no global agent-status subscription

This is the single most awkward fact in the API for a dashboard. Agent status, the thing herddash
exists to display, is the one event that must be subscribed per pane. So the client must track the
pane set from the global `pane.created`, `pane.closed` and `pane.agent_detected` streams and
maintain per-pane status subscriptions as panes come and go.

### Two naming conventions in the push payloads

The subscribe request uses dotted names. The push envelopes do not agree with each other.

```json
{"data":{"pane_id":"wB:p7","type":"pane_focused","workspace_id":"wB"},"event":"pane_focused"}
```

Lifecycle events push `{event, data}` with **underscored** event names, and `data` repeats the name
in a redundant `type`. The three pane-scoped streams use a different envelope whose `event` keeps
the **dots**. A decoder has to handle both.

### Subscriptions are all or nothing

One bad `pane_id` rejects the entire subscribe request and closes the connection. The documented
recovery is to refresh the pane list and retry, never to assume the other entries took effect.
This matters because a pane can close between the snapshot that named it and the subscribe that
references it.

### Subscribe before snapshotting

Since 0.9.0, a new lifecycle subscription starts with live events and no longer replays retained
history. Earlier versions flooded a new subscriber with stale events. Take the subscription first,
then the snapshot, or transitions in the gap are lost silently.

### `events_lost`

Added in 0.9.2. It arrives as an error response bearing the subscription's request id with
`error.code` of `events_lost`, after which the server closes that subscription connection. Other
clients are unaffected.

It fires when a subscriber falls behind the server's shared, non-durable retained history. The
history is shared across event types, so it can fire even when the evicted events would not have
matched your filters, and it can fire while subscriptions are still initialising.

Documented recovery: open a new subscription, wait for `subscription_started`, keep reading it, and
request a snapshot **on a separate connection**. Replace the cache with the snapshot. Treat
incoming events as invalidation signals that trigger another authoritative read rather than as
deltas to apply.

**Snapshots and events share no sequence boundary.** Do not replay buffered event payloads onto a
snapshot. Serialise refreshes, and refresh again if events arrive while a read is in flight.

## The snapshot

Method `session.snapshot`, empty params, result type `session_snapshot`. Required members are
`version`, `protocol`, `workspaces`, `tabs`, `panes`, `layouts` and `agents`, with optional
`focused_workspace_id`, `focused_tab_id` and `focused_pane_id`.

An agent record requires `terminal_id`, `agent_status`, `workspace_id`, `tab_id`, `pane_id`,
`focused` and `revision`. Optional and useful: `agent`, `display_agent`, `name`, `title`, `cwd`,
`foreground_cwd`, `terminal_title`, `terminal_title_stripped`, `agent_session`, `state_labels`,
`tokens`, `state_change_seq`, `interactive_ready`, `launch_pending`, `screen_detection_skipped`.

A pane record is the same minus the agent lifecycle fields, plus `label` and `scroll`. In `scroll`,
an `offset_from_bottom` of zero means the pane is at the bottom.

~~Use `revision` and `state_change_seq` for change detection.~~ *Corrected 2026-10-16:* on 0.9.5
`revision` did not move on any status change, and `state_change_seq` is a server-wide counter that
restarts with the server. See finding Q3. 0.9.5 also adds `completion_seq` to the agent record,
and `agent` and `restore_error` to the pane record.

## Reading pane output is not always passive

The most important correction to any naive design.

`agent.read` takes `target` and `source`, with optional `lines`, `format` and `strip_ansi`.
`pane.read` is the same with `pane_id`. The text comes back at `result.read.text`. On the wire the
sources are underscored: `visible`, `recent`, `recent_unwrapped`, `detection`. The CLI spells the
third with a hyphen.

| Source | Returns |
|---|---|
| `visible` | the currently rendered screen |
| `recent` | recent scrollback with terminal wrapping |
| `recent_unwrapped` | recent scrollback ignoring soft wrapping |
| `detection` | the bottom-buffer snapshot herdr's own agent detection reads, always plain text |

**Two traps.** For a recognised full-screen agent sitting at the bottom of its transcript, a
`recent` or `recent_unwrapped` read asking for more `lines` than the visible screen **drives the
agent's own mouse-scroll interface**, pages through its alternate screen and then returns the
viewport to the bottom. And if the agent is working, blocked or unknown, that same read fails with
`agent_not_idle`.

So a read loop built on `recent` would fail on exactly the panes a dashboard most wants to show.

Always passive: `visible`, `detection`, ANSI reads, output waits, subscriptions, a pane the user
has manually scrolled, and an attached pane. For recent sources, `lines` defaults to 80.

**herddash therefore reads `visible`, or `recent` bounded by the pane's `viewport_rows`.**

## Seen state, and the one thing herddash must never call

The CLI and API share the server's seen state. `done` means idle and not yet marked seen.
**Explicit `pane.focus` and `agent.focus` calls mark the target seen. Reads do not.**

A read-only dashboard is therefore safe, provided it never calls focus. That is a hard constraint,
not a style preference: calling focus would silently clear a completion the operator has not
actually looked at.

Each terminal client tracks viewed completions independently, so herddash's notion of outstanding
work can legitimately differ from a TUI badge. `pane.focused` events fire on any client's manual
selection and the payload does not say which client.

## `agent.explain`

Has a socket equivalent: method `agent.explain`, params `{"target": "..."}`, result type
`agent_explain`. Returns the final state, the detection manifest source and version, the matched
rule, the evidence for evaluated rules, the idle fallback reason and `screen_detection_skip_reason`.
The server must be restarted or handed off after a herdr upgrade before this is reliable.

## Generating the Go types

There is no official Go client. `herdr api schema --json` dumps the bundled schema from the
installed binary, and generating types from it is the approach the most complete third-party Go
client takes.

*Done 2026-10-16.* The 0.9.5 dump (`schema_version` 1, `protocol` 22) is committed verbatim as
`internal/herdr/gen/herdr-api-0.9.5.schema.json`. `go generate ./internal/herdr/gen` runs genwire,
an in-repository generator that imports only the standard library, and writes `wire_gen.go`. It
generates only what herddash uses and never a focus or pane-output request. A test regenerates and
fails on drift. No third-party generator was adopted: any would be a module dependency, which
needs an ADR, and the subset herddash needs is small. The usual Go choice, `go-jsonschema`
(`github.com/atombender/go-jsonschema`, latest on 2026-10-16), also fails on this dump: it cannot
resolve the schema's cross-section references (`#/schemas/<section>/$defs/<Name>`) and stops on
an `anyOf` ("could not merge anyOf types: types list is empty").

## Findings against herdr 0.9.5

**Recorded 2026-10-16** against `/opt/homebrew/Cellar/herdr/0.9.5/bin/herdr`, a headless
`herdr server` run under `env -i` with `HOME=/tmp/hd13/home`, so neither the operator's live server
nor the inherited `HERDR_SOCKET_PATH` was involved. Agent status was driven by `pane.report_agent`
and by a `sleep` started with argv0 `claude`, never by a real agent. Reproduce with
[herdr-0.9.5-probe.sh](herdr-0.9.5-probe.sh); request and response lines below are abridged from
its transcript.

### Q4. One request per connection — still true on 0.9.5

```text
>> (conn A) {"id":"p1","method":"ping","params":{}}
<< (conn A) {"id":"p1","result":{"type":"pong","version":"0.9.5","protocol":22,...}}
!! (conn A) second write failed: BrokenPipeError [Errno 32] Broken pipe
>> (conn B) two ping lines in a single write, ids a and b
<< (conn B) 1 response line(s), ids ['a'], then EOF
```

The server answers the first line and closes. A second line, written after the response or
pipelined in the same write, is never answered. herddash keeps a fresh dial per call (ADR-0006).

`ping` on 0.9.5 advertises six capabilities, answering the earlier open item:

```json
{"live_handoff":true,"detached_server_daemon":false,"endpoint_protocol_generation":1,
 "surface_interest":true,"health_check":true,"ssh_agent_registration":true}
```

`detached_server_daemon` is `false` for a server started as `herdr server`.

### Q1. Pane ids survive a restart that restores the session; nothing names the instance

```text
   before: panes [w1:p1 term_65df2e06a79ca1, w1:p3 term_65df2e161c7cd4, w2:p1 term_65df2e161b2853]
           agents [w1:p1 working state_change_seq 9]          (w1:p2 had been closed)
$ herdr server stop  -> exit 0
<< [stream] {"id":"s1","result":{"type":"subscription_started"}}
<< [stream] <EOF: server closed the connection>
$ herdr server
<< ping {"type":"pong","version":"0.9.5","protocol":22,"capabilities":{...identical...}}
   after:  panes [w1:p1 term_65df2e18d44a41, w1:p3 term_65df2e18d4d972, w2:p1 term_65df2e18d550d3]
           agents []
   pane ids kept: True    terminal ids changed: True
   next new pane in w1: w1:p4
   w1:p1 after one report on the new instance: state_change_seq 1
-- restart with session.json moved aside:
   fresh session: panes [] workspaces 0
```

- **Pane ids survive** when herdr restores its saved session (`session.json` in the herdr config
  directory). The restore keeps the gaps and the per-workspace next-number counter, so a closed
  pane's id is not handed out again: after `w1:p2` closed, the next pane was `w1:p4`.
- **Without a saved session the server starts empty**, and ids restart from `w1:p1`. That is the
  reassignment the epoch exists for.
- **No field names a server instance.** `ping` is byte-identical across the restart and the
  snapshot has no instance id or start time. A new instance shows itself by: every open connection
  reaching EOF; **every pane's `terminal_id` changing** while its `pane_id` stays; and
  `state_change_seq` restarting from 1.
- Agents do not survive: the restored panes run fresh shells, so the snapshot's `agents` is empty
  until herdr detects or is told about an agent again.
- Not tested: `server.live_handoff`. Whether a handoff keeps `terminal_id`s is unknown. Story 1.6,
  which owns epoch detection, must account for it.

### Q2. Foreground process: `pane.process_info`; only agent detection produced an event

Connection 1, subscribed to all 24 global kinds plus the pane's status and scroll streams, while
the pane ran `cd` and `sleep`:

```text
>> {"id":"r1","method":"pane.process_info","params":{"pane_id":"w1:p1"}}
<< {"id":"r1","result":{"type":"pane_process_info","process_info":{"pane_id":"w1:p1",
    "shell_pid":42985,"foreground_process_group_id":42985,"foreground_processes":[{"pid":42985,
    "name":"bash","argv0":"sh","argv":["-sh"],"cmdline":"-sh","cwd":"/private/tmp"}]}}}
>> {"id":"r1","method":"pane.send_text","params":{"pane_id":"w1:p1","text":"cd /tmp/hd13 && sleep 300\n"}}
<< ... "foreground_process_group_id":43008,"foreground_processes":[{"pid":43008,"name":"sleep",
    "argv0":"sleep","argv":["sleep","300"],"cmdline":"sleep 300","cwd":"/private/tmp/hd13"}] ...
-- every event on the all-24-global-kinds connection while the foreground and cwd changed:
<< [stream] {"id":"s1","result":{"type":"subscription_started"}}
```

Connection 2, a later subscription to the lifecycle kinds plus the pane's status stream, while the
foreground was replaced by a `sleep` with argv0 `claude`:

```text
>> [subscribe] {"id":"s1","method":"events.subscribe","params":{"subscriptions":[{"type":"pane.created"},
    {"type":"pane.closed"},{"type":"pane.updated"},{"type":"pane.exited"},{"type":"pane.agent_detected"},
    {"type":"pane.moved"},{"type":"pane.agent_status_changed","pane_id":"w1:p1"}]}}
>> {"id":"r1","method":"pane.send_keys","params":{"pane_id":"w1:p1","keys":["ctrl+c"]}}
>> {"id":"r1","method":"pane.send_text","params":{"pane_id":"w1:p1","text":"(exec -a claude sleep 600)\n"}}
<< ... "foreground_processes":[{"pid":43034,"name":"sleep","argv0":"claude","argv":["claude","600"],...}] ...
<< [stream] {"id":"s1","result":{"type":"subscription_started"}}
<< [stream] {"data":{"agent":"claude","pane_id":"w1:p1","type":"pane_agent_detected","workspace_id":"w1"},"event":"pane_agent_detected"}
<< [stream] {"data":{"agent":"claude","agent_status":"idle","pane_id":"w1:p1","workspace_id":"w1"},"event":"pane.agent_status_changed"}
```

- **The call is `pane.process_info`** with `pane_id`. It returns the shell pid, the foreground
  process group id, and each foreground process's `pid`, `name`, `argv0`, `argv`, `cmdline` and
  `cwd`. The process whose environment carries the Profile is the foreground process.
- **Starting `sleep` and changing directory produced no event** on connection 1, not even
  `pane_updated`.
- **Replacing the foreground with argv0 `claude` produced `pane.agent_detected`** on connection 2,
  then a status event (`name` stayed `sleep`). These are the only foreground changes tried. Others
  may or may not emit something; a change that emits nothing is seen only by calling
  `pane.process_info` again.

### Q3. Ordering: `state_change_seq`, snapshot only, per server instance

```text
   start                       agent_status idle    state_change_seq 1 completion_seq None revision 0
   report_agent working     -> working  2  None  0
   report_agent idle        -> idle     3  3     0
   report_agent blocked     -> blocked  4  None  0
   report_agent working     -> working  5  None  0
   report_agent working     -> working  5  None  0     (no change, no event)
   report_agent idle        -> idle     6  6     0
   on w1:p2 working         -> working  7  None  0
   on w1:p2 idle            -> idle     8  8     0
   on w1:p1 working         -> working  9  None  0
<< [stream] {"data":{"agent":"claude","agent_status":"working","pane_id":"w1:p1","workspace_id":"w1"},"event":"pane.agent_status_changed"}
   ... one event per change, in order, none for the repeated report ...
```

- **`state_change_seq` rises by one on every Status change and on nothing else.** A repeated
  report of the same state moves nothing and emits no event.
- **It is one counter for the whole server**, not per pane: `w1:p2`'s changes took 7 and 8, so
  `w1:p1` jumped from 6 to 9. Within one Agent it is strictly increasing, with gaps.
- **It restarts at 1 with a new server instance** (Q1), so it is comparable only within one.
- **`completion_seq`** is set to the `state_change_seq` of a change into `idle` that completed
  work, and is absent otherwise. **`revision`** never moved on a Status change.
- **A live `pane.agent_status_changed` event carries no sequence field** (none in the schema, none
  on the wire). Its order is its position on the pane's own subscription connection, which herdr
  delivered in change order.
- So herddash's `HerdrOrder` is `state_change_seq`, known only for snapshot-derived Transitions and
  meaningful only within one server instance; a live Transition has none.

### Errors and subscriptions

```text
>> {"id": "e1", "method": "no.such_method", "params": {}}
<< {"id":"e1","error":{"code":"invalid_request","message":"invalid request: unknown variant
    `no.such_method`, expected one of `ping`, `server.stop`, ...(abridged)"}}
>> {"id": "e2", "method": "pane.process_info", "params": {"pane_id": "w9:p9"}}
<< {"id":"e2","error":{"code":"pane_not_found","message":"pane not found"}}
>> [subscribe] {"id": "e3", "method": "events.subscribe", "params": {"subscriptions": [{"type": "pane.created"},
    {"type": "pane.agent_status_changed", "pane_id": "w1:p1"}, {"type": "pane.agent_status_changed", "pane_id": "w9:p9"}]}}
<< [stream] {"id":"e3","error":{"code":"pane_not_found","message":"pane w9:p9 not found"}}
<< [stream] <EOF: server closed the connection>
```

- **Every error echoes the request id** (`e1`, `e2`, `e3`), including the subscription setup error.
- **An unknown method is `invalid_request`**, not `unknown_method`.
- **Subscribe is all or nothing.** One unknown pane failed the request although `pane.created` and
  `w1:p1` were valid. The server then closed the connection, and nothing was delivered for the
  valid entries.

Live-only delivery. The Q2 connection 1 subscription opened after `workspace.create` had created
`w1` and `w1:p1`. The Q3 subscription opened after `w1:p1`'s earlier detection and `idle` status.
Each received `subscription_started` and then only later changes, with no replay:

```text
-- every event on the all-24-global-kinds connection while the foreground and cwd changed:
<< [stream] {"id":"s1","result":{"type":"subscription_started"}}
...
>> [subscribe] {"id": "s1", "method": "events.subscribe", "params": {"subscriptions": [{"type": "pane.agent_status_changed", "pane_id": "w1:p1"}]}}
   start {'agent_status': 'idle', 'state_change_seq': 1, ...}
<< [stream] {"id":"s1","result":{"type":"subscription_started"}}
<< [stream] {"data":{"agent":"claude","agent_status":"working","pane_id":"w1:p1","workspace_id":"w1"},"event":"pane.agent_status_changed"}
```

The first status event is the `working` reported after subscribing, not the earlier `idle`.

**`events_lost` does not appear anywhere in the 0.9.5 schema dump.** `error.code` is a free
string, so `events_lost` has no generated type. A client recognises it by matching the raw
`error.code`. Its behaviour stays UNVERIFIED.

### Other observations on 0.9.5

- **`done` was never reported.** With no terminal client attached, an agent going `working` then
  `idle` stayed `idle` in events and snapshots, focused pane or not, with `completion_seq` set.
  `done` is the unseen form of `idle`, and seen state is tracked per terminal client, so with no
  client attached there may be nothing to report `done` against. This bears on the Finished class
  and is untested with a client attached; Story 1.5 should observe it on a real setup.
- `pane.report_agent` accepts `idle`, `working`, `blocked` and `unknown`; `done` is not reportable.
- **A pane can change id.** `pane_moved` carries `previous_pane_id`, and pane ids are scoped to their
  workspace (`w1:p1`). Untested here; a move across workspaces would look like one pane closing and
  another appearing unless the engine follows `pane.moved`.

## Open items

- ~~Whether one-request-per-connection still holds in 0.9.3.~~ Holds on 0.9.5 (Q4, 2026-10-16).
- ~~Whether `ping` advertises capabilities beyond `live_handoff` and `detached_server_daemon`.~~ It
  advertises six on 0.9.5 (Q4).
- Whether any hard concurrent-connection cap exists. None found in docs or source; not probed.
- Whether `done` appears with a terminal client attached, and whether `events_lost` matches its
  documented shape. Neither was reproduced in isolation.
