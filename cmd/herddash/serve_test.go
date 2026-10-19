package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
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

func serveArgs(f *fakeHerdr, dataDir string) []string {
	return []string{"serve", "--socket", f.path, "--server", "test", "--host", "testhost", "--data-dir", dataDir}
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
	p.Agent(engine.Agent{Key: key, AgentProgram: "claude", Profile: "cldw", Status: core.StatusWorking}, at)
	p.Transition(tr)
	p.Removed(engine.Agent{Key: key, Status: core.StatusBlocked}, at+3000)
	p.Profile(engine.Agent{Key: key, AgentProgram: "claude", Profile: "cldp", Status: core.StatusBlocked}, "cldw", at+3500)
	p.Profile(engine.Agent{Key: key, AgentProgram: "claude", Profile: "cldp"}, "", at+3600)
	p.Reconnected("0.9.5", 1, true, at+4000)
	p.Reconnected("0.9.5", 2, false, at+5000)
	p.Resynced("events_lost", at+6000)
	p.Gate(engine.GateOK, "0.9.5", at+7000)
	p.Gate(engine.GateRefused, "0.8.2", at+8000)
	p.Gate(engine.GateAdvisory, "", at+9000)
	want := "2026-10-16T12:00:00.000Z agent      host=mac epoch=1 pane=w1:p1 program=claude profile=cldw status=working\n" +
		"2026-10-16T12:00:01.500Z transition host=mac epoch=1 pane=w1:p1 program=claude profile=cldw status=blocked previous=working provenance=live herdr=0.8.2\n" +
		"2026-10-16T12:00:03.000Z removed    host=mac epoch=1 pane=w1:p1 program=- profile=- status=blocked\n" +
		"2026-10-16T12:00:03.500Z profile    host=mac epoch=1 pane=w1:p1 program=claude profile=cldp previous=cldw\n" +
		"2026-10-16T12:00:03.600Z profile    host=mac epoch=1 pane=w1:p1 program=claude profile=cldp previous=-\n" +
		"2026-10-16T12:00:04.000Z reconnect  herdr=0.9.5 epoch=1 epoch_change=kept\n" +
		"2026-10-16T12:00:05.000Z reconnect  herdr=0.9.5 epoch=2 epoch_change=incremented\n" +
		"2026-10-16T12:00:06.000Z resync     cause=events_lost\n" +
		"2026-10-16T12:00:07.000Z gate       herdr=0.9.5 minimum=0.9.5 gate=ok\n" +
		"2026-10-16T12:00:08.000Z gate       herdr=0.8.2 minimum=0.9.5 gate=refused\n" +
		"2026-10-16T12:00:09.000Z gate       herdr=- minimum=0.9.5 gate=advisory\n"
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
