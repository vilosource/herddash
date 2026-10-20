package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vilosource/herddash/internal/engine"
)

// Options configures the handler New returns.
type Options struct {
	// View returns the engine's latest published View. It must be safe to
	// call from any goroutine (engine.Engine.View is).
	View func() engine.View
	// Logger receives the handler's own failures; nil discards them.
	Logger *slog.Logger
}

// The error codes in the JSON error shape.
const (
	codeHostNotAllowed   = "host_not_allowed"
	codeMethodNotAllowed = "method_not_allowed"
	codeNotFound         = "not_found"
)

// pathState is the current-state route.
const pathState = "/api/state"

// allowGetHead is the Allow header of every 405: the surface is GET only.
const allowGetHead = "GET, HEAD"

// New returns the read-only HTTP surface (AD-6, ADR-0007). Every request is
// first checked for a loopback Host header (DNS rebinding); then only
// GET or HEAD on a known route is served. It reads only through opts.View:
// it never calls herdr and never touches the store. It never sends a CORS
// header. It panics if opts.View is nil.
func New(opts Options) http.Handler {
	if opts.View == nil {
		panic("httpapi.New: Options.View is nil")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &handler{view: opts.View, log: log}
}

type handler struct {
	view func() engine.View
	log  *slog.Logger
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hd := w.Header()
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")

	if !hostAllowed(r.Host) {
		writeError(w, r, http.StatusForbidden, codeHostNotAllowed, "the Host header must be localhost, 127.0.0.1 or [::1]")
		return
	}
	if r.URL.Path != pathState {
		writeError(w, r, http.StatusNotFound, codeNotFound, "no such route")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		hd.Set("Allow", allowGetHead)
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET and HEAD are allowed")
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
