package core

import "testing"

func TestResolveProfile(t *testing.T) {
	rule := &ProfileRule{
		Variable: "CLAUDE_CONFIG_DIR",
		Absent:   "claude",
		Values: map[string]string{
			"/h/.claude-cldw":  "cldw",
			"/h/.claude-cldp/": "cldp",
		},
	}
	cases := []struct {
		name    string
		program AgentProgram
		rule    *ProfileRule
		value   string
		set     bool
		want    string
	}{
		{"mapped", "claude", rule, "/h/.claude-cldw", true, "cldw"},
		{"trailing slash on the value", "claude", rule, "/h/.claude-cldw/", true, "cldw"},
		{"trailing slash on the key", "claude", rule, "/h/.claude-cldp", true, "cldp"},
		{"unclean value", "claude", rule, "/h/./x/../.claude-cldw", true, "cldw"},
		{"absent", "claude", rule, "", false, "claude"},
		{"unset ignores a value", "claude", rule, "/h/.claude-cldw", false, "claude"},
		{"unmatched", "claude", rule, "/h/.other", true, ProfileUnresolved},
		{"set but empty", "claude", rule, "", true, ProfileUnresolved},
		{"no rule", "codex", nil, "", false, "codex"},
		{"no rule ignores a value", "codex", nil, "/h/.claude-cldw", true, "codex"},
		{"rule with no values", "claude", &ProfileRule{Variable: "X", Absent: "a"}, "/x", true, ProfileUnresolved},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveProfile(c.program, c.rule, c.value, c.set); got != c.want {
				t.Errorf("ResolveProfile(%q, %q, %v) = %q, want %q", c.program, c.value, c.set, got, c.want)
			}
		})
	}
}

func TestHeading(t *testing.T) {
	cases := []struct {
		profile string
		program AgentProgram
		want    string
	}{
		{"codex", "codex", "codex"},
		{"cldw", "claude", "cldw"},
		{"claude", "claude", "claude"},
		{ProfileUnresolved, "claude", "unresolved"},
		{"work", "codex", "work · codex"},
		{"", "claude", "claude"},
	}
	for _, c := range cases {
		if got := Heading(c.profile, c.program); got != c.want {
			t.Errorf("Heading(%q, %q) = %q, want %q", c.profile, c.program, got, c.want)
		}
	}
}
