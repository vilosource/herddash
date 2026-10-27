// Headless-Chrome check of the Board's Boxes and workspace groups (layout
// v4), the State label's background task count (ADR-0009), Claude Code's
// recap in the Context Line's row (ADR-0010) and its connection and version
// strips (Story 3.9), including a hung Daemon (Story 5.11): serve is
// SIGSTOPped with the stream left open, the Board must show the lost strip
// and dim its Boxes once STREAM_SILENCE_MS passes, and clear both after
// SIGCONT, and the acknowledgement controls (ADR-0014, Story 6.4): the Seen
// pill, the heading's bulk mark, the Undo bar, Seen across a status change
// and a restart, and a page on another origin refused. Not embedded. CI's board-browser job runs it on ubuntu against the
// runner's Google Chrome (Story 5.6); it also runs by hand:
//
//   go build -o /tmp/herddash ./cmd/herddash
//   node internal/httpapi/web/check/board-browser.mjs /tmp/herddash
//
// On Linux, point it at the browser with CHROME=$(command -v google-chrome).
//
// Node 22 or newer with built-ins only (its global WebSocket speaks Chrome's
// DevTools protocol over --remote-debugging-port; no npm). It runs the given
// herddash binary's serve against a fake herdr on a unix socket in a temporary
// directory, with its own HOME and no HERDR_SOCKET_PATH, so it never reaches
// the live herdr. Set CHROME to the browser binary if it is not at the macOS
// default. It exits 0 when every check passes.

import { spawn } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, existsSync } from "node:fs";
import { createServer } from "node:net";
import { createServer as createHTTPServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ACK_TOAST_MS, STREAM_SILENCE_MS, keyId, sortViews } from "../order.mjs";

const binary = process.argv[2];
if (!binary) {
  console.error("usage: node board-browser.mjs <herddash binary>");
  process.exit(2);
}
const CHROME = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
// More than order.mjs's LOST_GRACE_MS (3 s) plus the one-second tick.
const PAST_GRACE_MS = 4500;
// The one warning the Board is expected to log: the skipped-publication case.
const SKIPPED_WARNING = /^herddash: the stream skipped from view 1 to 3; reopening the stream$/;

const root = mkdtempSync(join(tmpdir(), "hd-check-"));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function until(what, cond, ms = 20000) {
  const end = Date.now() + ms;
  for (;;) {
    const v = await cond();
    if (v) return v;
    if (Date.now() > end) throw new Error("timed out waiting for " + what);
    await sleep(50);
  }
}

let failures = 0;
function check(what, ok, detail) {
  console.log((ok ? "ok     " : "FAIL   ") + what + (ok ? "" : "  " + JSON.stringify(detail)));
  if (!ok) failures++;
}

// ---- Fake herdr: ping, session.snapshot, pane.process_info, events.subscribe ----

const SNAPSHOT = {
  type: "session_snapshot",
  snapshot: {
    version: "0.9.5",
    protocol: 22,
    workspaces: [{ workspace_id: "w1", number: 1, label: "probe", focused: true, pane_count: 1, tab_count: 1, active_tab_id: "w1:t1", agent_status: "working" }],
    tabs: [{ tab_id: "w1:t1", workspace_id: "w1", number: 1, label: "1: build", focused: true, pane_count: 2, agent_status: "blocked" }],
    panes: [
      { pane_id: "w1:p1", terminal_id: "t1", workspace_id: "w1", tab_id: "w1:t1", focused: true, cwd: "/srv/proj", agent: "claude", agent_status: "working", revision: 0 },
      { pane_id: "w1:p2", terminal_id: "t2", workspace_id: "w1", tab_id: "w1:t1", focused: false, cwd: "/srv/proj", agent: "claude", agent_status: "blocked", revision: 0 },
    ],
    layouts: [
      {
        workspace_id: "w1", tab_id: "w1:t1", zoomed: false, focused_pane_id: "w1:p1",
        area: { x: 0, y: 0, width: 120, height: 40 },
        panes: [
          { pane_id: "w1:p1", focused: true, rect: { x: 0, y: 0, width: 60, height: 40 } },
          { pane_id: "w1:p2", focused: false, rect: { x: 60, y: 0, width: 60, height: 40 } },
        ],
        splits: [],
      },
    ],
    agents: [
      { terminal_id: "t1", agent: "claude", agent_status: "working", workspace_id: "w1", tab_id: "w1:t1", pane_id: "w1:p1", focused: true, state_change_seq: 3, revision: 0 },
      { terminal_id: "t2", agent: "claude", agent_status: "blocked", workspace_id: "w1", tab_id: "w1:t1", pane_id: "w1:p2", focused: false, state_change_seq: 2, revision: 0 },
    ],
  },
};

// fakeHerdr answers with snapshot() (SNAPSHOT by default) and keeps each
// pane's status subscription, so push can send that pane a status change.
function fakeHerdr(path, version, snapshot = () => SNAPSHOT) {
  let server = null;
  const conns = new Set();
  const statusSubs = new Map(); // pane id -> its subscription's connections
  const handle = (conn) => {
    conns.add(conn);
    conn.on("close", () => conns.delete(conn));
    conn.on("error", () => {});
    let buf = "";
    let answered = false;
    conn.on("data", (d) => {
      if (answered) return;
      buf += d;
      const nl = buf.indexOf("\n");
      if (nl < 0) return;
      answered = true;
      const req = JSON.parse(buf.slice(0, nl));
      const reply = (result) => conn.write(JSON.stringify({ id: req.id, result }) + "\n");
      switch (req.method) {
        case "ping":
          reply({ type: "pong", version, protocol: 22 });
          break;
        case "session.snapshot":
          reply(snapshot());
          break;
        case "pane.process_info":
          reply({ type: "pane_process_info", process_info: { pane_id: req.params.pane_id, shell_pid: 1, foreground_processes: [] } });
          break;
        case "events.subscribe": {
          reply({ type: "subscription_started" }); // held open until the client closes it
          const sub = (req.params?.subscriptions || []).find((x) => x.type === "pane.agent_status_changed");
          if (sub) {
            if (!statusSubs.has(sub.pane_id)) statusSubs.set(sub.pane_id, new Set());
            statusSubs.get(sub.pane_id).add(conn);
            conn.on("close", () => statusSubs.get(sub.pane_id)?.delete(conn));
          }
          break;
        }
        default:
          conn.end();
      }
    });
  };
  return {
    // subscribed reports whether pane has a live status subscription.
    subscribed: (pane) => (statusSubs.get(pane)?.size || 0) > 0,
    // push sends pane's status subscriptions a status change.
    push(pane, status, workspace) {
      const line = JSON.stringify({ data: { agent: "claude", agent_status: status, pane_id: pane, workspace_id: workspace }, event: "pane.agent_status_changed" });
      for (const c of statusSubs.get(pane) || []) c.write(line + "\n");
    },
    start() {
      return new Promise((r) => {
        server = createServer(handle);
        server.listen(path, r);
      });
    },
    stop() {
      for (const c of conns) c.destroy();
      return new Promise((r) => (server ? server.close(() => r()) : r()));
    },
  };
}

// ---- herddash serve ----

function serve(name, sock, bind, extra = []) {
  const home = join(root, name + "-home");
  const data = join(root, name + "-data");
  const env = { PATH: process.env.PATH, HOME: home };
  const args = ["serve", "--socket", sock, "--server", "test", "--host", "testhost", "--data-dir", data, "--bind", bind, ...extra];
  const p = spawn(binary, args, { env, stdio: "ignore" });
  const exited = new Promise((r) => p.on("exit", r));
  const addr = () =>
    until(name + " listening", () => {
      const log = join(data, "herddash.log");
      if (!existsSync(log)) return null;
      for (const line of readFileSync(log, "utf8").split("\n").reverse()) {
        try {
          const e = JSON.parse(line);
          if (e.msg === "listening" && e.addr) return e.addr;
        } catch {}
      }
      return null;
    });
  return { addr, stop: () => (p.kill("SIGINT"), exited), signal: (sig) => p.kill(sig) };
}

// ---- Chrome over the DevTools protocol ----

async function chrome() {
  const prof = join(root, "chrome");
  const c = spawn(CHROME, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + prof, "--no-first-run", "about:blank"], {
    stdio: ["ignore", "ignore", "pipe"],
  });
  let err = "";
  c.stderr.on("data", (d) => (err += d));
  let browserWs;
  try {
    browserWs = await until("DevTools endpoint", () => (err.match(/ws:\/\/\S+/) || [])[0]);
  } catch (e) {
    const exited = c.exitCode !== null || c.signalCode !== null ? `; Chrome exited (code ${c.exitCode}, signal ${c.signalCode})` : "";
    throw new Error(`${e.message}${exited}; Chrome's stderr:\n${err || "(empty)"}`);
  }
  const ws = new WebSocket(browserWs);
  await new Promise((r, j) => (ws.addEventListener("open", r), ws.addEventListener("error", j)));
  let id = 0;
  const pending = new Map();
  const warnings = [];
  ws.addEventListener("message", (m) => {
    const d = JSON.parse(m.data);
    if (d.id && pending.has(d.id)) {
      pending.get(d.id)(d);
      pending.delete(d.id);
    }
    if (d.method === "Runtime.consoleAPICalled" && d.params.type !== "log") warnings.push(d.params.args.map((a) => a.value).join(" "));
    if (d.method === "Runtime.exceptionThrown") warnings.push(d.params.exceptionDetails.text);
  });
  const send = (method, params = {}, sessionId) =>
    new Promise((r) => {
      const i = ++id;
      pending.set(i, r);
      ws.send(JSON.stringify({ id: i, method, params, sessionId }));
    });
  const { result } = await send("Target.createTarget", { url: "about:blank" });
  const { result: att } = await send("Target.attachToTarget", { targetId: result.targetId, flatten: true });
  const sid = att.sessionId;
  await send("Runtime.enable", {}, sid);
  await send("Page.enable", {}, sid);
  return {
    warnings,
    send: (method, params) => send(method, params, sid),
    open: (url) => send("Page.navigate", { url }, sid),
    eval: async (expr) => (await send("Runtime.evaluate", { expression: expr, returnByValue: true }, sid)).result.result.value,
    close: () => {
      const gone = new Promise((r) => c.on("exit", r));
      c.kill();
      return gone;
    },
  };
}

const SNAP = `JSON.stringify({
  strips: [...document.querySelectorAll("#strips .strip")].map((s) => ({ kind: s.className, text: s.textContent })),
  stale: document.getElementById("board").classList.contains("board--stale"),
  boxes: document.querySelectorAll("#board .box").length,
  logo: document.querySelector(".titlebar h1")?.textContent,
  needs: document.getElementById("summary-needs")?.textContent,
  needsOn: !!document.querySelector(".summary-needs--on"),
  link: document.getElementById("summary-link")?.textContent,
  linkKind: document.getElementById("summary-link")?.className,
})`;

// Each Box in document order: where it sits and what its four rows say.
const BOXES = `JSON.stringify({
  title: document.title,
  headings: [...document.querySelectorAll("#board .group-head")].map((h) => h.querySelector(".group-label").textContent + (h.querySelector(".count")?.textContent ?? "")),
  boxes: [...document.querySelectorAll("#board .box")].map((b) => ({
    kind: b.className,
    inGroup: !!b.closest(".group"),
    tab: b.querySelector(".tab")?.textContent,
    pane: b.querySelector(".pane")?.textContent ?? null,
    second: b.children[1]?.className,
    secondText: b.children[1]?.textContent,
    rows: b.children.length,
    height: b.getBoundingClientRect().height,
    titleIsLabel: b.title !== "" && b.title === b.getAttribute("aria-label"),
  })),
})`;

// ---- Acknowledgement (ADR-0014, Story 6.4) ----

// ACK_PANES are the acknowledgement check's Agents, one tab each: in
// workspace probe a Needs you, a Working, three Finished and an Idle Agent;
// in workspace solo one Finished Agent, so its heading has no bulk mark.
const ACK_PANES = [
  { pane: "w1:p1", ws: "w1", tab: "1: build", status: "working" },
  { pane: "w1:p2", ws: "w1", tab: "2: review", status: "blocked" },
  { pane: "w1:p3", ws: "w1", tab: "3: api", status: "done" },
  { pane: "w1:p4", ws: "w1", tab: "4: docs", status: "done" },
  { pane: "w1:p5", ws: "w1", tab: "5: tests", status: "done" },
  { pane: "w1:p6", ws: "w1", tab: "6: notes", status: "idle" },
  { pane: "w2:p1", ws: "w2", tab: "1: solo", status: "done" },
];

// ackSnapshot is herdr's snapshot of ACK_PANES at their current statuses.
function ackSnapshot() {
  const ws = { w1: "probe", w2: "solo" };
  return {
    type: "session_snapshot",
    snapshot: {
      version: "0.9.5",
      protocol: 22,
      workspaces: Object.entries(ws).map(([id, label], i) => {
        const n = ACK_PANES.filter((p) => p.ws === id).length;
        return { workspace_id: id, number: i + 1, label, focused: i === 0, pane_count: n, tab_count: n, active_tab_id: id + ":t1", agent_status: "working" };
      }),
      tabs: ACK_PANES.map((p, i) => ({
        tab_id: p.pane.replace(":p", ":t"), workspace_id: p.ws, number: i + 1, label: p.tab, focused: i === 0, pane_count: 1, agent_status: p.status,
      })),
      panes: ACK_PANES.map((p, i) => ({
        pane_id: p.pane, terminal_id: "t-" + p.pane, workspace_id: p.ws, tab_id: p.pane.replace(":p", ":t"), focused: i === 0,
        cwd: "/srv/" + p.pane.replace(":", "-"), agent: "claude", agent_status: p.status, revision: 0,
      })),
      layouts: ACK_PANES.map((p) => ({
        workspace_id: p.ws, tab_id: p.pane.replace(":p", ":t"), zoomed: false, focused_pane_id: p.pane,
        area: { x: 0, y: 0, width: 120, height: 40 },
        panes: [{ pane_id: p.pane, focused: true, rect: { x: 0, y: 0, width: 120, height: 40 } }],
        splits: [],
      })),
      agents: ACK_PANES.map((p, i) => ({
        terminal_id: "t-" + p.pane, agent: "claude", agent_status: p.status, workspace_id: p.ws, tab_id: p.pane.replace(":p", ":t"),
        pane_id: p.pane, focused: i === 0, state_change_seq: i + 1, revision: 0,
      })),
    },
  };
}

// ACK_BOARD is every Box and heading as the page draws them now.
const ACK_BOARD = `JSON.stringify({
  stale: document.getElementById("board").classList.contains("board--stale"),
  boxes: [...document.querySelectorAll("#board .box")].map((b) => ({
    pane: JSON.parse(b.dataset.key)[3],
    key: b.dataset.key,
    kind: b.className.replace("box box--", ""),
    group: b.closest(".group")?.querySelector(".group-label")?.textContent ?? null,
    label: b.getAttribute("aria-label"),
    cls: b.querySelector(".class")?.innerText,
    acks: b.querySelectorAll("button.ack").length,
    ackText: b.querySelector("button.ack")?.textContent ?? null,
    ackAria: b.querySelector("button.ack")?.getAttribute("aria-label") ?? null,
    ackTitle: b.querySelector("button.ack")?.title ?? null,
    ackLast: b.querySelector(".where")?.lastElementChild?.matches("button.ack") ?? false,
    ackDisabled: b.querySelector("button.ack")?.disabled ?? null,
    edge: getComputedStyle(b).borderLeftColor,
    edgeWidth: getComputedStyle(b).borderLeftWidth,
  })),
  heads: [...document.querySelectorAll("#board .group")].map((g) => {
    const bulk = g.querySelector(".group-head button.ack-all");
    return {
      label: g.querySelector(".group-label").textContent,
      counts: g.querySelector(".group-head .count").textContent,
      bulk: bulk ? { shown: !bulk.hidden && bulk.getBoundingClientRect().width > 0, text: bulk.textContent,
        aria: bulk.getAttribute("aria-label"), afterCounts: bulk.previousElementSibling?.classList.contains("count"), disabled: bulk.disabled } : null,
    };
  }),
  focus: (() => {
    const a = document.activeElement;
    const k = a?.closest?.("[data-key]");
    return { box: k ? JSON.parse(k.dataset.key)[3] : null, isBox: !!k && a === k,
      group: a?.closest?.(".group")?.querySelector(".group-label")?.textContent ?? null, body: a === document.body || !a };
  })(),
  toast: (() => {
    const t = document.querySelector(".toast");
    if (!t) return null;
    const u = t.querySelector(".toast-undo");
    return { on: t.classList.contains("toast--on"), role: t.getAttribute("role"), text: t.querySelector(".toast-text")?.textContent,
      undo: !!u && !u.hidden, position: getComputedStyle(t).position, visible: getComputedStyle(t).visibility === "visible" };
  })(),
  tokens: (() => {
    const p = document.createElement("div");
    document.body.appendChild(p);
    const c = (v) => { p.style.color = "var(" + v + ")"; return getComputedStyle(p).color; };
    const out = { quiet: c("--border-quiet"), finished: c("--finished-edge") };
    p.remove();
    return out;
  })(),
})`;

async function ackChecks(browser) {
  const sock = join(root, "ack.sock");
  const statuses = new Map(ACK_PANES.map((p) => [p.pane, p]));
  const herdr = fakeHerdr(sock, "0.9.5", ackSnapshot);
  await herdr.start();
  let d = serve("ack", sock, "127.0.0.1:0");
  const addr = await d.addr();
  const base = "http://" + addr;
  const board = async () => JSON.parse(await browser.eval(ACK_BOARD));
  const boardUntil = (what, cond, ms) =>
    until(what, async () => {
      const b = await board();
      return cond(b) ? b : null;
    }, ms);
  const box = (b, pane) => b.boxes.find((x) => x.pane === pane);
  const head = (b, label) => b.heads.find((h) => h.label === label);
  const state = async () => JSON.parse(await (await fetch(base + "/api/state")).text());
  const viewOf = async (pane) => (await state()).agents.find((a) => a.key.pane_id === pane);
  const click = (pane, sel) =>
    browser.eval(`(() => {
      const b = [...document.querySelectorAll("#board .box")].find((x) => JSON.parse(x.dataset.key)[3] === ${JSON.stringify(pane)});
      const btn = b?.querySelector(${JSON.stringify(sel)});
      if (!btn || btn.disabled) return false;
      btn.focus();
      btn.click();
      return true;
    })()`);
  const clickIn = (label, sel) =>
    browser.eval(`(() => {
      const g = [...document.querySelectorAll("#board .group")].find((x) => x.querySelector(".group-label").textContent === ${JSON.stringify(label)});
      const btn = g?.querySelector(${JSON.stringify(sel)});
      if (!btn || btn.disabled || btn.hidden) return false;
      btn.click();
      return true;
    })()`);
  // push changes a pane's status at the fake herdr, for its snapshot and its
  // status subscription alike.
  const push = async (pane, status) => {
    await until(pane + "'s status subscription", () => herdr.subscribed(pane));
    const p = statuses.get(pane);
    p.status = status;
    herdr.push(pane, status, p.ws);
  };
  try {
    await browser.open(base + "/");
    let b = await boardUntil("the acknowledgement Board", (x) => x.boxes.length === ACK_PANES.length && !x.stale);
    // Every status subscription is up before anything is marked, so no
    // Profile lookup or resync is still settling.
    await until("every status subscription", () => ACK_PANES.every((p) => herdr.subscribed(p.pane)));

    const finished = b.boxes.filter((x) => x.kind === "finished");
    check(
      "Seen pill on every Finished Box, at the end of its first line, and on no other kind",
      finished.length === 4 &&
        b.boxes.every((x) => (x.kind === "finished" ? x.acks === 1 && x.ackLast : x.acks === 0)) &&
        ["needs_you", "working", "idle"].every((k) => b.boxes.some((x) => x.kind === k)),
      b.boxes,
    );
    const docs0 = box(b, "w1:p4");
    check(
      "Seen pill: ✓ Seen, its title and an accessible name naming the tab",
      docs0.ackText === "✓Seen" && docs0.ackAria === "Mark 4: docs seen" && docs0.ackTitle === "Mark seen: you have read this Agent's result",
      docs0,
    );
    check(
      "heading: probe offers ✓ Mark 3 finished seen after its counts; solo, with one, offers none",
      head(b, "probe").counts === "3 finished · 1 idle · 1 working" && head(b, "probe").bulk.shown && head(b, "probe").bulk.afterCounts &&
        head(b, "probe").bulk.text === "✓ Mark 3 finished seen" && head(b, "probe").bulk.aria === "Mark 3 finished Agents in probe seen" &&
        head(b, "solo").counts === "1 finished" && head(b, "solo").bulk && !head(b, "solo").bulk.shown,
      b.heads,
    );

    // Background (ADR-0009) takes precedence, seen or not; the fake herdr has
    // no Claude process, so the Daemon's state is sent through a stand-in
    // EventSource with background set on two Finished Agents, one seen.
    {
      const sent = await state();
      for (const a of sent.agents) {
        if (a.key.pane_id === "w1:p3") a.background = true;
        if (a.key.pane_id === "w1:p5") Object.assign(a, { background: true, seen: true });
      }
      const { result: added } = await browser.send("Page.addScriptToEvaluateOnNewDocument", {
        source: `window.EventSource = class extends EventTarget {
          static CLOSED = 2;
          readyState = 1;
          constructor() {
            super();
            window.standIn = (window.standIn || 0) + 1;
            setTimeout(() => this.dispatchEvent(new MessageEvent("state", { data: ${JSON.stringify(JSON.stringify(sent))}, lastEventId: "1" })), 0);
          }
          close() {}
        };`,
      });
      await browser.open(base + "/");
      const x = await boardUntil("the stand-in Background Boxes", (y) => y.boxes.filter((z) => z.kind === "background").length === 2, 5000);
      check(
        "Background Boxes, a seen one included, carry no Seen pill",
        x.boxes.filter((z) => z.kind === "background").every((z) => z.acks === 0 && /Background/.test(z.label)) &&
          head(x, "probe").counts === "1 finished · 1 idle · 2 background · 1 working" && !head(x, "probe").bulk.shown,
        x,
      );
      await browser.send("Page.removeScriptToEvaluateOnNewDocument", { identifier: added.identifier });
      await browser.open(base + "/");
      await boardUntil("the real stream back", (y) => y.boxes.length === ACK_PANES.length && !y.stale && y.boxes.some((z) => z.kind === "finished"));
    }

    // One Box marked seen with its pill.
    const before = await viewOf("w1:p4");
    check("click on the Seen pill is accepted", await click("w1:p4", "button.ack"), null);
    b = await boardUntil("4: docs drawn Seen", (x) => box(x, "w1:p4")?.kind === "seen" && x.toast?.on);
    const docs = box(b, "w1:p4");
    const after = await viewOf("w1:p4");
    check("marked: focus stays on the Box whose pill was pressed, not the page", b.focus.box === "w1:p4" && b.focus.isBox, b.focus);
    check(
      "marked: the Box is box--seen, labelled SEEN, its accessible name says Seen, no pill",
      docs.cls === "SEEN" && /, Seen, /.test(docs.label) && !/Finished/.test(docs.label) && docs.acks === 0,
      docs,
    );
    check(
      "marked: the Daemon recorded it (seen, class finished, rank 2, the same waiting_since)",
      after.seen === true && after.attention_class === "finished" && after.sort_key[2] === 2 && after.waiting_since === before.waiting_since,
      { before, after },
    );
    check(
      "marked: the .box--seen rule draws its quiet 6px edge, not Finished's blue",
      docs.edge === b.tokens.quiet && docs.edge !== b.tokens.finished && docs.edgeWidth === "6px",
      { docs, tokens: b.tokens },
    );
    {
      const probe = b.boxes.filter((x) => x.group === "probe");
      const kinds = probe.map((x) => x.kind);
      const want = sortViews((await state()).agents)
        .filter((v) => v.workspace === "probe" && v.sort_key[0] !== 0)
        .map((v) => keyId(v.key));
      check(
        "marked: the Box moved after the group's Finished Boxes, among its Idle, in the Daemon's order",
        JSON.stringify(probe.map((x) => x.key)) === JSON.stringify(want) &&
          kinds.lastIndexOf("finished") < kinds.indexOf("seen") &&
          kinds.indexOf("seen") < kinds.indexOf("working") &&
          Math.abs(kinds.indexOf("seen") - kinds.indexOf("idle")) === 1,
        { kinds, want, got: probe.map((x) => x.key) },
      );
    }
    check(
      "marked: the heading drops one finished, shows 1 seen, and offers 2",
      head(b, "probe").counts === "2 finished · 1 idle · 1 seen · 1 working" && head(b, "probe").bulk.text === "✓ Mark 2 finished seen",
      b.heads,
    );
    check(
      "marked: the Undo bar, fixed, role status, says Marked 4: docs seen with an Undo",
      b.toast.on && b.toast.visible && b.toast.role === "status" && b.toast.position === "fixed" && b.toast.text === "Marked 4: docs seen" && b.toast.undo,
      b.toast,
    );

    // Undo returns it.
    const undone = await browser.eval(`(() => { const u = document.querySelector(".toast-undo"); if (!u || u.disabled) return false; u.focus(); u.click(); return true; })()`);
    b = await boardUntil("4: docs back to Finished", (x) => box(x, "w1:p4")?.kind === "finished");
    check(
      "Undo: the bar closes and the Box is Finished again, with its pill and the heading's 3",
      undone && !b.toast.on && box(b, "w1:p4").acks === 1 && head(b, "probe").counts === "3 finished · 1 idle · 1 working" &&
        (await viewOf("w1:p4")).seen === false,
      b,
    );
    check("Undo: focus moves from the closing bar to the Box it undid, not the page", b.focus.box === "w1:p4" && b.focus.isBox, b.focus);

    // Marked again: the bar goes by itself after 6 s; the Box stays seen.
    await click("w1:p4", "button.ack");
    await boardUntil("the Undo bar again", (x) => x.toast?.on && box(x, "w1:p4")?.kind === "seen");
    const shownAt = Date.now();
    b = await boardUntil("the Undo bar to go", (x) => !x.toast.on, ACK_TOAST_MS + 4000);
    const shown = Date.now() - shownAt;
    check("the Undo bar is gone after 6 s, the Box still seen", shown > ACK_TOAST_MS - 1500 && box(b, "w1:p4").kind === "seen", { shown, toast: b.toast });

    // The heading's bulk mark: exactly the two unseen Finished Boxes.
    await browser.eval(`(() => { const g = [...document.querySelectorAll("#board .group")].find((x) => x.querySelector(".group-label").textContent === "probe"); g?.querySelector("button.ack-all")?.focus(); })()`);
    check("bulk: the heading's button is accepted", await clickIn("probe", "button.ack-all"), null);
    b = await boardUntil("both marked by the heading", (x) => box(x, "w1:p3")?.kind === "seen" && box(x, "w1:p5")?.kind === "seen" && x.toast?.on);
    {
      const views = (await state()).agents;
      const seen = views.filter((v) => v.seen).map((v) => v.key.pane_id).sort();
      check(
        "bulk: marks exactly the group's Finished Agents, the bar says Marked 2 Agents seen",
        JSON.stringify(seen) === JSON.stringify(["w1:p3", "w1:p4", "w1:p5"]) && b.toast.text === "Marked 2 Agents seen" && b.toast.undo,
        { seen, toast: b.toast },
      );
    }
    check(
      "bulk: the heading now counts 3 seen and its button is hidden; solo is untouched",
      head(b, "probe").counts === "1 idle · 3 seen · 1 working" && !head(b, "probe").bulk.shown &&
        box(b, "w2:p1").kind === "finished" && box(b, "w2:p1").acks === 1,
      b.heads,
    );
    check("bulk: the hidden heading button hands focus to its group's first Box, not the page", b.focus.isBox && b.focus.group === "probe", b.focus);

    // A new turn ends the acknowledgement; the next finish is Finished.
    await push("w1:p4", "working");
    b = await boardUntil("4: docs working", (x) => box(x, "w1:p4")?.kind === "working");
    check("a seen Agent that goes working is Working, without a pill", box(b, "w1:p4").acks === 0, box(b, "w1:p4"));
    await push("w1:p4", "done");
    b = await boardUntil("4: docs finished again", (x) => box(x, "w1:p4")?.kind === "finished");
    check(
      "and when it finishes again it is a Finished Box with its pill",
      box(b, "w1:p4").acks === 1 && head(b, "probe").counts === "1 finished · 1 idle · 2 seen · 1 working",
      b,
    );

    // A page on another origin (another loopback port) cannot mark it.
    {
      const v = await viewOf("w1:p4");
      const target = base + "/api/agents/" + [v.key.host, v.key.server, String(v.key.epoch), v.key.pane_id].map(encodeURIComponent).join("/") + "/seen";
      const page = `<!doctype html><meta charset="utf-8"><title>other</title><script>
        fetch(${JSON.stringify(target)}, { method: "POST", headers: { "Content-Type": "application/json", "X-Herddash-Write": "1" },
          body: JSON.stringify({ waiting_since: ${v.waiting_since} }) })
          .then((r) => { window.result = { status: r.status }; }, (e) => { window.result = { error: String(e) }; });
      </script>`;
      const other = createHTTPServer((_, res) => res.writeHead(200, { "Content-Type": "text/html" }).end(page));
      await new Promise((r) => other.listen(0, "127.0.0.1", r));
      try {
        await browser.open("http://127.0.0.1:" + other.address().port + "/");
        const result = await until("the other origin's request", async () => browser.eval("window.result"), 10000);
        const after = await viewOf("w1:p4");
        check(
          "a page on another loopback origin cannot POST to the per-Agent route (no preflight answer, or 403)",
          (result.error !== undefined || result.status === 403) && after.seen === false && after.waiting_since === v.waiting_since,
          { result, after },
        );
      } finally {
        await new Promise((r) => other.close(r));
      }
      await browser.open(base + "/");
      b = await boardUntil("the Board after the other origin", (x) => x.boxes.length === ACK_PANES.length && !x.stale);
      check("and the Board still shows that Box Finished", box(b, "w1:p4").kind === "finished" && box(b, "w1:p4").acks === 1, box(b, "w1:p4"));
    }

    // While the Daemon is gone the Boxes dim and every control is disabled;
    // after serve restarts on the same data directory, Seen is still Seen.
    await d.stop();
    b = await boardUntil("the Boxes dimmed", (x) => x.stale, PAST_GRACE_MS + 5000);
    check(
      "dimmed: every Seen pill and bulk mark disabled",
      b.boxes.filter((x) => x.acks).every((x) => x.ackDisabled === true) && b.heads.every((h) => h.bulk.disabled),
      b,
    );
    d = serve("ack", sock, addr);
    b = await boardUntil("the Board back after a restart", (x) => !x.stale && x.boxes.length === ACK_PANES.length, 40000);
    check(
      "after serve restarts on the same data directory, the seen Boxes are still seen",
      ["w1:p3", "w1:p5"].every((p) => box(b, p).kind === "seen" && box(b, p).cls === "SEEN") &&
        box(b, "w1:p4").kind === "finished" && head(b, "probe").counts === "1 finished · 1 idle · 2 seen · 1 working" &&
        b.boxes.filter((x) => x.acks).every((x) => x.ackDisabled === false),
      b,
    );
  } finally {
    await d.stop();
    await herdr.stop();
  }
}

async function main() {
  const browser = await chrome();
  const snap = async () => JSON.parse(await browser.eval(SNAP));
  const kinds = (s) => s.strips.map((x) => x.kind.replace("strip strip--", ""));
  try {
    // 1. herdr 0.9.5: disconnect and return, then the Daemon stops and returns.
    const sock = join(root, "ok.sock");
    const herdr = fakeHerdr(sock, "0.9.5");
    await herdr.start();
    let d = serve("ok", sock, "127.0.0.1:0");
    const addr = await d.addr();
    await browser.open("http://" + addr + "/");
    let s = await until("a Box", async () => ((await snap()).boxes > 0 ? snap() : null));
    check("connected: no strip, Boxes not dimmed", s.strips.length === 0 && !s.stale, s);
    s = await until("the title bar's Needs you count", async () => {
      const x = await snap();
      return x.needs === "1 needs you" ? x : null;
    });
    check(
      "title bar: logo, Needs you count and herdr's version (ADR-0013)",
      s.logo === "herddash" && s.needsOn && s.link === "herdr 0.9.5" && s.linkKind.includes("summary-link--ok"),
      s,
    );

    const b = await until("both Boxes", async () => {
      const x = JSON.parse(await browser.eval(BOXES));
      return x.boxes.length === 2 ? x : null;
    });
    const [needs, work] = b.boxes;
    check("title carries the Needs you count", b.title === "(1) herddash · Needs you", b);
    check("headings: Needs you, then the workspace with its counts", b.headings.join("|") === "Needs you|probe1 working", b);
    check(
      "Needs you Box: top zone, tab, right badge, workspace second",
      needs.kind === "box box--needs_you" && !needs.inGroup && needs.tab === "1: build" && needs.pane === "right" &&
        needs.second === "workspace line" && needs.secondText === "probe",
      needs,
    );
    check(
      "Working Box: in its group, left badge, directory second",
      work.kind === "box box--working" && work.inGroup && work.pane === "left" && work.second === "dir line" && work.secondText === "/srv/proj",
      work,
    );
    check("four rows, one height, title equals the accessible name", b.boxes.every((x) => x.rows === 4 && x.titleIsLabel && x.height === needs.height), b);

    // The State label's task count (ADR-0009). The fake herdr has no Claude
    // process, so the Daemon reports none: the page is given the Daemon's own
    // state with counts set, through a stand-in EventSource that sends it
    // once as the state event.
    const state = JSON.parse(await (await fetch("http://" + addr + "/api/state")).text());
    const variants = [
      { name: "Working Box with 2 tasks shows · 2 bg after its class", tasks: 2, background: false, want: "working · 2 bg", kind: "box box--working" },
      { name: "Background Box with 3 tasks shows · 3 after its class", tasks: 3, background: true, want: "background · 3", kind: "box box--background" },
    ];
    for (const v of variants) {
      const sent = structuredClone(state);
      for (const a of sent.agents) {
        // Needs you never carries a count; it is set anyway to show so.
        a.background_tasks = v.tasks;
        if (a.status === "working") a.background = v.background;
      }
      const { result: added } = await browser.send("Page.addScriptToEvaluateOnNewDocument", {
        source: `window.EventSource = class extends EventTarget {
          static CLOSED = 2;
          readyState = 1;
          constructor() {
            super();
            window.standIn = (window.standIn || 0) + 1;
            setTimeout(() => this.dispatchEvent(new MessageEvent("state", { data: ${JSON.stringify(JSON.stringify(sent))}, lastEventId: "1" })), 0);
          }
          close() {}
        };`,
      });
      await browser.open("http://" + addr + "/");
      const labels = await until("the stand-in state", async () => {
        if (!(await browser.eval("window.standIn"))) return null;
        const x = JSON.parse(
          await browser.eval(`JSON.stringify([...document.querySelectorAll("#board .box")].map((b) => ({
            kind: b.className, state: b.querySelector(".class")?.textContent,
            count: b.querySelector(".class .count")?.textContent ?? null, label: b.getAttribute("aria-label") })))`),
        );
        return x.length === 2 && x.some((b) => b.kind === v.kind) ? x : null;
      }, 5000).catch(async (err) => {
        throw new Error(err.message + ": stand-in ran " + (await browser.eval("window.standIn")) + ", boxes " + (await browser.eval(BOXES)));
      });
      const box = labels.find((x) => x.kind === v.kind);
      const needsYou = labels.find((x) => x.kind === "box box--needs_you");
      check(
        v.name + ", and its accessible name says so",
        box.state.toLowerCase() === v.want && box.count !== null && new RegExp(v.tasks + " background tasks").test(box.label),
        labels,
      );
      check("Needs you Box shows no count (" + v.tasks + " sent)", !!needsYou && needsYou.count === null, labels);
      await browser.send("Page.removeScriptToEvaluateOnNewDocument", { identifier: added.identifier });
    }

    // Claude Code's recap (ADR-0010), through the same stand-in: the Working
    // Box gets a recap far longer than two lines, the Needs you Box none.
    const RECAP =
      "I refactored the store's retention sweep and every test passes. Next I'll open the pull request, " +
      "wait for CI, and then write the follow-up for the importer, which still reads the whole log at once.";
    {
      const sent = structuredClone(state);
      for (const a of sent.agents) a.recap = a.status === "working" ? RECAP : "";
      // The Needs you Box gets a Context Line, which it shows inside a <bdi>.
      for (const a of sent.agents) if (a.attention_class === "needs_you") a.context_line = "fix-auth-bug";
      const { result: added } = await browser.send("Page.addScriptToEvaluateOnNewDocument", {
        source: `window.EventSource = class extends EventTarget {
          static CLOSED = 2;
          readyState = 1;
          constructor() {
            super();
            window.standIn = (window.standIn || 0) + 1;
            setTimeout(() => this.dispatchEvent(new MessageEvent("state", { data: ${JSON.stringify(JSON.stringify(sent))}, lastEventId: "1" })), 0);
          }
          close() {}
        };`,
      });
      await browser.open("http://" + addr + "/");
      const x = await until("the stand-in state with a recap", async () => {
        if (!(await browser.eval("window.standIn"))) return null;
        const y = JSON.parse(
          await browser.eval(`JSON.stringify({
            recaps: document.getElementById("board").classList.contains("board--recaps"),
            boxes: [...document.querySelectorAll("#board .box")].map((b) => {
              const c = b.children[2];
              const cs = getComputedStyle(c);
              return {
                kind: b.className, height: b.getBoundingClientRect().height, label: b.getAttribute("aria-label"),
                titleIsLabel: b.title === b.getAttribute("aria-label"),
                ctxClass: c.className, ctxText: c.textContent,
                ctxBdi: c.children.length === 1 && c.firstElementChild.tagName === "BDI" && c.firstElementChild.textContent === c.textContent,
                ctxHeight: c.getBoundingClientRect().height,
                ctxScroll: c.scrollHeight, ctxClient: c.clientHeight, lineHeight: parseFloat(cs.lineHeight),
                clamp: cs.webkitLineClamp || cs.getPropertyValue("-webkit-line-clamp"), ellipsis: cs.textOverflow,
              };
            }),
          })`),
        );
        return y.boxes.length === 2 && y.recaps ? y : null;
      }, 5000);
      const withRecap = x.boxes.find((b) => b.kind === "box box--working");
      const other = x.boxes.find((b) => b.kind === "box box--needs_you");
      const line = withRecap.lineHeight;
      check(
        "recap Box: the recap in the context row, class recap, clamped to two lines",
        withRecap.ctxClass === "context recap" && withRecap.ctxText === RECAP && String(withRecap.clamp) === "2" &&
          Math.abs(withRecap.ctxHeight - 2 * line) < 1 && withRecap.ctxScroll > withRecap.ctxClient,
        withRecap,
      );
      check(
        "recap Box: accessible name and tooltip carry the full recap, not the Context Line",
        withRecap.label.includes(RECAP) && withRecap.titleIsLabel,
        withRecap,
      );
      check("Box without a recap keeps its Context Line row", other.ctxClass === "context line", other);
      check(
        "the context row's text, recap or Context Line, sits in a <bdi>",
        withRecap.ctxBdi && other.ctxBdi && other.ctxText === "fix-auth-bug",
        x.boxes,
      );
      check(
        "while a Box has a recap, every Box is one line taller",
        x.boxes.every((b) => Math.abs(b.height - (needs.height + line)) < 1),
        { base: needs.height, line, boxes: x.boxes.map((b) => b.height) },
      );
      await browser.send("Page.removeScriptToEvaluateOnNewDocument", { identifier: added.identifier });
    }

    // A skipped publication (Story 5.11), through the same stand-in: a valid
    // state with id 1, then an agent event with id 3. board.mjs must pass each
    // event's id to streamDecision, which warns and opens a new /events
    // EventSource.
    {
      const agent = state.agents[0];
      const warnedBefore = browser.warnings.length;
      const { result: added } = await browser.send("Page.addScriptToEvaluateOnNewDocument", {
        source: `window.standInUrls = [];
        window.EventSource = class extends EventTarget {
          static CLOSED = 2;
          readyState = 1;
          constructor(url) {
            super();
            window.standIn = (window.standIn || 0) + 1;
            window.standInUrls.push(String(url));
            setTimeout(() => {
              this.dispatchEvent(new MessageEvent("state", { data: ${JSON.stringify(JSON.stringify(state))}, lastEventId: "1" }));
              this.dispatchEvent(new MessageEvent("agent", { data: ${JSON.stringify(JSON.stringify(agent))}, lastEventId: "3" }));
            }, 0);
          }
          close() { this.readyState = 2; }
        };`,
      });
      await browser.open("http://" + addr + "/");
      const urls = await until(
        "a new /events EventSource after the skipped publication",
        async () => {
          const u = await browser.eval("window.standInUrls");
          return Array.isArray(u) && u.length >= 2 ? u : null;
        },
        10000,
      );
      const warned = browser.warnings.slice(warnedBefore);
      check(
        "skipped publication (ids 1 then 3): warns and opens a new /events EventSource",
        warned.some((w) => SKIPPED_WARNING.test(w)) && urls.every((u) => new URL(u, "http://" + addr).pathname === "/events"),
        { warned, urls },
      );
      await browser.send("Page.removeScriptToEvaluateOnNewDocument", { identifier: added.identifier });
    }

    await browser.open("http://" + addr + "/");
    await until("the real stream back", async () => ((await snap()).boxes === 2 ? true : null));
    {
      const y = JSON.parse(await browser.eval(BOXES));
      const recaps = await browser.eval(`document.getElementById("board").classList.contains("board--recaps")`);
      check(
        "no recap: no board--recaps, every Box its usual height",
        !recaps && y.boxes.every((b) => b.height === needs.height && b.rows === 4),
        { recaps, y },
      );
    }

    await herdr.stop();
    s = await until("the disconnected strip", async () => ((await snap()).strips.length ? snap() : null));
    check(
      "herdr stopped: disconnected strip and board--stale",
      kinds(s).join() === "disconnected" && s.stale && s.boxes > 0 && s.strips[0].text.startsWith("Disconnected from herdr. Last snapshot"),
      s,
    );
    check("herdr stopped: the title bar says so", s.link === "herdr disconnected" && s.linkKind.includes("summary-link--down"), s);
    await herdr.start();
    s = await until("the strip to clear", async () => {
      const x = await snap();
      return !x.strips.length && !x.stale ? x : null;
    });
    check("herdr back: strip and dimming cleared", s.boxes > 0, s);

    // A hung Daemon: the stream stays open but nothing arrives, not even a
    // keepalive. Paused, so a failure here must not leave it stopped.
    const boxesBeforeHang = (await snap()).boxes;
    d.signal("SIGSTOP");
    try {
      const hungAt = Date.now();
      s = await until(
        "the lost strip for a hung Daemon",
        async () => {
          const x = await snap();
          return x.strips.length && x.stale ? x : null;
        },
        STREAM_SILENCE_MS + 10000,
      );
      check(
        "serve hung (SIGSTOP), past STREAM_SILENCE_MS: lost strip, Boxes dimmed",
        kinds(s).join() === "disconnected" && s.stale && s.boxes > 0 && s.strips[0].text === "Lost contact with herddash. Reconnecting…" &&
          Date.now() - hungAt >= STREAM_SILENCE_MS - 15000,
        s,
      );
      check("serve hung: the title bar says it is reconnecting", s.link === "Reconnecting…" && s.linkKind.includes("summary-link--lost"), s);
    } finally {
      d.signal("SIGCONT");
    }
    s = await until(
      "the lost strip to clear after SIGCONT",
      async () => {
        const x = await snap();
        return !x.strips.length && !x.stale && x.boxes > 0 ? x : null;
      },
      40000,
    );
    check(
      "serve resumed (SIGCONT): lost strip and dimming cleared, the same Boxes back",
      s.strips.length === 0 && !s.stale && boxesBeforeHang > 0 && s.boxes === boxesBeforeHang,
      { before: boxesBeforeHang, s },
    );

    await d.stop();
    await sleep(PAST_GRACE_MS);
    s = await snap();
    check(
      "serve stopped, past the grace period: lost strip, Boxes dimmed",
      kinds(s).join() === "disconnected" && s.stale && s.strips[0].text === "Lost contact with herddash. Reconnecting…",
      s,
    );
    d = serve("ok", sock, addr);
    s = await until(
      "the lost strip to clear",
      async () => {
        const x = await snap();
        return !x.strips.length && !x.stale && x.boxes > 0 ? x : null;
      },
      40000,
    );
    check("serve back: lost strip cleared", true, s);
    await d.stop();
    await herdr.stop();

    // 2. herdr 0.8.2 without the flag: refused.
    const old = join(root, "old.sock");
    const herdrOld = fakeHerdr(old, "0.8.2");
    await herdrOld.start();
    d = serve("refused", old, "127.0.0.1:0");
    await browser.open("http://" + (await d.addr()) + "/");
    s = await until("the refused strip", async () => (kinds(await snap()).includes("refused") ? snap() : null));
    check("refused: red strip naming both versions, no Boxes", s.boxes === 0 && /0\.8\.2.*0\.9\.5/.test(s.strips.at(-1).text), s);
    await d.stop();

    // 3. herdr 0.8.2 with the dogfooding flag: advisory.
    d = serve("advisory", old, "127.0.0.1:0", ["--allow-unsupported-herdr"]);
    await browser.open("http://" + (await d.addr()) + "/");
    s = await until("the advisory strip with a Box", async () => {
      const x = await snap();
      return kinds(x).includes("advisory") && x.boxes > 0 ? x : null;
    });
    check("advisory: amber strip, Boxes shown", kinds(s).join() === "advisory" && !s.stale, s);
    await d.stop();
    await herdrOld.stop();

    // 4. The acknowledgement controls (ADR-0014).
    await ackChecks(browser);

    // The skipped-publication case's own warning is expected; nothing else is.
    const unexpected = browser.warnings.filter((w) => !SKIPPED_WARNING.test(w));
    check("no console warnings or exceptions", unexpected.length === 0, browser.warnings);
  } finally {
    await browser.close();
    // Chrome's helper processes can outlive the main one and still write to
    // the profile, so removal can race them. A leftover temporary directory
    // is not what this check is about: warn, do not fail.
    try {
      rmSync(root, { recursive: true, force: true, maxRetries: 20, retryDelay: 250 });
    } catch (e) {
      console.warn(`warn   could not remove ${root}: ${e.code || e.message}`);
    }
  }
}

main().then(
  () => process.exit(failures ? 1 : 0),
  (err) => {
    console.error(err);
    process.exit(1);
  },
);
