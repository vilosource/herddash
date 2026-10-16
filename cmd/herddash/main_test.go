package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
		{name: "unknown serve flag", args: []string{"serve", "--colour"}, wantCode: 2, wantStderr: "colour"},
		{name: "serve with extra argument", args: []string{"serve", "extra"}, wantCode: 2, wantStderr: "no arguments"},
		{name: "help", args: []string{"--help"}, wantCode: 0, wantStdout: "usage: herddash"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), tc.args, &stdout, &stderr)
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

// hermeticEnv points HOME at a temporary directory and clears the variables
// configuration reads, so the config subcommand never sees the real machine.
// HOME is under /tmp because herdr's default socket path under macOS's long
// per-user temporary directory exceeds the unix socket path limit.
func hermeticEnv(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	for _, k := range []string{"HERDR_SOCKET_PATH", "HERDR_SESSION", "XDG_DATA_HOME"} {
		t.Setenv(k, "")
	}
	return home
}

func TestConfigExampleAcceptance(t *testing.T) {
	home := hermeticEnv(t)
	example, err := os.ReadFile(filepath.Join("..", "..", "docs", "config.example.toml"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, example, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"config", "--config", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var got struct {
		ConfigFile string `json:"config_file"`
		SocketPath string `json:"socket_path"`
		Server     string `json:"server"`
		Profiles   map[string]struct {
			Rule     string `json:"rule"`
			Variable string `json:"variable"`
			Absent   string `json:"absent"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	claude, ok := got.Profiles["claude"]
	if !ok || claude.Absent != "claude" || claude.Rule != "env" || claude.Variable != "CLAUDE_CONFIG_DIR" {
		t.Fatalf("claude profile = %+v (present %v)", claude, ok)
	}
	if got.ConfigFile != path || got.Server != "default" || got.SocketPath != filepath.Join(home, ".config/herdr/herdr.sock") {
		t.Fatalf("config = %+v", got)
	}
	if !strings.HasPrefix(stdout.String(), "{\n  ") {
		t.Fatalf("stdout is not indented JSON: %q", stdout.String())
	}
}

func TestConfigSubcommand(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "no file, defaults", wantCode: 0, wantStdout: `"bind": "127.0.0.1:7318"`},
		{name: "flag beats file", file: `bind = "127.0.0.1:9000"`, args: []string{"--bind", "127.0.0.1:9001"},
			wantCode: 0, wantStdout: `"bind": "127.0.0.1:9001"`},
		{name: "session flag", args: []string{"--session", "work"}, wantCode: 0, wantStdout: `"server": "work"`},
		{name: "unknown key", file: `colour = "red"`, wantCode: 1, wantStderr: "colour"},
		{name: "bad value", file: `retention_days = 0`, wantCode: 1, wantStderr: "config.toml): 0: must be from 1 to 36500"},
		{name: "non-loopback", args: []string{"--bind", "0.0.0.0:7318"}, wantCode: 1, wantStderr: "authentication does not exist yet"},
		{name: "odd socket", args: []string{"--socket", "/tmp/x.sock"}, wantCode: 1,
			wantStderr: "config: server (from socket from flag --socket): socket \"/tmp/x.sock\" is neither"},
		{name: "missing explicit config", args: []string{"--config", "/nonexistent/herddash.toml"}, wantCode: 1,
			wantStderr: "config: config: cannot read configuration file: open /nonexistent/herddash.toml"},
		{name: "empty explicit config", args: []string{"--config="}, wantCode: 1, wantStderr: "config: config (from flag --config): path is empty"},
		{name: "unknown flag", args: []string{"--colour"}, wantCode: 2, wantStderr: "colour"},
		{name: "stray argument", args: []string{"extra"}, wantCode: 2, wantStderr: "no arguments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hermeticEnv(t)
			args := []string{"config"}
			if tc.file != "" {
				path := filepath.Join(t.TempDir(), "config.toml")
				if err := os.WriteFile(path, []byte(tc.file), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--config", path)
			}
			args = append(args, tc.args...)
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d; stderr = %q", code, tc.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantStdout) || (tc.wantStdout == "" && stdout.Len() != 0) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), tc.wantStdout)
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) || (tc.wantStderr == "" && stderr.Len() != 0) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

// TestConfigFlagsReachJSON passes every flag not otherwise exercised through
// run and asserts each value in the output, so a slip in the flag mapping
// cannot drop one silently.
func TestConfigFlagsReachJSON(t *testing.T) {
	hermeticEnv(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	database := filepath.Join(t.TempDir(), "h.db")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"config",
		"--host", "flaghost",
		"--server", "flagserver",
		"--data-dir", dataDir,
		"--database", database,
		"--retention-days", "12",
		"--allow-unsupported-herdr",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var got struct {
		Host                  string `json:"host"`
		Server                string `json:"server"`
		DataDir               string `json:"data_dir"`
		Database              string `json:"database"`
		RetentionDays         int    `json:"retention_days"`
		AllowUnsupportedHerdr bool   `json:"allow_unsupported_herdr"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if got.Host != "flaghost" || got.Server != "flagserver" || got.DataDir != dataDir ||
		got.Database != database || got.RetentionDays != 12 || !got.AllowUnsupportedHerdr {
		t.Fatalf("config = %+v", got)
	}
}
