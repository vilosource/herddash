package core

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// TranscriptLine is everything herddash takes from one line of a Claude Code
// session transcript (ADR-0009): task ids, a task's closing status and
// whether the main thread's turn started or ended, and, only when recaps are
// on, Claude Code's session recap (ADR-0010). Nothing else of the line is
// kept: no other text, input, output, prompt or result.
type TranscriptLine struct {
	// StartID is a background task started by the line, "" when none;
	// StartCommand says it is a shell command rather than a subagent.
	StartID      string
	StartCommand bool
	// EndID is a task a notification on the line is about, "" when none,
	// and EndStatus its status when it is one that closes the task
	// (completed, failed, killed, stopped); "" leaves it open.
	EndID     string
	EndStatus string
	// TurnStarted and TurnEnded say the main thread's turn started (a prompt,
	// a tool result or a queued command) or ended (end_turn or stop_sequence).
	TurnStarted bool
	TurnEnded   bool
	// Recap is Claude Code's session recap the line carries (ADR-0010),
	// trimmed, without its trailing "(disable recaps in /config)" and at most
	// MaxRecapRunes long; "" when none, and always "" when recaps are off.
	Recap string
}

// MaxRecapRunes caps a recap's length: Claude Code's documented cap.
const MaxRecapRunes = 400

// recapSuffix is the note Claude Code ends every recap with.
const recapSuffix = "(disable recaps in /config)"

// closingStatuses are the notification statuses that close a task. stopped is
// what a resumed session enqueues, as it starts, for each task its transcript
// shows started and never completed (Claude Code 2.1.292, observed
// 2026-10-23; ADR-0009 as amended by Story 5.2): without it, a command left
// open by a crashed session would stay open after the resume.
var closingStatuses = map[string]bool{"completed": true, "failed": true, "killed": true, "stopped": true}

// transcriptRecord is the only part of a transcript record that is decoded,
// by fixed field names (ADR-0009, observed 2026-10-21). Everything else on
// the line is skipped by the decoder and never held.
type transcriptRecord struct {
	Type          string          `json:"type"`
	IsSidechain   bool            `json:"isSidechain"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Message       json.RawMessage `json:"message"`
	Operation     string          `json:"operation"`
	Content       json.RawMessage `json:"content"`
	Attachment    json.RawMessage `json:"attachment"`
	Subtype       string          `json:"subtype"`
}

// DecodeTranscriptLine decodes one transcript line for the markers ADR-0009
// names, and ok is false when it holds none or does not decode:
//
//   - a user record whose toolUseResult.backgroundTaskId is a non-empty
//     string starts that background command;
//   - a toolUseResult with isAsync true, status "async_launched" and an
//     agentId starts that subagent;
//   - a user record whose toolUseResult has a string task_type and a task_id
//     (TaskStop's result) ends that task as killed: a task stopped this way
//     gets no notification (seen 2026-10-21 for every background command
//     left open in four transcripts);
//   - a queue-operation record, operation "enqueue", whose content is a
//     string starting "<task-notification>" ends the task in its
//     <task-id>, when its <status> closes it: completed, failed, killed, or
//     stopped (a resumed session's word for a task the previous process
//     left with no completion record); any other status, or none, leaves
//     it open;
//   - an assistant record that is not a sidechain with message.stop_reason
//     "end_turn" or "stop_sequence" ends the main turn (both are followed by
//     the turn's duration record; 33 of 4,902 main-thread turn ends in the
//     transcripts read on 2026-10-22 were stop_sequence); a user record that is not a sidechain,
//     or an attachment of type queued_command, starts it again;
//   - only when recaps is true (the operator's consent, ADR-0010): a system
//     record that is not a sidechain, with subtype "away_summary" and a
//     string content, carries the session recap (see recapOf). With recaps
//     false it is no marker, and its content is never looked at.
//
// Ids must be 1 to 128 letters, digits, "-" or "_"; a line with any other is
// not a marker for that id.
func DecodeTranscriptLine(line []byte, recaps bool) (TranscriptLine, bool) {
	var l TranscriptLine
	var rec transcriptRecord
	if len(bytes.TrimSpace(line)) == 0 || json.Unmarshal(line, &rec) != nil {
		return l, false
	}
	switch rec.Type {
	case "user":
		l.TurnStarted = !rec.IsSidechain
		l.StartID, l.StartCommand = taskStart(rec.ToolUseResult)
		if id := taskStopped(rec.ToolUseResult); id != "" {
			l.EndID, l.EndStatus = id, "killed"
		}
	case "assistant":
		switch stopReason(rec.Message) {
		case "end_turn", "stop_sequence":
			l.TurnEnded = !rec.IsSidechain
		}
	case "attachment":
		var a struct {
			Type string `json:"type"`
		}
		if isObject(rec.Attachment) && json.Unmarshal(rec.Attachment, &a) == nil && a.Type == "queued_command" && !rec.IsSidechain {
			l.TurnStarted = true
		}
	case "queue-operation":
		if rec.Operation == "enqueue" {
			l.EndID, l.EndStatus = taskNotification(rec.Content)
		}
	case "system":
		if recaps && !rec.IsSidechain && rec.Subtype == "away_summary" {
			l.Recap = recapOf(rec.Content)
		}
	}
	return l, l != TranscriptLine{}
}

// recapOf is a recap record's content as shown: trimmed, without Claude
// Code's trailing recapSuffix, trimmed again, and cut to at most
// MaxRecapRunes whole runes; "" when the content is not a string or nothing
// is left.
func recapOf(raw json.RawMessage) string {
	s := strings.TrimSpace(jsonString(raw))
	s = strings.TrimSpace(strings.TrimSuffix(s, recapSuffix))
	if utf8.RuneCountInString(s) > MaxRecapRunes {
		s = strings.TrimSpace(string([]rune(s)[:MaxRecapRunes]))
	}
	return s
}

// isObject reports whether raw is a JSON object.
func isObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}

// taskStart reads a toolUseResult for a background command's or subagent's
// start.
func taskStart(raw json.RawMessage) (id string, command bool) {
	if !isObject(raw) {
		return "", false
	}
	var r struct {
		BackgroundTaskID json.RawMessage `json:"backgroundTaskId"`
		IsAsync          bool            `json:"isAsync"`
		Status           json.RawMessage `json:"status"`
		AgentID          json.RawMessage `json:"agentId"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return "", false
	}
	if id := jsonString(r.BackgroundTaskID); taskID(id) {
		return id, true
	}
	if id := jsonString(r.AgentID); r.IsAsync && jsonString(r.Status) == "async_launched" && taskID(id) {
		return id, false
	}
	return "", false
}

// taskStopped reads a toolUseResult for TaskStop's result, which names the
// task it stopped in task_id beside a task_type; "" when it is not one.
func taskStopped(raw json.RawMessage) string {
	if !isObject(raw) {
		return ""
	}
	var r struct {
		TaskID   json.RawMessage `json:"task_id"`
		TaskType json.RawMessage `json:"task_type"`
	}
	if json.Unmarshal(raw, &r) != nil || jsonString(r.TaskType) == "" {
		return ""
	}
	if id := jsonString(r.TaskID); taskID(id) {
		return id
	}
	return ""
}

// stopReason is message.stop_reason, "" when absent or not a string.
func stopReason(raw json.RawMessage) string {
	if !isObject(raw) {
		return ""
	}
	var m struct {
		StopReason json.RawMessage `json:"stop_reason"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return jsonString(m.StopReason)
}

// taskNotification reads a queued <task-notification> for its <task-id> and
// a closing <status>; anything else in it is never looked at.
func taskNotification(raw json.RawMessage) (id, status string) {
	content := jsonString(raw)
	if !strings.HasPrefix(content, "<task-notification>") {
		return "", ""
	}
	id = between(content, "<task-id>", "</task-id>")
	if !taskID(id) {
		return "", ""
	}
	if s := between(content, "<status>", "</status>"); closingStatuses[s] {
		status = s
	}
	return id, status
}

// between is the text of s between the first open and the close after it,
// trimmed, "" when either is missing.
func between(s, open, close string) string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return ""
	}
	v, _, ok := strings.Cut(rest, close)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// jsonString is raw's value when it is a JSON string, "" otherwise.
func jsonString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// taskID reports whether id has the shape of a task id: 1 to 128 letters,
// digits, "-" or "_".
func taskID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// MaxOpenTasks caps how many open background tasks are kept per Agent; past
// it, the oldest is forgotten.
const MaxOpenTasks = 256

// BackgroundTasks is what one Claude Code Agent's transcript shows: its
// background work (ADR-0009), the tasks started and not yet closed and
// whether the main thread's turn has ended, and its latest session recap
// (ADR-0010). Its zero value has none, turn not ended, no recap. It is held
// in memory only.
type BackgroundTasks struct {
	open      []openTask
	turnEnded bool
	recap     string
}

type openTask struct {
	id      string
	command bool
}

// Apply folds one transcript line into b.
//
// A started turn clears the recap, so a stale one never shows beside a new
// turn; a recap line replaces it, the newest winning (ADR-0010).
func (b *BackgroundTasks) Apply(l TranscriptLine) {
	if l.TurnStarted {
		b.turnEnded = false
		b.recap = ""
	}
	if l.Recap != "" {
		b.recap = l.Recap
	}
	if l.TurnEnded {
		b.turnEnded = true
	}
	if l.StartID != "" && b.index(l.StartID) < 0 {
		if len(b.open) >= MaxOpenTasks {
			b.open = append(b.open[:0:0], b.open[len(b.open)-MaxOpenTasks+1:]...)
		}
		b.open = append(b.open, openTask{id: l.StartID, command: l.StartCommand})
	}
	if l.EndID != "" && closingStatuses[l.EndStatus] {
		if i := b.index(l.EndID); i >= 0 {
			b.open = append(b.open[:i:i], b.open[i+1:]...)
		}
	}
}

func (b BackgroundTasks) index(id string) int {
	for i, t := range b.open {
		if t.id == id {
			return i
		}
	}
	return -1
}

// Count is how many background tasks are open.
func (b BackgroundTasks) Count() int { return len(b.open) }

// TurnEnded reports whether the main thread's turn has ended with no later
// prompt, tool result or queued command.
func (b BackgroundTasks) TurnEnded() bool { return b.turnEnded }

// OpenCommand reports whether an open task is a background shell command, so
// a live tool shell the process check finds is accounted for.
func (b BackgroundTasks) OpenCommand() bool {
	for _, t := range b.open {
		if t.command {
			return true
		}
	}
	return false
}

// Recap is the Agent's latest session recap (ADR-0010), "" when there is
// none or a turn started since.
func (b BackgroundTasks) Recap() string { return b.recap }

// Clone is a copy of b that shares nothing with it.
func (b BackgroundTasks) Clone() BackgroundTasks {
	return BackgroundTasks{open: append([]openTask(nil), b.open...), turnEnded: b.turnEnded, recap: b.recap}
}
