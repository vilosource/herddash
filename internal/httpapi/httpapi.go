package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
)

// Options configures the handler New returns.
type Options struct {
	// View returns the engine's latest published View. It must be safe to
	// call from any goroutine (engine.Engine.View is).
	View func() engine.View
	// Subscribe starts a subscription to every publication from now on, with
	// a channel holding buffer of them, and returns the func that ends it
	// (engine.Engine.Subscribe). Each /events stream takes one.
	Subscribe func(buffer int) (*engine.ViewSubscription, func())
	// History returns one Agent's recorded Transitions in the record's order
	// (store.Store.History). The history route reads only through it.
	History func(ctx context.Context, key core.AgentKey) ([]core.Transition, error)
	// Logger receives the handler's own failures; nil discards them.
	Logger *slog.Logger
}

// The error codes in the JSON error shape.
const (
	codeHostNotAllowed   = "host_not_allowed"
	codeMethodNotAllowed = "method_not_allowed"
	codeNotFound         = "not_found"
)

// The routes.
const (
	// pathState is the current-state route.
	pathState = "/api/state"
	// pathEvents is the live event stream.
	pathEvents = "/events"
)

// The Allow headers of a 405: the surface is GET only, and the event stream
// does not answer HEAD.
const (
	allowGetHead = "GET, HEAD"
	allowGet     = "GET"
)

// New returns the read-only HTTP surface (AD-6, ADR-0007). Every request is
// first checked for a loopback Host header (DNS rebinding); then only
// GET (or HEAD, except on /events) on a known route is served. It reads only
// through opts.View, opts.Subscribe and opts.History: it never calls herdr and
// never writes to the store. It never sends a CORS header. It panics if
// opts.View, opts.Subscribe or opts.History is nil.
func New(opts Options) *Handler {
	if opts.View == nil {
		panic("httpapi.New: Options.View is nil")
	}
	if opts.Subscribe == nil {
		panic("httpapi.New: Options.Subscribe is nil")
	}
	if opts.History == nil {
		panic("httpapi.New: Options.History is nil")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Handler{
		view:         opts.View,
		subscribe:    opts.Subscribe,
		history:      opts.History,
		log:          log,
		stop:         make(chan struct{}),
		keepalive:    keepaliveInterval,
		writeTimeout: streamWriteTimeout,
	}
}

// Handler is the read-only HTTP surface New returns.
type Handler struct {
	view      func() engine.View
	subscribe func(buffer int) (*engine.ViewSubscription, func())
	history   func(ctx context.Context, key core.AgentKey) ([]core.Transition, error)
	log       *slog.Logger

	// stop is closed, once, by CloseStreams.
	stop     chan struct{}
	stopOnce sync.Once

	// keepalive and writeTimeout are keepaliveInterval and
	// streamWriteTimeout; tests lower them.
	keepalive    time.Duration
	writeTimeout time.Duration
}

// CloseStreams ends every open /events stream, and makes every later one end
// at once. serve registers it with http.Server.RegisterOnShutdown, since
// Shutdown does not end a long-lived response by itself. It may be called any
// number of times, from any goroutine.
func (h *Handler) CloseStreams() {
	h.stopOnce.Do(func() { close(h.stop) })
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hd := w.Header()
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")

	if !hostAllowed(r.Host) {
		writeError(w, r, http.StatusForbidden, codeHostNotAllowed, "the Host header must be localhost, 127.0.0.1 or [::1]")
		return
	}
	var (
		isHistory  bool
		historyKey core.AgentKey
	)
	switch {
	case strings.HasPrefix(r.URL.Path, prefixAgents):
		k, ok := parseHistoryPath(r.URL)
		if !ok {
			writeError(w, r, http.StatusNotFound, codeNotFound, "no such route")
			return
		}
		isHistory, historyKey = true, k
	case r.URL.Path == pathState:
	case r.URL.Path == pathEvents:
		if r.Method != http.MethodGet {
			hd.Set("Allow", allowGet)
			writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET is allowed")
			return
		}
		h.serveEvents(w, r)
		return
	default:
		writeError(w, r, http.StatusNotFound, codeNotFound, "no such route")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		hd.Set("Allow", allowGetHead)
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET and HEAD are allowed")
		return
	}
	if isHistory {
		h.serveHistory(w, r, historyKey)
		return
	}
	v := h.view()
	hd.Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	// The status is already sent: an encode or write failure (a client gone)
	// can only be logged.
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Warn("write state", "error", err.Error())
	}
}

// writeError writes the JSON error shape {"error":{"code","message"}}.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	e.Error.Code, e.Error.Message = code, message
	body, _ := json.Marshal(e) // two strings cannot fail to encode
	writeJSON(w, r, status, body)
}

// writeJSON writes body as JSON with its length; a HEAD gets the headers only.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

// hostAllowed reports whether a Host header names a loopback host:
// localhost, 127.0.0.1 or [::1], each optionally with a numeric port. Anything
// else, including an empty Host, is refused (ADR-0007: this closes the DNS
// rebinding read that a loopback bind alone does not).
func hostAllowed(h string) bool {
	host, port := h, ""
	if strings.HasPrefix(h, "[") {
		i := strings.IndexByte(h, ']')
		if i < 0 {
			return false
		}
		host, port = h[:i+1], h[i+1:]
	} else if i := strings.LastIndexByte(h, ':'); i >= 0 {
		host, port = h[:i], h[i:]
	}
	if port != "" && !validPort(port) {
		return false
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "[::1]":
		return true
	}
	return false
}

// validPort reports whether p is ":" followed by one to five digits.
func validPort(p string) bool {
	if len(p) < 2 || len(p) > 6 || p[0] != ':' {
		return false
	}
	for _, c := range p[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
