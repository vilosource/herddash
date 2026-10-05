# herdr socket API — implementation notes

**Recorded 2026-10-05 against herdr 0.9.3 documentation, the v0.9.3 source, and live probing of a
0.9.2-era server.** This is a frozen research record. It is the reference herddash's client is
built from. Where it is wrong, fix the code and amend this file in the same change.

0.9.3 is a two-line hotfix over 0.9.2 affecting terminal escape-sequence key handling, with no API
changes. Everything here applies equally to 0.9.2.

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

Use `revision` and `state_change_seq` for change detection.

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
client takes. **Dump the schema from a real 0.9.3 binary before freezing herddash's types.** The
findings above lean on 0.9.3 prose plus an older bundled schema, and while the drift looks small it
has not been verified against 0.9.3 itself.

## Open items

- Whether one-request-per-connection still holds in 0.9.3. Empirically true earlier and consistent
  with unchanged 0.9.3 source, but not executed against 0.9.3. **Verify this first; it determines
  the whole connection design.**
- Whether 0.9.3's `ping` advertises capabilities beyond `live_handoff` and
  `detached_server_daemon`.
- Whether any hard concurrent-connection cap exists. None found in docs or source.
