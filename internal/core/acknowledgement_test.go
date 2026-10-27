package core

import (
	"errors"
	"testing"
	"time"
)

func ack(t *testing.T, since int64, seen bool, at int64) Acknowledgement {
	t.Helper()
	a, err := NewAcknowledgement(AcknowledgementInput{Source: AckSourceBoard,
		Key: AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"}, FinishedSince: since, Seen: seen, ObservedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewAcknowledgement(t *testing.T) {
	good := AcknowledgementInput{Source: AckSourceBoard, Key: AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"},
		FinishedSince: 100, Seen: true, ObservedAt: 200}
	a, err := NewAcknowledgement(good)
	if err != nil {
		t.Fatal(err)
	}
	if a.Key() != good.Key || a.FinishedSince() != 100 || !a.Seen() || a.ObservedAt() != 200 || a.Source() != AckSourceBoard {
		t.Errorf("accessors %+v %d %v %d %q, want the input", a.Key(), a.FinishedSince(), a.Seen(), a.ObservedAt(), a.Source())
	}
	auto := good
	auto.Source = AckSourceHerdrFocus
	if a, err := NewAcknowledgement(auto); err != nil || a.Source() != AckSourceHerdrFocus || !a.Seen() {
		t.Errorf("herdr focus mark: source %q seen %v, %v; want a herdr_focus mark", a.Source(), a.Seen(), err)
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
		"empty source":            func(in *AcknowledgementInput) { in.Source = "" },
		"unknown source":          func(in *AcknowledgementInput) { in.Source = "other" },
		"undo from herdr focus":   func(in *AcknowledgementInput) { in.Seen, in.Source = false, AckSourceHerdrFocus },
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

func TestAcknowledgementHerdrOrder(t *testing.T) {
	in := AcknowledgementInput{Source: AckSourceBoard, Key: AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"},
		FinishedSince: 100, Seen: true, ObservedAt: 200}
	a, err := NewAcknowledgement(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.HerdrOrder(); ok {
		t.Error("an acknowledgement made with no order reports one")
	}
	o := uint64(9)
	in.HerdrOrder = &o
	if a, err = NewAcknowledgement(in); err != nil {
		t.Fatal(err)
	}
	o = 10 // the input's later change does not reach the row
	if got, ok := a.HerdrOrder(); !ok || got != 9 {
		t.Errorf("HerdrOrder %d %v, want 9", got, ok)
	}
}

func TestSeenAt(t *testing.T) {
	key := AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"}
	row := func(prev, next Status, at int64, order *uint64) Transition {
		t.Helper()
		in := TransitionInput{Key: key, AgentProgram: "claude", New: next, ObservedAt: at, Provenance: ProvenanceLive}
		if prev != "" {
			in.Previous = &prev
		}
		if order != nil {
			in.Provenance, in.HerdrOrder = ProvenanceSnapshot, order
		}
		tr, err := NewTransition(in)
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}
	ackAt := func(since int64, seen bool, at int64, order *uint64) Acknowledgement {
		t.Helper()
		a, err := NewAcknowledgement(AcknowledgementInput{Source: AckSourceBoard, Key: key, FinishedSince: since, Seen: seen, ObservedAt: at, HerdrOrder: order})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	n := func(v uint64) *uint64 { return &v }
	working := row("", StatusWorking, 100, nil)
	doneLive := row(StatusWorking, StatusDone, 200, nil)
	doneSnap := row(StatusWorking, StatusDone, 200, n(5))
	fold := func(ts ...Transition) FoldResult { return Fold(ts) }
	for _, c := range []struct {
		name    string
		rows    []Transition
		acks    []Acknowledgement
		current *uint64
		want    bool
	}{
		{"no current order: Seen alone", []Transition{working, doneLive}, []Acknowledgement{ackAt(200, true, 300, n(5))}, nil, true},
		{"orders unknown: Seen alone", []Transition{working, doneLive}, []Acknowledgement{ackAt(200, true, 300, nil)}, n(9), true},
		{"the acknowledged order now", []Transition{working, doneLive}, []Acknowledgement{ackAt(200, true, 300, n(5))}, n(5), true},
		{"a later turn missed after the mark", []Transition{working, doneLive}, []Acknowledgement{ackAt(200, true, 300, n(5))}, n(7), false},
		{"the row's order, the mark's unknown", []Transition{working, doneSnap}, []Acknowledgement{ackAt(200, true, 300, nil)}, n(5), true},
		{"a later turn missed, the row's order", []Transition{working, doneSnap}, []Acknowledgement{ackAt(200, true, 300, nil)}, n(7), false},
		// done to idle after the mark: the mark's order is the done's, so the
		// later row's decides, and a live one leaves Seen alone.
		{"done to idle after the mark, live", []Transition{working, doneLive, row(StatusDone, StatusIdle, 400, nil)},
			[]Acknowledgement{ackAt(200, true, 300, n(5))}, n(6), true},
		{"done to idle after the mark, snapshot", []Transition{working, doneLive, row(StatusDone, StatusIdle, 400, n(6))},
			[]Acknowledgement{ackAt(200, true, 300, n(5))}, n(6), true},
		{"done to idle after the mark, then a turn missed", []Transition{working, doneLive, row(StatusDone, StatusIdle, 400, n(6))},
			[]Acknowledgement{ackAt(200, true, 300, n(5))}, n(8), false},
		{"undone", []Transition{working, doneLive}, []Acknowledgement{ackAt(200, true, 300, n(5)), ackAt(200, false, 310, n(5))}, n(5), false},
		{"marked again after a missed turn", []Transition{working, doneLive},
			[]Acknowledgement{ackAt(200, true, 300, n(5)), ackAt(200, true, 500, n(7))}, n(7), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := SeenAt(fold(c.rows...), c.acks, c.rows[len(c.rows)-1], c.current); got != c.want {
				t.Errorf("SeenAt %v, want %v", got, c.want)
			}
		})
	}
}

func TestAckSourceValid(t *testing.T) {
	for s, want := range map[AckSource]bool{
		AckSourceBoard: true, AckSourceHerdrFocus: true, "": false, "other": false, "Board": false, "herdr-focus": false,
	} {
		if got := s.Valid(); got != want {
			t.Errorf("AckSource(%q).Valid() = %v, want %v", s, got, want)
		}
	}
}

// SeenOnLeave is ADR-0015's rule, with no I/O and no clock: true only for a
// Finished, non-Background, unseen view with a known waiting_since, no row
// for the finish and a dwell at or above the minimum.
func TestSeenOnLeave(t *testing.T) {
	const min = 3 * time.Second
	finished := AgentView{Class: ClassFinished, Status: StatusDone, WaitingSince: 1000}
	base := LeaveInput{View: finished, Dwell: min, MinDwell: min}
	with := func(mutate func(*LeaveInput)) LeaveInput {
		in := base
		mutate(&in)
		return in
	}
	for _, tt := range []struct {
		name string
		in   LeaveInput
		want bool
	}{
		{"finished, dwell exactly the minimum", base, true},
		{"finished, dwell above the minimum", with(func(in *LeaveInput) { in.Dwell = time.Hour }), true},
		{"finished idle (done to idle within the finish)", with(func(in *LeaveInput) { in.View.Status = StatusIdle }), true},
		{"dwell the minimum minus 1 ms", with(func(in *LeaveInput) { in.Dwell = min - time.Millisecond }), false},
		{"no dwell", with(func(in *LeaveInput) { in.Dwell = 0 }), false},
		{"finish has a row (Mark unseen sticks)", with(func(in *LeaveInput) { in.FinishHasRow = true }), false},
		{"needs you", with(func(in *LeaveInput) { in.View.Class, in.View.Status = ClassNeedsYou, StatusBlocked }), false},
		{"idle (class working)", with(func(in *LeaveInput) { in.View.Class, in.View.Status = ClassWorking, StatusIdle }), false},
		{"working", with(func(in *LeaveInput) { in.View.Class, in.View.Status = ClassWorking, StatusWorking }), false},
		{"unknown", with(func(in *LeaveInput) { in.View.Class, in.View.Status = ClassUnknown, StatusUnknown }), false},
		{"background", with(func(in *LeaveInput) { in.View.Background, in.View.BackgroundTasks = true, 1 }), false},
		{"already seen", with(func(in *LeaveInput) { in.View.Seen = true }), false},
		{"waiting_since unknown", with(func(in *LeaveInput) { in.View.WaitingSince = 0 }), false},
		{"zero minimum still needs everything else", with(func(in *LeaveInput) { in.Dwell, in.MinDwell, in.FinishHasRow = 0, 0, true }), false},
		{"zero minimum, zero dwell", with(func(in *LeaveInput) { in.Dwell, in.MinDwell = 0, 0 }), true},
	} {
		if got := SeenOnLeave(tt.in); got != tt.want {
			t.Errorf("%s: SeenOnLeave = %v, want %v", tt.name, got, tt.want)
		}
	}
}
