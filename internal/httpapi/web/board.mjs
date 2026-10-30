// The Board's DOM rendering (Stories 3.7 to 3.9). It opens /events, feeds
// every event to order.mjs's streamDecision and re-renders in place when the
// model changes: Boxes are matched by AgentKey (data-key), so an update
// changes, moves, adds or removes only the Boxes concerned, and keyboard focus
// stays on the same Agent's Box. Order, layout, formatting, event handling and
// the reconnect decisions live in order.mjs; this file only builds and places
// elements, ticks the Waiting Times each second and executes those decisions.
// Which connection, version and recording strips show, their text, and
// whether Boxes are dimmed or hidden are order.mjs's boardStatus; this file
// draws them.
//
// The acknowledgement controls (ADR-0014, Story 6.4) are ordinary buttons,
// with no key binding: a Seen pill on every Finished Box, a Mark unseen pill
// on every Seen Box (ADR-0015, Story 6.8), a heading's bulk mark, and the
// Undo bar. A click only sends the request; the Box changes when the stream's
// agent event says so, never optimistically. The in-view tag on a Finished
// Box whose pane herdr has focused (ADR-0015) is a label, not a control.

import {
  ACK_TOAST_MS,
  EVENT_NAMES,
  IN_VIEW_DOT,
  IN_VIEW_WORDS,
  IN_VIEW_TITLE,
  ackLabel,
  ackBody,
  ackError,
  ackPath,
  ackToast,
  anyRecap,
  boardStatus,
  boxKind,
  boxLabel,
  classLabel,
  stateCount,
  contextText,
  documentTitle,
  hasWaitingSince,
  inNeedsYou,
  inViewTag,
  keyId,
  layout,
  modelViews,
  recapOf,
  streamDecision,
  streamLost,
  streamState,
  stripsKey,
  waitingLabel,
  tabLabel,
  titleBar,
  unackLabel,
  unseenFinished,
} from "./order.mjs";

const board = document.getElementById("board");
const stripsEl = document.getElementById("strips");
const summaryNeeds = document.getElementById("summary-needs");
const summaryLink = document.getElementById("summary-link");

// st is the stream state streamDecision keeps: the model among it.
let st = streamState();

// boxes holds every drawn Box by keyId: {box, json, view, wait, label}. json
// is the view it was drawn from, so an unchanged view is not redrawn; label
// is its aria-label, set only when its text changes.
const boxes = new Map();

// groups holds the lower zone's workspace groups by sort_key[1].
const groups = new Map();

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

// fillBox draws view into box, an article kept across updates so that it
// keeps focus. It returns the Waiting Time element when it ticks. Four fixed
// rows (the design approved on 2026-10-21): the tab and pane badge; the
// workspace in the Needs you zone, else the directory (the group heading
// already names the workspace); the Context Line, or Claude Code's recap in
// its place, clamped to two lines (ADR-0010); the heading, class and Waiting
// Time.
function fillBox(box, view, now) {
  box.className = "box box--" + boxKind(view);

  // First, and the most prominent text: the tab, with the pane's position
  // in it when the tab has more than one.
  const wh = el("p", "where");
  wh.appendChild(el("span", "tab", tabLabel(view)));
  if (view.pane_position) wh.appendChild(el("span", "pane", view.pane_position));
  // A Finished Box whose pane herdr has focused shows the in-view tag, a
  // label and not a control (ADR-0015).
  if (inViewTag(view)) {
    // The dot is decorative: hidden from assistive technology, like the
    // Seen pill's icon, so a screen reader reads only "in view".
    const tag = el("span", "in-view");
    const dot = el("span", "in-view-dot", IN_VIEW_DOT);
    dot.setAttribute("aria-hidden", "true");
    tag.append(dot, " " + IN_VIEW_WORDS);
    tag.title = IN_VIEW_TITLE;
    wh.appendChild(tag);
  }
  // A Finished Box, and no other kind, ends its first line with the Seen
  // pill (ADR-0014); a Seen Box, and no other kind, with Mark unseen
  // (ADR-0015).
  const kind = boxKind(view);
  if (kind === "finished") wh.appendChild(ackButton(view));
  else if (kind === "seen") wh.appendChild(unackButton(view));

  // Second: in the Needs you zone the workspace; in a workspace group the
  // directory, truncated from the start in CSS, so the project segment stays
  // visible.
  let second;
  if (inNeedsYou(view)) {
    second = el("p", "workspace line", view.workspace);
  } else {
    second = el("p", "dir line");
    if (view.directory) second.appendChild(el("bdi", null, view.directory));
  }

  // Always present, so an empty Context Line keeps its row. A recap takes
  // the row in its place, up to two lines, the rest in the label. The text
  // is a pane's process's to set, so it sits in a <bdi>: whatever direction
  // it takes cannot reorder the rest of the Box (the Daemon also drops bidi
  // controls from the Context Line).
  const text = contextText(view);
  const ctx = el("p", recapOf(view) ? "context recap" : "context line");
  if (text) ctx.appendChild(el("bdi", null, text));
  else ctx.setAttribute("aria-hidden", "true");

  // Last row: the heading on the left, the class and Waiting Time on the
  // right, bold and never truncated.
  const row = el("p", "status-row");
  row.appendChild(el("span", "heading", view.heading));
  const state = el("span", "state");
  const cls = el("span", "class", classLabel(view));
  const count = stateCount(view);
  if (count) cls.appendChild(el("span", "count", count));
  state.appendChild(cls);
  const known = hasWaitingSince(view);
  const wait = el("span", known ? "wait" : "wait wait--none", waitingLabel(view, now));
  state.appendChild(wait);
  row.appendChild(state);

  box.replaceChildren(wh, second, ctx, row);
  return known ? wait : null;
}

// ackButton is a Finished Box's Seen pill, marking view's finish seen.
function ackButton(view) {
  const b = el("button", "ack");
  b.type = "button";
  b.title = "Mark seen: you have read this Agent's result";
  b.setAttribute("aria-label", ackLabel(view));
  const icon = el("span", "ack-icon", "✓");
  icon.setAttribute("aria-hidden", "true");
  b.append(icon, el("span", "ack-text", "Seen"));
  b.disabled = controlsOff || pending.has(keyId(view.key));
  b.addEventListener("click", (e) => {
    e.stopPropagation();
    acknowledge([view], true, { bulk: false });
  });
  return b;
}

// unackButton is a Seen Box's Mark unseen pill, showing view's finish as
// unread again (ADR-0015): the remedy for a mark the operator disagrees
// with, automatic or not. The Daemon then leaves that finish Finished.
function unackButton(view) {
  const b = el("button", "unack", "Mark unseen");
  b.type = "button";
  b.title = "Mark not seen: show this result as unread again";
  b.setAttribute("aria-label", unackLabel(view));
  b.disabled = controlsOff || pending.has(keyId(view.key));
  b.addEventListener("click", (e) => {
    e.stopPropagation();
    acknowledge([view], false, { bulk: false });
  });
  return b;
}

// boxFor returns the Box for view, drawing it only when it is new or its
// view changed.
function boxFor(view, now) {
  const id = keyId(view.key);
  const json = JSON.stringify(view);
  let b = boxes.get(id);
  if (!b) {
    const box = el("article");
    box.tabIndex = 0;
    box.dataset.key = id;
    b = { box, json: null, view, wait: null, label: null };
    boxes.set(id, b);
  }
  if (b.json !== json) {
    b.json = json;
    b.view = view;
    b.wait = fillBox(b.box, view, now);
    setLabel(b, now);
  }
  return b.box;
}

// setLabel sets a Box's accessible name and hover title at now, only when
// their text changes. The rows carry no titles of their own, so hovering
// anywhere on the Box shows everything it holds, untruncated.
function setLabel(b, now) {
  const label = boxLabel(b.view, now);
  if (label === b.label) return;
  b.label = label;
  b.box.setAttribute("aria-label", label);
  b.box.title = label;
}

// place makes parent's element children exactly want, in order, moving only
// those out of place.
function place(parent, want) {
  for (let i = 0; i < want.length; i++) {
    const at = parent.children[i];
    if (at !== want[i]) parent.insertBefore(want[i], at || null);
  }
  while (parent.children.length > want.length) parent.lastElementChild.remove();
}

function zone(label) {
  const z = el("section", "zone");
  z.setAttribute("aria-label", label);
  return z;
}

// The Needs you zone has a plain, visible heading; the lower zone's heading is
// for screen readers only, since each group's heading names its workspace.
const topZone = zone("Needs you");
const topHead = el("div", "group-head");
topHead.appendChild(el("h2", "group-label", "Needs you"));
const topGrid = el("div", "grid");
topZone.append(topHead, topGrid);
const lowerZone = zone("By workspace");
lowerZone.appendChild(el("h2", "sr-only", "By workspace"));

function groupFor(id, label, counts) {
  let g = groups.get(id);
  if (!g) {
    const group = el("section", "group");
    const head = el("div", "group-head");
    const header = el("h3", "group-label");
    const count = el("span", "count");
    // The heading's bulk mark (ADR-0014): every Finished Box the group shows
    // now, each by its exact finish.
    const bulk = el("button", "ack-all");
    bulk.type = "button";
    bulk.hidden = true;
    head.append(header, count, bulk);
    const grid = el("div", "grid");
    group.append(head, grid);
    g = { group, header, count, grid, bulk, views: [] };
    bulk.addEventListener("click", () => {
      const vs = unseenFinished(g.views);
      if (vs.length) acknowledge(vs, true, { bulk: true, group: id });
    });
    groups.set(id, g);
  }
  if (g.header.textContent !== label) {
    g.header.textContent = label;
    g.header.title = label;
  }
  if (g.count.textContent !== counts) g.count.textContent = counts;
  return g;
}

// drawBulk shows a group's bulk mark when it has 2 or more Finished Boxes.
function drawBulk(g, label, views) {
  g.views = views;
  const n = unseenFinished(views).length;
  g.bulk.hidden = n < 2;
  const text = "✓ Mark " + n + " finished seen";
  if (g.bulk.textContent !== text) g.bulk.textContent = text;
  const aria = "Mark " + n + " finished Agents in " + label + " seen";
  if (g.bulk.getAttribute("aria-label") !== aria) g.bulk.setAttribute("aria-label", aria);
}

// ---- Acknowledgement requests and the Undo bar (ADR-0014) ----

// controlsOff is set while the Boxes are dimmed or hidden: a Box the Board
// cannot vouch for is not one to mark. pending holds the keyIds, and
// pendingGroups the group ids, whose request is in flight.
let controlsOff = false;
const pending = new Set();
const pendingGroups = new Set();

// The Undo bar, one for the page; a new mark replaces its text and timer.
const toast = el("div", "toast");
toast.setAttribute("role", "status");
const toastText = el("span", "toast-text");
const toastUndo = el("button", "toast-undo", "Undo");
toastUndo.type = "button";
toast.append(toastText, toastUndo);
document.body.appendChild(toast);
let toastTimer = null;
let toastAction = null;
// toastFocus is the keyId of the Box the bar is about: when the bar closes
// with its Undo button focused, focus goes to that Box, not to the page.
let toastFocus = null;

function showToast(text, undo, focusId) {
  toastText.textContent = text;
  toastAction = undo;
  toastFocus = focusId ?? null;
  toastUndo.hidden = !undo;
  toast.classList.add("toast--on");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(hideToast, ACK_TOAST_MS);
  syncControls();
}

function hideToast() {
  clearTimeout(toastTimer);
  toastTimer = null;
  toastAction = null;
  if (toast.contains(document.activeElement)) {
    const b = toastFocus && boxes.get(toastFocus);
    if (b && b.box.isConnected) b.box.focus({ preventScroll: true });
    else document.activeElement.blur();
  }
  toastFocus = null;
  toast.classList.remove("toast--on");
}

toastUndo.addEventListener("click", () => {
  const undo = toastAction;
  hideToast();
  if (undo) undo();
});

// postAck sends one write and resolves to {ok, status, code}; a network
// failure is status 0.
async function postAck(path, body) {
  try {
    const r = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Herddash-Write": "1" },
      body: JSON.stringify(body),
      credentials: "same-origin",
      cache: "no-store",
    });
    if (r.ok) return { ok: true, status: r.status, code: "" };
    let code = "";
    try {
      code = (await r.json())?.error?.code || "";
    } catch {}
    return { ok: false, status: r.status, code };
  } catch {
    return { ok: false, status: 0, code: "" };
  }
}

// acknowledge marks views seen, or with seen false not seen: one view
// per-Agent unless it came from a heading, else the bulk route. how.undo is
// set for the Undo bar's request. The Board changes nothing itself: the
// Boxes change when the stream's agent events arrive. A mark or a Mark
// unseen that succeeds shows the Undo bar, whose Undo asks for the
// opposite; a refusal shows why, with no Undo.
async function acknowledge(views, seen, how) {
  if (controlsOff || !views.length) return;
  const ids = views.map((v) => keyId(v.key));
  if (ids.some((id) => pending.has(id)) || (how.group !== undefined && pendingGroups.has(how.group))) return;
  for (const id of ids) pending.add(id);
  if (how.group !== undefined) pendingGroups.add(how.group);
  syncControls();
  let res;
  try {
    if (how.bulk) res = await postAck(seen ? "/api/seen" : "/api/unseen", ackBody(views));
    else res = await postAck(ackPath(views[0].key, seen), { waiting_since: views[0].waiting_since });
  } finally {
    for (const id of ids) pending.delete(id);
    if (how.group !== undefined) pendingGroups.delete(how.group);
    syncControls();
  }
  if (!res.ok) showToast(ackError(res.status, res.code, seen, !!how.undo), null, ids[0]);
  else if (!how.undo) showToast(ackToast(views, seen), () => acknowledge(views, !seen, { bulk: how.bulk, undo: true }), ids[0]);
}

// setDisabled disables or enables btn. A browser drops a focused control's
// focus to the page when it is disabled, so focus first moves to target (a
// Box), keeping the operator's place on the Board.
function setDisabled(btn, off, target) {
  if (off && !btn.disabled && btn === document.activeElement && target?.isConnected) target.focus({ preventScroll: true });
  btn.disabled = off;
}

// syncControls enables or disables every acknowledgement control.
function syncControls() {
  for (const [id, b] of boxes) {
    for (const btn of b.box.querySelectorAll("button.ack, button.unack")) setDisabled(btn, controlsOff || pending.has(id), b.box);
  }
  for (const [id, g] of groups) setDisabled(g.bulk, controlsOff || pendingGroups.has(id), g.grid.querySelector(".box"));
  setDisabled(toastUndo, controlsOff, toastFocus && boxes.get(toastFocus)?.box);
}

// drawnStrips is the stripsKey of the strips drawn, and stripTick the
// element holding the ticking age, if any.
let drawnStrips = stripsKey([]);
let stripTick = null;

// drawStatus draws the strips and dims the Board from boardStatus at now. The
// live region (aria-atomic false) is redrawn only when a strip appears, goes
// or changes; the ticking age, aria-hidden, is updated in place and never
// replaces an announced node.
function drawStatus(status) {
  board.classList.toggle("board--stale", status.dimmed);
  const off = !!(status.dimmed || status.hideBoxes);
  if (off !== controlsOff) {
    controlsOff = off;
    syncControls();
  }
  drawTitleBar(status);
  const k = stripsKey(status.strips);
  if (k !== drawnStrips) {
    drawnStrips = k;
    stripTick = null;
    stripsEl.replaceChildren(
      ...status.strips.map((s) => {
        const p = el("p", "strip strip--" + s.kind);
        for (const part of s.parts) {
          if (part.tick) {
            const t = el("span", "time", part.text);
            t.setAttribute("aria-hidden", "true");
            p.appendChild(t);
            stripTick = t;
          } else if (part.sr) {
            p.appendChild(el("span", "sr-only", part.text));
          } else {
            p.appendChild(document.createTextNode(part.text));
          }
        }
        return p;
      }),
    );
    return;
  }
  const tick = status.strips.flatMap((s) => s.parts).find((p) => p.tick);
  if (tick && stripTick && stripTick.textContent !== tick.text) stripTick.textContent = tick.text;
}

// drawTitleBar draws the title bar's summary (ADR-0013): the Needs you count
// and herdr's connection, the facts the strips and the browser title already
// carry, so it is not a live region.
function drawTitleBar(status) {
  const t = titleBar(modelViews(st.model), st.model.connection, status.lost, status);
  if (summaryNeeds.textContent !== t.needsText) summaryNeeds.textContent = t.needsText;
  summaryNeeds.classList.toggle("summary-needs--on", t.needs > 0);
  if (summaryLink.textContent !== t.linkText) summaryLink.textContent = t.linkText;
  const cls = "summary-item summary-link summary-link--" + t.link;
  if (summaryLink.className !== cls) summaryLink.className = cls;
}

function currentStatus(now) {
  const lost = streamLost(st, now);
  return { ...boardStatus(st.model.connection, lost, now, st.model.recording), lost };
}

// hiddenFocus is the keyId of the Box that had focus when the Boxes were
// hidden (gate refused), so focus returns to it when they come back.
let hiddenFocus = null;

// render brings the Board in line with the model and the stream state.
function render() {
  const now = Date.now();
  const status = currentStatus(now);
  drawStatus(status);
  // A control inside a Box counts as its Box: a Box redrawn without its pill
  // keeps the focus, and one redrawn with the same pill (an in-view tag come
  // or gone, a Waiting Time restarted) gives it back to that pill.
  let focused = document.activeElement?.closest?.("[data-key]")?.dataset?.key;
  const focusedPill = ["ack", "unack"].find((c) => document.activeElement?.matches?.("button." + c));
  // A heading's bulk mark that this render hides would drop focus to the
  // page; its group keeps it instead (below).
  let bulkFocus;
  for (const [id, g] of groups) if (g.bulk === document.activeElement) bulkFocus = id;
  if (status.hideBoxes) {
    if (focused) hiddenFocus = focused;
  } else if (hiddenFocus) {
    if (!focused && (!document.activeElement || document.activeElement === document.body)) focused = hiddenFocus;
    hiddenFocus = null;
  }
  const views = modelViews(st.model);
  const { top, groups: lower } = layout(views);
  const title = documentTitle(status.hideBoxes ? [] : views);
  if (document.title !== title) document.title = title;
  // While any Box has a recap, every Box is one line taller (ADR-0010).
  board.classList.toggle("board--recaps", anyRecap(views));

  // Boxes whose Agent is gone are dropped first, so no grid shuffles them.
  const live = new Set(views.map((v) => keyId(v.key)));
  for (const [id, b] of boxes) {
    if (!live.has(id)) {
      b.box.remove();
      boxes.delete(id);
    }
  }

  place(topGrid, top.map((v) => boxFor(v, now)));
  const groupEls = [];
  const groupIds = new Set();
  for (const g of lower) {
    const id = g.views[0].sort_key[1];
    groupIds.add(id);
    const ge = groupFor(id, g.label, g.counts);
    drawBulk(ge, g.label, g.views);
    if (bulkFocus === id && ge.bulk.hidden && !focused) focused = keyId(g.views[0].key);
    place(ge.grid, g.views.map((v) => boxFor(v, now)));
    groupEls.push(ge.group);
  }
  for (const id of groups.keys()) if (!groupIds.has(id)) groups.delete(id);
  place(lowerZone, [lowerZone.firstElementChild, ...groupEls]);

  // Refused: no Boxes at all, whatever the model holds.
  const zones = [];
  if (!status.hideBoxes && top.length) zones.push(topZone);
  if (!status.hideBoxes && lower.length) zones.push(lowerZone);
  place(board, zones);

  // A Box moved in the document loses focus; give it back.
  if (focused && !status.hideBoxes) {
    const b = boxes.get(focused);
    if (b && !b.box.contains(document.activeElement)) {
      const pill = focusedPill && b.box.querySelector("button." + focusedPill);
      if (pill && !pill.disabled && !controlsOff) pill.focus({ preventScroll: true });
      else b.box.focus({ preventScroll: true });
    }
  }
  syncControls();
}

// tick advances the Waiting Times and the snapshot age, lets streamDecision
// judge the stream's silence, and shows a lost stream once its grace period
// has passed. Boxes hidden or shown follow only the model, so tick never
// needs a full render.
function tick() {
  const now = Date.now();
  step({ kind: "tick", now });
  for (const b of boxes.values()) {
    if (!b.wait) continue;
    const w = waitingLabel(b.view, now);
    if (b.wait.textContent !== w) b.wait.textContent = w;
    setLabel(b, now);
  }
}

// The stream. board.mjs only executes streamDecision's decisions: which
// events reach the model, and when to give up on a source and open a new one:
// after it closes for good (a non-200 reply, such as the 503 while the Daemon
// restarts), falls silent (a hung Daemon), skips a publication or sends an
// event the Board rejects. EventSource retries a dropped stream by itself.
function step(input) {
  const before = st.model;
  const d = streamDecision(st, input);
  if (d.reload) {
    location.reload();
    return;
  }
  st = d.st;
  if (d.warning) console.warn("herddash: " + d.warning);
  if (d.reopenAfter !== undefined) {
    if (d.close) d.close.close();
    setTimeout(open, d.reopenAfter);
  }
  if (st.model !== before) render();
  else drawStatus(currentStatus(Date.now()));
}

function open() {
  const s = new EventSource("/events");
  step({ kind: "open", source: s, now: Date.now() });
  s.addEventListener("open", () => step({ kind: "connected", source: s, now: Date.now() }));
  for (const name of EVENT_NAMES) {
    s.addEventListener(name, (e) => step({ kind: "event", source: s, name, data: e.data, id: e.lastEventId, now: Date.now() }));
  }
  s.addEventListener("error", () => step({ kind: "error", source: s, closed: s.readyState === EventSource.CLOSED, now: Date.now() }));
}

open();
setInterval(tick, 1000);
