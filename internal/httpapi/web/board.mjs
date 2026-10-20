// The Board's DOM rendering (Story 3.7). It opens /events and, on each state
// event, draws one Box per AgentView, in the layout order.mjs computes. Order,
// layout and formatting live in order.mjs; this file only builds elements and
// ticks the Waiting Times each second. Other event names are Story 3.8's, and
// the strips are Story 3.9's.

import {
  boxLabel,
  classLabel,
  hasWaitingSince,
  layout,
  neverWorking,
  waitingLabel,
  whereLabel,
} from "./order.mjs";

const board = document.getElementById("board");

// ticking holds every Box whose Waiting Time ticks: {view, wait, box}.
let ticking = [];

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

function renderBox(view, now) {
  const box = el("article", "box box--" + view.attention_class);
  if (neverWorking(view)) box.classList.add("box--never-working");
  box.tabIndex = 0;
  box.setAttribute("aria-label", boxLabel(view, now));

  const row = el("p", "status-row");
  row.appendChild(el("span", "class", classLabel(view)));
  const known = hasWaitingSince(view);
  const wait = el("span", known ? "wait" : "wait wait--none", waitingLabel(view, now));
  if (known) ticking.push({ view, wait, box });
  else wait.title = "Waiting Time unknown";
  row.appendChild(wait);
  box.appendChild(row);

  box.appendChild(el("p", "heading line", view.heading));

  // Always present, so an empty Context Line keeps its row.
  const ctx = el("p", "context line", view.context_line);
  if (view.context_line) ctx.title = view.context_line;
  else ctx.setAttribute("aria-hidden", "true");
  box.appendChild(ctx);

  // Truncated from the start in CSS, so the project segment stays visible.
  const dir = el("p", "dir line");
  if (view.directory) {
    dir.appendChild(el("bdi", null, view.directory));
    dir.title = view.directory;
  }
  box.appendChild(dir);

  const where = whereLabel(view);
  const wh = el("p", "where line", where);
  if (where) wh.title = where;
  box.appendChild(wh);
  return box;
}

function grid(views, now) {
  const g = el("div", "grid");
  for (const v of views) g.appendChild(renderBox(v, now));
  return g;
}

// render replaces the Board with the state event's AgentViews.
function render(agents) {
  const now = Date.now();
  const { top, groups } = layout(agents);
  ticking = [];

  const frag = document.createDocumentFragment();
  if (top.length) {
    const zone = el("section", "zone");
    zone.setAttribute("aria-label", "Needs you and Finished");
    zone.appendChild(el("h2", "sr-only", "Needs you and Finished"));
    zone.appendChild(grid(top, now));
    frag.appendChild(zone);
  }
  if (groups.length) {
    const zone = el("section", "zone");
    zone.setAttribute("aria-label", "Working and Unknown, by workspace");
    zone.appendChild(el("h2", "sr-only", "Working and Unknown"));
    for (const g of groups) {
      const group = el("div", "group");
      const hd = el("h3", "ws-header", g.label);
      hd.title = g.label;
      group.appendChild(hd);
      group.appendChild(grid(g.views, now));
      zone.appendChild(group);
    }
    frag.appendChild(zone);
  }
  board.replaceChildren(frag);
}

function tick() {
  const now = Date.now();
  for (const t of ticking) {
    t.wait.textContent = waitingLabel(t.view, now);
    t.box.setAttribute("aria-label", boxLabel(t.view, now));
  }
}

// EventSource reconnects by itself, and every stream opens with state. A
// state that does not parse, or is not an object with an agents array, keeps
// the last good rendering.
const events = new EventSource("/events");
events.addEventListener("state", (e) => {
  let state;
  try {
    state = JSON.parse(e.data);
  } catch (err) {
    console.warn("herddash: state event does not parse; keeping the last rendering", err);
    return;
  }
  if (state === null || typeof state !== "object" || !Array.isArray(state.agents)) {
    console.warn("herddash: state event has no agents array; keeping the last rendering");
    return;
  }
  render(state.agents);
});

setInterval(tick, 1000);
