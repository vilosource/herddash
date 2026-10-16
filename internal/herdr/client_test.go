package herdr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr/gen"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// oneShot answers every request with line and then waits for the client to
// close, asserting the method it was asked.
func oneShot(t *testing.T, method, line string) *fakeServer {
	t.Helper()
	return newFake(t, func(fc *fakeConn) {
		if fc.req.Method != method {
			fc.t.Errorf("method %q, want %q", fc.req.Method, method)
		}
		if len(fc.req.Params) == 0 {
			fc.t.Errorf("request has no params: %s", fc.raw)
		}
		fc.reply(line)
		fc.expectClientClose()
	})
}

// assertConnections checks that n calls used n connections, each closed by
// the client.
func assertConnections(t *testing.T, s *fakeServer, n int32) {
	t.Helper()
	// The fake counts the client's close on its own goroutine; give it a moment.
	deadline := time.Now().Add(5 * time.Second)
	for s.clientClosed.Load() < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := s.conns.Load(); got != n {
		t.Errorf("connections = %d, want %d", got, n)
	}
	if got := s.clientClosed.Load(); got != n {
		t.Errorf("connections closed by the client = %d, want %d", got, n)
	}
}

func TestPing(t *testing.T) {
	s := oneShot(t, "ping", fixture(t, "ping.json"))
	v, err := s.client().Ping(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if v != "0.9.5" {
		t.Errorf("version %q, want 0.9.5", v)
	}
	assertConnections(t, s, 1)
}

func TestOneConnectionPerCall(t *testing.T) {
	s := oneShot(t, "ping", fixture(t, "ping.json"))
	c := s.client()
	for range 3 {
		if _, err := c.Ping(testContext(t)); err != nil {
			t.Fatal(err)
		}
	}
	assertConnections(t, s, 3)
}

func TestSnapshot(t *testing.T) {
	s := oneShot(t, "session.snapshot", fixture(t, "snapshot.json"))
	snap, err := s.client().Snapshot(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	assertConnections(t, s, 1)
	if snap.HerdrVersion != "0.9.5" {
		t.Errorf("version %q", snap.HerdrVersion)
	}
	want := []struct{ id, terminal, workspace, tab string }{
		{"w1:p1", "term_65df2e06a79ca1", "w1", "w1:t1"},
		{"w1:p3", "term_65df2e161c7cd4", "w1", "w1:t1"},
		{"w2:p1", "term_65df2e161b2853", "w2", "w2:t1"},
	}
	if len(snap.Panes) != len(want) {
		t.Fatalf("%d panes, want %d", len(snap.Panes), len(want))
	}
	for i, w := range want {
		p := snap.Panes[i]
		if p.PaneID != w.id || p.TerminalID != w.terminal || p.WorkspaceID != w.workspace || p.TabID != w.tab {
			t.Errorf("pane %d = %+v, want %+v", i, p, w)
		}
	}
	a := snap.Panes[0].Agent
	if a == nil {
		t.Fatal("w1:p1 has no agent")
	}
	if a.AgentProgram != "claude" || a.Status != core.StatusWorking || a.StateChangeSeq == nil || *a.StateChangeSeq != 9 {
		t.Errorf("agent %+v seq %v", a, a.StateChangeSeq)
	}
	for _, p := range snap.Panes[1:] {
		if p.Agent != nil {
			t.Errorf("%s has agent %+v, want none", p.PaneID, p.Agent)
		}
	}
}

func TestForegroundProcess(t *testing.T) {
	gotPane := make(chan string, 1)
	line := fixture(t, "process_info.json")
	s := newFake(t, func(fc *fakeConn) {
		var p struct {
			PaneID string `json:"pane_id"`
		}
		json.Unmarshal(fc.req.Params, &p)
		gotPane <- p.PaneID
		if fc.req.Method != "pane.process_info" {
			fc.t.Errorf("method %q", fc.req.Method)
		}
		fc.reply(line)
		fc.expectClientClose()
	})
	fp, err := s.client().ForegroundProcess(testContext(t), "w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	assertConnections(t, s, 1)
	if p := <-gotPane; p != "w1:p1" {
		t.Errorf("pane_id sent %q", p)
	}
	if fp.PID != 43008 || fp.Name != "sleep" || fp.Argv0 != "sleep" {
		t.Errorf("foreground %+v", fp)
	}
}

func TestMapForegroundProcess(t *testing.T) {
	u32 := func(v uint32) *uint32 { return &v }
	str := func(v string) *string { return &v }
	procs := []gen.PaneProcessInfoProcess{
		{PID: 10, Name: "bash", Argv0: str("-bash")},
		{PID: 20, Name: "sleep", Argv0: str("claude")},
	}
	tests := []struct {
		name    string
		in      gen.PaneProcessInfo
		want    core.ForegroundProcess
		wantErr bool
	}{
		{name: "group leader", in: gen.PaneProcessInfo{ForegroundProcessGroupID: u32(20), ForegroundProcesses: procs},
			want: core.ForegroundProcess{PID: 20, Name: "sleep", Argv0: "claude"}},
		{name: "no leader listed", in: gen.PaneProcessInfo{ForegroundProcessGroupID: u32(99), ForegroundProcesses: procs},
			want: core.ForegroundProcess{PID: 10, Name: "bash", Argv0: "-bash"}},
		{name: "no group id", in: gen.PaneProcessInfo{ForegroundProcesses: procs[1:]},
			want: core.ForegroundProcess{PID: 20, Name: "sleep", Argv0: "claude"}},
		{name: "no argv0", in: gen.PaneProcessInfo{ForegroundProcesses: []gen.PaneProcessInfoProcess{{PID: 5, Name: "x"}}},
			want: core.ForegroundProcess{PID: 5, Name: "x"}},
		{name: "none", in: gen.PaneProcessInfo{ShellPID: u32(10)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapForegroundProcess(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrNoForegroundProcess) {
					t.Fatalf("err %v, want ErrNoForegroundProcess", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestForegroundProcessNone(t *testing.T) {
	s := oneShot(t, "pane.process_info",
		`{"id":"r1","result":{"type":"pane_process_info","process_info":{"pane_id":"w1:p1","shell_pid":1}}}`)
	_, err := s.client().ForegroundProcess(testContext(t), "w1:p1")
	if !errors.Is(err, ErrNoForegroundProcess) {
		t.Fatalf("err %v, want ErrNoForegroundProcess", err)
	}
}

func TestHerdrErrorIsTyped(t *testing.T) {
	s := oneShot(t, "pane.process_info", `{"id":"x","error":{"code":"pane_not_found","message":"pane not found"}}`)
	_, err := s.client().ForegroundProcess(testContext(t), "w9:p9")
	var herr *Error
	if !errors.As(err, &herr) {
		t.Fatalf("err %v (%T), want *Error", err, err)
	}
	if herr.Code != "pane_not_found" || herr.Message != "pane not found" || herr.Method != "pane.process_info" {
		t.Errorf("error %+v", herr)
	}
}

func TestNoResponseHonoursContext(t *testing.T) {
	s := newFake(t, func(fc *fakeConn) { fc.hold() })
	c := s.client()
	calls := map[string]func(context.Context) error{
		"ping":     func(ctx context.Context) error { _, err := c.Ping(ctx); return err },
		"snapshot": func(ctx context.Context) error { _, err := c.Snapshot(ctx); return err },
		"process":  func(ctx context.Context) error { _, err := c.ForegroundProcess(ctx, "w1:p1"); return err },
		"lifecycle": func(ctx context.Context) error {
			_, err := c.SubscribeLifecycle(ctx)
			return err
		},
		"status": func(ctx context.Context) error {
			_, err := c.SubscribeStatus(ctx, "w1:p1")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := call(ctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("err %v, want context.DeadlineExceeded", err)
			}
			if d := time.Since(start); d > 5*time.Second {
				t.Errorf("call took %v", d)
			}
		})
	}
}

func TestNoDeadlineUsesDefaultTimeout(t *testing.T) {
	saved := callTimeout
	callTimeout = 100 * time.Millisecond
	t.Cleanup(func() { callTimeout = saved })

	s := newFake(t, func(fc *fakeConn) { fc.hold() })
	c := s.client()
	calls := map[string]func() error{
		"ping": func() error { _, err := c.Ping(context.Background()); return err },
		"status": func() error {
			_, err := c.SubscribeStatus(context.Background(), "w1:p1")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- call() }()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("err %v, want context.DeadlineExceeded", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("call with no deadline hung on a silent server")
			}
		})
	}
}

func TestSnapshotLongLine(t *testing.T) {
	// A snapshot well past bufio's 4096-byte default buffer.
	var panes []string
	for i := range 200 {
		panes = append(panes, fmt.Sprintf(`{"pane_id":"w1:p%d","terminal_id":"term_%040d","workspace_id":"w1","tab_id":"w1:t1","focused":false,"agent_status":"unknown","revision":0}`, i, i))
	}
	line := `{"id":"r1","result":{"type":"session_snapshot","snapshot":{"version":"0.9.5","protocol":22,"workspaces":[],"tabs":[],"layouts":[],"agents":[],"panes":[` +
		strings.Join(panes, ",") + `]}}}`
	if len(line) <= 4096 {
		t.Fatalf("line is only %d bytes", len(line))
	}
	s := oneShot(t, "session.snapshot", line)
	snap, err := s.client().Snapshot(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Panes) != 200 || snap.Panes[199].PaneID != "w1:p199" {
		t.Errorf("%d panes", len(snap.Panes))
	}

	big := bytes.Repeat([]byte("x"), maxLine+1)
	if _, err := readLine(bufio.NewReader(bytes.NewReader(append(big, '\n')))); !errors.Is(err, errLineTooLong) {
		t.Errorf("readLine over maxLine: err %v, want errLineTooLong", err)
	}
}

func TestErrorResponseID(t *testing.T) {
	t.Run("empty id is a herdr error", func(t *testing.T) {
		s := newFake(t, func(fc *fakeConn) {
			fc.send(`{"id":"","error":{"code":"invalid_request","message":"no id"}}`)
		})
		_, err := s.client().Ping(testContext(t))
		var herr *Error
		if !errors.As(err, &herr) || herr.Code != "invalid_request" {
			t.Errorf("err %v, want *Error invalid_request", err)
		}
	})
	t.Run("wrong id is a decode error", func(t *testing.T) {
		s := newFake(t, func(fc *fakeConn) {
			fc.send(`{"id":"other","error":{"code":"invalid_request","message":"not yours"}}`)
		})
		_, err := s.client().Ping(testContext(t))
		var herr *Error
		if err == nil || errors.As(err, &herr) {
			t.Errorf("err %v, want a non-*Error decode error", err)
		}
	})
}

func TestMapSnapshot(t *testing.T) {
	str := func(v string) *string { return &v }
	u64 := func(v uint64) *uint64 { return &v }
	tests := []struct {
		name string
		in   gen.SessionSnapshot
		want []core.PaneObservation
	}{
		{
			name: "orphan agent record kept as its own pane",
			in: gen.SessionSnapshot{
				Panes:  []gen.PaneInfo{{PaneID: "w1:p1", TerminalID: "t1", WorkspaceID: "w1", TabID: "w1:t1", AgentStatus: "unknown"}},
				Agents: []gen.AgentInfo{{PaneID: "w1:p2", TerminalID: "t2", WorkspaceID: "w1", TabID: "w1:t1", Agent: str("codex"), AgentStatus: "blocked", StateChangeSeq: u64(4)}},
			},
			want: []core.PaneObservation{
				{PaneID: "w1:p1", TerminalID: "t1", WorkspaceID: "w1", TabID: "w1:t1"},
				{PaneID: "w1:p2", TerminalID: "t2", WorkspaceID: "w1", TabID: "w1:t1",
					Agent: &core.AgentObservation{AgentProgram: "codex", Status: core.StatusBlocked, StateChangeSeq: u64(4)}},
			},
		},
		{
			name: "agent without state_change_seq",
			in: gen.SessionSnapshot{
				Panes:  []gen.PaneInfo{{PaneID: "w1:p1", TerminalID: "t1", AgentStatus: "idle", Agent: str("claude")}},
				Agents: []gen.AgentInfo{{PaneID: "w1:p1", TerminalID: "t1", Agent: str("claude"), AgentStatus: "idle"}},
			},
			want: []core.PaneObservation{
				{PaneID: "w1:p1", TerminalID: "t1", Agent: &core.AgentObservation{AgentProgram: "claude", Status: core.StatusIdle}},
			},
		},
		{
			name: "pane-record agent fallback",
			in: gen.SessionSnapshot{
				Panes: []gen.PaneInfo{{PaneID: "w1:p1", TerminalID: "t1", AgentStatus: "working", Agent: str("claude")}},
			},
			want: []core.PaneObservation{
				{PaneID: "w1:p1", TerminalID: "t1", Agent: &core.AgentObservation{AgentProgram: "claude", Status: core.StatusWorking}},
			},
		},
		{
			name: "agent record wins over the pane record",
			in: gen.SessionSnapshot{
				Panes:  []gen.PaneInfo{{PaneID: "w1:p1", TerminalID: "t1", AgentStatus: "idle", Agent: str("claude")}},
				Agents: []gen.AgentInfo{{PaneID: "w1:p1", TerminalID: "t1", Agent: str("claude"), AgentStatus: "working", StateChangeSeq: u64(9)}},
			},
			want: []core.PaneObservation{
				{PaneID: "w1:p1", TerminalID: "t1", Agent: &core.AgentObservation{AgentProgram: "claude", Status: core.StatusWorking, StateChangeSeq: u64(9)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapSnapshot(tt.in).Panes
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %s\nwant %s", dumpPanes(got), dumpPanes(tt.want))
			}
		})
	}
}

func dumpPanes(ps []core.PaneObservation) string {
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "%+v", p)
		if p.Agent != nil {
			fmt.Fprintf(&b, " agent %+v", *p.Agent)
			if p.Agent.StateChangeSeq != nil {
				fmt.Fprintf(&b, " seq %d", *p.Agent.StateChangeSeq)
			}
		}
		b.WriteString("; ")
	}
	return b.String()
}

func TestClosedWithoutResponseIsAnError(t *testing.T) {
	s := newFake(t, func(fc *fakeConn) {})
	if _, err := s.client().Ping(testContext(t)); err == nil {
		t.Fatal("Ping succeeded with no response line")
	}
}

func TestResponseFaults(t *testing.T) {
	tests := map[string]string{
		"garbage":     `not json`,
		"wrong type":  `{"id":"r1","result":{"type":"session_snapshot","snapshot":{}}}`,
		"wrong id":    `{"id":"other","result":{"type":"pong","version":"0.9.5","protocol":22}}`,
		"empty":       `{"id":"r1"}`,
		"null result": `{"id":"r1","result":null}`,
	}
	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			s := newFake(t, func(fc *fakeConn) {
				// Sent as is, so the wrong id stays wrong.
				fc.send(line)
			})
			_, err := s.client().Ping(testContext(t))
			if err == nil {
				t.Fatal("Ping succeeded")
			}
			var herr *Error
			if errors.As(err, &herr) {
				t.Errorf("err %v is a herdr error", err)
			}
		})
	}
}

func TestDialFailure(t *testing.T) {
	c := New(socketDir(t) + "/absent.sock")
	if _, err := c.Ping(testContext(t)); err == nil {
		t.Fatal("Ping succeeded with no server")
	}
}
