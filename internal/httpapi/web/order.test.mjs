// Tests for order.mjs, run with `node --test internal/httpapi/web/*.test.mjs`
// and no npm packages. The golden order and headers come from core's fixture, which the
// Go tests in internal/core check against core.SortViews and
// core.WorkspaceHeaders, so the Board and the Daemon cannot drift apart.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import {
  boxLabel,
  compareViews,
  formatDuration,
  headerLabel,
  layout,
  neverWorking,
  sortViews,
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
    "Needs you, waiting at least 2m, unresolved · claude, a long context line that the Box truncates, ~/src/p, ops › 1",
  );
  assert.equal(
    boxLabel({ ...v, waiting_since: undefined, context_line: "", attention_class: "unknown" }, now),
    "Unknown, Waiting Time unknown, unresolved · claude, ~/src/p, ops › 1",
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
