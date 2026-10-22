package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
)

// The event stream's tunables.
const (
	// eventBuffer is how many publications a stream's subscription holds
	// before the engine drops it as too slow (AD-7).
	eventBuffer = 64
	// streamWriteTimeout bounds each write to a stream, so a stalled client
	// cannot pin its goroutine forever.
	streamWriteTimeout = 10 * time.Second
	// keepaliveInterval is how often a quiet stream sends a comment line.
	keepaliveInterval = 15 * time.Second
	// retryPrefix opens every stream: it tells an EventSource to wait 2 s
	// before reconnecting.
	retryPrefix = "retry: 2000\n"
)

// The SSE event names: the public /events contract. They are mapped from
// engine change kinds explicitly, so renaming an engine constant cannot
// change them.
const (
	eventState      = "state"
	eventAgent      = "agent"
	eventRemoved    = "removed"
	eventTransition = "transition"
	eventResync     = "resync"
	eventConnection = "connection"
)

// codeUnavailable is the error code of a /events request refused because the
// engine has stopped or the server is shutting down.
const codeUnavailable = "unavailable"

// codeInternal is the error code of a state that could not be encoded.
const codeInternal = "internal"

// loggedKinds holds the unknown change kinds already logged, so each is
// logged once per process.
var loggedKinds sync.Map

// wireRemoved is the data of a removed event.
type wireRemoved struct {
	Key core.AgentKey `json:"key"`
}

// wireTransition is a core.Transition as the event stream sends it, built
// from its accessors. Unknown values are omitted. observed_at is UTC Unix
// milliseconds. observed_by is omitted when herdr observed the change, and
// "claude_session" when Claude Code's session record did (ADR-0008). The import key is deliberately not sent: it identifies a
// backfilled row for re-import and means nothing to a reader.
type wireTransition struct {
	AgentProgram     core.AgentProgram `json:"agent_program"`
	Profile          string            `json:"profile"`
	Previous         *core.Status      `json:"previous,omitempty"`
	New              core.Status       `json:"new"`
	HerdrOrder       *uint64           `json:"herdr_order,omitempty"`
	HerdrVersion     string            `json:"herdr_version,omitempty"`
	ObservedAt       int64             `json:"observed_at"`
	Provenance       core.Provenance   `json:"provenance"`
	PossiblySpurious bool              `json:"possibly_spurious"`
	ObservedBy       core.Observer     `json:"observed_by,omitempty"`
}

// wireTransitionEvent is the data of a transition event: the Agent's key, the
// Transition and the Agent's view after it.
type wireTransitionEvent struct {
	Key        core.AgentKey  `json:"key"`
	Transition wireTransition `json:"transition"`
	Agent      core.AgentView `json:"agent"`
}

// wireResync is the data of a resync event.
type wireResync struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

func newWireTransition(t core.Transition) wireTransition {
	w := wireTransition{
		AgentProgram:     t.AgentProgram(),
		Profile:          t.Profile(),
		New:              t.New(),
		HerdrVersion:     t.HerdrVersion(),
		ObservedAt:       t.ObservedAt(),
		Provenance:       t.Provenance(),
		PossiblySpurious: t.PossiblySpurious(),
		ObservedBy:       t.ObservedBy(),
	}
	if p, ok := t.Previous(); ok {
		w.Previous = &p
	}
	if o, ok := t.HerdrOrder(); ok {
		w.HerdrOrder = &o
	}
	return w
}

// changeData returns the event name and data of one Change, or ok false for
// a kind the stream does not know.
func changeData(c engine.Change) (name string, data any, ok bool) {
	switch c.Kind {
	case engine.ChangeAgent:
		return eventAgent, c.Agent, true
	case engine.ChangeRemoved:
		return eventRemoved, wireRemoved{Key: c.Key}, true
	case engine.ChangeTransition:
		return eventTransition, wireTransitionEvent{Key: c.Transition.Key(), Transition: newWireTransition(c.Transition), Agent: c.Agent}, true
	case engine.ChangeResync:
		return eventResync, wireResync{Reason: c.Reason, Detail: c.Detail}, true
	case engine.ChangeConnection:
		return eventConnection, c.Connection, true
	}
	return "", nil, false
}

// serveEvents is GET /events: a Server-Sent Events stream (AD-7). It
// subscribes first, then reads the View, so nothing published between the two
// is missed: the stream opens with a retry line and a state event carrying
// exactly the /api/state body, then sends one event per Change of every later
// publication, in order, skipping publications the state already covers. It
// keeps no history and ignores Last-Event-ID: every stream starts with state.
// After CloseStreams, or once the engine has stopped, it answers 503 so a
// client backs off instead of reconnecting in a loop. A stream ends when the
// client goes, the subscription is closed (too slow, or the engine stopped), a
// write fails, or CloseStreams is called; it always unsubscribes.
func (h *Handler) serveEvents(w http.ResponseWriter, r *http.Request) {
	if h.stopped() {
		writeError(w, r, http.StatusServiceUnavailable, codeUnavailable, "the server is shutting down")
		return
	}
	sub, unsubscribe := h.subscribe(eventBuffer)
	defer unsubscribe()
	// A publication already queued is kept; a channel already closed means the
	// engine has stopped.
	var pending []engine.Publication
	select {
	case p, ok := <-sub.C:
		if !ok {
			writeError(w, r, http.StatusServiceUnavailable, codeUnavailable, "the engine has stopped")
			return
		}
		pending = append(pending, p)
	default:
	}
	v := h.view()
	state, err := json.Marshal(v)
	if err != nil {
		h.log.Error("events: encode state", "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, codeInternal, "the state could not be encoded")
		return
	}

	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	// The server's write deadline, if any, is for whole responses; a stream
	// sets its own around each write instead.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.log.Warn("events: clear write deadline", "error", err.Error())
	}
	w.WriteHeader(http.StatusOK)

	// write sends b and flushes it, within writeTimeout.
	write := func(b []byte) error {
		if err := rc.SetWriteDeadline(time.Now().Add(h.writeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if err := rc.Flush(); err != nil {
			return err
		}
		if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		return nil
	}
	logWrite := func(err error) {
		if errors.Is(err, http.ErrNotSupported) {
			h.log.Warn("events: the response cannot be flushed", "error", err.Error())
			return
		}
		h.log.Debug("events: write", "error", err.Error())
	}
	frame := func(prefix, event string, id uint64, body []byte) []byte {
		b := make([]byte, 0, len(prefix)+len(body)+64)
		b = append(b, prefix...)
		b = append(b, "event: "...)
		b = append(b, event...)
		b = append(b, "\nid: "...)
		b = strconv.AppendUint(b, id, 10)
		b = append(b, "\ndata: "...)
		b = append(b, body...)
		b = append(b, "\n\n"...)
		return b
	}

	if err := write(frame(retryPrefix, eventState, v.Seq, state)); err != nil {
		logWrite(err)
		return
	}

	// forward sends one publication's events; false ends the stream.
	forward := func(p engine.Publication) bool {
		if p.View.Seq <= v.Seq {
			return true
		}
		for _, c := range p.Changes {
			select {
			case <-h.stop:
				return false
			case <-r.Context().Done():
				return false
			default:
			}
			name, data, ok := changeData(c)
			if !ok {
				if _, seen := loggedKinds.LoadOrStore(c.Kind, struct{}{}); !seen {
					h.log.Warn("events: unknown change kind", "kind", string(c.Kind))
				}
				continue
			}
			body, err := json.Marshal(data)
			if err != nil {
				h.log.Error("events: encode", "kind", name, "error", err.Error())
				return false
			}
			if err := write(frame("", name, p.View.Seq, body)); err != nil {
				logWrite(err)
				return false
			}
		}
		return true
	}
	for _, p := range pending {
		if !forward(p) {
			return
		}
	}

	interval := h.keepalive
	if interval <= 0 {
		interval = keepaliveInterval
	}
	keepalive := time.NewTicker(interval)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.stop:
			return
		case <-keepalive.C:
			if err := write([]byte(": keepalive\n\n")); err != nil {
				logWrite(err)
				return
			}
		case p, ok := <-sub.C:
			if !ok {
				// Too slow, or the engine stopped: the client reconnects to a
				// fresh state.
				return
			}
			if !forward(p) {
				return
			}
		}
	}
}

// stopped reports whether CloseStreams has been called.
func (h *Handler) stopped() bool {
	select {
	case <-h.stop:
		return true
	default:
		return false
	}
}
