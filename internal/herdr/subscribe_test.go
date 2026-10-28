package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
)

const started = `{"id":"s1","result":{"type":"subscription_started"}}`

// subscribeRequest is what the fake decodes from an events.subscribe request.
type subscribeRequest struct {
	Subscriptions []struct {
		Type   string `json:"type"`
		PaneID string `json:"pane_id"`
	} `json:"subscriptions"`
}

// streamFake starts a fake that acknowledges a subscription, sends lines, and
// then closes the connection, or holds it open when hold is set. Each request it
// receives is passed to requests.
func streamFake(t *testing.T, hold bool, lines ...string) (*fakeServer, <-chan subscribeRequest) {
	t.Helper()
	requests := make(chan subscribeRequest, 8)
	s := newFake(t, func(fc *fakeConn) {
		if fc.req.Method != "events.subscribe" {
			fc.t.Errorf("method %q", fc.req.Method)
		}
		var r subscribeRequest
		if err := json.Unmarshal(fc.req.Params, &r); err != nil {
			fc.t.Errorf("params: %v", err)
		}
		requests <- r
		fc.reply(started)
		for _, l := range lines {
			fc.send(l)
		}
		if hold {
			fc.hold()
		}
	})
	return s, requests
}

// collect reads deliveries until the channel closes.
func collect[T any](t *testing.T, s *Subscription[T]) []Delivery[T] {
	t.Helper()
	var out []Delivery[T]
	timeout := time.After(5 * time.Second)
	for {
		select {
		case d, ok := <-s.C:
			if !ok {
				return out
			}
			out = append(out, d)
		case <-timeout:
			t.Fatalf("subscription did not end; got %d deliveries", len(out))
		}
	}
}

// lastHealth asserts the deliveries end with exactly one health signal of kind
// and returns it with the events before it.
func lastHealth[T any](t *testing.T, ds []Delivery[T], kind HealthKind) ([]T, *Health) {
	t.Helper()
	if len(ds) == 0 {
		t.Fatal("no deliveries")
	}
	var events []T
	for i, d := range ds[:len(ds)-1] {
		if d.Health != nil {
			t.Fatalf("delivery %d is health %v before the last", i, d.Health)
		}
		events = append(events, d.Event)
	}
	h := ds[len(ds)-1].Health
	if h == nil {
		t.Fatalf("last delivery is an event %+v, want health %s", ds[len(ds)-1].Event, kind)
	}
	if h.Kind != kind {
		t.Fatalf("health %v, want kind %s", h, kind)
	}
	return events, h
}

func TestSubscribeLifecycle(t *testing.T) {
	created := `{"event":"pane_created","data":{"type":"pane_created","pane":{"pane_id":"w1:p4","terminal_id":"term_x","workspace_id":"w1","tab_id":"w1:t1","focused":false,"agent_status":"unknown","revision":0}}}`
	moved := `{"event":"pane_moved","data":{"type":"pane_moved","pane":{"pane_id":"w2:p2","terminal_id":"term_x","workspace_id":"w2","tab_id":"w2:t1","focused":false,"agent_status":"unknown","revision":0},"previous_pane_id":"w1:p4","previous_tab_id":"w1:t1","previous_workspace_id":"w1"}}`
	detected := fixture(t, "event_lifecycle.json")
	closed := `{"event":"pane_closed","data":{"type":"pane_closed","pane_id":"w2:p2","workspace_id":"w2"}}`
	exited := `{"event":"pane_exited","data":{"type":"pane_exited","pane_id":"w1:p1","workspace_id":"w1"}}`
	detectedFinal := `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"claude","released":false,"final_status":"working"}}`
	released := `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"claude","released":true,"final_status":"idle"}}`

	contextLines := []string{
		`{"event":"workspace_renamed","data":{"type":"workspace_renamed","workspace_id":"w1","label":"renamed"}}`,
		`{"event":"workspace_moved","data":{"type":"workspace_moved","workspace_id":"w1","insert_index":1,"workspaces":[]}}`,
		`{"event":"workspace_reordered","data":{"type":"workspace_reordered","workspace_ids":["w2","w1"],"workspaces":[]}}`,
		`{"event":"tab_renamed","data":{"type":"tab_renamed","tab_id":"w1:t1","workspace_id":"w1","label":"main"}}`,
		`{"event":"tab_moved","data":{"type":"tab_moved","tab_id":"w1:t1","workspace_id":"w1","insert_index":0,"tabs":[]}}`,
		`{"event":"pane_updated","data":{"type":"pane_updated","pane":{"pane_id":"w1:p1","terminal_id":"term_x","workspace_id":"w1","tab_id":"w1:t1","focused":false,"agent_status":"idle","revision":1}}}`,
		`{"event":"layout_updated","data":{"type":"layout_updated","layout":{"workspace_id":"w1","tab_id":"w1:t1","zoomed":false,"area":{"x":0,"y":0,"width":120,"height":40},"focused_pane_id":"w1:p1","panes":[],"splits":[]}}}`,
	}
	lines := append([]string{created, moved, detected, closed, exited, detectedFinal, released}, contextLines...)
	s, requests := streamFake(t, false, lines...)
	sub, err := s.client().SubscribeLifecycle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	req := <-requests
	var types []string
	for _, r := range req.Subscriptions {
		if r.PaneID != "" {
			t.Errorf("lifecycle subscription %s carries pane_id %q", r.Type, r.PaneID)
		}
		types = append(types, r.Type)
	}
	wantTypes := []string{"pane.created", "pane.closed", "pane.moved", "pane.exited", "pane.agent_detected",
		"workspace.renamed", "workspace.moved", "workspace.reordered", "tab.renamed", "tab.moved", "pane.updated", "layout.updated", "pane.focused"}
	if !reflect.DeepEqual(types, wantTypes) {
		t.Errorf("subscribed %v, want %v", types, wantTypes)
	}

	events, _ := lastHealth(t, collect(t, sub), HealthClosed)
	want := []core.LifecycleEvent{
		{Kind: core.LifecycleCreated, PaneID: "w1:p4", TerminalID: "term_x", WorkspaceID: "w1", TabID: "w1:t1"},
		{Kind: core.LifecycleMoved, PaneID: "w2:p2", PreviousPaneID: "w1:p4", TerminalID: "term_x", WorkspaceID: "w2", TabID: "w2:t1"},
		{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude"},
		{Kind: core.LifecycleClosed, PaneID: "w2:p2"},
		{Kind: core.LifecycleExited, PaneID: "w1:p1"},
		{Kind: core.LifecycleAgentDetected, PaneID: "w1:p1", AgentProgram: "claude", FinalStatus: core.StatusWorking},
		{Kind: core.LifecycleAgentReleased, PaneID: "w1:p1", AgentProgram: "claude", FinalStatus: core.StatusIdle},
	}
	for range contextLines {
		want = append(want, core.LifecycleEvent{Kind: core.LifecycleContextChanged})
	}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events\n got %+v\nwant %+v", events, want)
	}
	if s.conns.Load() != 1 {
		t.Errorf("connections = %d, want 1", s.conns.Load())
	}
}

func TestSubscribeStatus(t *testing.T) {
	working := `{"data":{"agent":"claude","agent_status":"working","pane_id":"w1:p1","workspace_id":"w1"},"event":"pane.agent_status_changed"}`
	novel := `{"data":{"agent_status":"brand_new","pane_id":"w1:p1","workspace_id":"w1"},"event":"pane.agent_status_changed"}`
	s, requests := streamFake(t, true, fixture(t, "event_pane_scoped.json"), working, novel)
	sub, err := s.client().SubscribeStatus(testContext(t), "w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	req := <-requests
	if len(req.Subscriptions) != 1 || req.Subscriptions[0].Type != "pane.agent_status_changed" || req.Subscriptions[0].PaneID != "w1:p1" {
		t.Errorf("subscribed %+v", req.Subscriptions)
	}
	want := []core.StatusEvent{
		{PaneID: "w1:p1", Status: core.StatusIdle, AgentProgram: "claude"},
		{PaneID: "w1:p1", Status: core.StatusWorking, AgentProgram: "claude"},
		// Status strings pass through verbatim, even ones herddash does not know.
		{PaneID: "w1:p1", Status: "brand_new"},
	}
	for i, w := range want {
		select {
		case d := <-sub.C:
			if d.Health != nil || d.Event != w {
				t.Errorf("delivery %d = %+v (health %v), want %+v", i, d.Event, d.Health, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no delivery %d", i)
		}
	}
	// Close is unsubscribe: the channel closes with no health signal.
	if err := sub.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if ds := collect(t, sub); len(ds) != 0 {
		t.Errorf("after Close got %+v", ds)
	}
	if err := sub.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestSubscribeRejected(t *testing.T) {
	rejection := fixture(t, "error.json")
	s := newFake(t, func(fc *fakeConn) { fc.reply(rejection) })
	c := s.client()

	sub, err := c.SubscribeStatus(testContext(t), "w9:p9")
	if sub != nil {
		t.Error("rejected subscribe returned a subscription")
	}
	var h *Health
	if !errors.As(err, &h) {
		t.Fatalf("err %v, want a *Health", err)
	}
	if h.Kind != HealthRejected || h.Code != "pane_not_found" || h.Message != "pane w9:p9 not found" {
		t.Errorf("health %+v", h)
	}
	var herr *Error
	if !errors.As(err, &herr) || herr.Code != "pane_not_found" {
		t.Errorf("err %v does not unwrap to the herdr error", err)
	}

	if _, err := c.SubscribeLifecycle(testContext(t)); !errors.As(err, &h) || h.Kind != HealthRejected {
		t.Errorf("lifecycle err %v, want rejected", err)
	}
}

func TestSubscribeFailsBeforeStart(t *testing.T) {
	tests := map[string]struct {
		handle func(fc *fakeConn)
		kind   HealthKind
	}{
		"closed":      {func(fc *fakeConn) {}, HealthClosed},
		"garbage":     {func(fc *fakeConn) { fc.send("not json") }, HealthUndecodable},
		"wrong reply": {func(fc *fakeConn) { fc.reply(`{"id":"s1","result":{"type":"pong","version":"0.9.5","protocol":22}}`) }, HealthUndecodable},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := newFake(t, tt.handle)
			sub, err := s.client().SubscribeStatus(testContext(t), "w1:p1")
			var h *Health
			if sub != nil || !errors.As(err, &h) || h.Kind != tt.kind {
				t.Fatalf("sub %v err %v, want health %s", sub, err, tt.kind)
			}
		})
	}
}

func TestSubscriptionHealth(t *testing.T) {
	status := fixture(t, "event_pane_scoped.json")
	tests := []struct {
		name  string
		lines []string
		kind  HealthKind
		code  string
	}{
		{"events lost", []string{status, `{"id":"s1","error":{"code":"events_lost","message":"subscriber fell behind"}}`}, HealthEventsLost, "events_lost"},
		{"unknown code", []string{status, `{"id":"s1","error":{"code":"brand_new","message":"something new"}}`}, HealthError, "brand_new"},
		{"closed", []string{status}, HealthClosed, ""},
		{"garbage", []string{status, `not json`, status}, HealthUndecodable, ""},
		{"wrong event", []string{status, `{"data":{"pane_id":"w1:p1"},"event":"pane.scroll_changed"}`}, HealthUndecodable, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Hold the connection after the lines, so only the line itself can
			// end the stream (except in "closed", where nothing else is sent).
			hold := tt.kind != HealthClosed
			s, _ := streamFake(t, hold, tt.lines...)
			sub, err := s.client().SubscribeStatus(testContext(t), "w1:p1")
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			events, h := lastHealth(t, collect(t, sub), tt.kind)
			if len(events) != 1 || events[0].Status != core.StatusIdle {
				t.Errorf("events before health %+v, want the one idle event", events)
			}
			if h.Code != tt.code {
				t.Errorf("code %q, want %q", h.Code, tt.code)
			}
			if tt.code != "" {
				var herr *Error
				if !errors.As(h, &herr) || herr.Code != tt.code {
					t.Errorf("health %v does not unwrap to the herdr error", h)
				}
			}
		})
	}
}

func TestLifecycleUndecodable(t *testing.T) {
	tests := map[string]string{
		"garbage":       `{"event":`,
		"bad data type": `{"event":"pane_moved","data":{"pane":"w1:p1"}}`,
	}
	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			s, _ := streamFake(t, true, line)
			sub, err := s.client().SubscribeLifecycle(testContext(t))
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			if events, _ := lastHealth(t, collect(t, sub), HealthUndecodable); len(events) != 0 {
				t.Errorf("events %+v", events)
			}
		})
	}
}

// A lifecycle line that decodes as JSON but is not an event the subscription
// can deliver arrives flagged Unexpected, and the stream goes on: a later event
// still arrives and nothing ends the stream but the connection closing.
func TestLifecycleUnexpected(t *testing.T) {
	closed := `{"event":"pane_closed","data":{"type":"pane_closed","pane_id":"w2:p2","workspace_id":"w2"}}`
	tests := map[string]string{
		"unknown kind":            `{"event":"tab_created","data":{"type":"tab_created","tab_id":"w1:t2","workspace_id":"w1"}}`,
		"pane_focused no pane id": `{"event":"pane_focused","data":{"type":"pane_focused","workspace_id":"w1"}}`,
		"no data":                 `{"event":"pane_closed"}`,
		"no pane id":              `{"event":"pane_closed","data":{"type":"pane_closed","workspace_id":"w1"}}`,
	}
	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			s, _ := streamFake(t, false, line, line, closed)
			sub, err := s.client().SubscribeLifecycle(testContext(t))
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			ds := collect(t, sub)
			if len(ds) != 4 {
				t.Fatalf("deliveries %+v, want two unexpected, one event, one health", ds)
			}
			for i, d := range ds[:2] {
				if d.Unexpected == nil || !errors.Is(d.Unexpected, errUnexpectedEvent) || d.Health != nil || d.Event != (core.LifecycleEvent{}) {
					t.Errorf("delivery %d %+v, want unexpected with no event or health", i, d)
				}
			}
			if d := ds[2]; d.Unexpected != nil || d.Health != nil || d.Event.Kind != core.LifecycleClosed || d.Event.PaneID != "w2:p2" {
				t.Errorf("delivery 2 %+v, want the pane_closed event", d)
			}
			if d := ds[3]; d.Health == nil || d.Health.Kind != HealthClosed {
				t.Errorf("last delivery %+v, want health closed", d)
			}
		})
	}
}

// herdr's focus lines (ADR-0015), copied from the focus record's transcript
// (docs/research/herdr-focus-events-2026-10-27.md): every focus change sends
// workspace_focused, tab_focused and pane_focused. Only the subscription to
// pane.focused is requested. The first two are dropped without a delivery, so
// they never count as unexpected (which would make the engine read a
// snapshot); each pane_focused, a repeat for the same pane included, is
// delivered as LifecycleFocused with its pane and workspace; one with no
// pane_id is flagged unexpected; and the stream goes on.
func TestLifecycleFocus(t *testing.T) {
	lines := []string{
		`{"data":{"type":"workspace_focused","workspace_id":"w1"},"event":"workspace_focused"}`,
		`{"data":{"tab_id":"w1:t1","type":"tab_focused","workspace_id":"w1"},"event":"tab_focused"}`,
		`{"data":{"pane_id":"w1:p2","type":"pane_focused","workspace_id":"w1"},"event":"pane_focused"}`,
		`{"data":{"pane_id":"w1:p2","type":"pane_focused","workspace_id":"w1"},"event":"pane_focused"}`,
		`{"data":{"type":"workspace_focused","workspace_id":"w2"},"event":"workspace_focused"}`,
		`{"data":{"tab_id":"w2:t1","type":"tab_focused","workspace_id":"w2"},"event":"tab_focused"}`,
		`{"data":{"type":"pane_focused","workspace_id":"w2"},"event":"pane_focused"}`,
		`{"event":"tab_focused"}`,
		`{"data":{"pane_id":"w2:p1","type":"pane_focused","workspace_id":"w2"},"event":"pane_focused"}`,
	}
	s, requests := streamFake(t, false, lines...)
	sub, err := s.client().SubscribeLifecycle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	req := <-requests
	var focus []string
	for _, r := range req.Subscriptions {
		switch r.Type {
		case "pane.focused", "tab.focused", "workspace.focused":
			focus = append(focus, r.Type)
		}
	}
	if !reflect.DeepEqual(focus, []string{"pane.focused"}) {
		t.Errorf("focus subscriptions %v, want only pane.focused", focus)
	}

	ds := collect(t, sub)
	if len(ds) != 5 {
		t.Fatalf("deliveries %+v, want two focus events, one unexpected, one focus event, one health", ds)
	}
	for i, want := range map[int]core.LifecycleEvent{
		0: {Kind: core.LifecycleFocused, PaneID: "w1:p2", WorkspaceID: "w1"},
		1: {Kind: core.LifecycleFocused, PaneID: "w1:p2", WorkspaceID: "w1"},
		3: {Kind: core.LifecycleFocused, PaneID: "w2:p1", WorkspaceID: "w2"},
	} {
		if d := ds[i]; d.Unexpected != nil || d.Health != nil || d.Event != want {
			t.Errorf("delivery %d %+v, want event %+v", i, d, want)
		}
	}
	if d := ds[2]; d.Unexpected == nil || !errors.Is(d.Unexpected, errUnexpectedEvent) || d.Health != nil || d.Event != (core.LifecycleEvent{}) {
		t.Errorf("delivery 2 %+v, want the pane_focused with no pane id flagged unexpected", d)
	}
	if d := ds[4]; d.Health == nil || d.Health.Kind != HealthClosed {
		t.Errorf("last delivery %+v, want health closed", d)
	}
}

func TestCloseWhileUndrained(t *testing.T) {
	status := fixture(t, "event_pane_scoped.json")
	s := newFake(t, func(fc *fakeConn) {
		fc.reply(started)
		for range 3 {
			fc.send(status)
		}
		fc.expectClientClose()
	})
	sub, err := s.client().SubscribeStatus(testContext(t), "w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	// Nobody reads C; Close must still end the stream, close the connection and
	// close C.
	time.Sleep(20 * time.Millisecond)
	sub.Close()
	for _, d := range collect(t, sub) {
		if d.Health != nil {
			t.Errorf("health %v after Close", d.Health)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.clientClosed.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.clientClosed.Load() != 1 {
		t.Error("the fake did not see the client close the connection")
	}
}

func TestSubscriptionOutlivesSetupContext(t *testing.T) {
	status := fixture(t, "event_pane_scoped.json")
	sendNow := make(chan struct{})
	s := newFake(t, func(fc *fakeConn) {
		fc.reply(started)
		select {
		case <-sendNow:
			fc.send(status)
		case <-fc.srv.quit:
			return
		}
		fc.hold()
	})
	ctx, cancel := context.WithCancel(context.Background())
	sub, err := s.client().SubscribeStatus(ctx, "w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	cancel()
	time.Sleep(20 * time.Millisecond)
	close(sendNow)
	select {
	case d, ok := <-sub.C:
		if !ok || d.Health != nil || d.Event.Status != core.StatusIdle {
			t.Fatalf("delivery %+v (open %v, health %v), want the idle event", d.Event, ok, d.Health)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event after the setup context was cancelled")
	}
}
