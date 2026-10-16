package main

import (
	"strings"
	"testing"
)

// unionSchema is a minimal schema whose request section is a discriminated union
// on "method" with the given variant values.
func unionSchema(methods ...string) string {
	var variants []string
	for _, m := range methods {
		variants = append(variants, `{"type":"object","required":["method"],"properties":{"method":{"type":"string","const":"`+m+`"}}}`)
	}
	return `{"schema_version":1,"protocol":22,"schemas":{"request":{"title":"Request","type":"object",
		"properties":{"id":{"type":"string"}},"required":["id"],"oneOf":[` + strings.Join(variants, ",") + `]}}}`
}

func TestGenerateRejects(t *testing.T) {
	tests := []struct {
		name   string
		roots  []root
		schema string
		want   string
	}{
		{
			name: "one definition differing between sections",
			roots: []root{
				{section: "a", def: "Thing"},
				{section: "b", def: "Thing"},
			},
			schema: `{"schemas":{
				"a":{"$defs":{"Thing":{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}}},
				"b":{"$defs":{"Thing":{"type":"object","properties":{"x":{"type":"integer"}},"required":["x"]}}}}}`,
			want: "differs between schema sections",
		},
		{
			name:   "forbidden variant",
			roots:  []root{{section: "request", variants: []string{"ping", "pane.focus"}}},
			schema: unionSchema("ping", "pane.focus"),
			want:   "forbidden variant",
		},
		{
			name:   "variant the schema lacks",
			roots:  []root{{section: "request", variants: []string{"ping", "session.snapshot"}}},
			schema: unionSchema("ping"),
			want:   "has no variant",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved := roots
			roots = tt.roots
			defer func() { roots = saved }()
			_, err := generate([]byte(tt.schema), "test.json", "gen")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want one containing %q", err, tt.want)
			}
		})
	}
}
