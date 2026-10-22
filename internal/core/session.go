package core

import (
	"path/filepath"
	"strings"
)

// Observer says who observed a Transition (ADR-0008): herdr, the default and
// the empty value, or Claude Code's own session record.
type Observer string

// The Observers.
const (
	// ObservedByHerdr is herdr, through a status event, a snapshot or the
	// plugin log. It is stored as NULL.
	ObservedByHerdr Observer = ""
	// ObservedByClaudeSession is Claude Code's session record,
	// <config dir>/sessions/<pid>.json.
	ObservedByClaudeSession Observer = "claude_session"
)

// Valid reports whether o is one of the Observers.
func (o Observer) Valid() bool {
	return o == ObservedByHerdr || o == ObservedByClaudeSession
}

// SessionWaiting is the one session record status herddash uses: Claude Code
// is waiting on the operator. Its other values (busy, idle, shell, and any
// later one) are never mapped to anything.
const SessionWaiting = "waiting"

// sessionPrograms are the Agent Programs whose session record is read.
var sessionPrograms = map[AgentProgram]bool{"claude": true}

// ReadsSession reports whether an Agent of program has a session record worth
// reading: only Claude Code does.
func ReadsSession(program AgentProgram) bool {
	return sessionPrograms[program]
}

// EffectiveStatus is an Agent's Status from herdr's Status and its session
// record's status (ADR-0008): blocked while a Claude Code Agent's record says
// waiting, herdr's Status otherwise. session is "" when the record was not
// read or could not be.
func EffectiveStatus(program AgentProgram, herdr Status, session string) Status {
	if ReadsSession(program) && session == SessionWaiting {
		return StatusBlocked
	}
	return herdr
}

// SessionObservedAt is the observed_at of a Transition the session record
// caused: the record's statusUpdatedAt when it is known and not after readAt,
// raised to previous, the Agent's previous Transition, when it is earlier;
// readAt otherwise. All three are UTC Unix milliseconds. Claude Code stamps
// its status a few milliseconds before herdr reports the same moment (seen
// 2026-10-21: waiting at 15:42:24.741, herdr's done after it), so an earlier
// stamp is clamped, not discarded.
func SessionObservedAt(statusUpdatedAt, previous, readAt int64) int64 {
	if statusUpdatedAt > 0 && statusUpdatedAt <= readAt {
		return max(statusUpdatedAt, previous)
	}
	return readAt
}

// ClaudeConfigDir is the Claude Code configuration directory of a process
// whose CLAUDE_CONFIG_DIR is value (set says whether it is set at all): that
// value with a bare "~" or a leading "~/" expanded to home, as the Profile
// rules are; home's .claude when it is unset or empty. It is "" when it
// cannot be known: a relative path, or a home needed and unknown.
func ClaudeConfigDir(value string, set bool, home string) string {
	if !set || value == "" {
		if home == "" {
			return ""
		}
		return filepath.Join(home, ".claude")
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		if home == "" {
			return ""
		}
		value = filepath.Join(home, strings.TrimPrefix(value[1:], "/"))
	}
	if !filepath.IsAbs(value) {
		return ""
	}
	return filepath.Clean(value)
}
