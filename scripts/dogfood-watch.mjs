// Hand-run dogfood watcher for an installed herddash. Not embedded, not run
// in CI, installs nothing.
//
//   node scripts/dogfood-watch.mjs [--interval 2] [--grace 6] [--summary 1500]
//   node scripts/dogfood-watch.mjs --dump     # one table of Board vs files, then exit
//
// Every interval it reads the running Daemon's /api/state and keeps its own
// model from /events, and works out independently what each Claude Code
// Box should show, from the same files the Daemon reads:
//
//   - the Agent's process, by herdr's read-only pane.process_info (the call
//     the Daemon makes to resolve Profiles; never focus, never pane output);
//   - <config dir>/sessions/<pid>.json (status, sessionId, cwd; never the
//     .key files beside it);
//   - the session transcript, tailed with its own decoder (task starts and
//     ends, turn boundaries, the recap), and the process table's live
//     shell-snapshots children.
//
// It prints one line per anomaly that has held for --grace seconds, once
// until it clears ("ok" when it does), a summary every --summary seconds and
// a WATCHER line on its own failure; nothing else. It never prints transcript
// text: a recap is reported by length and its first words only.
//
// Node 22 or newer, built-ins only. The Board's own event model (order.mjs)
// rebuilds /events, so the stream check uses exactly the Board's logic.

import { execFileSync } from "node:child_process";
import { closeSync, existsSync, fstatSync, openSync, readFileSync, readSync, statSync } from "node:fs";
import { createConnection } from "node:net";
import { homedir } from "node:os";
import { join } from "node:path";
import { applyEvent, emptyModel, keyId } from "../internal/httpapi/web/order.mjs";

const arg = (name, dflt) => {
  const i = process.argv.indexOf("--" + name);
  return i > 0 ? Number(process.argv[i + 1]) : dflt;
};
const INTERVAL = arg("interval", 2) * 1000;
const GRACE = arg("grace", 6) * 1000;
const SUMMARY = arg("summary", 1500) * 1000;
const FLICKER_MS = 10000;
const PID_TTL = 60000;
const DUMP = process.argv.includes("--dump");
const BOUND = 8 << 20;

const out = (s) => console.log(new Date().toTimeString().slice(0, 8) + " " + s);
const home = homedir();

// ---- herddash's configuration, as the running binary reports it ----

const cfg = JSON.parse(execFileSync(join(home, ".local/bin/herddash"), ["config"], { encoding: "utf8" }));
const base = "http://" + cfg.bind;
const recapsOn = !!cfg.claude_recaps;
const configDirs = [join(home, ".claude")];
for (const dir of Object.keys(cfg.profiles?.claude?.values || {})) configDirs.push(dir);

// ---- herdr: one read-only request per connection ----

function herdr(method, params) {
  return new Promise((resolve, reject) => {
    const c = createConnection(cfg.socket_path);
    let buf = "";
    const t = setTimeout(() => (c.destroy(), reject(new Error(method + " timed out"))), 3000);
    c.on("connect", () => c.write(JSON.stringify({ id: "dw1", method, params }) + "\n"));
    c.on("data", (d) => {
      buf += d;
      const nl = buf.indexOf("\n");
      if (nl < 0) return;
      clearTimeout(t);
      c.destroy();
      const r = JSON.parse(buf.slice(0, nl));
      r.error ? reject(new Error(r.error.code)) : resolve(r.result);
    });
    c.on("error", (e) => (clearTimeout(t), reject(e)));
  });
}

const pids = new Map(); // pane_id -> { pid, at }
async function pidOf(pane) {
  const c = pids.get(pane);
  if (c && Date.now() - c.at < PID_TTL) return c.pid;
  let pid = 0;
  try {
    const pi = (await herdr("pane.process_info", { pane_id: pane })).process_info;
    const fg = pi.foreground_processes || [];
    pid = (fg.find((p) => p.pid === pi.foreground_process_group_id) || fg[0] || {}).pid || 0;
  } catch {}
  pids.set(pane, { pid, at: Date.now() });
  return pid;
}

// ---- Claude Code's session record and transcript ----

function sessionOf(pid) {
  for (const dir of configDirs) {
    const f = join(dir, "sessions", pid + ".json");
    try {
      return { dir, ...JSON.parse(readFileSync(f, "utf8")) };
    } catch {}
  }
  return null;
}

function transcriptPath(dir, cwd, id) {
  const broad = join(dir, "projects", cwd.replace(/[^A-Za-z0-9]/g, "-"), id + ".jsonl");
  const narrow = join(dir, "projects", cwd.replace(/[/.]/g, "-"), id + ".jsonl");
  return existsSync(broad) || !existsSync(narrow) ? broad : narrow;
}

const CLOSING = new Set(["completed", "failed", "killed"]);
const tails = new Map(); // path -> { ino, off, rest, open: Map(id -> command), turnEnded, recap }

function fold(t, r) {
  if (!r || typeof r !== "object") return;
  const tur = r.toolUseResult && typeof r.toolUseResult === "object" ? r.toolUseResult : null;
  const main = !r.isSidechain;
  if (r.type === "user") {
    if (main) (t.turnEnded = false), (t.recap = ""), (t.boundaryAt = Date.now());
    if (tur && typeof tur.backgroundTaskId === "string" && tur.backgroundTaskId) t.open.set(tur.backgroundTaskId, true);
    if (tur && tur.isAsync === true && tur.status === "async_launched" && tur.agentId) t.open.set(tur.agentId, false);
    if (tur && typeof tur.task_type === "string" && tur.task_id) t.open.delete(String(tur.task_id));
  } else if (r.type === "assistant" && main) {
    const s = r.message && r.message.stop_reason;
    if (s === "end_turn" || s === "stop_sequence") (t.turnEnded = true), (t.boundaryAt = Date.now());
  } else if (r.type === "attachment" && main && r.attachment && r.attachment.type === "queued_command") {
    t.turnEnded = false;
    t.recap = "";
    t.boundaryAt = Date.now();
  } else if (r.type === "queue-operation" && r.operation === "enqueue" && typeof r.content === "string" && r.content.startsWith("<task-notification>")) {
    const id = (r.content.match(/<task-id>\s*([^<]*?)\s*<\/task-id>/) || [])[1];
    const st = (r.content.match(/<status>\s*([^<]*?)\s*<\/status>/) || [])[1];
    if (id && CLOSING.has(st)) t.open.delete(id);
  } else if (r.type === "system" && main && r.subtype === "away_summary" && typeof r.content === "string") {
    const s = r.content.trim().replace(/\(disable recaps in \/config\)$/, "").trim();
    if (s) t.recap = [...s].slice(0, 400).join("").trim();
  }
}

function readTranscript(path) {
  let fd;
  try {
    fd = openSync(path, "r");
  } catch {
    return null;
  }
  try {
    const st = fstatSync(fd);
    let t = tails.get(path);
    if (!t || t.ino !== st.ino || st.size < t.off) {
      t = { ino: st.ino, off: Math.max(0, st.size - BOUND), rest: "", open: new Map(), turnEnded: false, recap: "", first: true, boundaryAt: 0 };
      tails.set(path, t);
    }
    while (t.off < st.size) {
      const n = Math.min(st.size - t.off, 4 << 20);
      const b = Buffer.alloc(n);
      readSync(fd, b, 0, n, t.off);
      t.off += n;
      let text = t.rest + b.toString("utf8");
      if (t.first) {
        text = text.slice(text.indexOf("\n") + 1);
        t.first = false;
      }
      const lines = text.split("\n");
      t.rest = lines.pop();
      for (const l of lines) {
        try {
          fold(t, JSON.parse(l));
        } catch {}
      }
    }
    return t;
  } finally {
    closeSync(fd);
  }
}

function shellChildren() {
  const live = new Map(); // ppid -> count
  const ps = execFileSync("ps", ["-axo", "pid=,ppid=,command="], { encoding: "utf8", maxBuffer: 64 << 20 });
  for (const line of ps.split("\n")) {
    const m = line.match(/^\s*(\d+)\s+(\d+)\s+(.*)$/);
    if (m && m[3].includes("shell-snapshots")) live.set(Number(m[2]), (live.get(Number(m[2])) || 0) + 1);
  }
  return live;
}

// ---- what a Box should show, from the files ----

function expected(view, s, t, shells) {
  const st = view.status;
  const open = t ? t.open.size : 0;
  const openCommand = t ? [...t.open.values()].some(Boolean) : false;
  const shell = (shells.get(s.pid) || 0) > 0;
  let count = open;
  if ((st === "done" || st === "idle") && shell && !openCommand) count += 1;
  let background = false;
  if (view.attention_class !== "needs_you" && view.attention_class !== "unknown") {
    if ((st === "done" || st === "idle") && count > 0) background = true;
    if (st === "working" && t && t.turnEnded && open > 0) background = true;
  }
  const shown = background || view.attention_class === "finished" || (view.attention_class === "working" && st !== "working");
  const recap = recapsOn && shown && t ? t.recap : "";
  return { count, background, recap, waiting: s.status === "waiting" };
}

// ---- anomalies: reported once they have held for GRACE, until they clear ----

const pending = new Map(); // key -> { since, text, reported }
let seenThisTick = new Set();
function flag(key, text, now, immediate = false) {
  seenThisTick.add(key);
  const p = pending.get(key) || { since: now, reported: false };
  p.text = text;
  if (!p.reported && (immediate || now - p.since >= GRACE)) {
    out("ANOMALY " + text);
    p.reported = true;
  }
  pending.set(key, p);
}
function endTick() {
  for (const [k, p] of pending) {
    if (seenThisTick.has(k)) continue;
    if (p.reported) out("ok      cleared: " + p.text);
    pending.delete(k);
  }
  seenThisTick = new Set();
}

const words = (s) => (s ? JSON.stringify(s.split(/\s+/).slice(0, 5).join(" ") + "…") + " (" + s.length + ")" : '""');
const name = (v) => v.workspace + " › " + v.tab + (v.pane_position ? " (" + v.pane_position + ")" : "");

// ---- the event stream, rebuilt with the Board's own model ----

let stream = emptyModel();
let streamUp = false;
async function followEvents() {
  for (;;) {
    try {
      const r = await fetch(base + "/events");
      if (r.status !== 200) throw new Error("status " + r.status);
      streamUp = true;
      const dec = new TextDecoder();
      let buf = "";
      for await (const chunk of r.body) {
        buf += dec.decode(chunk, { stream: true });
        let i;
        while ((i = buf.indexOf("\n\n")) >= 0) {
          const frame = buf.slice(0, i);
          buf = buf.slice(i + 2);
          let ev = "message";
          const data = [];
          for (const l of frame.split("\n")) {
            if (l.startsWith("event: ")) ev = l.slice(7);
            else if (l.startsWith("data: ")) data.push(l.slice(6));
          }
          if (data.length) stream = applyEvent(stream, ev, data.join("\n")).model;
        }
      }
    } catch {}
    streamUp = false;
    await new Promise((r) => setTimeout(r, 2000));
  }
}

// ---- the loop ----

const history = new Map(); // keyId -> [{ kind, at }]
let runId = null;
let lastLogSize = 0;
try {
  lastLogSize = statSync(join(cfg.data_dir, "herddash.log")).size;
} catch {}
let ticks = 0;
let lastSummary = Date.now();
const counts = { agents: 0, claude: 0, matched: 0, recaps: 0 };

function logLines() {
  const f = join(cfg.data_dir, "herddash.log");
  let size;
  try {
    size = statSync(f).size;
  } catch {
    return [];
  }
  if (size < lastLogSize) lastLogSize = 0;
  if (size === lastLogSize) return [];
  const fd = openSync(f, "r");
  const b = Buffer.alloc(size - lastLogSize);
  readSync(fd, b, 0, b.length, lastLogSize);
  closeSync(fd);
  lastLogSize = size;
  return b.toString("utf8").split("\n").filter(Boolean);
}

async function tick() {
  const now = Date.now();
  let state;
  try {
    state = await (await fetch(base + "/api/state")).json();
  } catch (e) {
    flag("api", "health: /api/state not reachable (" + e.message + ")", now, true);
    return;
  }
  const c = state.connection || {};
  if (runId && state.run_id !== runId) out("NOTE    Daemon restarted (run_id changed)");
  runId = state.run_id;
  if (!c.connected) flag("conn", "health: Daemon reports herdr disconnected", now);
  if (c.gate && c.gate !== "ok") flag("gate", "health: gate " + c.gate, now);
  for (const l of logLines()) {
    try {
      const e = JSON.parse(l);
      if (e.level === "WARN" || e.level === "ERROR") out("LOG     " + e.level + " " + e.msg + (e.pane ? " pane=" + e.pane : "") + (e.error ? " error=" + e.error : ""));
    } catch {}
  }

  const shells = shellChildren();
  counts.agents = state.agents.length;
  counts.claude = counts.matched = counts.recaps = 0;
  const seen = new Set();
  for (const v of state.agents) {
    const id = keyId(v.key);
    seen.add(id);
    const kind = v.background ? "background" : v.attention_class === "working" && v.status !== "working" ? "idle" : v.attention_class;
    const h = (history.get(id) || []).filter((x) => now - x.at < FLICKER_MS);
    if (!h.length || h[h.length - 1].kind !== kind) h.push({ kind, at: now });
    history.set(id, h);
    // A flip back and forth is flicker unless the Agent's transcript shows a
    // turn starting or ending in the window: a short real turn is not one.
    const flickered = h.length >= 3 && h[h.length - 1].kind === h[h.length - 3].kind;
    const flicker = () =>
      flag(id + "/flicker", name(v) + ": flicker " + h.map((x) => x.kind).join("→") + " within " + FLICKER_MS / 1000 + " s", now, true);

    // The stream's model must agree with the state once both have settled.
    const sv = stream.agents.get(id);
    if (streamUp && (!sv || sv.attention_class !== v.attention_class || sv.status !== v.status || sv.background !== v.background || (sv.recap || "") !== (v.recap || ""))) {
      flag(id + "/stream", name(v) + ": /events model disagrees with /api/state (" + (sv ? sv.attention_class + "/" + sv.status : "missing") + " vs " + v.attention_class + "/" + v.status + ")", now);
    }

    if (v.agent_program !== "claude") {
      if (flickered) flicker();
      continue;
    }
    counts.claude++;
    const pid = await pidOf(v.key.pane_id);
    const s = pid ? sessionOf(pid) : null;
    if (!s || !s.sessionId || !s.cwd) {
      if (flickered) flicker();
      continue;
    }
    counts.matched++;
    const t = readTranscript(transcriptPath(s.dir, s.cwd, s.sessionId));
    if (flickered && !(t && t.boundaryAt && now - t.boundaryAt < FLICKER_MS)) flicker();
    const e = expected(v, { pid, status: s.status }, t, shells);
    if (e.recap) counts.recaps++;
    if (DUMP)
      out(`DUMP    ${name(v).padEnd(38)} ${(v.attention_class + "/" + v.status).padEnd(17)} session ${String(s.status).padEnd(7)} bg ${v.background_tasks}/${e.count} ${v.background ? "B" : "-"}/${e.background ? "B" : "-"} recap ${(v.recap || "").length}/${e.recap.length}`);
    if (e.waiting && v.attention_class !== "needs_you") flag(id + "/needs", name(v) + ": session record says waiting, Board says " + v.attention_class + "/" + v.status, now);
    if (s.status !== "waiting" && v.attention_class === "needs_you" && v.status !== "blocked")
      flag(id + "/needs2", name(v) + ": Board says Needs you, session record says " + s.status, now);
    if (e.count !== v.background_tasks) flag(id + "/count", name(v) + ": background_tasks " + v.background_tasks + ", files say " + e.count + " (open " + (t ? t.open.size : "-") + ", turn ended " + (t ? t.turnEnded : "-") + ")", now);
    if (e.background !== !!v.background) flag(id + "/bg", name(v) + ": background " + v.background + ", files say " + e.background + " (status " + v.status + ")", now);
    if ((v.recap || "") !== e.recap) flag(id + "/recap", name(v) + ": recap " + words(v.recap) + ", files say " + words(e.recap), now);
  }
  for (const id of history.keys()) if (!seen.has(id)) history.delete(id);
  for (const [id] of stream.agents) if (streamUp && !seen.has(id)) flag(id + "/ghost", "stream model holds an Agent /api/state does not: " + id, now);

  ticks++;
  if (now - lastSummary >= SUMMARY) {
    out(`SUMMARY ${ticks} ticks; ${counts.agents} Agents, ${counts.claude} claude, ${counts.matched} matched to a session, ${counts.recaps} with a recap; ${[...pending.values()].filter((p) => p.reported).length} open anomalies`);
    lastSummary = now;
  }
}

process.on("uncaughtException", (e) => (out("WATCHER error " + e.message), process.exit(1)));
process.on("unhandledRejection", (e) => (out("WATCHER error " + (e && e.message)), process.exit(1)));
out(`WATCHER started: ${base}, recaps ${recapsOn ? "on" : "off"}, interval ${INTERVAL / 1000} s, grace ${GRACE / 1000} s`);
if (DUMP) {
  await tick();
  process.exit(0);
}
followEvents();
for (;;) {
  await tick();
  endTick();
  await new Promise((r) => setTimeout(r, INTERVAL));
}
