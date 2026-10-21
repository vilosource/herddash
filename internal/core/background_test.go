package core

import (
	"reflect"
	"testing"
)

// The children of a Claude Code process, as observed on 2026-10-21.
var (
	toolShell = []string{"/bin/zsh", "-c", "source /h/.claude-cldw/shell-snapshots/snapshot-zsh-1760000000000-abc.sh && eval 'sleep 75' < /dev/null && pwd -P >| /tmp/claude-1-cwd"}
	mcpServer = []string{"node", "/h/.npm/_npx/x/node_modules/.bin/mcp-server"}
	gopls     = []string{"gopls", "serve"}
	caffeine  = []string{"caffeinate", "-i", "-t", "300"}
)

func TestHasBackground(t *testing.T) {
	cases := []struct {
		name     string
		program  AgentProgram
		children [][]string
		want     bool
	}{
		{"tool shell", "claude", [][]string{mcpServer, toolShell}, true},
		{"only MCP, gopls and caffeinate", "claude", [][]string{mcpServer, gopls, caffeine}, false},
		{"no children", "claude", nil, false},
		{"empty argv", "claude", [][]string{nil, {}}, false},
		{"snapshot path without the directory", "claude", [][]string{{"zsh", "-c", "source snapshot-zsh-1.sh"}}, false},
		{"other program", "codex", [][]string{toolShell}, false},
		{"no program", "", [][]string{toolShell}, false},
	}
	for _, c := range cases {
		if got := HasBackground(c.program, c.children); got != c.want {
			t.Errorf("%s: HasBackground = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestChecksBackground(t *testing.T) {
	for _, s := range []Status{StatusDone, StatusIdle} {
		if !ChecksBackground("claude", s) {
			t.Errorf("claude %s: not checked", s)
		}
	}
	for _, s := range []Status{StatusWorking, StatusBlocked, StatusUnknown, ""} {
		if ChecksBackground("claude", s) {
			t.Errorf("claude %s: checked", s)
		}
	}
	if ChecksBackground("codex", StatusDone) {
		t.Error("codex done: checked, with no rule")
	}
}

// TestBackgroundView is the spec's I/O matrix at the view: when Background
// shows, its class rank, and that the class and Waiting Time do not change.
func TestBackgroundView(t *testing.T) {
	cases := []struct {
		name     string
		program  AgentProgram
		status   Status
		class    AttentionClass
		children [][]string
		want     bool
		rank     int
	}{
		{"background shell", "claude", StatusDone, ClassFinished, [][]string{toolShell}, true, 2},
		{"background shell, idle", "claude", StatusIdle, ClassFinished, [][]string{mcpServer, toolShell}, true, 2},
		{"really finished", "claude", StatusIdle, ClassFinished, [][]string{mcpServer, gopls, caffeine}, false, 3},
		{"working with a shell", "claude", StatusWorking, ClassWorking, [][]string{toolShell}, false, 1},
		{"idle never worked, shell", "claude", StatusIdle, ClassWorking, [][]string{toolShell}, true, 2},
		{"idle never worked, no shell", "claude", StatusIdle, ClassWorking, nil, false, 4},
		{"other program", "codex", StatusDone, ClassFinished, [][]string{toolShell}, false, 3},
		{"needs you", "claude", StatusBlocked, ClassNeedsYou, [][]string{toolShell}, false, 0},
		{"unknown", "claude", StatusUnknown, ClassUnknown, [][]string{toolShell}, false, 5},
		// A fold whose class is needs_you while the Status reads done: never
		// Background.
		{"needs you class, done Status", "claude", StatusDone, ClassNeedsYou, [][]string{toolShell}, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := AgentViewInput{
				Key: key("w1:p1"), AgentProgram: c.program, Profile: "cldw", Status: c.status,
				Fold:       FoldResult{Known: true, Status: c.status, Class: c.class, Since: 100, SinceProvenance: ProvenanceLive},
				Context:    PaneContext{WorkspacePosition: 1},
				Background: HasBackground(c.program, c.children),
			}
			v := NewAgentView(in)
			if v.Background != c.want || v.SortKey.ClassRank != c.rank {
				t.Errorf("background %v rank %d, want %v rank %d", v.Background, v.SortKey.ClassRank, c.want, c.rank)
			}
			in.Background = false
			plain := NewAgentView(in)
			if v.Class != plain.Class || v.WaitingSince != plain.WaitingSince || v.WaitingExact != plain.WaitingExact ||
				v.SortKey.Zone != plain.SortKey.Zone || v.SortKey.Group != plain.SortKey.Group {
				t.Errorf("Background changed more than the rank: %+v vs %+v", v, plain)
			}
		})
	}
}

// TestBackgroundOrder: inside a workspace group, Background sorts after
// Working and before Finished, longest wait first.
func TestBackgroundOrder(t *testing.T) {
	bg := func(pane string, status Status, class AttentionClass, since int64) AgentView {
		return NewAgentView(AgentViewInput{
			Key: key(pane), AgentProgram: "claude", Profile: "cldp", Status: status,
			Fold:    FoldResult{Known: true, Status: status, Class: class, Since: since, SinceProvenance: ProvenanceLive},
			Context: PaneContext{WorkspacePosition: 1}, Background: true,
		})
	}
	vs := []AgentView{
		view("fin", StatusDone, 1, 1),
		bg("bg-late", StatusIdle, ClassWorking, 9),
		view("working", StatusWorking, 50, 1),
		neverWorkingView("idle", 2, 1),
		bg("bg-early", StatusDone, ClassFinished, 3),
		view("needs", StatusBlocked, 60, 1),
	}
	SortViews(vs)
	want := []string{"needs", "working", "bg-early", "bg-late", "fin", "idle"}
	if got := panes(vs); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
