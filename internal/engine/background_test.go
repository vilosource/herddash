package engine

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/procenv"
)

// Children of a Claude Code process, as observed on 2026-10-21.
var (
	toolShell = procenv.Process{PID: 205, Argv: []string{"/bin/zsh", "-c",
		"source /h/.claude-cldw/shell-snapshots/snapshot-zsh-1760000000000-abc.sh && eval 'sleep 75'"}}
	mcpServer = procenv.Process{PID: 120, Argv: []string{"node", "mcp-server.js"}}
	caffeine  = procenv.Process{PID: 130, Argv: []string{"caffeinate", "-i"}}
)

// fakeChildren is a scripted process table, standing in for procenv.Children
// so no engine test reads a real process.
type fakeChildren struct {
	mu    sync.Mutex
	kids  map[int][]procenv.Process
	errs  map[int]error
	calls atomic.Int32
	// block, when set, holds every read until it is closed.
	block chan struct{}
}

func newFakeChildren() *fakeChildren {
	return &fakeChildren{kids: map[int][]procenv.Process{}, errs: map[int]error{}}
}

func (f *fakeChildren) read(pid int) ([]procenv.Process, error) {
	f.calls.Add(1)
	f.mu.Lock()
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[pid]; err != nil {
		return nil, err
	}
	return append([]procenv.Process(nil), f.kids[pid]...), nil
}

func (f *fakeChildren) set(pid int, ps ...procenv.Process) {
	f.mu.Lock()
	f.kids[pid] = ps
	f.mu.Unlock()
}

func (f *fakeChildren) fail(pid int, err error) {
	f.mu.Lock()
	f.errs[pid] = err
	f.mu.Unlock()
}

// awaitCalls waits until at least n reads have been made.
func (f *fakeChildren) awaitCalls(t *testing.T, n int32) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for f.calls.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d children reads, want %d", f.calls.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// fastBackground lowers the background tunables for one test.
func fastBackground(t *testing.T) {
	t.Helper()
	d, i := backgroundCheckDelay, backgroundInterval
	backgroundCheckDelay, backgroundInterval = 5*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { backgroundCheckDelay, backgroundInterval = d, i })
}

// startBackground runs a viewed engine on one Agent, w1:p1, of program with
// Status status, whose process p1PID resolves to cldw under rules for claude
// and codex, its children read from kids, the log collected; it waits until
// the Agent is in the View.
func startBackground(t *testing.T, program core.AgentProgram, status core.Status, kids *fakeChildren) (*harness, *watcher, *logSink) {
	t.Helper()
	fastBackground(t)
	fake := newFakeClient(t, oneAgent(program, status))
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	logs := &logSink{}
	h, w := startViewed(t, fake, func(o *Options, _ *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule(), "codex": claudeRule()}
		o.Environ = env.lookup
		o.Children = kids.read
		o.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	w.await("the agent", func(ps []Publication) bool { return len(ps) > 0 && len(ps[len(ps)-1].View.Agents) == 1 })
	return h, w, logs
}

// lastAgent is the one Agent's view in the latest publication.
func lastAgent(ps []Publication) core.AgentView {
	return ps[len(ps)-1].View.Agents[0]
}

// awaitBackground waits until the View shows the Agent with background
// set as want, and returns that view.
func awaitBackground(w *watcher, want bool) core.AgentView {
	w.t.Helper()
	ps := w.await("background", func(ps []Publication) bool {
		return len(ps) > 0 && len(ps[len(ps)-1].View.Agents) == 1 && lastAgent(ps).Background == want
	})
	return lastAgent(ps)
}

// flips counts the publications in which the Agent's background changed.
func flips(ps []Publication) int {
	n := 0
	for i := 1; i < len(ps); i++ {
		a, b := ps[i-1].View.Agents, ps[i].View.Agents
		if len(a) == 1 && len(b) == 1 && a[0].Background != b[0].Background {
			n++
		}
	}
	return n
}

// TestBackgroundShell covers the matrix's "Background shell" and "Shell ends"
// rows: a done claude Agent whose process has a tool-shell child reads
// Background, ranked 2, and reads Finished again once the shell is gone, with
// nothing recorded either way.
func TestBackgroundShell(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, mcpServer, toolShell)
	h, w, logs := startBackground(t, "claude", core.StatusDone, kids)
	v := awaitBackground(w, true)
	if v.Class != core.ClassFinished || v.SortKey.ClassRank != 2 || v.Status != core.StatusDone {
		t.Errorf("background view %+v", v)
	}
	n := len(w.all())
	kids.set(p1PID, mcpServer)
	v = awaitBackground(w, false)
	if v.SortKey.ClassRank != 3 {
		t.Errorf("finished view rank %d, want 3", v.SortKey.ClassRank)
	}
	after := w.all()[n:]
	for _, p := range after {
		for _, c := range p.Changes {
			if c.Kind != ChangeAgent {
				t.Errorf("a background change published %s", c.Kind)
			}
		}
	}
	// Further reads find the same: nothing more is published.
	calls := kids.calls.Load()
	kids.awaitCalls(t, calls+3)
	h.settle()
	if ps := w.all(); flips(ps) != 2 {
		t.Errorf("%d background changes published, want 2", flips(ps))
	}
	h.rec.expect(t, "agent w1:p1 claude done")
	if e := logs.entries(t, "DEBUG", "background changed"); len(e) != 2 {
		t.Errorf("background changes logged %v, want two", e)
	}
}

// TestBackgroundReallyFinished: an idle claude Agent with only MCP servers
// and caffeinate as children is never Background, though it is read.
func TestBackgroundReallyFinished(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, mcpServer, caffeine)
	h, w, _ := startBackground(t, "claude", core.StatusIdle, kids)
	kids.awaitCalls(t, 3)
	h.settle()
	ps := w.all()
	if v := lastAgent(ps); v.Background || flips(ps) != 0 {
		t.Errorf("view %+v, %d background changes", v, flips(ps))
	}
}

// TestBackgroundWorking: a working claude Agent with a shell child is never
// Background, and its children are not even read: the shell may be a
// foreground tool call.
func TestBackgroundWorking(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	h, w, _ := startBackground(t, "claude", core.StatusWorking, kids)
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	if n := kids.calls.Load(); n != 0 {
		t.Errorf("%d children reads for a working Agent", n)
	}
	if v := lastAgent(w.all()); v.Background || v.SortKey.ClassRank != 1 {
		t.Errorf("view %+v", v)
	}
}

// TestBackgroundIdleNeverWorked: an Agent idle since before it was seen
// working (class working, Status idle) with a shell child reads Background.
func TestBackgroundIdleNeverWorked(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	_, w, _ := startBackground(t, "claude", core.StatusIdle, kids)
	v := awaitBackground(w, true)
	if v.Class != core.ClassWorking || v.Status != core.StatusIdle || v.SortKey.ClassRank != 2 {
		t.Errorf("view %+v", v)
	}
}

// TestBackgroundOtherProgram: a done codex Agent with a shell-snapshot child,
// its process known, is not Background and its children are not read.
func TestBackgroundOtherProgram(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	h, w, _ := startBackground(t, "codex", core.StatusDone, kids)
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	if n := kids.calls.Load(); n != 0 {
		t.Errorf("%d children reads for codex", n)
	}
	if v := lastAgent(w.all()); v.Background {
		t.Errorf("view %+v", v)
	}
}

// TestBackgroundProcessGone: a failed read is not Background, logged once at
// debug level and never as a warning, and reads go on.
func TestBackgroundProcessGone(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	h, w, logs := startBackground(t, "claude", core.StatusDone, kids)
	awaitBackground(w, true)
	kids.fail(p1PID, errors.New("no such process"))
	awaitBackground(w, false)
	calls := kids.calls.Load()
	kids.awaitCalls(t, calls+3)
	h.settle()
	if e := logs.entries(t, "DEBUG", "background not read"); len(e) != 1 || e[0]["error"] == nil {
		t.Errorf("debug entries %v, want one naming the error", e)
	}
	if e := logs.entries(t, "WARN", ""); len(e) != 0 {
		t.Errorf("warnings %v", e)
	}
	if e := logs.entries(t, "ERROR", ""); len(e) != 0 {
		t.Errorf("errors %v", e)
	}
}

// TestBackgroundStatusLeaves: a working Transition ends Background in the
// Transition's own view, and the reads stop; done again starts them again.
func TestBackgroundStatusLeaves(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	h, w, _ := startBackground(t, "claude", core.StatusDone, kids)
	awaitBackground(w, true)
	h.fake.awaitSubscribe("w1:p1")
	h.settle()

	h.fake.setSnapshot(oneAgent("claude", core.StatusWorking))
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusWorking, AgentProgram: "claude"})
	ps := w.await("the working transition", func(ps []Publication) bool { return hasKind(ChangeTransition)(ps[len(ps)-1]) })
	for _, c := range ps[len(ps)-1].Changes {
		if c.Kind == ChangeTransition && (c.Agent.Background || c.Agent.SortKey.ClassRank != 1) {
			t.Errorf("transition view %+v", c.Agent)
		}
	}
	h.settle()
	calls := kids.calls.Load()
	time.Sleep(10 * backgroundInterval)
	if n := kids.calls.Load(); n != calls {
		t.Errorf("%d children reads after the Status left done", n-calls)
	}
	if v := lastAgent(w.all()); v.Background {
		t.Errorf("view %+v", v)
	}

	h.fake.setSnapshot(oneAgent("claude", core.StatusDone))
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "claude"})
	awaitBackground(w, true)
}

// TestBackgroundProgramChanged: another program in the pane clears the
// process, and so Background, until its Profile resolution reads the new one.
func TestBackgroundProgramChanged(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	h, w, _ := startBackground(t, "claude", core.StatusDone, kids)
	awaitBackground(w, true)
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.setSnapshot(oneAgent("codex", core.StatusDone))
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusDone, AgentProgram: "codex"})
	v := awaitBackground(w, false)
	if v.AgentProgram != "codex" {
		t.Errorf("view %+v", v)
	}
}

// A children read that never returns is abandoned at backgroundTimeout and is
// not joined by another read every backgroundInterval while it hangs.
func TestBackgroundReadHangs(t *testing.T) {
	to := backgroundTimeout
	backgroundTimeout = 20 * time.Millisecond
	t.Cleanup(func() { backgroundTimeout = to })
	kids := newFakeChildren()
	kids.block = make(chan struct{})
	t.Cleanup(func() { close(kids.block) })
	startBackground(t, "claude", core.StatusDone, kids)
	kids.awaitCalls(t, 1)
	time.Sleep(20 * backgroundInterval)
	if n := kids.calls.Load(); n != 1 {
		t.Errorf("%d children reads while the first hangs, want 1", n)
	}
}

// A Profile lookup that fails on re-detection keeps the process already
// known, so the watch goes on reading its children.
func TestBackgroundKeepsProcessOnFailedLookup(t *testing.T) {
	kids := newFakeChildren()
	kids.set(p1PID, toolShell)
	h, w, _ := startBackground(t, "claude", core.StatusDone, kids)
	awaitBackground(w, true)
	calls := h.fake.processCalls()
	h.fake.clearProc("w1:p1")
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	deadline := time.Now().Add(wait)
	for h.fake.processCalls() == calls {
		if time.Now().After(deadline) {
			t.Fatal("no process lookup after re-detection")
		}
		time.Sleep(time.Millisecond)
	}
	h.settle()
	time.Sleep(10 * backgroundInterval)
	if v := lastAgent(w.all()); !v.Background {
		t.Errorf("Background lost after a failed lookup: %+v", v)
	}
}
