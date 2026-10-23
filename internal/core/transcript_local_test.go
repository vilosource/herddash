package core

import (
	"encoding/json"
	"reflect"
	"testing"
)

// localFixtureLines is testdata/claude-transcript-2026-10-23.jsonl, a
// synthetic transcript shaped on the local-command records of Claude Code
// 2.1.292 (Story 5.14): a turn, its end, an away_summary, a /usage triple, a
// /recap triple and a skill command. Every text is invented; the recaps' are
// marked RECAP-AWAY and RECAP-MANUAL, every other text SECRET.
func localFixtureLines(t *testing.T) [][]byte {
	t.Helper()
	return fixtureFile(t, "claude-transcript-2026-10-23.jsonl")
}

// The local-command record shapes observed in Claude Code 2.1.292 (Story
// 5.14); every text a placeholder.
const (
	caveat         = `{"type":"user","isSidechain":false,"isMeta":true,"message":{"role":"user","content":"<local-command-caveat>CAVEAT</local-command-caveat>"}}`
	cmdName        = `{"type":"user","isSidechain":false,"message":{"role":"user","content":"<command-name>/cmd</command-name>\n<command-message>cmd</command-message>"}}`
	oldResult      = `{"type":"user","isSidechain":false,"message":{"role":"user","content":"<local-command-stdout>OUT</local-command-stdout>"}}`
	compactSummary = `{"type":"user","isSidechain":false,"isCompactSummary":true,"isVisibleInTranscriptOnly":true,"message":{"role":"user","content":"SUMMARY"}}`
	skillCmd       = `{"type":"user","isSidechain":false,"message":{"role":"user","content":"<command-message>skill</command-message>\n<command-name>/skill</command-name>"}}`
)

// localResult is a local_command result of command with content, as
// observed from Claude Code 2.1.273 on.
func localResult(command string, content any) string {
	c, _ := json.Marshal(content)
	return `{"type":"system","subtype":"local_command","isSidechain":false,"isMeta":false,"level":"info",` +
		`"commandRun":{"command":"` + command + `","args":""},"content":` + string(c) + `}`
}

// recapTriple is a manual /recap's three records, its stdout text.
func recapTriple(text string) []string {
	return []string{caveat, cmdName, localResult("recap", "<local-command-stdout>"+text+"</local-command-stdout>")}
}

// usageTriple is a /usage's three records.
func usageTriple() []string {
	return []string{caveat, cmdName, localResult("usage", "<local-command-stdout>OUT</local-command-stdout>")}
}

func cat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestLocalCommandFixture pins the local-command records of Claude Code
// 2.1.292: the caveat is no marker, the <command-name> record is pending, a
// result cancels it, /recap's result carries the recap, and a skill command
// starts a turn.
func TestLocalCommandFixture(t *testing.T) {
	var got []TranscriptLine
	var b BackgroundTasks
	for _, l := range localFixtureLines(t) {
		if m, ok := DecodeTranscriptLine(l, true); ok {
			got = append(got, m)
			b.Apply(m)
		}
	}
	const manual = "I tidied RECAP-MANUAL the parser and the tests pass. Next I'll open the pull request."
	want := []TranscriptLine{
		{TurnStarted: true},
		{TurnEnded: true},
		{Recap: "I tidied RECAP-AWAY the parser. Next I'll run the tests."},
		{CommandPending: true},
		{CommandResult: true},
		{CommandPending: true},
		{CommandResult: true, Recap: manual},
		{TurnStarted: true},
		{TurnStarted: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("markers\n%+v\nwant\n%+v", got, want)
	}
	if b.Recap() != "" || b.TurnEnded() {
		t.Errorf("after the skill command: recap %q, turn ended %v", b.Recap(), b.TurnEnded())
	}
	// Up to the skill command: the manual recap, turn still ended.
	lines := localFixtureLines(t)
	b = BackgroundTasks{}
	for _, l := range lines[:len(lines)-2] {
		if m, ok := DecodeTranscriptLine(l, true); ok {
			b.Apply(m)
		}
	}
	if b.Recap() != manual || !b.TurnEnded() || b.pending {
		t.Errorf("after /recap: recap %q, turn ended %v, pending %v", b.Recap(), b.TurnEnded(), b.pending)
	}
	// Up to /usage: the away_summary kept, turn still ended.
	b = BackgroundTasks{}
	for _, l := range lines[:7] {
		if m, ok := DecodeTranscriptLine(l, true); ok {
			b.Apply(m)
		}
	}
	if b.Recap() != want[2].Recap || !b.TurnEnded() || b.pending {
		t.Errorf("after /usage: recap %q, turn ended %v, pending %v", b.Recap(), b.TurnEnded(), b.pending)
	}
}

// TestLocalCommandMatrix covers the spec's I/O matrix (Story 5.14).
func TestLocalCommandMatrix(t *testing.T) {
	x := recap("X. (disable recaps in /config)")
	for name, tc := range map[string]struct {
		recaps    bool
		lines     []string
		recap     string
		turnEnded bool
		pending   bool
	}{
		"manual recap":                    {true, cat([]string{prompt, endTurn}, recapTriple("R.")), "R.", true, false},
		"manual recap, turn running":      {true, cat([]string{prompt}, recapTriple("R.")), "R.", false, false},
		"manual recap replaces":           {true, cat([]string{prompt, endTurn, x}, recapTriple("R.")), "R.", true, false},
		"manual recap trimmed":            {true, cat([]string{endTurn}, recapTriple("  R. (disable recaps in /config) ")), "R.", true, false},
		"local command keeps recap":       {true, cat([]string{prompt, endTurn, x}, usageTriple()), "X.", true, false},
		"old local result":                {true, []string{endTurn, x, caveat, cmdName, oldResult}, "X.", true, false},
		"confirmed by a prompt":           {true, []string{endTurn, x, cmdName, prompt}, "", false, false},
		"confirmed by a tool result":      {true, []string{endTurn, x, cmdName, bgStart}, "", false, false},
		"confirmed by a queued cmd":       {true, []string{endTurn, x, cmdName, queuedCmd}, "", false, false},
		"confirmed by end_turn":           {true, []string{endTurn, x, cmdName, endTurn}, "", true, false},
		"notification keeps pending":      {true, []string{endTurn, x, cmdName, notify("bx1", "completed")}, "X.", true, true},
		"notification then result":        {true, []string{endTurn, x, caveat, cmdName, notify("bx1", "completed"), usageTriple()[2]}, "X.", true, false},
		"recap keeps pending":             {true, []string{endTurn, x, cmdName, recap("Z.")}, "Z.", true, true},
		"second command does not confirm": {true, []string{endTurn, x, cmdName, cmdName}, "X.", true, true},
		"compact keeps recap":             {true, []string{endTurn, x, compactSummary, caveat, cmdName, oldResult}, "X.", true, false},
		"pending at end":                  {true, []string{endTurn, x, caveat, cmdName}, "X.", true, true},
		"attachment does not confirm": {true, cat([]string{endTurn, x, caveat, cmdName,
			`{"type":"attachment","attachment":{"type":"skill_listing"}}`}, usageTriple()[2:]), "X.", true, false},
		"skill command":             {true, []string{endTurn, x, skillCmd}, "", false, false},
		"recaps off":                {false, cat([]string{prompt, endTurn}, recapTriple("R.")), "", true, false},
		"recaps off keeps the turn": {false, cat([]string{prompt, endTurn}, usageTriple()), "", true, false},
		"caveat alone":              {true, []string{endTurn, x, caveat}, "X.", true, false},
		"caveat not meta starts":    {true, []string{endTurn, x, `{"type":"user","message":{"content":"<local-command-caveat>C</local-command-caveat>"}}`}, "", false, false},
		"meta command-name starts":  {true, []string{endTurn, x, `{"type":"user","isMeta":true,"message":{"content":"<command-name>/c</command-name>"}}`}, "", false, false},
		"sidechain command-name":    {true, []string{endTurn, x, `{"type":"user","isSidechain":true,"message":{"content":"<command-name>/c</command-name>"}}`}, "X.", true, false},
		"bad: content not a string": {true, cat([]string{endTurn, x}, []string{caveat, cmdName, localResult("recap", []string{"R."})}), "X.", true, false},
		"bad: no stdout tags":       {true, cat([]string{endTurn, x}, []string{caveat, cmdName, localResult("recap", "R.")}), "X.", true, false},
		"bad: no closing tag":       {true, cat([]string{endTurn, x}, []string{caveat, cmdName, localResult("recap", "<local-command-stdout>R.")}), "X.", true, false},
		"bad: empty stdout":         {true, cat([]string{endTurn, x}, recapTriple("  ")), "X.", true, false},
		"bad: no commandRun": {true, []string{endTurn, x, caveat, cmdName,
			`{"type":"system","subtype":"local_command","content":"<local-command-stdout>R.</local-command-stdout>"}`}, "X.", true, false},
		// A sidechain result is no marker, so the command stays pending.
		"bad: sidechain": {true, []string{endTurn, x, caveat, cmdName,
			`{"type":"system","subtype":"local_command","isSidechain":true,"commandRun":{"command":"recap"},"content":"<local-command-stdout>R.</local-command-stdout>"}`}, "X.", true, true},
	} {
		b := foldRecaps(tc.recaps, tc.lines...)
		if b.Recap() != tc.recap || b.TurnEnded() != tc.turnEnded || b.pending != tc.pending {
			t.Errorf("%s: recap %q, turn ended %v, pending %v; want %q, %v, %v",
				name, b.Recap(), b.TurnEnded(), b.pending, tc.recap, tc.turnEnded, tc.pending)
		}
	}
	// A bad /recap result is still a local result, carrying nothing else.
	for _, l := range []string{localResult("recap", []string{"R."}), localResult("recap", "R."),
		localResult("recap", "<local-command-stdout> </local-command-stdout>"), localResult("recap", nil)} {
		if m, ok := DecodeTranscriptLine([]byte(l), true); !ok || m != (TranscriptLine{CommandResult: true}) {
			t.Errorf("%s decoded as %+v, %v", l, m, ok)
		}
	}
	// The caveat and a sidechain result are no marker at all.
	for _, l := range []string{caveat,
		`{"type":"system","subtype":"local_command","isSidechain":true,"commandRun":{"command":"recap"},"content":"<local-command-stdout>R.</local-command-stdout>"}`} {
		if m, ok := DecodeTranscriptLine([]byte(l), true); ok {
			t.Errorf("%s decoded as %+v", l, m)
		}
	}
}

// TestLocalCommandPendingAcrossReads: a pending command is carried by the
// state into the next read and by a Clone, which shares nothing.
func TestLocalCommandPendingAcrossReads(t *testing.T) {
	b := foldRecaps(true, endTurn, recap("X."), caveat, cmdName)
	c := b.Clone()
	if !c.pending || c.Recap() != "X." || !c.TurnEnded() {
		t.Fatalf("clone: pending %v, recap %q, turn ended %v", c.pending, c.Recap(), c.TurnEnded())
	}
	// The next read: the result.
	if m, ok := DecodeTranscriptLine([]byte(localResult("recap", "<local-command-stdout>R.</local-command-stdout>")), true); ok {
		c.Apply(m)
	}
	if c.Recap() != "R." || !c.TurnEnded() || c.pending {
		t.Errorf("next read: recap %q, turn ended %v, pending %v", c.Recap(), c.TurnEnded(), c.pending)
	}
	if !b.pending || b.Recap() != "X." {
		t.Error("a clone changed the original")
	}
	// Or a prompt: the turn started.
	if m, ok := DecodeTranscriptLine([]byte(prompt), true); ok {
		b.Apply(m)
	}
	if b.Recap() != "" || b.TurnEnded() || b.pending {
		t.Errorf("prompt: recap %q, turn ended %v, pending %v", b.Recap(), b.TurnEnded(), b.pending)
	}
}
