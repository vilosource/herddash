package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
)

// noHistory is a History with no rows for any key.
func noHistory(context.Context, core.AgentKey) ([]core.Transition, error) { return nil, nil }

// fakeHistory serves fixed rows per key and records every key it is asked for.
type fakeHistory struct {
	rows  map[core.AgentKey][]core.Transition
	err   error
	calls []core.AgentKey
}

func (f *fakeHistory) History(_ context.Context, key core.AgentKey) ([]core.Transition, error) {
	f.calls = append(f.calls, key)
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[key], nil
}

func mustTransition(t *testing.T, in core.TransitionInput) core.Transition {
	t.Helper()
	tr, err := core.NewTransition(in)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func statusPtr(s core.Status) *core.Status { return &s }

var (
	liveKey     = core.AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"}
	escapedKey  = core.AgentKey{Host: "mac", Server: "a/b", Epoch: 2, PaneID: "w1:p1"}
	backfillKey = core.AgentKey{Host: "mac", Server: "default", Epoch: core.BackfillEpoch, PaneID: "w3:p2"}
)

// historyFixture holds three rows for liveKey (the second observed by Claude
// Code's session record, the others by herdr), one for escapedKey and two
// backfill rows for backfillKey.
func historyFixture(t *testing.T) *fakeHistory {
	order := uint64(41)
	return &fakeHistory{rows: map[core.AgentKey][]core.Transition{
		liveKey: {
			mustTransition(t, core.TransitionInput{Key: liveKey, AgentProgram: "claude", Profile: "work", New: core.StatusWorking,
				HerdrOrder: &order, HerdrVersion: "0.9.5", ObservedAt: 1000, Provenance: core.ProvenanceSnapshot}),
			mustTransition(t, core.TransitionInput{Key: liveKey, AgentProgram: "claude", Profile: "work", Previous: statusPtr(core.StatusWorking),
				New: core.StatusBlocked, HerdrVersion: "0.9.5", ObservedAt: 2000, Provenance: core.ProvenanceLive,
				ObservedBy: core.ObservedByClaudeSession}),
			mustTransition(t, core.TransitionInput{Key: liveKey, AgentProgram: "claude", Profile: "work", Previous: statusPtr(core.StatusBlocked),
				New: core.StatusWorking, HerdrVersion: "0.9.5", ObservedAt: 3000, Provenance: core.ProvenanceLive}),
		},
		escapedKey: {
			mustTransition(t, core.TransitionInput{Key: escapedKey, AgentProgram: "codex", Profile: "unresolved", New: core.StatusIdle,
				ObservedAt: 500, Provenance: core.ProvenanceSnapshot}),
		},
		backfillKey: {
			mustTransition(t, core.TransitionInput{Key: backfillKey, AgentProgram: "claude", Profile: "home", New: core.StatusWorking,
				ObservedAt: 100, Provenance: core.ProvenanceBackfill, ImportKey: "a"}),
			mustTransition(t, core.TransitionInput{Key: backfillKey, AgentProgram: "claude", Profile: "home", New: core.StatusDone,
				ObservedAt: 200, Provenance: core.ProvenanceBackfill, ImportKey: "b", PossiblySpurious: true}),
		},
	}}
}

func newHistoryHandler(f *fakeHistory, log *slog.Logger) http.Handler {
	v := testView()
	return New(Options{View: func() engine.View { return v }, Subscribe: noSubscribe, History: f.History, Logger: log})
}

// errorMessage returns the message of a JSON error body.
func errorMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Error struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q is not the JSON error shape: %v", w.Body, err)
	}
	return e.Error.Message
}

func TestHistoryFound(t *testing.T) {
	f := historyFixture(t)
	h := newHistoryHandler(f, nil)
	w := do(h, http.MethodGet, "/api/agents/mac/default/2/w1:p1/history", "localhost:7318", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body)
	}
	checkCommon(t, w)
	if len(f.calls) != 1 || f.calls[0] != liveKey {
		t.Errorf("History called with %v, want [%v]", f.calls, liveKey)
	}
	want := `{"key":{"host":"mac","server":"default","epoch":2,"pane_id":"w1:p1"},"transitions":[` +
		`{"agent_program":"claude","profile":"work","new":"working","herdr_order":41,"herdr_version":"0.9.5","observed_at":1000,"provenance":"snapshot","possibly_spurious":false},` +
		`{"agent_program":"claude","profile":"work","previous":"working","new":"blocked","herdr_version":"0.9.5","observed_at":2000,"provenance":"live","possibly_spurious":false,"observed_by":"claude_session"},` +
		`{"agent_program":"claude","profile":"work","previous":"blocked","new":"working","herdr_version":"0.9.5","observed_at":3000,"provenance":"live","possibly_spurious":false}]}`
	if got := w.Body.String(); got != want {
		t.Errorf("body\n%s\nwant\n%s", got, want)
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(want)) {
		t.Errorf("Content-Length %q, want %d", got, len(want))
	}
}

func TestHistoryEscaped(t *testing.T) {
	for _, target := range []string{
		"/api/agents/mac/a%2Fb/2/w1%3Ap1/history",
		"/api/agents/mac/a%2fb/2/w1:p1/history",
		"/api/agents/m%61c/a%2Fb/02/w1%3ap1/histor%79",
	} {
		f := historyFixture(t)
		w := do(newHistoryHandler(f, nil), http.MethodGet, target, "localhost", nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d, body %s", target, w.Code, w.Body)
			continue
		}
		if len(f.calls) != 1 || f.calls[0] != escapedKey {
			t.Errorf("%s: History called with %v, want [%v]", target, f.calls, escapedKey)
		}
		if !strings.Contains(w.Body.String(), `"server":"a/b"`) || !strings.Contains(w.Body.String(), `"profile":"unresolved"`) {
			t.Errorf("%s: body %s", target, w.Body)
		}
	}
}

// TestHistoryEscapedMixed puts a %2F in one segment and, in another, a byte
// that url.EscapedPath would re-escape (a raw non-ASCII byte, or a raw "),
// which makes RawPath non-canonical: the %2F must still stay in its segment.
func TestHistoryEscapedMixed(t *testing.T) {
	for _, c := range []struct {
		target string
		key    core.AgentKey
	}{
		{"/api/agents/mé/a%2Fb/2/w1:p1/history", core.AgentKey{Host: "mé", Server: "a/b", Epoch: 2, PaneID: "w1:p1"}},
		{"/api/agents/mac/a%2Fb/2/w1\"p1/history", core.AgentKey{Host: "mac", Server: "a/b", Epoch: 2, PaneID: "w1\"p1"}},
		{"/api/agents/m%C3%A9/a%2Fb/2/w1\"p1/history", core.AgentKey{Host: "mé", Server: "a/b", Epoch: 2, PaneID: "w1\"p1"}},
	} {
		f := historyFixture(t)
		f.rows[c.key] = f.rows[escapedKey]
		w := do(newHistoryHandler(f, nil), http.MethodGet, c.target, "localhost", nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d, body %s", c.target, w.Code, w.Body)
		}
		if len(f.calls) != 1 || f.calls[0] != c.key {
			t.Errorf("%s: History called with %+v, want [%+v]", c.target, f.calls, c.key)
		}
	}
}

// TestHistoryOrder serves rows in an order no sort would produce (equal
// observed_at in a given order, then an earlier row after a later one): the
// response keeps History's order exactly.
func TestHistoryOrder(t *testing.T) {
	in := func(prev *core.Status, next core.Status, at int64) core.Transition {
		return mustTransition(t, core.TransitionInput{Key: liveKey, AgentProgram: "claude", Profile: "work",
			Previous: prev, New: next, ObservedAt: at, Provenance: core.ProvenanceLive})
	}
	f := &fakeHistory{rows: map[core.AgentKey][]core.Transition{liveKey: {
		in(nil, core.StatusWorking, 500),
		in(statusPtr(core.StatusWorking), core.StatusIdle, 500),
		in(statusPtr(core.StatusIdle), core.StatusBlocked, 900),
		in(statusPtr(core.StatusBlocked), core.StatusDone, 100),
	}}}
	w := do(newHistoryHandler(f, nil), http.MethodGet, "/api/agents/mac/default/2/w1:p1/history", "localhost", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body)
	}
	var got struct {
		Transitions []struct {
			New        string `json:"new"`
			ObservedAt int64  `json:"observed_at"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, tr := range got.Transitions {
		order = append(order, tr.New+"@"+strconv.FormatInt(tr.ObservedAt, 10))
	}
	if g, want := strings.Join(order, " "), "working@500 idle@500 blocked@900 done@100"; g != want {
		t.Errorf("order %q, want %q", g, want)
	}
}

func TestHistoryUnknown(t *testing.T) {
	f := historyFixture(t)
	w := do(newHistoryHandler(f, nil), http.MethodGet, "/api/agents/mac/default/2/w9:p9/history", "localhost", nil)
	checkError(t, w, http.StatusNotFound, codeNotFound)
	if msg := errorMessage(t, w); msg != "no history for mac/default/2/w9:p9 (never recorded, or removed by Retention)" {
		t.Errorf("message %q", msg)
	}
	if len(f.calls) != 1 {
		t.Errorf("History called %d times, want 1", len(f.calls))
	}
}

func TestHistoryMalformed(t *testing.T) {
	for _, target := range []string{
		// Bad epoch.
		"/api/agents/mac/default/x/w1:p1/history",
		"/api/agents/mac/default/-1/w1:p1/history",
		"/api/agents/mac/default/+1/w1:p1/history",
		"/api/agents/mac/default//w1:p1/history",
		"/api/agents/mac/default/1.0/w1:p1/history",
		"/api/agents/mac/default/%201/w1:p1/history",
		"/api/agents/mac/default/9223372036854775808/w1:p1/history",
		// Shape.
		"/api/agents/",
		"/api/agents",
		"/api/agents/mac/default/2/w1:p1",
		"/api/agents/mac/default/2/w1:p1/",
		"/api/agents/mac/default/2/w1:p1/history/",
		"/api/agents/mac/default/2/w1:p1/history/x",
		"/api/agents/mac/2/w1:p1/history",
		"/api/agents/x/mac/default/2/w1:p1/history",
		"/api/agents/mac/default/2/w1:p1/events",
		"/api/agents/mac/default/2/w1:p1/History",
		// Empty segments.
		"/api/agents//default/2/w1:p1/history",
		"/api/agents/mac//2/w1:p1/history",
		"/api/agents/mac/default/2//history",
	} {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			f := historyFixture(t)
			w := do(newHistoryHandler(f, nil), m, target, "localhost", nil)
			checkError(t, w, http.StatusNotFound, codeNotFound)
			if len(f.calls) != 0 {
				t.Errorf("%s %s: History called with %v", m, target, f.calls)
			}
		}
	}
}

func TestHistoryBackfill(t *testing.T) {
	f := historyFixture(t)
	w := do(newHistoryHandler(f, nil), http.MethodGet, "/api/agents/mac/default/0/w3:p2/history", "localhost", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body)
	}
	var got struct {
		Key         core.AgentKey `json:"key"`
		Transitions []struct {
			New              string `json:"new"`
			Provenance       string `json:"provenance"`
			PossiblySpurious bool   `json:"possibly_spurious"`
			ImportKey        any    `json:"import_key"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Key != backfillKey || len(got.Transitions) != 2 {
		t.Fatalf("got %+v", got)
	}
	tr := got.Transitions
	if tr[0].New != "working" || tr[1].New != "done" || tr[0].Provenance != "backfill" || tr[0].PossiblySpurious || !tr[1].PossiblySpurious {
		t.Errorf("transitions %+v", tr)
	}
	if strings.Contains(w.Body.String(), "import_key") {
		t.Errorf("body sends the import key: %s", w.Body)
	}
}

func TestHistoryStoreError(t *testing.T) {
	f := historyFixture(t)
	f.err = errors.New("store: history of w1:p1: disk on fire")
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	w := do(newHistoryHandler(f, log), http.MethodGet, "/api/agents/mac/default/2/w1:p1/history", "localhost", nil)
	checkError(t, w, http.StatusInternalServerError, codeInternal)
	if strings.Contains(w.Body.String(), "disk on fire") {
		t.Errorf("body leaks the error: %s", w.Body)
	}
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "disk on fire") {
		t.Errorf("log %q, want the error at error level", logs.String())
	}
}

// TestHistoryCancelled ends the request context while History runs: whatever
// History returns (the context error, rows, no rows, another error), nothing
// is written and nothing is logged at error level.
func TestHistoryCancelled(t *testing.T) {
	rows := historyFixture(t).rows[liveKey]
	for _, c := range []struct {
		name string
		ret  func(ctx context.Context) ([]core.Transition, error)
	}{
		{"context error", func(ctx context.Context) ([]core.Transition, error) { return nil, ctx.Err() }},
		{"rows", func(context.Context) ([]core.Transition, error) { return rows, nil }},
		{"no rows", func(context.Context) ([]core.Transition, error) { return nil, nil }},
		{"other error", func(context.Context) ([]core.Transition, error) { return nil, errors.New("disk on fire") }},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		called := false
		hist := func(ctx context.Context, _ core.AgentKey) ([]core.Transition, error) {
			called = true
			cancel()
			return c.ret(ctx)
		}
		var logs bytes.Buffer
		h := New(Options{View: testView, Subscribe: noSubscribe, History: hist, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/agents/mac/default/2/w1:p1/history", nil)
		r.Host = "localhost"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if !called {
			t.Fatalf("%s: History not called", c.name)
		}
		if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" || w.Header().Get("Content-Length") != "" {
			t.Errorf("%s: a cancelled request got a response: %d %q", c.name, w.Code, w.Body)
		}
		if strings.Contains(logs.String(), "level=ERROR") {
			t.Errorf("%s: a cancelled request logged %q", c.name, logs.String())
		}
	}
}

func TestHistoryRules(t *testing.T) {
	const target = "/api/agents/mac/default/2/w1:p1/history"
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		f := historyFixture(t)
		w := do(newHistoryHandler(f, nil), m, target, "localhost", nil)
		checkError(t, w, http.StatusMethodNotAllowed, codeMethodNotAllowed)
		if got := w.Header().Get("Allow"); got != allowGetHead {
			t.Errorf("%s: Allow %q, want %q", m, got, allowGetHead)
		}
		if len(f.calls) != 0 {
			t.Errorf("%s: History called", m)
		}
	}
	for _, host := range []string{"evil.example", "", "localhost.evil.example:7318"} {
		f := historyFixture(t)
		w := do(newHistoryHandler(f, nil), http.MethodGet, target, host, nil)
		checkError(t, w, http.StatusForbidden, codeHostNotAllowed)
		if len(f.calls) != 0 {
			t.Errorf("Host %q: History called", host)
		}
	}
}

func TestHistoryHead(t *testing.T) {
	f := historyFixture(t)
	h := newHistoryHandler(f, nil)
	get := do(h, http.MethodGet, "/api/agents/mac/default/2/w1:p1/history", "localhost", nil)
	w := do(h, http.MethodHead, "/api/agents/mac/default/2/w1:p1/history", "localhost", nil)
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Errorf("HEAD: status %d, body %q", w.Code, w.Body)
	}
	checkCommon(t, w)
	if got, want := w.Header().Get("Content-Length"), strconv.Itoa(get.Body.Len()); got != want {
		t.Errorf("HEAD Content-Length %q, want the GET body length %s", got, want)
	}

	f = historyFixture(t)
	w = do(newHistoryHandler(f, nil), http.MethodHead, "/api/agents/mac/default/2/w9:p9/history", "localhost", nil)
	checkCommon(t, w)
	if w.Code != http.StatusNotFound || w.Body.Len() != 0 {
		t.Errorf("HEAD unknown: status %d, body %q; want 404 and no body", w.Code, w.Body)
	}
	if len(f.calls) != 1 {
		t.Errorf("HEAD unknown: History called %d times, want 1", len(f.calls))
	}
	get = do(newHistoryHandler(historyFixture(t), nil), http.MethodGet, "/api/agents/mac/default/2/w9:p9/history", "localhost", nil)
	checkError(t, get, http.StatusNotFound, codeNotFound)
	if got, want := w.Header().Get("Content-Length"), strconv.Itoa(get.Body.Len()); got != want {
		t.Errorf("HEAD unknown Content-Length %q, want the GET body length %s", got, want)
	}
}

func TestNewNilHistoryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "History is nil") {
			t.Errorf("recovered %v, want a panic naming the nil History", r)
		}
	}()
	New(Options{View: func() engine.View { return engine.View{} }, Subscribe: noSubscribe})
}
