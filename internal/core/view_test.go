package core

import (
	"encoding/json"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestContextLine(t *testing.T) {
	tests := []struct {
		program AgentProgram
		title   string
		want    string
	}{
		{"claude", "fix-auth-bug", "fix-auth-bug"},
		{"claude", "  fix-auth-bug ", "fix-auth-bug"},
		{"claude", "Claude Code", ""},
		{"claude", " Claude Code ", ""},
		{"claude", "", ""},
		{"claude", "  ", ""},
		{"codex", "anything", ""},
		{"", "anything", ""},
		{"claude", "\nfix\tbug", "fix bug"},
		{"claude", "fix\x00\x1b-bug \r\n  now", "fix-bug now"},
		{"claude", "✳ fix-auth", "fix-auth"},
		{"claude", "◐ Claude Code", ""},
		{"claude", "✳fix-auth", "✳fix-auth"},
		{"claude", "1 fix", "1 fix"},
	}
	for _, tt := range tests {
		if got := ContextLine(tt.program, tt.title); got != tt.want {
			t.Errorf("ContextLine(%q, %q) = %q, want %q", tt.program, tt.title, got, tt.want)
		}
	}
}

func TestShortenDir(t *testing.T) {
	tests := []struct{ dir, home, want string }{
		{"/Users/x/proj", "/Users/x", "~/proj"},
		{"/Users/x", "/Users/x", "~"},
		{"/Users/x/proj", "/Users/x/", "~/proj"},
		{"/Users/xy/proj", "/Users/x", "/Users/xy/proj"},
		{"/opt/proj", "/Users/x", "/opt/proj"},
		{"/Users/x/proj", "", "/Users/x/proj"},
		{"/Users/x/proj", "/", "/Users/x/proj"},
		{"", "/Users/x", ""},
	}
	for _, tt := range tests {
		if got := ShortenDir(tt.dir, tt.home); got != tt.want {
			t.Errorf("ShortenDir(%q, %q) = %q, want %q", tt.dir, tt.home, got, tt.want)
		}
	}
}

func TestViewHeading(t *testing.T) {
	tests := []struct {
		profile string
		program AgentProgram
		want    string
	}{
		{"codex", "codex", "codex"},
		{"cldw", "claude", "cldw · claude"},
	}
	for _, tt := range tests {
		v := NewAgentView(AgentViewInput{Key: key("p"), AgentProgram: tt.program, Profile: tt.profile})
		if v.Heading != tt.want {
			t.Errorf("heading(%q, %q) = %q, want %q", tt.profile, tt.program, v.Heading, tt.want)
		}
	}
}

func key(pane string) AgentKey {
	return AgentKey{Host: "h", Server: "default", Epoch: 1, PaneID: pane}
}

// view builds an AgentView with the class status gives, a known start unless
// since is 0, and the workspace position pos.
func view(pane string, status Status, since int64, pos int) AgentView {
	f := FoldResult{Class: ClassUnknown}
	if since != 0 {
		f = FoldResult{Known: true, Status: status, Class: ClassOf(status, true), Since: since, SinceProvenance: ProvenanceLive}
	}
	return NewAgentView(AgentViewInput{
		Key: key(pane), AgentProgram: "claude", Profile: "cldp", Status: status, Fold: f,
		Context: PaneContext{WorkspacePosition: pos},
	})
}

func panes(vs []AgentView) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Key.PaneID
	}
	return out
}

func TestBoardOrder(t *testing.T) {
	tests := []struct {
		name  string
		views []AgentView
		want  []string
	}{
		{"top zone flat", []AgentView{
			view("fin", StatusDone, 5, 1),
			view("ws1", StatusBlocked, 20, 1),
			view("ws2", StatusBlocked, 10, 2),
		}, []string{"ws2", "ws1", "fin"}},
		{"lower grouped", []AgentView{
			view("ws2-working", StatusWorking, 1, 2),
			view("ws1-unknown", StatusUnknown, 2, 1),
			view("ws1-working", StatusWorking, 3, 1),
		}, []string{"ws1-working", "ws1-unknown", "ws2-working"}},
		{"unknown since", []AgentView{
			view("unknown", StatusBlocked, 0, 1),
			view("known", StatusBlocked, 50, 1),
		}, []string{"known", "unknown"}},
		{"tie", []AgentView{
			view("b", StatusBlocked, 7, 1),
			view("a", StatusBlocked, 7, 2),
		}, []string{"a", "b"}},
		{"unknown position last", []AgentView{
			view("nowhere", StatusWorking, 1, 0),
			view("ws3", StatusWorking, 9, 3),
		}, []string{"ws3", "nowhere"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SortViews(tt.views)
			if got := panes(tt.views); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortKeyLessKey(t *testing.T) {
	base := view("p", StatusBlocked, 7, 1).SortKey
	tests := []struct {
		name string
		a, b AgentKey
	}{
		{"host", AgentKey{Host: "a", Server: "z", Epoch: 9, PaneID: "z"}, AgentKey{Host: "b", Server: "a", Epoch: 1, PaneID: "a"}},
		{"server", AgentKey{Host: "h", Server: "a", Epoch: 9, PaneID: "z"}, AgentKey{Host: "h", Server: "b", Epoch: 1, PaneID: "a"}},
		{"epoch numerically", AgentKey{Host: "h", Server: "s", Epoch: 2, PaneID: "z"}, AgentKey{Host: "h", Server: "s", Epoch: 10, PaneID: "a"}},
		{"pane", AgentKey{Host: "h", Server: "s", Epoch: 1, PaneID: "a"}, AgentKey{Host: "h", Server: "s", Epoch: 1, PaneID: "b"}},
	}
	for _, tt := range tests {
		a, b := base, base
		a.Key, b.Key = tt.a, tt.b
		if !a.Less(b) || b.Less(a) {
			t.Errorf("%s: want %v before %v", tt.name, tt.a, tt.b)
		}
	}
	if base.Less(base) {
		t.Error("a key is less than itself")
	}
}

func TestNewAgentView(t *testing.T) {
	k := AgentKey{Host: "mac", Server: "default", Epoch: 3, PaneID: "w1:p2"}
	v := NewAgentView(AgentViewInput{
		Key: k, AgentProgram: "claude", Profile: "cldw", Status: StatusIdle,
		Fold: FoldResult{Known: true, Status: StatusIdle, Class: ClassFinished, Since: 500, SinceProvenance: ProvenanceSnapshot, EverWorking: true},
		Context: PaneContext{ProjectDir: "/Users/x/proj", WorkspaceLabel: "ws", TabLabel: "t",
			WorkspacePosition: 2, PaneTitle: "fix-auth-bug"},
		Home: "/Users/x",
	})
	want := AgentView{
		Key: k, AgentProgram: "claude", Profile: "cldw", Heading: "cldw · claude",
		Directory: "~/proj", Workspace: "ws", Tab: "t", Status: StatusIdle, Class: ClassFinished,
		WaitingSince: 500, WaitingExact: false, PaneTitle: "fix-auth-bug", ContextLine: "fix-auth-bug",
		SortKey: SortKey{Zone: 0, Group: 0, ClassRank: 1, Start: 500, Key: k},
	}
	if v != want {
		t.Errorf("view =\n%+v\nwant\n%+v", v, want)
	}

	// An unknown fold: the class comes from the Status, the start is unknown.
	in := AgentViewInput{Key: k, AgentProgram: "codex", Profile: "codex", Status: StatusWorking,
		Fold: FoldResult{Class: ClassUnknown}}
	u := NewAgentView(in)
	if u.Class != ClassWorking || u.WaitingSince != 0 || u.WaitingExact || u.SortKey.Start != SortUnknown ||
		u.SortKey.Group != SortUnknown {
		t.Errorf("unknown fold view = %+v", u)
	}

	// A live class start is exact.
	in.Fold = FoldResult{Known: true, Status: StatusWorking, Class: ClassWorking, Since: 40, SinceProvenance: ProvenanceLive}
	if e := NewAgentView(in); !e.WaitingExact || e.WaitingSince != 40 {
		t.Errorf("live view = %+v", e)
	}

	// A known fold decides the class, even where ClassOf of the current
	// Status would say otherwise.
	in.Status = StatusWorking
	in.Fold = FoldResult{Known: true, Status: StatusBlocked, Class: ClassNeedsYou, Since: 100, SinceProvenance: ProvenanceLive}
	if f := NewAgentView(in); f.Class != ClassNeedsYou || f.SortKey.Zone != 0 || f.SortKey.ClassRank != 0 {
		t.Errorf("fold-class view = %+v", f)
	}
}

func TestAgentViewJSON(t *testing.T) {
	v := NewAgentView(AgentViewInput{
		Key: AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"}, AgentProgram: "claude",
		Profile: "cldp", Status: StatusWorking, Fold: FoldResult{Class: ClassUnknown},
		Context: PaneContext{PaneTitle: "Claude Code", WorkspacePosition: 0},
	})
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"context_line":""`,
		`"key":{"host":"mac","server":"default","epoch":2,"pane_id":"w1:p1"}`,
		`"attention_class":"working"`,
		`"waiting_exact":false`,
		`"sort_key":[1,9007199254740991,2,9007199254740991,"mac/default/2/w1:p1"]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON %s lacks %s", s, want)
		}
	}
	if strings.Contains(s, "waiting_since") {
		t.Errorf("JSON %s carries an unknown waiting_since", s)
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if sk, ok := m["sort_key"].([]any); !ok || len(sk) != 5 {
		t.Errorf("sort_key = %#v, want an array of five", m["sort_key"])
	}
	for _, f := range []string{"key", "agent_program", "profile", "heading", "directory", "workspace", "tab",
		"status", "attention_class", "waiting_exact", "pane_title", "context_line", "sort_key"} {
		if _, ok := m[f]; !ok {
			t.Errorf("JSON lacks %q", f)
		}
	}

	v.WaitingSince = 1234
	b, _ = json.Marshal(v)
	if !strings.Contains(string(b), `"waiting_since":1234`) {
		t.Errorf("JSON %s lacks a known waiting_since", b)
	}
}

// boardFixture is internal/core/testdata/board-order.json, the golden Board
// shared with the Board's ordering tests (Story 3.7).
type boardFixture struct {
	Home  string `json:"home"`
	Input []struct {
		Key               AgentKey     `json:"key"`
		AgentProgram      AgentProgram `json:"agent_program"`
		Profile           string       `json:"profile"`
		Status            Status       `json:"status"`
		EverWorking       bool         `json:"ever_working"`
		Since             *int64       `json:"since"`
		Exact             bool         `json:"exact"`
		Directory         string       `json:"directory"`
		Workspace         string       `json:"workspace"`
		WorkspacePosition int          `json:"workspace_position"`
		Tab               string       `json:"tab"`
		PaneTitle         string       `json:"pane_title"`
		// View is the expected AgentView, as NewAgentView serialises it.
		View json.RawMessage `json:"view"`
	} `json:"input"`
	Order   []string `json:"order"`
	Headers []int    `json:"headers"`
}

func loadBoard(t *testing.T) ([]AgentView, boardFixture) {
	t.Helper()
	b, err := os.ReadFile("testdata/board-order.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx boardFixture
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	views := make([]AgentView, 0, len(fx.Input))
	for _, in := range fx.Input {
		f := FoldResult{Class: ClassUnknown, EverWorking: in.EverWorking}
		if in.Since != nil {
			prov := ProvenanceSnapshot
			if in.Exact {
				prov = ProvenanceLive
			}
			f = FoldResult{Known: true, Status: in.Status, Class: ClassOf(in.Status, in.EverWorking),
				Since: *in.Since, SinceProvenance: prov, EverWorking: in.EverWorking}
		}
		views = append(views, NewAgentView(AgentViewInput{
			Key: in.Key, AgentProgram: in.AgentProgram, Profile: in.Profile, Status: in.Status, Fold: f,
			Context: PaneContext{ProjectDir: in.Directory, WorkspaceLabel: in.Workspace, TabLabel: in.Tab,
				WorkspacePosition: in.WorkspacePosition, PaneTitle: in.PaneTitle},
			Home: fx.Home,
		}))
	}
	return views, fx
}

func TestGoldenBoard(t *testing.T) {
	views, fx := loadBoard(t)
	classes := map[AttentionClass]bool{}
	for _, v := range views {
		classes[v.Class] = true
	}
	if len(classes) != 4 {
		t.Errorf("fixture covers classes %v, want all four", classes)
	}
	for i, v := range views {
		got, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var g, w any
		if err := json.Unmarshal(got, &g); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(fx.Input[i].View, &w); err != nil {
			t.Fatalf("input %d: expected view: %v", i, err)
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("input %d: view =\n%s\nwant\n%s", i, got, fx.Input[i].View)
		}
	}

	check := func(vs []AgentView) {
		t.Helper()
		SortViews(vs)
		got := make([]string, len(vs))
		for i, v := range vs {
			got[i] = KeyString(v.Key)
		}
		if !reflect.DeepEqual(got, fx.Order) {
			t.Errorf("order =\n%v\nwant\n%v", got, fx.Order)
		}
		if h := WorkspaceHeaders(vs); !reflect.DeepEqual(h, fx.Headers) {
			t.Errorf("headers = %v, want %v", h, fx.Headers)
		}
	}
	check(views)

	// Any input order gives the same Board.
	r := rand.New(rand.NewSource(1))
	for range 20 {
		r.Shuffle(len(views), func(i, j int) { views[i], views[j] = views[j], views[i] })
		check(views)
	}
}

func TestWorkspaceHeaders(t *testing.T) {
	if h := WorkspaceHeaders(nil); len(h) != 0 {
		t.Errorf("headers of nothing = %v", h)
	}
	vs := []AgentView{view("a", StatusBlocked, 1, 1), view("b", StatusDone, 1, 2)}
	SortViews(vs)
	if h := WorkspaceHeaders(vs); len(h) != 0 {
		t.Errorf("top zone only: headers = %v", h)
	}
	vs = []AgentView{view("a", StatusWorking, 1, 1), view("b", StatusWorking, 2, 1)}
	SortViews(vs)
	if h := WorkspaceHeaders(vs); !reflect.DeepEqual(h, []int{0}) {
		t.Errorf("one group: headers = %v, want [0]", h)
	}
}
