// Board ordering and formatting (Story 3.7). A plain ES module: no DOM, no
// browser globals, so `node --test` checks it against core's golden fixture
// (internal/core/testdata/board-order.json).
//
// The Daemon computes class, zone, group, heading and the sort key (AD-1).
// This module only orders AgentViews by that key, finds where workspace
// headers go and formats what a Box shows. It re-derives nothing.

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
