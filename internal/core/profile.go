package core

import "path/filepath"

// ProfileUnresolved is the Profile of an Agent whose rule could not be applied:
// the variable was set to a value the Profile Map does not name, or its process
// or environment could not be read in time. It is reserved, so the Profile Map
// cannot name a Profile "unresolved".
const ProfileUnresolved = "unresolved"

// ProfileRule is one Agent Program's Profile rule (FR-10, AD-14): the Profile
// is chosen by the value of one environment variable in the Agent's foreground
// process.
type ProfileRule struct {
	// Variable is the environment variable the rule reads.
	Variable string
	// Absent is the Profile when the variable is unset.
	Absent string
	// Values maps a value of the variable to its Profile. Keys arrive with any
	// leading "~/" already expanded.
	Values map[string]string
}

// ResolveProfile applies rule to the value of its variable in an Agent's
// process. set says whether the variable was set at all.
//
//   - With no rule, the Profile is the Agent Program's name.
//   - Unset gives rule.Absent.
//   - Set gives the Values entry whose key equals value, both compared after
//     filepath.Clean, so a trailing slash does not matter.
//   - Set but unmatched gives ProfileUnresolved.
func ResolveProfile(program AgentProgram, rule *ProfileRule, value string, set bool) string {
	if rule == nil {
		return string(program)
	}
	if !set {
		return rule.Absent
	}
	want := filepath.Clean(value)
	for k, profile := range rule.Values {
		if filepath.Clean(k) == want {
			return profile
		}
	}
	return ProfileUnresolved
}

// Heading is how an Agent is named by its Profile and Agent Program: the name
// once when they are equal (a program with no rule), the program alone when
// the Profile is empty, "profile · program" otherwise.
func Heading(profile string, program AgentProgram) string {
	if profile == string(program) || profile == "" {
		return string(program)
	}
	return profile + " · " + string(program)
}
