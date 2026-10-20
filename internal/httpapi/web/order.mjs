// Board ordering, formatting (Story 3.7), event handling (Story 3.8) and the
// connection and version strips (Story 3.9). A
// plain ES module: no DOM, no browser globals, so `node --test` checks it
// against core's golden fixture (internal/core/testdata/board-order.json).
//
// The Daemon computes class, zone, group, heading and the sort key (AD-1).
// This module only applies stream events to the Board's model, orders
// AgentViews by that key, finds where workspace headers go and formats what a
// Box shows. It re-derives nothing.

// cmpNum compares two numbers.
function cmpNum(a, b) {
  return a < b ? -1 : a > b ? 1 : 0;
}

// cmpStr compares two strings by Unicode code point, which is the byte order
// of their UTF-8 encodings, so it matches Go's strings.Compare. JavaScript's
// own < compares UTF-16 code units, which differs above U+FFFF.
function cmpStr(a, b) {
  if (a === b) return 0;
  const x = Array.from(a);
  const y = Array.from(b);
  const n = Math.min(x.length, y.length);
  for (let i = 0; i < n; i++) {
    const c = cmpNum(x[i].codePointAt(0), y[i].codePointAt(0));
    if (c) return c;
  }
  return cmpNum(x.length, y.length);
}

// compareViews orders two AgentViews exactly as internal/core/view.go
// SortKey.compare does: sort_key elements 0-3 (zone, group, class rank,
// start) numerically, then the AgentKey as host, server, epoch (as a number)
// and pane id. sort_key[4] is never compared as one string, because epoch
// "10" sorts before "9" as text.
export function compareViews(a, b) {
  for (let i = 0; i < 4; i++) {
    const c = cmpNum(a.sort_key[i], b.sort_key[i]);
    if (c) return c;
  }
  return (
    cmpStr(a.key.host, b.key.host) ||
    cmpStr(a.key.server, b.key.server) ||
    cmpNum(a.key.epoch, b.key.epoch) ||
    cmpStr(a.key.pane_id, b.key.pane_id)
  );
}

// sortViews returns a new array of the views in Board order.
export function sortViews(views) {
  return views.slice().sort(compareViews);
}

// workspaceHeaders returns the indexes of sorted views at which a zone-1
// workspace group starts, the first zone-1 view included, as
// core.WorkspaceHeaders does. The Board draws a workspace header before each.
export function workspaceHeaders(sorted) {
  const headers = [];
  for (let i = 0; i < sorted.length; i++) {
    const k = sorted[i].sort_key;
    if (k[0] !== 1) continue;
    const p = i > 0 ? sorted[i - 1].sort_key : null;
    if (p === null || p[0] !== 1 || p[1] !== k[1]) headers.push(i);
  }
  return headers;
}

function pad2(n) {
  return String(n).padStart(2, "0");
}

// formatDuration formats a span in ms: 0s to 59s, 1m to 59m, 1h 00m to
// 23h 59m, then 1d 00h onwards. The second unit is always two digits, so the
// width steps only at a unit boundary. A negative span (clock skew) is 0s.
export function formatDuration(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return s + "s";
  const m = Math.floor(s / 60);
  if (m < 60) return m + "m";
  const h = Math.floor(m / 60);
  if (h < 24) return h + "h " + pad2(m % 60) + "m";
  return Math.floor(h / 24) + "d " + pad2(h % 24) + "h";
}

// UNKNOWN_WAIT is what a Box shows when waiting_since is unknown.
export const UNKNOWN_WAIT = "—";

// hasWaitingSince reports whether the view carries a known waiting_since
// (the API omits it when unknown).
export function hasWaitingSince(view) {
  return typeof view.waiting_since === "number" && view.waiting_since > 0;
}

// waitingLabel is the Waiting Time a Box shows at nowMs: the duration since
// waiting_since, prefixed "≥ " when it is a lower bound, or a dash when
// waiting_since is unknown.
export function waitingLabel(view, nowMs) {
  if (!hasWaitingSince(view)) return UNKNOWN_WAIT;
  const d = formatDuration(nowMs - view.waiting_since);
  return view.waiting_exact ? d : "≥ " + d;
}

// CLASS_LABEL is the text of each Attention Class.
export const CLASS_LABEL = Object.freeze({
  needs_you: "Needs you",
  finished: "Finished",
  working: "Working",
  unknown: "Unknown",
});

// classLabel is the Attention Class as text.
export function classLabel(view) {
  return CLASS_LABEL[view.attention_class] || view.attention_class;
}

// whereLabel is "workspace › tab", leaving out an empty one and its separator.
export function whereLabel(view) {
  return [view.workspace, view.tab].filter(Boolean).join(" › ");
}

// headerLabel is the workspace header's text for the group starting at view.
export function headerLabel(view) {
  return view.workspace || "(no workspace)";
}

// neverWorking reports whether the view is an idle Agent never seen working:
// it sits with Working (the Daemon's class), and its Box gets no Waiting Time
// emphasis.
export function neverWorking(view) {
  return view.status === "idle" && view.attention_class === "working";
}

// layout splits views into what the Board draws: the top zone (Needs you,
// then Finished) and the lower zone's workspace groups, each in Board order
// with its header label.
export function layout(views) {
  const sorted = sortViews(views);
  const headers = workspaceHeaders(sorted);
  const top = sorted.slice(0, headers.length ? headers[0] : sorted.length);
  const groups = headers.map((start, h) => {
    const end = h + 1 < headers.length ? headers[h + 1] : sorted.length;
    return { label: headerLabel(sorted[start]), views: sorted.slice(start, end) };
  });
  return { top, groups };
}

// boxLabel is a Box's accessible name at nowMs: everything it shows,
// untruncated, so keyboard and screen-reader users can read it in full.
export function boxLabel(view, nowMs) {
  const w = hasWaitingSince(view)
    ? "waiting " + waitingLabel(view, nowMs).replace("≥ ", "at least ")
    : "Waiting Time unknown";
  return [classLabel(view), w, view.heading, view.context_line, view.directory, whereLabel(view)]
    .filter(Boolean)
    .join(", ");
}

// keyId is an AgentKey as one string, unique per key: the Board's model and
// each Box's data-key use it. It is not the sort_key's key string, which is
// ambiguous when a host or server contains "/".
export function keyId(key) {
  return JSON.stringify([key.host, key.server, key.epoch, key.pane_id]);
}

// The Board's model: the latest AgentViews by keyId, and the latest
// ConnectionTruth (null until the first state). A model is never modified;
// applyEvent returns a new one.
export function emptyModel() {
  return { agents: new Map(), connection: null };
}

// modelViews returns the model's AgentViews, in no particular order.
export function modelViews(model) {
  return Array.from(model.agents.values());
}

function isObject(x) {
  return x !== null && typeof x === "object" && !Array.isArray(x);
}

// validKey reports whether k is an AgentKey: host, server and pane id
// strings, and an integer epoch.
function validKey(k) {
  return (
    isObject(k) &&
    typeof k.host === "string" &&
    typeof k.server === "string" &&
    Number.isInteger(k.epoch) &&
    typeof k.pane_id === "string"
  );
}

// validView reports whether v is an AgentView the Board can place: a valid
// key, a known Attention Class and a sort key of four numbers and the key
// string.
function validView(v) {
  return (
    isObject(v) &&
    validKey(v.key) &&
    Object.hasOwn(CLASS_LABEL, v.attention_class) &&
    Array.isArray(v.sort_key) &&
    v.sort_key.length === 5 &&
    v.sort_key.slice(0, 4).every((n) => typeof n === "number" && Number.isFinite(n))
  );
}

function sameKey(a, b) {
  return a.host === b.host && a.server === b.server && a.epoch === b.epoch && a.pane_id === b.pane_id;
}

// EVENT_NAMES are exactly the event names the Daemon sends on /events. The
// Board listens for these only; an EventSource never dispatches a name
// nobody listens for.
export const EVENT_NAMES = Object.freeze(["state", "agent", "removed", "transition", "resync", "connection"]);

// HANDLED_EVENTS are the names applyEvent changes the model for; resync is
// not one.
export const HANDLED_EVENTS = Object.freeze(["state", "agent", "transition", "removed", "connection"]);

// applyEvent applies one /events event, its name and raw data, to model. It
// returns {model, warning}: model is the new model, or the same object when
// nothing changed; warning is set, and model unchanged, when the payload is
// malformed. state replaces everything; agent adds or replaces that
// AgentView; transition replaces it with the event's agent; removed drops the
// key; connection replaces the ConnectionTruth. resync, and any name outside
// HANDLED_EVENTS, changes nothing.
export function applyEvent(model, name, data) {
  if (!HANDLED_EVENTS.includes(name)) return { model };
  let d;
  try {
    d = JSON.parse(data);
  } catch (err) {
    return { model, warning: name + " event does not parse: " + err.message };
  }
  const bad = (why) => ({ model, warning: name + " event " + why + "; ignored" });
  switch (name) {
    case "state": {
      if (!isObject(d) || !Array.isArray(d.agents)) return bad("has no agents array");
      if (!d.agents.every(validView)) return bad("has a malformed AgentView");
      if (!isObject(d.connection)) return bad("has no connection");
      const agents = new Map();
      for (const v of d.agents) {
        const id = keyId(v.key);
        if (agents.has(id)) return bad("has two AgentViews for one AgentKey");
        agents.set(id, v);
      }
      return { model: { agents, connection: d.connection } };
    }
    case "agent": {
      if (!validView(d)) return bad("is not an AgentView");
      return { model: withAgent(model, d) };
    }
    case "transition": {
      if (!isObject(d) || !validView(d.agent)) return bad("has no AgentView");
      if (!validKey(d.key) || !sameKey(d.key, d.agent.key)) return bad("key does not match its AgentView");
      return { model: withAgent(model, d.agent) };
    }
    case "removed": {
      if (!isObject(d) || !validKey(d.key)) return bad("has no AgentKey");
      const id = keyId(d.key);
      if (!model.agents.has(id)) return { model };
      const agents = new Map(model.agents);
      agents.delete(id);
      return { model: { ...model, agents } };
    }
    case "connection": {
      if (!isObject(d)) return bad("is not a ConnectionTruth");
      return { model: { ...model, connection: d } };
    }
  }
  return { model };
}

function withAgent(model, view) {
  const agents = new Map(model.agents);
  agents.set(keyId(view.key), view);
  return { ...model, agents };
}

// Reconnect backoff after the stream closes for good (a non-200 reply, such
// as the 503 while the Daemon restarts): the first reopen waits
// RECONNECT_START_MS, each later one twice as long up to RECONNECT_CAP_MS,
// and a state event resets it.
export const RECONNECT_START_MS = 1000;
export const RECONNECT_CAP_MS = 30000;

// nextReconnectDelay is the wait after one of delay ms.
export function nextReconnectDelay(delay) {
  return Math.min(delay * 2, RECONNECT_CAP_MS);
}

// streamState is the Board's stream state before the first source opens:
// the model, the current source (any token board.mjs compares with ===),
// whether a reopen is pending, the next reopen delay, whether the current
// source has yet to deliver a valid state, and lostSince: when (ms) the
// first error on the current source came since the last valid state, or null
// (see streamLost).
export function streamState() {
  return {
    model: emptyModel(),
    source: null,
    pending: false,
    delay: RECONNECT_START_MS,
    awaitingState: true,
    lostSince: null,
  };
}

// streamDecision is the Board's stream logic, one input at a time. It returns
// {st, reopenAfter, warning}: the new state, a delay in ms when board.mjs
// must open a new source after it, and a warning to log.
//
//   - {kind: "open", source}: source is now current; no reopen is pending,
//     and its events are ignored until it delivers a valid state.
//   - {kind: "error", source, closed, now}: when source is current, the
//     stream is in trouble from now (lostSince, kept from an earlier error)
//     until the next valid state. When it is also
//     closed (the EventSource gave up, as on a non-200 reply) and no reopen
//     is pending, reopen after the current delay, and the delay after that
//     doubles up to the cap. Otherwise no reopen: EventSource retries a
//     dropped stream itself.
//   - {kind: "event", source, name, data}: an event from a source that is not
//     current is ignored, and so is every event before a valid state on the
//     current one, so a malformed state on a new stream cannot leave the old
//     model to be merged into. A valid state replaces the model, resets the
//     delay and ends a loss of the stream; other events go through
//     applyEvent.
export function streamDecision(st, input) {
  switch (input.kind) {
    case "open":
      return { st: { ...st, source: input.source, pending: false, awaitingState: true } };
    case "error": {
      if (input.source !== st.source) return { st };
      const lost = st.lostSince != null ? st : { ...st, lostSince: input.now };
      if (!input.closed || st.pending) return { st: lost };
      return { st: { ...lost, pending: true, delay: nextReconnectDelay(st.delay) }, reopenAfter: st.delay };
    }
    case "event": {
      if (input.source !== st.source) return { st };
      if (st.awaitingState && input.name !== "state") return { st };
      const r = applyEvent(st.model, input.name, input.data);
      if (r.warning) return { st, warning: r.warning };
      if (input.name === "state") {
        return { st: { ...st, model: r.model, awaitingState: false, delay: RECONNECT_START_MS, lostSince: null } };
      }
      return r.model === st.model ? { st } : { st: { ...st, model: r.model } };
    }
  }
  return { st };
}

// LOST_GRACE_MS is how long the stream may be in trouble, with no valid state
// since its first error, before the Board shows it has lost the Daemon: a
// transient drop that EventSource's own retry heals does not flash the strip.
export const LOST_GRACE_MS = 3000;

// streamLost reports whether, at nowMs, the Board has lost the Daemon's
// stream: an error on the current source at least LOST_GRACE_MS ago and no
// valid state since.
export function streamLost(st, nowMs) {
  return typeof st.lostSince === "number" && nowMs - st.lostSince >= LOST_GRACE_MS;
}

// formatAge formats how old the last snapshot is, in ms: it keeps seconds
// below an hour (0s to 59s, then 1m 00s to 59m 59s), because it is read when
// something is wrong, and then follows formatDuration.
export function formatAge(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return s + "s";
  const m = Math.floor(s / 60);
  if (m < 60) return m + "m " + pad2(s % 60) + "s";
  return formatDuration(ms);
}

// clockUTC is a UTC Unix ms time as HH:MM:SS UTC.
export function clockUTC(ms) {
  const d = new Date(ms);
  return pad2(d.getUTCHours()) + ":" + pad2(d.getUTCMinutes()) + ":" + pad2(d.getUTCSeconds()) + " UTC";
}

// LOST_TEXT is the connection strip while the Board has lost the Daemon's
// stream.
export const LOST_TEXT = "Lost contact with herddash. Reconnecting…";

function versionLabel(v) {
  return typeof v === "string" && v ? v : "(version unknown)";
}

// boardStatus decides what the Board shows around its Boxes, from the latest
// ConnectionTruth (null before the first state), whether the Board has lost
// the Daemon's stream, and the clock. It returns {strips, dimmed, hideBoxes}:
//
//   - strips, in order (connection, then version), each {kind, parts}: kind
//     is "disconnected", "advisory" or "refused"; parts are the strip's text
//     runs, each {text}, plus tick: true on the age that ticks (hidden from
//     screen readers) or sr: true on the fixed time read in its place.
//   - dimmed: every Box is dimmed and desaturated, never hidden, because the
//     Board's data is stale (herdr disconnected, or the Daemon's stream lost).
//   - hideBoxes: the gate is not ok or advisory (refused, unknown, or
//     missing while connected), so no Box is shown at all. A missing gate
//     while not connected is the Daemon before its first gate decision: no
//     version strip, and Boxes are not hidden for it.
//
// Anything but connected: true is disconnected, and anything but
// resync_guaranteed: true is not guaranteed.
//
// A lost stream replaces the herdr-disconnected text, because the
// ConnectionTruth is then itself stale; the version strip still follows the
// latest ConnectionTruth.
export function boardStatus(connection, lost, nowMs) {
  const c = isObject(connection) ? connection : null;
  const gate = c ? c.gate : "ok";
  // The Daemon omits gate before its first decision, while herdr is not yet
  // connected: the disconnected strip says enough, so no version strip then.
  const undecided = c !== null && (gate === undefined || gate === null || gate === "") && c.connected !== true;
  const hideBoxes = !undecided && gate !== "ok" && gate !== "advisory";
  const strips = [];
  if (lost) {
    strips.push({ kind: "disconnected", parts: [{ text: LOST_TEXT }] });
  } else if (c && c.connected !== true) {
    const at = c.last_snapshot_at;
    if (Number.isFinite(at) && at > 0) {
      strips.push({
        kind: "disconnected",
        parts: [
          { text: "Disconnected from herdr. Last snapshot " },
          { text: formatAge(nowMs - at) + " ago", tick: true },
          { text: "at " + clockUTC(at), sr: true },
          { text: hideBoxes ? "." : "; Boxes show that snapshot." },
        ],
      });
    } else {
      strips.push({ kind: "disconnected", parts: [{ text: "Disconnected from herdr. No snapshot received yet." }] });
    }
  }
  if (gate === "advisory") {
    let t =
      "herdr " + versionLabel(c.herdr_version) + " is below the supported minimum " +
      versionLabel(c.herdr_minimum) + "; recording under the dogfooding flag.";
    if (c.resync_guaranteed !== true) t += " Resync is not guaranteed with this herdr version.";
    strips.push({ kind: "advisory", parts: [{ text: t }] });
  } else if (gate === "refused") {
    strips.push({
      kind: "refused",
      parts: [
        {
          text:
            "herdr " + versionLabel(c.herdr_version) + " is not supported. herddash needs " +
            versionLabel(c.herdr_minimum) + " or newer and is recording nothing.",
        },
      ],
    });
  } else if (hideBoxes) {
    const g = typeof gate === "string" && gate ? gate : "none";
    strips.push({ kind: "refused", parts: [{ text: "herdr version gate unknown (" + g + "); nothing is being recorded." }] });
  }
  return { strips, dimmed: lost || (c !== null && c.connected !== true), hideBoxes };
}

// stripsKey identifies strips by everything but the ticking age, so the Board
// redraws its live region only when a strip appears, goes or changes, never as
// the age ticks.
export function stripsKey(strips) {
  return JSON.stringify(strips.map((s) => [s.kind, s.parts.map((p) => (p.tick ? "" : p.text))]));
}
