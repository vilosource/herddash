package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// usageCountsJSON is a usage object in the observed shape (Claude Code
// 2.1.292): the four counts, the cache_creation split, and fields herddash
// does not read.
func usageCountsJSON(in, out, w5, w1h, read int64) string {
	b, _ := json.Marshal(map[string]any{
		"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": read,
		"cache_creation_input_tokens": w5 + w1h,
		"cache_creation":              map[string]any{"ephemeral_5m_input_tokens": w5, "ephemeral_1h_input_tokens": w1h},
		"service_tier":                "standard", "output_tokens_details": map[string]any{"thinking_tokens": 1},
	})
	return string(b)
}

// assistantRec is an assistant record in the observed shape, its usage given
// raw; stop "" leaves stop_reason null. Its content and every other text is
// SECRET.
func assistantRec(msgID, reqID, model, stop, usage string, sidechain bool) string {
	sr := "null"
	if stop != "" {
		sr = `"` + stop + `"`
	}
	side := `"isSidechain":false`
	if sidechain {
		side = `"isSidechain":true,"agentId":"a1b2c3"`
	}
	return `{"type":"assistant",` + side + `,"requestId":"` + reqID + `","timestamp":"2026-11-11T10:00:01.250Z",` +
		`"cwd":"/SECRET","message":{"id":"` + msgID + `","model":"` + model + `","role":"assistant","stop_reason":` + sr +
		`,"content":[{"type":"text","text":"SECRET answer"}],"usage":` + usage + `}}`
}

const testModel = "claude-opus-4-5"

func mustUsage(t *testing.T, line string) TranscriptLine {
	t.Helper()
	l, _ := DecodeTranscriptLine([]byte(line), true, true)
	return l
}

// TestUsageDecode covers the decoder's marker: ids, model, stop flag, UTC
// milliseconds, the five counts, and agentId only on a sidechain.
func TestUsageDecode(t *testing.T) {
	l, ok := DecodeTranscriptLine([]byte(assistantRec("msg_1", "req_1", testModel, "tool_use", usageCountsJSON(3, 40, 5, 7, 900), false)), true, true)
	want := UsageMarker{MessageID: "msg_1", RequestID: "req_1", Model: testModel, Stopped: true, At: 1794391201250,
		Usage: Usage{Input: 3, Output: 40, CacheWrite5m: 5, CacheWrite1h: 7, CacheRead: 900}}
	if !ok || l.Usage != want || l.UsageIssue != UsageNone {
		t.Fatalf("got %+v ok %v, want %+v", l, ok, want)
	}
	if l.TurnEnded {
		t.Error("tool_use ended the turn")
	}
	side := mustUsage(t, assistantRec("msg_2", "req_2", testModel, "", usageCountsJSON(1, 2, 0, 0, 0), true))
	if side.Usage.AgentID != "a1b2c3" || side.Usage.Stopped {
		t.Errorf("sidechain marker %+v", side.Usage)
	}
	// A main-thread record carrying agentId does not keep it.
	main := strings.Replace(assistantRec("msg_3", "req_3", testModel, "", usageCountsJSON(1, 2, 0, 0, 0), false),
		`"isSidechain":false`, `"isSidechain":false,"agentId":"zz9"`, 1)
	if m := mustUsage(t, main); m.Usage.AgentID != "" || m.Usage.MessageID != "msg_3" {
		t.Errorf("main marker %+v", m.Usage)
	}
	// An offset timestamp is converted to UTC milliseconds.
	off := strings.Replace(assistantRec("msg_4", "req_4", testModel, "", usageCountsJSON(0, 0, 0, 0, 0), false),
		"2026-11-11T10:00:01.250Z", "2026-11-11T12:00:01.250+02:00", 1)
	if m := mustUsage(t, off); m.Usage.At != 1794391201250 {
		t.Errorf("offset At %d", m.Usage.At)
	}
}

// TestUsageEndTurn: an end_turn assistant line yields both TurnEnded and a
// marker.
func TestUsageEndTurn(t *testing.T) {
	l, ok := DecodeTranscriptLine([]byte(assistantRec("m", "r", testModel, "end_turn", usageCountsJSON(1, 1, 0, 0, 0), false)), false, true)
	if !ok || !l.TurnEnded || l.Usage.MessageID != "m" || !l.Usage.Stopped {
		t.Errorf("got %+v", l)
	}
}

// TestUsageSkipped: a <synthetic> record, and an assistant record whose usage
// is absent or not an object, are neither a marker nor a problem.
func TestUsageSkipped(t *testing.T) {
	for _, line := range []string{
		assistantRec("m", "r", "<synthetic>", "", usageCountsJSON(0, 0, 0, 0, 0), false),
		assistantRec("m", "r", "<synthetic>", "", `{"input_tokens":"x"}`, false),
		assistantRec("m", "r", testModel, "", `null`, false),
		assistantRec("m", "r", testModel, "", `[1,2]`, false),
		assistantRec("m", "r", testModel, "", `"SECRET"`, false),
		`{"type":"assistant","requestId":"r","message":{"id":"m","model":"x","content":"SECRET"}}`,
		`{"type":"assistant","requestId":"r","message":"SECRET"}`,
		`{"type":"assistant","requestId":"r"}`,
		// Not an assistant record: never a usage marker.
		`{"type":"user","requestId":"r","timestamp":"2026-11-11T10:00:01Z","message":{"id":"m","model":"x","usage":` + usageCountsJSON(1, 1, 0, 0, 0) + `}}`,
	} {
		l, ok := DecodeTranscriptLine([]byte(line), true, true)
		if l.Usage != (UsageMarker{}) || l.UsageIssue != UsageNone {
			t.Errorf("%s: %+v", line, l)
		}
		if strings.Contains(line, `"type":"assistant"`) && ok {
			t.Errorf("%s: ok", line)
		}
	}
}

// TestUsageIssues covers the matrix's "No key" and "Unknown shape" rows.
func TestUsageIssues(t *testing.T) {
	good := usageCountsJSON(1, 2, 3, 4, 5)
	set := func(field, val string) string {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(good), &m); err != nil {
			t.Fatal(err)
		}
		if val == "" {
			delete(m, field)
		} else {
			m[field] = json.RawMessage(val)
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	noKey := []string{
		assistantRec("m", "", testModel, "end_turn", good, false),
		assistantRec("", "r", testModel, "", good, false),
		strings.Replace(assistantRec("m", "r", testModel, "", good, false), `"requestId":"r",`, ``, 1),
		strings.Replace(assistantRec("m", "r", testModel, "", good, false), `"requestId":"r"`, `"requestId":7`, 1),
		strings.Replace(assistantRec("m", "r", testModel, "", good, false), `"id":"m"`, `"id":null`, 1),
	}
	for _, line := range noKey {
		l, ok := DecodeTranscriptLine([]byte(line), true, true)
		if !ok || l.UsageIssue != UsageNoKey || l.Usage != (UsageMarker{}) {
			t.Errorf("%s: %+v ok %v, want no key", line, l, ok)
		}
	}
	var unknown []string
	for _, f := range []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"} {
		for _, v := range []string{"", "-1", "1.0", "1.5", "1e3", `"1"`, "true", "null", "99999999999999999999"} {
			unknown = append(unknown, assistantRec("m", "r", testModel, "", set(f, v), false))
		}
	}
	for _, v := range []string{"", "null", "[]", `"x"`, "5",
		`{"ephemeral_5m_input_tokens":3}`, `{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":4.0}`,
		`{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":3}`,
		`{"ephemeral_5m_input_tokens":8,"ephemeral_1h_input_tokens":-1}`} {
		unknown = append(unknown, assistantRec("m", "r", testModel, "", set("cache_creation", v), false))
	}
	unknown = append(unknown,
		assistantRec("m", "r", "", "", good, false),
		strings.Replace(assistantRec("m", "r", testModel, "", good, false), "2026-11-11T10:00:01.250Z", "yesterday", 1),
		strings.Replace(assistantRec("m", "r", testModel, "", good, false), `"timestamp":"2026-11-11T10:00:01.250Z",`, ``, 1),
		strings.Replace(assistantRec("m", "r", testModel, "", good, false), `"2026-11-11T10:00:01.250Z"`, `1794391201250`, 1),
	)
	for _, line := range unknown {
		l, ok := DecodeTranscriptLine([]byte(line), true, true)
		if !ok || l.UsageIssue != UsageUnknownShape || l.Usage != (UsageMarker{}) {
			t.Errorf("%s: %+v ok %v, want unknown shape", line, l, ok)
		}
	}
	// The good shape itself, and a split summing exactly, decode.
	if l := mustUsage(t, assistantRec("m", "r", testModel, "", good, false)); l.UsageIssue != UsageNone || l.Usage.CacheWrite1h != 4 {
		t.Errorf("good: %+v", l)
	}
}

// usageLines is every line the transcript tests use plus usage records of
// each kind, for the "Usage off" comparisons.
func usageLines(t *testing.T) [][]byte {
	lines := append(fixtureLines(t), recapFixtureLines(t)...)
	lines = append(lines, localFixtureLines(t)...)
	for _, l := range []string{caveat, cmdName, oldResult, compactSummary, skillCmd, bashInput, bashStdout,
		prompt, endTurn, sideEnd, toolUse, queuedCmd, bgStart, agentStart, resumeShape, notify("bx1", "completed"), taskStop("bx1"),
		recap("X."), localResult("recap", "<local-command-stdout>R.</local-command-stdout>"),
		assistantRec("m1", "r1", testModel, "end_turn", usageCountsJSON(1, 2, 3, 4, 5), false),
		assistantRec("m2", "r2", testModel, "end_turn", usageCountsJSON(1, 2, 3, 4, 5), true),
		assistantRec("m3", "r3", testModel, "", usageCountsJSON(1, 2, 3, 4, 5), false),
		assistantRec("m4", "", testModel, "end_turn", usageCountsJSON(1, 2, 3, 4, 5), false),
		assistantRec("m5", "r5", testModel, "", `{"input_tokens":1}`, false),
		assistantRec("m6", "r6", "<synthetic>", "stop_sequence", usageCountsJSON(0, 0, 0, 0, 0), false),
		"garbage", ""} {
		lines = append(lines, []byte(l))
	}
	return lines
}

// TestUsageOff covers the matrix's "Usage off" row: with usage false no line
// carries a marker or an issue, and with it true every line is otherwise the
// same.
func TestUsageOff(t *testing.T) {
	sawMarker, sawIssue := false, false
	for _, l := range usageLines(t) {
		for _, recaps := range []bool{false, true} {
			off, okOff := DecodeTranscriptLine(l, recaps, false)
			if off.Usage != (UsageMarker{}) || off.UsageIssue != UsageNone {
				t.Errorf("%s: usage off gave %+v", l, off)
			}
			on, okOn := DecodeTranscriptLine(l, recaps, true)
			if okOff != (off != TranscriptLine{}) || okOn != (on != TranscriptLine{}) {
				t.Errorf("%s: ok off %v on %v", l, okOff, okOn)
			}
			sawMarker = sawMarker || on.Usage != (UsageMarker{})
			sawIssue = sawIssue || on.UsageIssue != UsageNone
			on.Usage, on.UsageIssue = UsageMarker{}, UsageNone
			if on != off {
				t.Errorf("%s: off %+v, on %+v (usage cleared)", l, off, on)
			}
		}
	}
	if !sawMarker || !sawIssue {
		t.Errorf("marker %v issue %v: the lines did not exercise usage", sawMarker, sawIssue)
	}
}

// TestUsageLeavesBackgroundTasks: usage markers do not change
// BackgroundTasks.Apply.
func TestUsageLeavesBackgroundTasks(t *testing.T) {
	var off, on BackgroundTasks
	for _, l := range usageLines(t) {
		if m, ok := DecodeTranscriptLine(l, true, false); ok {
			off.Apply(m)
		}
		if m, ok := DecodeTranscriptLine(l, true, true); ok {
			on.Apply(m)
		}
		if !reflect.DeepEqual(off, on) {
			t.Fatalf("%s: off %+v, on %+v", l, off, on)
		}
	}
}

// TestUsagePrivacy: the marker has only ids, the model, the stop flag, a time
// and counts, and no text of the record leaves the decoder.
func TestUsagePrivacy(t *testing.T) {
	want := map[string]reflect.Kind{"MessageID": reflect.String, "RequestID": reflect.String, "Model": reflect.String,
		"AgentID": reflect.String, "Stopped": reflect.Bool, "At": reflect.Int64, "Usage": reflect.Struct}
	rt := reflect.TypeOf(UsageMarker{})
	if rt.NumField() != len(want) {
		t.Fatalf("UsageMarker has %d fields, want %d", rt.NumField(), len(want))
	}
	for i := 0; i < rt.NumField(); i++ {
		if f := rt.Field(i); want[f.Name] != f.Type.Kind() {
			t.Errorf("field %s %s not expected", f.Name, f.Type)
		}
	}
	ut := reflect.TypeOf(Usage{})
	for i := 0; i < ut.NumField(); i++ {
		if ut.Field(i).Type.Kind() != reflect.Int64 {
			t.Errorf("Usage.%s is %s", ut.Field(i).Name, ut.Field(i).Type)
		}
	}
	for _, l := range usageLines(t) {
		m, _ := DecodeTranscriptLine(l, true, true)
		if s := strings.Join([]string{m.Usage.MessageID, m.Usage.RequestID, m.Usage.Model, m.Usage.AgentID}, " "); strings.Contains(s, "SECRET") {
			t.Errorf("%s leaked %q", l, s)
		}
	}
}

func marker(msg, req string, out int64, stopped bool, at int64) UsageMarker {
	return UsageMarker{MessageID: msg, RequestID: req, Model: testModel, Stopped: stopped, At: at,
		Usage: Usage{Input: 1, Output: out, CacheWrite5m: 2, CacheWrite1h: 3, CacheRead: 4}}
}

// TestUsageLedger covers the matrix's ledger rows.
func TestUsageLedger(t *testing.T) {
	resp := func(m UsageMarker) Response { return responseOf(m) }
	type step struct {
		in   UsageMarker
		want []Response
	}
	a1, a2, a3 := marker("A", "rA", 5, false, 1), marker("A", "rA", 7, false, 2), marker("A", "rA", 9, true, 3)
	b1, b2s, b3 := marker("B", "rB", 1, false, 4), marker("B", "rB", 4, true, 5), marker("B", "rB", 4, true, 6)
	c1s := marker("C", "rC", 2, true, 7)
	zero := UsageMarker{MessageID: "Z", RequestID: "rZ", Model: testModel, Stopped: true, At: 8}
	side := marker("S", "rS", 3, true, 9)
	side.AgentID = "agent-x"
	for _, tc := range []struct {
		name        string
		steps       []step
		wantPending bool
	}{
		{"multi-record", []step{{a1, nil}, {a2, nil}, {a3, []Response{resp(a3)}}}, false},
		{"stop before last", []step{{b1, nil}, {b2s, []Response{resp(b2s)}}, {b3, nil}}, false},
		{"superseded", []step{
			{marker("A", "rA", 5, false, 1), nil}, {marker("A", "rA", 9, false, 2), nil},
			{b1, []Response{resp(marker("A", "rA", 9, false, 2))}}}, true},
		{"superseded keeps the largest output", []step{
			{marker("A", "rA", 9, false, 1), nil}, {marker("A", "rA", 5, false, 2), nil},
			{c1s, []Response{resp(marker("A", "rA", 9, false, 1)), resp(c1s)}}}, false},
		{"superseded takes the later on a tie", []step{
			{marker("A", "rA", 9, false, 1), nil}, {marker("A", "rA", 9, false, 2), nil},
			{b1, []Response{resp(marker("A", "rA", 9, false, 2))}}}, true},
		{"end of file", []step{{a1, nil}, {a2, nil}}, true},
		{"later records of a completed key ignored", []step{
			{a3, []Response{resp(a3)}}, {a1, nil}, {marker("A", "rA", 99, true, 10), nil}}, false},
		{"same message id, another request id, is another Response", []step{
			{a3, []Response{resp(a3)}}, {marker("A", "rX", 1, true, 11), []Response{resp(marker("A", "rX", 1, true, 11))}}}, false},
		{"all zero is returned", []step{{zero, []Response{resp(zero)}}}, false},
		{"subagent id carried", []step{{side, []Response{resp(side)}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var g UsageLedger
			for i, s := range tc.steps {
				if got := g.Add(s.in); !reflect.DeepEqual(got, s.want) {
					t.Errorf("step %d: got %+v, want %+v", i, got, s.want)
				}
			}
			if g.Pending() != tc.wantPending {
				t.Errorf("pending %v, want %v", g.Pending(), tc.wantPending)
			}
		})
	}
	if r := responseOf(side); r.SubagentID != "agent-x" || r.At != 9 || !r.Stopped || r.Output != 3 {
		t.Errorf("response %+v", r)
	}
}

// TestUsageLedgerFromLines folds decoded lines: a three-record Response
// stopping on its last record is one Response at that record's values and
// timestamp.
func TestUsageLedgerFromLines(t *testing.T) {
	recs := []string{
		assistantRec("msg_A", "req_A", testModel, "", usageCountsJSON(3, 1, 10, 0, 100), false),
		assistantRec("msg_A", "req_A", testModel, "", usageCountsJSON(3, 1, 10, 0, 100), false),
		strings.Replace(assistantRec("msg_A", "req_A", testModel, "end_turn", usageCountsJSON(3, 250, 10, 0, 100), false),
			"10:00:01.250Z", "10:00:09.000Z", 1),
	}
	var g UsageLedger
	var got []Response
	for _, r := range recs {
		l, ok := DecodeTranscriptLine([]byte(r), false, true)
		if !ok || l.UsageIssue != UsageNone {
			t.Fatalf("%s: %+v", r, l)
		}
		got = append(got, g.Add(l.Usage)...)
	}
	want := []Response{{MessageID: "msg_A", RequestID: "req_A", Model: testModel, Stopped: true, At: 1794391209000,
		Usage: Usage{Input: 3, Output: 250, CacheWrite5m: 10, CacheRead: 100}}}
	if !reflect.DeepEqual(got, want) || g.Pending() {
		t.Errorf("got %+v pending %v, want %+v", got, g.Pending(), want)
	}
}
