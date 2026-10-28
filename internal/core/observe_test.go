package core

import "testing"

func TestLifecycleKindValid(t *testing.T) {
	tests := []struct {
		kind LifecycleKind
		want bool
	}{
		{LifecycleCreated, true},
		{LifecycleClosed, true},
		{LifecycleMoved, true},
		{LifecycleExited, true},
		{LifecycleAgentDetected, true},
		{LifecycleAgentReleased, true},
		{LifecycleContextChanged, true},
		{LifecycleFocused, true},
		{"workspace_renamed", false},
		{"", false},
		{"pane_created", false},
		{"pane_focused", false},
		{"tab_focused", false},
	}
	for _, tt := range tests {
		if got := tt.kind.Valid(); got != tt.want {
			t.Errorf("LifecycleKind(%q).Valid() = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestSnapshotPane(t *testing.T) {
	seq := uint64(9)
	s := Snapshot{
		HerdrVersion: "0.9.5",
		Panes: []PaneObservation{
			{PaneID: "w1:p1", TerminalID: "term_a", Context: PaneContext{ProjectDir: "/src/x", WorkspaceLabel: "probe", TabLabel: "1", WorkspacePosition: 1, PaneTitle: "fix-tests"}, Agent: &AgentObservation{AgentProgram: "claude", Status: StatusWorking, StateChangeSeq: &seq}},
			{PaneID: "w1:p3", TerminalID: "term_b"},
		},
	}
	tests := []struct {
		id       string
		found    bool
		terminal string
		agent    bool
	}{
		{"w1:p1", true, "term_a", true},
		{"w1:p3", true, "term_b", false},
		{"w9:p9", false, "", false},
		{"", false, "", false},
	}
	for _, tt := range tests {
		p, ok := s.Pane(tt.id)
		if ok != tt.found || p.TerminalID != tt.terminal || (p.Agent != nil) != tt.agent || (tt.agent && p.Context.PaneTitle != "fix-tests") {
			t.Errorf("Pane(%q) = %+v, %v", tt.id, p, ok)
		}
	}
}

func TestObservedStatusIsVerbatim(t *testing.T) {
	// Observation types carry herdr's Status strings unvalidated; validation
	// happens when a Transition is built.
	e := StatusEvent{PaneID: "w1:p1", Status: Status("brand_new")}
	if e.Status.Valid() || string(e.Status) != "brand_new" {
		t.Errorf("status %q", e.Status)
	}
}
