# ADR-0015: Seen when you leave the pane

**Status: Accepted** on 2026-10-27 by the operator (vfkb f14fa1cdfa89), after dogfooding Epic 6's
`-dev` build: visiting a pane in herdr and moving away did not mark its Box Seen. It amends
[ADR-0014](0014-acknowledging-a-finished-agent.md) (an acknowledgement can also be made by the
Daemon, from herdr's focus, and the Board gains an "in view" tag and a `Mark unseen` pill) and
[ADR-0006](0006-how-herddash-speaks-to-herdr.md) (the lifecycle subscription gains one global kind,
`pane.focused`, and the snapshot's `focused_pane_id` is read). It is grounded in the research record
[herdr-focus-events-2026-10-27.md](../research/herdr-focus-events-2026-10-27.md), observed against
an isolated herdr 0.9.5. Built as Stories 6.6 to 6.9 of Epic 6.

## Context

ADR-0014 made Seen a Board click. Dogfooding showed the operator's real act of reading is in herdr:
they switch to the pane, read the result, and switch away. The Box stayed Finished, so the Board
asked for a second, redundant action on every result.

The operator decided, quoted from vfkb f14fa1cdfa89: *a Finished Agent becomes Seen automatically
when, after its finish, its pane was focused in herdr for at least ~3 s and focus then moved to
another pane. A Board click still marks at once.* herdr's `done`/`idle` distinction is **not** used:
herdr's seen state is per terminal client, a detached herdr reports `idle` for every finish, and a
finish into the focused pane goes straight to `idle` (record, finding 5: a finish is "seen" as soon
as any attached client displays the pane's *tab*, which is coarser than focus). The decision adds a
`● in view` tag on a Finished Box whose pane is focused in herdr, a `Mark unseen` pill on Seen
Boxes (the operator's explicit choice wins over the automatic mark for that finish), and a source
on every recorded acknowledgement, shown in history.

What herdr 0.9.5 offers, from the record:

1. `pane.focused` is a global subscription kind. Its event, in the underscored lifecycle envelope,
   is `{"event":"pane_focused","data":{"type":"pane_focused","pane_id":…,"workspace_id":…}}`:
   no client, no previous pane, no time, no sequence number.
2. Every focus change, from a client's keys or an API focus call, emits `workspace_focused`,
   `tab_focused` and `pane_focused`, and `pane_focused` always names the pane landed on, across tab
   and workspace switches. It can repeat for the pane already focused.
3. `session.snapshot` always names `focused_pane_id`, attached or not; attaching and detaching emit
   nothing, and nothing herddash may read says whether a client is attached.
4. With two clients attached, views are per client, events are anonymous, and the snapshot's focus
   can jump to the other client's view with no event.

So the rule can be built, exactly with one attached client, which is the operator's setup. It
needs no focus call: herddash only reads.

## Decision

### What herddash reads from herdr (amending ADR-0006)

- `internal/herdr`'s lifecycle subscription gains **one** kind, `pane.focused`, on the same
  connection as the other lifecycle kinds. Not `tab.focused` or `workspace.focused`: they add
  nothing the pane event lacks. The wire types are not generated yet (`gen` holds only the
  `EventKindPaneFocused` constant): genwire's `roots` gain the `pane.focused` variant of
  `Subscription` and the `pane_focused` variant of `EventData`, regenerated from the committed
  0.9.5 schema dump by `go generate ./internal/herdr/gen`. Its `forbidden` list is unchanged: a
  subscription to focus is a read; `pane.focus`, `agent.focus` and the output reads stay refused.
- A `pane_focused` line maps to `core.LifecycleEvent{Kind: core.LifecycleFocused, PaneID,
  WorkspaceID}`. A `pane_focused` with no `pane_id` is an unexpected line, as for every other kind.
  A `workspace_focused` or `tab_focused` line, should herdr send one, is dropped without a
  delivery, so it never counts as unexpected (which would cause a snapshot read).
- The snapshot mapping reads `focused_pane_id` into `core.Snapshot.FocusedPaneID` (`""` when absent).
- **Nothing else.** `internal/herdr` still has no focus method: herddash never calls `pane.focus`,
  `agent.focus`, `tab.focus`, `workspace.focus` or any other mutation, and reading focus does not
  change herdr's seen state. ADR-0011's import rules are unchanged.
- A focus event is **not** a signal that tracked state is stale: it does not request a snapshot,
  does not count as "an event during a snapshot read" (AD-11's re-read), and is never recorded as a
  Transition. Focus is held in the engine's memory only; it is never written to the record.

### Where the engine thinks focus is

The engine (the single writer, AD-2) holds one **focused pane** per connection, with the instant it
started counting it, the **dwell start**, by the engine's clock (its injected clock, so tests drive
it; a `time.Time` keeps Go's monotonic reading, so a stepped wall clock does not lengthen a dwell).

- **A `pane_focused` for another pane** is a **leave** of the pane focused before it (the rule below
  is evaluated for that pane), and the new pane becomes focused with its dwell starting now.
- **A `pane_focused` for the pane already focused** is not a leave and changes nothing; its dwell
  goes on.
- **At a connection's first snapshot (a Daemon start, a reconnect) and at a resync**, the focused
  pane is the snapshot's `focused_pane_id`, with its dwell starting when the engine applied that
  snapshot: nothing before the engine saw it counts. Any focus held before the resync is dropped
  without a leave.
- **A reconcile snapshot (or any later read) whose `focused_pane_id` differs from the engine's** is
  a focus move the engine did not see as an event: a missed event, or another client's view (record,
  finding 4). The focused pane becomes the snapshot's, with its dwell starting now, and the pane
  focused before is **not** marked: only an observed `pane_focused` is a leave. A snapshot read that
  started before a `pane_focused` arrived is older than that event, so its focus is ignored.
  A read can also be newer than a `pane_focused` still on its way, the snapshot and the event stream
  being separate connections. So such a move is held, and when the very next `pane_focused` names
  the pane the snapshot moved to, that event is the leave of the pane focused before, judged with
  the dwell it had when the snapshot was applied. Any other event, a later snapshot, a resync or a
  restarted dwell of either pane discards it. *Amended 2026-10-27 by Story 6.7's review.*
- **The dwell restarts** whenever the focused pane's Agent enters a new finish or leaves one: when
  its fold's class or `Since` changes, a new Agent replaces it, or a turn herddash did not observe
  ends its acknowledgement (ADR-0014's amendment). herdr's `done` to `idle` within one finish moves
  neither, so it does not restart the dwell. So the dwell always measures time focused **during
  the current finish**: from the later of "focus landed" and "the finish began, as the engine saw
  it in this run".
- Focus moves while the Daemon is down, or while the lifecycle connection is down, are missed, and
  missed moves never mark.

### The rule: Seen on leave

When a pane is left, the engine marks its Agent's current finish Seen **exactly when all hold**:

1. the pane holds a present, published Agent under the epoch in force;
2. its view is Finished, not Background, and not already seen (Needs you, Idle, Working, Unknown
   and Background are never marked);
3. its fold is known with `Since > 0`, the pane holds no Profile-held rows and its fold is in the
   record (the same conditions as a Board mark, ADR-0014);
4. the dwell, from its dwell start to the leave, is at least `seenDwell` (**3 s**, an engine tunable,
   `defaultSeenDwell` in `internal/engine/tunables.go`, listed in `docs/config.example.toml`'s fixed
   timings); a pane landed on and left sooner is not marked, so flicking through panes marks
   nothing; and
5. **the finish has no acknowledgement row yet**: the automatic rule only ever writes a finish's
   first row. Once the operator has marked or unmarked a finish by hand, or the Daemon has marked
   it, only the operator's hand changes it. So `Mark unseen` after an automatic mark sticks for that
   finish, across leaves and across a Daemon restart, because it is derived from the record. When
   herdr's order shows a turn herddash did not observe (ADR-0014's amendment), the earlier rows
   belong to the earlier turn and do not count for this one.

The pure decision is one function in `internal/core` (AD-1), `core.SeenOnLeave`, taking the view's
class, Background and seen flags, whether the finish has a row, the dwell and the minimum; the
engine supplies the facts and the clock. A mark it decides is written exactly as a Board mark is,
through `store.AppendAcknowledgements`, with herdr's order when known, and source `herdr_focus`;
the engine logs it at info (pane, since, source) and publishes it as the Agent's `agent` change.
A write failure is logged and leaves the Box Finished (the Board shows more, never less); it is not
retried, and a later leave of the same finish tries again.

Consequences of the rule that are deliberate:

- **Visit, read, leave**: the Box is Finished and `in view` while the operator is there, and Seen
  when they leave.
- **Visit, prompt, leave**: the prompt starts a turn, so the Box is Working; it is Finished, not
  in view, when the turn ends while the operator is elsewhere. Status events and focus events
  arrive on different connections, so a leave can be evaluated before the `working` that preceded
  it arrives; the mark then written names the old finish and the `working` ends it at once, exactly
  as any Transition out of a finish does.
- **Stay and watch several turns, then leave**: each finish is Finished and in view; leaving marks
  the last one Seen.
- **A finish into the focused pane while the operator is away** stays Finished, in view, until
  they come back and leave that pane: no leave arrives while they are away.
- **A detached herdr never marks**: with no client attached no focus moves, so no `pane_focused`
  arrives (unless another API client calls a focus method, below).
- A Board click still marks at once, focused or not, as ADR-0014 decided.
- A new turn, Needs you or a new Agent ends Seen, automatic or not, as ADR-0014 decided; Background
  still takes precedence over Seen.

### The record (migration 0007)

Migrations 0005 and 0006 are not released, but the operator's database already is at schema 6:
the installed `-dev` build (0.8.0-dev, `edaf0ab`) carries both. Editing either in place would leave
that database at schema 6 without the new column, with no migration to add it. So a new migration:

```sql
-- 0007_acknowledgement_source.sql
ALTER TABLE acknowledgements ADD COLUMN source TEXT NOT NULL DEFAULT 'board'
    CHECK (source IN ('board', 'herdr_focus'));
```

- Every row recorded before it is a Board mark or undo, so `'board'` is their truthful source. It
  migrates a database at schema 5 or 6 in one transaction, like every migration.
- The table stays append-only. `core.AcknowledgementInput` and `core.Acknowledgement` gain
  `Source` (`core.AckSourceBoard`, `core.AckSourceHerdrFocus`); `core.NewAcknowledgement` refuses
  any other value, the empty one included. The one `INSERT` binds it and `store.Acknowledgements`
  reads it back through the constructor. An undo is always `board`.
- `core.Seen` and `core.SeenAt` are unchanged: a source does not change what a row means.
- Schema 7 is a one-way door: a build that knows schema 6 refuses a database migrated to 7, and
  0.7.x refuses it as it refuses 5 and 6.

### The view

- `AgentView` gains `in_view` (boolean, always sent): true exactly when the Agent's pane is the
  engine's focused pane. It does not change the sort key, the class or the counts. A focus move
  publishes once, with an `agent` change for each Agent whose view changed (at most the pane left
  and the pane landed on), plus the mark when one is written. No event name is added.
- **"In view" means herdr's focused pane, not that someone is looking.** herdr names a focused pane
  even with no client attached, and herddash cannot tell whether one is (record, finding 3). The
  Board's wording and title say what it is.

### The Board (amending ADR-0014's controls)

- A Finished Box whose view has `in_view` shows a tag, `● in view`, on its location line before
  the Seen pill. Its title is "Focused in herdr now. Leaving the pane after 3 s marks it seen."
  Only Finished Boxes show it; it is a label, not a control.
- A Seen Box gains a pill, `Mark unseen` (`.unack`), where a Finished Box has its Seen pill. It
  posts the Box's exact finish to the existing per-Agent `.../unseen` route, and its Undo bar reads
  "Marked <tab> not seen", whose Undo marks it seen again. It is the remedy for an automatic mark
  the operator disagrees with. Its accessible name is "Mark <tab> not seen".
- Nothing else changes: the Seen pill, the bulk mark, the 6 s Undo bar and the guard are ADR-0014's.

### History

The history route's `acknowledgements` items gain `source` (`"board"` or `"herdr_focus"`), always
sent. The Board still shows no history.

### The import graph

No edge is added. The focus subscription and mapping are inside `internal/herdr`, the rule in
`internal/core`, the tracking in `internal/engine`. `store.AppendAcknowledgements` is still called
only by the engine.

## Consequences

- The operator's ordinary act of reading in herdr clears the Board's Finished Box, and the Board
  click remains for results read some other way.
- **Several attached herdr clients break the premise.** Views are per client and events anonymous,
  so one client's navigation can mark Seen a pane the operator is still reading at another. The
  operator runs one client per host today; if that changes, an automatic mark can be wrong, and
  `Mark unseen` is the remedy. This is documented, not detected.
- **Another API client's focus call reads like the operator moving.** A tool calling `pane.focus`,
  `tab.focus` or `workspace.focus` can trigger a leave, even with herdr detached. herddash never
  makes such a call itself.
- **`● in view` on a detached herdr** shows on the pane last focused. It never leads to a mark by
  itself: a mark needs an observed leave.
- A Daemon restart keeps every mark (they are rows), loses only the in-memory focus and dwell, and
  starts counting again from the first snapshot. A leave within 3 s of a restart or resync does not
  mark, even if the operator had been reading longer.
- The record grows by at most one automatic row per finish.
- ADR-0014's statement that "an acknowledgement says the operator has read a result" now also
  covers the Daemon's inference of it from focus; the row's `source` says which.

## Alternatives considered and rejected

- **herdr's `done` to `idle`.** Coarser than focus (any pane in a displayed tab), per client, and
  `idle` for every finish while detached. Rejected by the operator.
- **Mark on focus alone, without a leave.** A finish into the focused pane while the operator is
  away would be marked though nobody read it.
- **Timing the dwell from the snapshot's focus at a restart backwards.** herdr gives no time; the
  engine counts only what it saw.
- **Marking on a snapshot that shows focus moved.** The move may be another client's view
  appearing with no navigation (finding 4); only an observed `pane_focused` is a leave.
- **Letting the automatic rule re-mark after `Mark unseen`.** The operator's explicit choice must
  win; one row per finish from the Daemon at most.
- **Editing migration 0005 or 0006 in place.** The operator's database is already at schema 6.
- **A timer that marks while still focused after 3 s.** The decision is "focus then moved"; a timer
  would also mark the away case.
- **Detecting an attached client** (server log, `no_foreground_client` from a mutating call).
  The log is not an API, and the calls mutate herdr.
