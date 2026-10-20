// Hand-run headless-Chrome check of the Board's connection and version strips
// (Story 3.9). Not embedded, not run in CI.
//
//   go build -o /tmp/herddash ./cmd/herddash
//   node internal/httpapi/web/check/board-browser.mjs /tmp/herddash
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
import { tmpdir } from "node:os";
import { join } from "node:path";

const binary = process.argv[2];
if (!binary) {
  console.error("usage: node board-browser.mjs <herddash binary>");
  process.exit(2);
}
const CHROME = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
// More than order.mjs's LOST_GRACE_MS (3 s) plus the one-second tick.
const PAST_GRACE_MS = 4500;

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
    tabs: [{ tab_id: "w1:t1", workspace_id: "w1", number: 1, label: "1", focused: true, pane_count: 1, agent_status: "working" }],
    panes: [{ pane_id: "w1:p1", terminal_id: "t1", workspace_id: "w1", tab_id: "w1:t1", focused: true, agent: "claude", agent_status: "working", revision: 0 }],
    layouts: [],
    agents: [{ terminal_id: "t1", agent: "claude", agent_status: "working", workspace_id: "w1", tab_id: "w1:t1", pane_id: "w1:p1", focused: true, state_change_seq: 3, revision: 0 }],
  },
};

function fakeHerdr(path, version) {
  let server = null;
  const conns = new Set();
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
          reply(SNAPSHOT);
          break;
        case "pane.process_info":
          reply({ type: "pane_process_info", process_info: { pane_id: "w1:p1", shell_pid: 1, foreground_processes: [] } });
          break;
        case "events.subscribe":
          reply({ type: "subscription_started" }); // held open until the client closes it
          break;
        default:
          conn.end();
      }
    });
  };
  return {
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
  return { addr, stop: () => (p.kill("SIGINT"), exited) };
}

// ---- Chrome over the DevTools protocol ----

async function chrome() {
  const prof = join(root, "chrome");
  const c = spawn(CHROME, ["--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + prof, "--no-first-run", "about:blank"], {
    stdio: ["ignore", "ignore", "pipe"],
  });
  let err = "";
  c.stderr.on("data", (d) => (err += d));
  const browserWs = await until("DevTools endpoint", () => (err.match(/ws:\/\/\S+/) || [])[0]);
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
  return {
    warnings,
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
})`;

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

    await herdr.stop();
    s = await until("the disconnected strip", async () => ((await snap()).strips.length ? snap() : null));
    check(
      "herdr stopped: disconnected strip and board--stale",
      kinds(s).join() === "disconnected" && s.stale && s.boxes > 0 && s.strips[0].text.startsWith("Disconnected from herdr. Last snapshot"),
      s,
    );
    await herdr.start();
    s = await until("the strip to clear", async () => {
      const x = await snap();
      return !x.strips.length && !x.stale ? x : null;
    });
    check("herdr back: strip and dimming cleared", s.boxes > 0, s);

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

    check("no console warnings or exceptions", browser.warnings.length === 0, browser.warnings);
  } finally {
    await browser.close();
    rmSync(root, { recursive: true, force: true, maxRetries: 5 });
  }
}

main().then(
  () => process.exit(failures ? 1 : 0),
  (err) => {
    console.error(err);
    process.exit(1);
  },
);
