package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr/gen"
)

// maxLine bounds one response or event line. herdr caps requests at 1 MiB; a
// snapshot of a large session is the biggest line it sends back.
const maxLine = 64 << 20

// defaultCallTimeout bounds a one-shot call, and a subscribe call's setup, when
// the caller's context has no deadline, so a silent herdr is an error and never
// a hang.
const defaultCallTimeout = 10 * time.Second

// callTimeout is defaultCallTimeout; tests lower it.
var callTimeout = defaultCallTimeout

// withDefaultTimeout applies callTimeout when ctx has no deadline of its own.
func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, callTimeout)
}

// ErrNoForegroundProcess is returned by ForegroundProcess when herdr reports no
// foreground process for the pane.
var ErrNoForegroundProcess = errors.New("herdr: pane has no foreground process")

// Error is an error response from herdr: its code, which is an open set, and its
// message.
type Error struct {
	// Method is the request method herdr answered.
	Method  string
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("herdr: %s: %s: %s", e.Method, e.Code, e.Message)
}

// Client speaks to one herdr server over its socket. It holds no connection:
// every one-shot request dials afresh, and every subscription owns its own
// connection (AD-5). It decides nothing (AD-11). A Client is safe for concurrent
// use.
//
// Every method honours its context. When the context has no deadline, a one-shot
// call, and a subscribe call's setup, is bounded by defaultCallTimeout, so a
// missing response line is an error, never a hang.
type Client struct {
	socketPath string
	nextID     atomic.Uint64
}

// New returns a Client for the herdr socket at socketPath. It does not dial.
func New(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

// Ping asks herdr for its version, the string the version gate reads.
func (c *Client) Ping(ctx context.Context) (string, error) {
	id := c.id("r")
	var pong gen.ResponseResultPong
	err := c.call(ctx, gen.RequestPing{ID: id, Method: gen.RequestMethodPing}, id, gen.ResponseResultTypePong, &pong)
	if err != nil {
		return "", err
	}
	return pong.Version, nil
}

// Snapshot reads herdr's whole session.
func (c *Client) Snapshot(ctx context.Context) (core.Snapshot, error) {
	id := c.id("r")
	var r gen.ResponseResultSessionSnapshot
	err := c.call(ctx, gen.RequestSessionSnapshot{ID: id, Method: gen.RequestMethodSessionSnapshot}, id, gen.ResponseResultTypeSessionSnapshot, &r)
	if err != nil {
		return core.Snapshot{}, err
	}
	return mapSnapshot(r.Snapshot), nil
}

// ForegroundProcess reads the foreground process of the pane: the process whose
// pid is the foreground process group id, else the first foreground process
// herdr lists. It returns an error wrapping ErrNoForegroundProcess when there is
// none.
func (c *Client) ForegroundProcess(ctx context.Context, paneID string) (core.ForegroundProcess, error) {
	id := c.id("r")
	req := gen.RequestPaneProcessInfo{ID: id, Method: gen.RequestMethodPaneProcessInfo,
		Params: gen.PaneProcessInfoParams{PaneID: &paneID}}
	var r gen.ResponseResultPaneProcessInfo
	if err := c.call(ctx, req, id, gen.ResponseResultTypePaneProcessInfo, &r); err != nil {
		return core.ForegroundProcess{}, err
	}
	fp, err := mapForegroundProcess(r.ProcessInfo)
	if err != nil {
		return core.ForegroundProcess{}, fmt.Errorf("herdr: %s %s: %w", gen.RequestMethodPaneProcessInfo, paneID, err)
	}
	return fp, nil
}

// id returns a fresh request id with the given prefix.
func (c *Client) id(prefix string) string {
	return prefix + strconv.FormatUint(c.nextID.Add(1), 10)
}

// method reads the method of a generated request value, for error messages.
func method(req any) string {
	switch r := req.(type) {
	case gen.RequestPing:
		return string(r.Method)
	case gen.RequestSessionSnapshot:
		return string(r.Method)
	case gen.RequestPaneProcessInfo:
		return string(r.Method)
	case gen.RequestEventsSubscribe:
		return string(r.Method)
	}
	return fmt.Sprintf("%T", req)
}

// call performs one request on its own connection: dial, write one line, read
// one response line, close. The result must have type want and is decoded into
// out.
func (c *Client) call(ctx context.Context, req any, id string, want gen.ResponseResultType, out any) error {
	m := method(req)
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return fmt.Errorf("herdr: %s: %w", m, err)
	}
	defer conn.Close()
	// Closing the connection when the context ends unblocks the write and read.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	line, err := roundTrip(conn, bufio.NewReader(conn), req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("herdr: %s: %w", m, ctxErr)
		}
		return fmt.Errorf("herdr: %s: %w", m, err)
	}
	result, herr, err := decodeResponse(line, id)
	if err != nil {
		return fmt.Errorf("herdr: %s: %w", m, err)
	}
	if herr != nil {
		herr.Method = m
		return herr
	}
	if err := decodeResult(result, want, out); err != nil {
		return fmt.Errorf("herdr: %s: %w", m, err)
	}
	return nil
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", c.socketPath)
}

// roundTrip writes req as one line and reads one line back from r.
func roundTrip(conn net.Conn, r *bufio.Reader, req any) ([]byte, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	line, err := readLine(r)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return line, nil
}

// errLineTooLong is returned by readLine for a line over maxLine.
var errLineTooLong = errors.New("line exceeds limit")

// readLine reads one newline-terminated line, without its newline. A final line
// with no newline before EOF is returned as a line; EOF with nothing read is
// io.EOF.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, errLineTooLong
		}
		switch {
		case err == nil:
			return line[:len(line)-1], nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case len(line) > 0 && errors.Is(err, io.EOF):
			return line, nil
		default:
			return nil, err
		}
	}
}

// decodeResponse decodes one response line. It returns the success result, or
// herdr's error response as an *Error, or a decoding error. An id that does not
// echo want is a decoding error; herdr leaves an error's id empty only when it
// could not read the request's id.
func decodeResponse(line []byte, want string) (json.RawMessage, *Error, error) {
	var probe struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return nil, nil, fmt.Errorf("decode response: %w", err)
	}
	if len(probe.Error) > 0 && string(probe.Error) != "null" {
		var er gen.ErrorResponse
		if err := json.Unmarshal(line, &er); err != nil {
			return nil, nil, fmt.Errorf("decode error response: %w", err)
		}
		if er.ID != "" && er.ID != want {
			return nil, nil, fmt.Errorf("error response id %q, want %q", er.ID, want)
		}
		return nil, &Error{Code: er.Error.Code, Message: er.Error.Message}, nil
	}
	var sr gen.SuccessResponse
	if err := json.Unmarshal(line, &sr); err != nil {
		return nil, nil, fmt.Errorf("decode response: %w", err)
	}
	if sr.ID != want {
		return nil, nil, fmt.Errorf("response id %q, want %q", sr.ID, want)
	}
	if len(sr.Result) == 0 || string(sr.Result) == "null" {
		return nil, nil, errors.New("response has neither result nor error")
	}
	return sr.Result, nil, nil
}

// decodeResult checks a result's type discriminator and decodes it into out.
func decodeResult(result json.RawMessage, want gen.ResponseResultType, out any) error {
	var head struct {
		Type gen.ResponseResultType `json:"type"`
	}
	if err := json.Unmarshal(result, &head); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	if head.Type != want {
		return fmt.Errorf("result type %q, want %q", head.Type, want)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(result, out); err != nil {
		return fmt.Errorf("decode %s: %w", want, err)
	}
	return nil
}
