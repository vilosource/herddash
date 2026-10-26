# Claude Code: bash mode (`!cmd`) is a model turn

**Recorded 2026-10-23 against Claude Code 2.1.267 to 2.1.292** (macOS, 473 transcripts in two
config directories: 318 under `~/.claude-cldp` and 155 under `~/.claude-cldw`). Frozen research
record, written for Story 5.15. It settles the deferred question from Story 5.14's spec: do
bash-mode records (`<bash-input>`, `<bash-stdout>`, `<bash-stderr>`) and `<local-command-stderr>`
results start a turn and clear a recap, and should they?

## Provenance

**Confirmed first-hand** by counting records in the 473 transcripts under
`~/.claude-cldp/projects/*/*.jsonl` (318, holding 7 bash commands) and
`~/.claude-cldw/projects/*/*.jsonl` (155, holding 49), 56 bash commands in all, on 2026-10-23.
Structure only was read: a record's `type`, a system record's `subtype`, `isMeta`, `isSidechain`,
`uuid`, `parentUuid`, `timestamp`, `promptId`, `turnOrigin`, `turnPosition`, the Claude Code
`version`; an assistant record's `message.model`, `message.stop_reason` and the types of its
`message.content` blocks; and which tag a string `message.content` starts with (or, for
`<bash-stderr>`, whether the tag occurs in it). No command, output, prompt or reply text was read or
kept, and none is copied here. Nothing was run against Claude Code, herdr or a live pane.

A **real model turn** here is an assistant record whose `message.model` is a real model id (not
`<synthetic>`) and whose content blocks are thinking, text or tool_use.

Not tested: Linux, the other config directories on this machine (`~/.claude`, `~/.claude-oneio`),
Claude Code releases before 2.1.267.

## Findings

### 1. The shapes

| Tag a string user record starts with | Records | `isMeta` | `isSidechain` |
|---|---|---|---|
| `<bash-input>` | 56 | false, all | false, all |
| `<bash-stdout>` | 56 | false, all | false, all |
| `<bash-stderr>` | 0 | | |
| `<local-command-stderr>` | 0 | | |

`<bash-stderr>` never leads a record: it sits inside every `<bash-stdout>` record's string, after
the `</bash-stdout>` close. Each bash command is the pair, input then output. `<bash-stdout>` records
written by 2.1.286 and later carry `turnOrigin: "human"`.

### 2. The model answers the command

Of the 56 bash commands, 55 had a later main-thread (`isSidechain` false) assistant record in the
same transcript. The first such record's `parentUuid` chain back to the `<bash-stdout>` record
was:

| Chain from the assistant record to `<bash-stdout>` | Commands |
|---|---|
| direct: its parent is the `<bash-stdout>` record | 40 |
| only through attachment records (instructions, date, session_context, prompt_snapshot and the like) | 13 |
| through one `isMeta` user record starting `<system-reminder>` | 1 |
| through a typed prompt | 1 |

So in 54 of the 55 a real model turn answered the bash command with no prompt between: Claude Code
sends the command and its output to the model as the user's message, and the model replies, often
with tool use. The gap from the `<bash-stdout>` record to that assistant record was under 5 s in
32, 5 to 60 s in 22 and longer in 1.

The 56th command had no later assistant record in its transcript: it is the end of the file. An
unanswered `!cmd` therefore leaves the turn open, exactly as an interrupted prompt does, which is
herddash's existing behaviour.

### 3. `<local-command-stderr>`

Not seen in any of the 473 transcripts. Claude Code's local commands here write their result as a
`system` record of subtype `local_command` or, in older releases and some commands, as a user
record starting `<local-command-stdout>`.

## What it means for herddash

- Bash mode **is** a turn on this Claude Code. Its two records keep decoding as turn starts
  (`core.DecodeTranscriptLine`'s default for a non-sidechain user record), so a `!cmd` clears the
  recap and reopens the turn, as the model's reply that follows it would. Treating them as no turn
  would keep a recap beside a running model turn, the stale recap ADR-0010 rules out. Story 5.15
  pins this with tests rather than changing it.
- **UNVERIFIED:** a user record starting `<local-command-stderr>` is treated as the older-form local
  result, the twin of `<local-command-stdout>`: `CommandResult`, no turn start. This is a defensive
  assumption by symmetry with `<local-command-stdout>`; the shape was never observed. A
  `local_command` system record is a result by its subtype whatever its content, so a
  `<local-command-stderr>` content there was already handled and never carries a manual recap.
