package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vilosource/herddash/internal/core"
)

// The history route: /api/agents/{host}/{server}/{epoch}/{pane}/history.
const (
	// prefixAgents opens every history path.
	prefixAgents = "/api/agents/"
	// segmentHistory is the history path's last segment.
	segmentHistory = "history"
)

// wireHistory is the body of a history response: the Agent's key and its
// recorded Transitions, in the record's order.
type wireHistory struct {
	Key         core.AgentKey    `json:"key"`
	Transitions []wireTransition `json:"transitions"`
}

// parseHistoryPath returns the AgentKey a request URL's history path names,
// or ok false when the path is not exactly
// /api/agents/{host}/{server}/{epoch}/{pane}/history. The path is split as
// the client sent it: from u.RawPath, each segment then percent-decoded on its
// own so an encoded "/" stays inside its segment, or, when RawPath is empty
// (the path held no escapes that decoding changes), from u.Path as it is.
// u.EscapedPath is not used: it re-escapes the decoded Path whenever RawPath
// is not canonical, losing every %2F. Every segment must be non-empty once
// decoded, and epoch a base-10 integer that the record can hold (at most
// 2^63-1).
func parseHistoryPath(u *url.URL) (key core.AgentKey, ok bool) {
	raw, escaped := u.Path, false
	if u.RawPath != "" {
		raw, escaped = u.RawPath, true
	}
	rest, found := strings.CutPrefix(raw, prefixAgents)
	if !found {
		return core.AgentKey{}, false
	}
	seg := strings.Split(rest, "/")
	if len(seg) != 5 {
		return core.AgentKey{}, false
	}
	for i, p := range seg {
		if escaped {
			s, err := url.PathUnescape(p)
			if err != nil {
				return core.AgentKey{}, false
			}
			p = s
		}
		if p == "" {
			return core.AgentKey{}, false
		}
		seg[i] = p
	}
	if seg[4] != segmentHistory {
		return core.AgentKey{}, false
	}
	// ParseUint takes digits only: no sign, no space. The bit size keeps the
	// epoch within the store's signed column.
	epoch, err := strconv.ParseUint(seg[2], 10, 63)
	if err != nil {
		return core.AgentKey{}, false
	}
	return core.AgentKey{Host: seg[0], Server: seg[1], Epoch: epoch, PaneID: seg[3]}, true
}

// keyPath renders key as the path segments that name it, escaped, for
// messages.
func keyPath(key core.AgentKey) string {
	return url.PathEscape(key.Host) + "/" + url.PathEscape(key.Server) + "/" +
		strconv.FormatUint(key.Epoch, 10) + "/" + url.PathEscape(key.PaneID)
}

// serveHistory is GET or HEAD on a history path (FR-14): the Agent's recorded
// Transitions within Retention, in the record's order, read through
// h.history and nothing else. It never writes, filters or calls herdr. A key
// with no rows gets 404; a failed read gets 500 with the detail only in the
// log; a request whose context has ended gets nothing.
func (h *Handler) serveHistory(w http.ResponseWriter, r *http.Request, key core.AgentKey) {
	ctx := r.Context()
	ts, err := h.history(ctx, key)
	if ctx.Err() != nil {
		return
	}
	// The server has no WriteTimeout (the event stream outlives any), so this
	// response bounds its own write, and clears the deadline after it so a
	// later request on the same connection is not cut short.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(h.writeTimeout)); err == nil {
		defer rc.SetWriteDeadline(time.Time{})
	} else if !errors.Is(err, http.ErrNotSupported) {
		h.log.Warn("history: set write deadline", "error", err.Error())
	}
	if err != nil {
		h.log.Error("history: read", "host", key.Host, "server", key.Server, "epoch", key.Epoch, "pane", key.PaneID, "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, codeInternal, "the history could not be read")
		return
	}
	if len(ts) == 0 {
		writeError(w, r, http.StatusNotFound, codeNotFound, "no history for "+keyPath(key)+" (never recorded, or removed by Retention)")
		return
	}
	out := wireHistory{Key: key, Transitions: make([]wireTransition, len(ts))}
	for i, t := range ts {
		out.Transitions[i] = newWireTransition(t)
	}
	body, err := json.Marshal(out)
	if err != nil {
		h.log.Error("history: encode", "host", key.Host, "server", key.Server, "epoch", key.Epoch, "pane", key.PaneID, "error", err.Error())
		writeError(w, r, http.StatusInternalServerError, codeInternal, "the history could not be encoded")
		return
	}
	writeJSON(w, r, http.StatusOK, body)
}
