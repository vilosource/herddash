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
	"unicode/utf8"
)

// fixtureLines is testdata/claude-transcript-2026-10-21.jsonl, a synthetic
// transcript shaped on the markers ADR-0009 names (every text invented, and
// every secret marked SECRET).
func fixtureLines(t *testing.T) [][]byte {
	t.Helper()
	return fixtureFile(t, "claude-transcript-2026-10-21.jsonl")
}

// recapFixtureLines is testdata/claude-transcript-2026-10-22.jsonl, a
// synthetic transcript shaped on the session recap records observed on
// 2026-10-22 (ADR-0010): two turns, each followed by its duration record and
// an away_summary. Every text is invented; the recaps' are marked RECAP-ONE
// and RECAP-TWO, every other text SECRET.
func recapFixtureLines(t *testing.T) [][]byte {
	t.Helper()
	return fixtureFile(t, "claude-transcript-2026-10-22.jsonl")
}

func fixtureFile(t *testing.T, name string) [][]byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
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

// fold applies every line of a transcript to a fresh BackgroundTasks, with
// recaps off.
func fold(lines ...string) BackgroundTasks {
	return foldRecaps(false, lines...)
}

// foldRecaps is fold with recaps on or off (ADR-0010).
func foldRecaps(recaps bool, lines ...string) BackgroundTasks {
	var b BackgroundTasks
	for _, l := range lines {
		if m, ok := DecodeTranscriptLine([]byte(l), recaps); ok {
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
		if m, ok := DecodeTranscriptLine(l, true); ok {
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
		"EndID": reflect.String, "EndStatus": reflect.String, "TurnStarted": reflect.Bool, "TurnEnded": reflect.Bool,
		"Recap": reflect.String, "CommandPending": reflect.Bool, "CommandResult": reflect.Bool}
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
		[]byte(resumeShape),
		[]byte(resumed("SECRET words")),
		[]byte(`{"type":"user","toolUseResult":{"success":true,"message":"SECRET","resumedAgentId":"SECRET/../x","pin":{"text":"SECRET"}}}`),
		[]byte(`{"type":"user","toolUseResult":{"success":"true","message":"SECRET","resumedAgentId":"ok2"}}`),
	)
	// Recaps on: the recap records' text is the only text that may leave
	// (TestRecapPrivacy covers it); none of these lines is one.
	lines = append(lines,
		[]byte(`{"type":"system","subtype":"away_summary","isSidechain":true,"content":"SECRET sidechain"}`),
		[]byte(`{"type":"system","subtype":"turn_duration","content":"SECRET other subtype"}`),
		[]byte(`{"type":"system","subtype":"away_summary","content":["SECRET array"]}`),
		[]byte(`{"type":"user","subtype":"away_summary","content":"SECRET user"}`),
	)
	// Local commands: their text never leaves, nor does a result that is not
	// /recap's.
	lines = append(lines, localFixtureLines(t)...)
	lines = append(lines,
		[]byte(caveat), []byte(`{"type":"user","message":{"content":"<command-name>SECRET</command-name>"}}`),
		[]byte(`{"type":"system","subtype":"local_command","commandRun":{"command":"usage","args":"SECRET"},"content":"<local-command-stdout>SECRET</local-command-stdout>"}`),
		[]byte(`{"type":"system","subtype":"local_command","commandRun":{"command":"recap"},"content":"SECRET unwrapped"}`),
		[]byte(`{"type":"system","subtype":"local_command","content":"<local-command-stdout>SECRET no commandRun</local-command-stdout>"}`),
		[]byte(`{"type":"user","message":{"content":"<local-command-stdout>SECRET old result</local-command-stdout>"}}`),
	)
	// The recap fixture last, so its newest recap is the state's; then
	// non-recap system lines, which must leave it as it is.
	lines = append(lines, recapFixtureLines(t)...)
	lines = append(lines,
		[]byte(`{"type":"system","subtype":"away_summary","isSidechain":true,"content":"SECRET sidechain"}`),
		[]byte(`{"type":"system","subtype":"turn_duration","content":"SECRET other subtype"}`),
		[]byte(`{"type":"system","subtype":"away_summary","content":["SECRET array"]}`),
	)
	var b BackgroundTasks
	for _, l := range lines {
		m, ok := DecodeTranscriptLine(l, true)
		if !ok {
			continue
		}
		recapLine := strings.Contains(string(l), `"away_summary"`) ||
			strings.Contains(string(l), `"local_command"`) && strings.Contains(string(l), `"commandRun":{"command":"recap"`)
		if strings.Contains(m.Recap, "SECRET") || (m.Recap != "" && !recapLine) {
			t.Errorf("a recap carries other text: %q", m.Recap)
		}
		b.Apply(m)
		other := m
		other.Recap = ""
		out := fmt.Sprintf("%+v %#v", other, other)
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
	// The state holds the newest recap's text and nothing else: no other
	// text, and no earlier recap.
	const newest = "I opened RECAP-TWO the pull request for the flaky test fix. Next I'll wait for review."
	if b.Recap() != newest {
		t.Errorf("state recap %q, want %q", b.Recap(), newest)
	}
	s := strings.ReplaceAll(fmt.Sprintf("%+v %#v", b, b), newest, "")
	if strings.Contains(s, "SECRET") || strings.Contains(s, "RECAP-") || strings.Contains(s, "Next I'll") {
		t.Errorf("the state carries other text: %s", s)
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
		"stopped on resume closes":         {[]string{prompt, bgStart, endTurn, notify("bx1", "stopped")}, 0, true},
		"stopped closes only its id":       {[]string{bgStart, agentStart, notify("bx1", "stopped")}, 1, false},
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
		if m, ok := DecodeTranscriptLine([]byte(l), true); ok {
			t.Errorf("%q decoded as %+v", l, m)
		}
	}
}

// resumeStopped is the notification a resumed session enqueues for a task
// the previous process left with no completion record, shaped on Claude
// Code 2.1.292 (observed 2026-10-23): <tool-use-id>, <summary> and <note>
// around the id and status. Every text is invented and marked SECRET.
const resumeStopped = `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-10-23T09:00:01.000Z",` +
	`"sessionId":"SECRET-session","content":"<task-notification>\n<task-id>bx1</task-id>\n` +
	`<tool-use-id>toolu_SECRET</tool-use-id>\n<status>stopped</status>\n` +
	`<summary>SECRET command didn't finish before the previous session ended</summary>\n` +
	`<note>SECRET no completion record was found.</note>\n</task-notification>"}`

// TestTranscriptResumeStoppedShape covers the matrix's "Observed shape" row:
// the full 2.1.292 notification decodes to the id and stopped, nothing more,
// and closes the task; with any status but a closing one it stays open.
func TestTranscriptResumeStoppedShape(t *testing.T) {
	m, ok := DecodeTranscriptLine([]byte(resumeStopped), true)
	if want := (TranscriptLine{EndID: "bx1", EndStatus: "stopped"}); !ok || m != want {
		t.Errorf("decoded %+v, %v; want %+v", m, ok, want)
	}
	if b := fold(prompt, bgStart, endTurn, resumeStopped); b.Count() != 0 || b.OpenCommand() {
		t.Errorf("%d open after stopped", b.Count())
	}
	for _, s := range []string{"<status>running</status>", ""} {
		l := strings.Replace(resumeStopped, "<status>stopped</status>", s, 1)
		if m, _ := DecodeTranscriptLine([]byte(l), true); m.EndStatus != "" || m.EndID != "bx1" {
			t.Errorf("status %q decoded as %+v", s, m)
		}
		if b := fold(bgStart, l); b.Count() != 1 {
			t.Errorf("status %q: %d open, want 1", s, b.Count())
		}
	}
}

// resumeShape is the user record Claude Code writes when SendMessage resumes
// a finished subagent, shaped on 2.1.292 (observed 2026-10-23, Story 5.16):
// a tool_result block array in message, and a toolUseResult with exactly
// success, message, resumedAgentId and pin. Every text is invented and
// marked SECRET.
const resumeShape = `{"parentUuid":"SECRET-uuid","isSidechain":false,"type":"user",` +
	`"message":{"role":"user","content":[{"tool_use_id":"toolu_SECRET","type":"tool_result",` +
	`"content":[{"type":"text","text":"SECRET resuming the agent in the background"}]}]},` +
	`"toolUseResult":{"success":true,"message":"Resuming agent SECRET in the background",` +
	`"resumedAgentId":"ag1","pin":{"id":"SECRET-pin","name":"SECRET pin","ref":"SECRET-ref"}},` +
	`"sessionId":"SECRET-session","timestamp":"2026-10-23T09:00:00.000Z"}`

// resumed is a minimal resume record for id.
func resumed(id string) string {
	return `{"type":"user","toolUseResult":{"success":true,"message":"Resuming agent","resumedAgentId":"` + id + `","pin":{}}}`
}

// TestTranscriptResumedSubagent covers Story 5.16's matrix: a successful
// resume reopens the subagent it names, as a subagent, and its next closing
// notification closes it; a failed or malformed resume opens nothing.
func TestTranscriptResumedSubagent(t *testing.T) {
	m, ok := DecodeTranscriptLine([]byte(resumeShape), true)
	if want := (TranscriptLine{StartID: "ag1", TurnStarted: true}); !ok || m != want {
		t.Errorf("decoded %+v, %v; want %+v", m, ok, want)
	}
	long := strings.Repeat("a", 129)
	for name, tc := range map[string]struct {
		lines     []string
		count     int
		turnEnded bool
	}{
		"resume after completion":        {[]string{prompt, agentStart, notify("ag1", "completed"), resumeShape}, 1, false},
		"resume closes: completed":       {[]string{resumeShape, notify("ag1", "completed")}, 0, false},
		"resume closes: failed":          {[]string{resumeShape, notify("ag1", "failed")}, 0, false},
		"resume closes: killed":          {[]string{resumeShape, notify("ag1", "killed")}, 0, false},
		"resume closes: stopped":         {[]string{resumeShape, notify("ag1", "stopped")}, 0, false},
		"resume, notification no status": {[]string{resumeShape, notify("ag1", "")}, 1, false},
		"success false":                  {[]string{`{"type":"user","toolUseResult":{"success":false,"resumedAgentId":"ag1"}}`}, 0, false},
		"success the string true":        {[]string{`{"type":"user","toolUseResult":{"success":"true","resumedAgentId":"ag1"}}`}, 0, false},
		"success absent":                 {[]string{`{"type":"user","toolUseResult":{"resumedAgentId":"ag1"}}`}, 0, false},
		"no id":                          {[]string{`{"type":"user","toolUseResult":{"success":true,"message":"x"}}`}, 0, false},
		"empty id":                       {[]string{resumed("")}, 0, false},
		"id not a string":                {[]string{`{"type":"user","toolUseResult":{"success":true,"resumedAgentId":7}}`}, 0, false},
		"id with a slash":                {[]string{resumed("a/b")}, 0, false},
		"id with a space":                {[]string{resumed("a b")}, 0, false},
		"id too long":                    {[]string{resumed(long)}, 0, false},
		"id of 128 opens":                {[]string{resumed(long[:128])}, 1, false},
		"resume of an open id":           {[]string{agentStart, resumed("ag1")}, 1, false},
		// The operator's report: one subagent still running, two finished
		// ones resumed: three open, not one.
		"one running, two resumed": {[]string{prompt, agentStart, `{"type":"user","toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"ag2"}}`,
			`{"type":"user","toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"ag3"}}`,
			notify("ag2", "completed"), notify("ag3", "completed"), resumed("ag2"), resumed("ag3"), endTurn}, 3, true},
		"main turn over":       {[]string{prompt, resumeShape, endTurn}, 1, true},
		"resume on a non-user": {[]string{`{"type":"assistant","toolUseResult":{"success":true,"resumedAgentId":"ag1"}}`}, 0, false},
	} {
		b := fold(tc.lines...)
		if b.Count() != tc.count || b.TurnEnded() != tc.turnEnded || b.OpenCommand() {
			t.Errorf("%s: %d open, turn ended %v, command %v; want %d, %v", name, b.Count(), b.TurnEnded(), b.OpenCommand(), tc.count, tc.turnEnded)
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

// recap is an away_summary record carrying content, shaped as observed on
// 2026-10-22 (ADR-0010).
func recap(content string) string {
	c, _ := json.Marshal(content)
	return `{"type":"system","subtype":"away_summary","isSidechain":false,"isMeta":false,"level":"info","content":` + string(c) + `}`
}

// TestRecapFixture pins the recap records observed on 2026-10-22: with
// recaps on, each away_summary is decoded with its suffix removed, the
// second prompt clears the first, and the newest is kept; with recaps off,
// no recap record is a marker at all.
func TestRecapFixture(t *testing.T) {
	var got []string
	var b BackgroundTasks
	for _, l := range recapFixtureLines(t) {
		if m, ok := DecodeTranscriptLine(l, true); ok {
			b.Apply(m)
			if m.Recap != "" {
				got = append(got, m.Recap)
			}
		}
	}
	want := []string{
		"I fixed RECAP-ONE the flaky test and pushed the branch. Next I'll open the pull request.",
		"I opened RECAP-TWO the pull request for the flaky test fix. Next I'll wait for review.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recaps %q, want %q", got, want)
	}
	if b.Recap() != want[1] || !b.TurnEnded() {
		t.Errorf("state recap %q, turn ended %v", b.Recap(), b.TurnEnded())
	}
	b = BackgroundTasks{}
	for _, l := range recapFixtureLines(t) {
		m, ok := DecodeTranscriptLine(l, false)
		if ok {
			b.Apply(m)
		}
		if strings.Contains(string(l), "away_summary") && (ok || m.Recap != "") {
			t.Errorf("recaps off: %s decoded as %+v", l, m)
		}
	}
	if b.Recap() != "" {
		t.Errorf("recaps off: state recap %q", b.Recap())
	}
}

// TestRecapMatrix covers the spec's decoder and state rows for the recap
// (ADR-0010).
func TestRecapMatrix(t *testing.T) {
	x := recap("X. (disable recaps in /config)")
	y := recap("  Y.\n (disable recaps in /config)  ")
	for name, tc := range map[string]struct {
		recaps bool
		lines  []string
		want   string
	}{
		"recap after turn":           {true, []string{prompt, endTurn, x}, "X."},
		"key off":                    {false, []string{prompt, endTurn, x}, ""},
		"new turn: prompt":           {true, []string{endTurn, x, prompt}, ""},
		"new turn: tool result":      {true, []string{endTurn, x, bgStart}, ""},
		"new turn: queued command":   {true, []string{endTurn, x, queuedCmd}, ""},
		"sidechain user keeps it":    {true, []string{endTurn, x, `{"type":"user","isSidechain":true,"message":{}}`}, "X."},
		"turn end keeps it":          {true, []string{x, endTurn}, "X."},
		"newest wins":                {true, []string{endTurn, x, recap("Z.")}, "Z."},
		"recap after a cleared one":  {true, []string{endTurn, x, prompt, endTurn, recap("Z.")}, "Z."},
		"trimmed, inner space kept":  {true, []string{y}, "Y."},
		"no suffix":                  {true, []string{recap(" plain recap ")}, "plain recap"},
		"suffix mid-text stays":      {true, []string{recap("a (disable recaps in /config) b")}, "a (disable recaps in /config) b"},
		"sidechain away_summary":     {true, []string{`{"type":"system","subtype":"away_summary","isSidechain":true,"content":"S."}`}, ""},
		"other subtype":              {true, []string{`{"type":"system","subtype":"turn_duration","content":"S."}`}, ""},
		"content not a string":       {true, []string{`{"type":"system","subtype":"away_summary","content":{"text":"S."}}`}, ""},
		"content an array":           {true, []string{`{"type":"system","subtype":"away_summary","content":["S."]}`}, ""},
		"content absent":             {true, []string{`{"type":"system","subtype":"away_summary"}`}, ""},
		"empty after trim":           {true, []string{recap("  (disable recaps in /config) ")}, ""},
		"empty recap keeps the last": {true, []string{x, recap("   ")}, "X."},
		"not a system record":        {true, []string{`{"type":"assistant","subtype":"away_summary","content":"S."}`}, ""},
	} {
		if got := foldRecaps(tc.recaps, tc.lines...).Recap(); got != tc.want {
			t.Errorf("%s: recap %q, want %q", name, got, tc.want)
		}
	}
	for _, l := range []string{recap(""), recap(" (disable recaps in /config)"),
		`{"type":"system","subtype":"away_summary","isSidechain":true,"content":"S."}`} {
		if m, ok := DecodeTranscriptLine([]byte(l), true); ok {
			t.Errorf("%s decoded as %+v", l, m)
		}
	}
	if m, ok := DecodeTranscriptLine([]byte(x), false); ok || m != (TranscriptLine{}) {
		t.Errorf("recaps off: decoded as %+v", m)
	}
}

// TestRecapBounds covers the matrix's "Bounds" row: a 1,000-rune recap of
// multibyte runes is cut to MaxRecapRunes whole runes, valid UTF-8.
func TestRecapBounds(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString([]string{"é", "😀", "a", "語"}[i%4])
	}
	long := sb.String()
	got := foldRecaps(true, recap(long+" (disable recaps in /config)")).Recap()
	if n := utf8.RuneCountInString(got); n != MaxRecapRunes {
		t.Errorf("%d runes, want %d", n, MaxRecapRunes)
	}
	if !utf8.ValidString(got) || !strings.HasPrefix(long, got) {
		t.Errorf("cut recap is not a valid prefix: %q", got)
	}
	exact := string([]rune(long)[:MaxRecapRunes])
	if got := foldRecaps(true, recap(exact)).Recap(); got != exact {
		t.Error("a recap of exactly the cap was changed")
	}
}

// TestRecapClone: a Clone carries the recap and shares nothing.
func TestRecapClone(t *testing.T) {
	b := foldRecaps(true, endTurn, recap("X."))
	c := b.Clone()
	if c.Recap() != "X." {
		t.Errorf("clone recap %q", c.Recap())
	}
	c.Apply(TranscriptLine{TurnStarted: true})
	if b.Recap() != "X." || c.Recap() != "" {
		t.Error("a clone changed the original")
	}
}

// TestRecapOffChangesNothingElse: with recaps off every line decodes as it
// does with them on, recap aside; a line whose only marker is a recap is no
// marker at all with them off.
func TestRecapOffChangesNothingElse(t *testing.T) {
	lines := append(fixtureLines(t), recapFixtureLines(t)...)
	lines = append(lines, localFixtureLines(t)...)
	for _, l := range []string{caveat, cmdName, oldResult, oldStderr, compactSummary, skillCmd, bashInput, bashStdout,
		localResult("usage", "<local-command-stderr>E</local-command-stderr>"),
		localResult("usage", "<local-command-stdout>O</local-command-stdout>"),
		localResult("recap", "<local-command-stdout>R.</local-command-stdout>"), localResult("recap", "R."), prompt, endTurn, sideEnd, toolUse, queuedCmd, bgStart, agentStart, resumeShape, notify("bx1", "completed"),
		notify("bx1", ""), taskStop("bx1"), recap("X. (disable recaps in /config)"), recap(""),
		`{"type":"system","subtype":"away_summary","isSidechain":true,"content":"S."}`,
		`{"type":"system","subtype":"turn_duration","content":"S."}`, "garbage", ""} {
		lines = append(lines, []byte(l))
	}
	sawRecap := false
	for _, l := range lines {
		off, okOff := DecodeTranscriptLine(l, false)
		on, okOn := DecodeTranscriptLine(l, true)
		if off.Recap != "" {
			t.Errorf("%s: recap %q with recaps off", l, off.Recap)
		}
		onlyRecap := on.Recap != ""
		sawRecap = sawRecap || onlyRecap
		on.Recap = ""
		if off != on {
			t.Errorf("%s: off %+v, on %+v (recap cleared)", l, off, on)
		}
		if want := okOn && !(onlyRecap && on == TranscriptLine{}); okOff != want {
			t.Errorf("%s: ok %v with recaps off, want %v", l, okOff, want)
		}
	}
	if !sawRecap {
		t.Error("no line carried a recap")
	}
}
