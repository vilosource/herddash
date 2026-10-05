# herdr multi-host support — what it is, and what it is not for us

**Recorded 2026-10-05 against herdr 0.9.3 documentation and the v0.9.3 source.** Frozen research
record. It exists because the obvious reading of the 0.9.0 release notes is wrong in a way that
would have shaped herddash's architecture incorrectly.

## The claim, and the correction

herdr 0.9.0 announced managing local and saved SSH machines from one window, with a combined agent
list, machine-scoped navigation and notifications, and automatic reconnects. Read quickly, that
sounds like one herdr server aggregating agents from several hosts, which would have given
herddash a three-machine view for free.

**It is client-side SSH federation, not server aggregation.** Each machine runs its own
independent herdr server, its own sessions and its own processes. The *client* holds several SSH
connections and merges them for display. No herdr server ever learns that another exists.

## What follows for herddash

**A socket API client sees exactly one machine.** There is no machine dimension in any API
payload. The term `machine_id` does not appear anywhere in the documentation, and no machine field
appears in a live snapshot. The documentation is explicit that workspace, tab and pane ids, and
agent names, are scoped to one server, and that two machines may each contain a `w1:p1` or an
agent named the same thing. Selecting a machine in the interface does not retarget CLI commands.

**"Machine-scoped notifications"** means the client attributes a notification to the machine it
came from and scopes navigation and filtering by machine. It is a client-side display concern, not
an API surface.

**Machines are not configuration.** There is no machines section in `config.toml`. Profiles are
managed imperatively through `herdr machine add`, `list`, `rename`, `enable`, `disable`, `remove`,
`status` and `reconnect`, and stored in an opaque client-side catalog holding an id, a label, an
SSH target, an explicit remote session and an enabled flag. No credentials. One profile targets
one remote session, not a whole host.

## The one bridge that does exist, and its limits

herdr 0.9.1 added a machine prefix that forwards JSON API requests and responses to a remote
server's socket over non-interactive SSH. No open interface is required, and payloads are not
interpolated into the shell command.

```bash
herdr --machine "Build machine" api snapshot
herdr --machine "Build machine" agent list
```

Forwarded: the workspace, worktree, tab, pane, notification and agent command groups except
attach, plus the snapshot, server status, and API-backed plugin commands. Not forwarded:
installation, configuration, session management and interactive attach.

The limits that matter:

- **It is CLI-only.** You shell out to `herdr` rather than dialling a socket, so there are **no
  event subscriptions across machines.** Snapshot polling is all that is on offer.
- It targets one machine at a time. Combined listings and routing through another client's
  connections are explicitly not included. Any union is yours to compute.
- The selector must be an enabled saved profile id or a unique case-sensitive label, not an
  arbitrary SSH hostname. Combining it with a session or remote flag is an error.
- Ids collide across machines, so namespacing is the caller's job.
- A failure does not fall back to the local machine and is not retried. Remote bridges close after
  roughly a minute of idleness.

## Decision for herddash

**One daemon instance per host, each on its own local socket, aggregated by the web tier.** Every
host keeps full subscription fidelity and sub-second status changes, which the SSH bridge cannot
provide. The alternative, one daemon polling remote snapshots through the machine prefix, trades
the event stream for polling latency on precisely the hosts you are not sitting at.

The data model therefore carries a host dimension from the first commit, and every herdr id is
namespaced by the host it came from, because herdr guarantees those ids collide.

Aggregation across daemons is out of scope for v1 and gets its own decision record.
