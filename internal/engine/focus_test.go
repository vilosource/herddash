package engine

import (
	"reflect"
	"testing"

	"github.com/vilosource/herddash/internal/core"
)

// Until Story 6.7 gives focus meaning, a pane_focused from herdr is ignored
// entirely (ADR-0015): it requests no snapshot read, between reads or during
// one (it is not an event that makes a read in flight stale), and changes no
// view.
func TestFocusIgnored(t *testing.T) {
	h, w, n := startedViewed(t)
	before := h.engine.View().Agents
	reads := h.fake.snapshots.Load()

	// Between reads: a move, a repeat for the same pane, a move back.
	h.focus("w1:p2", "w1")
	h.focus("w1:p2", "w1")
	h.focus("w1:p1", "w1")
	h.fake.noSnapshotCall()
	h.settle()
	if got := h.fake.snapshots.Load(); got != reads {
		t.Fatalf("%d snapshot reads after focus events, want %d", got, reads)
	}
	if got := len(w.all()); got != n {
		t.Errorf("%d publications after focus events, want %d: focus publishes nothing yet", got, n)
	}

	// During a read: the read in flight is not followed by another.
	h.fake.setGated(true)
	h.lifecycle(core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	h.fake.awaitSnapshotCall()
	h.focus("w1:p2", "w1")
	h.focus("w1:p3", "w1")
	h.fake.noSnapshotCall()
	h.fake.release <- struct{}{}
	h.fake.noSnapshotCall()
	h.fake.setGated(false)
	h.settle()
	if got := h.fake.snapshots.Load(); got != reads+1 {
		t.Errorf("%d snapshot reads, want %d: only the context change's", got, reads+1)
	}
	if got := h.engine.View().Agents; !reflect.DeepEqual(got, before) {
		t.Errorf("agents after focus events %+v, want unchanged %+v", got, before)
	}
}
