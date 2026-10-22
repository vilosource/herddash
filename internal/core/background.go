package core

import "strings"

// BackgroundRule is one Agent Program's background-task rule: it says whether
// one direct child of the Agent's process, by its arguments, is background
// work the Agent is waiting on (operator request 2026-10-21, FR-2).
type BackgroundRule func(argv []string) bool

// backgroundRules is the background-task rule table, keyed by Agent Program,
// built in like the Context Line rules. A program with no entry never waits
// on background work as far as herddash can tell.
var backgroundRules = map[AgentProgram]BackgroundRule{
	// Claude Code runs every Bash tool call, foreground or background, its
	// own or a subagent's, as a direct child shell that sources its shell
	// snapshot: `/bin/zsh -c source <config dir>/shell-snapshots/snapshot-zsh-….sh …`.
	// Between commands there is none. Its MCP servers, gopls and caffeinate
	// are children too, with other command lines, and are not background
	// work. Observed 2026-10-21 on this machine.
	"claude": func(argv []string) bool {
		for _, a := range argv {
			if strings.Contains(a, "/shell-snapshots/snapshot-") {
				return true
			}
		}
		return false
	},
}

// ChecksBackground reports whether an Agent of program with Status status
// could be waiting on background work, so its process's children are worth
// reading: its program has a rule and its Status is done or idle. A working
// Agent's shell may be a foreground tool call, and a blocked or unknown one
// is never Background.
func ChecksBackground(program AgentProgram, status Status) bool {
	_, ok := backgroundRules[program]
	return ok && (status == StatusDone || status == StatusIdle)
}

// HasBackground reports whether any of children, each a direct child of an
// Agent's process given by its arguments, is a background task by program's
// rule. A program with no rule never has one.
func HasBackground(program AgentProgram, children [][]string) bool {
	rule, ok := backgroundRules[program]
	if !ok {
		return false
	}
	for _, argv := range children {
		if rule(argv) {
			return true
		}
	}
	return false
}
