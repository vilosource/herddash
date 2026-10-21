package core

import (
	"errors"
	"testing"
)

// TestEffectiveStatus covers the matrix's "Missed question", "Question
// answered", "herdr also blocked" and "Non-claude" rows at the rule: only a
// claude Agent's waiting record makes it blocked, and no other record value
// is mapped to anything.
func TestEffectiveStatus(t *testing.T) {
	for _, c := range []struct {
		program AgentProgram
		herdr   Status
		session string
		want    Status
	}{
		{"claude", StatusDone, "waiting", StatusBlocked},    // missed question
		{"claude", StatusIdle, "waiting", StatusBlocked},    // herdr moved meanwhile
		{"claude", StatusBlocked, "waiting", StatusBlocked}, // herdr also blocked
		{"claude", StatusWorking, "busy", StatusWorking},    // question answered
		{"claude", StatusDone, "idle", StatusDone},
		{"claude", StatusWorking, "idle", StatusWorking},
		{"claude", StatusDone, "busy", StatusDone},
		{"claude", StatusIdle, "shell", StatusIdle},
		{"claude", StatusDone, "", StatusDone}, // unreadable
		{"claude", StatusDone, "Waiting", StatusDone},
		{"codex", StatusDone, "waiting", StatusDone}, // non-claude
		{"", StatusDone, "waiting", StatusDone},
	} {
		if got := EffectiveStatus(c.program, c.herdr, c.session); got != c.want {
			t.Errorf("EffectiveStatus(%s, %s, %q) = %s, want %s", c.program, c.herdr, c.session, got, c.want)
		}
	}
	if !ReadsSession("claude") || ReadsSession("codex") || ReadsSession("") {
		t.Error("ReadsSession is not claude alone")
	}
}

// TestSessionObservedAt covers the matrix's "statusUpdatedAt in future /
// before last" row at the rule.
func TestSessionObservedAt(t *testing.T) {
	for _, c := range []struct {
		name                    string
		updated, previous, read int64
		want                    int64
	}{
		{"true start", 500, 400, 600, 500},
		{"same as previous", 400, 400, 600, 400},
		{"same as read", 600, 400, 600, 600},
		{"in the future", 700, 400, 600, 600},
		{"before the previous Transition: clamped to it", 300, 400, 600, 400},
		{"unknown", 0, 400, 600, 600},
		{"negative", -5, 0, 600, 600},
		{"no previous", 500, 0, 600, 500},
	} {
		if got := SessionObservedAt(c.updated, c.previous, c.read); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestClaudeConfigDir(t *testing.T) {
	for _, c := range []struct {
		value string
		set   bool
		home  string
		want  string
	}{
		{"", false, "/h", "/h/.claude"},
		{"", true, "/h", "/h/.claude"},
		{"/h/.claude-cldw", true, "/h", "/h/.claude-cldw"},
		{"/h/.claude-cldw/", true, "/h", "/h/.claude-cldw"},
		{"~/.claude-cldw", true, "/h", "/h/.claude-cldw"},
		{"~", true, "/h", "/h"},
		{"~/.claude-cldw", true, "", ""},
		{"", false, "", ""},
		{"rel/.claude", true, "/h", ""},
		{"~other/.claude", true, "/h", ""},
	} {
		if got := ClaudeConfigDir(c.value, c.set, c.home); got != c.want {
			t.Errorf("ClaudeConfigDir(%q, %v, %q) = %q, want %q", c.value, c.set, c.home, got, c.want)
		}
	}
}

func TestTransitionObservedBy(t *testing.T) {
	in := TransitionInput{
		Key:        AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"},
		New:        StatusBlocked,
		ObservedAt: 5,
		Provenance: ProvenanceLive,
		ObservedBy: ObservedByClaudeSession,
	}
	tr, err := NewTransition(in)
	if err != nil || tr.ObservedBy() != ObservedByClaudeSession {
		t.Fatalf("%v, %v", tr.ObservedBy(), err)
	}
	in.ObservedBy = ""
	if tr, err := NewTransition(in); err != nil || tr.ObservedBy() != ObservedByHerdr {
		t.Fatalf("herdr: %v, %v", tr.ObservedBy(), err)
	}
	for name, mod := range map[string]func(*TransitionInput){
		"unknown observer": func(in *TransitionInput) { in.ObservedBy = "guess" },
		"snapshot": func(in *TransitionInput) {
			in.ObservedBy, in.Provenance = ObservedByClaudeSession, ProvenanceSnapshot
		},
		"backfill": func(in *TransitionInput) {
			in.ObservedBy, in.Provenance, in.Key.Epoch, in.ImportKey = ObservedByClaudeSession, ProvenanceBackfill, 0, "k"
		},
	} {
		bad := in
		mod(&bad)
		_, err := NewTransition(bad)
		var te *TransitionError
		if !errors.Is(err, ErrInvalidObserver) || !errors.As(err, &te) {
			t.Errorf("%s: error %v, want ErrInvalidObserver", name, err)
		}
	}
}
