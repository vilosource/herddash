#!/usr/bin/env bash
# herdr 0.9.5 milestone-1 probes (Story 1.3).
#
# Answers the architecture spine's four open questions against an isolated herdr
# 0.9.5 server and prints the evidence. The findings it produced on 2026-10-16 are
# recorded in docs/research/herdr-socket-api.md, "Findings against herdr 0.9.5".
#
#   docs/research/herdr-0.9.5-probe.sh [out-dir]
#
# out-dir (default /tmp/hd13/evidence) receives transcript.txt plus the raw lines
# used as decode fixtures (ping, process_info, event_lifecycle, event_pane_scoped,
# error and snapshot .json). Any earlier output there is removed first, and the run
# fails if a fixture is missing. The fixtures are copied by hand from out-dir into
# internal/herdr/gen/testdata/; this script never writes into the repository.
#
# Isolation, which is the point of this script:
#   - every herdr invocation runs under `env -i` with HOME=/tmp/hd13/home, so the
#     HERDR_SOCKET_PATH this machine's shells inherit never reaches herdr, and the
#     isolated server's socket is /tmp/hd13/home/.config/herdr/herdr.sock
#     (short on purpose: macOS caps socket paths at 104 bytes);
#   - the Python client refuses the live socket ~/.config/herdr/herdr.sock;
#   - the server is stopped through its own socket (`herdr server stop` under the
#     same wrapper), or else by the exact PID this script started;
#   - agent status is driven by `pane.report_agent` and by a `sleep` launched with
#     argv0 `claude`, never by a real agent;
#   - it never calls pane.focus, agent.focus, pane.read or agent.read.
#
# Requires: herdr 0.9.5 at $HERDR_BIN, python3 (standard library only).
set -euo pipefail

HERDR_BIN=${HERDR_BIN:-/opt/homebrew/Cellar/herdr/0.9.5/bin/herdr}
SCRATCH=/tmp/hd13
ISO_HOME=$SCRATCH/home
SOCK=$ISO_HOME/.config/herdr/herdr.sock
OUT=${1:-$SCRATCH/evidence}
WRAP=$SCRATCH/h

[ -x "$HERDR_BIN" ] || { echo "no herdr binary at $HERDR_BIN" >&2; exit 1; }
case "$("$HERDR_BIN" --version)" in
*0.9.5*) ;;
*) echo "expected herdr 0.9.5 at $HERDR_BIN" >&2; exit 1 ;;
esac

mkdir -p "$SCRATCH" "$OUT"
rm -f "$OUT"/*.json "$OUT/transcript.txt"
# A fresh, empty isolated home on every run, so pane ids start from w1:p1.
rm -rf "$ISO_HOME"
mkdir -p "$ISO_HOME"

cat >"$WRAP" <<EOF
#!/bin/sh
# Run herdr 0.9.5 fully isolated from the operator's live server.
exec env -i PATH=/opt/homebrew/bin:/usr/bin:/bin HOME=$ISO_HOME TERM=xterm-256color $HERDR_BIN "\$@"
EOF
chmod 755 "$WRAP"

PIDFILE=$SCRATCH/server.pid
start_server() {
	nohup "$WRAP" server >>"$SCRATCH/server.log" 2>&1 &
	echo $! >"$PIDFILE"
	i=0
	while [ ! -S "$SOCK" ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i + 1)); done
	[ -S "$SOCK" ] || { echo "isolated server did not start" >&2; exit 1; }
}
stop_server() {
	"$WRAP" server stop >/dev/null 2>&1 || true
	pid=$(cat "$PIDFILE" 2>/dev/null || true)
	if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
		sleep 1
		if kill -0 "$pid" 2>/dev/null; then kill "$pid"; fi
	fi
	rm -f "$PIDFILE"
}
trap stop_server EXIT

start_server

python3 - "$SOCK" "$OUT" "$WRAP" <<'PY' | tee "$OUT/transcript.txt"
import json, os, socket, subprocess, sys, threading, time

SOCK, OUT, WRAP = sys.argv[1], sys.argv[2], sys.argv[3]
LIVE = os.path.expanduser("~/.config/herdr/herdr.sock")
assert os.path.realpath(SOCK) != os.path.realpath(LIVE), "refusing to touch the live server"
FORBIDDEN = {"pane.focus", "agent.focus", "pane.read", "agent.read"}


def dial():
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.connect(SOCK)
    return s


def readline(s, timeout=5):
    s.settimeout(timeout)
    buf = b""
    while not buf.endswith(b"\n"):
        c = s.recv(65536)
        if not c:
            break
        buf += c
    return buf.decode().rstrip("\n")


def short(line, n=400):
    return line if len(line) <= n else line[:n] + " ...(abridged)"


def rpc(method, params=None, id="r1", quiet=False):
    assert method not in FORBIDDEN
    req = json.dumps({"id": id, "method": method, "params": params or {}})
    s = dial()
    s.sendall((req + "\n").encode())
    line = readline(s)
    s.close()
    if not quiet:
        print(">>", req)
        print("<<", short(line))
    return json.loads(line), line


def save(name, line):
    with open(os.path.join(OUT, name), "w") as f:
        f.write(line + "\n")


class Sub:
    """A long-lived events.subscribe connection collecting every line it receives."""

    def __init__(self, subs, id="s1"):
        self.s = dial()
        self.lines = []
        req = json.dumps({"id": id, "method": "events.subscribe", "params": {"subscriptions": subs}})
        print(">> [subscribe]", short(req))
        self.s.sendall((req + "\n").encode())
        self.t = threading.Thread(target=self._run, daemon=True)
        self.t.start()

    def _run(self):
        buf = b""
        while True:
            try:
                c = self.s.recv(65536)
            except OSError:
                break
            if not c:
                self.lines.append("<EOF: server closed the connection>")
                break
            buf += c
            while b"\n" in buf:
                l, buf = buf.split(b"\n", 1)
                self.lines.append(l.decode())

    def dump(self):
        for l in self.lines:
            print("<< [stream]", short(l))

    def close(self):
        try:
            self.s.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        self.s.close()


LIFECYCLE = [{"type": t} for t in ("pane.created", "pane.closed", "pane.updated",
                                   "pane.exited", "pane.agent_detected", "pane.moved")]


def agent(pane):
    snap, _ = rpc("session.snapshot", quiet=True)
    for a in snap["result"]["snapshot"]["agents"]:
        if a["pane_id"] == pane:
            return {k: a.get(k) for k in ("agent_status", "state_change_seq", "completion_seq", "revision")}
    return None


def section(title):
    print()
    print("=" * 78)
    print(title)
    print("=" * 78)


section("Setup: ping, one workspace")
_, line = rpc("ping")
save("ping.json", line)
rpc("workspace.create", {"label": "probe", "cwd": "/tmp"})
P1 = "w1:p1"

# ---------------------------------------------------------------------------
section("Q4. One request per connection: two ping lines on one connection")
s = dial()
s.sendall(b'{"id":"p1","method":"ping","params":{}}\n')
print(">> (conn A)", '{"id":"p1","method":"ping","params":{}}')
print("<< (conn A)", short(readline(s)))
time.sleep(0.3)
try:
    s.sendall(b'{"id":"p2","method":"ping","params":{}}\n')
    print(">> (conn A)", '{"id":"p2","method":"ping","params":{}}')
    rest = readline(s, 2)
    print("<< (conn A)", repr(rest) if rest else "EOF, no response to the second request")
except OSError as e:
    print("!! (conn A) second write failed:", type(e).__name__, e)
s.close()
s = dial()
s.sendall(b'{"id":"a","method":"ping","params":{}}\n{"id":"b","method":"ping","params":{}}\n')
print(">> (conn B) two ping lines in a single write, ids a and b")
s.settimeout(3)
buf = b""
closed = True
while True:
    try:
        c = s.recv(65536)
    except socket.timeout:
        closed = False
        break
    if not c:
        break
    buf += c
lines = [l for l in buf.decode().split("\n") if l]
print("<< (conn B) %d response line(s), ids %s, then %s" % (
    len(lines), [json.loads(l)["id"] for l in lines], "EOF" if closed else "connection stayed open"))
s.close()

# ---------------------------------------------------------------------------
section("Q2. Foreground process: shell pane runs `sleep 300`")
rpc("pane.process_info", {"pane_id": P1})
glob = [{"type": t} for t in (
    "workspace.created", "workspace.updated", "workspace.metadata_updated", "workspace.renamed",
    "workspace.moved", "workspace.reordered", "workspace.closed", "workspace.focused",
    "worktree.created", "worktree.opened", "worktree.removed", "tab.created", "tab.closed",
    "tab.focused", "tab.renamed", "tab.moved", "pane.created", "pane.closed", "pane.updated",
    "pane.focused", "pane.moved", "pane.exited", "pane.agent_detected", "layout.updated")]
sub = Sub(glob + [{"type": "pane.agent_status_changed", "pane_id": P1},
                  {"type": "pane.scroll_changed", "pane_id": P1}])
time.sleep(0.5)
rpc("pane.send_text", {"pane_id": P1, "text": "cd /tmp/hd13 && sleep 300\n"})
time.sleep(2)
_, line = rpc("pane.process_info", {"pane_id": P1})
save("process_info.json", line)
rpc("pane.get", {"pane_id": P1})
print("-- every event on the all-24-global-kinds connection while the foreground and cwd changed:")
sub.dump()
sub.close()

section("Q2 (cont.). Foreground becomes a recognised agent program (argv0 `claude`)")
sub = Sub(LIFECYCLE + [{"type": "pane.agent_status_changed", "pane_id": P1}])
time.sleep(0.5)
rpc("pane.send_keys", {"pane_id": P1, "keys": ["ctrl+c"]})
time.sleep(0.5)
rpc("pane.send_text", {"pane_id": P1, "text": "(exec -a claude sleep 600)\n"})
time.sleep(4)
rpc("pane.process_info", {"pane_id": P1})
sub.dump()
for l in sub.lines:
    if '"event":"pane_agent_detected"' in l:
        save("event_lifecycle.json", l)
    if '"event":"pane.agent_status_changed"' in l:
        save("event_pane_scoped.json", l)
sub.close()

# ---------------------------------------------------------------------------
section("Q3. Ordering: status reports on one pane, then a second pane")
sub = Sub([{"type": "pane.agent_status_changed", "pane_id": P1}])
time.sleep(0.5)
print("   start", agent(P1))
for st in ("working", "idle", "blocked", "working", "working", "idle"):
    rpc("pane.report_agent", {"pane_id": P1, "source": "hd13-probe", "agent": "claude", "state": st}, quiet=True)
    time.sleep(0.7)
    print("   report_agent state=%-8s -> snapshot %s" % (st, agent(P1)))
r, _ = rpc("pane.split", {"direction": "right", "target_pane_id": P1, "focus": False}, quiet=True)
P2 = r["result"]["pane"]["pane_id"]
for st in ("working", "idle"):
    rpc("pane.report_agent", {"pane_id": P2, "source": "hd13-probe", "agent": "claude", "state": st}, quiet=True)
    time.sleep(0.7)
    print("   report_agent on %s state=%-8s -> snapshot %s" % (P2, st, agent(P2)))
rpc("pane.report_agent", {"pane_id": P1, "source": "hd13-probe", "agent": "claude", "state": "working"}, quiet=True)
time.sleep(0.7)
print("   report_agent on %s state=working  -> snapshot %s" % (P1, agent(P1)))
print("-- the pane-scoped stream for %s:" % P1)
sub.dump()
sub.close()

# ---------------------------------------------------------------------------
section("Errors: unknown method, unknown pane, subscribe naming one unknown pane")
_, line = rpc("no.such_method", id="e1")
rpc("pane.process_info", {"pane_id": "w9:p9"}, id="e2")
sub = Sub([{"type": "pane.created"}, {"type": "pane.agent_status_changed", "pane_id": P1},
           {"type": "pane.agent_status_changed", "pane_id": "w9:p9"}], id="e3")
time.sleep(1.5)
sub.dump()
for l in sub.lines:
    if '"error"' in l:
        save("error.json", l)
sub.close()

# ---------------------------------------------------------------------------
section("Q1. Restart: snapshot, `server stop`, start, snapshot")
r, _ = rpc("workspace.create", {"label": "second", "cwd": "/tmp"}, quiet=True)
r, _ = rpc("pane.split", {"direction": "down", "target_pane_id": P2, "focus": False}, quiet=True)
rpc("pane.close", {"pane_id": P2}, quiet=True)
snap, line = rpc("session.snapshot", quiet=True)
save("snapshot.json", line)
before = snap["result"]["snapshot"]


def summary(s):
    return {
        "panes": [(p["pane_id"], p["terminal_id"]) for p in s["panes"]],
        "agents": [(a["pane_id"], a["agent_status"], a.get("state_change_seq")) for a in s["agents"]],
        "workspaces": [(w["workspace_id"], w["label"]) for w in s["workspaces"]],
    }


print("   before:", json.dumps(summary(before)))
sub = Sub(LIFECYCLE + [{"type": "pane.agent_status_changed", "pane_id": P1}])
time.sleep(0.5)
out = subprocess.run([WRAP, "server", "stop"], capture_output=True, text=True)
print("$ herdr server stop  -> exit", out.returncode)
time.sleep(2)
print("-- a subscription open across the stop:")
sub.dump()
sys.stdout.flush()
with open(os.path.join(OUT, "before.json"), "w") as f:
    json.dump(before, f)
PY

# The Python block above ends at the stop; start the same session again and finish.
start_server

python3 - "$SOCK" "$OUT" <<'PY' | tee -a "$OUT/transcript.txt"
import json, os, socket, sys, time

SOCK, OUT = sys.argv[1], sys.argv[2]
assert os.path.realpath(SOCK) != os.path.realpath(os.path.expanduser("~/.config/herdr/herdr.sock"))


def rpc(method, params=None, id="r1"):
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.connect(SOCK)
    s.sendall((json.dumps({"id": id, "method": method, "params": params or {}}) + "\n").encode())
    s.settimeout(5)
    buf = b""
    while not buf.endswith(b"\n"):
        c = s.recv(65536)
        if not c:
            break
        buf += c
    s.close()
    return json.loads(buf)


def summary(s):
    return {
        "panes": [(p["pane_id"], p["terminal_id"]) for p in s["panes"]],
        "agents": [(a["pane_id"], a["agent_status"], a.get("state_change_seq")) for a in s["agents"]],
        "workspaces": [(w["workspace_id"], w["label"]) for w in s["workspaces"]],
    }


before = json.load(open(os.path.join(OUT, "before.json")))
print("$ herdr server  (same isolated HOME, so herdr restores session.json)")
print("<< ping", json.dumps(rpc("ping")["result"]))
after = rpc("session.snapshot")["result"]["snapshot"]
print("   after: ", json.dumps(summary(after)))
bt = dict((p["pane_id"], p["terminal_id"]) for p in before["panes"])
at = dict((p["pane_id"], p["terminal_id"]) for p in after["panes"])
print("   pane ids kept:", sorted(bt) == sorted(at))
print("   terminal ids changed:", all(k in at and bt[k] != at[k] for k in bt))
r = rpc("pane.split", {"direction": "down", "target_pane_id": "w1:p1", "focus": False})
print("   next new pane in w1:", r["result"]["pane"]["pane_id"])
rpc("pane.report_agent", {"pane_id": "w1:p1", "source": "hd13-probe", "agent": "claude", "state": "working"})
time.sleep(0.7)
a = [x for x in rpc("session.snapshot")["result"]["snapshot"]["agents"] if x["pane_id"] == "w1:p1"]
print("   w1:p1 after one report on the new instance:", json.dumps({k: a[0].get(k) for k in ("agent_status", "state_change_seq", "terminal_id")}))
PY

printf '\nQ1 (cont.): restart without the saved session (session.json moved aside)\n' | tee -a "$OUT/transcript.txt"
stop_server
mv "$ISO_HOME/.config/herdr/session.json" "$SCRATCH/session.json.aside"
start_server
python3 - "$SOCK" <<'PY' | tee -a "$OUT/transcript.txt"
import json, socket, sys
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.connect(sys.argv[1])
s.sendall(b'{"id":"r1","method":"session.snapshot","params":{}}\n')
s.settimeout(5)
buf = b""
while not buf.endswith(b"\n"):
    c = s.recv(65536)
    if not c:
        break
    buf += c
snap = json.loads(buf)["result"]["snapshot"]
print("   fresh session: panes", [p["pane_id"] for p in snap["panes"]], "workspaces", len(snap["workspaces"]))
PY

for f in ping process_info event_lifecycle event_pane_scoped error snapshot; do
	[ -s "$OUT/$f.json" ] || { echo "fixture $OUT/$f.json was not written" >&2; exit 1; }
done
