# Claude Code: what a resumed session's transcript says about earlier background tasks

**Recorded 2026-10-23 against Claude Code 2.1.292** (Homebrew cask, macOS, `CLAUDE_CONFIG_DIR`
`~/.claude-cldp`). Frozen research record, written for Story 5.2. It settles the deferred question
from `spec-transcript-background.md`: does a resumed session copy earlier task starts into its new
transcript, so that a start whose end never came keeps an Agent BACKGROUND?

## Provenance

**Confirmed first-hand** by five throwaway sessions and three resumes (model `haiku`) in an empty scratch directory,
outside herdr. Each was started with a clean environment (`env -i`, so no `HERDR_*`, `TMUX*` or
`CLAUDE_CODE_*` variables) and `--dangerously-skip-permissions`, as `cldp` runs it. Interactive
sessions were driven through a pseudo-terminal. Each started one background Bash command
(`run_in_background: true`, a `sleep`) and then ended in one of the ways below. Only marker fields
were read from the transcripts (record type, timestamp, `sessionId`, `toolUseResult.backgroundTaskId`,
`queue-operation` content) and the session records under `sessions/<pid>.json`; no `.key` file was
opened. Every process the experiment started was killed afterwards.

The sessions, all on 2026-10-23 (UTC times):

| # | Started as | Ended by | Then |
|---|---|---|---|
| 1 | `claude -p`, `sleep 300` | `-p` itself, after the sleep completed | — |
| 2 | interactive, `sleep 600` | `/exit`, then Ctrl-C twice (task `killed`) | resumed 09:44:47; the pending `killed` notification was enqueued again at 09:44:47.9 |
| 3 | interactive, `sleep 900` | SIGKILL of the Claude Code process | resumed 09:46:00; `stopped` enqueued at 09:46:01.4 |
| 4 | interactive, `sleep 45` | `/exit`, then Ctrl-C twice | — |
| 5 | interactive, `sleep 45` | `/exit` → "Move to background and exit" | the daemon's new session recorded `completed`; the original id resumed 09:49:42, `stopped` enqueued at 09:49:43.3 |

The three resumes each ran one short prompt, about 20 s in all, and were then ended with `/exit`.

Not tested: a background **subagent** (only shell commands), `--continue` (presumed to behave like
`--resume`), Linux, and `claude attach` to a backgrounded session.

## Findings

### 1. Resume does not copy anything: it appends to the same transcript

`claude --resume <id>` (interactive) kept the same `sessionId`. Its new session record named it,
and every line it wrote went to the end of the existing `<sessionId>.jsonl`. No new transcript
file appeared. So herddash's question "are starts copied into a new file?" has the answer **no**.
But the old start is still in the file the new process's record names. When herddash follows the
new process, it re-reads from the 8 MiB bound and sees that start again.

### 2. Resume closes every task with no completion record, with status `stopped`

About a second after the resumed process started, before the first prompt, it enqueued one
notification for each task that had a start and no completion record:

```
{"type":"queue-operation","operation":"enqueue","timestamp":"…","sessionId":"…",
 "content":"<task-notification>\n<task-id>bnv2vmt6l</task-id>\n<tool-use-id>toolu_…</tool-use-id>\n<status>stopped</status>\n<summary>Background shell command didn't finish before the previous session ended</summary>\n<note>No completion record was found for it in the previous session. …</note>\n</task-notification>"}
```

This was seen twice: after a SIGKILL of the Claude Code process, and after "Move to background and
exit" (below) on resuming the original id. A notification that had already been enqueued and
never dequeued (`killed`, below) was simply enqueued again on resume, with its original status.

**herddash before Story 5.2 closed a task only on `completed`, `failed` or `killed`**, so `stopped`
left the task open, and the resumed Agent read BACKGROUND for work the session no longer tracks.
`stopped` does not mean the command ended. After the SIGKILL the shell was still running, orphaned,
and the resumed session will never be notified of it.

### 3. What each way of ending a session leaves behind

| Ending | Tool shell afterwards | Transcript |
|---|---|---|
| `claude -p` | `-p` waits for the background command to finish before it exits | `completed` notification |
| `/exit` → "Exit and stop tasks", or Ctrl-C twice | killed | `killed` notification, written at exit |
| `/exit` → "Move to background and exit" | keeps running (reparented to launchd) | original file: start, no end. The conversation continues in a **new** session id (`claude daemon`, record `kind: "bg"`), whose new transcript copies the history, start included, and records the `completed` notification there |
| SIGKILL of the Claude Code process | keeps running, orphaned (reparented to launchd) | start, no end; the session record `sessions/<pid>.json` stays until Claude Code reaps it |

With 2.1.292, `/exit` while background work runs shows a menu: "Exit and stop tasks", "Move to
background and exit", "Stay".

## What it means for herddash

- `stopped` closes a task (ADR-0009 amended, Story 5.2). With that, a crashed session that is resumed
  in a pane leaves BACKGROUND as soon as the resumed process writes its notification.
- No time-based ageing is needed for resume. The only path that copies starts into a new transcript
  ("Move to background") runs that session in Claude Code's daemon, outside the pane, and that
  session records its own completions.
- Between a crash and a resume, the pane no longer runs that Claude Code process. herddash's
  session watch for it is presumed to end when herdr stops reporting the Agent or reports another
  process. That part was not tested here: it would mean crashing a session inside herdr.
- An orphaned tool shell is not a child of any Claude Code process, so the process check never
  counts it.
