package httpapi

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
)

// boundsServer serves h with no server-wide timeouts, as the Daemon's server
// has no WriteTimeout, and reports when a connection is closed and how many
// handler calls returned.
func boundsServer(t *testing.T, h http.Handler) (srv *httptest.Server, closed chan struct{}, returned *atomic.Int32) {
	t.Helper()
	returned = new(atomic.Int32)
	closed = make(chan struct{}, 16)
	srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer returned.Add(1)
		h.ServeHTTP(w, r)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateClosed {
			closed <- struct{}{}
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, closed, returned
}

// dialRaw opens a connection to srv that the test drives by hand.
func dialRaw(t *testing.T, srv *httptest.Server) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// waitClosed asserts the server closes a connection within d.
func waitClosed(t *testing.T, closed chan struct{}, d time.Duration) {
	t.Helper()
	select {
	case <-closed:
	case <-time.After(d):
		t.Fatalf("the server still holds a connection whose client never reads, after %v", d)
	}
}

// bigView is a View whose /api/state body is far larger than the socket
// buffers between a client and the server.
func bigView() engine.View {
	v := testView()
	line := strings.Repeat("x", 200)
	agents := make([]core.AgentView, 0, 40000)
	for i := range 40000 {
		a := v.Agents[0]
		a.Key.PaneID = fmt.Sprintf("w%d:p1", i)
		a.SortKey.Key = a.Key
		a.ContextLine = line
		agents = append(agents, a)
	}
	v.Agents = agents
	v.Headers = nil
	return v
}

// TestStateStalledReader asks for a large /api/state and never reads it: the
// write fails at the deadline, the handler returns and the connection closes.
func TestStateStalledReader(t *testing.T) {
	v := bigView()
	h := New(Options{View: func() engine.View { return v }, Subscribe: noSubscribe, History: noHistory, Acknowledge: noAcknowledge, Acknowledgements: noAcknowledgements})
	h.writeTimeout = 50 * time.Millisecond
	srv, closed, returned := boundsServer(t, h)
	conn := dialRaw(t, srv)
	if _, err := conn.Write([]byte("GET /api/state HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, closed, 5*time.Second)
	if n := returned.Load(); n != 1 {
		t.Errorf("handler returns: %d, want 1", n)
	}
}

// TestErrorReplyStalledReader pipelines many refused requests, or requests for
// one of the Board's assets, on one connection and never reads a reply. Each reply is short, written to the
// socket by the server only after the handler returns, still inside the
// deadline ServeHTTP set; once the socket fills, that write fails and the
// connection closes.
func TestErrorReplyStalledReader(t *testing.T) {
	for _, c := range []struct{ name, req string }{
		{"404", "GET /nothing-here HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"},
		{"405", "DELETE /api/state HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"},
		{"403", "GET /api/state HTTP/1.1\r\nHost: evil.example\r\n\r\n"},
		{"asset", "GET /assets/board.css HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := New(Options{View: testView, Subscribe: noSubscribe, History: noHistory, Acknowledge: noAcknowledge, Acknowledgements: noAcknowledgements})
			h.writeTimeout = 50 * time.Millisecond
			srv, closed, _ := boundsServer(t, h)
			conn := dialRaw(t, srv)
			// Far more replies than the socket buffers hold. The writes run on
			// their own goroutine, since the server stops reading once its
			// replies back up; they end with an error when it closes.
			go func() {
				batch := []byte(strings.Repeat(c.req, 1000))
				for range 100 {
					if _, err := conn.Write(batch); err != nil {
						return
					}
				}
			}()
			waitClosed(t, closed, 10*time.Second)
		})
	}
}

// TestKeepAliveReuse sends a second request on one connection after more
// than writeTimeout: the second request sets a fresh deadline before it
// writes, so the first one's, long passed, does not cut it.
func TestKeepAliveReuse(t *testing.T) {
	h := New(Options{View: testView, Subscribe: noSubscribe, History: noHistory, Acknowledge: noAcknowledge, Acknowledgements: noAcknowledgements})
	h.writeTimeout = 50 * time.Millisecond
	srv, _, _ := boundsServer(t, h)
	conn := dialRaw(t, srv)
	br := bufio.NewReader(conn)
	for i := range 2 {
		if i > 0 {
			time.Sleep(4 * h.writeTimeout)
		}
		if _, err := conn.Write([]byte("GET /api/state HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("request %d: body: %v", i+1, err)
		}
		if resp.StatusCode != http.StatusOK || resp.Close {
			t.Fatalf("request %d: status %d, close %v; want 200 on a kept-alive connection", i+1, resp.StatusCode, resp.Close)
		}
		// A short reply keeps its length: bounding the write must not change
		// the framing to chunked.
		if resp.ContentLength <= 0 || len(resp.TransferEncoding) != 0 {
			t.Fatalf("request %d: Content-Length %d, Transfer-Encoding %v; want a Content-Length and no chunking", i+1, resp.ContentLength, resp.TransferEncoding)
		}
	}
}

// TestEventsOutlastWriteTimeout keeps a stream open for many times the
// lowered writeTimeout: the bound ServeHTTP sets does not cut it, and its
// keepalives keep arriving.
func TestEventsOutlastWriteTimeout(t *testing.T) {
	f := &fakeEngine{view: eventsView()}
	h, srv, _ := newEvents(t, f)
	h.writeTimeout = 50 * time.Millisecond
	h.keepalive = 100 * time.Millisecond
	s := openStream(t, srv, nil)
	if e := s.next(t); e.event != "state" {
		t.Fatalf("first event %+v, want state", e)
	}
	start := time.Now()
	n := 0
	for time.Since(start) < 20*h.writeTimeout {
		if e := s.next(t); e.event != "keepalive" {
			t.Fatalf("event %+v, want keepalive", e)
		}
		n++
	}
	if n < 5 {
		t.Errorf("%d keepalives in %v, want at least 5", n, time.Since(start))
	}
}

// TestAckReplyAfterSlowEngine marks an Agent Seen through an engine that
// answers only after several writeTimeouts: the reply gets its own bound
// after the engine's answer, so it still arrives.
func TestAckReplyAfterSlowEngine(t *testing.T) {
	f := &fakeAck{block: func(context.Context) { time.Sleep(300 * time.Millisecond) }}
	h := New(Options{View: testView, Subscribe: noSubscribe, History: noHistory, Acknowledge: f.Acknowledge, Acknowledgements: noAcknowledgements})
	h.writeTimeout = 50 * time.Millisecond
	srv, _, _ := boundsServer(t, h)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/agents/h/s/1/w1:p2/seen", strings.NewReader(`{"waiting_since":2000}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("X-Herddash-Write", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("no reply after a slow engine: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %q, error %v; want 200", resp.StatusCode, body, err)
	}
}
