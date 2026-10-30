// Tests for order.mjs, run with `node --test internal/httpapi/web/*.test.mjs`
// and no npm packages. The golden order and headers come from core's fixture, which the
// Go tests in internal/core check against core.SortViews and
// core.WorkspaceHeaders, so the Board and the Daemon cannot drift apart.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import {
  EVENT_NAMES,
  HANDLED_EVENTS,
  RECONNECT_CAP_MS,
  RECONNECT_START_MS,
  LOST_GRACE_MS,
  LOST_TEXT,
  RECORDING_OFF_TEXT,
  STREAM_SILENCE_MS,
  applyEvent,
  boardStatus,
  titleBar,
  boxLabel,
  anyRecap,
  contextText,
  recapOf,
  clockUTC,
  compareViews,
  emptyModel,
  formatAge,
  formatDuration,
  headerLabel,
  keyId,
  layout,
  modelViews,
  isIdle,
  isBackground,
  BACKGROUND_LABEL,
  boxKind,
  classLabel,
  backgroundTasks,
  stateCount,
  stateLabel,
  tasksLabel,
  documentTitle,
  groupCounts,
  unseenFinished,
  ackBody,
  ackPath,
  ackToast,
  ackError,
  ACK_STALE,
  ACK_OFF,
  ACK_FAILED,
  UNDO_STALE,
  UNDO_OFF,
  UNDO_FAILED,
  UNSEEN_FAILED,
  REDO_STALE,
  REDO_OFF,
  REDO_FAILED,
  IN_VIEW_DOT,
  IN_VIEW_TEXT,
  IN_VIEW_WORDS,
  IN_VIEW_TITLE,
  inViewTag,
  ackLabel,
  unackLabel,
  ACK_TOAST_MS,
  SEEN_LABEL,
  isSeen,
  inNeedsYou,
  paneLabel,
  tabLabel,
  nextReconnectDelay,
  sortViews,
  streamDecision,
  streamLost,
  streamState,
  stripsKey,
  waitingLabel,
  whereLabel,
  workspaceHeaders,
} from "./order.mjs";

const fixture = JSON.parse(
  readFileSync(new URL("../../core/testdata/board-order.json", import.meta.url), "utf8"),
);

const UNKNOWN = Number.MAX_SAFE_INTEGER;

function keyString(v) {
  return v.key.host + "/" + v.key.server + "/" + v.key.epoch + "/" + v.key.pane_id;
}

// view builds a minimal AgentView with the given sort key and AgentKey.
function view(sortKey, host, server, epoch, pane) {
  const key = { host, server, epoch, pane_id: pane };
  return { key, sort_key: [...sortKey, host + "/" + server + "/" + epoch + "/" + pane] };
}

test("golden fixture: order equals the fixture's", () => {
  const views = fixture.input.map((i) => i.view);
  assert.ok(views.length > 0);
  const sorted = sortViews(views);
  assert.deepEqual(sorted.map(keyString), fixture.order);
  // sort_key[4] is the key string the Daemon sends; it must agree.
  assert.deepEqual(sorted.map((v) => v.sort_key[4]), fixture.order);
});

test("golden fixture: a seen Agent sorts between its group's Finished and older Idle Agents", () => {
  // ADR-0014, Story 6.1: the Daemon ranks a seen, non-Background Agent 2,
  // Idle's, most recent first; the Board orders by the sort key as sent.
  const sorted = sortViews(fixture.input.map((i) => i.view));
  const docs = sorted.filter((v) => v.workspace === "docs");
  assert.deepEqual(docs.map((v) => v.key.pane_id), ["w4:p1", "w4:p2", "w4:p3"]);
  assert.deepEqual(docs.map((v) => v.seen), [false, true, false]);
  assert.equal(docs[1].attention_class, "finished");
  assert.equal(docs[1].sort_key[2], 2);
});

test("golden fixture: headers equal the fixture's", () => {
  const sorted = sortViews(fixture.input.map((i) => i.view));
  assert.deepEqual(workspaceHeaders(sorted), fixture.headers);
});

test("golden fixture: order does not depend on input order", () => {
  const views = fixture.input.map((i) => i.view);
  assert.deepEqual(sortViews(views.slice().reverse()).map(keyString), fixture.order);
});

test("sortViews does not modify its input", () => {
  const views = fixture.input.map((i) => i.view);
  const before = views.map(keyString);
  sortViews(views);
  assert.deepEqual(views.map(keyString), before);
});

test("epoch is compared as a number: 9 before 10", () => {
  const a = view([1, 2, 2, 100], "mac", "default", 10, "w1:p1");
  const b = view([1, 2, 2, 100], "mac", "default", 9, "w1:p1");
  assert.deepEqual(sortViews([a, b]).map((v) => v.key.epoch), [9, 10]);
  assert.ok(compareViews(b, a) < 0);
});

test("a host that is a prefix of another sorts first", () => {
  const a = view([0, 0, 0, 5], "mac-mini", "default", 1, "w1:p1");
  const b = view([0, 0, 0, 5], "mac", "default", 1, "w1:p1");
  assert.deepEqual(sortViews([a, b]).map((v) => v.key.host), ["mac", "mac-mini"]);
});

test("server and pane id break later ties", () => {
  const a = view([0, 0, 1, 5], "h", "s2", 1, "w1:p1");
  const b = view([0, 0, 1, 5], "h", "s1", 1, "w1:p2");
  const c = view([0, 0, 1, 5], "h", "s1", 1, "w1:p1");
  assert.deepEqual(sortViews([a, b, c]).map(keyString), ["h/s1/1/w1:p1", "h/s1/1/w1:p2", "h/s2/1/w1:p1"]);
});

test("strings compare by code point, as Go does", () => {
  // U+FF5E is below U+1F600 by code point, but above its UTF-16 lead surrogate.
  const a = view([0, 0, 0, 5], "h\u{1F600}", "s", 1, "p");
  const b = view([0, 0, 0, 5], "h～", "s", 1, "p");
  assert.deepEqual(sortViews([a, b]).map((v) => v.key.host), ["h～", "h\u{1F600}"]);
});

test("sort_key elements 0-3 compare numerically, the unknown sentinel last", () => {
  const known = view([1, 3, 2, 1000], "h", "s", 1, "b");
  const unknownStart = view([1, 3, 2, UNKNOWN], "h", "s", 1, "a");
  const unknownGroup = view([1, UNKNOWN, 2, 1], "h", "s", 1, "a");
  const top = view([0, 0, 1, 20], "h", "s", 1, "z");
  const needs = view([0, 0, 0, 30], "h", "s", 1, "z");
  const sorted = sortViews([unknownGroup, unknownStart, known, top, needs]);
  assert.deepEqual(sorted, [needs, top, known, unknownStart, unknownGroup]);
  assert.deepEqual(workspaceHeaders(sorted), [2, 4]);
});

test("headers: none without a lower zone, index 0 with no top zone", () => {
  assert.deepEqual(workspaceHeaders([]), []);
  assert.deepEqual(workspaceHeaders([view([0, 0, 0, 1], "h", "s", 1, "a")]), []);
  const lower = [view([1, 1, 2, 1], "h", "s", 1, "a"), view([1, 1, 3, 1], "h", "s", 1, "b"), view([1, 2, 2, 1], "h", "s", 1, "c")];
  assert.deepEqual(workspaceHeaders(lower), [0, 2]);
});

test("duration boundaries", () => {
  const cases = [
    [0, "0s"],
    [999, "0s"],
    [59_000, "59s"],
    [60_000, "1m"],
    [3_599_000, "59m"],
    [3_600_000, "1h 00m"],
    [3_660_000, "1h 01m"],
    [86_399_000, "23h 59m"],
    [86_400_000, "1d 00h"],
    [90_000_000, "1d 01h"],
    [12 * 86_400_000, "12d 00h"],
    [-5_000, "0s"],
  ];
  for (const [ms, want] of cases) assert.equal(formatDuration(ms), want, ms + "ms");
});

test("waiting label: exact, lower bound and unknown", () => {
  const now = 1_000_000_000;
  assert.equal(waitingLabel({ waiting_since: now - 720_000, waiting_exact: true }, now), "12m");
  assert.equal(waitingLabel({ waiting_since: now - 720_000, waiting_exact: false }, now), "≥ 12m");
  assert.ok(waitingLabel({ waiting_since: now - 1000, waiting_exact: false }, now).startsWith("≥ "));
  assert.equal(waitingLabel({ waiting_exact: false }, now), "—");
  assert.equal(waitingLabel({ waiting_exact: true }, now), "—");
});

test("waiting label ticks with the clock", () => {
  const v = { waiting_since: 1000, waiting_exact: true };
  assert.equal(waitingLabel(v, 1000 + 58_000), "58s");
  assert.equal(waitingLabel(v, 1000 + 59_000), "59s");
  assert.equal(waitingLabel(v, 1000 + 60_000), "1m");
});

test("where label leaves out an empty label and its separator", () => {
  assert.equal(whereLabel({ workspace: "ops", tab: "2" }), "ops › 2");
  assert.equal(whereLabel({ workspace: "ops", tab: "" }), "ops");
  assert.equal(whereLabel({ workspace: "", tab: "2" }), "2");
  assert.equal(whereLabel({ workspace: "", tab: "" }), "");
});

test("box label carries everything, in full", () => {
  const now = 10_000_000;
  const v = {
    attention_class: "needs_you",
    heading: "unresolved",
    context_line: "a long context line that the Box truncates",
    directory: "~/src/p",
    workspace: "ops",
    tab: "1",
    pane_position: "top-left",
    waiting_since: now - 120_000,
    waiting_exact: false,
  };
  assert.equal(
    boxLabel(v, now),
    "ops › 1, top-left pane, a long context line that the Box truncates, ~/src/p, unresolved, Needs you, waiting at least 2m",
  );
  assert.equal(
    boxLabel({ ...v, pane_position: "", waiting_since: undefined, context_line: "", attention_class: "unknown" }, now),
    "ops › 1, ~/src/p, unresolved, Unknown, Waiting Time unknown",
  );
  assert.equal(
    boxLabel({ ...v, attention_class: "working", status: "idle", pane_position: "" }, now),
    "ops › 1, a long context line that the Box truncates, ~/src/p, unresolved, Idle, waiting at least 2m",
  );
});

test("golden fixture views keep their heading and directory as sent", () => {
  // The Board shows heading and directory verbatim; the Daemon shortened home.
  for (const { view: v } of fixture.input) {
    assert.ok(!v.directory.startsWith(fixture.home + "/") && v.directory !== fixture.home, v.directory);
    assert.equal(typeof v.heading, "string");
    assert.ok(v.heading.length > 0);
  }
  assert.ok(fixture.input.some(({ view: v }) => v.directory.startsWith("~/")), "no fixture directory starts with ~/");
});

test("header label: the workspace, (unlisted workspace), or (no workspace)", () => {
  assert.equal(headerLabel({ workspace: "ops" }), "ops");
  // An unlabelled workspace herdr did not list has its own group below the
  // sentinel; only the sentinel group (no workspace id) is (no workspace).
  assert.equal(headerLabel({ workspace: "", sort_key: [1, 2 ** 52 + 7, 4, -1, "k"] }), "(unlisted workspace)");
  assert.equal(headerLabel({ workspace: "", sort_key: [1, UNKNOWN - 1, 4, -1, "k"] }), "(unlisted workspace)");
  assert.equal(headerLabel({ workspace: "", sort_key: [1, UNKNOWN, 4, -1, "k"] }), "(no workspace)");
  assert.equal(headerLabel({ workspace: "ws", sort_key: [1, 2 ** 52 + 7, 4, -1, "k"] }), "ws");
  assert.equal(headerLabel({ workspace: "" }), "(no workspace)");
  assert.equal(headerLabel({}), "(no workspace)");
});

test("idle: class working with any Status but working", () => {
  assert.equal(isIdle({ status: "idle", attention_class: "working" }), true);
  assert.equal(isIdle({ status: "unknown", attention_class: "working" }), true);
  assert.equal(isIdle({ status: "working", attention_class: "working" }), false);
  assert.equal(isIdle({ status: "idle", attention_class: "finished" }), false);
  assert.equal(isIdle({ status: "unknown", attention_class: "unknown" }), false);
});

test("idle label: class working, status idle reads Idle and draws as idle", () => {
  const v = { status: "idle", attention_class: "working" };
  assert.equal(classLabel(v), "Idle");
  assert.equal(boxKind(v), "idle");
  assert.equal(classLabel({ status: "working", attention_class: "working" }), "Working");
  assert.equal(boxKind({ status: "working", attention_class: "working" }), "working");
  assert.equal(classLabel({ status: "done", attention_class: "finished" }), "Finished");
  assert.equal(boxKind({ status: "blocked", attention_class: "needs_you" }), "needs_you");
});

test("background: the Daemon's field, read as sent, before Finished and Idle", () => {
  const done = { status: "done", attention_class: "finished", background: true };
  const idle = { status: "idle", attention_class: "working", background: true };
  for (const v of [done, idle]) {
    assert.equal(isBackground(v), true);
    assert.equal(boxKind(v), "background");
    assert.equal(classLabel(v), "Background");
  }
  assert.equal(BACKGROUND_LABEL, "Background");
  // Never derived from the Status or class: false or absent is not Background.
  assert.equal(boxKind({ ...done, background: false }), "finished");
  assert.equal(boxKind({ status: "done", attention_class: "finished" }), "finished");
  assert.equal(boxKind({ ...idle, background: false }), "idle");
  assert.equal(classLabel({ ...idle, background: "yes" }), "Idle");
});

test("background box label says Background", () => {
  const now = 1_000_000;
  const v = {
    key: { host: "h", server: "default", epoch: 1, pane_id: "w1:p1" },
    attention_class: "finished",
    status: "done",
    background: true,
    heading: "cldw",
    context_line: "await-ci",
    directory: "~/src/p",
    workspace: "ops",
    tab: "1",
    pane_position: "",
    waiting_since: now - 75_000,
    waiting_exact: true,
  };
  assert.equal(boxLabel(v, now), "ops › 1, await-ci, ~/src/p, cldw, Background, waiting 1m");
});

test("golden fixture: Background views are the Daemon's, ranked 3", () => {
  const bg = fixture.input.filter((i) => i.view.background);
  assert.ok(bg.length > 0, "no Background view in the fixture");
  for (const { view: v } of bg) {
    assert.equal(boxKind(v), "background", keyString(v));
    assert.equal(v.sort_key[2], 3, keyString(v));
    assert.ok(v.status === "done" || v.status === "idle" || v.status === "working", keyString(v));
  }
  // A working one is Background only with its main turn over (ADR-0009).
  const working = bg.filter((i) => i.view.status === "working");
  assert.ok(working.length > 0, "no working Background view in the fixture");
  for (const i of working) assert.equal(i.turn_ended, true, keyString(i.view));
  // An input marked background that the Daemon did not show as such (another
  // program) stays Finished.
  assert.ok(fixture.input.some((i) => i.background && !i.view.background));
});

test("tab label: the tab, else the workspace", () => {
  assert.equal(tabLabel({ workspace: "ops", tab: "2: api" }), "2: api");
  assert.equal(tabLabel({ workspace: "ops", tab: "" }), "ops");
  assert.equal(tabLabel({ workspace: "", tab: "" }), "");
});

test("pane label: the position, or nothing for a one-pane tab", () => {
  assert.equal(paneLabel({ pane_position: "top-right" }), "top-right pane");
  assert.equal(paneLabel({ pane_position: "" }), "");
  assert.equal(paneLabel({}), "");
});

test("needs you zone is the Daemon's zone 0", () => {
  for (const { view: v } of fixture.input) assert.equal(inNeedsYou(v), v.attention_class === "needs_you", keyString(v));
});

test("group counts: finished, idle, background and working, zeros left out", () => {
  const w = { status: "working", attention_class: "working" };
  const f = { status: "done", attention_class: "finished" };
  const i = { status: "idle", attention_class: "working" };
  const u = { status: "unknown", attention_class: "unknown" };
  const b = { status: "done", attention_class: "finished", background: true };
  assert.equal(groupCounts([w, f, i, i, u]), "1 finished · 2 idle · 1 working");
  assert.equal(groupCounts([i, f]), "1 finished · 1 idle");
  assert.equal(groupCounts([u]), "");
  assert.equal(groupCounts([f, b, w, b]), "1 finished · 2 background · 1 working");
  assert.equal(groupCounts([b]), "1 background");
});

test("title: the Needs you count, or plain herddash", () => {
  const n = { attention_class: "needs_you" };
  const w = { attention_class: "working" };
  assert.equal(documentTitle([n, w, n]), "(2) herddash · Needs you");
  assert.equal(documentTitle([w]), "herddash");
  assert.equal(documentTitle([]), "herddash");
});

test("golden fixture: needs you alone on top, then each group finished, idle, background, working, unknown", () => {
  const { top, groups } = layout(fixture.input.map((i) => i.view).reverse());
  assert.ok(top.length > 0);
  for (const v of top) assert.equal(v.attention_class, "needs_you");
  const rank = { finished: 1, idle: 2, seen: 2, background: 3, working: 4, unknown: 5 };
  for (const g of groups) {
    const ranks = g.views.map((v) => rank[boxKind(v)]);
    assert.deepEqual(ranks, ranks.slice().sort((a, b) => a - b), g.label);
    // Inside a class the most recent comes first (operator decision 2026-10-21).
    for (let j = 1; j < g.views.length; j++) {
      const [p, v] = [g.views[j - 1], g.views[j]];
      if (rank[boxKind(p)] === rank[boxKind(v)] && p.waiting_since !== undefined && v.waiting_since !== undefined) {
        assert.ok(p.waiting_since >= v.waiting_since, keyString(v));
      }
    }
    assert.ok(g.views.every((v) => v.attention_class !== "needs_you"));
  }
  const one = groups.find((g) => g.label === "herddash");
  assert.deepEqual(one.views.map(boxKind), ["finished", "idle", "background", "working", "unknown"]);
  assert.equal(one.counts, "1 finished · 1 idle · 1 background · 1 working");
});

test("golden fixture: each unpositioned workspace has its own header and label, after the positioned ones", () => {
  // Story 5.13: a workspace herdr did not position gets its own group, above
  // every real position and below the unknown sentinel (no workspace id).
  const { groups } = layout(fixture.input.map((i) => i.view).reverse());
  const panesOf = (g) => g.views.map((v) => v.key.pane_id);
  const w7 = groups.findIndex((g) => panesOf(g).includes("w7:p1"));
  const w8 = groups.findIndex((g) => panesOf(g).includes("w8:p1"));
  const w9 = groups.findIndex((g) => panesOf(g).includes("w9:p1"));
  assert.ok(w7 >= 0 && w8 >= 0 && w9 >= 0);
  assert.notEqual(w7, w8);
  assert.deepEqual(panesOf(groups[w7]), ["w7:p1"]);
  assert.deepEqual(panesOf(groups[w8]), ["w8:p1"]);
  assert.equal(groups[w7].label, "scratch");
  assert.equal(groups[w8].label, "(unlisted workspace)");
  assert.equal(groups[w9].label, "(no workspace)");
  const positioned = groups.filter((g) => g.views[0].sort_key[1] < 2 ** 52);
  assert.ok(positioned.length > 0);
  for (const i of [w7, w8]) {
    const g = groups[i].views[0].sort_key[1];
    assert.ok(g >= 2 ** 52 && g < UNKNOWN, String(g));
    assert.ok(i >= positioned.length, "after every positioned group");
  }
  assert.equal(w9, groups.length - 1);
  assert.equal(groups[w9].views[0].sort_key[1], UNKNOWN);
});

test("layout: golden fixture's top zone and groups", () => {
  const views = fixture.input.map((i) => i.view);
  const { top, groups } = layout(views.slice().reverse());
  const h = fixture.headers;
  assert.equal(top.length, h[0]);
  assert.deepEqual(
    groups.map((g) => g.views.length),
    h.map((start, i) => (i + 1 < h.length ? h[i + 1] : views.length) - start),
  );
  const flat = [...top, ...groups.flatMap((g) => g.views)];
  assert.deepEqual(flat.map(keyString), fixture.order);
  assert.deepEqual(
    groups.map((g) => g.label),
    groups.map((g) => headerLabel(g.views[0])),
  );
  for (const g of groups) for (const v of g.views) assert.equal(v.sort_key[1], g.views[0].sort_key[1]);
});

test("layout: no top zone, no lower zone, nothing", () => {
  const w1 = { ...view([1, 1, 2, 1], "h", "s", 1, "a"), workspace: "one" };
  const w2 = { ...view([1, 2, 2, 1], "h", "s", 1, "b"), workspace: "" };
  const lower = layout([w2, w1]);
  assert.deepEqual(lower.top, []);
  assert.deepEqual(lower.groups.map((g) => [g.label, g.views]), [["one", [w1]], ["(no workspace)", [w2]]]);

  const n = view([0, 0, 0, 1], "h", "s", 1, "n");
  const f = view([0, 0, 0, 2], "h", "s", 1, "f");
  const upper = layout([f, n]);
  assert.deepEqual(upper.top, [n, f]);
  assert.deepEqual(upper.groups, []);

  assert.deepEqual(layout([]), { top: [], groups: [] });
});

// Story 3.8: the event function.

const fixtureConnection = {
  connected: true,
  last_snapshot_at: 99,
  herdr_version: "0.9.5",
  herdr_minimum: "0.9.5",
  gate: "ok",
  resync_guaranteed: true,
};

// fixtureModel is the model after a state event carrying the fixture's views.
function fixtureModel() {
  const data = JSON.stringify({
    run_id: "r",
    seq: 7,
    agents: fixture.input.map((i) => i.view),
    connection: fixtureConnection,
    headers: fixture.headers,
  });
  const { model, warning } = applyEvent(emptyModel(), "state", data);
  assert.equal(warning, undefined);
  return model;
}

// fixtureView returns the fixture's view for a pane id (epoch 1 unless given).
function fixtureView(pane, epoch = 1) {
  const v = fixture.input.map((i) => i.view).find((v) => v.key.pane_id === pane && v.key.epoch === epoch);
  assert.ok(v, pane);
  return v;
}

// boardOrder is the key strings in Board order: the top zone, then the groups.
function boardOrder(model) {
  const { top, groups } = layout(modelViews(model));
  return [...top, ...groups.flatMap((g) => g.views)].map(keyString);
}

// changed returns v changed by fields, with sort_key elements 0-3 as given.
function changed(v, fields, sortKey) {
  return { ...v, ...fields, sort_key: [...sortKey, v.sort_key[4]] };
}

function transitionData(view, previous) {
  return JSON.stringify({
    key: view.key,
    transition: { agent_program: view.agent_program, profile: view.profile, previous, new: view.status, observed_at: 1, provenance: "live", possibly_spurious: false },
    agent: view,
  });
}

test("keyId is unique per AgentKey, even with / in a host", () => {
  const a = keyId({ host: "a/b", server: "c", epoch: 1, pane_id: "p" });
  const b = keyId({ host: "a", server: "b/c", epoch: 1, pane_id: "p" });
  assert.notEqual(a, b);
  assert.equal(keyId({ host: "h", server: "s", epoch: 9, pane_id: "p" }), keyId({ pane_id: "p", epoch: 9, server: "s", host: "h" }));
});

test("state replaces the AgentViews and the ConnectionTruth", () => {
  const model = fixtureModel();
  assert.equal(model.agents.size, fixture.input.length);
  assert.deepEqual(model.connection, fixtureConnection);
  assert.deepEqual(boardOrder(model), fixture.order);

  const one = fixtureView("w2:p1");
  const conn = { ...fixtureConnection, connected: false };
  const { model: next } = applyEvent(model, "state", JSON.stringify({ agents: [one], connection: conn, headers: [] }));
  assert.deepEqual(modelViews(next), [one]);
  assert.deepEqual(next.connection, conn);
  assert.equal(model.agents.size, fixture.input.length, "the old model is unchanged");
});

test("agent adds an unseen Agent in its sort position", () => {
  const model = fixtureModel();
  const base = fixtureView("w2:p3");
  // Working since 45, more recent than w2:p3's 40, so just before it.
  const v = changed(base, { key: { ...base.key, pane_id: "w2:p9" }, waiting_since: 45 }, [1, 2, 4, -45]);
  v.sort_key[4] = "mac/default/1/w2:p9";
  const { model: next, warning } = applyEvent(model, "agent", JSON.stringify(v));
  assert.equal(warning, undefined);
  assert.equal(next.agents.size, model.agents.size + 1);
  const order = boardOrder(next);
  assert.equal(order.indexOf("mac/default/1/w2:p9"), order.indexOf("mac/default/1/w2:p3") - 1);
});

test("agent replaces a known Agent", () => {
  const model = fixtureModel();
  const v = { ...fixtureView("w2:p3"), context_line: "new" };
  const { model: next } = applyEvent(model, "agent", JSON.stringify(v));
  assert.equal(next.agents.size, model.agents.size);
  assert.equal(next.agents.get(keyId(v.key)).context_line, "new");
});

test("transition into blocked lands in Needs you at its longest-waiting position", () => {
  const model = fixtureModel();
  const v = changed(fixtureView("w2:p3"), { status: "blocked", attention_class: "needs_you", waiting_since: 100, waiting_exact: true }, [0, 0, 0, 100]);
  const { model: next } = applyEvent(model, "transition", transitionData(v, "working"));
  const { top, groups } = layout(modelViews(next));
  const needs = top.filter((x) => x.attention_class === "needs_you").map(keyString);
  // Newly blocked sorts last among the known starts; an unknown start stays after it.
  assert.deepEqual(needs, ["mac/default/1/w2:p1", "mac/default/1/w1:p1", "mac/default/1/w2:p3", "mac/default/1/w3:p1"]);
  assert.ok(!groups.some((g) => g.views.some((x) => keyString(x) === "mac/default/1/w2:p3")));
});

test("transition into working moves the Box out of the top zone into its workspace group", () => {
  const model = fixtureModel();
  const v = changed(fixtureView("w1:p1"), { status: "working", attention_class: "working", waiting_since: 200, waiting_exact: true }, [1, 1, 4, -200]);
  const { model: next } = applyEvent(model, "transition", transitionData(v, "blocked"));
  const { top, groups } = layout(modelViews(next));
  assert.ok(!top.some((x) => keyString(x) === "mac/default/1/w1:p1"));
  const g = groups.find((g) => g.views.some((x) => keyString(x) === "mac/default/1/w1:p1"));
  assert.equal(g.label, "herddash");
  assert.deepEqual(g.views.map(keyString), ["mac/default/1/w1:p2", "mac/default/1/w1:p5", "mac/default/1/w1:p6", "mac/default/1/w1:p1", "mac/default/1/w1:p3", "mac/default/1/w1:p4"]);
});

test("applyEvent keeps the sort key and waiting_since a done-to-idle transition carries (core owns the guarantee)", () => {
  const model = fixtureModel();
  const before = fixtureView("w1:p2");
  assert.equal(before.status, "done");
  const v = { ...before, status: "idle" };
  const { model: next } = applyEvent(model, "transition", transitionData(v, "done"));
  assert.deepEqual(boardOrder(next), boardOrder(model));
  const now = 1_000_000;
  assert.equal(waitingLabel(next.agents.get(keyId(v.key)), now), waitingLabel(before, now));
  assert.equal(next.agents.get(keyId(v.key)).status, "idle");
});

test("removed drops the key; an unknown key changes nothing", () => {
  const model = fixtureModel();
  const key = fixtureView("w2:p1").key;
  const { model: next, warning } = applyEvent(model, "removed", JSON.stringify({ key }));
  assert.equal(warning, undefined);
  assert.equal(next.agents.size, model.agents.size - 1);
  assert.ok(!next.agents.has(keyId(key)));
  assert.ok(!boardOrder(next).includes("mac/default/1/w2:p1"));
  const again = applyEvent(next, "removed", JSON.stringify({ key }));
  assert.equal(again.model, next);
  assert.equal(again.warning, undefined);
});

test("connection replaces the ConnectionTruth only", () => {
  const model = fixtureModel();
  const conn = { ...fixtureConnection, connected: false, last_snapshot_at: 5 };
  const { model: next } = applyEvent(model, "connection", JSON.stringify(conn));
  assert.deepEqual(next.connection, conn);
  assert.equal(next.agents, model.agents);
});

// The browser never delivers a name nobody listens for; applyEvent still
// leaves the model alone if it is handed one.
test("applyEvent: resync and a name it does not handle change nothing, with no warning", () => {
  const model = fixtureModel();
  for (const [name, data] of [
    ["resync", '{"reason":"events_lost"}'],
    ["somethingnew", '{"x":1}'],
    ["somethingnew", "not json"],
  ]) {
    const r = applyEvent(model, name, data);
    assert.equal(r.model, model, name);
    assert.equal(r.warning, undefined, name);
  }
});

test("a malformed payload changes nothing and gives one warning", () => {
  const model = fixtureModel();
  const v = fixtureView("w2:p1");
  const cases = [
    ["agent", "{bad json"],
    ["agent", "null"],
    ["agent", JSON.stringify({ ...v, sort_key: [0, 0, 0] })],
    ["agent", JSON.stringify({ ...v, sort_key: [0, 0, "0", 1, "k"] })],
    ["agent", JSON.stringify({ ...v, key: { ...v.key, epoch: "1" } })],
    ["transition", JSON.stringify({ key: v.key })],
    ["transition", JSON.stringify({ key: { ...v.key, pane_id: "other" }, agent: v })],
    ["removed", "{}"],
    ["removed", "{bad"],
    ["connection", "[]"],
    ["state", "{bad"],
    ["state", "null"],
    ["state", JSON.stringify({ agents: null, connection: fixtureConnection })],
    ["state", JSON.stringify({ agents: [v, { key: v.key }], connection: fixtureConnection })],
    ["state", JSON.stringify({ agents: [v] })],
    ["state", JSON.stringify({ agents: [v, { ...v, context_line: "dup" }], connection: fixtureConnection })],
    ["agent", JSON.stringify({ ...v, attention_class: undefined })],
    ["agent", JSON.stringify({ ...v, attention_class: "bored" })],
  ];
  for (const [name, data] of cases) {
    const r = applyEvent(model, name, data);
    assert.equal(r.model, model, name + " " + data);
    assert.equal(typeof r.warning, "string", name + " " + data);
    assert.ok(r.warning.startsWith(name + " event"), r.warning);
  }
});

test("reconnect backoff doubles from the start to the cap", () => {
  assert.ok(RECONNECT_START_MS >= 500 && RECONNECT_START_MS <= 2000);
  assert.ok(RECONNECT_CAP_MS >= 20_000 && RECONNECT_CAP_MS <= 60_000);
  let d = RECONNECT_START_MS;
  const seen = [d];
  for (let i = 0; i < 10; i++) seen.push((d = nextReconnectDelay(d)));
  assert.equal(seen[1], RECONNECT_START_MS * 2);
  assert.equal(seen.at(-1), RECONNECT_CAP_MS);
  assert.ok(seen.every((x) => x <= RECONNECT_CAP_MS));
});

test("EVENT_NAMES are exactly the Daemon's names and cover every name applyEvent handles", () => {
  assert.deepEqual([...EVENT_NAMES].sort(), ["agent", "connection", "keepalive", "removed", "resync", "state", "transition"]);
  for (const name of HANDLED_EVENTS) assert.ok(EVENT_NAMES.includes(name), name);
  // A name is handled exactly when applyEvent looks at its payload.
  for (const name of EVENT_NAMES) {
    const r = applyEvent(emptyModel(), name, "{bad");
    assert.equal(typeof r.warning === "string", HANDLED_EVENTS.includes(name), name);
  }
});

const stateData = () =>
  JSON.stringify({ agents: fixture.input.map((i) => i.view), connection: fixtureConnection, headers: fixture.headers });

// opened returns the stream state with source opened at now, and its state
// (view id, when given) applied at now.
function opened(source, now = 0, id) {
  let { st } = streamDecision(streamState(), { kind: "open", source, now });
  ({ st } = streamDecision(st, { kind: "event", source, name: "state", data: stateData(), id, now }));
  assert.equal(st.awaitingState, false);
  return st;
}

test("stream: error reopens only for the current, closed source with no reopen pending", () => {
  const a = {};
  let st = opened(a);
  // Still connecting: EventSource retries by itself.
  assert.equal(streamDecision(st, { kind: "error", source: a, closed: false, now: 1 }).reopenAfter, undefined);
  // A source that is not current.
  assert.equal(streamDecision(st, { kind: "error", source: {}, closed: true, now: 1 }).reopenAfter, undefined);
  const d = streamDecision(st, { kind: "error", source: a, closed: true, now: 1 });
  assert.equal(d.reopenAfter, RECONNECT_START_MS);
  assert.equal(d.close, a);
  assert.equal(d.st.delay, RECONNECT_START_MS * 2);
  assert.equal(d.st.pending, true);
  // A second error while the reopen is pending does nothing.
  const again = streamDecision(d.st, { kind: "error", source: a, closed: true, now: 1 });
  assert.equal(again.reopenAfter, undefined);
  assert.equal(again.st, d.st);
});

test("stream: the delay doubles to the cap across failed reopens, and a valid state resets it", () => {
  let st = opened({});
  const delays = [];
  for (let i = 0; i < 8; i++) {
    const s = {};
    ({ st } = streamDecision(st, { kind: "open", source: s }));
    const d = streamDecision(st, { kind: "error", source: s, closed: true, now: 1 });
    delays.push(d.reopenAfter);
    st = d.st;
  }
  assert.deepEqual(delays.slice(0, 3), [RECONNECT_START_MS, RECONNECT_START_MS * 2, RECONNECT_START_MS * 4]);
  assert.equal(delays.at(-1), RECONNECT_CAP_MS);
  const b = {};
  ({ st } = streamDecision(st, { kind: "open", source: b }));
  assert.equal(st.pending, false);
  ({ st } = streamDecision(st, { kind: "event", source: b, name: "state", data: stateData() }));
  assert.equal(st.delay, RECONNECT_START_MS);
});

test("stream: a malformed state does not reset the delay or replace the model; it backs off", () => {
  let st = opened({});
  const s = {};
  ({ st } = streamDecision(st, { kind: "open", source: s }));
  st = streamDecision(st, { kind: "error", source: s, closed: true, now: 1 }).st;
  const b = {};
  ({ st } = streamDecision(st, { kind: "open", source: b }));
  const d = streamDecision(st, { kind: "event", source: b, name: "state", data: "{bad", now: 2 });
  assert.equal(typeof d.warning, "string");
  assert.equal(d.reopenAfter, RECONNECT_START_MS * 2);
  assert.equal(d.close, b);
  assert.equal(d.st.delay, RECONNECT_START_MS * 4);
  assert.equal(d.st.model, st.model);
  assert.equal(d.st.awaitingState, true);
});

test("stream: a new source's events are ignored until a valid state arrives on it", () => {
  const a = {};
  let st = opened(a);
  const old = st.model;
  const b = {};
  ({ st } = streamDecision(st, { kind: "open", source: b }));
  assert.equal(st.awaitingState, true);
  const key = fixtureView("w2:p1").key;
  const removed = JSON.stringify({ key });
  // Before any state, and after a malformed one: no event reaches the old model.
  for (const pre of [null, "{bad"]) {
    if (pre) st = streamDecision(st, { kind: "event", source: b, name: "state", data: pre }).st;
    const d = streamDecision(st, { kind: "event", source: b, name: "removed", data: removed });
    assert.equal(d.st.model, old);
    assert.equal(d.warning, undefined);
  }
  // The old source is no longer current.
  ({ st } = streamDecision(st, { kind: "event", source: b, name: "state", data: stateData() }));
  assert.equal(streamDecision(st, { kind: "event", source: a, name: "removed", data: removed }).st.model, st.model);
  // A valid state on the new source, then its events apply.
  const d = streamDecision(st, { kind: "event", source: b, name: "removed", data: removed });
  assert.ok(!d.st.model.agents.has(keyId(key)));
});

test("stream: events on the current source go through applyEvent", () => {
  const a = {};
  const st = opened(a);
  assert.equal(st.model.agents.size, fixture.input.length);
  const bad = streamDecision(st, { kind: "event", source: a, name: "agent", data: "{bad" });
  assert.equal(bad.st.model, st.model);
  assert.equal(typeof bad.warning, "string");
  const resync = streamDecision(st, { kind: "event", source: a, name: "resync", data: '{"reason":"x"}' });
  assert.equal(resync.st.model, st.model);
  assert.equal(resync.warning, undefined);
  assert.equal(resync.reopenAfter, undefined);
  const conn = { ...fixtureConnection, connected: false };
  const c = streamDecision(st, { kind: "event", source: a, name: "connection", data: JSON.stringify(conn) });
  assert.deepEqual(c.st.model.connection, conn);
});

// ---- A hung Daemon, a rejected event, a skipped publication (Story 5.11) ----

// ticks feeds a tick every second from `from` to `to` ms, failing on a
// reopen, and returns the state.
function ticks(st, from, to) {
  for (let now = from; now <= to; now += 1000) {
    const d = streamDecision(st, { kind: "tick", now });
    assert.equal(d.reopenAfter, undefined, "tick at " + now);
    st = d.st;
  }
  return st;
}

const agentData = (pane) => JSON.stringify(fixtureView(pane));

test("stream: keepalives every 15 s keep the stream alive", () => {
  const a = {};
  let st = opened(a, 0, "7");
  for (let now = 15000; now <= 180000; now += 15000) {
    st = ticks(st, now - 14000, now - 1000);
    const d = streamDecision(st, { kind: "event", source: a, name: "keepalive", data: "{}", id: "7", now });
    assert.equal(d.reopenAfter, undefined);
    assert.equal(d.warning, undefined);
    assert.equal(d.st.model, st.model);
    st = d.st;
  }
  assert.equal(st.lastEventAt, 180000);
  assert.equal(streamLost(st, 180000), false);
  assert.equal(streamDecision(st, { kind: "tick", now: 180000 + STREAM_SILENCE_MS }).reopenAfter, undefined);
});

test("stream: a Daemon silent past the bound is lost at once and reopened, backing off", () => {
  const a = {};
  let st = opened(a, 0, "7");
  assert.equal(streamDecision(st, { kind: "tick", now: STREAM_SILENCE_MS }).reopenAfter, undefined);
  let d = streamDecision(st, { kind: "tick", now: STREAM_SILENCE_MS + 1 });
  assert.equal(d.reopenAfter, RECONNECT_START_MS);
  assert.equal(d.close, a);
  assert.equal(d.warning, undefined);
  assert.equal(d.st.lostSince, 0, "lost since the last sign of life");
  assert.equal(streamLost(d.st, STREAM_SILENCE_MS + 1), true, "the lost strip shows at once");
  assert.equal(boardStatus(d.st.model.connection, streamLost(d.st, STREAM_SILENCE_MS + 1), STREAM_SILENCE_MS + 1).dimmed, true);
  assert.equal(d.st.model, st.model, "the Boxes stay, dimmed");
  st = d.st;
  // The reopened source is silent too (the Daemon still stopped): each
  // reopen waits twice as long, up to the cap.
  const delays = [d.reopenAfter];
  let now = STREAM_SILENCE_MS + 1;
  for (let i = 0; i < 7; i++) {
    now += delays.at(-1);
    const s = {};
    ({ st } = streamDecision(st, { kind: "open", source: s, now }));
    assert.equal(streamLost(st, now), true);
    now += STREAM_SILENCE_MS + 1;
    d = streamDecision(st, { kind: "tick", now });
    assert.equal(d.close, s);
    delays.push(d.reopenAfter);
    st = d.st;
  }
  assert.deepEqual(delays, [1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
  assert.equal(st.lostSince, 0);
});

test("stream: silence while a reopen is pending decides nothing more", () => {
  const a = {};
  const st = streamDecision(opened(a), { kind: "tick", now: STREAM_SILENCE_MS + 1 }).st;
  assert.equal(st.pending, true);
  for (const now of [STREAM_SILENCE_MS + 2, 10 * STREAM_SILENCE_MS]) {
    const d = streamDecision(st, { kind: "tick", now });
    assert.equal(d.st, st);
    assert.equal(d.reopenAfter, undefined);
  }
});

test("stream: recovery, a reopened source's valid state clears the loss and resets the delay", () => {
  const a = {};
  let { st } = streamDecision(opened(a), { kind: "tick", now: STREAM_SILENCE_MS + 1 });
  const b = {};
  ({ st } = streamDecision(st, { kind: "open", source: b, now: STREAM_SILENCE_MS + 1001 }));
  ({ st } = streamDecision(st, { kind: "connected", source: b, now: STREAM_SILENCE_MS + 1002 }));
  ({ st } = streamDecision(st, { kind: "event", source: b, name: "state", data: stateData(), id: "9", now: STREAM_SILENCE_MS + 1003 }));
  assert.equal(st.lostSince, null);
  assert.equal(st.delay, RECONNECT_START_MS);
  assert.equal(st.pending, false);
  assert.equal(streamLost(st, STREAM_SILENCE_MS + 1003), false);
  assert.deepEqual(boardStatus(st.model.connection, false, NOW).strips, []);
});

test("stream: a rejected state is lost from now and reopened", () => {
  const a = {};
  let { st } = streamDecision(streamState(), { kind: "open", source: a, now: 0 });
  const d = streamDecision(st, { kind: "event", source: a, name: "state", data: JSON.stringify({ agents: null }), id: "7", now: 10 });
  assert.equal(typeof d.warning, "string");
  assert.ok(d.warning.startsWith("state event"), d.warning);
  assert.equal(d.st.lostSince, 10);
  assert.equal(d.close, a);
  assert.equal(d.reopenAfter, RECONNECT_START_MS);
  assert.equal(d.st.delay, RECONNECT_START_MS * 2);
  // A reopen that heals within the grace period never shows the strip.
  assert.equal(streamLost(d.st, 10 + LOST_GRACE_MS - 1), false);
  assert.equal(streamLost(d.st, 10 + LOST_GRACE_MS), true);
  // It repeats: backoff.
  const b = {};
  ({ st } = streamDecision(d.st, { kind: "open", source: b, now: 1010 }));
  const again = streamDecision(st, { kind: "event", source: b, name: "state", data: "{bad", id: "7", now: 1020 });
  assert.equal(again.reopenAfter, RECONNECT_START_MS * 2);
  assert.equal(again.st.lostSince, 10, "trouble kept from its start");
});

test("stream: a rejected delta is lost from now and reopened", () => {
  const view = fixtureView("w2:p1");
  const cases = [
    ["agent", JSON.stringify({ ...view, attention_class: "bored" })],
    ["transition", JSON.stringify({ key: { ...view.key, pane_id: "other" }, transition: {}, agent: view })],
    ["removed", JSON.stringify({ key: null })],
    ["connection", "[not json"],
  ];
  for (const [name, data] of cases) {
    const a = {};
    const st = opened(a, 0, "7");
    const d = streamDecision(st, { kind: "event", source: a, name, data, id: "8", now: 50 });
    assert.equal(typeof d.warning, "string", name);
    assert.ok(d.warning.startsWith(name + " event"), d.warning);
    assert.equal(d.st.model, st.model, name);
    assert.equal(d.st.lostSince, 50, name);
    assert.equal(d.close, a, name);
    assert.equal(d.reopenAfter, RECONNECT_START_MS, name);
    assert.equal(d.st.delay, RECONNECT_START_MS * 2, name);
  }
});

test("stream: a skipped publication is lost from now and reopened", () => {
  for (const id of ["9", "6", "100"]) {
    const a = {};
    const st = opened(a, 0, "7");
    const d = streamDecision(st, { kind: "event", source: a, name: "agent", data: agentData("w2:p1"), id, now: 70 });
    assert.equal(typeof d.warning, "string", id);
    assert.match(d.warning, new RegExp("from view 7 to " + id), id);
    assert.equal(d.st.model, st.model, id);
    assert.equal(d.st.lostSince, 70, id);
    assert.equal(d.close, a, id);
    assert.equal(d.reopenAfter, RECONNECT_START_MS, id);
  }
});

test("stream: one publication's events share a sequence; ids 8, 8, 9 after state 7 apply in order", () => {
  const a = {};
  let st = opened(a, 0, "7");
  const v1 = fixtureView("w2:p1");
  const v2 = fixtureView("w1:p1");
  const steps = [
    ["removed", JSON.stringify({ key: v1.key }), "8"],
    ["removed", JSON.stringify({ key: v2.key }), "8"],
    ["agent", JSON.stringify(v1), "9"],
  ];
  for (const [name, data, id] of steps) {
    const d = streamDecision(st, { kind: "event", source: a, name, data, id, now: 1 });
    assert.equal(d.reopenAfter, undefined, name + " " + id);
    assert.equal(d.warning, undefined, name + " " + id);
    st = d.st;
  }
  assert.equal(st.lastSeq, 9);
  assert.ok(st.model.agents.has(keyId(v1.key)));
  assert.ok(!st.model.agents.has(keyId(v2.key)));
});

test("stream: keepalive, resync and an event with no id are not sequence-checked", () => {
  const a = {};
  let st = opened(a, 0, "7");
  for (const [name, data, id] of [
    ["keepalive", "{}", "3"],
    ["keepalive", "{}", ""],
    ["agent", agentData("w2:p1"), ""],
    ["agent", agentData("w2:p1"), undefined],
    ["agent", agentData("w2:p1"), "not-a-number"],
  ]) {
    const d = streamDecision(st, { kind: "event", source: a, name, data, id, now: 1 });
    assert.equal(d.reopenAfter, undefined, name + " " + id);
    assert.equal(d.warning, undefined, name + " " + id);
    st = d.st;
  }
  assert.equal(st.lastSeq, 7);
  // A publication holding only a resync still advances the sequence.
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "resync", data: '{"reason":"closed"}', id: "8", now: 2 }));
  assert.equal(st.lastSeq, 8);
  const d = streamDecision(st, { kind: "event", source: a, name: "agent", data: agentData("w2:p1"), id: "9", now: 3 });
  assert.equal(d.reopenAfter, undefined);
  assert.equal(d.st.lastSeq, 9);
});

test("stream: EventSource's own open re-arms awaitingState and the silence clock", () => {
  const a = {};
  let st = opened(a, 0, "7");
  const old = st.model;
  // Another source's open changes nothing.
  assert.equal(streamDecision(st, { kind: "connected", source: {}, now: 5 }).st, st);
  ({ st } = streamDecision(st, { kind: "connected", source: a, now: 20000 }));
  assert.equal(st.awaitingState, true);
  assert.equal(st.lastEventAt, 20000);
  assert.equal(st.lastSeq, null);
  // Until the reconnected stream's state, its events change nothing.
  const removed = JSON.stringify({ key: fixtureView("w2:p1").key });
  let d = streamDecision(st, { kind: "event", source: a, name: "removed", data: removed, id: "3", now: 20001 });
  assert.equal(d.st.model, old);
  assert.equal(d.reopenAfter, undefined);
  // Silence is measured from the reconnect and the ignored event.
  assert.equal(streamDecision(d.st, { kind: "tick", now: 20001 + STREAM_SILENCE_MS }).reopenAfter, undefined);
  // The state on the reconnected stream sets the sequence afresh.
  d = streamDecision(d.st, { kind: "event", source: a, name: "state", data: stateData(), id: "3", now: 20002 });
  assert.equal(d.st.lastSeq, 3);
  assert.equal(d.st.awaitingState, false);
});

test("stream: events from a source that is not current never move the silence clock", () => {
  const a = {};
  let st = opened(a, 0, "7");
  const b = {};
  ({ st } = streamDecision(st, { kind: "open", source: b, now: 1000 }));
  for (let now = 2000; now < 40000; now += 5000) {
    st = streamDecision(st, { kind: "event", source: a, name: "keepalive", data: "{}", now }).st;
  }
  assert.equal(st.lastEventAt, 1000);
  const d = streamDecision(st, { kind: "tick", now: 1001 + STREAM_SILENCE_MS });
  assert.equal(d.close, b);
  assert.equal(d.reopenAfter, RECONNECT_START_MS);
});

test("stream: a gap or a rejected delta before the first valid state on a source is not checked", () => {
  const a = {};
  let { st } = streamDecision(streamState(), { kind: "open", source: a, now: 0 });
  for (const [name, data, id] of [
    ["agent", "{bad", "99"],
    ["removed", "{bad", "1"],
    ["connection", "[", ""],
  ]) {
    const d = streamDecision(st, { kind: "event", source: a, name, data, id, now: 1 });
    assert.equal(d.warning, undefined, name);
    assert.equal(d.reopenAfter, undefined, name);
    assert.equal(d.st.model, st.model, name);
    st = d.st;
  }
  assert.equal(st.awaitingState, true);
});

// ---- Strips (Story 3.9) ----

const NOW = Date.UTC(2026, 9, 20, 12, 0, 0);
const okConn = { connected: true, last_snapshot_at: NOW - 1000, herdr_version: "0.9.5", herdr_minimum: "0.9.5", gate: "ok", resync_guaranteed: true };

// stripText is a strip as it reads on screen: every part but the screen-reader-only one.
const stripText = (s) => s.parts.filter((p) => !p.sr).map((p) => p.text).join("");

test("strips: all well shows no strip and normal Boxes", () => {
  assert.deepEqual(boardStatus(okConn, false, NOW), { strips: [], dimmed: false, hideBoxes: false });
  // Before the first state, with the stream up: nothing to say yet.
  assert.deepEqual(boardStatus(null, false, NOW), { strips: [], dimmed: false, hideBoxes: false });
});

test("strips: disconnected shows the ticking snapshot age and dims Boxes", () => {
  const c = { ...okConn, connected: false, last_snapshot_at: NOW - 160000 };
  const st = boardStatus(c, false, NOW);
  assert.equal(st.dimmed, true);
  assert.equal(st.hideBoxes, false);
  assert.equal(st.strips.length, 1);
  const [s] = st.strips;
  assert.equal(s.kind, "disconnected");
  assert.equal(stripText(s), "Disconnected from herdr. Last snapshot 2m 40s ago; Boxes show that snapshot.");
  // The age ticks and is hidden from screen readers, which get the fixed UTC time.
  assert.deepEqual(s.parts.find((p) => p.tick), { text: "2m 40s ago", tick: true });
  assert.deepEqual(s.parts.find((p) => p.sr), { text: "at 11:57:20 UTC", sr: true });
  const later = boardStatus(c, false, NOW + 5000).strips;
  assert.equal(later[0].parts.find((p) => p.tick).text, "2m 45s ago");
  // The ticking age does not change the strips' identity, so the live region is not redrawn.
  assert.equal(stripsKey(later), stripsKey(st.strips));
});

test("strips: disconnected before any snapshot", () => {
  const c = { ...okConn, connected: false };
  delete c.last_snapshot_at;
  const st = boardStatus(c, false, NOW);
  assert.equal(st.dimmed, true);
  assert.equal(st.strips.length, 1);
  assert.equal(st.strips[0].kind, "disconnected");
  assert.equal(stripText(st.strips[0]), "Disconnected from herdr. No snapshot received yet.");
  assert.ok(!st.strips[0].parts.some((p) => p.tick || p.sr));
});

test("strips: the Daemon unreachable replaces the herdr text and dims Boxes", () => {
  for (const c of [okConn, { ...okConn, connected: false, last_snapshot_at: NOW - 160000 }, null]) {
    const st = boardStatus(c, true, NOW);
    assert.equal(st.dimmed, true);
    assert.equal(st.strips.length, 1);
    assert.equal(st.strips[0].kind, "disconnected");
    assert.equal(stripText(st.strips[0]), "Lost contact with herddash. Reconnecting…");
    assert.equal(stripText(st.strips[0]), LOST_TEXT);
  }
  // The version strip still follows the latest ConnectionTruth.
  const adv = boardStatus({ ...okConn, gate: "advisory", herdr_version: "0.9.4" }, true, NOW);
  assert.deepEqual(adv.strips.map((s) => s.kind), ["disconnected", "advisory"]);
});

test("strips: advisory with resync guaranteed has no resync caveat and shows Boxes", () => {
  const st = boardStatus({ ...okConn, gate: "advisory", herdr_version: "0.9.4", herdr_minimum: "0.9.5" }, false, NOW);
  assert.equal(st.dimmed, false);
  assert.equal(st.hideBoxes, false);
  assert.equal(st.strips.length, 1);
  assert.equal(st.strips[0].kind, "advisory");
  assert.equal(stripText(st.strips[0]), "herdr 0.9.4 is below the supported minimum 0.9.5; recording under the dogfooding flag.");
});

test("strips: advisory below 0.9.0 adds the resync caveat", () => {
  const c = { ...okConn, gate: "advisory", herdr_version: "0.8.2", resync_guaranteed: false };
  const [s] = boardStatus(c, false, NOW).strips;
  assert.equal(s.kind, "advisory");
  assert.equal(
    stripText(s),
    "herdr 0.8.2 is below the supported minimum 0.9.5; recording under the dogfooding flag. " +
      "Resync is not guaranteed with this herdr version.",
  );
  // Absent means not guaranteed.
  const absent = { ...c };
  delete absent.resync_guaranteed;
  assert.equal(stripText(boardStatus(absent, false, NOW).strips[0]), stripText(s));
});

test("strips: refused names both versions and hides every Box", () => {
  const st = boardStatus({ ...okConn, gate: "refused", herdr_version: "0.8.2", resync_guaranteed: false }, false, NOW);
  assert.equal(st.hideBoxes, true);
  assert.equal(st.strips.length, 1);
  assert.equal(st.strips[0].kind, "refused");
  assert.equal(stripText(st.strips[0]), "herdr 0.8.2 is not supported. herddash needs 0.9.5 or newer and is recording nothing.");
  // Whatever the model holds: hideBoxes depends on the ConnectionTruth alone.
  let { model } = applyEvent(emptyModel(), "state", stateData());
  ({ model } = applyEvent(model, "connection", JSON.stringify({ ...okConn, gate: "refused", herdr_version: "0.8.2" })));
  assert.ok(model.agents.size > 0);
  assert.equal(boardStatus(model.connection, false, NOW).hideBoxes, true);
});

test("strips: disconnected and advisory show slate first, then amber", () => {
  const c = { ...okConn, connected: false, last_snapshot_at: NOW - 160000, gate: "advisory", herdr_version: "0.8.2", resync_guaranteed: false };
  const st = boardStatus(c, false, NOW);
  assert.deepEqual(st.strips.map((s) => s.kind), ["disconnected", "advisory"]);
  assert.equal(st.dimmed, true);
  assert.equal(st.hideBoxes, false);
});

test("strips: a connection event showing herdr connected clears the strip and the dimming", () => {
  const a = {};
  let st = opened(a);
  const down = { ...okConn, connected: false, last_snapshot_at: NOW - 160000 };
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "connection", data: JSON.stringify(down) }));
  assert.equal(boardStatus(st.model.connection, false, NOW).dimmed, true);
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "connection", data: JSON.stringify(okConn) }));
  assert.deepEqual(boardStatus(st.model.connection, false, NOW), { strips: [], dimmed: false, hideBoxes: false });
});

test("stream: lost only after LOST_GRACE_MS of errors with no valid state", () => {
  const a = {};
  let st = opened(a);
  assert.equal(st.lostSince, null);
  assert.equal(streamLost(st, NOW), false);
  // Not the current source: nothing.
  assert.equal(streamDecision(st, { kind: "error", source: {}, closed: false, now: NOW }).st, st);
  // EventSource retrying by itself: in trouble from NOW, no reopen, not yet lost.
  let d = streamDecision(st, { kind: "error", source: a, closed: false, now: NOW });
  assert.equal(d.st.lostSince, NOW);
  assert.equal(d.reopenAfter, undefined);
  assert.equal(streamLost(d.st, NOW + LOST_GRACE_MS - 1), false);
  assert.equal(streamLost(d.st, NOW + LOST_GRACE_MS), true);
  // A transient drop healed within the grace period never shows.
  const healed = streamDecision(d.st, { kind: "event", source: a, name: "state", data: stateData() }).st;
  assert.equal(healed.lostSince, null);
  assert.equal(streamLost(healed, NOW + LOST_GRACE_MS), false);
  // Closed for good: a later error keeps the first time, and a new source does not clear it.
  d = streamDecision(d.st, { kind: "error", source: a, closed: true, now: NOW + 500 });
  assert.equal(d.st.lostSince, NOW);
  assert.equal(d.reopenAfter, RECONNECT_START_MS);
  const b = {};
  ({ st } = streamDecision(d.st, { kind: "open", source: b }));
  assert.equal(st.lostSince, NOW);
  // A malformed state does not clear it; a valid one does.
  st = streamDecision(st, { kind: "event", source: b, name: "state", data: "{bad" }).st;
  assert.equal(streamLost(st, NOW + LOST_GRACE_MS), true);
  ({ st } = streamDecision(st, { kind: "event", source: b, name: "state", data: stateData() }));
  assert.equal(streamLost(st, NOW + LOST_GRACE_MS), false);
});

test("strips: anything but connected true is disconnected", () => {
  for (const connected of [undefined, "true", 1, null]) {
    const c = { ...okConn, connected };
    if (connected === undefined) delete c.connected;
    const st = boardStatus(c, false, NOW);
    assert.equal(st.dimmed, true, String(connected));
    assert.equal(st.strips[0].kind, "disconnected", String(connected));
  }
});

test("strips: a non-finite last_snapshot_at reads as no snapshot", () => {
  for (const at of [NaN, Infinity, "99", -5, 0]) {
    const [s] = boardStatus({ ...okConn, connected: false, last_snapshot_at: at }, false, NOW).strips;
    assert.equal(stripText(s), "Disconnected from herdr. No snapshot received yet.", String(at));
  }
});

test("strips: a missing or unknown gate is refused, naming the gate", () => {
  const missing = { ...okConn };
  delete missing.gate;
  for (const [c, g] of [
    [missing, "none"],
    [{ ...okConn, gate: "x" }, "x"],
    [{ ...okConn, gate: 7 }, "none"],
  ]) {
    const st = boardStatus(c, false, NOW);
    assert.equal(st.hideBoxes, true, g);
    assert.equal(st.strips.length, 1);
    assert.equal(st.strips[0].kind, "refused");
    assert.equal(stripText(st.strips[0]), "herdr version gate unknown (" + g + "); nothing is being recorded.");
  }
});

test("strips: a missing gate while disconnected is the Daemon before its first decision", () => {
  for (const gate of [undefined, ""]) {
    const c = { connected: false, herdr_minimum: "0.9.5", resync_guaranteed: false, gate };
    if (gate === undefined) delete c.gate;
    const st = boardStatus(c, false, NOW);
    assert.deepEqual(
      st.strips.map((s) => s.kind),
      ["disconnected"],
      String(gate),
    );
    assert.equal(stripText(st.strips[0]), "Disconnected from herdr. No snapshot received yet.");
    assert.equal(st.hideBoxes, false);
    assert.equal(st.dimmed, true);
  }
  // Connected with a missing gate is still refused.
  const connected = { ...okConn };
  delete connected.gate;
  const st = boardStatus(connected, false, NOW);
  assert.deepEqual(
    st.strips.map((s) => s.kind),
    ["refused"],
  );
  assert.equal(stripText(st.strips[0]), "herdr version gate unknown (none); nothing is being recorded.");
  assert.equal(st.hideBoxes, true);
  // A present unknown gate while disconnected is still refused.
  assert.equal(boardStatus({ ...okConn, connected: false, gate: "x" }, false, NOW).hideBoxes, true);
});

test("strips: disconnected and refused drop the clause about Boxes", () => {
  const c = { ...okConn, connected: false, last_snapshot_at: NOW - 160000, gate: "refused", herdr_version: "0.8.2" };
  const st = boardStatus(c, false, NOW);
  assert.equal(st.hideBoxes, true);
  assert.deepEqual(
    st.strips.map((s) => s.kind),
    ["disconnected", "refused"],
  );
  assert.equal(stripText(st.strips[0]), "Disconnected from herdr. Last snapshot 2m 40s ago.");
});

test("strips: lost and refused show the lost strip, then red, with no Boxes", () => {
  const st = boardStatus({ ...okConn, gate: "refused", herdr_version: "0.8.2" }, true, NOW);
  assert.deepEqual(
    st.strips.map((s) => s.kind),
    ["disconnected", "refused"],
  );
  assert.equal(stripText(st.strips[0]), LOST_TEXT);
  assert.equal(st.dimmed, true);
  assert.equal(st.hideBoxes, true);
});

test("strips: an unreadable herdr version, refused, says herddash could not read it", () => {
  for (const version of [undefined, ""]) {
    const r = { ...okConn, gate: "refused", herdr_version: version };
    if (version === undefined) delete r.herdr_version;
    const st = boardStatus(r, false, NOW);
    assert.deepEqual(st.strips.map((s) => s.kind), ["refused"]);
    assert.equal(
      stripText(st.strips[0]),
      "herddash could not read herdr's version. It needs 0.9.5 or newer and is recording nothing.",
      String(version),
    );
    assert.equal(st.hideBoxes, true);
  }
});

test("strips: an unreadable herdr version, advisory, says herddash could not read it", () => {
  for (const version of [undefined, ""]) {
    const c = { ...okConn, gate: "advisory", herdr_version: version };
    if (version === undefined) delete c.herdr_version;
    assert.equal(
      stripText(boardStatus(c, false, NOW).strips[0]),
      "herddash could not read herdr's version; recording under the dogfooding flag.",
      String(version),
    );
    for (const rg of [false, undefined]) {
      assert.equal(
        stripText(boardStatus({ ...c, resync_guaranteed: rg }, false, NOW).strips[0]),
        "herddash could not read herdr's version; recording under the dogfooding flag. Resync is not guaranteed with this herdr version.",
        String(version) + " " + String(rg),
      );
    }
  }
});

test("strips: a state event showing herdr connected clears the disconnected strip", () => {
  const a = {};
  let st = opened(a);
  const down = { ...okConn, connected: false, last_snapshot_at: NOW - 160000 };
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "connection", data: JSON.stringify(down) }));
  assert.equal(boardStatus(st.model.connection, streamLost(st, NOW), NOW).dimmed, true);
  const data = JSON.stringify({ agents: fixture.input.map((i) => i.view), connection: okConn });
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "state", data }));
  assert.deepEqual(boardStatus(st.model.connection, streamLost(st, NOW), NOW), {
    strips: [],
    dimmed: false,
    hideBoxes: false,
  });
});

test("strips: recording off shows the red recording strip; Boxes stay shown and undimmed", () => {
  const st = boardStatus(okConn, false, NOW, false);
  assert.deepEqual(st, { strips: [{ kind: "recording", parts: [{ text: RECORDING_OFF_TEXT }] }], dimmed: false, hideBoxes: false });
  assert.equal(
    stripText(st.strips[0]),
    "herddash is not recording this run: its saved state could not be loaded. Boxes are live, but no history is being written. See herddash.log.",
  );
  // Only exactly false is a run recording nothing.
  for (const r of [true, undefined, null, "false", 0]) {
    assert.deepEqual(boardStatus(okConn, false, NOW, r).strips, [], String(r));
  }
});

test("strips: recording off comes after the connection and version strips; only disconnection dims", () => {
  const c = { ...okConn, connected: false, gate: "advisory", herdr_version: "0.9.4", last_snapshot_at: NOW - 160000 };
  const st = boardStatus(c, false, NOW, false);
  assert.deepEqual(st.strips.map((s) => s.kind), ["disconnected", "advisory", "recording"]);
  assert.equal(st.dimmed, true);
  assert.equal(st.hideBoxes, false);
  // Disconnected alone with recording off: connection strip, then recording.
  const down = boardStatus({ ...okConn, connected: false }, false, NOW, false);
  assert.deepEqual(down.strips.map((s) => s.kind), ["disconnected", "recording"]);
  assert.equal(down.dimmed, true);
  // Recording off with herdr connected dims nothing; a lost stream still dims.
  assert.equal(boardStatus(okConn, false, NOW, false).dimmed, false);
  assert.deepEqual(boardStatus(okConn, true, NOW, false).strips.map((s) => s.kind), ["disconnected", "recording"]);
  assert.notEqual(stripsKey(st.strips), stripsKey(boardStatus(c, false, NOW).strips));
});

test("state keeps the view's recording; an older Daemon's state without it means recording", () => {
  const a = {};
  let st = opened(a);
  const off = JSON.stringify({ agents: fixture.input.map((i) => i.view), connection: okConn, recording: false });
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "state", data: off }));
  assert.equal(st.model.recording, false);
  assert.deepEqual(boardStatus(st.model.connection, streamLost(st, NOW), NOW, st.model.recording).strips.map((s) => s.kind), ["recording"]);
  // A change event keeps it: no event but state carries it.
  const conn = JSON.stringify({ ...okConn, herdr_version: "0.9.6" });
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "connection", data: conn }));
  assert.equal(st.model.recording, false);
  const on = JSON.stringify({ agents: [], connection: okConn, recording: true });
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "state", data: on }));
  assert.equal(st.model.recording, true);
  // An older Daemon sends no recording: the model holds none, and no strip shows.
  const old = JSON.stringify({ agents: [], connection: okConn });
  ({ st } = streamDecision(st, { kind: "event", source: a, name: "state", data: old }));
  assert.equal("recording" in st.model, false);
  assert.deepEqual(boardStatus(st.model.connection, false, NOW, st.model.recording).strips, []);
});

test("formatAge keeps seconds below an hour, then follows formatDuration", () => {
  assert.equal(formatAge(-5), "0s");
  assert.equal(formatAge(59999), "59s");
  assert.equal(formatAge(60000), "1m 00s");
  assert.equal(formatAge(160000), "2m 40s");
  assert.equal(formatAge(3599999), "59m 59s");
  assert.equal(formatAge(3600000), "1h 00m");
  assert.equal(formatAge(86400000), "1d 00h");
  assert.equal(clockUTC(Date.UTC(2026, 0, 2, 3, 4, 5)), "03:04:05 UTC");
});

test("stream: a state from another Daemon run reloads the page; the same run does not", () => {
  const source = {};
  const state = (run) =>
    JSON.stringify({ run_id: run, agents: fixture.input.map((i) => i.view), connection: fixtureConnection, headers: fixture.headers });
  let { st } = streamDecision(streamState(), { kind: "open", source });
  let d = streamDecision(st, { kind: "event", source, name: "state", data: state("run-a") });
  assert.equal(d.reload, undefined, "the first state");
  st = d.st;
  ({ st } = streamDecision(st, { kind: "open", source }));
  d = streamDecision(st, { kind: "event", source, name: "state", data: state("run-a") });
  assert.equal(d.reload, undefined, "a reconnect to the same run");
  st = d.st;
  ({ st } = streamDecision(st, { kind: "open", source }));
  d = streamDecision(st, { kind: "event", source, name: "state", data: state("run-b") });
  assert.equal(d.reload, true, "a restarted Daemon");
});

test("state label: WORKING · N bg, BACKGROUND · N when N > 1 (ADR-0009)", () => {
  const working = { status: "working", attention_class: "working" };
  const bg = { status: "working", attention_class: "working", background: true };
  const done = { status: "done", attention_class: "finished", background: true };
  assert.equal(stateLabel({ ...working, background_tasks: 1 }), "Working · 1 bg");
  assert.equal(stateLabel({ ...working, background_tasks: 3 }), "Working · 3 bg");
  assert.equal(stateLabel({ ...working, background_tasks: 0 }), "Working");
  assert.equal(stateLabel(working), "Working");
  assert.equal(stateLabel({ ...bg, background_tasks: 1 }), "Background");
  assert.equal(stateLabel({ ...bg, background_tasks: 2 }), "Background · 2");
  assert.equal(stateLabel({ ...done, background_tasks: 4 }), "Background · 4");
  assert.equal(stateCount({ ...done, background_tasks: 1 }), "");
  // Read as sent: never a count from anything but a positive whole number.
  for (const n of [-1, 1.5, "2", null, undefined]) assert.equal(backgroundTasks({ background_tasks: n }), 0);
  // Other kinds show no count, whatever is sent.
  const others = [
    { status: "done", attention_class: "finished", background_tasks: 2 },
    { status: "idle", attention_class: "working", background_tasks: 2 },
    { status: "blocked", attention_class: "needs_you", background_tasks: 2 },
    { status: "unknown", attention_class: "unknown", background_tasks: 2 },
  ];
  for (const v of others) assert.equal(stateCount(v), "", JSON.stringify(v));
});

test("accessible name says the background tasks", () => {
  const now = 1_000_000;
  const v = {
    key: { host: "h", server: "default", epoch: 1, pane_id: "w1:p1" },
    attention_class: "working",
    status: "working",
    background: false,
    background_tasks: 2,
    heading: "cldw",
    context_line: "build",
    directory: "~/src/p",
    workspace: "ops",
    tab: "1",
    pane_position: "",
    waiting_since: now - 75_000,
    waiting_exact: true,
  };
  assert.equal(boxLabel(v, now), "ops › 1, build, ~/src/p, cldw, Working, 2 background tasks, waiting 1m");
  assert.equal(tasksLabel({ background_tasks: 1 }), "1 background task");
  assert.equal(tasksLabel({}), "");
  // Group counts are unchanged by the count.
  assert.equal(groupCounts([v, { ...v, background: true }]), "1 background · 1 working");
});

test("golden fixture: background_tasks as the Daemon sent it", () => {
  const w = fixture.input.find((i) => i.view.status === "working" && i.view.background_tasks > 0 && !i.view.background);
  assert.ok(w, "no Working view with background tasks in the fixture");
  assert.equal(stateLabel(w.view), "Working · " + w.view.background_tasks + " bg");
  for (const { view: v } of fixture.input) {
    assert.ok(Number.isInteger(v.background_tasks), keyString(v));
    if (v.attention_class === "needs_you" || v.attention_class === "unknown") assert.equal(v.background_tasks, 0);
  }
});

test("recap: the Box's context text and accessible name use it in full (ADR-0010)", () => {
  const now = 1_000_000;
  const recap = "I wrote the store tests and they pass. Next I'll open the pull request, then wait for review of every change.";
  const v = {
    key: { host: "h", server: "default", epoch: 1, pane_id: "w1:p1" },
    attention_class: "finished",
    status: "done",
    background: false,
    background_tasks: 0,
    heading: "cldw",
    context_line: "write-tests",
    recap,
    directory: "~/src/p",
    workspace: "ops",
    tab: "1",
    pane_position: "",
    waiting_since: now - 75_000,
    waiting_exact: true,
  };
  assert.equal(recapOf(v), recap);
  assert.equal(contextText(v), recap);
  assert.equal(boxLabel(v, now), "ops › 1, " + recap + ", ~/src/p, cldw, Finished, waiting 1m");
  assert.ok(!boxLabel(v, now).includes("write-tests"));
  // No recap, an empty one, or one of another type: the Context Line, as before.
  for (const r of [undefined, "", null, 5]) {
    const w = { ...v, recap: r };
    assert.equal(recapOf(w), "");
    assert.equal(contextText(w), "write-tests");
    assert.equal(boxLabel(w, now), "ops › 1, write-tests, ~/src/p, cldw, Finished, waiting 1m");
  }
  assert.equal(contextText({ ...v, recap: "", context_line: "" }), "");
  // Class, kind and group counts are unchanged by a recap.
  assert.equal(boxKind(v), boxKind({ ...v, recap: "" }));
  assert.equal(groupCounts([v]), groupCounts([{ ...v, recap: "" }]));
});

test("anyRecap: true while any view has a recap", () => {
  assert.equal(anyRecap([]), false);
  assert.equal(anyRecap([{ recap: "" }, { context_line: "x" }]), false);
  assert.equal(anyRecap([{ recap: "" }, { recap: "A. Next B." }]), true);
});

test("golden fixture: recap as the Daemon sent it", () => {
  const withRecap = fixture.input.filter((i) => i.view.recap !== "");
  assert.ok(withRecap.length > 0, "no view with a recap in the fixture");
  for (const { view: v } of fixture.input) assert.equal(typeof v.recap, "string", keyString(v));
  for (const i of withRecap) {
    assert.equal(i.view.recap, i.recap, keyString(i.view));
    assert.equal(contextText(i.view), i.recap);
  }
  assert.equal(anyRecap(fixture.input.map((i) => i.view)), true);
});

// The title bar's summary (ADR-0013, Story 5.19) restates the Needs you count
// and herdr's connection.
test("titleBar: the Needs you count, 0 while the Boxes are hidden", () => {
  const conn = { connected: true, gate: "ok", herdr_version: "0.9.5", herdr_minimum: "0.9.5" };
  const v = (c) => ({ attention_class: c });
  const at = (views, c, lost) => titleBar(views, c, lost, boardStatus(c, lost, 0));
  assert.deepEqual(at([], conn, false), { needs: 0, needsText: "Nothing needs you", link: "ok", linkText: "herdr 0.9.5" });
  assert.equal(at([v("needs_you"), v("working")], conn, false).needsText, "1 needs you");
  assert.equal(at([v("needs_you"), v("needs_you")], conn, false).needsText, "2 need you");
  const refused = { ...conn, herdr_version: "0.8.2", gate: "refused" };
  assert.deepEqual(at([v("needs_you")], refused, false), { needs: 0, needsText: "Nothing needs you", link: "bad", linkText: "herdr 0.8.2" });
});

test("titleBar: herdr's connection, in the strips' order of precedence", () => {
  const conn = { connected: true, gate: "ok", herdr_version: "0.9.5", herdr_minimum: "0.9.5" };
  const at = (c, lost) => titleBar([], c, lost, boardStatus(c, lost, 0));
  assert.deepEqual([at({ ...conn, gate: "advisory", herdr_version: "0.9.2" }, false).link, at({ ...conn, gate: "advisory", herdr_version: "0.9.2" }, false).linkText], ["warn", "herdr 0.9.2"]);
  assert.deepEqual([at({ ...conn, connected: false }, false).link, at({ ...conn, connected: false }, false).linkText], ["down", "herdr disconnected"]);
  assert.deepEqual([at({ ...conn, connected: false }, true).link, at({ ...conn, connected: false }, true).linkText], ["lost", "Reconnecting…"]);
  assert.deepEqual([at(null, false).link, at(null, false).linkText], ["none", "herdr"]);
  assert.deepEqual([at({ ...conn, herdr_version: "" }, false).linkText], ["herdr"]);
});

// ---- Acknowledgement (ADR-0014, Story 6.4) ----

const ackView = (pane, tab, extra = {}) => ({
  key: { host: "h", server: "s", epoch: 3, pane_id: pane },
  tab,
  workspace: "probe",
  status: "done",
  attention_class: "finished",
  waiting_since: 1000 + pane.length,
  seen: false,
  ...extra,
});

test("seen: the Daemon's field, drawn as its own kind, after Background", () => {
  const f = ackView("w1:p1", "api");
  const s = { ...f, seen: true };
  assert.equal(isSeen(s), true);
  assert.equal(isSeen(f), false);
  assert.equal(isSeen({ ...f, seen: "true" }), false, "only a true boolean is seen");
  assert.equal(boxKind(f), "finished");
  assert.equal(boxKind(s), "seen");
  assert.equal(boxKind({ ...s, status: "idle" }), "seen", "done to idle within the finish keeps it");
  // A seen Agent whose background work opens is Background, as the Daemon ranks it 3.
  assert.equal(boxKind({ ...s, background: true }), "background");
  assert.equal(classLabel({ ...s, background: true }), BACKGROUND_LABEL);
  assert.equal(SEEN_LABEL, "Seen");
  assert.equal(classLabel(s), "Seen");
  assert.equal(classLabel(f), "Finished");
  // A view without the field (an older Daemon) is never seen.
  const { seen, ...noField } = f;
  assert.equal(boxKind(noField), "finished");
});

test("seen box label says Seen, not Finished", () => {
  const s = { ...ackView("w1:p1", "api"), seen: true, heading: "claude", directory: "/srv" };
  const label = boxLabel(s, 1000 + 60000);
  assert.match(label, /, Seen, /);
  assert.doesNotMatch(label, /Finished/);
});

test("group counts: seen after idle, not counted finished", () => {
  const w = { status: "working", attention_class: "working" };
  const f = { status: "done", attention_class: "finished" };
  const i = { status: "idle", attention_class: "working" };
  const s = { status: "done", attention_class: "finished", seen: true };
  const b = { status: "done", attention_class: "finished", background: true };
  assert.equal(groupCounts([w, f, i, i, i, s, s, b, w]), "1 finished · 3 idle · 2 seen · 1 background · 2 working");
  assert.equal(groupCounts([s]), "1 seen");
  // A seen Background Agent counts once, as Background.
  assert.equal(groupCounts([{ ...s, background: true }]), "1 background");
});

test("group counts: marking one Finished view drops one finished and adds 1 seen", () => {
  const views = [ackView("w1:p1", "a"), ackView("w1:p2", "b"), { status: "idle", attention_class: "working" }];
  assert.equal(groupCounts(views), "2 finished · 1 idle");
  const after = [views[0], { ...views[1], seen: true }, views[2]];
  assert.equal(groupCounts(after), "1 finished · 1 idle · 1 seen");
});

test("unseenFinished: the Finished Boxes only", () => {
  const f1 = ackView("w1:p1", "a");
  const f2 = ackView("w1:p2", "b");
  const s = { ...ackView("w1:p3", "c"), seen: true };
  const bg = { ...ackView("w1:p4", "d"), background: true };
  const n = { ...ackView("w1:p5", "e"), status: "blocked", attention_class: "needs_you" };
  const i = { ...ackView("w1:p6", "f"), status: "idle", attention_class: "working" };
  const w = { ...ackView("w1:p7", "g"), status: "working", attention_class: "working" };
  assert.deepEqual(unseenFinished([f1, s, bg, n, i, w, f2]), [f1, f2]);
  assert.deepEqual(unseenFinished([s, bg]), []);
  assert.deepEqual(unseenFinished([]), []);
});

test("ackBody: each view's key and waiting_since, in order, nothing else", () => {
  const a = { ...ackView("w1:p1", "a"), waiting_since: 111, sort_key: [1, 1, 1, -111, "x"] };
  const b = { ...ackView("w1:p2", "b"), waiting_since: 222 };
  assert.deepEqual(ackBody([a, b]), {
    agents: [
      { key: { host: "h", server: "s", epoch: 3, pane_id: "w1:p1" }, waiting_since: 111 },
      { key: { host: "h", server: "s", epoch: 3, pane_id: "w1:p2" }, waiting_since: 222 },
    ],
  });
  assert.deepEqual(ackBody([]), { agents: [] });
});

test("ackPath: the per-Agent route, each segment escaped", () => {
  assert.equal(ackPath({ host: "h", server: "s", epoch: 3, pane_id: "w1:p1" }, true), "/api/agents/h/s/3/w1%3Ap1/seen");
  assert.equal(ackPath({ host: "a/b", server: "s v", epoch: 10, pane_id: "p" }, false), "/api/agents/a%2Fb/s%20v/10/p/unseen");
});

test("ackToast: one tab by name, several by count, undo says not seen", () => {
  const a = ackView("w1:p1", "2: api");
  const b = ackView("w1:p2", "3: docs");
  assert.equal(ackToast([a], true), "Marked 2: api seen");
  assert.equal(ackToast([a, b], true), "Marked 2 Agents seen");
  assert.equal(ackToast([a], false), "Marked 2: api not seen");
  assert.equal(ackToast([a, b], false), "Marked 2 Agents not seen");
  // An empty tab falls back to the workspace, as the Box's first line does.
  assert.equal(ackToast([{ ...a, tab: "" }], true), "Marked probe seen");
});

test("ackError: 409 stale, 503 not recording, anything else a plain failure", () => {
  const table = [
    [409, "stale_view", ACK_STALE],
    [409, "not_finished", ACK_STALE],
    [503, "recording_off", ACK_OFF],
    [404, "unknown_agent", ACK_FAILED],
    [403, "origin_not_allowed", ACK_FAILED],
    [500, "store_failed", ACK_FAILED],
    [0, "", ACK_FAILED],
  ];
  for (const [status, code, want] of table) assert.equal(ackError(status, code), want, status + " " + code);
  assert.equal(ACK_STALE, "It changed before it was marked; nothing was marked");
  assert.equal(ACK_OFF, "herddash is not recording; nothing was marked");
  assert.equal(ACK_FAILED, "Could not mark it seen");
  assert.equal(ACK_TOAST_MS, 6000);
});

test("ackError: a refused Undo says the mark stands, never that nothing was marked", () => {
  const table = [
    [409, "stale_view", UNDO_STALE],
    [503, "recording_off", UNDO_OFF],
    [404, "unknown_agent", UNDO_FAILED],
    [0, "", UNDO_FAILED],
  ];
  for (const [status, code, want] of table) assert.equal(ackError(status, code, false, true), want, status + " " + code);
  for (const t of [UNDO_STALE, UNDO_OFF, UNDO_FAILED]) {
    assert.ok(!t.includes("nothing was marked"), t);
    assert.ok(t.endsWith("it is still marked seen"), t);
  }
});

test("ackError: a refused Mark unseen reads as a mark's refusal, else could not mark it not seen", () => {
  const table = [
    [409, "stale_view", ACK_STALE],
    [409, "not_finished", ACK_STALE],
    [503, "recording_off", ACK_OFF],
    [404, "unknown_agent", UNSEEN_FAILED],
    [403, "origin_not_allowed", UNSEEN_FAILED],
    [500, "store_failed", UNSEEN_FAILED],
    [0, "", UNSEEN_FAILED],
  ];
  for (const [status, code, want] of table) assert.equal(ackError(status, code, false), want, status + " " + code);
  assert.equal(ackError(404, "", false, false), UNSEEN_FAILED);
  assert.equal(UNSEEN_FAILED, "Could not mark it not seen");
});

test("ackError: a refused Undo of a Mark unseen says it is still not seen", () => {
  const table = [
    [409, "stale_view", REDO_STALE],
    [503, "recording_off", REDO_OFF],
    [404, "unknown_agent", REDO_FAILED],
    [0, "", REDO_FAILED],
  ];
  for (const [status, code, want] of table) assert.equal(ackError(status, code, true, true), want, status + " " + code);
  for (const t of [REDO_STALE, REDO_OFF, REDO_FAILED]) {
    assert.ok(!t.includes("nothing was marked"), t);
    assert.ok(t.endsWith("it is still not seen"), t);
  }
});

test("inViewTag: only a Finished Box whose pane herdr has focused", () => {
  const f = ackView("w1:p1", "1: api", { in_view: true });
  const table = [
    ["finished, in view", f, true],
    ["finished, not in view", { ...f, in_view: false }, false],
    ["finished, in_view absent", { ...f, in_view: undefined }, false],
    ["finished, in_view not a boolean", { ...f, in_view: "true" }, false],
    ["seen, in view", { ...f, seen: true }, false],
    ["background, in view", { ...f, background: true }, false],
    ["seen background, in view", { ...f, seen: true, background: true }, false],
    ["needs you, in view", { ...f, status: "blocked", attention_class: "needs_you", sort_key: [0, 0, 0, 0, ""] }, false],
    ["idle, in view", { ...f, status: "idle", attention_class: "working" }, false],
    ["working, in view", { ...f, status: "working", attention_class: "working" }, false],
    ["unknown, in view", { ...f, status: "unknown", attention_class: "unknown" }, false],
  ];
  for (const [name, v, want] of table) assert.equal(inViewTag(v), want, name);
  assert.equal(IN_VIEW_TEXT, "● in view");
  assert.equal(IN_VIEW_DOT + " " + IN_VIEW_WORDS, IN_VIEW_TEXT);
  assert.equal(IN_VIEW_WORDS, "in view");
  assert.equal(IN_VIEW_TITLE, "Focused in herdr now. Leaving the pane after 3 s marks it seen.");
});

test("boxLabel: a Box with the in-view tag says in view after its class, no other Box does", () => {
  const now = 1000 + 60_000;
  const f = { ...ackView("w1:p1", "1: api"), waiting_since: 1000, waiting_exact: true, heading: "cldw", directory: "~/src/p", in_view: true };
  assert.equal(boxLabel(f, now), "probe › 1: api, ~/src/p, cldw, Finished, in view, waiting 1m");
  assert.equal(boxLabel({ ...f, in_view: false }, now), "probe › 1: api, ~/src/p, cldw, Finished, waiting 1m");
  assert.equal(boxLabel({ ...f, seen: true }, now), "probe › 1: api, ~/src/p, cldw, Seen, waiting 1m");
  assert.equal(boxLabel({ ...f, background: true }, now), "probe › 1: api, ~/src/p, cldw, Background, waiting 1m");
  assert.equal(boxLabel({ ...f, status: "idle", attention_class: "working" }, now), "probe › 1: api, ~/src/p, cldw, Idle, waiting 1m");
  assert.equal(
    boxLabel({ ...f, status: "blocked", attention_class: "needs_you", sort_key: [0, 0, 0, 0, ""] }, now),
    "probe › 1: api, ~/src/p, cldw, Needs you, waiting 1m",
  );
});

test("ackLabel and unackLabel: the pills' accessible names, by the Box's first line", () => {
  const v = ackView("w1:p1", "2: api");
  assert.equal(ackLabel(v), "Mark 2: api seen");
  assert.equal(unackLabel(v), "Mark 2: api not seen");
  // An empty tab falls back to the workspace, as the Box's first line does.
  assert.equal(unackLabel({ ...v, tab: "" }), "Mark probe not seen");
});

test("layout: a seen view sits after its group's Finished views, by the Daemon's rank 2", () => {
  const mk = (pane, rank, start, extra) => ({ ...view([1, 5, rank, start], "h", "s", 1, pane), workspace: "probe", ...extra });
  const f1 = mk("p1", 1, -300, { status: "done", attention_class: "finished" });
  const f2 = mk("p2", 1, -200, { status: "done", attention_class: "finished" });
  const s = mk("p3", 2, -250, { status: "done", attention_class: "finished", seen: true });
  const i = mk("p4", 2, -100, { status: "idle", attention_class: "working" });
  const w = mk("p5", 4, -50, { status: "working", attention_class: "working" });
  const { groups } = layout([w, s, i, f2, f1]);
  assert.equal(groups.length, 1);
  assert.deepEqual(groups[0].views.map(boxKind), ["finished", "finished", "seen", "idle", "working"]);
  assert.equal(groups[0].counts, "2 finished · 1 idle · 1 seen · 1 working");
});
