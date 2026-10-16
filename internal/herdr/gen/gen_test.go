package gen

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestGeneratedCodeIsCurrent regenerates the wire types into a temporary
// directory and fails if they differ from the committed wire_gen.go, so the
// committed file is always what `go generate` produces from the committed schema.
func TestGeneratedCodeIsCurrent(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		if goBin, err = exec.LookPath("go"); err != nil {
			t.Skip("go toolchain not found")
		}
	}
	out := filepath.Join(t.TempDir(), "wire_gen.go")
	cmd := exec.Command(goBin, "run", "./genwire", "-schema", "herdr-api-0.9.5.schema.json", "-out", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("genwire: %v\n%s", err, b)
	}
	fresh, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("wire_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fresh, committed) {
		t.Fatal("wire_gen.go is stale: run `go generate ./internal/herdr/gen` and commit the result")
	}
}

// TestNoForbiddenRequests guards AD-5 at the source: no RequestMethod or
// SubscriptionType constant in the generated code is a focus method or a
// pane-output method or subscription, whatever the constant is called.
func TestNoForbiddenRequests(t *testing.T) {
	forbidden := map[string]bool{
		"pane.focus": true, "agent.focus": true, "pane.read": true, "agent.read": true,
		"pane.wait_for_output": true, "pane.output_matched": true,
	}
	file, err := parser.ParseFile(token.NewFileSet(), "wire_gen.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			typ, ok := vs.Type.(*ast.Ident)
			if !ok || (typ.Name != "RequestMethod" && typ.Name != "SubscriptionType") {
				continue
			}
			for i, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("%s: non-literal value", vs.Names[i].Name)
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				checked++
				if forbidden[value] {
					t.Errorf("%s %s = %q is forbidden (AD-5)", vs.Names[i].Name, typ.Name, value)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no RequestMethod or SubscriptionType constants to check")
	}
}

// strict decodes b into v, failing on any field the generated type lacks, so
// these fixtures prove the types cover everything herdr 0.9.5 actually sent.
func strict(t *testing.T, b []byte, v any) {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		t.Fatalf("decode %T: %v", v, err)
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// resultType reads the discriminator of a success response's result.
func resultType(t *testing.T, raw json.RawMessage) ResponseResultType {
	t.Helper()
	var head struct {
		Type ResponseResultType `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		t.Fatal(err)
	}
	return head.Type
}

// The fixtures in testdata are raw lines recorded from an isolated herdr 0.9.5 by
// docs/research/herdr-0.9.5-probe.sh on 2026-10-16.

func TestDecodePing(t *testing.T) {
	var resp SuccessResponse
	strict(t, fixture(t, "ping.json"), &resp)
	if got := resultType(t, resp.Result); got != ResponseResultTypePong {
		t.Fatalf("result type %q", got)
	}
	var pong ResponseResultPong
	strict(t, resp.Result, &pong)
	if pong.Version != "0.9.5" || pong.Protocol != 22 {
		t.Errorf("version %q protocol %d", pong.Version, pong.Protocol)
	}
	if pong.Capabilities == nil || !pong.Capabilities.LiveHandoff {
		t.Errorf("capabilities %+v", pong.Capabilities)
	}
}

func TestDecodeSnapshot(t *testing.T) {
	var resp SuccessResponse
	strict(t, fixture(t, "snapshot.json"), &resp)
	if got := resultType(t, resp.Result); got != ResponseResultTypeSessionSnapshot {
		t.Fatalf("result type %q", got)
	}
	var r ResponseResultSessionSnapshot
	strict(t, resp.Result, &r)
	s := r.Snapshot
	if s.Version != "0.9.5" || len(s.Workspaces) != 2 || len(s.Panes) != 3 || len(s.Agents) != 1 {
		t.Fatalf("snapshot %d workspaces, %d panes, %d agents, version %q",
			len(s.Workspaces), len(s.Panes), len(s.Agents), s.Version)
	}
	a := s.Agents[0]
	if a.PaneID != "w1:p1" || a.AgentStatus != AgentStatusWorking || a.TerminalID == "" {
		t.Errorf("agent %+v", a)
	}
	if a.StateChangeSeq == nil || *a.StateChangeSeq != 9 {
		t.Errorf("state_change_seq %v, want 9", a.StateChangeSeq)
	}
	if a.CompletionSeq != nil {
		t.Errorf("completion_seq %v, want absent for a working agent", *a.CompletionSeq)
	}
	if a.Agent == nil || *a.Agent != "claude" {
		t.Errorf("agent program %v", a.Agent)
	}
	if p := s.Panes[0]; p.Scroll == nil || p.Scroll.ViewportRows == 0 {
		t.Errorf("pane scroll %+v", p.Scroll)
	}
}

func TestDecodeProcessInfo(t *testing.T) {
	var resp SuccessResponse
	strict(t, fixture(t, "process_info.json"), &resp)
	var r ResponseResultPaneProcessInfo
	strict(t, resp.Result, &r)
	pi := r.ProcessInfo
	if pi.ShellPID == nil || pi.ForegroundProcessGroupID == nil || len(pi.ForegroundProcesses) != 1 {
		t.Fatalf("process info %+v", pi)
	}
	fg := pi.ForegroundProcesses[0]
	if fg.Name != "sleep" || fg.PID != *pi.ForegroundProcessGroupID || fg.PID == *pi.ShellPID {
		t.Errorf("foreground %+v", fg)
	}
}

func TestDecodeLifecycleEvent(t *testing.T) {
	var env EventEnvelope
	strict(t, fixture(t, "event_lifecycle.json"), &env)
	if env.Event != EventKindPaneAgentDetected {
		t.Fatalf("event %q", env.Event)
	}
	var d EventDataPaneAgentDetected
	strict(t, env.Data, &d)
	// The lifecycle envelope repeats the underscored name in data.type.
	if string(d.Type) != string(env.Event) || d.PaneID != "w1:p1" || d.Agent == nil || *d.Agent != "claude" {
		t.Errorf("data %+v", d)
	}
}

func TestDecodePaneScopedEvent(t *testing.T) {
	var env SubscriptionEventEnvelope
	strict(t, fixture(t, "event_pane_scoped.json"), &env)
	// The pane-scoped envelope keeps the dotted name and has no data.type.
	if env.Event != SubscriptionEventKindPaneAgentStatusChanged {
		t.Fatalf("event %q", env.Event)
	}
	var d PaneAgentStatusChangedEvent
	strict(t, env.Data, &d)
	if d.PaneID != "w1:p1" || d.AgentStatus != AgentStatusIdle {
		t.Errorf("data %+v", d)
	}
}

func TestDecodeError(t *testing.T) {
	var resp ErrorResponse
	strict(t, fixture(t, "error.json"), &resp)
	if resp.ID != "e3" || resp.Error.Code != "pane_not_found" || !strings.Contains(resp.Error.Message, "w9:p9") {
		t.Errorf("error %+v", resp)
	}
}

func TestEncodeSubscribe(t *testing.T) {
	sub, err := json.Marshal(SubscriptionPaneAgentStatusChanged{Type: SubscriptionTypePaneAgentStatusChanged, PaneID: "w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	req := RequestEventsSubscribe{ID: "s1", Method: RequestMethodEventsSubscribe,
		Params: EventsSubscribeParams{Subscriptions: []json.RawMessage{sub}}}
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"s1","method":"events.subscribe","params":{"subscriptions":[{"pane_id":"w1:p1","type":"pane.agent_status_changed"}]}}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	ping, err := json.Marshal(RequestPing{ID: "r1", Method: RequestMethodPing})
	if err != nil {
		t.Fatal(err)
	}
	if string(ping) != `{"id":"r1","method":"ping","params":{}}` {
		t.Errorf("ping %s", ping)
	}
}
