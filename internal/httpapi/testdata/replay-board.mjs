// Replays a captured /events stream through the Board's own stream logic
// (Story 5.11), for engine_contract_test.go:
//
//   node internal/httpapi/testdata/replay-board.mjs <events.json>
//
// The file holds {events: [{name, id, data}]}, every event of one stream in
// order, keepalives included. Each goes through order.mjs's streamDecision on
// one source, so the sequence and rejection checks run exactly as on the
// Board. It prints one JSON object: seqs, for every view sequence an event
// carried, the model's agents (sorted by keyId) and connection after that
// sequence's last event; warnings, every warning streamDecision gave; and
// reopens, every reopen or reload it decided. Node built-ins only.

import { readFileSync } from "node:fs";
import { streamDecision, streamState } from "../web/order.mjs";

const { events } = JSON.parse(readFileSync(process.argv[2], "utf8"));
const source = {};
let now = 0;
let { st } = streamDecision(streamState(), { kind: "open", source, now });
({ st } = streamDecision(st, { kind: "connected", source, now }));

const seqs = {};
const warnings = [];
const reopens = [];
for (const [i, e] of events.entries()) {
  now += 1;
  const d = streamDecision(st, { kind: "event", source, name: e.name, data: e.data, id: e.id, now });
  if (d.warning) warnings.push("event " + i + " (" + e.name + " " + e.id + "): " + d.warning);
  if (d.reopenAfter !== undefined) reopens.push("event " + i + " (" + e.name + " " + e.id + "): reopen");
  if (d.reload) reopens.push("event " + i + " (" + e.name + " " + e.id + "): reload");
  st = d.st;
  if (e.id === "" || e.name === "keepalive") continue;
  // The model's map is keyed by keyId.
  const agents = [...st.model.agents.entries()].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)).map(([, v]) => v);
  seqs[e.id] = { agents, connection: st.model.connection };
}
if (st.awaitingState) warnings.push("the stream never delivered a valid state");
process.stdout.write(JSON.stringify({ seqs, warnings, reopens }) + "\n");
