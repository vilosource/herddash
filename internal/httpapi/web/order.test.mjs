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
  applyEvent,
  boardStatus,
  boxLabel,
  clockUTC,
  compareViews,
  emptyModel,
  formatAge,
  formatDuration,
  headerLabel,
  keyId,
  layout,
  modelViews,
  neverWorking,
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
    heading: "unresolved · claude",
    context_line: "a long context line that the Box truncates",
    directory: "~/src/p",
    workspace: "ops",
    tab: "1",
    waiting_since: now - 120_000,
    waiting_exact: false,
  };
  assert.equal(
    boxLabel(v, now),
    "Needs you, waiting at least 2m, ops › 1, a long context line that the Box truncates, ~/src/p, unresolved · claude",
  );
  assert.equal(
    boxLabel({ ...v, waiting_since: undefined, context_line: "", attention_class: "unknown" }, now),
    "Unknown, Waiting Time unknown, ops › 1, ~/src/p, unresolved · claude",
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

test("header label: the workspace, or (no workspace)", () => {
  assert.equal(headerLabel({ workspace: "ops" }), "ops");
  assert.equal(headerLabel({ workspace: "" }), "(no workspace)");
  assert.equal(headerLabel({}), "(no workspace)");
});

test("never working: idle with class working only", () => {
  assert.equal(neverWorking({ status: "idle", attention_class: "working" }), true);
  assert.equal(neverWorking({ status: "working", attention_class: "working" }), false);
  assert.equal(neverWorking({ status: "idle", attention_class: "finished" }), false);
  assert.equal(neverWorking({ status: "unknown", attention_class: "unknown" }), false);
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
  const f = view([0, 0, 1, 1], "h", "s", 1, "f");
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
  const v = changed(base, { key: { ...base.key, pane_id: "w2:p9" } }, [1, 2, 2, 45]);
  v.sort_key[4] = "mac/default/1/w2:p9";
  const { model: next, warning } = applyEvent(model, "agent", JSON.stringify(v));
  assert.equal(warning, undefined);
  assert.equal(next.agents.size, model.agents.size + 1);
  const order = boardOrder(next);
  assert.equal(order.indexOf("mac/default/1/w2:p9"), order.indexOf("mac/default/1/w2:p3") + 1);
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
  const v = changed(fixtureView("w1:p1"), { status: "working", attention_class: "working", waiting_since: 200, waiting_exact: true }, [1, 1, 2, 200]);
  const { model: next } = applyEvent(model, "transition", transitionData(v, "blocked"));
  const { top, groups } = layout(modelViews(next));
  assert.ok(!top.some((x) => keyString(x) === "mac/default/1/w1:p1"));
  const g = groups.find((g) => g.views.some((x) => keyString(x) === "mac/default/1/w1:p1"));
  assert.equal(g.label, "herddash");
  assert.deepEqual(g.views.map(keyString), ["mac/default/1/w1:p3", "mac/default/1/w1:p5", "mac/default/1/w1:p1", "mac/default/1/w1:p4"]);
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
  assert.deepEqual([...EVENT_NAMES].sort(), ["agent", "connection", "removed", "resync", "state", "transition"]);
  for (const name of HANDLED_EVENTS) assert.ok(EVENT_NAMES.includes(name), name);
  // A name is handled exactly when applyEvent looks at its payload.
  for (const name of EVENT_NAMES) {
    const r = applyEvent(emptyModel(), name, "{bad");
    assert.equal(typeof r.warning === "string", HANDLED_EVENTS.includes(name), name);
  }
});

const stateData = () =>
  JSON.stringify({ agents: fixture.input.map((i) => i.view), connection: fixtureConnection, headers: fixture.headers });

// opened returns the stream state with source current and its state applied.
function opened(source) {
  let { st } = streamDecision(streamState(), { kind: "open", source });
  ({ st } = streamDecision(st, { kind: "event", source, name: "state", data: stateData() }));
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

test("stream: a malformed state neither resets the delay nor replaces the model", () => {
  let st = opened({});
  const s = {};
  ({ st } = streamDecision(st, { kind: "open", source: s }));
  st = streamDecision(st, { kind: "error", source: s, closed: true, now: 1 }).st;
  const b = {};
  ({ st } = streamDecision(st, { kind: "open", source: b }));
  const d = streamDecision(st, { kind: "event", source: b, name: "state", data: "{bad" });
  assert.equal(typeof d.warning, "string");
  assert.equal(d.st.delay, RECONNECT_START_MS * 2);
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
  assert.equal(bad.st, st);
  assert.equal(typeof bad.warning, "string");
  const resync = streamDecision(st, { kind: "event", source: a, name: "resync", data: '{"reason":"x"}' });
  assert.equal(resync.st, st);
  const conn = { ...fixtureConnection, connected: false };
  const c = streamDecision(st, { kind: "event", source: a, name: "connection", data: JSON.stringify(conn) });
  assert.deepEqual(c.st.model.connection, conn);
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

test("strips: an unknown herdr version reads as such", () => {
  const c = { ...okConn, gate: "advisory" };
  delete c.herdr_version;
  assert.match(stripText(boardStatus(c, false, NOW).strips[0]), /^herdr \(version unknown\) is below the supported minimum 0\.9\.5;/);
  const r = { ...okConn, gate: "refused", herdr_minimum: "" };
  delete r.herdr_version;
  assert.equal(
    stripText(boardStatus(r, false, NOW).strips[0]),
    "herdr (version unknown) is not supported. herddash needs (version unknown) or newer and is recording nothing.",
  );
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
