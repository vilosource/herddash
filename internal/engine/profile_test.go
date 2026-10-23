package engine

import (
	"log/slog"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
)

// TestDetectedProgramKept: agent_detected names the Agent Program of a new
// pane; a status event that beats the snapshot and names no program still
// reports the Agent under it (Epic 1 retro #4).
func TestDetectedProgramKept(t *testing.T) {
	h := started(t)
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p4", AgentProgram: "claude"})
	h.fake.awaitSubscribe("w1:p4")
	h.fake.awaitSnapshotCall()
	h.fake.statusSub("w1:p4").event(core.StatusEvent{PaneID: "w1:p4", Status: core.StatusBlocked})
	h.rec.expect(t, "agent w1:p1 claude working", "agent w1:p2 claude idle", "agent w1:p4 claude blocked")
	h.fake.setGated(false)
	h.fake.release <- struct{}{}
	h.settle()
}

// Profile retries and held rows (Story 5.4).

// awaitLog waits until at least n log entries at level contain msg.
func awaitLog(t *testing.T, logs *logSink, level, msg string, n int) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for len(logs.entries(t, level, msg)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("fewer than %d %s entries %q:\n%s", n, level, msg, logs.text())
		}
		time.Sleep(time.Millisecond)
	}
}

// setProfileRetryDelay sets profileRetryDelay for one test, restoring it
// after.
func setProfileRetryDelay(t *testing.T, d time.Duration) {
	t.Helper()
	prev := profileRetryDelay
	profileRetryDelay = d
	t.Cleanup(func() { profileRetryDelay = prev })
}

// startProfiledStore is startProfiled recording into a fake store, which it
// returns too.
func startProfiledStore(t *testing.T, snap core.Snapshot, prepare func(*fakeClient, *fakeEnviron)) (*harness, *fakeEnviron, *fakeStore) {
	t.Helper()
	fake := newFakeClient(t, snap)
	fake.setProc("w1:p1", p1PID)
	env := newFakeEnviron(t)
	if prepare != nil {
		prepare(fake, env)
	}
	st := newFakeStore(fake)
	h := startRecording(t, fake, st, func(o *Options, r *recorder) {
		o.Profiles = map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}
		o.Environ = env.lookup
		o.Logger = slog.New(slog.DiscardHandler)
		r.profileLines = true
	})
	return h, env, st
}

// awaitProcessCalls waits until at least n foreground process reads.
func (h *harness) awaitProcessCalls(n int) {
	h.t.Helper()
	deadline := time.Now().Add(wait)
	for h.fake.processCalls() < n {
		if time.Now().After(deadline) {
			h.t.Fatalf("fewer than %d foreground process reads", n)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestProfileRetrySucceeds: a first lookup that times out reports the Agent
// unresolved; the retry resolves it, reported as a Profile change, and the
// retry is logged at Info with its attempt and wait.
func TestProfileRetrySucceeds(t *testing.T) {
	h, env, logs := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
		env.hold()
	})
	h.rec.expect(t, "agent w1:p1 claude working profile=unresolved")
	env.unblock()
	h.rec.expect(t, "agent w1:p1 claude working profile=unresolved", "profile w1:p1 cldw previous unresolved")
	infos := logs.entries(t, "INFO", "profile lookup failed; retrying")
	if len(infos) == 0 || infos[0]["attempt"] != float64(1) || infos[0]["retry_in"] != profileRetryWait(1).String() {
		t.Errorf("retry entries %v", infos)
	}
	if w := logs.entries(t, "WARN", "profile lookup failed"); len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"})
	h.rec.expect(t, "agent w1:p1 claude working profile=unresolved", "profile w1:p1 cldw previous unresolved",
		"transition w1:p1 claude working->idle live profile=cldw")
}

// TestProfileRuleOutcomeNotRetried: an unmatched value and an absent variable
// are rule outcomes: one lookup, never retried.
func TestProfileRuleOutcomeNotRetried(t *testing.T) {
	cases := []struct {
		name    string
		value   *string
		profile string
	}{
		{"unmatched", new("/h/.other"), core.ProfileUnresolved},
		{"absent", nil, "claude"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, _, logs := startProfiled(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
				if c.value != nil {
					env.set(p1PID, claudeVar, *c.value)
				}
			})
			h.rec.expect(t, "agent w1:p1 claude working profile="+c.profile)
			time.Sleep(4 * profileRetryWait(profileRetries))
			h.settle()
			if n := h.fake.processCalls(); n != 1 {
				t.Errorf("%d lookups, want 1", n)
			}
			if es := logs.entries(t, "INFO", "profile lookup failed"); len(es) != 0 {
				t.Errorf("retries %v", es)
			}
			h.rec.expect(t, "agent w1:p1 claude working profile="+c.profile)
		})
	}
}

// TestProfileRedetectTransient: resolving a known Agent's Profile again for
// its same program, a transient failure keeps the Profile it holds; a retry
// may still change it.
func TestProfileRedetectTransient(t *testing.T) {
	t.Run("kept", func(t *testing.T) {
		h, _, logs := startedProfiled(t)
		h.fake.clearProc("w1:p1")
		h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
		awaitLog(t, logs, "WARN", "profile lookup failed", 1)
		h.settle()
		if n := h.fake.processCalls(); n != 2+profileRetries {
			t.Errorf("%d lookups, want %d", n, 2+profileRetries)
		}
		if as := h.agents(); len(as) != 1 || as[0].Profile != "cldw" {
			t.Errorf("agents %+v", as)
		}
		h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"})
		h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "transition w1:p1 claude working->idle live profile=cldw")
	})
	t.Run("retry changes it", func(t *testing.T) {
		setProfileRetryDelay(t, 50*time.Millisecond)
		h, env, logs := startedProfiled(t)
		h.fake.clearProc("w1:p1")
		env.set(p1PID, claudeVar, "/h/.claude-cldp")
		h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
		awaitLog(t, logs, "INFO", "profile lookup failed; retrying", 1)
		h.fake.setProc("w1:p1", p1PID)
		h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "profile w1:p1 cldp previous cldw")
	})
	t.Run("only the first settle ends the hold", func(t *testing.T) {
		setProfileTimeout(t, 500*time.Millisecond)
		h, env, _ := startedProfiled(t)
		env.hold()
		h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
		h.awaitProcessCalls(2)
		sub := h.fake.statusSub("w1:p1")
		sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"})
		sub.awaitDelivered(t, 1)
		h.agents()
		if got := h.rec.all(); len(got) != 1 {
			t.Fatalf("a row was reported while held: %v", got)
		}
		// The lookup times out, keeping cldw: the held row is released.
		h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "transition w1:p1 claude working->idle live profile=cldw")
		// The retry, held too, does not hold rows.
		h.awaitProcessCalls(3)
		begin := time.Now()
		sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
		h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "transition w1:p1 claude working->idle live profile=cldw",
			"transition w1:p1 claude idle->blocked live profile=cldw")
		if d := time.Since(begin); d >= profileTimeout {
			t.Errorf("a row during a retry waited %v", d)
		}
	})
}

// TestProfileProgramChangeKnown: a status event naming another program, one
// with no rule, for a known Agent reports the Profile change first, then the
// row with the new Profile.
func TestProfileProgramChangeKnown(t *testing.T) {
	h, _, _ := startedProfiled(t)
	h.fake.setSnapshot(oneAgent("codex", core.StatusBlocked))
	h.fake.statusSub("w1:p1").event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "codex"})
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "profile w1:p1 codex previous cldw",
		"transition w1:p1 codex working->blocked live profile=codex")
}

// heldProgramChange starts an engine whose w1:p1 runs codex (no rule), then
// sends two status events naming claude while claude's lookup is held.
func heldProgramChange(t *testing.T, h *harness) {
	t.Helper()
	h.rec.expect(t, "agent w1:p1 codex working profile=codex")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	h.fake.setSnapshot(oneAgent("claude", core.StatusIdle))
	sub := h.fake.statusSub("w1:p1")
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"})
	sub.awaitDelivered(t, 2)
	h.agents()
	if got := h.rec.all(); len(got) != 1 {
		t.Fatalf("a row was reported while held: %v", got)
	}
}

func holdCldw(_ *fakeClient, env *fakeEnviron) {
	env.set(p1PID, claudeVar, "/h/.claude-cldw")
	env.hold()
}

// TestProfileProgramChangeHeldRows: rows of a known Agent whose new program's
// Profile is being looked up wait for it, then carry it at their own times.
// A removal or the run's end releases them first, unresolved, since the held
// Profile was resolved for another program.
func TestProfileProgramChangeHeldRows(t *testing.T) {
	t.Run("settled", func(t *testing.T) {
		setProfileTimeout(t, wait)
		h, env, _ := startProfiled(t, oneAgent("codex", core.StatusWorking), holdCldw)
		heldProgramChange(t, h)
		env.unblock()
		h.rec.expect(t, "agent w1:p1 codex working profile=codex", "profile w1:p1 cldw previous codex",
			"transition w1:p1 claude working->blocked live profile=cldw",
			"transition w1:p1 claude blocked->idle live profile=cldw")
		ts := h.rec.observedTimes()
		if ts[2] >= ts[1] || ts[3] >= ts[1] || ts[2] >= ts[3] {
			t.Errorf("observed times %v: the rows keep the times they were observed at", ts)
		}
		h.settle()
		if as := h.agents(); len(as) != 1 || as[0].Status != core.StatusIdle || as[0].Profile != "cldw" {
			t.Errorf("agents %+v", as)
		}
	})
	t.Run("pane removed", func(t *testing.T) {
		setProfileTimeout(t, wait)
		h, env, _ := startProfiled(t, oneAgent("codex", core.StatusWorking), holdCldw)
		heldProgramChange(t, h)
		h.fake.setSnapshot(core.Snapshot{HerdrVersion: "0.9.5", Panes: []core.PaneObservation{shellPane("w1:p3")}})
		h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: "w1:p1"})
		h.rec.expect(t, "agent w1:p1 codex working profile=codex",
			"transition w1:p1 claude working->blocked live profile=unresolved",
			"transition w1:p1 claude blocked->idle live profile=unresolved",
			"removed w1:p1 profile=codex")
		env.unblock()
		h.settle()
		if got := h.rec.all(); len(got) != 4 {
			t.Errorf("lines after the removal: %v", got)
		}
	})
	t.Run("run ends", func(t *testing.T) {
		setProfileTimeout(t, wait)
		h, _, st := startProfiledStore(t, oneAgent("codex", core.StatusWorking), holdCldw)
		heldProgramChange(t, h)
		h.stop()
		rows := st.rowList()
		if len(rows) < 2 {
			t.Fatalf("rows %v", rows)
		}
		for i, want := range []core.Status{core.StatusBlocked, core.StatusIdle} {
			r := rows[len(rows)-2+i]
			if r.New() != want || r.AgentProgram() != "claude" || r.Profile() != core.ProfileUnresolved {
				t.Errorf("row %s", row(r))
			}
		}
	})
}

// TestProfileHeldRowRecordedAtShutdown: a row held while a known Agent's
// Profile is resolved again for its same program is appended when Run ends,
// with the Profile it holds.
func TestProfileHeldRowRecordedAtShutdown(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, st := startProfiledStore(t, oneAgent("claude", core.StatusWorking), func(_ *fakeClient, env *fakeEnviron) {
		env.set(p1PID, claudeVar, "/h/.claude-cldw")
	})
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw")
	h.fake.awaitSubscribe("w1:p1")
	h.settle()
	env.hold()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.awaitProcessCalls(2)
	sub := h.fake.statusSub("w1:p1")
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "claude"})
	sub.awaitDelivered(t, 1)
	h.agents()
	before := len(st.rowList())
	h.stop()
	rows := st.rowList()
	if len(rows) != before+1 {
		t.Fatalf("rows %d before the end, %d after", before, len(rows))
	}
	r := rows[len(rows)-1]
	if p, _ := r.Previous(); r.New() != core.StatusBlocked || p != core.StatusWorking || r.Profile() != "cldw" ||
		r.Provenance() != core.ProvenanceLive {
		t.Errorf("row %s", row(r))
	}
}

// TestProfileReconnect: a reconnect that keeps the epoch resolves every
// Agent's Profile again; a transient failure keeps the one it holds.
func TestProfileReconnect(t *testing.T) {
	t.Run("changed", func(t *testing.T) {
		h, env, _ := startedProfiled(t)
		env.set(p1PID, claudeVar, "/h/.claude-cldp")
		h.loseLifecycle()
		h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "reconnected 0.9.5 epoch 1 kept",
			"profile w1:p1 cldp previous cldw")
	})
	t.Run("transient failure", func(t *testing.T) {
		h, _, logs := startedProfiled(t)
		h.fake.clearProc("w1:p1")
		h.loseLifecycle()
		h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "reconnected 0.9.5 epoch 1 kept")
		awaitLog(t, logs, "WARN", "profile lookup failed", 1)
		h.settle()
		h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "reconnected 0.9.5 epoch 1 kept")
		if as := h.agents(); len(as) != 1 || as[0].Profile != "cldw" {
			t.Errorf("agents %+v", as)
		}
	})
}

// TestProfileSessionRowAfterProgramChange: a program change that ends a
// session verdict records the return before the new program's Profile is
// resolved; that row waits for it too, so it never carries the old
// program's Profile.
func TestProfileSessionRowAfterProgramChange(t *testing.T) {
	for _, c := range []struct {
		name    string
		rules   map[core.AgentProgram]core.ProfileRule
		profile string
	}{
		{"no rule", map[core.AgentProgram]core.ProfileRule{"claude": claudeRule()}, "codex"},
		{"rule", map[core.AgentProgram]core.ProfileRule{"claude": claudeRule(), "codex": {Variable: claudeVar, Absent: "codex",
			Values: map[string]string{"/h/.claude-cldw": "codex-w"}}}, "codex-w"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sess := newFakeSessions()
			sess.set(p1PID, "waiting", base)
			r := startSession(t, "claude", core.StatusDone, sess.read, func(o *Options, _ *fakeEnviron) {
				o.Profiles = c.rules
			})
			r.st.awaitRows(t, 2)
			r.at(1_000)
			r.event("codex", core.StatusWorking)
			r.st.awaitRows(t, 3)
			r.h.settle()
			rows := r.st.rowList()
			if len(rows) != 3 {
				t.Fatalf("%d rows", len(rows))
			}
			last := rows[2]
			if last.New() != core.StatusWorking || last.AgentProgram() != "codex" || last.Profile() != c.profile {
				t.Errorf("row %s", row(last))
			}
		})
	}
}

// TestProfileRetryWait: the wait doubles from profileRetryDelay.
func TestProfileRetryWait(t *testing.T) {
	setProfileRetryDelay(t, 2*time.Second)
	for i, want := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second} {
		if got := profileRetryWait(i + 1); got != want {
			t.Errorf("profileRetryWait(%d) = %v, want %v", i+1, got, want)
		}
	}
}

// viewOfP1 is w1:p1 as the latest published View shows it.
func viewOfP1(t *testing.T, h *harness) core.AgentView {
	t.Helper()
	for _, a := range h.engine.View().Agents {
		if a.Key.PaneID == "w1:p1" {
			return a
		}
	}
	t.Fatalf("w1:p1 not in the view")
	return core.AgentView{}
}

// TestProfileHeldView: while rows are held, the published view keeps what
// was last released; the release publishes the new program, Status and
// Profile.
func TestProfileHeldView(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, _ := startProfiled(t, oneAgent("codex", core.StatusWorking), holdCldw)
	heldProgramChange(t, h)
	if v := viewOfP1(t, h); v.AgentProgram != "codex" || v.Status != core.StatusWorking || v.Profile != "codex" {
		t.Errorf("view while held %+v", v)
	}
	env.unblock()
	h.rec.awaitCount(t, "transition w1:p1 claude blocked->idle live profile=cldw", 1)
	h.settle()
	if v := viewOfP1(t, h); v.AgentProgram != "claude" || v.Status != core.StatusIdle || v.Profile != "cldw" {
		t.Errorf("view after the release %+v", v)
	}
}

// TestProfileHeldOldProgramRow: a row held for a redetection of the same
// program keeps that program's Profile when the program then changes; the
// new program's row carries the new Profile.
func TestProfileHeldOldProgramRow(t *testing.T) {
	setProfileTimeout(t, wait)
	h, env, _ := startedProfiled(t)
	env.hold()
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"})
	h.awaitProcessCalls(2)
	sub := h.fake.statusSub("w1:p1")
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"})
	sub.awaitDelivered(t, 1)
	h.agents()
	h.fake.setSnapshot(oneAgent("codex", core.StatusBlocked))
	sub.event(core.StatusEvent{PaneID: "w1:p1", Status: core.StatusBlocked, AgentProgram: "codex"})
	h.rec.expect(t, "agent w1:p1 claude working profile=cldw", "profile w1:p1 codex previous cldw",
		"transition w1:p1 claude working->idle live profile=cldw",
		"transition w1:p1 codex idle->blocked live profile=codex")
}

// TestProfileRetryReset: a trigger after the retries ran out gets every
// retry again, and a trigger during a retry's wait makes that retry stale.
func TestProfileRetryReset(t *testing.T) {
	redetect := core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"}
	t.Run("after giving up", func(t *testing.T) {
		h, _, logs := startedProfiled(t)
		h.fake.clearProc("w1:p1")
		h.lifecycle(redetect)
		awaitLog(t, logs, "WARN", "profile lookup failed", 1)
		h.settle()
		h.lifecycle(redetect)
		awaitLog(t, logs, "WARN", "profile lookup failed", 2)
		time.Sleep(4 * profileRetryWait(profileRetries))
		h.settle()
		if n, want := h.fake.processCalls(), 1+2*(1+profileRetries); n != want {
			t.Errorf("%d lookups, want %d", n, want)
		}
	})
	t.Run("stale retry", func(t *testing.T) {
		setProfileRetryDelay(t, 200*time.Millisecond)
		h, _, logs := startedProfiled(t)
		h.fake.clearProc("w1:p1")
		h.lifecycle(redetect)
		awaitLog(t, logs, "INFO", "profile lookup failed; retrying", 1)
		h.fake.setProc("w1:p1", p1PID)
		h.lifecycle(redetect)
		h.awaitResolved(logs, 3)
		time.Sleep(4 * profileRetryDelay)
		h.settle()
		if n := h.fake.processCalls(); n != 3 {
			t.Errorf("%d lookups, want 3: a stale retry ran", n)
		}
		if n := len(logs.entries(t, "INFO", "profile lookup failed; retrying")); n != 1 {
			t.Errorf("%d retries logged, want 1", n)
		}
	})
}
