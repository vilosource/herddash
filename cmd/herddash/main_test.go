package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "version", args: []string{"version"}, wantCode: 0, wantStdout: "herddash dev ("},
		{name: "version with extra argument", args: []string{"version", "extra"}, wantCode: 2, wantStderr: "usage: herddash"},
		{name: "unknown command", args: []string{"frobnicate"}, wantCode: 2, wantStderr: "usage: herddash"},
		{name: "no command", args: nil, wantCode: 2, wantStderr: "usage: herddash"},
		{name: "help", args: []string{"--help"}, wantCode: 0, wantStdout: "usage: herddash"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d", code, tc.wantCode)
			}
			if !strings.HasPrefix(stdout.String(), tc.wantStdout) || (tc.wantStdout == "" && stdout.Len() != 0) {
				t.Fatalf("stdout = %q, want prefix %q", stdout.String(), tc.wantStdout)
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) || (tc.wantStderr == "" && stderr.Len() != 0) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

func TestCommitFrom(t *testing.T) {
	const full = "184609e42c1bb12acef49f2b7cb0e6c740289efb"
	rev := debug.BuildSetting{Key: "vcs.revision", Value: full}
	tests := []struct {
		name     string
		stamped  string
		settings []debug.BuildSetting
		ok       bool
		want     string
	}{
		{name: "stamped wins", stamped: "abc", settings: []debug.BuildSetting{rev}, ok: true, want: "abc"},
		{name: "stamped without build info", stamped: "abc", want: "abc"},
		{name: "revision truncated to 12", settings: []debug.BuildSetting{rev}, ok: true, want: "184609e42c1b"},
		{name: "short revision kept", settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}}, ok: true, want: "abc123"},
		{name: "dirty suffix", settings: []debug.BuildSetting{rev, {Key: "vcs.modified", Value: "true"}}, ok: true, want: "184609e42c1b-dirty"},
		{name: "clean tree", settings: []debug.BuildSetting{rev, {Key: "vcs.modified", Value: "false"}}, ok: true, want: "184609e42c1b"},
		{name: "no revision", settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}, ok: true, want: "unknown"},
		{name: "no build info", want: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := commitFrom(tc.stamped, tc.settings, tc.ok); got != tc.want {
				t.Fatalf("commitFrom = %q, want %q", got, tc.want)
			}
		})
	}
}
