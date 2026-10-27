#!/usr/bin/env bash
# herdr 0.9.5 focus probes (2026-10-27).
#
# Answers how herdr reports focus to an API client, so herddash can derive "the
# operator left this pane" (vfkb f14fa1cdfa89). The findings are recorded in
# docs/research/herdr-focus-events-2026-10-27.md.
#
#   docs/research/herdr-focus-probe.sh [out-dir]
#
# out-dir (default /tmp/hdf/evidence) receives transcript.txt.
#
# Isolation, which is the point of this script (the same as herdr-0.9.5-probe.sh):
#   - every herdr invocation, the terminal clients included, runs under `env -i`
#     with HOME=/tmp/hdf/home, so the HERDR_SOCKET_PATH this machine's shells
#     inherit never reaches herdr; the isolated socket is
#     /tmp/hdf/home/.config/herdr/herdr.sock;
#   - the Python client refuses the live socket ~/.config/herdr/herdr.sock;
#   - the isolated config.toml written below exists only under that HOME;
#   - the server is stopped through its own socket, or else by the exact PID this
#     script started; terminal clients are killed by the PIDs this script forked.
#
# Unlike herdr-0.9.5-probe.sh it DOES call pane.focus, tab.focus and
# workspace.focus, on the isolated server only: they are what is being observed.
# herddash itself never calls them.
#
# Requires: herdr 0.9.5 at $HERDR_BIN, python3 (standard library only).
set -euo pipefail

HERDR_BIN=${HERDR_BIN:-/opt/homebrew/Cellar/herdr/0.9.5/bin/herdr}
SCRATCH=/tmp/hdf
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
rm -f "$OUT/transcript.txt"
rm -rf "$ISO_HOME"
mkdir -p "$ISO_HOME/.config/herdr"
# Workspace switching has no default key; bind one in the isolated config only.
cat >"$ISO_HOME/.config/herdr/config.toml" <<'EOF'
[keys]
next_workspace = "prefix+period"
previous_workspace = "prefix+comma"
EOF

cat >"$WRAP" <<EOF
#!/bin/sh
# Run herdr 0.9.5 fully isolated from the operator's live server.
exec env -i PATH=/opt/homebrew/bin:/usr/bin:/bin HOME=$ISO_HOME TERM=xterm-256color $HERDR_BIN "\$@"
EOF
chmod 755 "$WRAP"

PIDFILE=$SCRATCH/server.pid
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

nohup "$WRAP" server >>"$SCRATCH/server.log" 2>&1 &
echo $! >"$PIDFILE"
i=0
while [ ! -S "$SOCK" ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i + 1)); done
[ -S "$SOCK" ] || { echo "isolated server did not start" >&2; exit 1; }

python3 -I - "$SOCK" "$WRAP" <<'PY' | tee "$OUT/transcript.txt"
import fcntl, json, os, pty, signal, socket, struct, sys, termios, threading, time

SOCK, WRAP = sys.argv[1], sys.argv[2]
LIVE = os.path.expanduser("~/.config/herdr/herdr.sock")
assert os.path.realpath(SOCK) != os.path.realpath(LIVE), "refusing to touch the live server"
assert SOCK.startswith("/tmp/hdf/"), "refusing a socket outside the isolated home"


def dial():
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.connect(SOCK)
    return s


def rpc(method, params=None, quiet=False):
    req = json.dumps({"id": "r1", "method": method, "params": params or {}})
    s = dial()
    s.sendall((req + "\n").encode())
    s.settimeout(5)
    buf = b""
    while not buf.endswith(b"\n"):
        c = s.recv(65536)
        if not c:
            break
        buf += c
    s.close()
    line = buf.decode().rstrip("\n")
    if not quiet:
        print(">>", req)
        print("<<", line if len(line) < 300 else line[:300] + " ...(abridged)")
    return json.loads(line)


class Sub:
    def __init__(self, subs):
        self.s = dial()
        self.lines = []
        self.lock = threading.Lock()
        self.s.sendall((json.dumps({"id": "s1", "method": "events.subscribe",
                                    "params": {"subscriptions": subs}}) + "\n").encode())
        threading.Thread(target=self._run, daemon=True).start()

    def _run(self):
        buf = b""
        while True:
            try:
                c = self.s.recv(65536)
            except OSError:
                break
            if not c:
                break
            buf += c
            while b"\n" in buf:
                l, buf = buf.split(b"\n", 1)
                with self.lock:
                    self.lines.append(l.decode())

    def drain(self):
        with self.lock:
            out, self.lines = self.lines, []
        return out


GLOBAL = ["workspace.created", "workspace.updated", "workspace.metadata_updated", "workspace.renamed",
          "workspace.moved", "workspace.reordered", "workspace.closed", "workspace.focused",
          "worktree.created", "worktree.opened", "worktree.removed", "tab.created", "tab.closed",
          "tab.focused", "tab.renamed", "tab.moved", "pane.created", "pane.closed", "pane.updated",
          "pane.focused", "pane.moved", "pane.exited", "pane.agent_detected", "layout.updated"]


def focus_state(tag):
    snap = rpc("session.snapshot", quiet=True)["result"]["snapshot"]
    f = (snap.get("focused_workspace_id"), snap.get("focused_tab_id"), snap.get("focused_pane_id"))
    panes = [p["pane_id"] for p in snap["panes"] if p.get("focused")]
    tabs = [t["tab_id"] for t in snap["tabs"] if t.get("focused")]
    wss = [w["workspace_id"] for w in snap["workspaces"] if w.get("focused")]
    lays = {l.get("tab_id"): l.get("focused_pane_id") for l in snap["layouts"]}
    agents = {a["pane_id"]: (a["agent_status"], a.get("focused")) for a in snap["agents"]}
    print("   [%s] snapshot focused_workspace_id=%s focused_tab_id=%s focused_pane_id=%s" % ((tag,) + f))
    print("        panes focused=%s tabs focused=%s workspaces focused=%s" % (panes, tabs, wss))
    print("        layouts focused_pane_id per tab=%s" % lays)
    if agents:
        print("        agents (status, focused)=%s" % agents)
    return snap


def show_events(sub, wait=1.0):
    time.sleep(wait)
    lines = sub.drain()
    if not lines:
        print("   events: (none)")
    keep = ("focused", "agent_status")
    for l in lines:
        if any(k in l for k in keep):
            print("   event:", l if len(l) < 400 else l[:400] + " ...(abridged)")
        else:
            try:
                ev = json.loads(l).get("event") or json.loads(l).get("result", {}).get("type")
            except ValueError:
                ev = l[:80]
            print("   event (other kind):", ev)


def section(t):
    print()
    print("=" * 78)
    print(t)
    print("=" * 78)


class Client:
    """A real herdr terminal client on its own pty, 120x40."""

    def __init__(self, name, dismiss_onboarding=False):
        self.name = name
        pid, fd = pty.fork()
        if pid == 0:
            os.execv(WRAP, [WRAP])
        self.pid, self.fd = pid, fd
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
        os.kill(pid, signal.SIGWINCH)
        self.bytes = 0
        threading.Thread(target=self._drain, daemon=True).start()
        print("   started terminal client %s, pid %d" % (name, pid))
        if dismiss_onboarding:
            # A fresh HOME's first client opens a welcome modal ("enter continue"),
            # then the agent integrations list ("enter install, esc close"). While
            # it is open, prefix keys do nothing. Continue once, then close; never
            # press enter on the integrations list, which would install one.
            time.sleep(2)
            self.keys("\r")
            time.sleep(0.5)
            self.keys("\x1b")
            print("   client %s: dismissed the first-run welcome (enter, esc)" % name)

    def _drain(self):
        while True:
            try:
                b = os.read(self.fd, 65536)
            except OSError:
                break
            if not b:
                break
            self.bytes += len(b)

    def keys(self, *seq):
        for k in seq:
            os.write(self.fd, k.encode())
            time.sleep(0.3)

    def prefix(self, key):
        print("   client %s: ctrl+b %s" % (self.name, key))
        self.keys("\x02", key)

    def kill(self):
        try:
            os.kill(self.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        for _ in range(20):
            p, _ = os.waitpid(self.pid, os.WNOHANG)
            if p:
                break
            time.sleep(0.1)
        else:
            os.kill(self.pid, signal.SIGKILL)
            os.waitpid(self.pid, 0)
        try:
            os.close(self.fd)
        except OSError:
            pass
        print("   terminal client %s (pid %d) ended; it had drawn %d bytes" % (self.name, self.pid, self.bytes))


section("Setup: workspace w1 with two panes in tab 1 and a second tab; workspace w2")
rpc("ping")
rpc("workspace.create", {"label": "one", "cwd": "/tmp"})
rpc("pane.split", {"direction": "right", "target_pane_id": "w1:p1", "focus": False})
rpc("tab.create", {"workspace_id": "w1", "cwd": "/tmp", "focus": False})
rpc("workspace.create", {"label": "two", "cwd": "/tmp", "focus": False})
snap = focus_state("after setup")
print("   panes:", [(p["pane_id"], p["tab_id"]) for p in snap["panes"]])
print("   tabs:", [(t["tab_id"], t["workspace_id"]) for t in snap["tabs"]])
tab_of = {p["pane_id"]: p["tab_id"] for p in snap["panes"]}
T1, T2 = tab_of["w1:p1"], [t["tab_id"] for t in snap["tabs"] if t["workspace_id"] == "w1" and t["tab_id"] != tab_of["w1:p1"]][0]
W1P3 = [p for p, t in tab_of.items() if t == T2][0]
W2P = [p for p in tab_of if p.startswith("w2:")][0]

sub = Sub([{"type": t} for t in GLOBAL])
time.sleep(0.5)
print("   subscription:", sub.drain())

section("Q2/Q3 with no terminal client: API focus calls on the isolated server")
for desc, method, params in (
        ("pane in the same tab", "pane.focus", {"pane_id": "w1:p2"}),
        ("back to the first pane", "pane.focus", {"pane_id": "w1:p1"}),
        ("switch tab", "tab.focus", {"tab_id": T2}),
        ("switch back to tab 1", "tab.focus", {"tab_id": T1}),
        ("switch workspace", "workspace.focus", {"workspace_id": "w2"}),
        ("switch back to workspace 1", "workspace.focus", {"workspace_id": "w1"}),
        ("pane in another tab (pane.focus across tabs)", "pane.focus", {"pane_id": W1P3}),
        ("pane in another workspace (pane.focus across workspaces)", "pane.focus", {"pane_id": W2P}),
        ("pane.focus on the pane already focused", "pane.focus", {"pane_id": W2P}),
        ("back to w1:p1", "pane.focus", {"pane_id": "w1:p1"})):
    print("-- %s" % desc)
    rpc(method, params)
    show_events(sub)
    focus_state("after " + desc)

section("Seen state with no client: finish on an unfocused and on the focused pane")
st = Sub([{"type": "pane.agent_status_changed", "pane_id": p} for p in ("w1:p1", "w1:p2")])
time.sleep(0.5)
st.drain()
for p in ("w1:p2", "w1:p1"):
    for s in ("working", "idle"):
        rpc("pane.report_agent", {"pane_id": p, "source": "hdf-probe", "agent": "claude", "state": s}, quiet=True)
        time.sleep(0.7)
    print("-- %s went working then idle (focused pane is w1:p1)" % p)
    show_events(st, 0.3)
show_events(sub, 0.3)
focus_state("no client, after the finishes")

section("Q3: attach one real terminal client (pty, 120x40)")
A = Client("A", dismiss_onboarding=True)
time.sleep(2)
show_events(sub)
show_events(st, 0)
focus_state("client A attached")

section("Q2 driven by client A's own navigation")
for desc, key in (("focus pane right (same tab)", "l"), ("focus pane left (same tab)", "h"),
                  ("next tab", "n"), ("previous tab", "p"),
                  ("next workspace", "."), ("previous workspace", ",")):
    print("-- %s" % desc)
    A.prefix(key)
    show_events(sub)
    show_events(st, 0)
    focus_state("after client " + desc)

section("Seen state with client A attached")
st.drain()
print("-- w1:p2 (unfocused) goes working then idle")
for s in ("working", "idle"):
    rpc("pane.report_agent", {"pane_id": "w1:p2", "source": "hdf-probe", "agent": "claude", "state": s}, quiet=True)
    time.sleep(0.7)
show_events(st, 0.3)
focus_state("w1:p2 finished unfocused")
print("-- client A focuses w1:p2")
A.prefix("l")
show_events(sub)
show_events(st, 0)
focus_state("client A on w1:p2")
print("-- w1:p2 (focused) goes working then idle")
for s in ("working", "idle"):
    rpc("pane.report_agent", {"pane_id": "w1:p2", "source": "hdf-probe", "agent": "claude", "state": s}, quiet=True)
    time.sleep(0.7)
show_events(st, 0.3)
print("-- client A moves away to w1:p1")
A.prefix("h")
show_events(sub)
show_events(st, 0)
focus_state("client A back on w1:p1")

section("Q2 with client A attached: API focus calls")
for desc, method, params in (("pane.focus same tab", "pane.focus", {"pane_id": "w1:p2"}),
                             ("tab.focus", "tab.focus", {"tab_id": T2}),
                             ("workspace.focus", "workspace.focus", {"workspace_id": "w2"}),
                             ("pane.focus back to w1:p1", "pane.focus", {"pane_id": "w1:p1"})):
    print("-- %s" % desc)
    rpc(method, params)
    show_events(sub)
    focus_state("client attached, after API " + desc)

section("Q4: a second terminal client B")
B = Client("B")
time.sleep(3)
print("-- what B shows first: does it start on the server focus, and does A still have its own?")
show_events(sub)
focus_state("clients A and B attached")
for who, key, desc in ((B, "l", "B focuses pane right"), (A, "n", "A next tab"),
                       (B, ".", "B next workspace"), (A, "h", "A focus pane left")):
    print("-- %s" % desc)
    who.prefix(key)
    show_events(sub)
    focus_state("after " + desc)

print("-- w1:p2 goes working then idle with two clients attached")
st.drain()
for s in ("working", "idle"):
    rpc("pane.report_agent", {"pane_id": "w1:p2", "source": "hdf-probe", "agent": "claude", "state": s}, quiet=True)
    time.sleep(0.7)
show_events(st, 0.3)
focus_state("two clients, w1:p2 finished")

section("Q3: detach both clients")
B.kill()
time.sleep(1)
show_events(sub)
focus_state("client B gone, A attached")
A.kill()
time.sleep(1)
show_events(sub)
show_events(st, 0)
focus_state("no client attached")
print("-- pane.focus with no client after a detach")
rpc("pane.focus", {"pane_id": "w1:p2"})
show_events(sub)
show_events(st, 0)
focus_state("no client, after pane.focus w1:p2")

section("Seen state, one client C: which finishes are `done`, and what turns them `idle`")
ALL = ["w1:p1", "w1:p2", "w1:p3", "w2:p1"]
st2 = Sub([{"type": "pane.agent_status_changed", "pane_id": p} for p in ALL])
time.sleep(0.5)
st2.drain()
C = Client("C")
time.sleep(3)
show_events(sub)
focus_state("client C attached (no keys pressed)")


def finish(p):
    for s in ("working", "idle"):
        rpc("pane.report_agent", {"pane_id": p, "source": "hdf-probe", "agent": "claude", "state": s}, quiet=True)
        time.sleep(0.7)


print("-- w1:p3 finishes: its tab w1:t2 is not displayed by C")
finish("w1:p3")
show_events(st2, 0.3)
print("-- w1:p1 finishes: same tab as C's focused pane w1:p2, displayed, not focused")
finish("w1:p1")
show_events(st2, 0.3)
print("-- w2:p1 finishes: other workspace")
finish("w2:p1")
show_events(st2, 0.3)
focus_state("after the three finishes")
print("-- C: next tab (lands on w1:t2, whose only pane is w1:p3)")
C.prefix("n")
show_events(sub, 0.5)
show_events(st2, 0.3)
focus_state("C on w1:t2")
print("-- C: next workspace (lands on w2:p1)")
C.prefix(".")
show_events(sub, 0.5)
show_events(st2, 0.3)
focus_state("C on w2")
print("-- w1:p2 finishes while C is on w2; then C goes back to w1")
finish("w1:p2")
show_events(st2, 0.3)
C.prefix(",")
show_events(sub, 0.5)
show_events(st2, 0.3)
focus_state("C back on w1")
print("-- C: previous tab (w1:t1, focused pane there w1:p2)")
C.prefix("p")
show_events(sub, 0.5)
show_events(st2, 0.3)
focus_state("C on w1:t1")

section("Q4 again: which client's view does the snapshot report?")
D = Client("D")
time.sleep(3)
show_events(sub)
focus_state("clients C and D attached; D pressed nothing")
print("-- D: next workspace")
D.prefix(".")
show_events(sub, 0.5)
focus_state("after D next workspace")
print("-- C: ctrl+b l (C is on w1:t1 at w1:p2, which has no right neighbour)")
C.prefix("l")
show_events(sub, 0.5)
focus_state("after C pressed a prefix key that moves nothing")
print("-- D: types a plain character into its shell, then backspace")
D.keys("x", "\x7f")
show_events(sub, 0.5)
focus_state("after D typed into its pane")
print("-- C: types a plain character into its shell, then backspace")
C.keys("x", "\x7f")
show_events(sub, 0.5)
focus_state("after C typed into its pane")
print("-- w1:p1 finishes: C displays w1:t1 (focused pane w1:p2), D is on w2")
finish("w1:p1")
show_events(st2, 0.3)
print("-- w1:p3 finishes: displayed by neither client")
finish("w1:p3")
show_events(st2, 0.3)
focus_state("after finishes with two clients")
print("-- D: previous workspace (back to w1)")
D.prefix(",")
show_events(sub, 0.5)
show_events(st2, 0.3)
focus_state("after D previous workspace")
D.kill()
C.kill()
time.sleep(1)
show_events(sub)
show_events(st2, 0)
focus_state("all clients gone")
print("-- w1:p1 finishes with no client attached")
finish("w1:p1")
show_events(st2, 0.3)
focus_state("no client, final")
PY
