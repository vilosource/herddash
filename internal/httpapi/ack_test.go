package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
)

// fakeAck is an Acknowledge that records every request and answers each
// item with a view of its key, seen as the request asks, or with err.
type fakeAck struct {
	mu    sync.Mutex
	reqs  []engine.AckRequest
	err   error
	block func(ctx context.Context)
}

func (f *fakeAck) Acknowledge(ctx context.Context, req engine.AckRequest) ([]core.AgentView, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	if f.block != nil {
		f.block(ctx)
	}
	if f.err != nil {
		return nil, f.err
	}
	views := make([]core.AgentView, len(req.Items))
	for i, it := range req.Items {
		views[i] = core.AgentView{Key: it.Key, Status: core.StatusDone, Class: core.ClassFinished, WaitingSince: it.WaitingSince, Seen: req.Seen}
	}
	return views, nil
}

func (f *fakeAck) calls() []engine.AckRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs
}

func newAckHandler(f *fakeAck, log *slog.Logger) http.Handler {
	v := testView()
	return New(Options{View: func() engine.View { return v }, Subscribe: noSubscribe, History: noHistory,
		Acknowledge: f.Acknowledge, Acknowledgements: noAcknowledgements, Logger: log})
}

const ackHost = "127.0.0.1:7318"

// sameOrigin is the headers the Board's fetch sends with a write (the browser
// adding Origin and Sec-Fetch-Site).
func sameOrigin() map[string]string {
	return map[string]string{
		"Origin":           "http://" + ackHost,
		"Sec-Fetch-Site":   "same-origin",
		"X-Herddash-Write": "1",
		"Content-Type":     "application/json",
	}
}

// post sends method on target with body and hdr; a header given as "" is
// removed.
func post(h http.Handler, method, target, host string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = host
	for k, v := range hdr {
		if v == "\x00" {
			r.Header.Del(k)
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// with returns sameOrigin's headers changed by kv pairs; "\x00" removes one.
func with(kv ...string) map[string]string {
	h := sameOrigin()
	for i := 0; i < len(kv); i += 2 {
		h[kv[i]] = kv[i+1]
	}
	return h
}

func bulkBody(items ...engine.AckItem) string {
	var b strings.Builder
	b.WriteString(`{"agents":[`)
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(it.Key)
		fmt.Fprintf(&b, `{"key":%s,"waiting_since":%d}`, k, it.WaitingSince)
	}
	b.WriteString(`]}`)
	return b.String()
}

// padTo pads a JSON object body with spaces before its closing brace to n
// bytes.
func padTo(body string, n int) string {
	return body[:len(body)-1] + strings.Repeat(" ", n-len(body)) + "}"
}

var (
	ackKey1 = core.AgentKey{Host: "mac", Server: "default", Epoch: 2, PaneID: "w1:p1"}
	ackKey2 = core.AgentKey{Host: "mac", Server: "a/b", Epoch: 2, PaneID: "w1:p2"}
)

func TestWriteValid(t *testing.T) {
	for _, c := range []struct {
		name, target, body string
		hdr                map[string]string
		want               engine.AckRequest
	}{
		{"per-Agent mark", "/api/agents/mac/default/2/w1:p1/seen", `{"waiting_since":1000}`, sameOrigin(),
			engine.AckRequest{Seen: true, Items: []engine.AckItem{{Key: ackKey1, WaitingSince: 1000}}}},
		{"per-Agent undo", "/api/agents/mac/default/2/w1:p1/unseen", `{"waiting_since":1000}`, sameOrigin(),
			engine.AckRequest{Items: []engine.AckItem{{Key: ackKey1, WaitingSince: 1000}}}},
		{"per-Agent escaped", "/api/agents/mac/a%2Fb/02/w1%3Ap2/seen", ` {"waiting_since": 7} `, sameOrigin(),
			engine.AckRequest{Seen: true, Items: []engine.AckItem{{Key: ackKey2, WaitingSince: 7}}}},
		{"bulk mark", "/api/seen", bulkBody(engine.AckItem{Key: ackKey2, WaitingSince: 9}, engine.AckItem{Key: ackKey1, WaitingSince: 8}), sameOrigin(),
			engine.AckRequest{Seen: true, Items: []engine.AckItem{{Key: ackKey2, WaitingSince: 9}, {Key: ackKey1, WaitingSince: 8}}}},
		{"bulk undo", "/api/unseen", bulkBody(engine.AckItem{Key: ackKey1, WaitingSince: 8}), sameOrigin(),
			engine.AckRequest{Items: []engine.AckItem{{Key: ackKey1, WaitingSince: 8}}}},
		{"charset and case", "/api/seen", bulkBody(engine.AckItem{Key: ackKey1, WaitingSince: 8}),
			with("Origin", "HTTP://127.0.0.1:7318", "Content-Type", "Application/JSON; Charset=UTF-8", "Sec-Fetch-Site", "\x00"),
			engine.AckRequest{Seen: true, Items: []engine.AckItem{{Key: ackKey1, WaitingSince: 8}}}},
		{"per-Agent at its bound", "/api/agents/mac/default/2/w1:p1/seen", padTo(`{"waiting_since":1000}`, maxAgentBody), sameOrigin(),
			engine.AckRequest{Seen: true, Items: []engine.AckItem{{Key: ackKey1, WaitingSince: 1000}}}},
		{"bulk at its bound", "/api/seen", padTo(bulkBody(engine.AckItem{Key: ackKey1, WaitingSince: 8}), maxBulkBody), sameOrigin(),
			engine.AckRequest{Seen: true, Items: []engine.AckItem{{Key: ackKey1, WaitingSince: 8}}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeAck{}
			w := post(newAckHandler(f, nil), http.MethodPost, c.target, ackHost, c.hdr, c.body)
			checkCommon(t, w)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, body %s", w.Code, w.Body)
			}
			if got := f.calls(); len(got) != 1 || !reflect.DeepEqual(got[0], c.want) {
				t.Fatalf("Acknowledge got %+v, want exactly [%+v]", got, c.want)
			}
			var body struct {
				Agents []struct {
					Key          core.AgentKey `json:"key"`
					WaitingSince int64         `json:"waiting_since"`
					Seen         bool          `json:"seen"`
				} `json:"agents"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("body %s: %v", w.Body, err)
			}
			if len(body.Agents) != len(c.want.Items) {
				t.Fatalf("body %s: want %d views", w.Body, len(c.want.Items))
			}
			for i, it := range c.want.Items {
				a := body.Agents[i]
				if a.Key != it.Key || a.WaitingSince != it.WaitingSince || a.Seen != c.want.Seen {
					t.Errorf("view %d %+v, want %+v seen %v (request order)", i, a, it, c.want.Seen)
				}
			}
		})
	}
}

func TestWriteRefused(t *testing.T) {
	const agent = "/api/agents/mac/default/2/w1:p1/seen"
	one := `{"waiting_since":1000}`
	bulk1 := bulkBody(engine.AckItem{Key: ackKey1, WaitingSince: 1000})
	many := make([]engine.AckItem, maxBulkItems+1)
	for i := range many {
		many[i] = engine.AckItem{Key: core.AgentKey{Host: "h", Server: "s", Epoch: 1, PaneID: fmt.Sprint("w1:p", i)}, WaitingSince: 1}
	}
	full := bulkBody(many[:maxBulkItems]...)
	if len(full) > maxBulkBody {
		t.Fatalf("256 items take %d bytes, over the bulk bound", len(full))
	}
	for _, c := range []struct {
		name, target, host, body string
		hdr                      map[string]string
		status                   int
		code                     string
	}{
		{"no Origin", agent, ackHost, one, with("Origin", "\x00"), 403, codeOriginNotAllowed},
		{"Origin null", agent, ackHost, one, with("Origin", "null"), 403, codeOriginNotAllowed},
		{"Origin evil", agent, ackHost, one, with("Origin", "http://evil.example"), 403, codeOriginNotAllowed},
		{"Origin other port", agent, ackHost, one, with("Origin", "http://127.0.0.1:7319"), 403, codeOriginNotAllowed},
		{"Origin https", agent, ackHost, one, with("Origin", "https://127.0.0.1:7318"), 403, codeOriginNotAllowed},
		{"Origin other loopback name", agent, ackHost, one, with("Origin", "http://localhost:7318"), 403, codeOriginNotAllowed},
		{"Origin with a path", agent, ackHost, one, with("Origin", "http://127.0.0.1:7318/"), 403, codeOriginNotAllowed},
		{"bulk no Origin", "/api/seen", ackHost, bulk1, with("Origin", "\x00"), 403, codeOriginNotAllowed},
		{"Sec-Fetch-Site cross-site", agent, ackHost, one, with("Sec-Fetch-Site", "cross-site"), 403, codeOriginNotAllowed},
		{"Sec-Fetch-Site same-site", agent, ackHost, one, with("Sec-Fetch-Site", "same-site"), 403, codeOriginNotAllowed},
		{"Sec-Fetch-Site none", agent, ackHost, one, with("Sec-Fetch-Site", "none"), 403, codeOriginNotAllowed},
		{"no X-Herddash-Write", agent, ackHost, one, with("X-Herddash-Write", "\x00"), 403, codeWriteHeaderMissing},
		{"X-Herddash-Write 0", agent, ackHost, one, with("X-Herddash-Write", "0"), 403, codeWriteHeaderMissing},
		{"Content-Type text/plain", agent, ackHost, one, with("Content-Type", "text/plain"), 415, codeUnsupportedMediaType},
		{"Content-Type form", agent, ackHost, one, with("Content-Type", "application/x-www-form-urlencoded"), 415, codeUnsupportedMediaType},
		{"Content-Type multipart", agent, ackHost, one, with("Content-Type", "multipart/form-data; boundary=x"), 415, codeUnsupportedMediaType},
		{"Content-Type missing", agent, ackHost, one, with("Content-Type", "\x00"), 415, codeUnsupportedMediaType},
		{"Content-Type other charset", agent, ackHost, one, with("Content-Type", "application/json; charset=latin1"), 415, codeUnsupportedMediaType},
		{"Content-Type other parameter", agent, ackHost, one, with("Content-Type", "application/json; x=1"), 415, codeUnsupportedMediaType},
		{"per-Agent one byte over", agent, ackHost, padTo(one, maxAgentBody+1), sameOrigin(), 413, codeBodyTooLarge},
		{"bulk one byte over", "/api/seen", ackHost, padTo(bulk1, maxBulkBody+1), sameOrigin(), 413, codeBodyTooLarge},
		{"unknown field", agent, ackHost, `{"waiting_since":1000,"all":true}`, sameOrigin(), 400, codeBadRequest},
		{"bulk unknown key field", "/api/seen", ackHost, `{"agents":[{"key":{"host":"mac","server":"default","epoch":2,"pane_id":"w1:p1","workspace":"x"},"waiting_since":1}]}`, sameOrigin(), 400, codeBadRequest},
		{"trailing data", agent, ackHost, one + ` {}`, sameOrigin(), 400, codeBadRequest},
		{"trailing garbage", agent, ackHost, one + `x`, sameOrigin(), 400, codeBadRequest},
		{"empty body", agent, ackHost, ``, sameOrigin(), 400, codeBadRequest},
		{"null body", agent, ackHost, `null`, sameOrigin(), 400, codeBadRequest},
		{"array body", "/api/seen", ackHost, `[]`, sameOrigin(), 400, codeBadRequest},
		{"waiting_since missing", agent, ackHost, `{}`, sameOrigin(), 400, codeBadRequest},
		{"waiting_since 0", agent, ackHost, `{"waiting_since":0}`, sameOrigin(), 400, codeBadRequest},
		{"waiting_since negative", agent, ackHost, `{"waiting_since":-5}`, sameOrigin(), 400, codeBadRequest},
		{"waiting_since fraction", agent, ackHost, `{"waiting_since":1.5}`, sameOrigin(), 400, codeBadRequest},
		{"waiting_since string", agent, ackHost, `{"waiting_since":"1000"}`, sameOrigin(), 400, codeBadRequest},
		{"0 bulk items", "/api/seen", ackHost, `{"agents":[]}`, sameOrigin(), 400, codeBadRequest},
		{"null bulk items", "/api/seen", ackHost, `{"agents":null}`, sameOrigin(), 400, codeBadRequest},
		{"257 bulk items", "/api/seen", ackHost, bulkBody(many...), sameOrigin(), 400, codeBadRequest},
		{"duplicate key", "/api/unseen", ackHost, bulkBody(engine.AckItem{Key: ackKey1, WaitingSince: 1}, engine.AckItem{Key: ackKey1, WaitingSince: 2}), sameOrigin(), 400, codeBadRequest},
		{"key missing", "/api/seen", ackHost, `{"agents":[{"waiting_since":1}]}`, sameOrigin(), 400, codeBadRequest},
		{"key epoch missing", "/api/seen", ackHost, `{"agents":[{"key":{"host":"mac","server":"default","pane_id":"w1:p1"},"waiting_since":1}]}`, sameOrigin(), 400, codeBadRequest},
		{"key epoch too large", "/api/seen", ackHost, `{"agents":[{"key":{"host":"mac","server":"default","epoch":9223372036854775808,"pane_id":"w1:p1"},"waiting_since":1}]}`, sameOrigin(), 400, codeBadRequest},
		{"key empty pane", "/api/seen", ackHost, `{"agents":[{"key":{"host":"mac","server":"default","epoch":2,"pane_id":""},"waiting_since":1}]}`, sameOrigin(), 400, codeBadRequest},
		{"bulk item waiting_since 0", "/api/seen", ackHost, bulkBody(engine.AckItem{Key: ackKey1, WaitingSince: 0}), sameOrigin(), 400, codeBadRequest},
		{"non-loopback Host, its Origin", agent, "evil.example:7318", one, with("Origin", "http://evil.example:7318"), 403, codeHostNotAllowed},
		{"bulk non-loopback Host, its Origin", "/api/seen", "evil.example", bulk1, with("Origin", "http://evil.example"), 403, codeHostNotAllowed},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeAck{}
			w := post(newAckHandler(f, nil), http.MethodPost, c.target, c.host, c.hdr, c.body)
			checkError(t, w, c.status, c.code)
			if got := f.calls(); len(got) != 0 {
				t.Errorf("Acknowledge called with %+v", got)
			}
		})
	}
	// The full 256 items pass the guard and reach the engine.
	f := &fakeAck{}
	if w := post(newAckHandler(f, nil), http.MethodPost, "/api/seen", ackHost, sameOrigin(), full); w.Code != http.StatusOK || len(f.calls()) != 1 || len(f.calls()[0].Items) != maxBulkItems {
		t.Errorf("256 items: status %d, body %s", w.Code, w.Body)
	}
}

// TestWriteGuardBeforeBody checks no guard refusal reads the body: a body
// that fails when read is never touched.
func TestWriteGuardBeforeBody(t *testing.T) {
	for _, hdr := range []map[string]string{
		with("Origin", "http://evil.example"), with("Sec-Fetch-Site", "cross-site"),
		with("X-Herddash-Write", "\x00"), with("Content-Type", "text/plain"),
	} {
		f := &fakeAck{}
		body := &trapBody{}
		r := httptest.NewRequest(http.MethodPost, "/api/seen", body)
		r.Host = ackHost
		for k, v := range hdr {
			if v == "\x00" {
				r.Header.Del(k)
			} else {
				r.Header.Set(k, v)
			}
		}
		w := httptest.NewRecorder()
		newAckHandler(f, nil).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden && w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%v: status %d", hdr, w.Code)
		}
		if body.read {
			t.Errorf("%v: the body was read before the guard refused it", hdr)
		}
	}
}

type trapBody struct{ read bool }

func (b *trapBody) Read([]byte) (int, error) { b.read = true; return 0, errors.New("read") }

func TestWriteEngineErrors(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
		in     string // the message must hold it
	}{
		{fmt.Errorf("%w: w1:p1", engine.ErrUnknownAgent), 404, codeUnknownAgent, "w1:p1"},
		{fmt.Errorf("%w: w1:p1", engine.ErrNotFinished), 409, codeNotFinished, "w1:p1"},
		{&engine.StaleError{Keys: []core.AgentKey{ackKey1, ackKey2}}, 409, codeStaleView, "mac/default/2/w1:p1, mac/a%2Fb/2/w1:p2"},
		{engine.ErrRecordingOff, 503, codeRecordingOff, "not recording"},
		{fmt.Errorf("%w: w1:p1 twice", engine.ErrBadRequest), 400, codeBadRequest, "twice"},
		{errors.New("engine: acknowledgement not recorded: disk on fire"), 500, codeStoreFailed, "could not be recorded"},
		{engine.ErrStopped, 500, codeStoreFailed, "could not be recorded"},
	} {
		for _, target := range []string{"/api/agents/mac/default/2/w1:p1/seen", "/api/unseen"} {
			f := &fakeAck{err: c.err}
			var logs bytes.Buffer
			body := `{"waiting_since":1000}`
			if target == "/api/unseen" {
				body = bulkBody(engine.AckItem{Key: ackKey1, WaitingSince: 1000})
			}
			w := post(newAckHandler(f, slog.New(slog.NewTextHandler(&logs, nil))), http.MethodPost, target, ackHost, sameOrigin(), body)
			checkError(t, w, c.status, c.code)
			if msg := errorMessage(t, w); !strings.Contains(msg, c.in) {
				t.Errorf("%v on %s: message %q, want it to hold %q", c.err, target, msg, c.in)
			}
			if strings.Contains(w.Body.String(), "disk on fire") {
				t.Errorf("the body leaks the store's error: %s", w.Body)
			}
			if c.status == 500 && !strings.Contains(logs.String(), "level=ERROR") {
				t.Errorf("%v: not logged at error level: %q", c.err, logs.String())
			}
			if len(f.calls()) != 1 {
				t.Errorf("Acknowledge called %d times, want 1", len(f.calls()))
			}
		}
	}
}

// TestWriteCancelled ends the request context while Acknowledge runs: no
// response body is written, whatever it returns.
func TestWriteCancelled(t *testing.T) {
	for _, err := range []error{nil, context.Canceled, engine.ErrRecordingOff} {
		ctx, cancel := context.WithCancel(context.Background())
		f := &fakeAck{err: err, block: func(context.Context) { cancel() }}
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/seen", strings.NewReader(bulkBody(engine.AckItem{Key: ackKey1, WaitingSince: 1})))
		r.Host = ackHost
		for k, v := range sameOrigin() {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		newAckHandler(f, nil).ServeHTTP(w, r)
		if len(f.calls()) != 1 {
			t.Fatalf("%v: Acknowledge called %d times", err, len(f.calls()))
		}
		if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
			t.Errorf("%v: a cancelled request got a response: %d %q", err, w.Code, w.Body)
		}
	}
}

// TestWriteMethods checks every method on every route: a write route takes
// only POST (405, Allow: POST, for the rest, OPTIONS included); a read route
// keeps its own 405 and Allow for POST, OPTIONS and the rest. The guard's
// headers do not change that, and none of these calls the engine.
func TestWriteMethods(t *testing.T) {
	writes := []string{"/api/seen", "/api/unseen", "/api/agents/mac/default/2/w1:p1/seen", "/api/agents/mac/default/2/w1:p1/unseen"}
	reads := map[string]string{
		"/": allowGetHead, "/assets/board.css": allowGetHead, "/api/state": allowGetHead,
		"/api/agents/mac/default/2/w1:p1/history": allowGetHead, "/events": allowGet,
	}
	others := []string{http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions}
	f := &fakeAck{}
	h := newAckHandler(f, nil)
	for _, p := range writes {
		for _, m := range append([]string{http.MethodGet, http.MethodHead}, others...) {
			w := post(h, m, p, ackHost, sameOrigin(), `{"waiting_since":1}`)
			if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != allowPost {
				t.Errorf("%s %s: status %d, Allow %q; want 405 and POST", m, p, w.Code, w.Header().Get("Allow"))
			}
			checkCommon(t, w)
			if m == http.MethodHead && w.Body.Len() != 0 {
				t.Errorf("HEAD %s: body %q", p, w.Body)
			}
		}
	}
	for p, allow := range reads {
		for _, m := range append([]string{http.MethodPost}, others...) {
			w := post(h, m, p, ackHost, sameOrigin(), `{"waiting_since":1}`)
			checkError(t, w, http.StatusMethodNotAllowed, codeMethodNotAllowed)
			if got := w.Header().Get("Allow"); got != allow {
				t.Errorf("%s %s: Allow %q, want %q", m, p, got, allow)
			}
		}
	}
	// A preflight, as a cross-site page's browser sends it, is a 405 with no
	// CORS header anywhere.
	pre := map[string]string{"Origin": "http://evil.example", "Access-Control-Request-Method": "POST",
		"Access-Control-Request-Headers": "content-type, x-herddash-write"}
	for _, p := range writes {
		w := post(h, http.MethodOptions, p, ackHost, pre, "")
		checkError(t, w, http.StatusMethodNotAllowed, codeMethodNotAllowed)
	}
	if len(f.calls()) != 0 {
		t.Errorf("Acknowledge called with %+v", f.calls())
	}
}

// TestWriteUnknownPath checks a malformed write path is a 404, the history
// path's own rules, and never reaches the engine.
func TestWriteUnknownPath(t *testing.T) {
	f := &fakeAck{}
	h := newAckHandler(f, nil)
	for _, p := range []string{
		"/api/seen/", "/api/unseen/x", "/api/Seen", "/api/agents/mac/default/x/w1:p1/seen",
		"/api/agents/mac/default/2/w1:p1/seen/", "/api/agents/mac/default/2//seen", "/api/agents/mac/default/2/w1:p1/Seen",
		"/api/agents/mac/default/-1/w1:p1/unseen", "/api/agents/mac/2/w1:p1/seen",
	} {
		w := post(h, http.MethodPost, p, ackHost, sameOrigin(), `{"waiting_since":1}`)
		checkError(t, w, http.StatusNotFound, codeNotFound)
	}
	if len(f.calls()) != 0 {
		t.Errorf("Acknowledge called with %+v", f.calls())
	}
}

func TestNewNilAcknowledgePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "Acknowledge is nil") {
			t.Errorf("recovered %v, want a panic naming the nil Acknowledge", r)
		}
	}()
	New(Options{View: testView, Subscribe: noSubscribe, History: noHistory, Acknowledgements: noAcknowledgements})
}

func TestNewNilAcknowledgementsPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "Acknowledgements is nil") {
			t.Errorf("recovered %v, want a panic naming the nil Acknowledgements", r)
		}
	}()
	New(Options{View: testView, Subscribe: noSubscribe, History: noHistory, Acknowledge: noAcknowledge})
}
