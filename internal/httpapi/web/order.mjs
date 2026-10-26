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

// IDLE_LABEL is what a Box says for an idle Agent (see isIdle).
export const IDLE_LABEL = "Idle";

// isIdle reports whether the view is an idle Agent: class working (the
// Daemon's) while its Status is anything but working, such as an Agent idle
// since before it was ever seen working. Its Box reads Idle, in amber, and
// gets no Waiting Time emphasis. It is the one thing the Board derives from
// the Status; the Daemon ranks the same Agents Idle in the sort key.
export function isIdle(view) {
  return view.attention_class === "working" && view.status !== "working";
}

// BACKGROUND_LABEL is what a Box says for an Agent waiting on background work.
export const BACKGROUND_LABEL = "Background";

// isBackground reports whether the view is an Agent waiting on background
// work: the Daemon's background field, read as sent and never derived here
// (operator request 2026-10-21). The Daemon sets it for a done or idle Agent,
// or a working one whose main turn is over (ADR-0009), so it takes precedence
// over Finished, Idle and Working.
export function isBackground(view) {
  return view.background === true;
}

// boxKind is the kind a Box is drawn as: "background", "idle", or its
// Attention Class.
export function boxKind(view) {
  if (isBackground(view)) return "background";
  return isIdle(view) ? "idle" : view.attention_class;
}

// classLabel is the Attention Class as text, Background or Idle for those.
export function classLabel(view) {
  if (isBackground(view)) return BACKGROUND_LABEL;
  if (isIdle(view)) return IDLE_LABEL;
  return CLASS_LABEL[view.attention_class] || view.attention_class;
}

// backgroundTasks is the Daemon's background_tasks count, read as sent: 0
// when it is absent or not a positive whole number.
export function backgroundTasks(view) {
  const n = view.background_tasks;
  return Number.isInteger(n) && n > 0 ? n : 0;
}

// stateCount is what follows the class in a Box's State label (ADR-0009):
// " · N bg" on a Working Box with background tasks open, " · N" on a
// Background Box with more than one, "" otherwise.
export function stateCount(view) {
  const n = backgroundTasks(view);
  const kind = boxKind(view);
  if (kind === "working" && n > 0) return " · " + n + " bg";
  if (kind === "background" && n > 1) return " · " + n;
  return "";
}

// stateLabel is a Box's State label as text: the class label and its count.
export function stateLabel(view) {
  return classLabel(view) + stateCount(view);
}

// tasksLabel is the background task count in words for the accessible name,
// "" when there are none.
export function tasksLabel(view) {
  const n = backgroundTasks(view);
  if (n === 0) return "";
  return n === 1 ? "1 background task" : n + " background tasks";
}

// whereLabel is "workspace › tab", leaving out an empty one and its separator.
export function whereLabel(view) {
  return [view.workspace, view.tab].filter(Boolean).join(" › ");
}

// tabLabel is a Box's first line: the tab, or the workspace when the tab is
// empty.
export function tabLabel(view) {
  return view.tab || view.workspace || "";
}

// paneLabel is the pane position in words for a tooltip or accessible name,
// "" when the tab has one pane.
export function paneLabel(view) {
  return view.pane_position ? view.pane_position + " pane" : "";
}

// inNeedsYou reports whether the view sits in the Needs you zone, by the
// Daemon's sort key (zone 0).
export function inNeedsYou(view) {
  return view.sort_key[0] === 0;
}

// UNPOSITIONED_GROUP is the first group core gives a workspace herdr did not
// list (2^52, core.UnpositionedGroup); such groups run up to, not including,
// the unknown sentinel Number.MAX_SAFE_INTEGER.
const UNPOSITIONED_GROUP = 2 ** 52;

// headerLabel is the workspace header's text for the group starting at view:
// the workspace's label; else "(unlisted workspace)" for a workspace herdr did
// not list, which has its own group below the sentinel and never a label;
// else "(no workspace)".
export function headerLabel(view) {
  if (view.workspace) return view.workspace;
  const g = view.sort_key ? view.sort_key[1] : undefined;
  if (g >= UNPOSITIONED_GROUP && g < Number.MAX_SAFE_INTEGER) return "(unlisted workspace)";
  return "(no workspace)";
}

// groupCounts is a workspace group's counts in the group's reading order,
// "1 finished · 3 idle · 1 background · 2 working", leaving out zeros and
// Unknown.
export function groupCounts(views) {
  const n = { finished: 0, idle: 0, background: 0, working: 0 };
  for (const v of views) {
    const k = boxKind(v);
    if (k in n) n[k]++;
  }
  return ["finished", "idle", "background", "working"]
    .filter((k) => n[k] > 0)
    .map((k) => n[k] + " " + k)
    .join(" · ");
}

// BOARD_TITLE is the browser tab's title when nothing needs you.
export const BOARD_TITLE = "herddash";

// documentTitle is the browser tab's title: "(N) herddash · Needs you" when N
// Agents need you, else "herddash".
export function documentTitle(views) {
  const n = views.filter((v) => v.attention_class === "needs_you").length;
  return n > 0 ? "(" + n + ") " + BOARD_TITLE + " · Needs you" : BOARD_TITLE;
}

// titleBar is what the title bar's summary shows (ADR-0013): {needs,
// needsText, link, linkText}. needs counts the Agents that need you, 0 while
// the Boxes are hidden, as documentTitle's. link is the summary's connection
// tone: "lost" (the Daemon's stream), "down" (herdr disconnected), "bad" (the
// gate hides the Boxes), "warn" (advisory), "none" (no state yet) or "ok"; linkText names herdr and
// its version, or what is wrong. It repeats the strips' facts, never adds one.
export function titleBar(views, connection, lost, status) {
  const needs = status.hideBoxes ? 0 : views.filter((v) => v.attention_class === "needs_you").length;
  const needsText = needs > 0 ? needs + (needs === 1 ? " needs you" : " need you") : "Nothing needs you";
  const c = isObject(connection) ? connection : null;
  const v = c && typeof c.herdr_version === "string" && c.herdr_version ? "herdr " + c.herdr_version : "herdr";
  let link = "ok";
  let linkText = v;
  if (lost) {
    link = "lost";
    linkText = "Reconnecting…";
  } else if (c && c.connected !== true) {
    link = "down";
    linkText = "herdr disconnected";
  } else if (!c) {
    link = "none";
  } else if (status.hideBoxes) {
    link = "bad";
  } else if (c && c.gate === "advisory") {
    link = "warn";
  }
  return { needs, needsText, link, linkText };
}

// layout splits views into what the Board draws: the top zone (Needs you
// alone) and the lower zone's workspace groups (Finished, Idle, Background,
// Working, Unknown), each in Board order with its header label and counts.
export function layout(views) {
  const sorted = sortViews(views);
  const headers = workspaceHeaders(sorted);
  const top = sorted.slice(0, headers.length ? headers[0] : sorted.length);
  const groups = headers.map((start, h) => {
    const end = h + 1 < headers.length ? headers[h + 1] : sorted.length;
    const views = sorted.slice(start, end);
    return { label: headerLabel(sorted[start]), counts: groupCounts(views), views };
  });
  return { top, groups };
}

// recapOf is a view's session recap (ADR-0010): Claude Code's own
// two-sentence summary, sent only when the operator turned recaps on; ""
// when there is none.
export function recapOf(view) {
  return typeof view.recap === "string" ? view.recap : "";
}

// contextText is what a Box shows in its Context Line's row: the recap when
// there is one, else the Context Line (ADR-0010).
export function contextText(view) {
  return recapOf(view) || view.context_line || "";
}

// anyRecap reports whether any of views has a recap. While one does, every
// Box is one line taller and its context row two lines high, so every Box
// keeps one height; with none, the Board is as before ADR-0010.
export function anyRecap(views) {
  return views.some((v) => recapOf(v) !== "");
}

// boxLabel is a Box's accessible name at nowMs: everything it shows,
// untruncated, so keyboard and screen-reader users can read it in full:
// workspace and tab, pane, the recap in full or else the Context Line,
// directory, heading, class, its background tasks, and time.
export function boxLabel(view, nowMs) {
  const w = hasWaitingSince(view)
    ? "waiting " + waitingLabel(view, nowMs).replace("≥ ", "at least ")
    : "Waiting Time unknown";
  return [whereLabel(view), paneLabel(view), contextText(view), view.directory, view.heading, classLabel(view), tasksLabel(view), w]
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
// nobody listens for. keepalive (ADR-0012) changes no model: it is the
// Daemon's sign of life on a quiet stream.
export const EVENT_NAMES = Object.freeze(["state", "agent", "removed", "transition", "resync", "connection", "keepalive"]);

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
      return { model: { agents, connection: d.connection, runId: d.run_id } };
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
// and a state event resets it. These and STREAM_SILENCE_MS are listed under
// "Fixed timings" in docs/config.example.toml; change it with them.
export const RECONNECT_START_MS = 1000;
export const RECONNECT_CAP_MS = 30000;

// nextReconnectDelay is the wait after one of delay ms.
export function nextReconnectDelay(delay) {
  return Math.min(delay * 2, RECONNECT_CAP_MS);
}

// STREAM_SILENCE_MS is how long the current source may go without any event,
// keepalive included, before the Board takes the stream for lost: a hung or
// stopped Daemon, or a half-open socket, sends nothing, and EventSource sees
// no error. The Daemon sends a keepalive every 15 s (ADR-0012); a Go test
// keeps this above twice that.
export const STREAM_SILENCE_MS = 35000;

// streamState is the Board's stream state before the first source opens:
// the model; the current source (any token board.mjs compares with ===);
// whether a reopen is pending; the next reopen delay; whether the current
// source has yet to deliver a valid state; lostSince, when (ms) the stream
// was first in trouble since the last valid state, or null (see streamLost);
// lastEventAt, when (ms) the current source last showed a sign of life (it
// was opened or connected, or delivered an event of any name), or null; and
// lastSeq, the view sequence of the last event checked on it, or null.
export function streamState() {
  return {
    model: emptyModel(),
    source: null,
    pending: false,
    delay: RECONNECT_START_MS,
    awaitingState: true,
    lostSince: null,
    lastEventAt: null,
    lastSeq: null,
  };
}

// seqOf is an event id as a view sequence, or null when it is empty, absent
// or not an integer.
function seqOf(id) {
  return typeof id === "string" && /^[0-9]+$/.test(id) ? Number(id) : null;
}

// reopen is the decision to give up on the current source: close it, and
// open a new one after the current delay, the delay after that doubling up to
// the cap. The stream is in trouble from lostSince, and the source's events
// are ignored from now on until a valid state.
function reopen(st, lostSince, warning) {
  const d = {
    st: { ...st, lostSince, pending: true, awaitingState: true, delay: nextReconnectDelay(st.delay) },
    reopenAfter: st.delay,
    close: st.source,
  };
  if (warning) d.warning = warning;
  return d;
}

// streamDecision is the Board's stream logic, one input at a time. It never
// reads the clock: every input that needs the time carries it as now (ms). It
// returns {st, reopenAfter, close, warning, reload}: the new state; when
// board.mjs must give up on a source, the source to close and the delay in ms
// after which to open a new one; a warning to log; and reload when the page
// must reload. Every reopen goes through the same backoff, and none is
// decided while one is already pending.
//
//   - {kind: "open", source, now}: source is now current; no reopen is
//     pending, its events are ignored until it delivers a valid state, and
//     its silence is measured from now.
//   - {kind: "connected", source, now}: EventSource's own open event, also
//     when it has reconnected by itself. For the current source, its events
//     are again ignored until a valid state (a reconnected stream begins with
//     one), and its silence is measured from now.
//   - {kind: "error", source, closed, now}: when source is current, the
//     stream is in trouble from now (lostSince, kept from earlier trouble)
//     until the next valid state. When it is also closed (the EventSource
//     gave up, as on a non-200 reply), reopen. Otherwise no reopen:
//     EventSource retries a dropped stream itself.
//   - {kind: "tick", now}: when the current source has shown no sign of life
//     for more than STREAM_SILENCE_MS, the Daemon is hung or the socket
//     half-open: reopen, in trouble since that last sign of life, which is
//     already past LOST_GRACE_MS, so the lost strip shows at once.
//   - {kind: "event", source, name, data, id, now}: id is the event's id (its
//     view sequence) as EventSource gives it. An event from a source that is
//     not current is ignored, and is no sign of life; any other event is one.
//     Every event before a valid state on the current source is ignored, so a
//     malformed state on a new stream cannot leave the old model to be merged
//     into. A valid state replaces the model, resets the delay, ends a loss of
//     the stream and sets lastSeq from its id. A later agent, transition,
//     removed or connection event whose id is an integer must carry lastSeq
//     or lastSeq + 1 (one publication's events share its sequence, and each
//     publication adds one); anything else is a skipped publication. A
//     skipped publication, or an event (state included) that applyEvent
//     rejects, reopens the stream with a warning, in trouble from now: the
//     reopened stream's first event is the whole state, the only fresh load
//     that cannot lose a change (AD-7). keepalive and resync change no model
//     and are never checked; a resync's id still advances lastSeq, since a
//     publication may hold nothing but a resync.
export function streamDecision(st, input) {
  switch (input.kind) {
    case "open":
      return {
        st: { ...st, source: input.source, pending: false, awaitingState: true, lastEventAt: input.now ?? null, lastSeq: null },
      };
    case "connected":
      if (input.source !== st.source) return { st };
      return { st: { ...st, awaitingState: true, lastEventAt: input.now ?? st.lastEventAt, lastSeq: null } };
    case "error": {
      if (input.source !== st.source) return { st };
      const lostSince = st.lostSince ?? input.now;
      if (!input.closed || st.pending) return { st: st.lostSince != null ? st : { ...st, lostSince } };
      return reopen(st, lostSince);
    }
    case "tick": {
      if (st.source === null || st.pending || typeof st.lastEventAt !== "number") return { st };
      if (!(input.now - st.lastEventAt > STREAM_SILENCE_MS)) return { st };
      return reopen(st, st.lostSince ?? st.lastEventAt);
    }
    case "event": {
      if (input.source !== st.source) return { st };
      const at = typeof input.now === "number" ? { ...st, lastEventAt: input.now } : st;
      if (st.awaitingState && input.name !== "state") return { st: at };
      const n = seqOf(input.id);
      if (!HANDLED_EVENTS.includes(input.name)) {
        return { st: input.name === "resync" && n !== null ? { ...at, lastSeq: n } : at };
      }
      // While a reopen is pending this source is already being given up on.
      const trouble = (warning) =>
        st.pending ? { st: at, warning } : reopen(at, st.lostSince ?? input.now ?? null, warning + "; reopening the stream");
      if (input.name !== "state" && n !== null && st.lastSeq !== null && n !== st.lastSeq && n !== st.lastSeq + 1) {
        return trouble("the stream skipped from view " + st.lastSeq + " to " + n);
      }
      const r = applyEvent(st.model, input.name, input.data);
      if (r.warning) return trouble(r.warning);
      if (input.name === "state") {
        // Another Daemon run, such as one restarted onto a new build: the page
        // reloads, so it never draws new data with old code.
        if (st.model.runId && r.model.runId && r.model.runId !== st.model.runId) return { st: at, reload: true };
        return { st: { ...at, model: r.model, awaitingState: false, delay: RECONNECT_START_MS, lostSince: null, lastSeq: n } };
      }
      return { st: { ...at, model: r.model, lastSeq: n !== null ? n : st.lastSeq } };
    }
  }
  return { st };
}

// LOST_GRACE_MS is how long the stream may be in trouble, with no valid state
// since the trouble began, before the Board shows it has lost the Daemon: a
// transient drop that EventSource's own retry heals does not flash the strip.
export const LOST_GRACE_MS = 3000;

// streamLost reports whether, at nowMs, the Board has lost the Daemon's
// stream: trouble on it (an error, silence past STREAM_SILENCE_MS, a skipped
// publication or a rejected event) at least LOST_GRACE_MS ago and no valid
// state since.
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
  // An empty or absent herdr_version is one herddash could not read, which
  // the gate treats as below the minimum; say so rather than call it old.
  const unread = c !== null && !(typeof c.herdr_version === "string" && c.herdr_version);
  if (gate === "advisory") {
    let t = unread
      ? "herddash could not read herdr's version; recording under the dogfooding flag."
      : "herdr " + c.herdr_version + " is below the supported minimum " + versionLabel(c.herdr_minimum) +
        "; recording under the dogfooding flag.";
    if (c.resync_guaranteed !== true) t += " Resync is not guaranteed with this herdr version.";
    strips.push({ kind: "advisory", parts: [{ text: t }] });
  } else if (gate === "refused") {
    const needs = versionLabel(c.herdr_minimum) + " or newer and is recording nothing.";
    strips.push({
      kind: "refused",
      parts: [
        {
          text: unread
            ? "herddash could not read herdr's version. It needs " + needs
            : "herdr " + c.herdr_version + " is not supported. herddash needs " + needs,
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
