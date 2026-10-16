package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
// it, except that with lifecycleLost it closes the lifecycle subscription right
// after confirming it. No test here dials a real herdr.
type fakeHerdr struct {
	path          string
	lifecycleLost bool
	openSubs      atomic.Int32 // subscriptions the client has not closed
	statusSubs    atomic.Int32 // status subscriptions ever started
	// push makes every open status subscription send one change to blocked,
	// which later snapshots then report too.
	push    chan struct{}
	blocked atomic.Bool
}

const fakeSnapshot = `{"type":"session_snapshot","snapshot":{"version":"0.9.5","protocol":22,` +
	`"workspaces":[{"workspace_id":"w1","number":1,"label":"probe","focused":true,"pane_count":2,"tab_count":1,"active_tab_id":"w1:t1","agent_status":"working"}],` +
	`"tabs":[{"tab_id":"w1:t1","workspace_id":"w1","number":1,"label":"1","focused":true,"pane_count":2,"agent_status":"working"}],` +
	`"panes":[{"pane_id":"w1:p1","terminal_id":"t1","workspace_id":"w1","tab_id":"w1:t1","focused":true,"agent":"claude","agent_status":"working","revision":0},` +
	`{"pane_id":"w1:p2","terminal_id":"t2","workspace_id":"w1","tab_id":"w1:t1","focused":false,"agent_status":"unknown","revision":0}],` +
	`"layouts":[],` +
	`"agents":[{"terminal_id":"t1","agent":"claude","agent_status":"working","workspace_id":"w1","tab_id":"w1:t1","pane_id":"w1:p1","focused":true,"state_change_seq":3,"revision":0}]}}`

func startFakeHerdr(t *testing.T, lifecycleLost bool) *fakeHerdr {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeHerdr{path: filepath.Join(dir, "herdr.sock"), lifecycleLost: lifecycleLost, push: make(chan struct{})}
	ln, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				f.serve(t, conn)
			}()
		}
	}()
	return f
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
		snap := fakeSnapshot
		if f.blocked.Load() {
			snap = strings.ReplaceAll(snap, `"agent":"claude","agent_status":"working"`, `"agent":"claude","agent_status":"blocked"`)
		}
		reply(snap)
	case "events.subscribe":
		reply(`{"type":"subscription_started"}`)
		status := bytes.Contains(req.Params, []byte("pane.agent_status_changed"))
		if !status && f.lifecycleLost {
			return
		}
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
	f := startFakeHerdr(t, false)
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx, stop := signalContext()
	defer stop()

	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, serveArgs(f, dataDir), &stdout, &stderr) }()

	want := "agent      host=testhost pane=w1:p1 program=claude status=working"
	waitFor(t, "the Agent line", func() bool { return strings.Contains(stdout.String(), want) })
	waitFor(t, "the status subscription", func() bool { return f.statusSubs.Load() >= 1 })
	time.Sleep(200 * time.Millisecond) // let the read that follows the subscription finish
	f.push <- struct{}{}
	change := "transition host=testhost pane=w1:p1 program=claude status=blocked previous=working provenance=live"
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
	f := startFakeHerdr(t, true)
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
	run(context.Background(), serveArgs(f, dataDir), &stdout, &stderr)
	fi, err := os.Stat(log)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %#o, want 0600", fi.Mode().Perm())
	}
}

func TestServeLifecycleLost(t *testing.T) {
	hermeticEnv(t)
	f := startFakeHerdr(t, true)
	var stdout, stderr syncBuffer
	code := run(context.Background(), serveArgs(f, filepath.Join(t.TempDir(), "data")), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	if s := stderr.String(); !strings.Contains(s, "lifecycle") || !strings.Contains(s, "closed") {
		t.Errorf("stderr %q does not name the lost lifecycle stream", s)
	}
}

// TestNoArgumentsServes checks that "herddash" alone is serve: with no herdr
// to reach, it fails to start, exit 1, naming the cause.
func TestNoArgumentsServes(t *testing.T) {
	home := hermeticEnv(t)
	var stdout, stderr syncBuffer
	code := run(context.Background(), nil, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "ping herdr") {
		t.Fatalf("exit code %d, stderr %q; want 1 naming the failed ping", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "herddash", logFileName)); err != nil {
		t.Errorf("serve did not set up its data directory and log: %v", err)
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
	want := "2026-10-16T12:00:00.000Z agent      host=mac pane=w1:p1 program=claude status=working\n" +
		"2026-10-16T12:00:01.500Z transition host=mac pane=w1:p1 program=claude status=blocked previous=working provenance=live\n" +
		"2026-10-16T12:00:03.000Z removed    host=mac pane=w1:p1 program=- status=blocked\n"
	if b.String() != want {
		t.Errorf("printed\n%s\nwant\n%s", b.String(), want)
	}
}
