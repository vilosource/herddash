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
		{"background shell", "claude", StatusDone, ClassFinished, [][]string{toolShell}, true, 3},
		{"background shell, idle", "claude", StatusIdle, ClassFinished, [][]string{mcpServer, toolShell}, true, 3},
		{"really finished", "claude", StatusIdle, ClassFinished, [][]string{mcpServer, gopls, caffeine}, false, 1},
		{"working with a shell", "claude", StatusWorking, ClassWorking, [][]string{toolShell}, false, 4},
		{"idle never worked, shell", "claude", StatusIdle, ClassWorking, [][]string{toolShell}, true, 3},
		{"idle never worked, no shell", "claude", StatusIdle, ClassWorking, nil, false, 2},
		{"other program", "codex", StatusDone, ClassFinished, [][]string{toolShell}, false, 1},
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
// Finished and Idle and before Working, the most recent first (operator
// decision 2026-10-21).
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
	want := []string{"needs", "fin", "idle", "bg-late", "bg-early", "working"}
	if got := panes(vs); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// TestTranscriptView covers the matrix's display rows (ADR-0009): the count is
// the transcript's open tasks, plus one for a tool shell no open command
// accounts for; a working Agent with tasks open is Background only once its
// main turn ended; Needs you and Unknown never change.
func TestTranscriptView(t *testing.T) {
	tasks := func(turnEnded bool, lines ...TranscriptLine) BackgroundTasks {
		var b BackgroundTasks
		for _, l := range lines {
			b.Apply(l)
		}
		b.Apply(TranscriptLine{TurnEnded: turnEnded})
		return b
	}
	cmd := TranscriptLine{StartID: "bx1", StartCommand: true}
	sub := TranscriptLine{StartID: "ag1"}
	cases := []struct {
		name   string
		status Status
		class  AttentionClass
		tr     BackgroundTasks
		shell  bool
		count  int
		bg     bool
		rank   int
	}{
		{"bg command open, idle", StatusDone, ClassFinished, tasks(true, cmd), false, 1, true, 3},
		{"bg command closes", StatusDone, ClassFinished, tasks(true, cmd, TranscriptLine{EndID: "bx1", EndStatus: "completed"}), false, 0, false, 1},
		{"main turn over, subagent running", StatusWorking, ClassWorking, tasks(true, sub), false, 1, true, 3},
		{"working with bg", StatusWorking, ClassWorking, tasks(false, cmd), false, 1, false, 4},
		{"working, turn ended, none open", StatusWorking, ClassWorking, tasks(true), false, 0, false, 4},
		{"idle with two open", StatusIdle, ClassWorking, tasks(false, cmd, sub), false, 2, true, 3},
		{"shell accounted for by a command", StatusDone, ClassFinished, tasks(true, cmd), true, 1, true, 3},
		{"shell beside a subagent", StatusDone, ClassFinished, tasks(true, sub), true, 2, true, 3},
		{"shell alone, as built", StatusDone, ClassFinished, tasks(true), true, 1, true, 3},
		{"needs you", StatusBlocked, ClassNeedsYou, tasks(true, cmd, sub), true, 0, false, 0},
		{"unknown", StatusUnknown, ClassUnknown, tasks(true, sub), true, 0, false, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := AgentViewInput{
				Key: key("w1:p1"), AgentProgram: "claude", Profile: "cldw", Status: c.status,
				Fold:       FoldResult{Known: true, Status: c.status, Class: c.class, Since: 100, SinceProvenance: ProvenanceLive},
				Context:    PaneContext{WorkspacePosition: 1},
				Background: c.shell,
				Transcript: c.tr,
			}
			v := NewAgentView(in)
			if v.BackgroundTasks != c.count || v.Background != c.bg || v.SortKey.ClassRank != c.rank {
				t.Errorf("tasks %d background %v rank %d, want %d %v %d", v.BackgroundTasks, v.Background, v.SortKey.ClassRank, c.count, c.bg, c.rank)
			}
			if v.Class != c.class || v.Status != c.status {
				t.Errorf("class %s status %s changed", v.Class, v.Status)
			}
		})
	}
}
