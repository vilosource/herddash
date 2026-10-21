package engine

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/procenv"
)

// noSession is the engine tests' default Session: no record for anyone, so
// no engine test reads a real one.
func noSession(string, int) (procenv.Session, error) {
	return procenv.Session{}, procenv.ErrSession
}

// fakeSessions is a scripted set of session records, standing in for
// procenv.ClaudeSession.
type fakeSessions struct {
	mu    sync.Mutex
	recs  map[int]procenv.Session
	errs  map[int]error
	dirs  []string
	calls atomic.Int32
	// block, when set, holds every read until it is closed.
	block chan struct{}
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{recs: map[int]procenv.Session{}, errs: map[int]error{}}
}

func (f *fakeSessions) read(dir string, pid int) (procenv.Session, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.dirs = append(f.dirs, dir)
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[pid]; err != nil {
		return procenv.Session{}, err
	}
	s, ok := f.recs[pid]
	if !ok {
		return procenv.Session{}, procenv.ErrSession
	}
	return s, nil
}

// set makes pid's record say status, set at updatedAt.
func (f *fakeSessions) set(pid int, status string, updatedAt int64) {
	f.mu.Lock()
	f.recs[pid] = procenv.Session{PID: pid, Status: status, StatusUpdatedAt: updatedAt}
	delete(f.errs, pid)
	f.mu.Unlock()
}

func (f *fakeSessions) fail(pid int, err error) {
	f.mu.Lock()
	f.errs[pid] = err
	f.mu.Unlock()
}

func (f *fakeSessions) dirList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.dirs...)
}

// awaitCalls waits until at least n reads have been made.
func (f *fakeSessions) awaitCalls(t *testing.T, n int32) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for f.calls.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d session reads, want %d", f.calls.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// moreReads waits for three more reads than have been made, then for the
// engine to settle, so whatever they caused has been handled.
func (f *fakeSessions) moreReads(t *testing.T, h *harness) {
	t.Helper()
	f.awaitCalls(t, f.calls.Load()+3)
	h.settle()
}

// manualClock is a clock the test moves by hand, in UTC Unix milliseconds.
type manualClock struct{ ms atomic.Int64 }

func (c *manualClock) now() time.Time { return time.UnixMilli(c.ms.Load()) }

// base is the manual clock's start: the first sight's observed_at.
const base int64 = 1_800_000_000_000

// sessionRig is one engine on one Agent, w1:p1, its process p1PID, with a
// recording store, a view watcher, the log and a manual clock.
type sessionRig struct {
	h     *harness
	st    *fakeStore
	w     *watcher
	logs  *logSink
	clock *manualClock
}

// startSession runs a viewed, recording engine on one Agent of program with
// Status status, whose process p1PID has CLAUDE_CONFIG_DIR /h/.claude-cldw
// (unless opts changes it), its session records read through read; it waits
// until the Agent's first sight is recorded.
func startSession(t *testing.T, program core.AgentProgram, status core.Status,
	read SessionFunc, opts func(*Options, *fakeEnviron)) *sessionRig {
	t.Helper()
	si := sessionInterval
	sessionInterval = 5 * time.Millisecond
	t.Cleanup(func() { sessionInterval = si })
	fake := newFakeClient(t, oneAgent(program, status))
	fake.setProc("w1:p1", p1PID)
	st := newFakeStore(fake)
	env := newFakeEnviron(t)
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	r := &sessionRig{st: st, logs: &logSink{}, clock: &manualClock{}}
	r.clock.ms.Store(base)
	r.h, r.w = startViewed(t, fake, func(o *Options, _ *recorder) {
		o.Store = st
		o.Clock = r.clock.now
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule(), "codex": claudeRule()}
		o.Environ = env.lookup
		o.Session = read
		o.Logger = slog.New(slog.NewJSONHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		if opts != nil {
			opts(o, env)
		}
	})
	r.w.await("the agent", func(ps []Publication) bool { return len(ps) > 0 && len(ps[len(ps)-1].View.Agents) == 1 })
	r.st.awaitRows(t, 1)
	r.h.fake.awaitSubscribe("w1:p1")
	return r
}

// at moves the clock to base+ms.
func (r *sessionRig) at(ms int64) { r.clock.ms.Store(base + ms) }

// event sends herdr's status event for w1:p1.
func (r *sessionRig) event(program core.AgentProgram, s core.Status) {
	r.h.fake.setSnapshot(oneAgent(program, s))
	r.h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: s, AgentProgram: program})
}

// sessionRow is a recorded Transition as these tests compare it.
type sessionRow struct {
	prev, next core.Status
	by         core.Observer
	at         int64
	prov       core.Provenance
}

func describeRow(tr core.Transition) sessionRow {
	prev, _ := tr.Previous()
	return sessionRow{prev, tr.New(), tr.ObservedBy(), tr.ObservedAt() - base, tr.Provenance()}
}

// expectSessionRows waits for len(want) rows after the first sight and checks
// them, and that no more were written.
func (r *sessionRig) expectSessionRows(t *testing.T, want ...sessionRow) {
	t.Helper()
	rows := r.st.awaitRows(t, 1+len(want))
	if len(rows) != 1+len(want) {
		t.Fatalf("%d rows, want %d", len(rows), 1+len(want))
	}
	for i, w := range want {
		if got := describeRow(rows[1+i]); got != w {
			t.Errorf("row %d is %+v, want %+v", 1+i, got, w)
		}
	}
}

// view is the one Agent's view in the latest publication.
func (r *sessionRig) view() core.AgentView { return lastAgent(r.w.all()) }

// TestSessionMissedQuestion covers the matrix's "Missed question" row: herdr
// says done, the record says waiting since T: a live Transition to blocked at
// T, observed by claude_session, and the view is Needs you. The record is
// read under the process's CLAUDE_CONFIG_DIR.
func TestSessionMissedQuestion(t *testing.T) {
	sess := newFakeSessions()
	sess.set(p1PID, "idle", base-60_000)
	r := startSession(t, "claude", core.StatusDone, sess.read, nil)
	sess.moreReads(t, r.h)
	r.at(10_000)
	sess.set(p1PID, "waiting", base+5_000)
	r.expectSessionRows(t, sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 5_000, core.ProvenanceLive})
	v := r.w.await("needs you", func(ps []Publication) bool { return lastAgent(ps).Class == core.ClassNeedsYou })
	if a := lastAgent(v); a.Status != core.StatusBlocked || a.SortKey.Zone != 0 {
		t.Errorf("view %+v", a)
	}
	if a := agentByPane(t, r.h.agents(), "w1:p1"); a.WaitingSince != base+5_000 || !a.WaitingExact {
		t.Errorf("waiting since %d exact %v, want T, exact", a.WaitingSince-base, a.WaitingExact)
	}
	r.h.rec.expect(t, "agent w1:p1 claude done", "transition w1:p1 claude done->blocked live")
	for _, d := range sess.dirList() {
		if d != "/h/.claude-cldw" {
			t.Fatalf("record read under %q, want /h/.claude-cldw", d)
		}
	}
	// It stays waiting: nothing more is recorded.
	sess.moreReads(t, r.h)
	r.expectSessionRows(t, sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 5_000, core.ProvenanceLive})
	if e := r.logs.entries(t, "DEBUG", "claude session"); len(e) == 0 {
		t.Error("no session change logged")
	}
}

// TestSessionQuestionAnswered covers the matrix's "Question answered" row:
// herdr says working while waiting holds (nothing recorded), then the record
// leaves waiting: a Transition to working, observed by claude_session.
func TestSessionQuestionAnswered(t *testing.T) {
	sess := newFakeSessions()
	sess.set(p1PID, "waiting", base)
	r := startSession(t, "claude", core.StatusDone, sess.read, nil)
	blocked := sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 0, core.ProvenanceLive}
	r.expectSessionRows(t, blocked)
	r.at(1_000)
	r.event("claude", core.StatusWorking)
	sess.moreReads(t, r.h)
	r.expectSessionRows(t, blocked)
	if v := r.view(); v.Status != core.StatusBlocked {
		t.Errorf("view %+v while waiting", v)
	}
	r.at(3_000)
	sess.set(p1PID, "busy", base+2_000)
	r.expectSessionRows(t, blocked,
		sessionRow{core.StatusBlocked, core.StatusWorking, core.ObservedByClaudeSession, 2_000, core.ProvenanceLive})
	r.w.await("working", func(ps []Publication) bool { return lastAgent(ps).Status == core.StatusWorking })
}

// TestSessionHerdrAlsoBlocked covers the matrix's "herdr also blocked" row:
// no extra Transition while waiting, nor when it ends with herdr still
// blocked.
func TestSessionHerdrAlsoBlocked(t *testing.T) {
	sess := newFakeSessions()
	sess.set(p1PID, "waiting", base)
	r := startSession(t, "claude", core.StatusBlocked, sess.read, nil)
	sess.moreReads(t, r.h)
	r.expectSessionRows(t)
	sess.set(p1PID, "busy", base+1)
	sess.moreReads(t, r.h)
	r.expectSessionRows(t)
	r.h.rec.expect(t, "agent w1:p1 claude blocked")
}

// TestSessionHerdrMovesDuringWaiting covers the matrix's "herdr moves during
// waiting" row: done then idle from herdr records nothing while waiting
// holds; when it ends, the Agent returns to idle.
func TestSessionHerdrMovesDuringWaiting(t *testing.T) {
	sess := newFakeSessions()
	sess.set(p1PID, "waiting", base)
	r := startSession(t, "claude", core.StatusDone, sess.read, nil)
	blocked := sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 0, core.ProvenanceLive}
	r.expectSessionRows(t, blocked)
	r.event("claude", core.StatusIdle)
	sess.moreReads(t, r.h)
	// A reconcile read agrees too: herdr's idle is not a disagreement.
	r.h.fake.setSnapshot(oneAgent("claude", core.StatusIdle))
	r.h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged, PaneID: "w1:p1"})
	sess.moreReads(t, r.h)
	r.expectSessionRows(t, blocked)
	r.at(9_000)
	sess.set(p1PID, "idle", base+8_000)
	r.expectSessionRows(t, blocked,
		sessionRow{core.StatusBlocked, core.StatusIdle, core.ObservedByClaudeSession, 8_000, core.ProvenanceLive})
}

// writeRecord writes w1:p1's session record under dir/sessions.
func writeRecord(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions", strconv.Itoa(p1PID)+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestSessionRecordUnusable covers the matrix's "Record missing / bad / pid
// mismatch" row with the real reader on a temporary home: herdr's Status
// alone, one debug entry, no warning. The process has no CLAUDE_CONFIG_DIR,
// so the record is read under the Daemon's ~/.claude.
func TestSessionRecordUnusable(t *testing.T) {
	for name, body := range map[string]string{
		"missing":      "",
		"bad":          `{"pid":` + strconv.Itoa(p1PID) + `,"status":`,
		"pid mismatch": `{"pid":7,"status":"waiting","statusUpdatedAt":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if body != "" {
				writeRecord(t, filepath.Join(home, ".claude"), body)
			}
			var calls atomic.Int32
			read := func(dir string, pid int) (procenv.Session, error) {
				calls.Add(1)
				return procenv.ClaudeSession(dir, pid)
			}
			r := startSession(t, "claude", core.StatusDone, read, func(o *Options, env *fakeEnviron) {
				o.Home = home
				env.vars = map[int]map[string]string{}
			})
			for n := int32(0); calls.Load() < 5; n++ {
				time.Sleep(time.Millisecond)
				if n > 5000 {
					t.Fatal("too few session reads")
				}
			}
			r.h.settle()
			r.expectSessionRows(t)
			if v := r.view(); v.Status != core.StatusDone || v.Class != core.ClassFinished {
				t.Errorf("view %+v", v)
			}
			if e := r.logs.entries(t, "DEBUG", "claude session not read"); len(e) != 1 || e[0]["error"] == nil {
				t.Errorf("debug entries %v, want one naming the error", e)
			}
			if e := r.logs.entries(t, "WARN", ""); len(e) != 0 {
				t.Errorf("warnings %v", e)
			}
		})
	}
}

// TestSessionRecordGoes: a waiting record that becomes unreadable returns the
// Agent to herdr's Status at the read time, observed by claude_session; the
// real reader, a temporary home and the default ~/.claude again.
func TestSessionRecordGoes(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	writeRecord(t, dir, `{"pid":`+strconv.Itoa(p1PID)+`,"sessionId":"x","cwd":"/secret/cwd","status":"waiting","statusUpdatedAt":`+
		strconv.FormatInt(base, 10)+`}`)
	r := startSession(t, "claude", core.StatusDone, procenv.ClaudeSession, func(o *Options, env *fakeEnviron) {
		o.Home = home
		env.vars = map[int]map[string]string{}
	})
	blocked := sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 0, core.ProvenanceLive}
	r.expectSessionRows(t, blocked)
	r.at(4_000)
	writeRecord(t, dir, `not json`)
	r.expectSessionRows(t, blocked,
		sessionRow{core.StatusBlocked, core.StatusDone, core.ObservedByClaudeSession, 4_000, core.ProvenanceLive})
	r.logs.mu.Lock()
	logged := r.logs.b.String()
	r.logs.mu.Unlock()
	if strings.Contains(logged, "/secret/cwd") || strings.Contains(logged, `"x"`) {
		t.Errorf("a record field other than status was logged: %s", logged)
	}
}

// TestSessionNonClaude covers the matrix's "Non-claude" row: a codex Agent
// whose process has a waiting record is not read, and stays done.
func TestSessionNonClaude(t *testing.T) {
	sess := newFakeSessions()
	sess.set(p1PID, "waiting", base)
	r := startSession(t, "codex", core.StatusDone, sess.read, nil)
	r.h.settle()
	time.Sleep(10 * sessionInterval)
	r.h.settle()
	if n := sess.calls.Load(); n != 0 {
		t.Errorf("%d session reads for codex", n)
	}
	r.expectSessionRows(t)
	if v := r.view(); v.Status != core.StatusDone {
		t.Errorf("view %+v", v)
	}
}

// TestSessionObservedAtBad covers the matrix's "statusUpdatedAt in future /
// before last" row: either way observed_at is the read time.
func TestSessionObservedAtBad(t *testing.T) {
	for name, updated := range map[string]int64{
		"in the future":              base + 3_600_000,
		"before the first sight":     base - 1,
		"unknown":                    0,
		"before the last transition": base + 500,
	} {
		t.Run(name, func(t *testing.T) {
			sess := newFakeSessions()
			sess.set(p1PID, "busy", base)
			r := startSession(t, "claude", core.StatusDone, sess.read, nil)
			want := []sessionRow{}
			if name == "before the last transition" {
				r.at(1_000)
				r.event("claude", core.StatusIdle)
				want = append(want, sessionRow{core.StatusDone, core.StatusIdle, core.ObservedByHerdr, 1_000, core.ProvenanceLive})
				r.expectSessionRows(t, want...)
			}
			r.at(2_000)
			sess.set(p1PID, "waiting", updated)
			prev := core.StatusDone
			if len(want) > 0 {
				prev = core.StatusIdle
			}
			// A stamp earlier than the Agent's previous Transition is clamped to
			// it; one in the future, or none, gives the read time.
			at := int64(2_000)
			switch name {
			case "before the first sight":
				at = 0
			case "before the last transition":
				at = 1_000
			}
			want = append(want, sessionRow{prev, core.StatusBlocked, core.ObservedByClaudeSession, at, core.ProvenanceLive})
			r.expectSessionRows(t, want...)
		})
	}
}

// TestSessionProgramChanged: another program in the pane ends the watch, and
// a Status the record caused returns to herdr's.
func TestSessionProgramChanged(t *testing.T) {
	sess := newFakeSessions()
	sess.set(p1PID, "waiting", base)
	r := startSession(t, "claude", core.StatusDone, sess.read, nil)
	blocked := sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 0, core.ProvenanceLive}
	r.expectSessionRows(t, blocked)
	r.at(1_000)
	r.event("codex", core.StatusDone)
	r.expectSessionRows(t, blocked,
		sessionRow{core.StatusBlocked, core.StatusDone, core.ObservedByClaudeSession, 1_000, core.ProvenanceLive})
	r.h.settle()
	calls := sess.calls.Load()
	time.Sleep(10 * sessionInterval)
	if n := sess.calls.Load(); n != calls {
		t.Errorf("%d session reads after the program changed", n-calls)
	}
}

// TestSessionRemoved: removing the Agent stops its reads and records nothing
// more.
func TestSessionRemoved(t *testing.T) {
	sess := newFakeSessions()
	sess.set(p1PID, "waiting", base)
	r := startSession(t, "claude", core.StatusDone, sess.read, nil)
	r.expectSessionRows(t, sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 0, core.ProvenanceLive})
	r.h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
	r.w.await("removed", func(ps []Publication) bool { return len(ps[len(ps)-1].View.Agents) == 0 })
	r.h.settle()
	calls := sess.calls.Load()
	time.Sleep(10 * sessionInterval)
	if n := sess.calls.Load(); n != calls {
		t.Errorf("%d session reads after removal", n-calls)
	}
	r.expectSessionRows(t, sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 0, core.ProvenanceLive})
}

// A session read that never returns is abandoned at sessionTimeout and is not
// joined by another read every sessionInterval while it hangs.
func TestSessionReadHangs(t *testing.T) {
	to := sessionTimeout
	sessionTimeout = 20 * time.Millisecond
	t.Cleanup(func() { sessionTimeout = to })
	sess := newFakeSessions()
	sess.block = make(chan struct{})
	t.Cleanup(func() { close(sess.block) })
	r := startSession(t, "claude", core.StatusDone, sess.read, nil)
	sess.awaitCalls(t, 1)
	time.Sleep(20 * sessionInterval)
	if n := sess.calls.Load(); n != 1 {
		t.Errorf("%d session reads while the first hangs, want 1", n)
	}
	if e := r.logs.entries(t, "DEBUG", "claude session not read"); len(e) != 1 {
		t.Errorf("debug entries %v, want one", e)
	}
}

// The configuration directory is read from the environment once per process:
// a later environment failure does not stop the reads.
func TestSessionEnvironmentFails(t *testing.T) {
	sess := newFakeSessions()
	sess.set(p1PID, "waiting", base)
	var env *fakeEnviron
	r := startSession(t, "claude", core.StatusDone, sess.read, func(_ *Options, e *fakeEnviron) { env = e })
	r.expectSessionRows(t, sessionRow{core.StatusDone, core.StatusBlocked, core.ObservedByClaudeSession, 0, core.ProvenanceLive})
	env.fail(p1PID, errors.New("gone"))
	sess.moreReads(t, r.h)
	if v := r.view(); v.Status != core.StatusBlocked {
		t.Errorf("view %+v", v)
	}
}
