package core

import (
	"errors"
	"testing"
)

func statusPtr(s Status) *Status { return &s }

func TestNewTransition(t *testing.T) {
	live := AgentKey{Host: "h", Server: "default", Epoch: 1, PaneID: "w1:p1"}
	backfill := AgentKey{Host: "h", Server: "default", Epoch: BackfillEpoch, PaneID: "w1:p1"}
	const at int64 = 1_760_000_000_000

	tests := []struct {
		name    string
		in      TransitionInput
		wantErr error
		field   string
	}{
		{
			name: "valid live",
			in:   TransitionInput{Key: live, Previous: statusPtr(StatusWorking), New: StatusBlocked, Provenance: ProvenanceLive, ObservedAt: at},
		},
		{
			name: "valid snapshot",
			in:   TransitionInput{Key: live, New: StatusDone, Provenance: ProvenanceSnapshot, ObservedAt: at},
		},
		{
			name: "valid backfill at epoch 0",
			in:   TransitionInput{Key: backfill, New: StatusIdle, Provenance: ProvenanceBackfill, ImportKey: "k", ObservedAt: at},
		},
		{
			name: "previous unknown status is a real status",
			in:   TransitionInput{Key: live, Previous: statusPtr(StatusUnknown), New: StatusWorking, Provenance: ProvenanceLive, ObservedAt: at},
		},
		{
			name:    "backfill with nonzero epoch",
			in:      TransitionInput{Key: live, New: StatusIdle, Provenance: ProvenanceBackfill, ImportKey: "k", ObservedAt: at},
			wantErr: ErrInvalidEpoch,
			field:   "epoch",
		},
		{
			name:    "live at epoch 0",
			in:      TransitionInput{Key: backfill, New: StatusIdle, Provenance: ProvenanceLive, ObservedAt: at},
			wantErr: ErrInvalidEpoch,
			field:   "epoch",
		},
		{
			name:    "snapshot at epoch 0",
			in:      TransitionInput{Key: backfill, New: StatusIdle, Provenance: ProvenanceSnapshot, ObservedAt: at},
			wantErr: ErrInvalidEpoch,
			field:   "epoch",
		},
		{
			name:    "bad new status",
			in:      TransitionInput{Key: live, New: Status("sleeping"), Provenance: ProvenanceLive, ObservedAt: at},
			wantErr: ErrInvalidStatus,
			field:   "new status",
		},
		{
			name:    "empty new status",
			in:      TransitionInput{Key: live, Provenance: ProvenanceLive, ObservedAt: at},
			wantErr: ErrInvalidStatus,
			field:   "new status",
		},
		{
			name:    "bad previous status",
			in:      TransitionInput{Key: live, Previous: statusPtr("Working"), New: StatusIdle, Provenance: ProvenanceLive, ObservedAt: at},
			wantErr: ErrInvalidStatus,
			field:   "previous status",
		},
		{
			name:    "bad provenance",
			in:      TransitionInput{Key: live, New: StatusIdle, Provenance: Provenance("guess"), ObservedAt: at},
			wantErr: ErrInvalidProvenance,
			field:   "provenance",
		},
		{
			name:    "empty host",
			in:      TransitionInput{Key: AgentKey{Server: "default", Epoch: 1, PaneID: "w1:p1"}, New: StatusIdle, Provenance: ProvenanceLive, ObservedAt: at},
			wantErr: ErrInvalidKey,
			field:   "host",
		},
		{
			name:    "empty server",
			in:      TransitionInput{Key: AgentKey{Host: "h", Epoch: 1, PaneID: "w1:p1"}, New: StatusIdle, Provenance: ProvenanceLive, ObservedAt: at},
			wantErr: ErrInvalidKey,
			field:   "server",
		},
		{
			name:    "empty pane id",
			in:      TransitionInput{Key: AgentKey{Host: "h", Server: "default", Epoch: 1}, New: StatusIdle, Provenance: ProvenanceLive, ObservedAt: at},
			wantErr: ErrInvalidKey,
			field:   "pane id",
		},
		{
			name:    "zero observed at",
			in:      TransitionInput{Key: live, New: StatusIdle, Provenance: ProvenanceLive},
			wantErr: ErrInvalidObservedAt,
			field:   "observed at",
		},
		{
			name:    "negative observed at",
			in:      TransitionInput{Key: live, New: StatusIdle, Provenance: ProvenanceLive, ObservedAt: -1},
			wantErr: ErrInvalidObservedAt,
			field:   "observed at",
		},
		{
			name:    "backfill without import key",
			in:      TransitionInput{Key: backfill, New: StatusIdle, Provenance: ProvenanceBackfill, ObservedAt: at},
			wantErr: ErrInvalidImportKey,
			field:   "import key",
		},
		{
			name:    "live with import key",
			in:      TransitionInput{Key: live, New: StatusIdle, Provenance: ProvenanceLive, ImportKey: "k", ObservedAt: at},
			wantErr: ErrInvalidImportKey,
			field:   "import key",
		},
		{
			name:    "snapshot with import key",
			in:      TransitionInput{Key: live, New: StatusIdle, Provenance: ProvenanceSnapshot, ImportKey: "k", ObservedAt: at},
			wantErr: ErrInvalidImportKey,
			field:   "import key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewTransition(tc.in)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("NewTransition: unexpected error %v", err)
				}
				if got.New() != tc.in.New || got.Provenance() != tc.in.Provenance || got.Key() != tc.in.Key {
					t.Fatalf("NewTransition returned %+v, want fields from %+v", got, tc.in)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewTransition error = %v, want %v", err, tc.wantErr)
			}
			var te *TransitionError
			if !errors.As(err, &te) {
				t.Fatalf("NewTransition error %T is not *TransitionError", err)
			}
			if te.Field != tc.field {
				t.Fatalf("TransitionError.Field = %q, want %q", te.Field, tc.field)
			}
		})
	}
}

func TestTransitionRoundTrip(t *testing.T) {
	order := uint64(42)
	in := TransitionInput{
		Key:          AgentKey{Host: "mac", Server: "work", Epoch: BackfillEpoch, PaneID: "w2:p3"},
		AgentProgram: "claude",
		Profile:      "cldw",
		Previous:     statusPtr(StatusWorking),
		New:          StatusBlocked,
		HerdrOrder:   &order,
		HerdrVersion: "0.9.5",
		ObservedAt:   1_760_000_000_123,
		Provenance:   ProvenanceBackfill,
		ImportKey:    "plugin-log:17",
	}
	tr, err := NewTransition(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := tr.Key(); got != in.Key {
		t.Errorf("Key() = %+v, want %+v", got, in.Key)
	}
	if got := tr.AgentProgram(); got != in.AgentProgram {
		t.Errorf("AgentProgram() = %q, want %q", got, in.AgentProgram)
	}
	if got := tr.Profile(); got != in.Profile {
		t.Errorf("Profile() = %q, want %q", got, in.Profile)
	}
	if got, ok := tr.Previous(); !ok || got != StatusWorking {
		t.Errorf("Previous() = %q, %v; want %q, true", got, ok, StatusWorking)
	}
	if got := tr.New(); got != in.New {
		t.Errorf("New() = %q, want %q", got, in.New)
	}
	if got, ok := tr.HerdrOrder(); !ok || got != 42 {
		t.Errorf("HerdrOrder() = %d, %v; want 42, true", got, ok)
	}
	if got := tr.HerdrVersion(); got != in.HerdrVersion {
		t.Errorf("HerdrVersion() = %q, want %q", got, in.HerdrVersion)
	}
	if got := tr.ObservedAt(); got != in.ObservedAt {
		t.Errorf("ObservedAt() = %d, want %d", got, in.ObservedAt)
	}
	if got := tr.Provenance(); got != in.Provenance {
		t.Errorf("Provenance() = %q, want %q", got, in.Provenance)
	}
	if got := tr.ImportKey(); got != in.ImportKey {
		t.Errorf("ImportKey() = %q, want %q", got, in.ImportKey)
	}
}

func TestTransitionUnknownPreviousIsAbsent(t *testing.T) {
	tr, err := NewTransition(TransitionInput{
		Key:        AgentKey{Host: "h", Server: "default", Epoch: 1, PaneID: "w1:p1"},
		New:        StatusWorking,
		Provenance: ProvenanceLive,
		ObservedAt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prev, ok := tr.Previous(); ok || prev != "" {
		t.Fatalf("Previous() = %q, %v; want absent", prev, ok)
	}
	if _, ok := tr.HerdrOrder(); ok {
		t.Fatal("HerdrOrder() present, want absent")
	}
}

func TestTransitionCopiesPointers(t *testing.T) {
	prev := StatusWorking
	order := uint64(7)
	tr, err := NewTransition(TransitionInput{
		Key:        AgentKey{Host: "h", Server: "default", Epoch: 1, PaneID: "w1:p1"},
		Previous:   &prev,
		New:        StatusDone,
		HerdrOrder: &order,
		Provenance: ProvenanceLive,
		ObservedAt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	prev, order = StatusBlocked, 9
	if p, _ := tr.Previous(); p != StatusWorking {
		t.Fatalf("Previous() = %q after caller mutation, want %q", p, StatusWorking)
	}
	if o, _ := tr.HerdrOrder(); o != 7 {
		t.Fatalf("HerdrOrder() = %d after caller mutation, want 7", o)
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{StatusWorking, StatusBlocked, StatusDone, StatusIdle, StatusUnknown} {
		if !s.Valid() {
			t.Errorf("%q.Valid() = false", s)
		}
	}
	for _, s := range []Status{"", "Working", "waiting", "null"} {
		if s.Valid() {
			t.Errorf("%q.Valid() = true", s)
		}
	}
}

func TestProvenanceValid(t *testing.T) {
	for _, p := range []Provenance{ProvenanceLive, ProvenanceSnapshot, ProvenanceBackfill} {
		if !p.Valid() {
			t.Errorf("%q.Valid() = false", p)
		}
	}
	for _, p := range []Provenance{"", "Live", "import"} {
		if p.Valid() {
			t.Errorf("%q.Valid() = true", p)
		}
	}
}
