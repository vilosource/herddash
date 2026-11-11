package core

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

// Usage is one Response's token counts (ADR-0017): input, output, the two
// cache-write splits (5 minutes and 1 hour) and cache read.
type Usage struct {
	Input        int64
	Output       int64
	CacheWrite5m int64
	CacheWrite1h int64
	CacheRead    int64
}

// UsageMarker is what one assistant transcript record says about token usage
// (ADR-0017): the Response's two ids, its model, whether the record has a stop
// reason, the record's timestamp in UTC milliseconds, its five counts and, on
// a sidechain record, its agentId. Nothing else of the record is kept. The
// zero value is no marker.
type UsageMarker struct {
	MessageID string
	RequestID string
	Model     string
	AgentID   string
	Stopped   bool
	At        int64
	Usage
}

// UsageIssue is why an assistant record that carries a usage object yields no
// UsageMarker. Problems are returned, never logged: core does no I/O.
type UsageIssue uint8

const (
	// UsageNone is no problem.
	UsageNone UsageIssue = iota
	// UsageNoKey is a usage record lacking requestId or message.id: it cannot
	// be told apart from another Response, so it is not counted.
	UsageNoKey
	// UsageUnknownShape is a usage record whose fields are not the shape
	// observed: a count missing, not an integer or negative, cache_creation
	// absent or not an object, its split not summing to
	// cache_creation_input_tokens, an empty model, or a timestamp that does
	// not parse.
	UsageUnknownShape
)

// syntheticModel is the model Claude Code writes on records it made itself,
// not from an API response; they are never counted.
const syntheticModel = "<synthetic>"

// usageMessage is the only part of an assistant record's message decoded for
// usage: everything else, content included, is skipped and never held.
type usageMessage struct {
	ID         json.RawMessage `json:"id"`
	Model      json.RawMessage `json:"model"`
	StopReason json.RawMessage `json:"stop_reason"`
	Usage      json.RawMessage `json:"usage"`
}

// usageCounts is the shape of message.usage, each count kept raw so that it
// can be checked as a strict integer.
type usageCounts struct {
	Input         json.RawMessage `json:"input_tokens"`
	Output        json.RawMessage `json:"output_tokens"`
	CacheCreation json.RawMessage `json:"cache_creation_input_tokens"`
	CacheRead     json.RawMessage `json:"cache_read_input_tokens"`
	Split         json.RawMessage `json:"cache_creation"`
}

type usageSplit struct {
	Write5m json.RawMessage `json:"ephemeral_5m_input_tokens"`
	Write1h json.RawMessage `json:"ephemeral_1h_input_tokens"`
}

// usageOf reads an assistant record for its usage marker. A record whose
// message is not an object, whose message.usage is absent or not an object, or
// whose model is <synthetic> is neither a marker nor a problem: it returns the
// zero marker and UsageNone.
func usageOf(rec transcriptRecord) (UsageMarker, UsageIssue) {
	if !isObject(rec.Message) {
		return UsageMarker{}, UsageNone
	}
	var m usageMessage
	if json.Unmarshal(rec.Message, &m) != nil || !isObject(m.Usage) {
		return UsageMarker{}, UsageNone
	}
	model := jsonString(m.Model)
	if model == syntheticModel {
		return UsageMarker{}, UsageNone
	}
	mk := UsageMarker{MessageID: jsonString(m.ID), RequestID: jsonString(rec.RequestID)}
	if mk.MessageID == "" || mk.RequestID == "" {
		return UsageMarker{}, UsageNoKey
	}
	var c usageCounts
	var s usageSplit
	if model == "" || json.Unmarshal(m.Usage, &c) != nil || !isObject(c.Split) || json.Unmarshal(c.Split, &s) != nil {
		return UsageMarker{}, UsageUnknownShape
	}
	var creation int64
	ok := true
	for _, f := range []struct {
		raw json.RawMessage
		to  *int64
	}{
		{c.Input, &mk.Input}, {c.Output, &mk.Output}, {c.CacheCreation, &creation}, {c.CacheRead, &mk.CacheRead},
		{s.Write5m, &mk.CacheWrite5m}, {s.Write1h, &mk.CacheWrite1h},
	} {
		v, good := count(f.raw)
		ok = ok && good
		*f.to = v
	}
	if !ok || mk.CacheWrite5m > creation || mk.CacheWrite1h != creation-mk.CacheWrite5m {
		return UsageMarker{}, UsageUnknownShape
	}
	at, err := time.Parse(time.RFC3339Nano, jsonString(rec.Timestamp))
	if err != nil {
		return UsageMarker{}, UsageUnknownShape
	}
	mk.At = at.UnixMilli()
	mk.Model = model
	mk.Stopped = jsonString(m.StopReason) != ""
	if rec.IsSidechain {
		mk.AgentID = jsonString(rec.AgentID)
	}
	return mk, UsageNone
}

// count is raw as a strict count: a JSON integer of at least 0 that fits an
// int64, never a float, exponent, string, bool or null.
func count(raw json.RawMessage) (int64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0, false
	}
	for _, b := range raw {
		if b < '0' || b > '9' {
			return 0, false
		}
	}
	v, err := strconv.ParseInt(string(raw), 10, 64)
	return v, err == nil
}

// Response is one complete model Response (ADR-0017): the records sharing a
// message id and request id, taken at one record's values. Stopped says that
// record had a stop reason; false means a record of another Response followed
// before any did, and the one with the largest output was taken. SubagentID is
// the record's agentId, "" on the main thread.
type Response struct {
	MessageID  string
	RequestID  string
	Model      string
	SubagentID string
	Stopped    bool
	At         int64
	Usage
}

type responseKey struct{ messageID, requestID string }

func keyOf(m UsageMarker) responseKey { return responseKey{m.MessageID, m.RequestID} }

func responseOf(m UsageMarker) Response {
	return Response{
		MessageID: m.MessageID, RequestID: m.RequestID, Model: m.Model, SubagentID: m.AgentID,
		Stopped: m.Stopped, At: m.At, Usage: m.Usage,
	}
}

// UsageLedger folds one transcript file's usage markers, in file order, into
// complete Responses (ADR-0017). A Response completes at its first record with
// a stop reason, taking that record's values and timestamp, or, with none,
// when a record of another Response follows, taking its record with the
// largest output (the later on a tie) and that record's timestamp. Later
// records of the Response just completed are ignored. It holds at most the
// pending Response and the last completed key, reads no clock, and its zero
// value is ready to use. A Response still pending at the end of the file is
// never returned: that is the accepted loss.
type UsageLedger struct {
	pending    UsageMarker
	hasPending bool
	done       responseKey
	hasDone    bool
}

// Add folds one marker into the ledger and returns the Responses it
// completed, in order: none, one, or two (the pending one superseded, then a
// new one that stopped on its first record). Callers pass only markers
// DecodeTranscriptLine returned, whose ids are both non-empty; a zero marker
// or one with an empty id would become a Response with an empty key.
func (g *UsageLedger) Add(m UsageMarker) []Response {
	k := keyOf(m)
	if g.hasDone && k == g.done {
		return nil
	}
	var out []Response
	if g.hasPending {
		if keyOf(g.pending) == k {
			if m.Stopped {
				g.complete(m)
				return []Response{responseOf(m)}
			}
			if m.Output >= g.pending.Output {
				g.pending = m
			}
			return nil
		}
		out = append(out, responseOf(g.pending))
		g.complete(g.pending)
	}
	if m.Stopped {
		g.complete(m)
		return append(out, responseOf(m))
	}
	g.pending, g.hasPending = m, true
	return out
}

func (g *UsageLedger) complete(m UsageMarker) {
	g.done, g.hasDone = keyOf(m), true
	g.pending, g.hasPending = UsageMarker{}, false
}

// Pending reports whether a Response is waiting for its stop reason or for a
// record of another Response.
func (g *UsageLedger) Pending() bool { return g.hasPending }
