package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
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
		reply(`{"type":"pong","version":"0.9.5","protocol":22}`)
	case "session.snapshot":
		snap := strings.ReplaceAll(fakeSnapshot, `"terminal_id":"t`, fmt.Sprintf(`"terminal_id":"i%d-t`, f.instance.Load()))
		if f.blocked.Load() {
			snap = strings.ReplaceAll(snap, `"agent":"claude","agent_status":"working"`, `"agent":"claude","agent_status":"blocked"`)
		}
		reply(snap)
	case "events.subscribe":
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

	want := "agent      host=testhost epoch=1 pane=w1:p1 program=claude status=working"
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), want) })
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	time.Sleep(200 * time.Millisecond) // let the read that follows the subscription finish
	f.push <- struct{}{}
	change := "transition host=testhost epoch=1 pane=w1:p1 program=claude status=blocked previous=working provenance=live"
	waitFor(t, "the live transition line", func() bool { return strings.Contains(stdout.String(), change) })
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

	agent := "agent      host=testhost epoch=1 pane=w1:p1 program=claude status=working"
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), agent) })
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	f.restart()

	waitFor(t, "the reconnected line", func() bool {
		return strings.Contains(stdout.String(), "reconnect  herdr=0.9.5 epoch=2 epoch_change=incremented")
	})
	again := "agent      host=testhost epoch=2 pane=w1:p1 program=claude status=working"
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
	tr, err := core.NewTransition(core.TransitionInput{Key: key, AgentProgram: "claude", Previous: &prev,
		New: core.StatusBlocked, ObservedAt: at + 1500, Provenance: core.ProvenanceLive})
	if err != nil {
		t.Fatal(err)
	}
	p.Agent(engine.Agent{Key: key, AgentProgram: "claude", Status: core.StatusWorking}, at)
	p.Transition(tr)
	p.Removed(engine.Agent{Key: key, Status: core.StatusBlocked}, at+3000)
	p.Reconnected("0.9.5", 1, true, at+4000)
	p.Reconnected("0.9.5", 2, false, at+5000)
	p.Resynced("events_lost", at+6000)
	want := "2026-10-16T12:00:00.000Z agent      host=mac epoch=1 pane=w1:p1 program=claude status=working\n" +
		"2026-10-16T12:00:01.500Z transition host=mac epoch=1 pane=w1:p1 program=claude status=blocked previous=working provenance=live\n" +
		"2026-10-16T12:00:03.000Z removed    host=mac epoch=1 pane=w1:p1 program=- status=blocked\n" +
		"2026-10-16T12:00:04.000Z reconnect  herdr=0.9.5 epoch=1 epoch_change=kept\n" +
		"2026-10-16T12:00:05.000Z reconnect  herdr=0.9.5 epoch=2 epoch_change=incremented\n" +
		"2026-10-16T12:00:06.000Z resync     cause=events_lost\n"
	if b.String() != want {
		t.Errorf("printed\n%s\nwant\n%s", b.String(), want)
	}
}
