package core

import (
	"math/rand/v2"
	"testing"
)

// tr builds a live or snapshot Transition to s at ms for the fold tests. It
// carries a previous Status (its value does not matter to the fold), so it
// continues the Agent folded so far.
func tr(t *testing.T, s Status, at int64, prov Provenance) Transition {
	t.Helper()
	prev := StatusUnknown
	return build(t, &prev, s, at, prov)
}

// first builds an Agent's first row: a Transition with no previous Status.
func first(t *testing.T, s Status, at int64, prov Provenance) Transition {
	t.Helper()
	return build(t, nil, s, at, prov)
}

func build(t *testing.T, prev *Status, s Status, at int64, prov Provenance) Transition {
	t.Helper()
	x, err := NewTransition(TransitionInput{
		Key:      AgentKey{Host: "mac", Server: "default", Epoch: 1, PaneID: "w1:p1"},
		Previous: prev, New: s, ObservedAt: at, Provenance: prov,
	})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestClassOf(t *testing.T) {
	for _, c := range []struct {
		s    Status
		ever bool
		want AttentionClass
	}{
		{StatusBlocked, false, ClassNeedsYou},
		{StatusBlocked, true, ClassNeedsYou},
		{StatusDone, false, ClassFinished},
		{StatusDone, true, ClassFinished},
		{StatusIdle, true, ClassFinished},
		{StatusIdle, false, ClassWorking},
		{StatusWorking, false, ClassWorking},
		{StatusWorking, true, ClassWorking},
		{StatusUnknown, true, ClassUnknown},
		{"", false, ClassUnknown},
		{"bogus", true, ClassUnknown},
	} {
		if got := ClassOf(c.s, c.ever); got != c.want {
			t.Errorf("ClassOf(%q, %v) = %s, want %s", c.s, c.ever, got, c.want)
		}
	}
}

func TestFold(t *testing.T) {
	const L, S = ProvenanceLive, ProvenanceSnapshot
	type step struct {
		s    Status
		at   int64
		prov Provenance
	}
	for _, c := range []struct {
		name  string
		steps []step
		want  FoldResult
	}{
		{"class start", []step{{StatusWorking, 1, L}, {StatusBlocked, 5, L}},
			FoldResult{Known: true, Status: StatusBlocked, Class: ClassNeedsYou, Since: 5, SinceProvenance: L, EverWorking: true}},
		{"done to idle", []step{{StatusWorking, 1, L}, {StatusDone, 5, L}, {StatusIdle, 9, S}},
			FoldResult{Known: true, Status: StatusIdle, Class: ClassFinished, Since: 5, SinceProvenance: L, EverWorking: true}},
		{"never working", []step{{StatusIdle, 3, S}},
			FoldResult{Known: true, Status: StatusIdle, Class: ClassWorking, Since: 3, SinceProvenance: S}},
		{"never working, then working: Idle ends, Working starts", []step{{StatusIdle, 3, S}, {StatusWorking, 6, L}},
			FoldResult{Known: true, Status: StatusWorking, Class: ClassWorking, Since: 6, SinceProvenance: L, EverWorking: true}},
		{"working stays working", []step{{StatusIdle, 3, S}, {StatusWorking, 6, L}, {StatusWorking, 8, S}},
			FoldResult{Known: true, Status: StatusWorking, Class: ClassWorking, Since: 6, SinceProvenance: L, EverWorking: true}},
		{"idle after working", []step{{StatusWorking, 1, L}, {StatusIdle, 4, L}},
			FoldResult{Known: true, Status: StatusIdle, Class: ClassFinished, Since: 4, SinceProvenance: L, EverWorking: true}},
		{"lower bound", []step{{StatusWorking, 1, L}, {StatusBlocked, 7, S}},
			FoldResult{Known: true, Status: StatusBlocked, Class: ClassNeedsYou, Since: 7, SinceProvenance: S, EverWorking: true}},
		{"unknown status", []step{{StatusWorking, 1, L}, {StatusUnknown, 2, L}},
			FoldResult{Known: true, Status: StatusUnknown, Class: ClassUnknown, Since: 2, SinceProvenance: L, EverWorking: true}},
		{"no transitions", nil, FoldResult{Class: ClassUnknown}},
	} {
		var ts []Transition
		for _, s := range c.steps {
			ts = append(ts, tr(t, s.s, s.at, s.prov))
		}
		if got := Fold(ts); got != c.want {
			t.Errorf("%s: Fold = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestFoldStartsOverAtFirstRow pins the boundary between successive Agents on
// one AgentKey (AD-3): a Transition with no previous Status discards the fold
// so far, EverWorking included.
func TestFoldStartsOverAtFirstRow(t *testing.T) {
	const L, S = ProvenanceLive, ProvenanceSnapshot
	for _, c := range []struct {
		name string
		ts   []Transition
		want FoldResult
	}{
		{"new agent idle after a finished one",
			[]Transition{first(t, StatusWorking, 1, L), tr(t, StatusDone, 5, L), first(t, StatusIdle, 9, S)},
			FoldResult{Known: true, Status: StatusIdle, Class: ClassWorking, Since: 9, SinceProvenance: S}},
		{"same status and class across the boundary still moves Since",
			[]Transition{first(t, StatusBlocked, 1, L), first(t, StatusBlocked, 5, S)},
			FoldResult{Known: true, Status: StatusBlocked, Class: ClassNeedsYou, Since: 5, SinceProvenance: S}},
		{"new agent then works",
			[]Transition{first(t, StatusWorking, 1, L), tr(t, StatusIdle, 3, L), first(t, StatusIdle, 6, S),
				tr(t, StatusWorking, 8, L), tr(t, StatusBlocked, 9, L)},
			FoldResult{Known: true, Status: StatusBlocked, Class: ClassNeedsYou, Since: 9, SinceProvenance: L, EverWorking: true}},
		{"only the last boundary counts",
			[]Transition{first(t, StatusWorking, 1, L), first(t, StatusDone, 2, L), first(t, StatusIdle, 3, L), tr(t, StatusIdle, 4, S)},
			FoldResult{Known: true, Status: StatusIdle, Class: ClassWorking, Since: 3, SinceProvenance: L}},
	} {
		if got := Fold(c.ts); got != c.want {
			t.Errorf("%s: Fold = %+v, want %+v", c.name, got, c.want)
		}
		var f FoldResult
		for _, x := range c.ts {
			f = f.Add(x)
		}
		if f != c.want {
			t.Errorf("%s: Add one by one = %+v, want %+v", c.name, f, c.want)
		}
		last := 0
		for i, x := range c.ts {
			if _, ok := x.Previous(); !ok {
				last = i
			}
		}
		if got := Fold(c.ts[last:]); got != c.want {
			t.Errorf("%s: Fold from the last first row = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestWaitingTime(t *testing.T) {
	exact := Fold([]Transition{tr(t, StatusWorking, 1, ProvenanceLive), tr(t, StatusBlocked, 5, ProvenanceLive)})
	if ms, ok := WaitingTime(exact, 105); ms != 100 || !ok {
		t.Errorf("exact: %d, %v; want 100, true", ms, ok)
	}
	lower := Fold([]Transition{tr(t, StatusBlocked, 5, ProvenanceSnapshot)})
	if ms, ok := WaitingTime(lower, 105); ms != 100 || ok {
		t.Errorf("lower bound: %d, %v; want 100, false", ms, ok)
	}
	if ms, ok := WaitingTime(lower, 2); ms != 0 || ok {
		t.Errorf("clock behind: %d, %v; want 0, false", ms, ok)
	}
	if ms, ok := WaitingTime(Fold(nil), 105); ms != 0 || ok {
		t.Errorf("unknown: %d, %v; want 0, false", ms, ok)
	}
}

func TestAddEqualsFold(t *testing.T) {
	statuses := []Status{StatusWorking, StatusBlocked, StatusDone, StatusIdle, StatusUnknown}
	provs := []Provenance{ProvenanceLive, ProvenanceSnapshot}
	r := rand.New(rand.NewPCG(1, 2))
	for range 500 {
		n := r.IntN(12)
		ts := make([]Transition, n)
		at := int64(1)
		for i := range ts {
			at += r.Int64N(1000)
			st, prov := statuses[r.IntN(len(statuses))], provs[r.IntN(len(provs))]
			if r.IntN(5) == 0 {
				ts[i] = first(t, st, at, prov)
			} else {
				ts[i] = tr(t, st, at, prov)
			}
		}
		f := Fold(nil)
		for i, x := range ts {
			f = f.Add(x)
			if want := Fold(ts[:i+1]); f != want {
				t.Fatalf("after %d: Add gives %+v, Fold gives %+v", i+1, f, want)
			}
		}
		if want := referenceFold(ts); f != want {
			t.Fatalf("Fold gives %+v, the reference %+v", f, want)
		}
	}
}

// referenceFold computes a fold from its definition, independently of Add:
// only the Transitions from the last one with no previous Status count; each
// one's Class from the ever-working prefix, and Since from the earliest
// Transition of the trailing run in the final Class, where Idle (class working
// before any working Status) and Working count as different runs.
func referenceFold(ts []Transition) FoldResult {
	if len(ts) == 0 {
		return FoldResult{Class: ClassUnknown}
	}
	for i := len(ts) - 1; i > 0; i-- {
		if _, ok := ts[i].Previous(); !ok {
			ts = ts[i:]
			break
		}
	}
	type run struct {
		class AttentionClass
		ever  bool
	}
	runs := make([]run, len(ts))
	ever := false
	for i, x := range ts {
		ever = ever || x.New() == StatusWorking
		runs[i] = run{ClassOf(x.New(), ever), ever}
	}
	last := len(ts) - 1
	start := last
	for start > 0 && runs[start-1] == runs[last] {
		start--
	}
	return FoldResult{Known: true, Status: ts[last].New(), Class: runs[last].class, Since: ts[start].ObservedAt(),
		SinceProvenance: ts[start].Provenance(), EverWorking: ever}
}
