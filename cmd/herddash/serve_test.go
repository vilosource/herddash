package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
	"github.com/vilosource/herddash/internal/store"
)

// fakeHerdr is a minimal herdr on a unix socket: it answers ping and
// session.snapshot, and holds every subscription open until the client closes
// it. restart stands in for herdr restarting. No test here dials a real herdr.
type fakeHerdr struct {
	t          *testing.T
	path       string
	openSubs   atomic.Int32 // subscriptions the client has not closed
	statusSubs atomic.Int32 // status subscriptions ever started
	// push makes every open status subscription send one change to blocked,
	// which later snapshots then report too.
	push    chan struct{}
	blocked atomic.Bool
	// instance is prefixed to every terminal id; a restart changes it.
	instance atomic.Int32
	// version is what ping answers; unset means 0.9.5.
	version atomic.Value
	// snapshots and subscribes count session.snapshot and events.subscribe
	// requests.
	snapshots  atomic.Int32
	subscribes atomic.Int32
	// procPID is the pid pane.process_info reports as the foreground process;
	// 0 reports none.
	procPID atomic.Int32

	mu    sync.Mutex
	ln    net.Listener
	conns []net.Conn
	wg    sync.WaitGroup
}

const fakeSnapshot = `{"type":"session_snapshot","snapshot":{"version":"0.9.5","protocol":22,` +
	`"workspaces":[{"workspace_id":"w1","number":1,"label":"probe","focused":true,"pane_count":2,"tab_count":1,"active_tab_id":"w1:t1","agent_status":"working"}],` +
	`"tabs":[{"tab_id":"w1:t1","workspace_id":"w1","number":1,"label":"1","focused":true,"pane_count":2,"agent_status":"working"}],` +
	`"panes":[{"pane_id":"w1:p1","terminal_id":"t1","workspace_id":"w1","tab_id":"w1:t1","focused":true,"agent":"claude","agent_status":"working","revision":0},` +
	`{"pane_id":"w1:p2","terminal_id":"t2","workspace_id":"w1","tab_id":"w1:t1","focused":false,"agent_status":"unknown","revision":0}],` +
	`"layouts":[],` +
	`"agents":[{"terminal_id":"t1","agent":"claude","agent_status":"working","workspace_id":"w1","tab_id":"w1:t1","pane_id":"w1:p1","focused":true,"state_change_seq":3,"revision":0}]}}`

func startFakeHerdr(t *testing.T) *fakeHerdr {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeHerdr{t: t, path: filepath.Join(dir, "herdr.sock"), push: make(chan struct{})}
	f.listen()
	t.Cleanup(f.shutdown)
	return f
}

// listen starts accepting on the socket.
func (f *fakeHerdr) listen() {
	ln, err := net.Listen("unix", f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	f.ln = ln
	f.mu.Unlock()
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			if f.ln != ln {
				// Accepted after shutdown began: shutdown will not see it.
				f.mu.Unlock()
				conn.Close()
				return
			}
			f.conns = append(f.conns, conn)
			f.mu.Unlock()
			f.wg.Add(1)
			go func() {
				defer f.wg.Done()
				defer conn.Close()
				f.serve(f.t, conn)
			}()
		}
	}()
}

// shutdown stops listening and drops every connection, as a herdr that exits.
func (f *fakeHerdr) shutdown() {
	f.mu.Lock()
	if f.ln != nil {
		f.ln.Close()
		f.ln = nil
	}
	for _, c := range f.conns {
		c.Close()
	}
	f.conns = nil
	f.mu.Unlock()
	f.wg.Wait()
	os.Remove(f.path)
}

// restart is herdr restarting: every connection ends, and the new instance
// keeps the pane ids but issues new terminal ids.
func (f *fakeHerdr) restart() {
	f.shutdown()
	f.instance.Add(1)
	f.listen()
}

func (f *fakeHerdr) serve(t *testing.T, conn net.Conn) {
	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	var req struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		t.Errorf("fake herdr: request is not JSON: %q", line)
		return
	}
	id, _ := json.Marshal(req.ID)
	reply := func(result string) { conn.Write([]byte(`{"id":` + string(id) + `,"result":` + result + "}\n")) }
	switch req.Method {
	case "ping":
		v, _ := f.version.Load().(string)
		if v == "" {
			v = "0.9.5"
		}
		reply(`{"type":"pong","version":"` + v + `","protocol":22}`)
	case "session.snapshot":
		f.snapshots.Add(1)
		snap := strings.ReplaceAll(fakeSnapshot, `"terminal_id":"t`, fmt.Sprintf(`"terminal_id":"i%d-t`, f.instance.Load()))
		if f.blocked.Load() {
			snap = strings.ReplaceAll(snap, `"agent":"claude","agent_status":"working"`, `"agent":"claude","agent_status":"blocked"`)
		}
		reply(snap)
	case "pane.process_info":
		// A child process the test started stands in for the Agent's
		// foreground process; without one, herdr reports none.
		pid := f.procPID.Load()
		if pid == 0 {
			reply(`{"type":"pane_process_info","process_info":{"pane_id":"w1:p1","shell_pid":1,"foreground_processes":[]}}`)
			return
		}
		reply(fmt.Sprintf(`{"type":"pane_process_info","process_info":{"pane_id":"w1:p1","shell_pid":%d,"foreground_process_group_id":%d,`+
			`"foreground_processes":[{"pid":%d,"name":"claude","argv0":"claude","argv":["claude"],"cmdline":"claude","cwd":"/tmp"}]}}`, pid, pid, pid))
	case "events.subscribe":
		f.subscribes.Add(1)
		reply(`{"type":"subscription_started"}`)
		status := bytes.Contains(req.Params, []byte("pane.agent_status_changed"))
		if status {
			f.statusSubs.Add(1)
		}
		f.openSubs.Add(1)
		closed := make(chan struct{})
		go func() {
			r.ReadByte() // until the client closes, or the test ends
			close(closed)
		}()
		if status {
			select {
			case <-f.push:
				f.blocked.Store(true)
				conn.Write([]byte(`{"data":{"agent":"claude","agent_status":"blocked","pane_id":"w1:p1","workspace_id":"w1"},"event":"pane.agent_status_changed"}` + "\n"))
			case <-closed:
			}
		}
		<-closed
		f.openSubs.Add(-1)
	default:
		t.Errorf("fake herdr: unexpected method %q", req.Method)
	}
}

// syncBuffer is a bytes.Buffer safe for one writer and concurrent readers.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// serveArgs runs serve against f with its own data directory, listening on a
// port the kernel picks so no test binds the default 7318.
func serveArgs(f *fakeHerdr, dataDir string) []string {
	return []string{"serve", "--socket", f.path, "--server", "test", "--host", "testhost", "--data-dir", dataDir,
		"--bind", "127.0.0.1:0"}
}

// listenAddr waits for serve's "listening" log entry in dataDir and returns
// its address.
func listenAddr(t *testing.T, dataDir string) string {
	t.Helper()
	var addr string
	waitFor(t, "the listening log entry", func() bool {
		b, _ := os.ReadFile(filepath.Join(dataDir, logFileName))
		for _, line := range strings.Split(string(b), "\n") {
			var entry map[string]any
			if json.Unmarshal([]byte(line), &entry) == nil && entry["msg"] == "listening" {
				addr, _ = entry["addr"].(string)
				return addr != ""
			}
		}
		return false
	})
	return addr
}

// TestServeSignal sends the test process a real SIGINT while serve runs under
// signalContext: serve closes its subscriptions and exits 0.
func TestServeSignal(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, stop := signalContext()
	defer stop()

	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()

	want := "agent      host=testhost epoch=1 pane=w1:p1 program=claude profile=claude status=working"
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), want) })
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	time.Sleep(200 * time.Millisecond) // let the read that follows the subscription finish
	f.push <- struct{}{}
	change := "transition host=testhost epoch=1 pane=w1:p1 program=claude profile=claude status=blocked previous=working provenance=live herdr=0.9.5"
	waitFor(t, "the live transition line", func() bool { return strings.Contains(stdout.String(), change) })
	gate := "gate       herdr=0.9.5 minimum=0.9.5 gate=ok"
	if out := stdout.String(); strings.Count(out, gate) != 1 || strings.Index(out, gate) > strings.Index(out, "agent ") {
		t.Errorf("want one gate line before the Agent line:\n%s", out)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code %d, stderr %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop on SIGINT")
	}
	waitFor(t, "every subscription closed", func() bool { return f.openSubs.Load() == 0 })
	if strings.Contains(stdout.String(), "w1:p2") {
		t.Errorf("a shell pane was printed:\n%s", stdout.String())
	}

	log := filepath.Join(dataDir, logFileName)
	fi, err := os.Stat(log)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %#o, want 0600", fi.Mode().Perm())
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(b), "\n")
	var entry map[string]any
	if err := json.Unmarshal([]byte(first), &entry); err != nil || entry["msg"] != "herddash starting" {
		t.Errorf("first log line %q is not the JSON start entry (%v)", first, err)
	}
	if !strings.Contains(string(b), `"herddash stopped"`) {
		t.Errorf("log has no stop entry:\n%s", b)
	}
}

// shutdownLimit is how long Story 4.1's tests allow serve to stop after a
// signal. It is well under shutdownTimeout, so a stream that would hold
// Shutdown until its timeout fails the test instead of racing it.
const shutdownLimit = 2 * time.Second

// TestServeShutdownAndRestart is Story 4.1: with an /events stream open and a
// live Transition just pushed, a real SIGTERM makes serve end the stream and
// exit 0 well within shutdownTimeout. Shutdown stops the HTTP server without
// timing out and closes the database cleanly. A serve started at once on the
// same data directory and the same address starts cleanly and records a second
// daemon_start marker. Every Transition serve printed is in an intact record.
func TestServeShutdownAndRestart(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, stop := signalContext()
	defer stop()

	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
	addr := listenAddr(t, dataDir)
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), "pane=w1:p1") })
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/events status %d", resp.StatusCode)
	}
	gotState := make(chan struct{})
	ended := make(chan error, 1)
	go func() {
		r := bufio.NewReader(resp.Body)
		seen := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ended <- err
				return
			}
			if !seen && line == "event: state\n" {
				seen = true
				close(gotState)
			}
		}
	}()
	select {
	case <-gotState:
	case <-time.After(5 * time.Second):
		t.Fatal("no state event on /events")
	}

	// A live change goes in just before the signal, so a write may be in
	// progress when shutdown begins.
	f.push <- struct{}{}
	start := time.Now()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code %d after SIGTERM, stderr %q", code, stderr.String())
		}
	case <-time.After(shutdownLimit):
		t.Fatalf("serve did not stop within %v of SIGTERM", shutdownLimit)
	}
	t.Logf("stopped %v after SIGTERM", time.Since(start).Round(time.Millisecond))
	// The server ended the stream: an EOF the client did not cause.
	select {
	case err := <-ended:
		if err != io.EOF {
			t.Errorf("the /events stream ended with %v, want EOF from the server", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the /events stream was still open after serve exited")
	}
	b, err := os.ReadFile(filepath.Join(dataDir, logFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`"http shutdown"`, `"database close"`} {
		if strings.Contains(string(b), bad) {
			t.Errorf("log has %s: shutdown did not finish cleanly:\n%s", bad, b)
		}
	}
	if !strings.Contains(string(b), `"herddash stopped"`) {
		t.Errorf("log has no stop entry:\n%s", b)
	}
	printedBlocked := strings.Contains(stdout.String(), "status=blocked previous=working provenance=live")

	// A restart straight away, on the same address as under launchd or
	// systemd: the lock and the port must both be free.
	args := serveArgs(f, dataDir)
	args[len(args)-1] = addr
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	var stdout2, stderr2 syncBuffer
	done2 := make(chan int, 1)
	go func() { done2 <- run(ctx2, args, &stdout2, &stderr2) }()
	deadline := time.After(5 * time.Second)
	for !strings.Contains(stdout2.String(), "pane=w1:p1") {
		select {
		case code := <-done2:
			t.Fatalf("restarted serve exited %d, stderr %q", code, stderr2.String())
		case <-deadline:
			t.Fatalf("restarted serve showed no Agent; stderr %q", stderr2.String())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel2()
	if code := <-done2; code != 0 {
		t.Fatalf("restarted serve exit code %d, stderr %q", code, stderr2.String())
	}

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "herddash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Errorf("integrity_check %q, %v", integrity, err)
	}
	var starts int
	if err := db.QueryRow(`SELECT count(*) FROM resync_markers WHERE reason = 'daemon_start'`).Scan(&starts); err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Errorf("%d daemon_start markers, want 2 (one per serve)", starts)
	}
	if printedBlocked {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM transitions WHERE pane_id = 'w1:p1' AND new_status = 'blocked'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Error("serve printed the blocked Transition but the record lacks it")
		}
	}
}

// TestHelperServe is TestServeExitCode's child process: it runs main's code
// path, with the arguments in HERDDASH_TEST_SERVE_ARGS, and exits with its
// code.
func TestHelperServe(t *testing.T) {
	raw := os.Getenv("HERDDASH_TEST_SERVE_ARGS")
	if raw == "" {
		t.Skip("run only as TestServeExitCode's child process")
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	os.Exit(mainCode(args))
}

// TestServeExitCode runs serve through main's code path in a child process
// and sends it SIGTERM: the process itself exits 0, which is what launchd and
// systemd act on.
func TestServeExitCode(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	args, err := json.Marshal(serveArgs(f, dataDir))
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestHelperServe$")
	child.Env = append(os.Environ(), "HERDDASH_TEST_SERVE_ARGS="+string(args))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	listenAddr(t, dataDir)
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("child serve: %v (exit code %d)", err, child.ProcessState.ExitCode())
		}
	case <-time.After(shutdownLimit + 3*time.Second):
		child.Process.Kill()
		t.Fatal("child serve did not exit after SIGTERM")
	}
	if code := child.ProcessState.ExitCode(); code != 0 {
		t.Errorf("child serve exit code %d, want 0", code)
	}
}

func TestServeTightensLogMode(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dataDir, logFileName)
	if err := os.WriteFile(log, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(log, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), "pane=w1:p1") })
	cancel()
	<-done
	fi, err := os.Stat(log)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %#o, want 0600", fi.Mode().Perm())
	}
}

// TestServeReconnects restarts the fake herdr under a running serve: serve
// keeps running, prints the reconnect with the incremented epoch, and goes on
// watching the new instance.
func TestServeReconnects(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, filepath.Join(t.TempDir(), "data")), &stdout, &stderr) }()

	agent := "agent      host=testhost epoch=1 pane=w1:p1 program=claude profile=claude status=working"
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), agent) })
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	f.restart()

	waitFor(t, "the reconnected line", func() bool {
		return strings.Contains(stdout.String(), "reconnect  herdr=0.9.5 epoch=2 epoch_change=incremented")
	})
	again := "agent      host=testhost epoch=2 pane=w1:p1 program=claude profile=claude status=working"
	waitFor(t, "the Agent seen again under epoch 2", func() bool { return strings.Contains(stdout.String(), again) })
	waitFor(t, "a status subscription to the new instance", func() bool { return f.statusSubs.Load() >= 2 })
	select {
	case code := <-done:
		t.Fatalf("serve exited %d during the restart, stderr %q", code, stderr.String())
	default:
	}
	out := stdout.String()
	r := strings.Index(out, "reconnect  ")
	rm := strings.Index(out, "removed    host=testhost epoch=1 pane=w1:p1")
	ag := strings.Index(out, again)
	if r < 0 || rm < r || ag < rm {
		t.Errorf("want the reconnect line, the removal under epoch 1, then the Agent under epoch 2:\n%s", out)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
}

// TestNoArgumentsServes checks that "herddash" alone is serve: with no herdr
// to reach, it sets up its data directory and log and keeps trying, until
// stopped, exit 0.
func TestNoArgumentsServes(t *testing.T) {
	home := hermeticEnv(t)
	// The default config file moves the bind off 7318, so this test, too,
	// never binds the default port.
	cfgDir := filepath.Join(home, ".config", "herddash")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("bind = \"127.0.0.1:0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var stdout, stderr syncBuffer
	code := run(ctx, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q; want 0 after waiting for herdr", code, stderr.String())
	}
	b, err := os.ReadFile(filepath.Join(home, ".local", "share", "herddash", logFileName))
	if err != nil {
		t.Fatalf("serve did not set up its data directory and log: %v", err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if msg, _ := entry["msg"].(string); strings.Contains(msg, "herdr not reachable") {
			found = true
			if entry["level"] != "INFO" {
				t.Errorf("failed attempt logged at %v, want INFO: %s", entry["level"], line)
			}
		}
	}
	if !found {
		t.Errorf("log does not record a failed attempt:\n%s", b)
	}
}

func TestPrinter(t *testing.T) {
	var b bytes.Buffer
	p := printer{w: &b}
	key := core.AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"}
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC).UnixMilli()
	prev := core.StatusWorking
	tr, err := core.NewTransition(core.TransitionInput{Key: key, AgentProgram: "claude", Profile: "cldw", Previous: &prev,
		New: core.StatusBlocked, HerdrVersion: "0.8.2", ObservedAt: at + 1500, Provenance: core.ProvenanceLive})
	if err != nil {
		t.Fatal(err)
	}
	p.Agent(engine.Agent{Key: key, AgentProgram: "claude", Profile: "cldw", Status: core.StatusWorking,
		Class: engine.ClassWorking, WaitingSince: at - 12*60*1000 - 59000}, at)
	p.Transition(tr, engine.Agent{Key: key, AgentProgram: "claude", Profile: "cldw", Status: core.StatusBlocked,
		Class: engine.ClassNeedsYou, WaitingSince: at + 1500, WaitingExact: true})
	p.Removed(engine.Agent{Key: key, Status: core.StatusBlocked}, at+3000)
	p.Profile(engine.Agent{Key: key, AgentProgram: "claude", Profile: "cldp", Status: core.StatusBlocked}, "cldw", at+3500)
	p.Profile(engine.Agent{Key: key, AgentProgram: "claude", Profile: "cldp"}, "", at+3600)
	p.Reconnected("0.9.5", 1, true, at+4000)
	p.Reconnected("0.9.5", 2, false, at+5000)
	p.Resynced("events_lost", at+6000)
	p.Gate(engine.GateOK, "0.9.5", at+7000)
	p.Gate(engine.GateRefused, "0.8.2", at+8000)
	p.Gate(engine.GateAdvisory, "", at+9000)
	p.Agent(engine.Agent{Key: key, Status: core.StatusUnknown}, at+10000)
	want := "2026-10-16T12:00:00.000Z agent      host=mac epoch=1 pane=w1:p1 program=claude profile=cldw status=working class=working waiting=≥12m\n" +
		"2026-10-16T12:00:01.500Z transition host=mac epoch=1 pane=w1:p1 program=claude profile=cldw status=blocked previous=working provenance=live herdr=0.8.2 class=needs_you waiting=0s\n" +
		"2026-10-16T12:00:03.000Z removed    host=mac epoch=1 pane=w1:p1 program=- profile=- status=blocked\n" +
		"2026-10-16T12:00:03.500Z profile    host=mac epoch=1 pane=w1:p1 program=claude profile=cldp previous=cldw\n" +
		"2026-10-16T12:00:03.600Z profile    host=mac epoch=1 pane=w1:p1 program=claude profile=cldp previous=-\n" +
		"2026-10-16T12:00:04.000Z reconnect  herdr=0.9.5 epoch=1 epoch_change=kept\n" +
		"2026-10-16T12:00:05.000Z reconnect  herdr=0.9.5 epoch=2 epoch_change=incremented\n" +
		"2026-10-16T12:00:06.000Z resync     cause=events_lost\n" +
		"2026-10-16T12:00:07.000Z gate       herdr=0.9.5 minimum=0.9.5 gate=ok\n" +
		"2026-10-16T12:00:08.000Z gate       herdr=0.8.2 minimum=0.9.5 gate=refused\n" +
		"2026-10-16T12:00:09.000Z gate       herdr=- minimum=0.9.5 gate=advisory\n" +
		"2026-10-16T12:00:10.000Z agent      host=mac epoch=1 pane=w1:p1 program=- profile=- status=unknown class=unknown waiting=-\n"
	if b.String() != want {
		t.Errorf("printed\n%s\nwant\n%s", b.String(), want)
	}
}

// TestServeRefusesUnsupportedHerdr runs serve against a herdr 0.8.2 without
// the override: one gate line, no Agent, no subscription, and serve keeps
// running.
func TestServeRefusesUnsupportedHerdr(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	f.version.Store("0.8.2")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	dataDir := filepath.Join(t.TempDir(), "data")
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()

	gate := "gate       herdr=0.8.2 minimum=0.9.5 gate=refused"
	waitFor(t, "the gate line", func() bool { return strings.Contains(stdout.String(), gate) })
	time.Sleep(200 * time.Millisecond)
	if out := stdout.String(); strings.Count(out, "gate ") != 1 || strings.Contains(out, "agent ") || strings.Contains(out, "transition ") {
		t.Errorf("want only the one gate line from a refused herdr:\n%s", out)
	}
	if n, m := f.subscribes.Load(), f.snapshots.Load(); n != 0 || m != 0 {
		t.Errorf("%d events.subscribe and %d session.snapshot requests reached a refused herdr", n, m)
	}
	select {
	case code := <-done:
		t.Fatalf("serve exited %d, stderr %q", code, stderr.String())
	default:
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
	b, err := os.ReadFile(filepath.Join(dataDir, logFileName))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if entry["level"] == "WARN" && entry["herdr_version"] == "0.8.2" && entry["herdr_minimum"] == "0.9.5" {
			found = true
		}
	}
	if !found {
		t.Errorf("log has no warning naming herdr 0.8.2 and the 0.9.5 minimum:\n%s", b)
	}
}

// TestServeAdvisoryHerdr runs serve against a herdr 0.8.2 with
// --allow-unsupported-herdr: an advisory gate line, then Agents and
// Transitions as usual, each Transition naming herdr 0.8.2.
func TestServeAdvisoryHerdr(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	f.version.Store("0.8.2")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	args := append(serveArgs(f, filepath.Join(t.TempDir(), "data")), "--allow-unsupported-herdr")
	go func() { done <- run(ctx, args, &stdout, &stderr) }()

	waitFor(t, "the gate line", func() bool {
		return strings.Contains(stdout.String(), "gate       herdr=0.8.2 minimum=0.9.5 gate=advisory")
	})
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), "agent      host=testhost epoch=1 pane=w1:p1") })
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	time.Sleep(200 * time.Millisecond) // let the read that follows the subscription finish
	f.push <- struct{}{}
	change := "transition host=testhost epoch=1 pane=w1:p1 program=claude profile=claude status=blocked previous=working provenance=live herdr=0.8.2"
	waitFor(t, "the live transition line", func() bool { return strings.Contains(stdout.String(), change) })
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
}

// TestHelperSleep is TestServeResolvesProfile's child process: it only
// sleeps, and only when run as that child.
func TestHelperSleep(t *testing.T) {
	if os.Getenv("HERDDASH_TEST_SLEEP") == "" {
		t.Skip("run only as TestServeResolvesProfile's child process")
	}
	time.Sleep(30 * time.Second)
}

// TestServeResolvesProfile runs serve with a Profile Map whose claude rule
// reads a dedicated variable. The Agent's foreground process, as the fake
// herdr reports it, is a child this test starts with a controlled
// environment, so the Agent line names the Profile the rule maps it to.
func TestServeResolvesProfile(t *testing.T) {
	hermeticEnv(t)
	// The child is this test binary, not a system binary: macOS withholds
	// an Apple platform binary's environment from kern.procargs2.
	child := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
	child.Env = []string{"HERDDASH_TEST_SLEEP=1", "HERDDASH_TEST_PROFILE=/tmp/herddash-test-profile/"}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		child.Process.Kill()
		child.Wait()
	})
	f := startFakeHerdr(t)
	f.procPID.Store(int32(child.Process.Pid))
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := "[profiles.claude]\nrule = \"env\"\nvariable = \"HERDDASH_TEST_PROFILE\"\nabsent = \"none\"\n" +
		"values = { \"/tmp/herddash-test-profile\" = \"mine\" }\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	dataDir := filepath.Join(t.TempDir(), "data")
	args := append(serveArgs(f, dataDir), "--config", cfg)
	go func() { done <- run(ctx, args, &stdout, &stderr) }()

	want := "agent      host=testhost epoch=1 pane=w1:p1 program=claude profile=mine status=working"
	waitFor(t, "the Agent line with its Profile", func() bool { return strings.Contains(stdout.String(), want) })
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
}

// TestServeTwice runs a second serve on the data directory of a running one:
// the second exits 1 at once, naming the database's lock, and the first goes
// on running.
func TestServeTwice(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), "pane=w1:p1") })

	var stdout2, stderr2 syncBuffer
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if code := run(ctx2, serveArgs(f, dataDir), &stdout2, &stderr2); code != 1 {
		t.Fatalf("second serve exit code %d, want 1; stderr %q", code, stderr2.String())
	}
	lock := filepath.Join(dataDir, "herddash.db.lock")
	if got := stderr2.String(); !strings.HasPrefix(got, "herddash: ") || !strings.Contains(got, lock) {
		t.Errorf("second serve stderr %q, want herddash: <err> naming %s", got, lock)
	}
	if stdout2.String() != "" {
		t.Errorf("second serve printed %q", stdout2.String())
	}
	for _, name := range []string{"herddash.db", "herddash.db.lock"} {
		fi, err := os.Stat(filepath.Join(dataDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %#o, want 0600", name, fi.Mode().Perm())
		}
	}
	select {
	case code := <-done:
		t.Fatalf("first serve exited %d, stderr %q", code, stderr.String())
	default:
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
	b, err := os.ReadFile(filepath.Join(dataDir, logFileName))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), `"herddash starting"`); n != 1 {
		t.Errorf("the running Daemon's log has %d start entries, want 1 (the refused serve wrote to it):\n%s", n, b)
	}
	// serve closed the store on exit.
	st, err := store.Open(filepath.Join(dataDir, "herddash.db"))
	if err != nil {
		t.Fatalf("Open after serve exited: %v", err)
	}
	st.Close()
}

// TestServeDatabaseFlag runs serve with --database outside the data
// directory: the database and its lock are created there. Under a directory
// others can read, serve exits 1 naming that directory.
func TestServeDatabaseFlag(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")

	t.Run("elsewhere", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "elsewhere", "records.db")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var stdout, stderr syncBuffer
		done := make(chan int, 1)
		args := append(serveArgs(f, dataDir), "--database", db)
		go func() { done <- run(ctx, args, &stdout, &stderr) }()
		waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), "pane=w1:p1") })
		for _, p := range []string{db, db + ".lock"} {
			fi, err := os.Stat(p)
			if err != nil {
				t.Errorf("%s: %v", p, err)
				continue
			}
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("%s mode %#o, want 0600", p, fi.Mode().Perm())
			}
		}
		if _, err := os.Stat(filepath.Join(dataDir, "herddash.db")); err == nil {
			t.Error("a database was created in the data directory too")
		}
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("exit code %d, stderr %q", code, stderr.String())
		}
	})

	t.Run("loose parent", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "shared")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr syncBuffer
		args := append(serveArgs(f, dataDir), "--database", filepath.Join(dir, "records.db"))
		if code := run(context.Background(), args, &stdout, &stderr); code != 1 {
			t.Fatalf("exit code %d, want 1; stderr %q", code, stderr.String())
		}
		if got := stderr.String(); !strings.HasPrefix(got, "herddash: ") || !strings.Contains(got, dir) {
			t.Errorf("stderr %q, want herddash: <err> naming %s", got, dir)
		}
	})
}

// TestServeRecords runs serve against the fake herdr twice on one data
// directory, with herdr restarted in between: each run leaves a daemon_start
// marker and a connect marker (the second saying the epoch incremented), every
// Agent's first sight and the live change are rows in the database, and the
// second run resumes from the persisted epoch.
func TestServeRecords(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	serveOnce := func(want string, push bool) {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var stdout, stderr syncBuffer
		done := make(chan int, 1)
		go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
		waitFor(t, want, func() bool { return strings.Contains(stdout.String(), want) })
		if push {
			waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
			// The read that follows the subscription has been served.
			waitFor(t, "the read after the subscription", func() bool { return f.snapshots.Load() >= 2 })
			f.push <- struct{}{}
			waitFor(t, "the live transition line", func() bool { return strings.Contains(stdout.String(), "provenance=live") })
		}
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("exit code %d, stderr %q", code, stderr.String())
		}
	}
	serveOnce("agent      host=testhost epoch=1 pane=w1:p1", true)
	f.blocked.Store(false)
	f.restart()
	serveOnce("agent      host=testhost epoch=2 pane=w1:p1", false)

	path := filepath.Join(dataDir, "herddash.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	history := func(epoch uint64) []string {
		t.Helper()
		trs, err := st.History(context.Background(), core.AgentKey{Host: "testhost", Server: "test", Epoch: epoch, PaneID: "w1:p1"})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tr := range trs {
			prev, ok := tr.Previous()
			if !ok {
				prev = "nil"
			}
			order := "-"
			if o, ok := tr.HerdrOrder(); ok {
				order = fmt.Sprint(o)
			}
			out = append(out, fmt.Sprintf("%s->%s %s seq=%s herdr=%s profile=%s", prev, tr.New(), tr.Provenance(), order, tr.HerdrVersion(), tr.Profile()))
		}
		return out
	}
	if got, want := strings.Join(history(1), "; "),
		"nil->working snapshot seq=3 herdr=0.9.5 profile=claude; working->blocked live seq=- herdr=0.9.5 profile=claude"; got != want {
		t.Errorf("epoch 1 history %q, want %q", got, want)
	}
	if got, want := strings.Join(history(2), "; "), "nil->working snapshot seq=3 herdr=0.9.5 profile=claude"; got != want {
		t.Errorf("epoch 2 history %q, want %q", got, want)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// The markers have no read API yet; read them directly.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT host, server, epoch, reason, detail FROM resync_markers ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var markers []string
	for rows.Next() {
		var host, server, reason string
		var detail sql.NullString
		var epoch int
		if err := rows.Scan(&host, &server, &epoch, &reason, &detail); err != nil {
			t.Fatal(err)
		}
		d := "NULL"
		if detail.Valid {
			d = detail.String
		}
		markers = append(markers, fmt.Sprintf("%s/%s %d %s %s", host, server, epoch, reason, d))
	}
	if got, want := strings.Join(markers, "; "), "testhost/test 1 daemon_start NULL; testhost/test 1 connect NULL; "+
		"testhost/test 1 daemon_start NULL; testhost/test 2 connect epoch incremented"; got != want {
		t.Errorf("markers %q, want %q", got, want)
	}
}

func TestCompact(t *testing.T) {
	const s, m, h, d = 1000, 60 * 1000, 3600 * 1000, 24 * 3600 * 1000
	for _, c := range []struct {
		ms   int64
		want string
	}{
		{0, "0s"}, {999, "0s"}, {45 * s, "45s"}, {m, "1m"}, {12*m + 30*s, "12m"},
		{h, "1h00m"}, {h + 5*m + 59*s, "1h05m"}, {23*h + 59*m, "23h59m"},
		{d, "1d00h"}, {2*d + 3*h + 40*m, "2d03h"}, {3*d + 23*h, "3d23h"}, {400 * d, "400d00h"},
	} {
		if got := compact(c.ms); got != c.want {
			t.Errorf("compact(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
	// A line timestamped before the class began never prints a negative wait.
	a := engine.Agent{Class: engine.ClassNeedsYou, WaitingSince: 5000}
	if got := waiting(a, 1000); got != "class=needs_you waiting=≥0s" {
		t.Errorf("waiting %q", got)
	}
}

// TestServeRestartKeepsWaiting runs serve twice on one data directory and one
// herdr that does not restart. The first run sees w1:p1 go blocked live. The
// second finds it still blocked: it writes no second first-sight row, and its
// Agent line shows the Waiting Time from the live row, exact.
func TestServeRestartKeepsWaiting(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	serveOnce := func(want string, push bool) string {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var stdout, stderr syncBuffer
		done := make(chan int, 1)
		go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
		waitFor(t, want, func() bool { return strings.Contains(stdout.String(), want) })
		if push {
			waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
			waitFor(t, "the read after the subscription", func() bool { return f.snapshots.Load() >= 2 })
			f.push <- struct{}{}
			waitFor(t, "the live transition line", func() bool { return strings.Contains(stdout.String(), "provenance=live") })
		}
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("exit code %d, stderr %q", code, stderr.String())
		}
		return stdout.String()
	}
	first := serveOnce("agent      host=testhost epoch=1 pane=w1:p1", true)
	if !strings.Contains(first, "status=working class=working waiting=≥0s") {
		t.Errorf("first run's Agent line is not a lower bound from its first sight:\n%s", first)
	}
	if !strings.Contains(first, "provenance=live herdr=0.9.5 class=needs_you waiting=0s") {
		t.Errorf("first run's transition line is not exact from itself:\n%s", first)
	}
	second := serveOnce("agent      host=testhost epoch=1 pane=w1:p1", false)
	var line string
	for l := range strings.SplitSeq(second, "\n") {
		if strings.Contains(l, " agent ") {
			line = l
		}
	}
	if !strings.Contains(line, "status=blocked class=needs_you waiting=") || strings.Contains(line, "≥") {
		t.Errorf("second run's Agent line %q, want needs_you with an exact Waiting Time", line)
	}

	st, err := store.Open(filepath.Join(dataDir, "herddash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	trs, err := st.History(context.Background(), core.AgentKey{Host: "testhost", Server: "test", Epoch: 1, PaneID: "w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trs) != 2 {
		t.Fatalf("%d rows for w1:p1, want the first sight and the live change only", len(trs))
	}
	f1 := core.Fold(trs)
	if f1.Class != core.ClassNeedsYou || f1.SinceProvenance != core.ProvenanceLive || f1.Since != trs[1].ObservedAt() {
		t.Errorf("fold %+v, want needs_you since the live row", f1)
	}
	// The printed Waiting Time runs from the live row to the line's own time.
	at, err := time.Parse("2006-01-02T15:04:05.000Z", strings.Fields(line)[0])
	if err != nil {
		t.Fatal(err)
	}
	gap := at.UnixMilli() - trs[1].ObservedAt()
	if want := "waiting=" + compact(gap); gap < 0 || !strings.HasSuffix(line, want) {
		t.Errorf("second run's Agent line %q, want it to end %q (%d ms after the live row)", line, want, gap)
	}
}

func TestServeSweepsRetention(t *testing.T) {
	// serve passes retention_days (default 90) to the engine: the first
	// connection's sweep deletes a gone Agent's 100-day-old row and an old
	// marker, and keeps the recent ones.
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "herddash.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	const day = 24 * 3600 * 1000
	for i, at := range []int64{now - 100*day, now - 10*day} {
		tr, err := core.NewTransition(core.TransitionInput{
			Key:          core.AgentKey{Host: "testhost", Server: "test", Epoch: 9, PaneID: fmt.Sprintf("w9:p%d", i)},
			AgentProgram: "claude", Profile: "claude", New: core.StatusDone, HerdrVersion: "0.9.5",
			ObservedAt: at, Provenance: core.ProvenanceLive})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Append(tr); err != nil {
			t.Fatal(err)
		}
		if err := st.AppendResyncMarker(store.ResyncMarker{Host: "testhost", Server: "test", Epoch: 9,
			ObservedAt: at, Reason: store.ReasonDaemonStart}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
	waitFor(t, "the agent line", func() bool { return strings.Contains(stdout.String(), "agent      host=testhost") })
	logPath := filepath.Join(dataDir, "herddash.log")
	waitFor(t, "the sweep logged", func() bool {
		b, _ := os.ReadFile(logPath)
		return strings.Contains(string(b), `"msg":"retention sweep","cutoff"`)
	})
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// What is left of the seeded rows, in each table: only the 10-day ones.
	for _, table := range []string{"transitions", "resync_markers"} {
		rows, err := db.Query(`SELECT observed_at FROM ` + table + ` WHERE epoch = 9 ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		var left []int64
		for rows.Next() {
			var at int64
			if err := rows.Scan(&at); err != nil {
				t.Fatal(err)
			}
			left = append(left, at)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if len(left) != 1 || left[0] != now-10*day {
			t.Errorf("%s seeded rows left at %v, want only the 10-day one at %d", table, left, now-10*day)
		}
	}
}

// TestServeAPI runs serve against the fake herdr on a kernel-picked port,
// reads the address from the log and GETs /api/state: the fake's Agent is
// there with the connection connected. A non-loopback Host is refused. stdout
// names the URL, and once serve returns its listener is closed.
func TestServeAPI(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
	addr := listenAddr(t, dataDir)
	if host, _, err := net.SplitHostPort(addr); err != nil || host != "127.0.0.1" {
		t.Fatalf("listening on %q, want 127.0.0.1:<port>", addr)
	}
	waitFor(t, "the listening line", func() bool {
		return strings.Contains(stdout.String(), " listening  http://"+addr+"\n")
	})

	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	type state struct {
		RunID  string `json:"run_id"`
		Seq    uint64 `json:"seq"`
		Agents []struct {
			Key          core.AgentKey `json:"key"`
			AgentProgram string        `json:"agent_program"`
			Status       string        `json:"status"`
		} `json:"agents"`
		Connection struct {
			Connected bool   `json:"connected"`
			Gate      string `json:"gate"`
		} `json:"connection"`
	}
	var got state
	// Every failure is transient here: retry until waitFor gives up.
	waitFor(t, "the Agent in /api/state", func() bool {
		resp, err := client.Get("http://" + addr + "/api/state")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
			return false
		}
		got = state{}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			return false
		}
		return got.Connection.Connected && len(got.Agents) == 1
	})
	a := got.Agents[0]
	want := core.AgentKey{Host: "testhost", Server: "test", Epoch: 1, PaneID: "w1:p1"}
	if a.Key != want || a.AgentProgram != "claude" || a.Status != "working" {
		t.Errorf("agent %+v, want %+v claude working", a, want)
	}
	if got.RunID == "" || got.Seq == 0 || got.Connection.Gate != "ok" {
		t.Errorf("state run_id %q seq %d gate %q", got.RunID, got.Seq, got.Connection.Gate)
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Host evil.example: status %d, want 403", resp.StatusCode)
	}

	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
	tr.CloseIdleConnections()
	if c, err := net.Dial("tcp", addr); err == nil {
		c.Close()
		t.Errorf("a dial to %s succeeded after serve returned; the listener is still open", addr)
	}
}

// TestServeHTTPFailure closes the listener under the running HTTP server: serve
// prints herddash: <err>, stops the engine and exits 1 rather than running on
// without its API.
func TestServeHTTPFailure(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	var ln net.Listener
	listened := make(chan struct{})
	listen = func(network, address string) (net.Listener, error) {
		l, err := net.Listen(network, address)
		ln = l
		close(listened)
		return l, err
	}
	t.Cleanup(func() { listen = net.Listen })
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), "agent      host=testhost") })
	<-listened
	ln.Close()
	select {
	case code := <-done:
		if code != 1 {
			t.Fatalf("exit code %d, want 1; stderr %q", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve went on running after its HTTP server failed")
	}
	if got := stderr.String(); !strings.HasPrefix(got, "herddash: ") {
		t.Errorf("stderr %q, want herddash: <err>", got)
	}
	waitFor(t, "every subscription closed", func() bool { return f.openSubs.Load() == 0 })
}

// TestServePortInUse runs serve with --bind on an address already listening:
// it exits 1 with herddash: <err> before the engine starts.
func TestServePortInUse(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	dataDir := filepath.Join(t.TempDir(), "data")
	args := append(serveArgs(f, dataDir), "--bind", taken.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stdout, stderr syncBuffer
	if code := run(ctx, args, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code %d, want 1; stderr %q", code, stderr.String())
	}
	if got := stderr.String(); !strings.HasPrefix(got, "herddash: ") || !strings.Contains(got, taken.Addr().String()) {
		t.Errorf("stderr %q, want herddash: <err> naming %s", got, taken.Addr())
	}
	if stdout.String() != "" {
		t.Errorf("serve printed %q; the engine started", stdout.String())
	}
	if n := f.snapshots.Load() + f.subscribes.Load(); n != 0 {
		t.Errorf("herdr was asked %d times; the engine started", n)
	}
}

// TestServeEvents runs serve against the fake herdr and opens /events: the
// stream starts with state, and a live status change arrives as a transition
// event within one second. Then, with the stream still open, ctx ends: serve
// returns well before shutdownTimeout, because Shutdown ends the stream.
func TestServeEvents(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
	addr := listenAddr(t, dataDir)
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	waitFor(t, "the read after the subscription", func() bool { return f.snapshots.Load() >= 2 })

	reqCtx, reqCancel := context.WithCancel(context.Background())
	defer reqCancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, Content-Type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	type event struct{ name, data string }
	events := make(chan event, 100)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		r := bufio.NewReader(resp.Body)
		var e event
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSuffix(line, "\n")
			switch {
			case line == "":
				select {
				case events <- e:
				case <-reqCtx.Done():
					return
				}
				e = event{}
			case strings.HasPrefix(line, "event: "):
				e.name = line[len("event: "):]
			case strings.HasPrefix(line, "data: "):
				e.data = line[len("data: "):]
			}
		}
	}()
	select {
	case e := <-events:
		if e.name != "state" || !strings.Contains(e.data, `"pane_id":"w1:p1"`) {
			t.Fatalf("first event %q %s, want state with the fake's Agent", e.name, e.data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no state event")
	}

	start := time.Now()
	select {
	case f.push <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("the fake herdr did not take the push")
	}
	deadline := time.After(time.Second)
	for got := false; !got; {
		select {
		case e := <-events:
			if e.name != "transition" {
				continue
			}
			got = true
			for _, s := range []string{`"pane_id":"w1:p1"`, `"previous":"working"`, `"new":"blocked"`, `"provenance":"live"`, `"attention_class":"needs_you"`} {
				if !strings.Contains(e.data, s) {
					t.Errorf("transition %s lacks %s", e.data, s)
				}
			}
		case <-deadline:
			t.Fatalf("no transition event within 1 s of the live change")
		}
	}
	t.Logf("transition after %v", time.Since(start))

	start = time.Now()
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code %d, stderr %q", code, stderr.String())
		}
	case <-time.After(shutdownTimeout):
		t.Fatal("serve did not return within shutdownTimeout with /events open")
	}
	t.Logf("shutdown took %v", time.Since(start))
	if d := time.Since(start); d > shutdownTimeout/2 {
		t.Errorf("serve took %v to return with /events open", d)
	}
	// Drain events while waiting, so the reader never blocks on a full channel.
	timeout := time.After(5 * time.Second)
	for open := true; open; {
		select {
		case <-ended:
			open = false
		case <-events:
		case <-timeout:
			t.Error("the stream is still open after serve returned")
			open = false
		}
	}
}

// TestServeHistory runs serve against the fake herdr, records a live change,
// and reads the Agent's history over HTTP: the first sight and the live row,
// in order, read from the opened store. An unknown key gets 404.
func TestServeHistory(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()
	addr := listenAddr(t, dataDir)
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	waitFor(t, "the read after the subscription", func() bool { return f.snapshots.Load() >= 2 })
	select {
	case f.push <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("the fake herdr did not take the push")
	}
	waitFor(t, "the live transition line", func() bool { return strings.Contains(stdout.String(), "provenance=live") })

	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	get := func(path string) (int, []byte) {
		t.Helper()
		resp, err := client.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}

	code, body := get("/api/agents/testhost/test/1/w1%3Ap1/history")
	if code != http.StatusOK {
		t.Fatalf("status %d, body %s", code, body)
	}
	var got struct {
		Key         core.AgentKey `json:"key"`
		Transitions []struct {
			Previous   string `json:"previous"`
			New        string `json:"new"`
			Provenance string `json:"provenance"`
			Profile    string `json:"profile"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if want := (core.AgentKey{Host: "testhost", Server: "test", Epoch: 1, PaneID: "w1:p1"}); got.Key != want {
		t.Errorf("key %+v, want %+v", got.Key, want)
	}
	var rows []string
	for _, r := range got.Transitions {
		rows = append(rows, fmt.Sprintf("%s->%s %s %s", r.Previous, r.New, r.Provenance, r.Profile))
	}
	if g, w := strings.Join(rows, "; "), "->working snapshot claude; working->blocked live claude"; g != w {
		t.Errorf("history %q, want %q", g, w)
	}

	if code, body := get("/api/agents/testhost/test/1/w9:p9/history"); code != http.StatusNotFound || !strings.Contains(string(body), `"not_found"`) {
		t.Errorf("unknown key: status %d, body %s", code, body)
	}

	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr.String())
	}
}
