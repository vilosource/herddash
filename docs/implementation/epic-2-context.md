# Epic 2 Context: History that survives restarts

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Turn the live terminal feed Epic 1 built into a durable record. Every Transition is written the moment it is observed, with its provenance. Every gap is written as a resync marker. Waiting Times are computed from the record, so they survive Daemon and herdr restarts and are marked exact only when they truly are. Retention keeps the database bounded without losing what current Agents depend on, and the operator's weeks of plugin log are imported once as backfilled history. The record is what makes the Board (Epic 3) trustworthy. Epic 1 already provides the engine actor, the herdr client, resync and reconnect with the epoch rule, the version gate, Profile resolution, `core.NewTransition`, configuration (data directory, `--database`, `retention_days`), the umask `077` in `main` and the `herddash.log` slog file. Nothing is persisted yet.

## Stories

- Story 2.1: A local database herddash owns
- Story 2.2: Record every Transition
- Story 2.3: Gaps are recorded, not hidden
- Story 2.4: Waiting Time that survives restarts
- Story 2.5: Retention keeps the database bounded
- Story 2.6: Import the plugin's log as history

## Requirements & Constraints

- **Owner-only storage:** the data directory is `0700`. The database, WAL, SHM, lock and log files are `0600`, asserted by tests; this also pins the umask set in `main`, which nothing tests yet. A `--database` path outside the data directory must still have an owner-only parent directory (deferred from Story 1.2).
- **One process owns the database:** `store.Open` takes an exclusive advisory `flock` on a lock file beside the database, for `serve` and the importer alike. A second opener fails at once and names the lock. The OS releases a crashed process's lock.
- **Schema:** migrations are embedded, numbered SQL files, applied in order at start in one transaction, and the schema version is recorded. A database newer than the binary is refused with a clear message. The database runs in WAL mode.
- **Every Transition stores:** Host, Server, epoch, pane id, Agent Program, the Profile as resolved at that moment, previous and new Status, herdr ordering, herdr version, provenance (`live`/`snapshot`/`backfill`), a possibly-spurious flag, `observed_at` (UTC Unix ms) and a nullable unique `import_key` for imported rows only.
- **Unknown means NULL**, never the string `unknown`, which is a real Status.
- **Resync markers** record time, Host, Server, epoch and reason. Each Daemon start writes one with reason `daemon_start`. Other reasons are `events_lost`, an unknown error code, a rejected subscribe, a reconnect, and a reconcile disagreement.
- **A Profile Map edit never rewrites history.** Old rows keep their Profile, and new rows carry the new one.
- **Waiting Time** is exact only when the current class began with a `live` Transition and the Status seen on reconnect is unchanged. Otherwise it is a lower bound from the earliest evidence (last recorded Transition, else Daemon start), shown with `≥`. `done` becoming `idle` does not move the start. An `idle` Agent that was never `working` counts as Working. A herdr restart with unchanged Status records no Transition, only the marker.
- **Retention:** N days (default 90). For a present Agent, it keeps every Transition from the one that began its current class onward, plus its latest `working` Transition. Absent Agents are swept by age alone. The sweep never runs during a resync.
- **Import:** `herddash import-plugin-log <file> --herdr-0.9.2-since <date>` runs with no Daemon. It writes `backfill` rows with epoch 0, the Profile from the log, NULL previous Status and NULL herdr ordering. `done` entries before the date are flagged possibly spurious, and entries older than Retention are skipped. Running it twice adds nothing. Imported rows never become a live Agent or its Waiting Time.
- Tests use temporary files only and never touch a live herdr. The `-race`, vet and gofmt gates hold.

## Technical Decisions

- **Dependency:** this epic adds `modernc.org/sqlite` v1.60.1 with `modernc.org/libc` pinned exactly to v1.77.1. They are upgraded only together, and never with a blanket `go get -u`. The build stays pure Go (`CGO_ENABLED=0`), and nothing else may be added without an ADR.
- **Package roles:** `internal/store` handles open with the lock, migrations (`internal/store/migrations/` numbered `.sql`, embedded), append, history and fold-input reads, and the Retention sweep. `store` imports only `core`. Only `engine` and `importer` call `store.Append`, which a call-site test enforces. `httpapi` later calls only read functions. `cmd` may import `store`, and `importer` imports `core`, `store` and `config`.
- **Append-only:** Transitions and markers are inserted and never updated. The Retention sweep is the only delete in the codebase. A migration touching the transitions table needs an ADR first, so get its columns right in 2.1.
- **One constructor, one insert:** `core.NewTransition` (already built, with `HerdrOrder *uint64`, `HerdrVersion`, `ObservedAt`, `Provenance` and `ImportKey`; it rejects a mismatched import key) builds every row. `store.Append` writes every row.
- **herdr ordering column = `state_change_seq`.** It is one server-wide counter, present only on snapshot agent records, and it restarts at 1 per herdr instance. It is stored for `snapshot` Transitions, NULL for `live` and `backfill`, and compared only within one server instance. Order within an Agent is `observed_at`, then engine record order. *Story 2.2's acceptance criterion "read back in herdr's order" predates this finding; the binding rule is the one above, so reconcile it when 2.2's spec is written.*
- **Single writer:** the engine goroutine is the only Daemon-side writer. The importer is the only other writer, and the lock keeps it from running beside a Daemon.
- **Fold:** at start and after each resync, the engine loads each Agent's history through one `store` read and passes it to the pure `core.Fold`. It returns the start instant of the current class, that Transition's provenance, and whether the Agent was ever `working`. `core` never reads the clock, so inject it.
- **Identity:** `AgentKey{Host, Server, Epoch, PaneID}`. Epoch 0 is backfill only. The epoch and the pane-to-`terminal_id` map live only in memory today, so a Daemon restart starts at epoch 1 with nothing to compare. The spine expects milestone 2 to persist them; no story names this explicitly.
- **Logging:** never log environment values except a matched `CLAUDE_CONFIG_DIR`. Pane Titles are logged only at debug.

## Cross-Story Dependencies

- **2.1 comes first.** It provides the store everything else uses. 2.2 wires the engine's existing Sink events into `store.Append` and writes the `daemon_start` marker. 2.3 turns the engine's existing resync and reconnect signals into markers. 2.4 needs the 2.2 record and changes the terminal output. 2.5 needs 2.4's class-start logic for its floor. 2.6 needs 2.1's lock and `import_key`.
- **Operator checkpoint:** 2.6 needs a plugin-log sample and the date the Host reached herdr 0.9.2 before it starts. 2.4 may be split when its story is created.
- **Into Epic 3:** history reads feed the history route. The fold result feeds `AgentView`'s `waiting_since` and `waiting_exact`. Markers feed SSE `resync` events.
- **Dogfooding:** the live server is still 0.8.2, behind the advisory flag, so the herdr version is recorded on every row.
