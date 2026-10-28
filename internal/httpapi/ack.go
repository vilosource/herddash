package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/engine"
)

// The write routes (ADR-0014, amending ADR-0007): the only routes that accept
// anything but GET and HEAD, and the only ones that change anything, which is
// herddash's own acknowledgement of a finish and nothing else.
const (
	// pathSeen and pathUnseen are the bulk mark and undo.
	pathSeen   = "/api/seen"
	pathUnseen = "/api/unseen"
	// segmentSeen and segmentUnseen end a per-Agent write path,
	// /api/agents/{host}/{server}/{epoch}/{pane}/seen and .../unseen.
	segmentSeen   = "seen"
	segmentUnseen = "unseen"
)

// The write routes' bounds.
const (
	// maxAgentBody and maxBulkBody bound a per-Agent and a bulk body.
	maxAgentBody = 4 << 10
	maxBulkBody  = 64 << 10
	// maxBulkItems bounds a bulk request's items.
	maxBulkItems = 256
	// writeHeaderName and writeHeaderValue are the custom header every write
	// must carry: a page on another origin cannot send it without a CORS
	// preflight, which herddash never answers.
	writeHeaderName  = "X-Herddash-Write"
	writeHeaderValue = "1"
)

// allowPost is the Allow header of a 405 on a write route.
const allowPost = "POST"

// The write routes' error codes, beside the read routes' own.
const (
	codeOriginNotAllowed     = "origin_not_allowed"
	codeWriteHeaderMissing   = "write_header_missing"
	codeUnsupportedMediaType = "unsupported_media_type"
	codeBodyTooLarge         = "body_too_large"
	codeBadRequest           = "bad_request"
	codeUnknownAgent         = "unknown_agent"
	codeNotFinished          = "not_finished"
	codeStaleView            = "stale_view"
	codeRecordingOff         = "recording_off"
	codeStoreFailed          = "store_failed"
)

// writeRoute is a write route a request names: per-Agent (one key from the
// path) or bulk, a mark or an undo.
type writeRoute struct {
	seen bool
	bulk bool
	key  core.AgentKey // per-Agent only
}

// wireAgentBody is a per-Agent write's body.
type wireAgentBody struct {
	WaitingSince int64 `json:"waiting_since"`
}

// wireBulkBody is a bulk write's body.
type wireBulkBody struct {
	Agents []wireBulkItem `json:"agents"`
}

type wireBulkItem struct {
	Key          *wireKey `json:"key"`
	WaitingSince int64    `json:"waiting_since"`
}

// wireKey is an AgentKey in a bulk body: every field required.
type wireKey struct {
	Host   string  `json:"host"`
	Server string  `json:"server"`
	Epoch  *uint64 `json:"epoch"`
	PaneID string  `json:"pane_id"`
}

// wireAgents is a write's success body: the Agents' views after it, in
// request order.
type wireAgents struct {
	Agents []core.AgentView `json:"agents"`
}

// writeRouteOf reports the write route r's path names, if any.
func writeRouteOf(r *http.Request) (writeRoute, bool) {
	switch r.URL.Path {
	case pathSeen:
		return writeRoute{seen: true, bulk: true}, true
	case pathUnseen:
		return writeRoute{bulk: true}, true
	}
	if !strings.HasPrefix(r.URL.Path, prefixAgents) {
		return writeRoute{}, false
	}
	key, last, ok := parseAgentPath(r.URL)
	if !ok || (last != segmentSeen && last != segmentUnseen) {
		return writeRoute{}, false
	}
	return writeRoute{seen: last == segmentSeen, key: key}, true
}

// serveWrite is any method on a write route. Only POST is served; the guard
// runs, in ADR-0014's order, before a byte of the body is read, so a
// cross-site request never reaches the engine: the Host was already checked
// loopback by ServeHTTP; then Origin, Sec-Fetch-Site, the custom header and
// the content type; then the body is read within its bound and decoded
// strictly. Only then is the engine asked, through h.acknowledge, and its
// answer mapped to a status. herdr is never told, and nothing here writes to
// the store.
func (h *Handler) serveWrite(w http.ResponseWriter, r *http.Request, route writeRoute) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", allowPost)
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only POST is allowed")
		return
	}
	if status, code, msg, ok := guardWrite(r); !ok {
		writeError(w, r, status, code, msg)
		return
	}
	limit := int64(maxAgentBody)
	if route.bulk {
		limit = maxBulkBody
	}
	body, err := h.readBody(w, r, limit)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, codeBodyTooLarge, "the body is larger than its bound")
			return
		}
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "the body could not be read")
		return
	}
	req, msg, ok := decodeWrite(body, route)
	if !ok {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, msg)
		return
	}
	ctx := r.Context()
	views, err := h.acknowledge(ctx, req)
	if ctx.Err() != nil {
		// The client is gone: whatever the engine did, nobody reads the answer.
		return
	}
	defer h.boundWrite(w, "acknowledge")()
	if err != nil {
		h.writeAckError(w, r, err)
		return
	}
	out, err := json.Marshal(wireAgents{Agents: views})
	if err != nil {
		h.log.Error("acknowledge: encode", "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, codeInternal, "the views could not be encoded")
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// guardWrite checks a write's headers, in order, and returns the refusal of
// the first that fails: Origin required and equal, case-insensitively, to
// "http://" + Host (a browser always sends it on a POST, so the Board passes;
// "null" and every other origin are refused); Sec-Fetch-Site, when present,
// same-origin; X-Herddash-Write: 1; and a JSON Content-Type, with at most a
// charset=utf-8 parameter.
func guardWrite(r *http.Request) (status int, code, msg string, ok bool) {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || !strings.EqualFold(origins[0], "http://"+r.Host) {
		return http.StatusForbidden, codeOriginNotAllowed, "a write must come from the Board's own origin (Origin http://" + r.Host + ")", false
	}
	if sites, present := r.Header[http.CanonicalHeaderKey("Sec-Fetch-Site")]; present && (len(sites) != 1 || sites[0] != "same-origin") {
		return http.StatusForbidden, codeOriginNotAllowed, "a write must be same-origin (Sec-Fetch-Site)", false
	}
	if v := r.Header.Values(writeHeaderName); len(v) != 1 || v[0] != writeHeaderValue {
		return http.StatusForbidden, codeWriteHeaderMissing, "a write must carry " + writeHeaderName + ": " + writeHeaderValue, false
	}
	if !jsonContentType(r.Header.Values("Content-Type")) {
		return http.StatusUnsupportedMediaType, codeUnsupportedMediaType, "the body must be application/json", false
	}
	return 0, "", "", true
}

// jsonContentType reports whether a request's Content-Type values are one
// application/json, with no parameter but an optional charset=utf-8.
func jsonContentType(values []string) bool {
	if len(values) != 1 {
		return false
	}
	mt, params, err := mime.ParseMediaType(values[0])
	if err != nil || mt != "application/json" {
		return false
	}
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			return false
		}
	}
	return true
}

// readBody reads r's body, at most limit bytes, within the handler's write
// timeout: the server bounds only the headers' read, so a body trickled in
// would otherwise hold the request open. A body over limit fails with an
// *http.MaxBytesError.
func (h *Handler) readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	rc := http.NewResponseController(w)
	if err := rc.SetReadDeadline(time.Now().Add(h.writeTimeout)); err == nil {
		defer rc.SetReadDeadline(time.Time{})
	} else if !errors.Is(err, http.ErrNotSupported) {
		h.log.Warn("acknowledge: set read deadline", "error", err.Error())
	}
	return io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
}

// decodeWrite decodes a write's body for route strictly: exactly one JSON
// object, no unknown field, no trailing data, a positive waiting_since on
// every item, and for a bulk body 1 to maxBulkItems items, each with a
// well-formed key (host, server and pane_id non-empty, epoch present and
// within the record's range), no key twice. It returns the engine's request,
// or a message saying what is wrong.
func decodeWrite(body []byte, route writeRoute) (engine.AckRequest, string, bool) {
	if t := bytes.TrimLeft(body, " \t\r\n"); len(t) == 0 || t[0] != '{' {
		return engine.AckRequest{}, "the body must be one JSON object", false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	decodeOne := func(v any) bool {
		if dec.Decode(v) != nil {
			return false
		}
		_, err := dec.Token()
		return err == io.EOF
	}
	req := engine.AckRequest{Seen: route.seen}
	if !route.bulk {
		var b wireAgentBody
		if !decodeOne(&b) {
			return engine.AckRequest{}, `the body must be exactly {"waiting_since": <ms>}`, false
		}
		if b.WaitingSince <= 0 {
			return engine.AckRequest{}, "waiting_since must be a positive UTC Unix millisecond", false
		}
		req.Items = []engine.AckItem{{Key: route.key, WaitingSince: b.WaitingSince}}
		return req, "", true
	}
	var b wireBulkBody
	if !decodeOne(&b) {
		return engine.AckRequest{}, `the body must be exactly {"agents": [{"key": {...}, "waiting_since": <ms>}, ...]}`, false
	}
	if len(b.Agents) == 0 || len(b.Agents) > maxBulkItems {
		return engine.AckRequest{}, "agents must hold 1 to 256 items", false
	}
	seen := make(map[core.AgentKey]bool, len(b.Agents))
	for _, it := range b.Agents {
		k := it.Key
		if k == nil || k.Host == "" || k.Server == "" || k.PaneID == "" || k.Epoch == nil || *k.Epoch > 1<<63-1 {
			return engine.AckRequest{}, "every item needs a key with host, server, epoch and pane_id", false
		}
		if it.WaitingSince <= 0 {
			return engine.AckRequest{}, "waiting_since must be a positive UTC Unix millisecond", false
		}
		key := core.AgentKey{Host: k.Host, Server: k.Server, Epoch: *k.Epoch, PaneID: k.PaneID}
		if seen[key] {
			return engine.AckRequest{}, "the key " + keyPath(key) + " is named twice", false
		}
		seen[key] = true
		req.Items = append(req.Items, engine.AckItem{Key: key, WaitingSince: it.WaitingSince})
	}
	return req, "", true
}

// writeAckError maps the engine's refusal to its status and code. A stale
// refusal names the stale keys; any error the engine does not type is a
// failure to record, logged with its detail.
func (h *Handler) writeAckError(w http.ResponseWriter, r *http.Request, err error) {
	var stale *engine.StaleError
	switch {
	case errors.As(err, &stale):
		names := make([]string, len(stale.Keys))
		for i, k := range stale.Keys {
			names[i] = keyPath(k)
		}
		writeError(w, r, http.StatusConflict, codeStaleView, "changed since the Board showed it: "+strings.Join(names, ", "))
	case errors.Is(err, engine.ErrStaleView):
		writeError(w, r, http.StatusConflict, codeStaleView, "changed since the Board showed it")
	case errors.Is(err, engine.ErrUnknownAgent):
		writeError(w, r, http.StatusNotFound, codeUnknownAgent, "no such Agent now: "+err.Error())
	case errors.Is(err, engine.ErrNotFinished):
		writeError(w, r, http.StatusConflict, codeNotFinished, "only a Finished Agent can be marked seen: "+err.Error())
	case errors.Is(err, engine.ErrRecordingOff):
		writeError(w, r, http.StatusServiceUnavailable, codeRecordingOff, "herddash is not recording, so nothing can be marked")
	case errors.Is(err, engine.ErrBadRequest):
		writeError(w, r, http.StatusBadRequest, codeBadRequest, err.Error())
	default:
		h.log.Error("acknowledge", "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, codeStoreFailed, "the acknowledgement could not be recorded")
	}
}

// ackFunc and acksFunc are Options.Acknowledge's and
// Options.Acknowledgements' types.
type (
	ackFunc  = func(ctx context.Context, req engine.AckRequest) ([]core.AgentView, error)
	acksFunc = func(ctx context.Context, key core.AgentKey) ([]core.Acknowledgement, error)
)
