package core

import (
	"errors"
	"testing"
)

func ack(t *testing.T, since int64, seen bool, at int64) Acknowledgement {
	t.Helper()
	a, err := NewAcknowledgement(AcknowledgementInput{
		Key: AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"}, FinishedSince: since, Seen: seen, ObservedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewAcknowledgement(t *testing.T) {
	good := AcknowledgementInput{Key: AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"},
		FinishedSince: 100, Seen: true, ObservedAt: 200}
	a, err := NewAcknowledgement(good)
	if err != nil {
		t.Fatal(err)
	}
	if a.Key() != good.Key || a.FinishedSince() != 100 || !a.Seen() || a.ObservedAt() != 200 {
		t.Errorf("accessors %+v %d %v %d, want the input", a.Key(), a.FinishedSince(), a.Seen(), a.ObservedAt())
	}
	undo := good
	undo.Seen = false
	if a, err := NewAcknowledgement(undo); err != nil || a.Seen() {
		t.Errorf("undo: %v, %v; want a row with seen clear", a.Seen(), err)
	}
	for name, mutate := range map[string]func(*AcknowledgementInput){
		"empty host":              func(in *AcknowledgementInput) { in.Key.Host = "" },
		"empty server":            func(in *AcknowledgementInput) { in.Key.Server = "" },
		"empty pane id":           func(in *AcknowledgementInput) { in.Key.PaneID = "" },
		"backfill epoch":          func(in *AcknowledgementInput) { in.Key.Epoch = 0 },
		"zero finished since":     func(in *AcknowledgementInput) { in.FinishedSince = 0 },
		"negative finished since": func(in *AcknowledgementInput) { in.FinishedSince = -1 },
		"zero observed at":        func(in *AcknowledgementInput) { in.ObservedAt = 0 },
		"negative observed at":    func(in *AcknowledgementInput) { in.ObservedAt = -5 },
	} {
		in := good
		mutate(&in)
		a, err := NewAcknowledgement(in)
		if !errors.Is(err, ErrInvalidAcknowledgement) {
			t.Errorf("%s: error %v, want ErrInvalidAcknowledgement", name, err)
		}
		if a != (Acknowledgement{}) {
			t.Errorf("%s: returned %+v, want the zero value", name, a)
		}
	}
}

func TestSeen(t *testing.T) {
	finished := FoldResult{Known: true, Status: StatusDone, Class: ClassFinished, Since: 100, SinceProvenance: ProvenanceLive, EverWorking: true}
	with := func(mutate func(*FoldResult)) FoldResult {
		f := finished
		mutate(&f)
		return f
	}
	for _, c := range []struct {
		name string
		f    FoldResult
		acks []Acknowledgement
		want bool
	}{
		{"no rows", finished, nil, false},
		{"seen", finished, []Acknowledgement{ack(t, 100, true, 110)}, true},
		{"seen then undone", finished, []Acknowledgement{ack(t, 100, true, 110), ack(t, 100, false, 120)}, false},
		{"undone then seen again", finished, []Acknowledgement{ack(t, 100, true, 110), ack(t, 100, false, 120), ack(t, 100, true, 130)}, true},
		{"only an older finish", finished, []Acknowledgement{ack(t, 50, true, 60)}, false},
		{"older finish undone after the current seen", finished,
			[]Acknowledgement{ack(t, 100, true, 110), ack(t, 50, false, 120)}, true},
		{"a later finish seen does not count", finished, []Acknowledgement{ack(t, 150, true, 160)}, false},
		{"needs you", with(func(f *FoldResult) { f.Status, f.Class = StatusBlocked, ClassNeedsYou }),
			[]Acknowledgement{ack(t, 100, true, 110)}, false},
		{"idle never working", with(func(f *FoldResult) { f.Status, f.Class, f.EverWorking = StatusIdle, ClassWorking, false }),
			[]Acknowledgement{ack(t, 100, true, 110)}, false},
		{"working", with(func(f *FoldResult) { f.Status, f.Class = StatusWorking, ClassWorking }),
			[]Acknowledgement{ack(t, 100, true, 110)}, false},
		{"unknown", with(func(f *FoldResult) { f.Status, f.Class = StatusUnknown, ClassUnknown }),
			[]Acknowledgement{ack(t, 100, true, 110)}, false},
		{"fold not known", FoldResult{Class: ClassFinished, Since: 100}, []Acknowledgement{ack(t, 100, true, 110)}, false},
		{"since 0", with(func(f *FoldResult) { f.Since = 0 }), []Acknowledgement{ack(t, 100, true, 110)}, false},
	} {
		if got := Seen(c.f, c.acks); got != c.want {
			t.Errorf("%s: Seen = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSeenAcrossTransitions folds real Transitions: done to idle within one
// finish keeps the acknowledgement, and anything that starts the class over
// ends it without a further row.
func TestSeenAcrossTransitions(t *testing.T) {
	ts := []Transition{
		first(t, StatusWorking, 10, ProvenanceLive),
		tr(t, StatusDone, 100, ProvenanceLive),
	}
	acks := []Acknowledgement{ack(t, 100, true, 110)}
	if !Seen(Fold(ts), acks) {
		t.Fatal("the acknowledged finish is not seen")
	}
	idle := append(ts, tr(t, StatusIdle, 120, ProvenanceLive))
	if f := Fold(idle); f.Since != 100 || !Seen(f, acks) {
		t.Errorf("done to idle: since %d, seen %v; want 100, seen", f.Since, Seen(f, acks))
	}
	for name, next := range map[string]Transition{
		"a new turn":        tr(t, StatusWorking, 130, ProvenanceLive),
		"needs you":         tr(t, StatusBlocked, 130, ProvenanceLive),
		"a new Agent's row": first(t, StatusDone, 130, ProvenanceLive),
	} {
		if Seen(Fold(append(idle[:len(idle):len(idle)], next)), acks) {
			t.Errorf("%s: still seen", name)
		}
	}
	// A new turn that finishes again is a new finish: not seen.
	again := append(idle[:len(idle):len(idle)], tr(t, StatusWorking, 130, ProvenanceLive), tr(t, StatusDone, 140, ProvenanceLive))
	if Seen(Fold(again), acks) {
		t.Error("the next finish is seen")
	}
}

func TestAgentViewSeen(t *testing.T) {
	k := AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"}
	in := func(status Status, class AttentionClass, seen bool) AgentViewInput {
		return AgentViewInput{Key: k, AgentProgram: "claude", Profile: "cldp", Status: status,
			Fold:    FoldResult{Known: true, Status: status, Class: class, Since: 100, SinceProvenance: ProvenanceLive, EverWorking: true},
			Context: PaneContext{WorkspacePosition: 2}, Seen: seen}
	}

	plain := NewAgentView(in(StatusDone, ClassFinished, false))
	if plain.Seen || plain.SortKey.ClassRank != 1 {
		t.Errorf("unseen finished: seen %v rank %d, want false 1", plain.Seen, plain.SortKey.ClassRank)
	}
	for _, status := range []Status{StatusDone, StatusIdle} {
		v := NewAgentView(in(status, ClassFinished, true))
		if !v.Seen || v.Class != ClassFinished || v.WaitingSince != 100 {
			t.Errorf("%s seen: seen %v class %s since %d, want true finished 100", status, v.Seen, v.Class, v.WaitingSince)
		}
		idle := NewAgentView(AgentViewInput{Key: k, Status: StatusIdle,
			Fold:    FoldResult{Known: true, Status: StatusIdle, Class: ClassWorking, Since: 100},
			Context: PaneContext{WorkspacePosition: 2}})
		if v.SortKey != idle.SortKey {
			t.Errorf("%s seen: sort key %+v, want Idle's %+v", status, v.SortKey, idle.SortKey)
		}
		if v.SortKey.ClassRank != 2 || v.SortKey.Start != -100 || v.SortKey.Zone != 1 || v.SortKey.Group != 2 {
			t.Errorf("%s seen: sort key %+v, want zone 1, group 2, rank 2, start -100", status, v.SortKey)
		}
	}

	// Seen applies only to a finished class.
	for _, c := range []struct {
		status Status
		class  AttentionClass
	}{
		{StatusBlocked, ClassNeedsYou}, {StatusIdle, ClassWorking}, {StatusWorking, ClassWorking}, {StatusUnknown, ClassUnknown},
	} {
		v := NewAgentView(in(c.status, c.class, true))
		ref := NewAgentView(in(c.status, c.class, false))
		if v.Seen || v.SortKey != ref.SortKey {
			t.Errorf("%s/%s: seen %v sort key %+v, want unseen %+v", c.status, c.class, v.Seen, v.SortKey, ref.SortKey)
		}
	}

	// Background still takes precedence: rank 3.
	bg := in(StatusDone, ClassFinished, true)
	bg.Transcript.Apply(TranscriptLine{StartID: "task-1"})
	v := NewAgentView(bg)
	if !v.Background || v.SortKey.ClassRank != 3 || v.Class != ClassFinished {
		t.Errorf("seen Background: background %v rank %d class %s, want true 3 finished", v.Background, v.SortKey.ClassRank, v.Class)
	}
}
