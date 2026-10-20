package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
)

// testView is a published View with three Agents in Board order: one needing
// the operator, one finished, one working in a workspace group.
func testView() engine.View {
	key := func(pane string) core.AgentKey {
		return core.AgentKey{Host: "h", Server: "s", Epoch: 1, PaneID: pane}
	}
	agents := []core.AgentView{
		{
			Key: key("w2:p1"), AgentProgram: "claude", Profile: "work", Heading: "work · claude",
			Directory: "~/src/b", Workspace: "two", Tab: "1", Status: core.StatusWorking, Class: core.ClassWorking,
			WaitingSince: 3000, WaitingExact: true,
			SortKey: core.SortKey{Zone: 1, Group: 1, ClassRank: 0, Start: 3000, Key: key("w2:p1")},
		},
		{
			Key: key("w1:p1"), AgentProgram: "claude", Profile: "claude", Heading: "claude",
			Directory: "~/src/a", Workspace: "one", Tab: "1", Status: core.StatusBlocked, Class: core.ClassNeedsYou,
			WaitingSince: 1000, WaitingExact: true, PaneTitle: "fix the parser", ContextLine: "fix the parser",
			SortKey: core.SortKey{Zone: 0, Group: 0, ClassRank: 0, Start: 1000, Key: key("w1:p1")},
		},
		{
			Key: key("w1:p2"), AgentProgram: "codex", Profile: "unresolved", Heading: "unresolved · codex",
			Directory: "~/src/a", Workspace: "one", Tab: "2", Status: core.StatusDone, Class: core.ClassFinished,
			WaitingSince: 2000,
			SortKey:      core.SortKey{Zone: 0, Group: 0, ClassRank: 1, Start: 2000, Key: key("w1:p2")},
		},
	}
	core.SortViews(agents)
	return engine.View{
		RunID:  "0123456789abcdef0123456789abcdef",
		Seq:    7,
		Agents: agents,
		Connection: core.ConnectionTruth{
			Connected: true, LastSnapshotAt: 4000, HerdrVersion: "0.9.5",
			HerdrMinimum: core.HerdrMinimum, Gate: core.GateOK, ResyncGuaranteed: true,
		},
		Headers: core.WorkspaceHeaders(agents),
	}
}

func newHandler(t *testing.T) (http.Handler, engine.View) {
	t.Helper()
	v := testView()
	return New(Options{View: func() engine.View { return v }, Subscribe: noSubscribe, History: noHistory}), v
}

func do(h http.Handler, method, target, host string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = host
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// checkCommon asserts the headers every response carries, and that none is a
// CORS header.
func checkCommon(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type %q, want application/json", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options %q, want nosniff", got)
	}
	for k := range w.Header() {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Errorf("CORS header %s sent", k)
		}
	}
}

func checkError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	checkCommon(t, w)
	if w.Code != status {
		t.Errorf("status %d, want %d", w.Code, status)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("error body %q is not the error shape: %v", w.Body.String(), err)
	}
	if body.Error.Code != code || body.Error.Message == "" {
		t.Errorf("error %+v, want code %q and a message", body.Error, code)
	}
}

func TestState(t *testing.T) {
	h, v := newHandler(t)
	w := do(h, http.MethodGet, "/api/state", "127.0.0.1:7318", nil)
	checkCommon(t, w)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body)
	}
	want, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if !bytes.Equal(w.Body.Bytes(), want) {
		t.Errorf("body\n%s\nwant\n%s", w.Body, want)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"run_id", "seq", "agents", "headers", "connection"} {
		if _, ok := got[f]; !ok {
			t.Errorf("field %q missing from %s", f, w.Body)
		}
	}
	// SortKey encodes as an array with no decoder; read only the keys.
	var decoded struct {
		Agents []struct {
			Key core.AgentKey `json:"key"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	order := []string{"w1:p1", "w1:p2", "w2:p1"}
	if len(decoded.Agents) != len(order) {
		t.Fatalf("%d agents, want %d", len(decoded.Agents), len(order))
	}
	for i, p := range order {
		if decoded.Agents[i].Key.PaneID != p {
			t.Errorf("agent %d is %s, want %s (sort order)", i, decoded.Agents[i].Key.PaneID, p)
		}
	}
}

// TestStateReadsEachRequest checks the handler reads the latest View on every
// request rather than caching one, through a fake View func alone: Options has
// no herdr client or store, so no request can reach either.
func TestStateReadsEachRequest(t *testing.T) {
	var seq uint64
	h := New(Options{View: func() engine.View { seq++; return engine.View{Seq: seq} }, Subscribe: noSubscribe, History: noHistory})
	for want := uint64(1); want <= 2; want++ {
		w := do(h, http.MethodGet, "/api/state", "localhost", nil)
		var v struct {
			Seq uint64 `json:"seq"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v.Seq != want {
			t.Errorf("seq %d (%v), want %d", v.Seq, err, want)
		}
	}
}

func TestHosts(t *testing.T) {
	h, _ := newHandler(t)
	for _, host := range []string{"localhost", "localhost:9", "[::1]:7318", "[::1]", "127.0.0.1", "LOCALHOST:7318"} {
		w := do(h, http.MethodGet, "/api/state", host, nil)
		checkCommon(t, w)
		if w.Code != http.StatusOK {
			t.Errorf("Host %q: status %d, want 200", host, w.Code)
		}
	}
}

func TestBadHost(t *testing.T) {
	h, _ := newHandler(t)
	for _, host := range []string{
		"evil.example", "127.0.0.1.evil.example", "", "evil.example:7318", "localhost.evil.example",
		"127.0.0.2", "[::2]:7318", "[::1", "localhost:", "localhost:x", "localhost:7318:1", "0.0.0.0",
	} {
		t.Run(host, func(t *testing.T) {
			// Host is checked first: also on unknown paths and other methods.
			for _, r := range [][2]string{{http.MethodGet, "/api/state"}, {http.MethodGet, "/nope"}, {http.MethodPost, "/api/state"}} {
				checkError(t, do(h, r[0], r[1], host, nil), http.StatusForbidden, "host_not_allowed")
			}
		})
	}
}

func TestMethod(t *testing.T) {
	h, _ := newHandler(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		w := do(h, m, "/api/state", "127.0.0.1:7318", nil)
		checkError(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
		if got := w.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s: Allow %q, want GET, HEAD", m, got)
		}
	}
}

func TestHead(t *testing.T) {
	h, _ := newHandler(t)
	w := do(h, http.MethodHead, "/api/state", "127.0.0.1:7318", nil)
	checkCommon(t, w)
	if w.Code != http.StatusOK {
		t.Errorf("status %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("HEAD body %q, want none", w.Body)
	}
}

func TestUnknown(t *testing.T) {
	h, _ := newHandler(t)
	for _, p := range []string{"/nope", "/api/state/x", "/api/state/", "/api", "/assets/", "/events/", "/api/events",
		"/api/agents/h/s/1/w1:p1/history"} {
		checkError(t, do(h, http.MethodGet, p, "127.0.0.1:7318", nil), http.StatusNotFound, "not_found")
	}
}

func TestNoCORS(t *testing.T) {
	h, _ := newHandler(t)
	hdr := map[string]string{
		"Origin":                         "http://evil.example",
		"Access-Control-Request-Method":  "GET",
		"Access-Control-Request-Headers": "content-type",
	}
	for _, r := range []struct{ method, path, host string }{
		{http.MethodGet, "/api/state", "127.0.0.1:7318"},
		{http.MethodOptions, "/api/state", "127.0.0.1:7318"},
		{http.MethodGet, "/nope", "localhost"},
		{http.MethodGet, "/api/state", "evil.example"},
	} {
		checkCommon(t, do(h, r.method, r.path, r.host, hdr))
	}
	w := do(h, http.MethodOptions, "/api/state", "127.0.0.1:7318", hdr)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("preflight status %d, want 405", w.Code)
	}
}

func TestNewNilViewPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "View is nil") {
			t.Errorf("recovered %v, want a panic naming the nil View", r)
		}
	}()
	New(Options{Subscribe: noSubscribe, History: noHistory})
}

func TestNewNilSubscribePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "Subscribe is nil") {
			t.Errorf("recovered %v, want a panic naming the nil Subscribe", r)
		}
	}()
	New(Options{View: func() engine.View { return engine.View{} }, History: noHistory})
}

// noSubscribe is a Subscribe whose subscription never delivers anything.
func noSubscribe(int) (*engine.ViewSubscription, func()) {
	return &engine.ViewSubscription{C: make(chan engine.Publication)}, func() {}
}
