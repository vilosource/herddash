package herdr

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer is a scripted herdr stand-in on a unix socket in a temporary
// directory. Each accepted connection reads one request line and hands it to the
// test's handler; the connection closes when the handler returns. No test dials
// anything else.
type fakeServer struct {
	path string
	ln   net.Listener
	quit chan struct{}

	conns        atomic.Int32 // connections accepted
	clientClosed atomic.Int32 // connections the client closed after its response

	mu   sync.Mutex
	open []net.Conn
	wg   sync.WaitGroup
}

// fakeConn is one accepted connection with the request line it carried.
type fakeConn struct {
	t    *testing.T
	srv  *fakeServer
	conn net.Conn
	r    *bufio.Reader
	// raw is the request line; req is it decoded.
	raw []byte
	req struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
}

// socketDir returns a fresh directory short enough for a unix socket path on
// macOS (104 bytes), removed when the test ends.
func socketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func newFake(t *testing.T, handle func(fc *fakeConn)) *fakeServer {
	t.Helper()
	path := filepath.Join(socketDir(t), "herdr.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{path: path, ln: ln, quit: make(chan struct{})}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.conns.Add(1)
			s.mu.Lock()
			s.open = append(s.open, conn)
			s.mu.Unlock()
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer conn.Close()
				fc := &fakeConn{t: t, srv: s, conn: conn, r: bufio.NewReader(conn)}
				line, err := fc.r.ReadBytes('\n')
				if err != nil {
					return
				}
				fc.raw = line
				if err := json.Unmarshal(line, &fc.req); err != nil {
					t.Errorf("fake: request is not JSON: %q", line)
					return
				}
				handle(fc)
			}()
		}
	}()
	t.Cleanup(func() {
		close(s.quit)
		ln.Close()
		s.mu.Lock()
		for _, c := range s.open {
			c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s
}

func (s *fakeServer) client() *Client { return New(s.path) }

// send writes one line as is.
func (fc *fakeConn) send(line string) {
	if _, err := fc.conn.Write([]byte(line + "\n")); err != nil {
		fc.t.Logf("fake: write: %v", err)
	}
}

// reply writes a response line with its id replaced by the request's id.
func (fc *fakeConn) reply(line string) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		fc.t.Errorf("fake: response is not JSON: %v", err)
		return
	}
	id, _ := json.Marshal(fc.req.ID)
	m["id"] = id
	b, err := json.Marshal(m)
	if err != nil {
		fc.t.Error(err)
		return
	}
	fc.send(string(b))
}

// expectClientClose waits for the client to close its end, and counts it.
func (fc *fakeConn) expectClientClose() {
	fc.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := fc.r.ReadByte()
	if errors.Is(err, io.EOF) {
		fc.srv.clientClosed.Add(1)
		return
	}
	select {
	case <-fc.srv.quit:
		// The test ended and its cleanup closed the connection first.
		return
	default:
	}
	fc.t.Errorf("fake: client did not close the connection after its response: %v", err)
}

// hold blocks until the test ends, keeping the connection open.
func (fc *fakeConn) hold() { <-fc.srv.quit }

// fixture reads a recorded herdr 0.9.5 line from gen/testdata. Call it from the
// test goroutine, before the fake server needs it.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("gen", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(trimNewline(b))
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
