// The Board's DOM rendering (Stories 3.7 to 3.9). It opens /events, feeds
// every event to order.mjs's streamDecision and re-renders in place when the
// model changes: Boxes are matched by AgentKey (data-key), so an update
// changes, moves, adds or removes only the Boxes concerned, and keyboard focus
// stays on the same Agent's Box. Order, layout, formatting, event handling and
// the reconnect decisions live in order.mjs; this file only builds and places
// elements, ticks the Waiting Times each second and executes those decisions.
// Which connection and version strips show, their text, and whether Boxes are
// dimmed or hidden are order.mjs's boardStatus; this file draws them.

import {
  EVENT_NAMES,
  boardStatus,
  boxLabel,
  classLabel,
  hasWaitingSince,
  keyId,
  layout,
  modelViews,
  neverWorking,
  streamDecision,
  streamLost,
  streamState,
  stripsKey,
  waitingLabel,
  whereLabel,
} from "./order.mjs";

const board = document.getElementById("board");
const stripsEl = document.getElementById("strips");

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
// keeps focus. It returns the Waiting Time element when it ticks.
function fillBox(box, view, now) {
  box.className = "box box--" + view.attention_class;
  if (neverWorking(view)) box.classList.add("box--never-working");

  // First, and the most prominent text: how the operator finds the pane.
  // The workspace gives way before the tab: the tab is what a long workspace
  // name would otherwise push out of sight.
  const where = whereLabel(view);
  const wh = el("p", "where");
  if (view.workspace) wh.appendChild(el("span", "ws", view.workspace));
  if (view.workspace && view.tab) wh.appendChild(el("span", "sep", "›"));
  if (view.tab) wh.appendChild(el("span", "tab", view.tab));
  if (where) wh.title = where;

  // Always present, so an empty Context Line keeps its row.
  const ctx = el("p", "context line", view.context_line);
  if (view.context_line) ctx.title = view.context_line;
  else ctx.setAttribute("aria-hidden", "true");

  // Truncated from the start in CSS, so the project segment stays visible.
  const dir = el("p", "dir line");
  if (view.directory) {
    dir.appendChild(el("bdi", null, view.directory));
    dir.title = view.directory;
  }

  // Last row: profile · program on the left, the Attention Class and Waiting
  // Time on the right, bold.
  const row = el("p", "status-row");
  row.appendChild(el("span", "heading", view.heading));
  const state = el("span", "state");
  state.appendChild(el("span", "class", classLabel(view)));
  const known = hasWaitingSince(view);
  const wait = el("span", known ? "wait" : "wait wait--none", waitingLabel(view, now));
  if (!known) wait.title = "Waiting Time unknown";
  state.appendChild(wait);
  row.appendChild(state);

  box.replaceChildren(wh, ctx, dir, row);
  return known ? wait : null;
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

// setLabel sets a Box's aria-label at now, only when its text changes.
function setLabel(b, now) {
  const label = boxLabel(b.view, now);
  if (label === b.label) return;
  b.label = label;
  b.box.setAttribute("aria-label", label);
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

function zone(label, heading) {
  const z = el("section", "zone");
  z.setAttribute("aria-label", label);
  z.appendChild(el("h2", "sr-only", heading));
  return z;
}

const topZone = zone("Needs you and Finished", "Needs you and Finished");
const topGrid = el("div", "grid");
topZone.appendChild(topGrid);
const lowerZone = zone("Working and Unknown, by workspace", "Working and Unknown");

function groupFor(id, label) {
  let g = groups.get(id);
  if (!g) {
    const group = el("div", "group");
    const header = el("h3", "ws-header");
    const grid = el("div", "grid");
    group.append(header, grid);
    g = { group, header, grid };
    groups.set(id, g);
  }
  if (g.header.textContent !== label) {
    g.header.textContent = label;
    g.header.title = label;
  }
  return g;
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

function currentStatus(now) {
  return boardStatus(st.model.connection, streamLost(st, now), now);
}

// hiddenFocus is the keyId of the Box that had focus when the Boxes were
// hidden (gate refused), so focus returns to it when they come back.
let hiddenFocus = null;

// render brings the Board in line with the model and the stream state.
function render() {
  const now = Date.now();
  const status = currentStatus(now);
  drawStatus(status);
  let focused = document.activeElement?.dataset?.key;
  if (status.hideBoxes) {
    if (focused) hiddenFocus = focused;
  } else if (hiddenFocus) {
    if (!focused && (!document.activeElement || document.activeElement === document.body)) focused = hiddenFocus;
    hiddenFocus = null;
  }
  const views = modelViews(st.model);
  const { top, groups: lower } = layout(views);

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
    const ge = groupFor(id, g.label);
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
    if (b && document.activeElement !== b.box) b.box.focus({ preventScroll: true });
  }
}

// tick advances the Waiting Times and the snapshot age, and shows a lost
// stream once its grace period has passed. Boxes hidden or shown follow only
// the model, so tick never needs a full render.
function tick() {
  const now = Date.now();
  drawStatus(currentStatus(now));
  for (const b of boxes.values()) {
    if (!b.wait) continue;
    const w = waitingLabel(b.view, now);
    if (b.wait.textContent !== w) b.wait.textContent = w;
    setLabel(b, now);
  }
}

// The stream. board.mjs only executes streamDecision's decisions: which
// events reach the model, and when to open a new source after one closes for
// good (a non-200 reply, such as the 503 while the Daemon restarts).
// EventSource retries a dropped stream by itself.
function step(input) {
  const before = st.model;
  const d = streamDecision(st, input);
  st = d.st;
  if (d.warning) console.warn("herddash: " + d.warning);
  if (d.reopenAfter !== undefined) {
    input.source.close();
    setTimeout(open, d.reopenAfter);
  }
  if (st.model !== before) render();
  else drawStatus(currentStatus(Date.now()));
}

function open() {
  const s = new EventSource("/events");
  step({ kind: "open", source: s });
  for (const name of EVENT_NAMES) {
    s.addEventListener(name, (e) => step({ kind: "event", source: s, name, data: e.data }));
  }
  s.addEventListener("error", () => step({ kind: "error", source: s, closed: s.readyState === EventSource.CLOSED, now: Date.now() }));
}

open();
setInterval(tick, 1000);
