# ADR-0009: Background work from Claude Code's session transcripts

**Status: Proposed.** Written 2026-10-21 at the operator's request, after a dogfooding
investigation, first as ADR-0008. The operator split it on 2026-10-21: reading the session record
became [ADR-0008](0008-claude-code-session-record-as-status-authority.md) (accepted); reading the
transcript, below, waits for a separate decision. Nothing in it is built yet.

## Context

The operator asked: "as a user I need to be able to differentiate between an agent waiting for a
subagent or background task vs really Finished", and then: "I want to know when you are Working
and you have background tasks or you are finished and have background tasks", including
"subagents and workflows in the background".

herdr reports a Claude Code Agent's Status by screen detection. Observed on 2026-10-21 against
this machine's live record and on a Claude Code session's own pane:

- A turn that ends with a background shell command still running reads `done`, then `idle`, until
  the command ends and Claude Code wakes. A background subagent that is *running* reads `working`;
  one waiting on its own background shells reads `done`.
- Every Bash tool call, foreground or background, main session or subagent, runs as a direct child
  `zsh -c source …/shell-snapshots/…` of the Claude Code process, in its own process group, with
  stdout to a `tasks/<id>.output` file. Foreground and background shells are indistinguishable in
  the process table. herdr's `pane.process_info` does not list them at all (isolated herdr 0.9.5).
- Subagents and workflows run inside the Claude Code process: no process exists while they think.
  On disk each subagent has `<config dir>/projects/<project>/<session>/subagents/agent-<id>.jsonl`
  (its transcript) and `agent-<id>.meta.json` (type, description, `"requestShape":"background"`),
  neither of which says whether it is still running.

The development build installed on 2026-10-21 (spec `background-tasks`) covers only the first
case: a `done`/`idle` Agent with a live tool shell reads Background. It cannot see a background
subagent or workflow that is thinking, nor background work while the Agent is Working.

Two Claude Code files do carry what is needed:

1. **`<config dir>/sessions/<pid>.json`**, one per running Claude Code process, written by Claude
   Code itself, maps the process to its `sessionId` and `cwd` (also `status`: `busy`, `idle`,
   `waiting`, `shell`). The same directory holds `<pid>.<hash>.key` files, which are secrets.
2. **The session transcript**, `<config dir>/projects/<cwd with "/" replaced by "-">/<sessionId>.jsonl`,
   records, as structured fields:
   - a background command's start: a `user` record whose `toolUseResult.backgroundTaskId` is the
     task id;
   - a background subagent's start: a `toolUseResult` with `isAsync: true`,
     `status: "async_launched"` and `agentId`;
   - either one's end: a `queue-operation` record with `operation: "enqueue"` whose `content`
     begins `<task-notification><task-id>ID</task-id>` and carries a `<status>`. In this
     session's transcript, 119 such notifications: `completed` 110, `failed` 4, `killed` 3, and 2
     with no status.

These are Claude Code's internal files, not a published interface, and the transcript holds the
whole conversation. Reading it is a new data source and a privacy decision, which is why this is
an ADR.

## Decision

- **Source.** The session record that ADR-0008 already reads gives the Agent's `sessionId` and
  `cwd`, and so its transcript's path.
- **Transcript, markers only.** It tails that session's transcript from the end it last read
  (offset kept in memory; from the start on first sight, bounded to the last N MB), decoding each
  line only far enough to find the three markers above, by fixed field names. It extracts task
  ids and the notification's `<status>` only. Message text, tool inputs and outputs, prompts and
  results are never kept, logged, stored, sent or shown. A line that does not parse is skipped.
- **Open tasks.** A task id opens at its start marker and closes at a task-notification for it
  whose status is `completed`, `failed` or `killed`; a notification without a status does not
  close it. The Agent's **open background tasks** is the count of open ids, plus, while
  its Status is `done` or `idle`, any live tool-shell child not otherwise accounted for (the
  existing process check). That check also covers a subagent's own background shells, which the
  main transcript is presumed not to record (UNVERIFIED: they are expected in the subagent's own
  transcript; not observed).
- **View, not record.** `AgentView` gains `background_tasks` (a count). The Attention Class, the
  Fold, Waiting Time and the stored record are unchanged: no stored schema change.
- **Main turn over, a background subagent running.** herdr reports `working` and Claude Code's
  session record says `busy` while any background subagent runs, even when the main conversation
  has ended its turn and is only waiting (observed 2026-10-21 on this session: the operator asked
  "why is the dashboard showing that 3:herddash is WORKING yet you are just waiting for an Agent
  to finish its work?"). The transcript tells the two apart: the main thread's last assistant
  record ended its turn and a subagent id is still open. That Agent reads **BACKGROUND**, not
  Working.
- **Board.** Working with open tasks reads `WORKING · 2 bg` (green); `done`/`idle` with open tasks
  reads **BACKGROUND** (green, dashed edge, ranked just before Working), as built; Finished only when
  none are open. Group counts as built.
- **Failure is quiet and safe.** A missing or unreadable session file or transcript, a file
  larger than the bound, or a format that no longer yields markers falls back to the process
  check alone, logged once at debug level. It never guesses a count.
- **Cost.** Tailing reads only new bytes, on the existing background interval (5 s) and only for
  `claude` Agents.

## Consequences

- The Board can say, for every Claude Code Agent, whether background shell commands, subagents
  or (presumed, see below) workflows are still open, whether it is Working or not.
- herddash depends on Claude Code's internal file layout and record shapes. A Claude Code release
  can change them silently; the fallback keeps the Board honest but less informative. Fixture
  tests pin the shapes observed on 2026-10-21 so a change is noticed.
- herddash now reads, line by line, a file containing the operator's conversations. It extracts
  nothing but ids and a status word, but it is the first time herddash opens conversational data.
  The PRD's NFR5 ("never read pane output") is not breached in letter; this ADR records that it
  is a deliberate extension in spirit, approved by the operator.
- Only Claude Code is covered. Codex and others keep herdr's Status alone.
- **Unverified:** workflows' records were not observed (running one was not authorised for the
  experiment). They are presumed to follow the subagent markers; the build must observe one
  before claiming them.

## Alternatives considered and rejected

- **Process check only (built).** No new data source, but blind to thinking subagents and
  workflows, and to background work while Working. Kept as the fallback.
- **Carry-over only.** A shell alive at the end of a turn is background thereafter, even while
  Working. Covers an Agent woken while earlier work runs, but not work started in the current turn
  or any subagent. Subsumed by this decision.
- **Claude Code hooks.** A hook sees a background tool call start but nothing fires when it ends,
  so a second source is still needed; and it means installing into every Claude Code config dir,
  which herddash never does (deployment belongs to the provisioning repository).
- **Reading pane output.** Ruled out by NFR5 and by herdr's warning that reads are not passive.
- **Asking herdr upstream** to report background work: right in the long run, not available now.
