# Epic 3 Context: The Board

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Put the product in front of the operator: one read-only page, open all day beside the terminal, with one Box per Agent and the Agent that has needed the operator longest at the top. The page is served on loopback from the engine's published view, and the same view feeds a read-only API and event stream that any later surface can use. The Board is what herddash is measured by, and its restraint is the differentiator. It has no terminal, no prompt input and no pane control. Stories 3.1 to 3.7 are done: core's `AgentView`, Context Line and sort key with its golden fixture; the engine's published view and change lists; `/api/state`; the `/events` SSE stream; per-Agent history; the operator-approved Board design under `docs/design/` (`board-design.md`, `board.html`, `board-fixture.json`); and the Board at `/`, built from `internal/httpapi/web/{order.mjs,board.mjs,board.css,index.html}` and tested by `node --test` in CI. What remains is keeping the Board live (3.8) and the connection and version strips (3.9).

## Stories

- Story 3.1: Each Agent's view: Context Line and order
- Story 3.2: The engine publishes views and change lists
- Story 3.3: Read-only API on loopback with current state
- Story 3.4: Live event stream
- Story 3.5: History per Agent
- Story 3.6: Board visual design
- Story 3.7: The Board renders every Agent
- Story 3.8: The Board stays live
- Story 3.9: Connection and version strips

## Requirements & Constraints

- **Context Line:** each Agent Program has its own rule, with one global default, which is the empty string. For Claude Code, a Pane Title that is a session name becomes the Context Line. A title of `Claude Code` (the placeholder) or an empty title gets the default, and so does any program with no rule. Adding a rule for a new program touches only `internal/core` and its documentation, never the Board or the API.
- **Order:** Needs you, then Finished, form a flat top zone across workspaces, each sorted longest-waiting first. Working and then Unknown follow, grouped by workspace in herdr's snapshot order.
  - A Transition into `blocked` moves the Box into Needs you at its longest-waiting position, so a newly blocked Agent sorts last there, not first (operator-confirmed). The sort key already does this; nothing special-cases `blocked`.
  - A Transition into `working` moves it into its workspace group.
  - `done` turning into `idle` keeps both the Box's place and its Waiting Time.
  - An `idle` Agent never seen `working` sits with Working.
- **Box contents:** a heading, the shortened project directory, the workspace and tab labels, the Attention Class, the Waiting Time and the Context Line.
  - The heading is `profile · program`, with the name shown once when they are equal.
  - The Waiting Time ticks, and shows `≥` when it is not exact.
  - The Context Line is one line, truncated with an ellipsis, with full text on hover. Its space stays reserved when it is empty.
  - Box height is fixed in CSS. An `unresolved` Profile is shown as such, never as a blank.
- **Restraint:** the Board carries only Boxes, workspace labels and the connection and version strips. Anything more needs an ADR superseding ADR-0003. Needs you must be visually dominant and readable from across a desk.
- **Live:** the Board opens `/events` first and fetches nothing else. A recorded Transition reaches an open Board within one second. Boxes are added, moved or removed without a reload. There is no polling, and the Board reconnects by itself after a stream drop or Daemon restart and re-renders from the new `state`.
- **Connection truth:** when herdr is disconnected, a thin strip shows the last snapshot time and Boxes are dimmed, never hidden.
  - With gate `refused`, an unsupported-version strip names `herdr_version` and `herdr_minimum`, and no Boxes are shown.
  - With gate `advisory`, a warning strip says resync is not guaranteed, and Boxes are shown.
- **Read-only surface:** there are no write routes, and no response triggers a herdr call. Requests are refused when their `Host` header is not `localhost`, `127.0.0.1` or `[::1]` (with any port), and no CORS headers are ever sent. The default port is loopback-only and is not 7317, and a non-loopback bind is refused at start.
- **History:** returns one Agent's Transitions within Retention, in order and with provenance, including the Transition that began its current class. An unknown key gets a 404.

## Technical Decisions

- **Core computes everything once.** Attention Class, Waiting Time and its exactness, Profile, Context Line and the sort key are core functions. They take state and an injected clock and produce one `AgentView`. `core` never reads the clock and imports only the standard library, and `arch_test.go` enforces both.
- **Sort key** `[zone, group, class_rank, start, agent_key]`, compared element by element:
  - In the top zone, `group` is 0.
  - In the lower zone, `group` is the workspace's position in herdr's snapshot (`PaneContext.WorkspacePosition`).
  - A golden fixture of the expected order and workspace-header positions is committed with Story 3.1. Later Board tests reuse it.
- **AgentView fields:** AgentKey, Profile, Agent Program, `heading`, the shortened directory, the workspace and tab labels, Status, Attention Class, `waiting_since`, `waiting_exact`, the Pane Title, the Context Line and the sort key.
- **JSON** fields are `snake_case`, and absent values are omitted, except an empty Context Line, which is sent as `""`. Times are UTC Unix ms. Class values are `needs_you`, `finished`, `working` and `unknown`. Errors use `{"error":{"code","message"}}` with a matching HTTP status.
- **ConnectionTruth** is `{connected, last_snapshot_at, herdr_version, herdr_minimum, gate: ok|advisory|refused, resync_guaranteed}`.
- **Published view:** the engine actor stays the single owner of state. After each change it publishes an immutable view (every AgentView, ConnectionTruth, a view sequence and a run id) together with the change list that produced it. Readers never lock engine state. The run id is new on every Daemon start, and the sequence restarts with it.
- **Routes:** GET `/` (root only), `/assets/`, `/api/state`, `/api/agents/{host}/{server}/{epoch}/{pane}/history` and `/events`. Other methods get 405 and unknown paths get 404, both in the JSON error shape. `httpapi` imports `core`, `engine` (for the published view) and `store`, and calls only `store`'s read functions, never `Append`.
- **SSE:**
  - The first event on every stream is `state`, with exactly the `/api/state` body plus the run id and view sequence.
  - Change events follow in publish order: `agent` (a full AgentView), `removed` (the AgentKey), `transition` (the full Transition and its AgentKey), `resync` (the marker) and `connection` (the full ConnectionTruth). `id:` is the view sequence.
  - Each stream has a bounded buffer. A slow client is disconnected and reconnects to a fresh `state`.
  - The server keeps no history and does not handle `Last-Event-ID`. The write deadline is cleared per stream.
  - WebSockets and fetch-then-subscribe are rejected.
- **Board code:** hand-written HTML, CSS and JS, embedded under `internal/httpapi/web`, with no npm and no build step. The ordering, formatting and event-handling logic is a plain ES module with no DOM access, tested with `node --test` (with no npm packages) against core's golden fixture. CI runs these with a node step (Node 18 or newer), added by Story 3.7. Beyond rendering, the Board does only three things: it ticks the clock from `waiting_since`, orders Boxes by sort key, and draws a workspace header where `group` changes in the lower zone.
- **Logging:** Pane Titles are logged only at debug level.

## UX & Interaction Patterns

There is no separate UX document. Story 3.6 is the design, produced against an anonymised golden fixture from a real `/api/state` response. The fixture covers every class, several workspaces, exact and lower-bound times, long and empty Context Lines, and `unresolved`. The design defines palette, type and spacing tokens, Box anatomy, per-class emphasis, the workspace header and both strips, in light and dark. It is delivered as a short note plus a static HTML page. The Box layout is a responsive grid of cards. The Board is glanced at, never operated.

## Cross-Story Dependencies

- 3.1 to 3.7 are done, and the 3.6 design approval is recorded. 3.8 builds on 3.4's stream and 3.7's Board modules. 3.9 needs 3.2's ConnectionTruth and 3.7.
- 3.8's one-second budget is checked in a Go test of the stream. The Board's event handling is tested in the same ES module tests as 3.7's ordering.
- **Live herdr:** the live server runs 0.9.5 since 2026-10-20, so a real `/api/state` shows gate `ok`. Exercise `advisory` and `refused` (Story 3.9) with fixtures, never by touching the live server.
