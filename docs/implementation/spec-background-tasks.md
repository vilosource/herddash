---
title: 'Board: tell an Agent waiting on background work apart from a Finished one'
type: 'feature'
created: '2026-10-21'
status: 'in-progress'
baseline_commit: '1cd38439ac073f3fa4893198e5fb6560a1c802df'
route: 'dispatch'
review_loop_iteration: 1
context:
  - '{project-root}/docs/design/board-design.md'
  - '{project-root}/docs/implementation/spec-board-workspace-groups.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The operator asked (2026-10-21): "as a user I need to be able to differentiate
between an agent waiting for a subagent or background task vs really Finished." Today a Claude
Code Agent that ends its turn with a background shell still running (its own, or one a subagent
started) is reported by herdr as `done`, then `idle`, and the Board shows it Finished.

**Evidence (2026-10-21, on this session's own pane `wA:p1Z`, logged every 2 s from herddash's
API and `ps`):**
- A background `sleep 75`: herdr reported `done` from the moment the turn ended until the task
  finished and Claude Code woke, 70 s later. The pane title did not change.
- A background subagent waiting on its own background shells: `done` while nothing was thinking;
  `working` again as soon as the subagent ran, though the main turn had ended. So a subagent that
  is *running* already reads Working; the gap is background shells.
- Claude Code runs every Bash tool call, foreground or background, main or subagent, as a direct
  child of its own process: `/bin/zsh -c source <config dir>/shell-snapshots/snapshot-zsh-….sh …`,
  in **its own process group** (pgid = its pid). Between commands there is none. MCP servers,
  gopls and `caffeinate` are children too, in Claude Code's process group, with other command
  lines.
- herdr's `pane.process_info` lists only the foreground process group (checked on an isolated
  herdr 0.9.5), so it does not show those shells: herddash must read the children itself.

**Approach:** A Claude Code Agent whose Status is `done` or `idle` and whose process has a live
tool-shell child is waiting on background work. The Daemon finds that by reading the Agent
process's direct children (local process reads, never herdr and never pane output), and the Board
shows such an Agent as **Background**, a separate state ranked right after Working.

## Boundaries & Constraints

**Always:**
- **Process reads (`internal/procenv`):** add `Children(pid int) ([]Process, error)` returning the
  direct children of pid with their argv (`Process{PID int; Argv []string}`): darwin through
  `kern.proc.all` (golang.org/x/sys/unix, already a dependency) for the parent pid and
  `kern.procargs2` for argv (reuse the existing parser; Apple-signed binaries such as /bin/zsh
  still return argv); linux by scanning `/proc/<pid>/stat` (ppid, field 4, parsed after the last
  `)`) and `/proc/<pid>/cmdline`; other OSes `nil, ErrUnsupported`-style error as `lookup` does.
  A child that exits mid-read is skipped, not an error. No new dependency.
- **Rule (core, AD-1):** `core.BackgroundRule` per Agent Program, built in like the Context Line
  rules: for `claude`, any child whose argv contains an element with
  `/shell-snapshots/snapshot-` is a background task. `core.HasBackground(program, children
  [][]string) bool`. Other programs: never.
- **When it applies (core):** only while the Status is `done` or `idle` (class Finished, or Idle
  as the Board ranks it). A `working`, `blocked` or `unknown` Agent is never Background: in
  `working` the shell may be a foreground tool call.
- **View (core):** `AgentViewInput.Background bool`; `AgentView.Background bool`
  (`json:"background"`), true only under the rule above. The Attention Class, the Fold, Waiting
  Time and the record are **unchanged** (no stored schema change): Waiting Time keeps counting
  from when the class began, i.e. when the turn ended. `ClassRank` becomes needs_you 0, Working
  1, **Background 2**, Finished 3, Idle 4, Unknown 5; zone and group as now. Regenerate the golden
  fixture and keep the node tests in step.
- **Engine:** remember each pane's Agent pid from `readProfile`'s foreground process (and clear it
  when the pane goes or its program changes). While a `claude` pane's Status is `done` or `idle`
  and its pid is known, check its children `backgroundCheckDelay` (1 s) after the Status changes
  and then every `backgroundInterval` (5 s) until the Status leaves `done`/`idle`; the reads run
  off the actor like `readProfile` (bounded by a timeout, abandoned when late) and the result is
  sent back as a message, which republishes the view only when `Background` changes. Tunables
  live in `tunables.go`. Failures (process gone, unsupported OS) mean not Background, logged at
  debug level once per pane, never as a warning.
- **Board:** `background: true` draws kind `background`: label `BACKGROUND`, a 6px **dashed**
  leading edge in `--working-edge`, the label in `--working-accent`, flush with the page like
  Working; Waiting Time as Finished's (full ink). `boxKind`/`classLabel` read the field (no
  derivation in JS). Group counts gain `n background` between working and finished, zeros left
  out. Accessible name and hover title say "Background".
- **Docs:** PRD FR-2 gets a dated amendment (operator request 2026-10-21, quoted above);
  `docs/design/board-design.md` (Box, order, emphasis, counts); the socket API record's "Other
  observations" gets the isolated `process_info` finding (foreground process group only).

**Never:**
- Read pane output, focus panes, call herdr for this, or install hooks or anything into Claude
  Code or herdr.
- Mark Working, Needs you or Unknown Agents Background, or change the record.
- Treat MCP servers, `caffeinate` or any non-shell-snapshot child as a background task.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Background shell | claude, Status done, child argv has `/shell-snapshots/snapshot-` | `background: true`, rank 2, Board `BACKGROUND` | test fails |
| Really finished | claude, Status idle, children only MCP/gopls/caffeinate | `background: false`, rank 3 | test fails |
| Working with a shell | claude, Status working, shell child | `background: false`, rank 1 | test fails |
| Idle never worked, shell | class working, Status idle, shell child | `background: true`, rank 2 | test fails |
| Other program | codex, Status done, shell-snapshot child | `background: false` | test fails |
| Shell ends | next check finds none | view republished with `background: false` | test fails |
| Process gone | Children errors | not Background, no warning | test fails |
| Status leaves done/idle | working Transition | checks stop; `background: false` | test fails |
| linux children | fake /proc tree in a temp dir | right pids and argv | test fails |

</frozen-after-approval>

## Code Map

- `internal/procenv` (`procenv.go`, `_darwin.go`, `_linux.go`, `_other.go`, tests) -- `Children`
- `internal/core/profile.go` or a new `internal/core/background.go` -- the rule; `view.go` --
  `AgentViewInput.Background`, `AgentView.Background`, `newSortKey` ranks; golden fixture
- `internal/engine/engine.go` (`resolve`, `readProfile` ~1431, pane struct, view build),
  `tunables.go`, engine tests with an injected children func like `e.environ`
- `internal/httpapi/web/order.mjs` (`boxKind`, `classLabel`, `groupCounts`, `CLASS_LABEL`),
  `order.test.mjs`, `board.css`
- `docs/prd.md` FR-2, `docs/design/board-design.md`, `docs/research/herdr-socket-api.md`
- Toolchain: Go `~/.local/share/mise/installs/go/1.26.5/bin`, Node `~/.local/share/mise/installs/node/22/bin`

## Tasks & Acceptance

**Execution:**
- [x] `internal/procenv` -- `Children` for darwin and linux, tests (darwin: spawn a child of the test process and find it; linux: a fake /proc root)
- [x] `internal/core` -- rule, view field, ranks, fixture, tests
- [x] `internal/engine` -- pid tracking, checks, tunables, tests with an injected children func
- [x] `internal/httpapi/web` -- kind, label, CSS, counts, node tests
- [x] docs

**Acceptance Criteria:**
- Given the module, when `go build ./... && go vet ./... && go test -race ./...`, `gofmt -l .` and `node --test internal/httpapi/web/*.test.mjs` run, then all pass, `arch_test` included, and every matrix row has a test.
- Given a dev build installed locally, when this Claude Code session ends its turn with a background `sleep` running, then its Box reads BACKGROUND until the sleep ends.

## Implementation Notes

- **procenv.** `Children(pid)` returns `[]Process{PID, Argv}` in pid order. darwin filters
  `kern.proc.all` by `Eproc.Ppid` and reads each child's argv through `kern.procargs2`, through a
  new `splitProcargs2` that `parseProcargs2` now shares (its behaviour is unchanged). linux is
  `childrenIn("/proc", pid)`, kept in the untagged `procenv.go` so its fake-/proc test runs on
  every OS; the parent pid is the second field after the last `)` of `stat`. A child whose stat,
  cmdline or procargs2 cannot be read is skipped; a zombie's empty cmdline gives a child with no
  argv. Other OSes wrap `ErrUnsupported`. The darwin test starts `sleep 30` and finds it.
- **core.** `background.go` holds the rule table (`claude`: any argv element containing
  `/shell-snapshots/snapshot-`), `HasBackground` and `ChecksBackground(program, status)` (a rule
  exists and the Status is `done` or `idle`), so the engine asks core whether to read at all.
  `NewAgentView` shows `Background` only when `ChecksBackground` holds **and** the class is
  finished or working: a fold whose class is needs_you while the Status reads `done` is never
  Background (the spec's Never list). Ranks are Working 1, Background 2, Finished 3, Idle 4,
  Unknown 5. The golden fixture gained an input `background` field and three inputs (a Background
  Finished in `herddash`, a Background never-worked Idle in `api`, and a codex input marked
  background that stays Finished); its views, order and headers were regenerated from core's own
  output, and `TestGoldenBoard` now also checks the fixture covers all six ranks.
- **engine.** `Options.Children` (default `procenv.Children`). The pid comes from
  `readProfile`'s foreground process (`profileMsg.pid`, also kept when the environment read
  fails); `settleProfile` stores it, `setProgram` clears it, `remove` stops the watch. One watch
  per pane, tagged by a generation and cancelled by its own context: `followBackground` restarts
  it on every Status Transition, first report and pid change, reading after
  `backgroundCheckDelay` and then every `backgroundInterval`; each read runs off the actor
  bounded by `backgroundTimeout` (2 s, a third tunable) and is abandoned when late. The verdict
  arrives as `backgroundMsg`; `publish`'s existing `noteView` comparison republishes only when the
  view changes. Leaving `done`/`idle` stops the watch and clears the verdict before the
  Transition's view is built, so the Transition itself carries `background: false`. A failed read
  logs `background not read` at debug once per pane. Engine tests default `Children` to an empty
  fake so no test reads a real process.
- **Board.** `isBackground` reads `view.background === true` only; `boxKind`/`classLabel` check it
  before Idle. `.box--background` is a 6px dashed `--working-edge` with the label in
  `--working-accent`; the Waiting Time keeps the default full ink. `board.mjs` needed no change:
  the hover title and accessible name come from `boxLabel`, which uses `classLabel`. The hand-run
  browser check (`check/board-browser.mjs`) was not extended or run.
- **Docs.** PRD FR-2 amendment with the operator's quote and three consequences; board design
  (Box row 4, order, emphasis, group counts); socket API record's "Other observations" gained the
  `process_info` foreground-group finding, attributed to this spec's isolated check (not re-run).
- **Notable.** Background needs the Agent's pid, which only a Profile resolution reads, so a
  `claude` Agent with no Profile rule configured never reads Background (the shipped default
  config has the `CLAUDE_CONFIG_DIR` rule). A resolution that times out also clears the pid until
  the next one. Checks keep running while herdr is disconnected, on the last known Status.
  The acceptance criterion's live check (a dev build on this Mac) was not run here.

## Spec Change Log

- **2026-10-21 — Board order inside a workspace group (operator decision).** "I want the box to
  be sorted according to their status, Most recently Finished first, and then the Idles and then
  the Working - this allows me to prioritize which ones to deal with." Background goes just
  before Working, and every class in a workspace group is newest first. `ClassRank` is now
  needs_you 0, Finished 1, Idle 2, **Background 3**, Working 4, Unknown 5, and a zone-1 `Start`
  is the negated class start; Needs you is unchanged, longest wait first. The ranks stated above
  (Approach, the I/O matrix, the Implementation notes) are the original ones and are superseded:
  Background shell rank 3, Really finished rank 1, Working with a shell rank 4, Idle never
  worked with a shell rank 3. The golden fixture was regenerated from core's output, and
  `groupCounts` reads `finished · idle · background · working`. PRD FR-3 and the Board design
  carry the same amendment.

## Review Triage Log

One combined reviewer (correctness, edge cases, verification gaps), 2026-10-21. Nothing high.

- **Patched — a hung read piled up goroutines.** A children read that never returns was
  abandoned but a new one started every interval. Each pane now holds a busy flag while its read
  runs, and a scheduled read is skipped while the last has not returned
  (`TestBackgroundReadHangs`).
- **Patched — a failed re-lookup lost the pid.** A Profile lookup that failed on re-detection set
  the pid to 0 and ended the watch for good. A failed lookup now keeps the known process
  (`TestBackgroundKeepsProcessOnFailedLookup`, which fails without the fix).
- **Patched — one process-table read per pane every 5 s.** `procenv` now reads the
  parent-to-children table at most once a second and shares it; only the children's arguments are
  read per pane.
- **Patched — the spine's procenv row** names the children reader.
- **Accepted — a gone process reads as no children**, not an error: the outcome (not Background)
  is the same.
- **Dogfooding — 1 s Finished blink.** The operator saw the Box flash between Background and
  Finished: for the first check's 1 s the Box read Finished and sorted after the Finished Boxes,
  then jumped back. `defaultBackgroundCheckDelay` is now 250 ms; a browser check showed the Box
  then holding Background steadily.
- **Watch in dogfooding — false positives.** A `!` bash-mode command run by the operator while
  the Agent is idle may read Background for its duration (UNVERIFIED).

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -race -count=1 ./...` -- expected: all pass
- `node --test --test-reporter=tap internal/httpapi/web/*.test.mjs` -- expected: all pass
- `test -z "$(gofmt -l .)"` -- expected: exit 0
