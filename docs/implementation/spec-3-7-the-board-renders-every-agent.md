---
title: 'Story 3.7: The Board renders every Agent'
type: 'feature'
created: '2026-10-20'
status: 'done'
baseline_commit: '9ec89df2d08f75aadc9714ece0c6b1bd5ca6b697'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/docs/implementation/epic-3-context.md'
  - '{project-root}/docs/design/board-design.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** herddash serves an API but no Board. The operator cannot yet glance at which Agent
needs them and for how long (FR-1 to FR-3, AD-1, ADR-0003).

**Approach:** Serve the Board at `/` with its files under `/assets/`, embedded in the binary, built
from the design the operator approved on 2026-10-20 (`docs/design/board-design.md`,
`docs/design/board.html`). It opens `/events`, renders one Box per AgentView from the `state`
event, and keeps its ordering and formatting in a plain ES module that `node --test` checks in CI
against core's golden fixture.

## Boundaries & Constraints

**Always:**
- **Routes (ADR-0007):** `/` exactly serves the Board page; `/assets/<name>` serves the embedded
  files; any other path under `/assets/` is 404 in the JSON error shape. GET and HEAD only, same
  Host check and headers as the other routes. Files live in `internal/httpapi/web/` and are
  embedded with `go:embed` by `internal/httpapi`; test files are not embedded.
- **Board page:** the approved design's tokens, Box anatomy, class emphasis, workspace headers, zone
  separation, empty states, responsive behaviour and accessibility, in light and dark by system
  preference. System fonts only, no network beyond `/events`, no design-review toolbar, no strips
  (Story 3.9).
- **Data:** the page opens `EventSource('/events')` and renders from the `state` event's body
  (exactly `/api/state`'s shape). It renders exactly one Box per AgentView and nothing else. Other
  event names are left to Story 3.8.
- **A Box** shows the AgentView's `heading` verbatim (so `unresolved · claude` and distinct
  Profiles come from the Daemon), `directory` truncated from the start, `workspace › tab` without
  stray separators, the Attention Class as text, the Waiting Time ticking each second from
  `waiting_since` with `≥` when `waiting_exact` is false and a dash when unknown, and
  `context_line` on one line with an ellipsis, full text in `title` and in the Box's accessible
  name, its row kept when empty. Box height is fixed in CSS.
- **Ordering module** (`web/order.js`, an ES module with no DOM or browser globals): sorts
  AgentViews by `sort_key` elements 0-3 numerically, then by `key` host, server, epoch as a
  number, pane id, exactly as `internal/core/view.go` `SortKey.compare`; computes workspace
  headers as `core.WorkspaceHeaders` does; formats durations as the design note defines
  (`0s`-`59s`, `1m`-`59m`, `1h 00m`-`23h 59m`, `1d 00h` on) and the Waiting Time label. The
  Board computes nothing else.
- **Tests:** `web/order.test.js` runs under `node --test` with no npm packages. It reads
  `internal/core/testdata/board-order.json`, sorts the fixture's expected views and checks the
  order and headers equal the fixture's; plus cases the fixture lacks: epoch 9 vs 10, a host that
  is a prefix of another, the duration boundaries, `≥`, unknown start. CI runs it.
- Go tests cover the routes: `/` and each asset with its content type, HEAD, 404 for unknown
  assets, 405 for other methods, the Host check.

**Never:**
- Handle `agent`, `removed`, `transition`, `resync` or `connection` events (Story 3.8), draw the
  strips (Story 3.9), poll, or fetch anything but `/events`.
- Add npm, a bundler, a build step, a dependency or a CDN. Change `/api/state` or `/events`.
- Re-derive class, zone, group or heading in JavaScript.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Page | GET `/` | 200, the Board HTML | none |
| Asset | GET `/assets/order.js` | 200, `text/javascript` | none |
| Unknown asset | GET `/assets/nope.js` | 404 JSON error | none |
| Not root | GET `/index.html` | 404 JSON error | none |
| Method | POST `/` | 405 with `Allow` | none |
| Golden order | board-order.json views | order and headers equal the fixture's | test fails |
| Epoch tie-break | equal sort_key[0..3], epochs 9 and 10 | epoch 9 first | test fails |
| Durations | 59s, 60s, 3599s, 3600s, 86399s, 86400s | `59s`, `1m`, `59m`, `1h 00m`, `23h 59m`, `1d 00h` | test fails |
| Lower bound | `waiting_exact` false | label starts `≥ ` | test fails |
| Unknown start | no `waiting_since` | a dash | test fails |
| State render | a `state` event with N AgentViews | N Boxes in module order, headers where it says | none |

</frozen-after-approval>

## Code Map

- `internal/httpapi/httpapi.go` -- `ServeHTTP` route switch (`prefixAgents`, `pathState`, `pathEvents`, default 404), `writeError`, `hostAllowed`, `allowGetHead`; add `/` and `/assets/` here
- `internal/httpapi/httpapi_test.go` -- route test style to follow
- `internal/core/view.go` -- `SortKey` (Zone, Group, ClassRank, Start, Key), `compare` at ~215, `MarshalJSON` (array of five, element 4 the key string), `WorkspaceHeaders`, `ShortenDir`
- `internal/core/testdata/board-order.json` -- golden fixture: `input[].view` (expected AgentViews), `order` (key strings), `headers`; unknown sentinel is 9007199254740991
- `docs/design/board.html` -- the approved page: reuse its CSS and render code, drop the toolbar, the embedded fixture and the strips
- `docs/design/board-design.md` -- the approved design note
- `.github/workflows/ci.yml` -- add a step running `node --version && node --test internal/httpapi/web/` (ubuntu runners have Node preinstalled; no new action)
- `arch_test.go` -- `internal/httpapi/web` holds no Go files, so the import graph is unchanged
- Local node: `~/.local/share/mise/installs/node/22/bin/node`; local Go: `~/.local/share/mise/installs/go/1.26.5/bin`

## Tasks & Acceptance

**Execution:**
- [x] `internal/httpapi/web/index.html`, `board.css`, `board.js` -- the Board page, styles and DOM rendering from `state`
- [x] `internal/httpapi/web/order.js`, `order.test.js` -- ordering, headers and formatting, and their node tests
- [x] `internal/httpapi/httpapi.go`, `httpapi_test.go` (or a new `board.go`/`board_test.go`) -- embed and serve `/` and `/assets/`
- [x] `.github/workflows/ci.yml` -- the node test step

**Acceptance Criteria:**
- Given the module, when `go test -race ./...` and `node --test internal/httpapi/web/` run, then every matrix row passes and arch_test passes.
- Given `herddash serve` against a fake herdr (never the live server), when `/` is opened, then the Board renders the current Agents in the approved design.

## Implementation Notes

- The node command is `node --test internal/httpapi/web/*.test.mjs`: Node 22 does not take a directory, so the directory form in this spec's Code Map and Verification fails. CI runs the glob with the TAP reporter, fails below Node 18 and fails if no test ran.
- The JavaScript files are `.mjs` (`order.mjs`, `order.test.mjs`, `board.mjs`) so any Node loads them as ES modules without a `package.json`.
- Board responses carry a Content-Security-Policy (`default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`); the page has no inline script or style.
- Zone and group layout, headers, ordering, labels and "never seen working" live in `order.mjs`; `board.mjs` only builds elements.
- Checked end to end: `herddash serve` against the fake herdr from `serve_test.go`, `/` loaded in headless Chrome; one Needs you Box rendered and ticked. Never the live herdr.
- Reconnecting after a 503 and keeping focus across re-renders are deferred to Story 3.8.

## Spec Change Log

## Review Triage Log

| # | Source | Finding | Verdict | Evidence / route |
|---|--------|---------|---------|------------------|
| 1 | BH, VG, ECH | `node --test internal/httpapi/web/` fails on Node 22; only the CI glob form passes | low | Reproduced: directory form reports `# fail 1`, glob form 16/16; the test file's header repeats it; patch the comment; the correct command is recorded in Implementation Notes (editing this spec's commands is out of a patch's reach) |
| 2 | VG, BH | `board.js` rendering (zone split, group slicing, Box count) runs in no test | medium | VG pre-verified: changing `headers[0]` or the group end passes every test; patch: move the layout arithmetic into the ordering module as a pure function and test it on the golden fixture, no top zone and no lower zone |
| 3 | VG, BH, ECH | `headerLabel` and its `(no workspace)` fallback untested | low | The fallback is the approved page's (`docs/design/board.html`); patch with a test |
| 4 | BH, ECH | A parsed `state` without an `agents` array (or `null`) wipes the Board or throws, silently | medium | `render` falls back to `[]`; patch: keep the last render unless `agents` is an array, and warn in the console |
| 5 | BH | "No network beyond `/events`" is enforced only by string greps | medium | Misses `//host`, CSS `url()`, `import()`, beacons; patch: a Content-Security-Policy on Board responses, asserted in a test |
| 6 | BH, ECH | A `state` re-render discards keyboard focus | low | Real; incremental updates are Story 3.8's, which can keep focus by key; defer |
| 7 | BH | `data-theme` CSS is dead without the review toolbar | low | Nothing sets it; direct deletion; patch |
| 8 | VG, ECH | `board.js` derives "never seen working" from status and class | low | Presentation of two AgentView fields, but untested and outside the module; patch: move it into the ordering module with a test |
| 9 | BH | `aria-label` on a plain span is not announced; every Box's label is rewritten each second | low | Patch: drop the span's label (the Box's carries it); keep the per-tick Box label |
| 10 | BH | CI prints the Node version but asserts nothing, and would pass with no test files | low | Patch with the `.mjs` rename (below): fail below Node 18 and when no test ran |
| 11 | BH | The directory test is vacuous; the embed test covers one file | low | Patch: assert a `~/` directory exists; walk `web/` so every non-test file is served and no test file is |
| 12 | BH | Leftovers: a broken doc-comment wrap, a duplicated `/assets/` 404 test, no query-string test | low | Direct; patch |
| 13 | ECH | After a 503 from `/events` (Daemon restarting) EventSource stops retrying, freezing the Board | medium | Real browser behaviour; reconnecting after a drop or Daemon restart is Story 3.8's acceptance criterion; defer |
| 14 | BH | Spec/tracker status mismatch; empty notes | false | Workflow ordering; notes are filled at finalize |
| 15 | own check | `order.js` is ESM without `package.json`; a Node 20 runner would load it as CommonJS | medium | Node 22 detects ESM, Node 20 does not; patch: `.mjs` files (served as `text/javascript`) |

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `node --test internal/httpapi/web/` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
- `grep -rE 'https?://' internal/httpapi/web/` -- expected: nothing
