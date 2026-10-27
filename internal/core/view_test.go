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
		// Bidi controls are dropped; other format characters, such as the
		// zero-width joiner in an emoji sequence, are kept.
		{"claude", "fix\u202egnp.exe", "fixgnp.exe"},
		{"claude", "\u2066a\u2069 \u200eb\u200f \u061cc \u202a\u202b\u202c\u202dd", "a b c d"},
		{"claude", "\U0001F468\u200d\U0001F4BB pair", "\U0001F468\u200d\U0001F4BB pair"},
		// Only Claude Code's own status glyphs are a prefix to strip; a
		// session name's leading "-", "#" or emoji stays.
		{"claude", "- wip", "- wip"},
		{"claude", "# notes", "# notes"},
		{"claude", "🚀 launch", "🚀 launch"},
		{"claude", "◐ fix", "fix"},
		{"claude", "✳", ""},
		{"claude", " ✳ ", ""},
		{"claude", "◐", ""},
		{"claude", "✳ ✳ x", "✳ x"},
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
		{"cldw", "claude", "cldw"},
		{ProfileUnresolved, "claude", "unresolved"},
		{"work", "codex", "work · codex"},
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

// neverWorkingView builds the view of an idle Agent never seen working: class
// working with Status idle, which the Board labels Idle.
func neverWorkingView(pane string, since int64, pos int) AgentView {
	f := FoldResult{Class: ClassUnknown}
	if since != 0 {
		f = FoldResult{Known: true, Status: StatusIdle, Class: ClassWorking, Since: since, SinceProvenance: ProvenanceLive}
	}
	return NewAgentView(AgentViewInput{
		Key: key(pane), AgentProgram: "claude", Profile: "cldp", Status: StatusIdle, Fold: f,
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
		{"needs you alone on top, flat", []AgentView{
			view("fin", StatusDone, 5, 1),
			view("ws1", StatusBlocked, 20, 1),
			view("ws2", StatusBlocked, 10, 2),
		}, []string{"ws2", "ws1", "fin"}},
		{"one workspace: needs you, then finished, idle, working", []AgentView{
			neverWorkingView("idle", 1, 1),
			view("fin-idle", StatusIdle, 2, 1),
			view("unknown", StatusUnknown, 3, 1),
			view("working", StatusWorking, 4, 1),
			view("needs", StatusBlocked, 5, 1),
			view("fin-done", StatusDone, 6, 1),
			neverWorkingView("idle-unknown-start", 0, 1),
		}, []string{"needs", "fin-done", "fin-idle", "idle", "idle-unknown-start", "working", "unknown"}},
		// Operator decision 2026-10-21: every class in a workspace group is
		// newest first, while Needs you stays longest-waiting first.
		{"workspace group newest first, needs you longest first", []AgentView{
			view("fin-old", StatusDone, 1, 1),
			view("needs-new", StatusBlocked, 9, 1),
			view("fin-new", StatusDone, 5, 1),
			view("working-old", StatusWorking, 2, 1),
			view("needs-old", StatusBlocked, 3, 2),
			view("fin-mid", StatusDone, 3, 1),
			view("working-new", StatusWorking, 8, 1),
		}, []string{"needs-old", "needs-new", "fin-new", "fin-mid", "fin-old", "working-new", "working-old"}},
		{"finished grouped by workspace", []AgentView{
			view("ws2-fin", StatusDone, 1, 2),
			view("ws1-fin", StatusDone, 9, 1),
		}, []string{"ws1-fin", "ws2-fin"}},
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

// TestNewlyBlockedSortsLast checks that, of two blocked Agents in Needs you,
// the one whose class began later sorts after the earlier one: a newly
// blocked Agent sorts last there (operator decision 2026-10-20, FR-3). Its
// pane id would sort first, so only the class start decides.
func TestNewlyBlockedSortsLast(t *testing.T) {
	blocked := func(pane string, since int64) AgentView {
		return NewAgentView(AgentViewInput{
			Key: key(pane), AgentProgram: "claude", Profile: "cldp", Status: StatusBlocked,
			Fold: FoldResult{Known: true, Status: StatusBlocked, Class: ClassNeedsYou, Since: since,
				SinceProvenance: ProvenanceLive, EverWorking: true},
			Context: PaneContext{WorkspacePosition: 1},
		})
	}
	earlier := blocked("w1:p9", 100)
	newer := blocked("w1:p1", 200)
	if earlier.Class != ClassNeedsYou || newer.Class != ClassNeedsYou {
		t.Fatalf("classes %s and %s, want needs_you", earlier.Class, newer.Class)
	}
	if !earlier.SortKey.Less(newer.SortKey) || newer.SortKey.Less(earlier.SortKey) {
		t.Errorf("newly blocked %+v sorts before the earlier %+v", newer.SortKey, earlier.SortKey)
	}
}

func TestNewAgentView(t *testing.T) {
	k := AgentKey{Host: "mac", Server: "default", Epoch: 3, PaneID: "w1:p2"}
	v := NewAgentView(AgentViewInput{
		Key: k, AgentProgram: "claude", Profile: "cldw", Status: StatusIdle,
		Fold: FoldResult{Known: true, Status: StatusIdle, Class: ClassFinished, Since: 500, SinceProvenance: ProvenanceSnapshot, EverWorking: true},
		Context: PaneContext{ProjectDir: "/Users/x/proj", WorkspaceLabel: "ws", TabLabel: "t",
			WorkspacePosition: 2, PaneTitle: "fix-auth-bug", PanePosition: "top-left"},
		Home: "/Users/x",
	})
	want := AgentView{
		Key: k, AgentProgram: "claude", Profile: "cldw", Heading: "cldw",
		Directory: "~/proj", Workspace: "ws", Tab: "t", PanePosition: "top-left", Status: StatusIdle, Class: ClassFinished,
		WaitingSince: 500, WaitingExact: false, PaneTitle: "fix-auth-bug", ContextLine: "fix-auth-bug",
		SortKey: SortKey{Zone: 1, Group: 2, ClassRank: 1, Start: -500, Key: k},
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

func TestUnpositionedGroups(t *testing.T) {
	in := func(pane string, pos int, ws string) AgentView {
		return NewAgentView(AgentViewInput{
			Key: key(pane), AgentProgram: "claude", Profile: "cldp", Status: StatusWorking,
			Fold:    FoldResult{Known: true, Status: StatusWorking, Class: ClassWorking, Since: 10, SinceProvenance: ProvenanceLive},
			Context: PaneContext{WorkspaceID: ws, WorkspacePosition: pos},
		})
	}
	a, a2 := in("w7:p1", 0, "w7"), in("w7:p2", 0, "w7")
	b := in("w8:p1", 0, "w8")
	none := in("w9:p1", 0, "")
	positioned := in("w1:p1", 3, "w1")
	neg := in("w6:p1", -1, "w6")

	if a.SortKey.Group != a2.SortKey.Group {
		t.Errorf("one workspace, two groups: %d and %d", a.SortKey.Group, a2.SortKey.Group)
	}
	if a.SortKey.Group == b.SortKey.Group {
		t.Errorf("w7 and w8 share group %d", a.SortKey.Group)
	}
	if none.SortKey.Group != SortUnknown {
		t.Errorf("no workspace id: group %d, want SortUnknown", none.SortKey.Group)
	}
	if positioned.SortKey.Group != 3 {
		t.Errorf("positioned group = %d, want 3", positioned.SortKey.Group)
	}
	if neg.SortKey.Group != UnpositionedGroup("w6") {
		t.Errorf("position -1: group %d, want UnpositionedGroup", neg.SortKey.Group)
	}
	for _, v := range []AgentView{a, b, neg} {
		if g := v.SortKey.Group; g < 1<<52 || g >= SortUnknown {
			t.Errorf("%s: group %d outside [2^52, 2^53-1)", v.Key.PaneID, g)
		}
	}
	for _, id := range []string{"", "w1", "w7", "a much longer workspace id", "\xff"} {
		if g := UnpositionedGroup(id); g < 1<<52 || g > SortUnknown-1 {
			t.Errorf("UnpositionedGroup(%q) = %d, out of range", id, g)
		}
	}

	vs := []AgentView{none, b, positioned, a, a2}
	SortViews(vs)
	if vs[0].Key.PaneID != "w1:p1" || vs[len(vs)-1].Key.PaneID != "w9:p1" {
		t.Errorf("order = %v, want the positioned first and no workspace id last", panes(vs))
	}
	// Positioned, w7 and w8 (in hash order), then no workspace id: four headers.
	if h := WorkspaceHeaders(vs); len(h) != 4 {
		t.Errorf("headers = %v over %v, want four groups", h, panes(vs))
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
		`"sort_key":[1,9007199254740991,4,9007199254740991,"mac/default/2/w1:p1"]`,
		`"pane_position":""`,
		`"background":false`,
		`"seen":false`,
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
		"pane_position", "status", "attention_class", "waiting_exact", "pane_title", "context_line", "recap", "background", "background_tasks", "seen", "sort_key"} {
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
		WorkspaceID       string       `json:"workspace_id"`
		WorkspacePosition int          `json:"workspace_position"`
		Tab               string       `json:"tab"`
		PanePosition      string       `json:"pane_position"`
		PaneTitle         string       `json:"pane_title"`
		Background        bool         `json:"background"`
		// Tasks are background subagents the transcript shows open, and
		// TurnEnded says its main turn ended (ADR-0009).
		Tasks     []string `json:"tasks"`
		TurnEnded bool     `json:"turn_ended"`
		// Recap is the latest session recap the transcript shows
		// (ADR-0010), applied after Tasks and TurnEnded.
		Recap string `json:"recap"`
		// Seen is the operator's acknowledgement of the current finish
		// (ADR-0014).
		Seen bool `json:"seen"`
		// InView is the Agent's pane being herdr's focused pane (ADR-0015).
		InView bool `json:"in_view"`
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
		var tr BackgroundTasks
		for _, id := range in.Tasks {
			tr.Apply(TranscriptLine{StartID: id})
		}
		tr.Apply(TranscriptLine{TurnEnded: in.TurnEnded})
		tr.Apply(TranscriptLine{Recap: in.Recap})
		views = append(views, NewAgentView(AgentViewInput{
			Key: in.Key, AgentProgram: in.AgentProgram, Profile: in.Profile, Status: in.Status, Fold: f,
			Context: PaneContext{ProjectDir: in.Directory, WorkspaceLabel: in.Workspace, TabLabel: in.Tab,
				WorkspaceID: in.WorkspaceID, WorkspacePosition: in.WorkspacePosition, PaneTitle: in.PaneTitle, PanePosition: in.PanePosition},
			Home: fx.Home, Background: in.Background, Transcript: tr, Seen: in.Seen, InView: in.InView,
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
	ranks := map[int]bool{}
	for _, v := range views {
		ranks[v.SortKey.ClassRank] = true
	}
	if len(ranks) != 6 {
		t.Errorf("fixture covers class ranks %v, want all six", ranks)
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
	vs := []AgentView{view("a", StatusBlocked, 1, 1), view("b", StatusBlocked, 1, 2)}
	SortViews(vs)
	if h := WorkspaceHeaders(vs); len(h) != 0 {
		t.Errorf("top zone only: headers = %v", h)
	}
	vs = []AgentView{view("a", StatusBlocked, 1, 1), view("b", StatusDone, 1, 2), view("c", StatusWorking, 1, 1)}
	SortViews(vs)
	if h := WorkspaceHeaders(vs); !reflect.DeepEqual(h, []int{1, 2}) {
		t.Errorf("finished in its group: headers = %v, want [1 2]", h)
	}
	vs = []AgentView{view("a", StatusWorking, 1, 1), view("b", StatusWorking, 2, 1)}
	SortViews(vs)
	if h := WorkspaceHeaders(vs); !reflect.DeepEqual(h, []int{0}) {
		t.Errorf("one group: headers = %v, want [0]", h)
	}
}

func TestConnectionTruthJSON(t *testing.T) {
	cases := []struct {
		name string
		in   ConnectionTruth
		want string
	}{
		{"before any gate", ConnectionTruth{HerdrMinimum: HerdrMinimum},
			`{"connected":false,"herdr_minimum":"0.9.5","resync_guaranteed":false}`},
		{"connected", ConnectionTruth{Connected: true, LastSnapshotAt: 1760000000000, HerdrVersion: "0.9.5",
			HerdrMinimum: HerdrMinimum, Gate: GateOK, ResyncGuaranteed: true},
			`{"connected":true,"last_snapshot_at":1760000000000,"herdr_version":"0.9.5","herdr_minimum":"0.9.5","gate":"ok","resync_guaranteed":true}`},
		{"refused", ConnectionTruth{HerdrVersion: "0.8.2", HerdrMinimum: HerdrMinimum, Gate: GateRefused},
			`{"connected":false,"herdr_version":"0.8.2","herdr_minimum":"0.9.5","gate":"refused","resync_guaranteed":false}`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.want {
			t.Errorf("%s: got %s, want %s", c.name, b, c.want)
		}
	}
}

func TestPanePositions(t *testing.T) {
	r := func(id string, x, y, w, h int) PaneRect { return PaneRect{PaneID: id, X: x, Y: y, Width: w, Height: h} }
	tests := []struct {
		name  string
		rects []PaneRect
		want  map[string]string
	}{
		{"one pane", []PaneRect{r("a", 0, 0, 80, 24)}, map[string]string{}},
		{"no panes", nil, map[string]string{}},
		{"two columns", []PaneRect{r("a", 0, 0, 40, 24), r("b", 41, 0, 39, 24)},
			map[string]string{"a": "left", "b": "right"}},
		{"three panes: one left, two stacked right",
			[]PaneRect{r("a", 0, 0, 40, 24), r("b", 41, 0, 39, 12), r("c", 41, 13, 39, 11)},
			map[string]string{"a": "left", "b": "top-right", "c": "bottom-right"}},
		{"two rows", []PaneRect{r("a", 0, 0, 80, 12), r("b", 0, 13, 80, 11)},
			map[string]string{"a": "top", "b": "bottom"}},
		{"three columns", []PaneRect{r("a", 0, 0, 20, 24), r("b", 21, 0, 20, 24), r("c", 42, 0, 20, 24)},
			map[string]string{"a": "left", "b": "middle", "c": "right"}},
		{"quadrants", []PaneRect{r("a", 0, 0, 40, 12), r("b", 41, 0, 39, 12), r("c", 0, 13, 40, 11), r("d", 41, 13, 39, 11)},
			map[string]string{"a": "top-left", "b": "top-right", "c": "bottom-left", "d": "bottom-right"}},
		{"four columns: the inner two numbered", []PaneRect{r("a", 0, 0, 19, 24), r("b", 20, 0, 19, 24), r("c", 40, 0, 19, 24), r("d", 60, 0, 20, 24)},
			map[string]string{"a": "left", "b": "middle 1", "c": "middle 2", "d": "right"}},
		{"three rows", []PaneRect{r("a", 0, 0, 80, 7), r("b", 0, 8, 80, 7), r("c", 0, 16, 80, 8)},
			map[string]string{"a": "top", "b": "middle", "c": "bottom"}},
		{"one wide pane over a column of three and one tall pane",
			[]PaneRect{r("t", 0, 0, 120, 10), r("l1", 0, 10, 60, 10), r("l2", 0, 20, 60, 10), r("l3", 0, 30, 60, 10), r("r", 60, 10, 60, 30)},
			map[string]string{"t": "top", "l1": "top-left", "l2": "middle-left", "l3": "bottom-left", "r": "bottom-right"}},
		{"one wide pane over two", []PaneRect{r("a", 0, 0, 80, 12), r("b", 0, 13, 40, 11), r("c", 41, 13, 39, 11)},
			map[string]string{"a": "top", "b": "bottom-left", "c": "bottom-right"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PanePositions(tt.rects)
			for _, rect := range tt.rects {
				if got[rect.PaneID] != tt.want[rect.PaneID] {
					t.Errorf("%s = %q, want %q", rect.PaneID, got[rect.PaneID], tt.want[rect.PaneID])
				}
			}
		})
	}
}

// TestAgentViewRecap covers the matrix's "View" row: a recap on a Finished
// Agent is in its AgentView's JSON as recap, and every other field is what
// it is without one (ADR-0010).
func TestAgentViewRecap(t *testing.T) {
	in := AgentViewInput{
		Key: AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"}, AgentProgram: "claude",
		Profile: "cldp", Status: StatusDone,
		Fold:    FoldResult{Known: true, Status: StatusDone, Class: ClassFinished, Since: 500, SinceProvenance: ProvenanceLive, EverWorking: true},
		Context: PaneContext{PaneTitle: "write-tests", WorkspacePosition: 1},
	}
	plain := NewAgentView(in)
	in.Transcript.Apply(TranscriptLine{TurnEnded: true})
	in.Transcript.Apply(TranscriptLine{Recap: "I did X. Next I'll do Y."})
	withRecap := NewAgentView(in)
	if withRecap.Recap != "I did X. Next I'll do Y." || plain.Recap != "" {
		t.Errorf("recap %q / %q", withRecap.Recap, plain.Recap)
	}
	b, _ := json.Marshal(withRecap)
	if !strings.Contains(string(b), `"recap":"I did X. Next I'll do Y."`) {
		t.Errorf("JSON %s lacks the recap", b)
	}
	if b, _ := json.Marshal(plain); !strings.Contains(string(b), `"recap":""`) {
		t.Errorf("JSON %s lacks an empty recap", b)
	}
	withRecap.Recap = ""
	if !reflect.DeepEqual(withRecap, plain) {
		t.Errorf("a recap changed another field:\n%+v\n%+v", withRecap, plain)
	}
}

// TestAgentViewRecapHidden covers the matrix's "View, hidden" row: a recap
// is shown only on a Finished, Idle or Background Box; Working (not
// Background), Needs you and Unknown get "".
func TestAgentViewRecapHidden(t *testing.T) {
	const recap = "I did X. Next I'll do Y."
	for name, tc := range map[string]struct {
		status Status
		class  AttentionClass
		task   bool
		want   string
	}{
		"finished":                {StatusDone, ClassFinished, false, recap},
		"idle":                    {StatusIdle, ClassWorking, false, recap},
		"background (done)":       {StatusDone, ClassFinished, true, recap},
		"background (working)":    {StatusWorking, ClassWorking, true, recap},
		"working, not background": {StatusWorking, ClassWorking, false, ""},
		"needs you":               {StatusBlocked, ClassNeedsYou, false, ""},
		"unknown":                 {StatusUnknown, ClassUnknown, false, ""},
	} {
		in := AgentViewInput{
			Key: AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"}, AgentProgram: "claude",
			Status: tc.status,
			Fold:   FoldResult{Known: true, Status: tc.status, Class: tc.class, Since: 500, EverWorking: true},
		}
		if tc.task {
			in.Transcript.Apply(TranscriptLine{StartID: "ag1"})
		}
		in.Transcript.Apply(TranscriptLine{TurnEnded: true})
		in.Transcript.Apply(TranscriptLine{Recap: recap})
		v := NewAgentView(in)
		if v.Recap != tc.want || v.Background != tc.task {
			t.Errorf("%s: recap %q, background %v; want %q, %v", name, v.Recap, v.Background, tc.want, tc.task)
		}
	}
}
