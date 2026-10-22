package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureLines is testdata/claude-transcript-2026-10-21.jsonl, a synthetic
// transcript shaped on the markers ADR-0009 names (every text invented, and
// every secret marked SECRET).
func fixtureLines(t *testing.T) [][]byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "claude-transcript-2026-10-21.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		out = append(out, append([]byte(nil), sc.Bytes()...))
	}
	return out
}

// fold applies every line of a transcript to a fresh BackgroundTasks.
func fold(lines ...string) BackgroundTasks {
	var b BackgroundTasks
	for _, l := range lines {
		if m, ok := DecodeTranscriptLine([]byte(l)); ok {
			b.Apply(m)
		}
	}
	return b
}

// The record shapes the tests build on.
const (
	prompt     = `{"type":"user","message":{"role":"user","content":"hello"}}`
	endTurn    = `{"type":"assistant","isSidechain":false,"message":{"stop_reason":"end_turn","content":[]}}`
	sideEnd    = `{"type":"assistant","isSidechain":true,"message":{"stop_reason":"end_turn"}}`
	toolUse    = `{"type":"assistant","message":{"stop_reason":"tool_use"}}`
	queuedCmd  = `{"type":"attachment","attachment":{"type":"queued_command","prompt":"x"}}`
	bgStart    = `{"type":"user","toolUseResult":{"backgroundTaskId":"bx1"}}`
	agentStart = `{"type":"user","toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"ag1"}}`
)

func notify(id, status string) string {
	s := ""
	if status != "" {
		s = `<status>` + status + `</status>`
	}
	return `{"type":"queue-operation","operation":"enqueue","content":"<task-notification><task-id>` + id + `</task-id>` + s + `</task-notification>"}`
}

// TestTranscriptFixture pins the shapes observed on 2026-10-21: after the
// fixture the subagent is open, the command closed, and the main turn over
// (a sidechain end_turn before it did not count).
func TestTranscriptFixture(t *testing.T) {
	var b BackgroundTasks
	var got []TranscriptLine
	for _, l := range fixtureLines(t) {
		if m, ok := DecodeTranscriptLine(l); ok {
			got = append(got, m)
			b.Apply(m)
		}
	}
	want := []TranscriptLine{
		{TurnStarted: true},
		{TurnStarted: true, StartID: "bk7q2x9", StartCommand: true},
		{TurnStarted: true, StartID: "a3f9c2d1e0b4a5968"},
		{TurnEnded: true},
		{EndID: "bk7q2x9"},
		{EndID: "bk7q2x9", EndStatus: "completed"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("markers\n%+v\nwant\n%+v", got, want)
	}
	if b.Count() != 1 || !b.TurnEnded() || b.OpenCommand() {
		t.Errorf("state: %d open, turn ended %v, command %v", b.Count(), b.TurnEnded(), b.OpenCommand())
	}
}

// TestTranscriptPrivacy covers the matrix's "Privacy" row: lines full of
// prompts, inputs, outputs and text give markers holding only ids, a status
// and flags; nothing of the text leaves the decoder.
func TestTranscriptPrivacy(t *testing.T) {
	// The marker type has only these fields, of these kinds.
	want := map[string]reflect.Kind{"StartID": reflect.String, "StartCommand": reflect.Bool,
		"EndID": reflect.String, "EndStatus": reflect.String, "TurnStarted": reflect.Bool, "TurnEnded": reflect.Bool}
	rt := reflect.TypeOf(TranscriptLine{})
	if rt.NumField() != len(want) {
		t.Fatalf("TranscriptLine has %d fields, want %d", rt.NumField(), len(want))
	}
	for i := 0; i < rt.NumField(); i++ {
		if f := rt.Field(i); want[f.Name] != f.Type.Kind() {
			t.Errorf("field %s %s not expected", f.Name, f.Type)
		}
	}
	lines := fixtureLines(t)
	// And hostile ones: text where an id or status goes, ids with spaces.
	lines = append(lines,
		[]byte(`{"type":"user","toolUseResult":{"backgroundTaskId":"SECRET text here"}}`),
		[]byte(`{"type":"user","toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"SECRET/../x"}}`),
		[]byte(notify("SECRET words", "completed")),
		[]byte(notify("ok1", "SECRET-status")),
		[]byte(`{"type":"queue-operation","operation":"enqueue","content":"SECRET <task-notification><task-id>x</task-id>"}`),
	)
	var b BackgroundTasks
	for _, l := range lines {
		m, ok := DecodeTranscriptLine(l)
		if !ok {
			continue
		}
		b.Apply(m)
		out := fmt.Sprintf("%+v %#v", m, m)
		if strings.Contains(out, "SECRET") {
			t.Errorf("a marker carries text: %s", out)
		}
		for _, s := range []string{m.StartID, m.EndID} {
			if s != "" && !taskID(s) {
				t.Errorf("id %q is not an id", s)
			}
		}
		if m.EndStatus != "" && !closingStatuses[m.EndStatus] {
			t.Errorf("status %q kept", m.EndStatus)
		}
	}
	if s := fmt.Sprintf("%+v %#v", b, b); strings.Contains(s, "SECRET") {
		t.Errorf("the state carries text: %s", s)
	}
	if j, _ := json.Marshal(b); string(j) != "{}" {
		t.Errorf("the state serialises as %s", j)
	}
}

// taskStop is TaskStop's tool result for id, shaped as observed on
// 2026-10-21 (all text invented).
func taskStop(id string) string {
	return `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"stopped"}]},` +
		`"toolUseResult":{"command":"sleep 99","message":"Successfully stopped task","task_id":"` + id + `","task_type":"local_bash"}}`
}

// TestTranscriptMatrix covers the matrix's decoder rows.
func TestTranscriptMatrix(t *testing.T) {
	for name, tc := range map[string]struct {
		lines     []string
		count     int
		turnEnded bool
	}{
		"bg command open, idle":            {[]string{prompt, bgStart, endTurn}, 1, true},
		"bg command closes":                {[]string{prompt, bgStart, endTurn, notify("bx1", "completed")}, 0, true},
		"failed and killed close too":      {[]string{bgStart, agentStart, notify("bx1", "failed"), notify("ag1", "killed")}, 0, false},
		"main turn over, subagent running": {[]string{prompt, agentStart, endTurn}, 1, true},
		"working with bg":                  {[]string{prompt, bgStart, toolUse}, 1, false},
		"notification without status":      {[]string{bgStart, notify("bx1", "")}, 1, false},
		"stopped by TaskStop":              {[]string{prompt, bgStart, taskStop("bx1"), endTurn}, 0, true},
		"TaskStop of another id":           {[]string{prompt, bgStart, taskStop("zz9"), endTurn}, 1, true},
		"unknown status stays open":        {[]string{bgStart, notify("bx1", "running")}, 1, false},
		"other id's notification":          {[]string{bgStart, notify("zz9", "completed")}, 1, false},
		"sidechain end_turn":               {[]string{prompt, sideEnd}, 0, false},
		"stop_sequence ends a turn":        {[]string{prompt, agentStart, `{"type":"assistant","message":{"stop_reason":"stop_sequence"}}`}, 1, true},
		"max_tokens does not":              {[]string{prompt, `{"type":"assistant","message":{"stop_reason":"max_tokens"}}`}, 0, false},
		"new prompt":                       {[]string{prompt, endTurn, prompt}, 0, false},
		"tool result starts a turn":        {[]string{endTurn, bgStart}, 1, false},
		"queued command starts a turn":     {[]string{endTurn, queuedCmd}, 0, false},
		"sidechain user does not start":    {[]string{endTurn, `{"type":"user","isSidechain":true,"message":{}}`}, 0, true},
		"duplicate start counts once":      {[]string{bgStart, bgStart}, 1, false},
		"undecodable line":                 {[]string{bgStart, `{"type":"user",`, "garbage", "", `[1,2]`, `{"type":"queue-operation","operation":"enqueue","content":5}`, endTurn}, 1, true},
		"tool result not an object":        {[]string{`{"type":"user","toolUseResult":"Error: SECRET"}`, endTurn}, 0, true},
		"isAsync without async_launched":   {[]string{`{"type":"user","toolUseResult":{"isAsync":true,"status":"completed","agentId":"ag1"}}`}, 0, false},
		"backgroundTaskId on a non-user":   {[]string{`{"type":"assistant","toolUseResult":{"backgroundTaskId":"bx1"}}`}, 0, false},
		"dequeue is not an end":            {[]string{bgStart, `{"type":"queue-operation","operation":"dequeue","content":"<task-notification><task-id>bx1</task-id><status>completed</status>"}`}, 1, false},
	} {
		b := fold(tc.lines...)
		if b.Count() != tc.count || b.TurnEnded() != tc.turnEnded {
			t.Errorf("%s: %d open, turn ended %v; want %d, %v", name, b.Count(), b.TurnEnded(), tc.count, tc.turnEnded)
		}
	}
	for _, l := range []string{"garbage", "", `{"type":"user",`, `{"type":"summary","summary":"x"}`, toolUse} {
		if m, ok := DecodeTranscriptLine([]byte(l)); ok {
			t.Errorf("%q decoded as %+v", l, m)
		}
	}
}

// TestBackgroundTasksCap: at most MaxOpenTasks are kept, the oldest dropped;
// and a Clone shares nothing.
func TestBackgroundTasksCap(t *testing.T) {
	var b BackgroundTasks
	for i := 0; i < MaxOpenTasks+10; i++ {
		b.Apply(TranscriptLine{StartID: fmt.Sprintf("t%d", i), StartCommand: i == 0})
	}
	if b.Count() != MaxOpenTasks || b.index("t0") >= 0 || b.index("t9") >= 0 || b.index("t10") < 0 || b.OpenCommand() {
		t.Errorf("%d open; t0 %d t9 %d t10 %d", b.Count(), b.index("t0"), b.index("t9"), b.index("t10"))
	}
	c := b.Clone()
	c.Apply(TranscriptLine{EndID: "t11", EndStatus: "completed"})
	c.Apply(TranscriptLine{TurnEnded: true})
	if b.index("t11") < 0 || b.TurnEnded() || c.index("t11") >= 0 || b.Count() != MaxOpenTasks {
		t.Error("a clone changed the original")
	}
}
