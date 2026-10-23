package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr/gen"
)

// HealthKind names what went wrong with a subscription stream. Every kind means
// the engine can no longer trust that it saw every event; what to do about it is
// the engine's decision (AD-11).
type HealthKind string

// The health signal kinds.
const (
	// HealthEventsLost is herdr's events_lost error. herdr's documentation says
	// it means the subscriber fell behind herdr's retained history; that is
	// unreproduced here (docs/research/herdr-socket-api.md), so the code is
	// matched raw and the meaning is not relied on.
	HealthEventsLost HealthKind = "events_lost"
	// HealthError is any other error response after the subscription started.
	// herdr's codes are an open set; Code and Message carry it.
	HealthError HealthKind = "error"
	// HealthRejected is an error response to the subscribe request itself.
	// Subscribe is all or nothing, so nothing in the request took effect.
	HealthRejected HealthKind = "rejected"
	// HealthClosed is the connection closing or failing.
	HealthClosed HealthKind = "closed"
	// HealthUndecodable is a line that could not be decoded into an event.
	HealthUndecodable HealthKind = "undecodable"
)

// codeEventsLost is matched on the raw error.code; the 0.9.5 schema does not
// define it.
const codeEventsLost = "events_lost"

// Health is a stream-health signal. A subscription delivers at most one, as its
// last delivery, and then closes its channel. It is also the error, reachable
// with errors.As, that a subscribe call returns when the subscription never
// started.
type Health struct {
	Kind HealthKind
	// Code and Message are herdr's, for HealthEventsLost, HealthError and
	// HealthRejected.
	Code    string
	Message string
	// Err is the underlying cause: the *Error herdr sent, the connection error,
	// or the decoding error.
	Err error
}

func (h *Health) Error() string {
	switch {
	case h.Code != "":
		return fmt.Sprintf("herdr: subscription %s: %s: %s", h.Kind, h.Code, h.Message)
	case h.Err != nil:
		return fmt.Sprintf("herdr: subscription %s: %v", h.Kind, h.Err)
	}
	return fmt.Sprintf("herdr: subscription %s", h.Kind)
}

func (h *Health) Unwrap() error { return h.Err }

// Delivery is one item from a subscription: an event, a line flagged
// unexpected, or, last of all, a health signal.
type Delivery[T any] struct {
	Event T
	// Unexpected is set, on the lifecycle subscription only, for a line that
	// decodes as JSON but is not an event it can deliver (an unknown kind, no
	// data, no pane id). The delivery is not final: the stream goes on. Event
	// is then the zero value.
	Unexpected error
	// Health is set on the final delivery when the stream ended other than by
	// Close; Event is then the zero value.
	Health *Health
}

// Subscription is one live herdr subscription on its own connection. Events
// arrive on C in the order herdr sent them. C closes when the stream ends,
// after at most one health signal, or after Close.
//
// Close is required: a consumer that stops reading C without calling Close
// holds the subscription's goroutine and connection open.
type Subscription[T any] struct {
	C <-chan Delivery[T]

	conn      net.Conn
	done      chan struct{}
	closeOnce sync.Once
}

// Close ends the subscription, which is how herdr unsubscribes: the connection
// closes, and then C closes. One delivery already in flight when Close is called
// may still arrive on C after it; after that, no health signal is sent for the
// Close itself. Close is safe to call more than once and from any goroutine.
func (s *Subscription[T]) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		err = s.conn.Close()
	})
	return err
}

// lifecycleSubscriptions are the global kinds herddash follows, each as its own
// generated variant: the pane lifecycle kinds, then the seven kinds that only
// signal that tracked context may be stale. layout.updated is among them
// because pane positions come from the tab's layout, which a split or a close
// changes.
var lifecycleSubscriptions = []any{
	gen.SubscriptionPaneCreated{Type: gen.SubscriptionTypePaneCreated},
	gen.SubscriptionPaneClosed{Type: gen.SubscriptionTypePaneClosed},
	gen.SubscriptionPaneMoved{Type: gen.SubscriptionTypePaneMoved},
	gen.SubscriptionPaneExited{Type: gen.SubscriptionTypePaneExited},
	gen.SubscriptionPaneAgentDetected{Type: gen.SubscriptionTypePaneAgentDetected},
	gen.SubscriptionWorkspaceRenamed{Type: gen.SubscriptionTypeWorkspaceRenamed},
	gen.SubscriptionWorkspaceMoved{Type: gen.SubscriptionTypeWorkspaceMoved},
	gen.SubscriptionWorkspaceReordered{Type: gen.SubscriptionTypeWorkspaceReordered},
	gen.SubscriptionTabRenamed{Type: gen.SubscriptionTypeTabRenamed},
	gen.SubscriptionTabMoved{Type: gen.SubscriptionTypeTabMoved},
	gen.SubscriptionPaneUpdated{Type: gen.SubscriptionTypePaneUpdated},
	gen.SubscriptionLayoutUpdated{Type: gen.SubscriptionTypeLayoutUpdated},
}

// SubscribeLifecycle subscribes to pane created, closed, moved, exited and
// agent-detected events, and to workspace renamed, moved and reordered, tab
// renamed and moved, pane updated and layout updated events, on one
// connection. The last seven are delivered as core.LifecycleContextChanged with no detail. It returns once
// herdr has confirmed the subscription started. The context, or defaultCallTimeout when it has no
// deadline, bounds only that setup; the subscription lives until Close or until
// the stream ends.
func (c *Client) SubscribeLifecycle(ctx context.Context) (*Subscription[core.LifecycleEvent], error) {
	subs := make([]json.RawMessage, 0, len(lifecycleSubscriptions))
	for _, v := range lifecycleSubscriptions {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("herdr: subscribe lifecycle: %w", err)
		}
		subs = append(subs, b)
	}
	return subscribe(ctx, c, subs, decodeLifecycle, true)
}

// SubscribeStatus subscribes to the agent status changes of one pane on its own
// connection, and returns once herdr has confirmed the subscription started. An
// unknown pane is rejected by herdr: the error unwraps to a *Health of kind
// HealthRejected. The context, or defaultCallTimeout when it has no deadline,
// bounds only the setup.
func (c *Client) SubscribeStatus(ctx context.Context, paneID string) (*Subscription[core.StatusEvent], error) {
	b, err := json.Marshal(gen.SubscriptionPaneAgentStatusChanged{Type: gen.SubscriptionTypePaneAgentStatusChanged, PaneID: paneID})
	if err != nil {
		return nil, fmt.Errorf("herdr: subscribe status %s: %w", paneID, err)
	}
	return subscribe(ctx, c, []json.RawMessage{b}, decodeStatus, false)
}

// subscribe opens a connection, sends one events.subscribe request, waits for
// subscription_started, and then streams decoded events until the connection
// ends. With passUnexpected, a line the decoder reports as errUnexpectedEvent is
// delivered flagged Unexpected and the stream goes on; without it, such a line
// ends the stream as HealthUndecodable.
func subscribe[T any](ctx context.Context, c *Client, subs []json.RawMessage, decode func([]byte) (T, error), passUnexpected bool) (*Subscription[T], error) {
	id := c.id("s")
	req := gen.RequestEventsSubscribe{ID: id, Method: gen.RequestMethodEventsSubscribe,
		Params: gen.EventsSubscribeParams{Subscriptions: subs}}
	// Bounds setup only: the AfterFunc below is stopped before this returns.
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("herdr: subscribe: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	r := bufio.NewReader(conn)
	line, err := roundTrip(conn, r, req)
	stopped := stop()
	if err == nil && !stopped {
		// The context ended as the response arrived, and the connection is gone.
		err = ctx.Err()
	}
	if err != nil {
		conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("herdr: subscribe: %w", ctxErr)
		}
		return nil, fmt.Errorf("herdr: subscribe: %w", &Health{Kind: HealthClosed, Err: err})
	}
	if h := startHealth(line, id); h != nil {
		conn.Close()
		return nil, fmt.Errorf("herdr: subscribe: %w", h)
	}

	ch := make(chan Delivery[T])
	s := &Subscription[T]{C: ch, conn: conn, done: make(chan struct{})}
	go s.stream(r, ch, decode, passUnexpected)
	return s, nil
}

// startHealth checks the response to a subscribe request. It returns nil when
// the subscription started, and otherwise the health signal saying why not.
func startHealth(line []byte, id string) *Health {
	result, herr, err := decodeResponse(line, id)
	if err != nil {
		return &Health{Kind: HealthUndecodable, Err: err}
	}
	if herr != nil {
		herr.Method = string(gen.RequestMethodEventsSubscribe)
		return &Health{Kind: HealthRejected, Code: herr.Code, Message: herr.Message, Err: herr}
	}
	if err := decodeResult(result, gen.ResponseResultTypeSubscriptionStarted, nil); err != nil {
		return &Health{Kind: HealthUndecodable, Err: err}
	}
	return nil
}

// stream reads lines until the stream ends, delivering events (and, with
// passUnexpected, unexpected lines) in order and at most one health signal last,
// then closes ch.
func (s *Subscription[T]) stream(r *bufio.Reader, ch chan<- Delivery[T], decode func([]byte) (T, error), passUnexpected bool) {
	defer close(ch)
	defer s.Close()
	for {
		line, err := readLine(r)
		if err != nil {
			s.deliver(ch, Delivery[T]{Health: &Health{Kind: HealthClosed, Err: err}})
			return
		}
		if h := streamError(line); h != nil {
			s.deliver(ch, Delivery[T]{Health: h})
			return
		}
		ev, err := decode(line)
		if err != nil && passUnexpected && errors.Is(err, errUnexpectedEvent) {
			if !s.deliver(ch, Delivery[T]{Unexpected: err}) {
				return
			}
			continue
		}
		if err != nil {
			s.deliver(ch, Delivery[T]{Health: &Health{Kind: HealthUndecodable, Err: err}})
			return
		}
		if !s.deliver(ch, Delivery[T]{Event: ev}) {
			return
		}
	}
}

// deliver sends d unless the subscription was closed first. After Close no
// health signal is sent: the caller ended the stream on purpose.
func (s *Subscription[T]) deliver(ch chan<- Delivery[T], d Delivery[T]) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case ch <- d:
		return true
	case <-s.done:
		return false
	}
}

// streamError returns the health signal for an error response arriving after the
// subscription started, or nil when the line is not an error response.
func streamError(line []byte) *Health {
	var probe struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(line, &probe) != nil || len(probe.Error) == 0 || string(probe.Error) == "null" {
		return nil
	}
	var er gen.ErrorResponse
	if err := json.Unmarshal(line, &er); err != nil {
		return &Health{Kind: HealthUndecodable, Err: fmt.Errorf("decode error response: %w", err)}
	}
	herr := &Error{Method: string(gen.RequestMethodEventsSubscribe), Code: er.Error.Code, Message: er.Error.Message}
	kind := HealthError
	if er.Error.Code == codeEventsLost {
		kind = HealthEventsLost
	}
	return &Health{Kind: kind, Code: er.Error.Code, Message: er.Error.Message, Err: herr}
}
