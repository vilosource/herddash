package engine

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/procenv"
)

// Synthetic transcript records, shaped on the markers ADR-0009 names; every
// text is invented and marked SECRET so a test can tell it never leaked.
const (
	trPrompt   = `{"type":"user","isSidechain":false,"message":{"role":"user","content":"PROMPT-SECRET do the thing"}}`
	trEndTurn  = `{"type":"assistant","isSidechain":false,"message":{"content":[{"type":"text","text":"TEXT-SECRET waiting"}],"stop_reason":"end_turn"}}`
	trBgStart  = `{"type":"user","message":{"content":[{"type":"tool_result","content":"RESULT-SECRET"}]},"toolUseResult":{"stdout":"OUTPUT-SECRET","backgroundTaskId":"bk7q2x9"}}`
	trSubStart = `{"type":"user","message":{"content":"RESULT-SECRET"},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"a3f9c2d1","prompt":"PROMPT-SECRET"}}`
)

func trDone(id string) string {
	return `{"type":"queue-operation","operation":"enqueue","content":"<task-notification><task-id>` + id +
		`</task-id><status>completed</status><summary>SUMMARY-SECRET</summary></task-notification>"}`
}

// trCWD and trSession name the transcript under the test's config dir.
const (
	trCWD     = "/w/SECRET.proj"
	trSession = "sess-0001"
)

// transcriptRig is a session rig on one claude Agent whose process has
// CLAUDE_CONFIG_DIR set to a temporary directory, whose session record
// (a fake) names trSession in trCWD, and whose transcript is path.
type transcriptRig struct {
	*sessionRig
	sess *fakeSessions
	path string
}

// startTranscript runs a session rig on a claude Agent of Status status
// whose transcript holds lines; more changes the Options.
func startTranscript(t *testing.T, status core.Status, lines []string, more func(*Options)) *transcriptRig {
	t.Helper()
	cfg := t.TempDir()
	path, err := procenv.ClaudeTranscriptPath(cfg, trCWD, trSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if lines != nil {
		writeTranscript(t, path, lines...)
	}
	sess := newFakeSessions()
	sess.recs[p1PID] = procenv.Session{PID: p1PID, Status: "busy", SessionID: trSession, CWD: trCWD}
	r := startSession(t, "claude", status, sess.read, func(o *Options, env *fakeEnviron) {
		env.set(p1PID, claudeVar, cfg)
		if more != nil {
			more(o)
		}
	})
	return &transcriptRig{sessionRig: r, sess: sess, path: path}
}

// writeTranscript appends lines to the transcript at path.
func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// awaitTasks waits until the Agent's view shows n background tasks and
// Background as bg, and returns it.
func (r *transcriptRig) awaitTasks(n int, bg bool) core.AgentView {
	r.w.t.Helper()
	ps := r.w.await(fmt.Sprintf("%d background tasks, background %v", n, bg), func(ps []Publication) bool {
		return len(ps) > 0 && len(ps[len(ps)-1].View.Agents) == 1 &&
			lastAgent(ps).BackgroundTasks == n && lastAgent(ps).Background == bg
	})
	return lastAgent(ps)
}

// noLeak fails the test if the log carries any transcript text, the working
// directory, its slug or the session id.
func (r *transcriptRig) noLeak(t *testing.T) {
	t.Helper()
	r.logs.mu.Lock()
	logged := r.logs.b.String()
	r.logs.mu.Unlock()
	for _, s := range []string{"SECRET", trSession, "bk7q2x9", "a3f9c2d1"} {
		if strings.Contains(logged, s) {
			t.Errorf("the log carries %q: %s", s, logged)
		}
	}
}

// TestTranscriptSubagentAfterTurn covers the matrix's "Main turn over,
// subagent running" row: herdr says working, the main turn ended with a
// subagent open: Background (rank 3), one task; its notification makes it
// Working again. Nothing is recorded either way.
func TestTranscriptSubagentAfterTurn(t *testing.T) {
	r := startTranscript(t, core.StatusWorking, []string{trPrompt, trSubStart, trEndTurn}, nil)
	v := r.awaitTasks(1, true)
	if v.Status != core.StatusWorking || v.Class != core.ClassWorking || v.SortKey.ClassRank != 3 {
		t.Errorf("view %+v", v)
	}
	writeTranscript(t, r.path, trDone("a3f9c2d1"))
	v = r.awaitTasks(0, false)
	if v.SortKey.ClassRank != 4 {
		t.Errorf("rank %d, want Working", v.SortKey.ClassRank)
	}
	r.expectSessionRows(t)
	r.noLeak(t)
}

// TestTranscriptCommandIdle covers the matrix's "Bg command open, idle" and
// "Bg command closes" rows: herdr says done with a background command open:
// Background, one task; closed: Finished.
func TestTranscriptCommandIdle(t *testing.T) {
	r := startTranscript(t, core.StatusDone, []string{trPrompt, trBgStart, trEndTurn}, nil)
	if v := r.awaitTasks(1, true); v.Class != core.ClassFinished || v.SortKey.ClassRank != 3 {
		t.Errorf("view %+v", v)
	}
	writeTranscript(t, r.path, trDone("bk7q2x9"))
	if v := r.awaitTasks(0, false); v.Class != core.ClassFinished || v.SortKey.ClassRank != 1 {
		t.Errorf("view %+v", v)
	}
	r.expectSessionRows(t)
	r.noLeak(t)
}

// TestTranscriptWorkingWithBg covers the matrix's "Working with bg" row: the
// turn goes on with a task open: Working, with the count shown.
func TestTranscriptWorkingWithBg(t *testing.T) {
	r := startTranscript(t, core.StatusWorking, []string{trPrompt, trBgStart}, nil)
	if v := r.awaitTasks(1, false); v.SortKey.ClassRank != 4 || v.Status != core.StatusWorking {
		t.Errorf("view %+v", v)
	}
	// A turn that ends makes it Background; a new prompt Working again.
	writeTranscript(t, r.path, trEndTurn)
	r.awaitTasks(1, true)
	writeTranscript(t, r.path, trPrompt)
	r.awaitTasks(1, false)
}

// TestTranscriptMissing covers the matrix's "No session record / transcript"
// row: with no transcript, or a record that names none, the process check
// alone decides (a tool shell: Background, one task), and the failure is
// logged once at debug level, naming no path.
func TestTranscriptMissing(t *testing.T) {
	for name, noID := range map[string]bool{"no transcript": false, "record names none": true} {
		t.Run(name, func(t *testing.T) {
			fastBackground(t)
			kids := newFakeChildren()
			kids.set(p1PID, toolShell)
			r := startTranscript(t, core.StatusDone, nil, func(o *Options) { o.Children = kids.read })
			if noID {
				r.sess.mu.Lock()
				r.sess.recs[p1PID] = procenv.Session{PID: p1PID, Status: "idle"}
				r.sess.mu.Unlock()
			}
			r.awaitTasks(1, true)
			r.sess.moreReads(t, r.h)
			r.awaitTasks(1, true)
			if e := r.logs.entries(t, "DEBUG", "claude transcript not read"); len(e) != 1 {
				t.Errorf("debug entries %v, want one", e)
			}
			if e := r.logs.entries(t, "WARN", ""); len(e) != 0 {
				t.Errorf("warnings %v", e)
			}
			r.noLeak(t)
			if r.logs.entries(t, "DEBUG", "claude transcript not read")[0]["error"] == nil {
				t.Error("the failure is not named")
			}
			r.logs.mu.Lock()
			logged := r.logs.b.String()
			r.logs.mu.Unlock()
			if strings.Contains(logged, "proj") || strings.Contains(logged, r.path) {
				t.Errorf("the log names the transcript: %s", logged)
			}
		})
	}
}

// TestTranscriptRecordEnds: a session record that goes for good drops what
// the transcript said: Finished, not Background.
func TestTranscriptRecordEnds(t *testing.T) {
	r := startTranscript(t, core.StatusDone, []string{trPrompt, trSubStart, trEndTurn}, nil)
	r.awaitTasks(1, true)
	r.sess.fail(p1PID, fmt.Errorf("%w: %w", procenv.ErrSession, fs.ErrNotExist))
	r.awaitTasks(0, false)
	// A passing failure keeps it: back, then one timeout-like failure.
	r.sess.mu.Lock()
	delete(r.sess.errs, p1PID)
	r.sess.mu.Unlock()
	r.awaitTasks(1, true)
	r.sess.failNext(t, r.h, p1PID, 1, fmt.Errorf("%w: caught mid-write", procenv.ErrSession))
	if v := r.view(); v.BackgroundTasks != 1 || !v.Background {
		t.Errorf("a passing failure dropped the transcript: %+v", v)
	}
}

// TestTranscriptPrivacyLog covers the matrix's "Privacy" row at the engine:
// with every Debug entry captured, a transcript full of prompts, results and
// text, its tasks opening and closing, logs only counts and flags.
func TestTranscriptPrivacyLog(t *testing.T) {
	r := startTranscript(t, core.StatusWorking, []string{trPrompt, trBgStart, trSubStart, trEndTurn,
		`{"type":"assistant","message":{"content":"TEXT-SECRET","stop_reason":"SECRET"}}`, "garbage SECRET"}, nil)
	r.awaitTasks(2, true)
	writeTranscript(t, r.path, trDone("bk7q2x9"), trDone("a3f9c2d1"))
	r.awaitTasks(0, false)
	if e := r.logs.entries(t, "DEBUG", "claude background tasks"); len(e) == 0 {
		t.Error("no change logged")
	}
	r.noLeak(t)
}

// TestTranscriptRotated: a transcript replaced by a shorter one starts
// afresh: tasks only it shows.
func TestTranscriptRotated(t *testing.T) {
	r := startTranscript(t, core.StatusDone, []string{trPrompt, trBgStart, trSubStart, trEndTurn}, nil)
	r.awaitTasks(2, true)
	tmp := r.path + ".new"
	if err := os.WriteFile(tmp, []byte(trSubStart+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		t.Fatal(err)
	}
	r.awaitTasks(1, true)
	time.Sleep(10 * sessionInterval)
	if v := r.view(); v.BackgroundTasks != 1 {
		t.Errorf("tasks %d after the rotation, want 1", v.BackgroundTasks)
	}
}

// TestTranscriptReadResumes: each read after the first starts where the last
// one stopped, so only new bytes are read (ADR-0009's read bound), and none
// restarts from the bound while the file only grows.
func TestTranscriptReadResumes(t *testing.T) {
	type call struct {
		offset    int64
		restarted bool
	}
	var mu sync.Mutex
	var calls []call
	r := startTranscript(t, core.StatusDone, []string{trPrompt, trBgStart, trEndTurn}, func(o *Options) {
		o.Transcript = func(tl procenv.Tail, path string, line func([]byte)) (procenv.Tail, bool, error) {
			next, restarted, err := procenv.TailTranscript(tl, path, line)
			mu.Lock()
			calls = append(calls, call{tl.Offset(), restarted})
			mu.Unlock()
			return next, restarted, err
		}
	})
	r.awaitTasks(1, true)
	writeTranscript(t, r.path, trDone("bk7q2x9"))
	r.awaitTasks(0, false)
	mu.Lock()
	defer mu.Unlock()
	if len(calls) < 2 {
		t.Fatalf("%d reads", len(calls))
	}
	if calls[0].offset != 0 {
		t.Errorf("first read from %d, want 0", calls[0].offset)
	}
	for i, c := range calls[1:] {
		if c.offset == 0 || c.restarted {
			t.Errorf("read %d: from %d, restarted %v; want the previous read's end", i+2, c.offset, c.restarted)
		}
	}
}
