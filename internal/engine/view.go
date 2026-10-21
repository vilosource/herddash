package engine

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/vilosource/herddash/internal/core"
)

// View is one consistent picture of every Agent and of the connection to
// herdr, published by the engine after each inbox message that changed
// something visible (AD-2, AD-7). A published View is never mutated: each one
// owns fresh slices, and a reader must not modify them either.
type View struct {
	// RunID names this Daemon run: 16 random bytes in hex, made in New.
	RunID string `json:"run_id"`
	// Seq is 1 for the run's first publication and goes up by exactly one per
	// publication; 0 before the first.
	Seq uint64 `json:"seq"`
	// Agents is every reported Agent, in Board order (core.SortViews).
	Agents     []core.AgentView     `json:"agents"`
	Connection core.ConnectionTruth `json:"connection"`
	// Headers are the indexes in Agents where a workspace header goes
	// (core.WorkspaceHeaders).
	Headers []int `json:"headers"`
}

// ChangeKind names what one Change describes.
type ChangeKind string

// The kinds of Change.
const (
	// ChangeAgent is an Agent first seen, or any change to its view other than
	// a Transition (Profile, context, Agent Program). Agent is its new view.
	ChangeAgent ChangeKind = "agent"
	// ChangeRemoved is an Agent gone. Key is its AgentKey and Agent its last
	// view.
	ChangeRemoved ChangeKind = "removed"
	// ChangeTransition is a recorded Transition: Transition, and Agent the
	// Agent's view after it.
	ChangeTransition ChangeKind = "transition"
	// ChangeResync is a resync beginning: Reason and Detail.
	ChangeResync ChangeKind = "resync"
	// ChangeConnection is any change to the ConnectionTruth: Connection is the
	// new one.
	ChangeConnection ChangeKind = "connection"
)

// Change is one thing that changed between two Views. Only the fields its
// Kind names are set.
type Change struct {
	Kind       ChangeKind
	Key        core.AgentKey
	Agent      core.AgentView
	Transition core.Transition
	Reason     string
	Detail     string
	Connection core.ConnectionTruth
}

// Publication is one View with the changes that produced it from the one
// before. Both are shared read-only by every subscriber, as a View is shared
// by every reader: a subscriber must not modify them.
//
// Changes are in the order they happened within the inbox message, except
// that agent changes found only by publish's final check of every reported
// Agent's view are appended after the others, in pane-id order.
type Publication struct {
	View    View
	Changes []Change
}

// ViewSubscription delivers every Publication, in order, on C. When C is full the
// engine closes it and drops the subscription: the reader starts again from a
// fresh View (AD-7).
type ViewSubscription struct {
	C <-chan Publication
	c chan Publication
}

// publisher holds what readers see: the latest View, read lock-free, and the
// subscriber set, locked only by Subscribe, unsubscribing and publish.
type publisher struct {
	view atomic.Pointer[View]
	mu   sync.Mutex
	subs map[*ViewSubscription]struct{}
	// stopped is set once Run has returned: every subscription was closed,
	// and Subscribe hands out closed ones.
	stopped bool
}

func newRunID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	return hex.EncodeToString(b[:])
}

// View returns the latest published View, or, before the first publication,
// one with Seq 0 and the run id. It never waits for the engine and is safe from
// any goroutine.
func (e *Engine) View() View {
	return *e.pub.view.Load()
}

// Subscribe returns a subscription that receives every publication from now
// on, on a channel holding buffer of them (at least 1), and the func that ends
// it. The engine never blocks on it: when the channel is full, the engine
// closes it and drops the subscription. The func closes the channel if the
// engine has not, and may be called any number of times. Both are safe from
// any goroutine. When Run returns, every subscription's channel is closed;
// Subscribe after that returns a closed channel and a func that does nothing.
// A reader that wants the current state too subscribes first,
// then reads View, and skips publications with a Seq not above that View's.
func (e *Engine) Subscribe(buffer int) (*ViewSubscription, func()) {
	if buffer < 1 {
		buffer = 1
	}
	c := make(chan Publication, buffer)
	s := &ViewSubscription{C: c, c: c}
	e.pub.mu.Lock()
	if e.pub.stopped {
		e.pub.mu.Unlock()
		close(c)
		return s, func() {}
	}
	e.pub.subs[s] = struct{}{}
	e.pub.mu.Unlock()
	return s, func() {
		e.pub.mu.Lock()
		defer e.pub.mu.Unlock()
		if _, ok := e.pub.subs[s]; ok {
			delete(e.pub.subs, s)
			close(s.c)
		}
	}
}

// change notes one change for the publication that ends the current message.
func (e *Engine) change(c Change) {
	e.changes = append(e.changes, c)
}

// setTruth notes a connection change when t differs from the current truth.
func (e *Engine) setTruth(t core.ConnectionTruth) {
	if t == e.truth {
		return
	}
	e.truth = t
	e.change(Change{Kind: ChangeConnection, Connection: t})
}

// viewOf builds the view of a reported Agent.
func (e *Engine) viewOf(p *pane) core.AgentView {
	return core.NewAgentView(core.AgentViewInput{
		Key:          e.key(p.id),
		AgentProgram: p.program,
		Profile:      p.profile,
		Status:       p.status,
		Fold:         p.fold,
		Context:      p.context,
		Home:         e.home,
		Background:   p.background,
		Transcript:   p.tasks,
	})
}

// noteView notes an agent change when a reported Agent's view differs from
// the one last noted for it.
func (e *Engine) noteView(p *pane) {
	if !p.known {
		return
	}
	v := e.viewOf(p)
	if p.noted && v == p.view {
		return
	}
	p.view, p.noted = v, true
	e.change(Change{Kind: ChangeAgent, Agent: v})
}

// publish ends one inbox message: any view change not yet noted is noted,
// and if anything changed, a new View is built from scratch, stored for
// readers, and offered to every subscriber with its change list.
func (e *Engine) publish() {
	ids := make([]string, 0, len(e.panes))
	for id, p := range e.panes {
		if p.known {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		e.noteView(e.panes[id])
	}
	if len(e.changes) == 0 {
		return
	}
	changes := e.changes
	e.changes = nil

	agents := make([]core.AgentView, 0, len(ids))
	for _, id := range ids {
		agents = append(agents, e.panes[id].view)
	}
	core.SortViews(agents)
	e.viewSeq++
	v := View{
		RunID:      e.runID,
		Seq:        e.viewSeq,
		Agents:     agents,
		Connection: e.truth,
		Headers:    core.WorkspaceHeaders(agents),
	}
	e.pub.view.Store(&v)

	e.pub.mu.Lock()
	defer e.pub.mu.Unlock()
	for s := range e.pub.subs {
		select {
		case s.c <- Publication{View: v, Changes: changes}:
		default:
			delete(e.pub.subs, s)
			close(s.c)
			e.log.Debug("view subscriber too slow; dropped")
		}
	}
}

// stopPublishing runs as Run returns: a connection still up is published as
// lost, so readers do not take the last View for a live one, then every
// subscription is closed and dropped, and no new one stays open.
func (e *Engine) stopPublishing() {
	if e.truth.Connected {
		t := e.truth
		t.Connected = false
		e.setTruth(t)
	}
	e.publish()
	e.pub.mu.Lock()
	defer e.pub.mu.Unlock()
	e.pub.stopped = true
	for s := range e.pub.subs {
		delete(e.pub.subs, s)
		close(s.c)
	}
}
