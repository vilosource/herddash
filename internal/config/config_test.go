package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/vilosource/herddash/internal/core"
)

// testEnv returns an injected environment rooted in a short temporary home, so
// no test touches the real home directory or environment. The home is under
// /tmp because macOS's per-user temporary directory is so long that herdr's
// default socket path under it exceeds the unix socket path limit.
func testEnv(t *testing.T, vars map[string]string) Env {
	t.Helper()
	if vars == nil {
		vars = map[string]string{}
	}
	return Env{Home: shortTempDir(t), Hostname: "box.example.com", Vars: vars}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// wantKeyError asserts err is an *Error naming key.
func wantKeyError(t *testing.T, err error, key string) {
	t.Helper()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("error = %v (%T), want *config.Error naming %q", err, err, key)
	}
	if ce.Key != key {
		t.Fatalf("error key = %q, want %q (error: %v)", ce.Key, key, err)
	}
	if !strings.Contains(err.Error(), key) {
		t.Fatalf("error text %q does not name %q", err.Error(), key)
	}
}

func TestLoadDefaultsWithNoFile(t *testing.T) {
	env := testEnv(t, nil)
	c, err := Load(Flags{}, env)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct{ name, got, want string }{
		{"file", c.File(), ""},
		{"host", c.Host(), "box"},
		{"socket", c.SocketPath(), filepath.Join(env.Home, ".config/herdr/herdr.sock")},
		{"server", c.Server(), "default"},
		{"bind", c.Bind(), "127.0.0.1:7318"},
		{"data_dir", c.DataDir(), filepath.Join(env.Home, ".local/share/herddash")},
		{"database", c.Database(), filepath.Join(env.Home, ".local/share/herddash/herddash.db")},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %q, want %q", ch.name, ch.got, ch.want)
		}
	}
	if c.RetentionDays() != 90 {
		t.Errorf("retention_days = %d, want 90", c.RetentionDays())
	}
	if c.AllowUnsupportedHerdr() {
		t.Error("allow_unsupported_herdr = true, want false")
	}
	if c.ClaudeRecaps() {
		t.Error("claude_recaps = true, want false")
	}
	if len(c.AgentPrograms()) != 0 {
		t.Errorf("profiles = %v, want none", c.AgentPrograms())
	}
}

func TestLoadReadsDefaultFile(t *testing.T) {
	env := testEnv(t, nil)
	dir := filepath.Join(env.Home, ".config", "herddash")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("retention_days = 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(Flags{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.File() != path || c.RetentionDays() != 7 {
		t.Fatalf("file = %q retention = %d, want %q and 7", c.File(), c.RetentionDays(), path)
	}
}

func TestLoadExplicitMissingFile(t *testing.T) {
	env := testEnv(t, nil)
	_, err := Load(Flags{Config: new(filepath.Join(env.Home, "nope.toml"))}, env)
	wantKeyError(t, err, "config")
}

func TestXDGDataHome(t *testing.T) {
	env := testEnv(t, map[string]string{"XDG_DATA_HOME": "/xdg/data"})
	c, err := Load(Flags{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir() != "/xdg/data/herddash" || c.Database() != "/xdg/data/herddash/herddash.db" {
		t.Fatalf("data_dir = %q database = %q", c.DataDir(), c.Database())
	}
	// A relative XDG_DATA_HOME is invalid per the XDG specification and ignored.
	env = testEnv(t, map[string]string{"XDG_DATA_HOME": "rel"})
	c, err = Load(Flags{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir() != filepath.Join(env.Home, ".local/share/herddash") {
		t.Fatalf("data_dir = %q", c.DataDir())
	}
}

func TestFlagBeatsFileBeatsDefault(t *testing.T) {
	env := testEnv(t, nil)
	path := writeConfig(t, `
bind = "127.0.0.1:9000"
host = "filehost"
data_dir = "~/data"
retention_days = 30
allow_unsupported_herdr = true
claude_recaps = true
`)
	c, err := Load(Flags{Config: new(path), Bind: new("127.0.0.1:9001"), RetentionDays: new(5)}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.Bind() != "127.0.0.1:9001" {
		t.Errorf("bind = %q, want flag value", c.Bind())
	}
	if c.RetentionDays() != 5 {
		t.Errorf("retention_days = %d, want flag value", c.RetentionDays())
	}
	if c.Host() != "filehost" {
		t.Errorf("host = %q, want file value", c.Host())
	}
	if c.DataDir() != filepath.Join(env.Home, "data") {
		t.Errorf("data_dir = %q, want expanded file value", c.DataDir())
	}
	if c.Database() != filepath.Join(env.Home, "data", "herddash.db") {
		t.Errorf("database = %q, want under data_dir", c.Database())
	}
	if !c.AllowUnsupportedHerdr() {
		t.Error("allow_unsupported_herdr = false, want file value")
	}
	if !c.ClaudeRecaps() {
		t.Error("claude_recaps = false, want file value")
	}
	c, err = Load(Flags{Config: new(path), AllowUnsupportedHerdr: new(false), Host: new("flaghost")}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.AllowUnsupportedHerdr() || c.Host() != "flaghost" {
		t.Errorf("flag did not beat file: allow=%v host=%q", c.AllowUnsupportedHerdr(), c.Host())
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		flags Flags
		vars  map[string]string
		key   string
	}{
		{name: "unknown key", file: `colour = "red"`, key: "colour"},
		{name: "unknown nested key", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"a\"\ncolour = 1", key: "profiles.claude.colour"},
		{name: "retention zero", file: `retention_days = 0`, key: "retention_days"},
		{name: "retention flag zero", flags: Flags{RetentionDays: new(0)}, key: "retention_days"},
		{name: "retention wrong type", file: `retention_days = "ninety"`, key: "retention_days"},
		{name: "non-loopback bind", file: `bind = "0.0.0.0:7318"`, key: "bind"},
		{name: "empty-host bind", flags: Flags{Bind: new(":7318")}, key: "bind"},
		{name: "bad bind port", file: `bind = "127.0.0.1:http"`, key: "bind"},
		{name: "odd socket path", file: `socket_path = "/tmp/x.sock"`, key: "server"},
		{name: "relative socket path", file: `socket_path = "x.sock"`, key: "socket_path"},
		{name: "odd env socket path", vars: map[string]string{"HERDR_SOCKET_PATH": "/tmp/x.sock"}, key: "server"},
		{name: "session with slash", file: `session = "a/b"`, key: "session"},
		{name: "env session with slash", vars: map[string]string{"HERDR_SESSION": "../x"}, key: "HERDR_SESSION"},
		{name: "empty host", file: `host = ""`, key: "host"},
		{name: "empty server", file: `server = ""`, key: "server"},
		{name: "relative data dir", flags: Flags{DataDir: new("data")}, key: "data_dir"},
		{name: "relative database", file: `database = "x.db"`, key: "database"},
		{name: "unknown rule", file: "[profiles.claude]\nrule = \"magic\"\nvariable = \"X\"\nabsent = \"a\"", key: "profiles.claude.rule"},
		{name: "missing rule", file: "[profiles.claude]\nvariable = \"X\"\nabsent = \"a\"", key: "profiles.claude.rule"},
		{name: "empty absent", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"\"", key: "profiles.claude.absent"},
		{name: "missing variable", file: "[profiles.claude]\nrule = \"env\"\nabsent = \"a\"", key: "profiles.claude.variable"},
		{name: "empty value", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"a\"\nvalues = {\"\" = \"p\"}", key: "profiles.claude.values"},
		{name: "empty profile name", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"a\"\nvalues = {\"/x\" = \"\"}", key: `profiles.claude.values."/x"`},
		{name: "losing session still validated", file: "socket_path = \"~/.config/herdr/herdr.sock\"\nsession = \"a/b\"", key: "session"},
		{name: "losing HERDR_SESSION still validated", vars: map[string]string{"HERDR_SOCKET_PATH": "HOME/.config/herdr/herdr.sock", "HERDR_SESSION": "a/b"}, key: "HERDR_SESSION"},
		{name: "file host overridden by flag still validated", file: `host = "a b"`, flags: Flags{Host: new("ok")}, key: "host"},
		{name: "file server overridden by flag still validated", file: `server = "x/y"`, flags: Flags{Server: new("ok")}, key: "server"},
		{name: "file session overridden by flag still validated", file: `session = "x#y"`, flags: Flags{Session: new("ok")}, key: "session"},
		{name: "retention above cap", file: `retention_days = 36501`, key: "retention_days"},
		{name: "socket path too long", flags: Flags{Socket: new("/" + strings.Repeat("a", 103)), Server: new("s")}, key: "socket_path"},
		{name: "session with question mark", flags: Flags{Session: new("a?b")}, key: "session"},
		{name: "session with hash", flags: Flags{Session: new("a#b")}, key: "session"},
		{name: "server with percent", flags: Flags{Server: new("a%b")}, key: "server"},
		{name: "host with non-ASCII space", flags: Flags{Host: new("a\u2003b")}, key: "host"},
		{name: "host with non-ASCII control", flags: Flags{Host: new("a\u0085b")}, key: "host"},
		{name: "absent unresolved", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"unresolved\"", key: "profiles.claude.absent"},
		{name: "value named unresolved", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"a\"\nvalues = {\"/x\" = \"unresolved\"}", key: `profiles.claude.values."/x"`},
		{name: "program with whitespace", file: "[profiles.\" claude\"]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"a\"", key: `profiles." claude"`},
		{name: "variable with equals", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"A=B\"\nabsent = \"a\"", key: "profiles.claude.variable"},
		{name: "variable with space", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"A B\"\nabsent = \"a\"", key: "profiles.claude.variable"},
		{name: "variable with NUL", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"A\\u0000B\"\nabsent = \"a\"", key: "profiles.claude.variable"},
		{name: "explicit empty config path", flags: Flags{Config: new("")}, key: "config"},
		{name: "duplicate after expansion", file: "[profiles.claude]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"a\"\nvalues = {\"~/x\" = \"p\", \"HOME/x\" = \"q\"}", key: `profiles.claude.values."~/x"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv(t, tc.vars)
			flags := tc.flags
			if tc.file != "" {
				flags.Config = new(writeConfig(t, strings.ReplaceAll(tc.file, "HOME", env.Home)))
			}
			_, err := Load(flags, env)
			wantKeyError(t, err, tc.key)
		})
	}
}

func TestNonLoopbackBindSaysWhy(t *testing.T) {
	env := testEnv(t, nil)
	_, err := Load(Flags{Bind: new("0.0.0.0:7318")}, env)
	if !errors.Is(err, ErrNonLoopbackBind) || !strings.Contains(err.Error(), "authentication does not exist yet") {
		t.Fatalf("error = %v, want it to say authentication does not exist yet", err)
	}
}

func TestRetentionCapAccepted(t *testing.T) {
	c, err := Load(Flags{RetentionDays: new(MaxRetentionDays)}, testEnv(t, nil))
	if err != nil || c.RetentionDays() != 36500 {
		t.Fatalf("retention = %d, err = %v", c.RetentionDays(), err)
	}
}

func TestNonLoopbackIPv6Rejected(t *testing.T) {
	_, err := Load(Flags{Bind: new("[::]:7318")}, testEnv(t, nil))
	wantKeyError(t, err, "bind")
}

func TestLoopbackBinds(t *testing.T) {
	for _, b := range []string{"127.0.0.1:7318", "[::1]:7318", "localhost:65535", "127.0.0.1:0", "LOCALHOST:7318"} {
		env := testEnv(t, nil)
		if _, err := Load(Flags{Bind: new(b)}, env); err != nil {
			t.Errorf("bind %q: %v", b, err)
		}
	}
}

// TestRefusedLoopbackBinds checks that a loopback bind the API's Host check
// would refuse is refused at start, naming the accepted hosts, as are ports
// that are not all digits.
func TestRefusedLoopbackBinds(t *testing.T) {
	for _, b := range []string{"127.0.0.2:1", "localhost.:7318", "[0:0:0:0:0:0:0:1]:7318", "[::ffff:127.0.0.1]:7318"} {
		_, err := Load(Flags{Bind: new(b)}, testEnv(t, nil))
		wantKeyError(t, err, "bind")
		if err == nil || errors.Is(err, ErrNonLoopbackBind) || !strings.Contains(err.Error(), "localhost, 127.0.0.1 or ::1") {
			t.Errorf("bind %q: error %v, want one naming localhost, 127.0.0.1 or ::1", b, err)
		}
	}
	for _, b := range []string{"127.0.0.1:-0", "127.0.0.1:+0", "127.0.0.1:+7318", "127.0.0.1:65536", "127.0.0.1:"} {
		_, err := Load(Flags{Bind: new(b)}, testEnv(t, nil))
		wantKeyError(t, err, "bind")
	}
}

func TestSocketResolution(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		flags      Flags
		vars       map[string]string
		wantSocket string // relative to home
		wantServer string
		wantSess   string
	}{
		{name: "default", wantSocket: ".config/herdr/herdr.sock", wantServer: "default"},
		{name: "HERDR_SESSION", vars: map[string]string{"HERDR_SESSION": "work"},
			wantSocket: ".config/herdr/sessions/work/herdr.sock", wantServer: "work", wantSess: "work"},
		{name: "HERDR_SOCKET_PATH session", vars: map[string]string{"HERDR_SOCKET_PATH": "HOME/.config/herdr/sessions/ops/herdr.sock"},
			wantSocket: ".config/herdr/sessions/ops/herdr.sock", wantServer: "ops"},
		{name: "HERDR_SOCKET_PATH default", vars: map[string]string{"HERDR_SOCKET_PATH": "HOME/.config/herdr/herdr.sock"},
			wantSocket: ".config/herdr/herdr.sock", wantServer: "default"},
		{name: "HERDR_SOCKET_PATH beats HERDR_SESSION",
			vars:       map[string]string{"HERDR_SOCKET_PATH": "HOME/.config/herdr/sessions/ops/herdr.sock", "HERDR_SESSION": "work"},
			wantSocket: ".config/herdr/sessions/ops/herdr.sock", wantServer: "ops"},
		{name: "flag session beats HERDR_SOCKET_PATH", flags: Flags{Session: new("a")},
			vars:       map[string]string{"HERDR_SOCKET_PATH": "HOME/.config/herdr/sessions/ops/herdr.sock"},
			wantSocket: ".config/herdr/sessions/a/herdr.sock", wantServer: "a", wantSess: "a"},
		{name: "file session", file: `session = "b"`,
			wantSocket: ".config/herdr/sessions/b/herdr.sock", wantServer: "b", wantSess: "b"},
		{name: "flag session beats file session", file: `session = "b"`, flags: Flags{Session: new("a")},
			wantSocket: ".config/herdr/sessions/a/herdr.sock", wantServer: "a", wantSess: "a"},
		{name: "socket_path beats session", file: "socket_path = \"~/.config/herdr/sessions/s/herdr.sock\"\nsession = \"b\"",
			wantSocket: ".config/herdr/sessions/s/herdr.sock", wantServer: "s"},
		{name: "flag socket beats file socket", file: `socket_path = "/tmp/x.sock"`, flags: Flags{Socket: new("~/.config/herdr/herdr.sock")},
			wantSocket: ".config/herdr/herdr.sock", wantServer: "default"},
		{name: "odd path with server", file: "socket_path = \"HOME/x.sock\"\nserver = \"mine\"",
			wantSocket: "x.sock", wantServer: "mine"},
		{name: "explicit server wins", flags: Flags{Server: new("custom"), Session: new("a")},
			wantSocket: ".config/herdr/sessions/a/herdr.sock", wantServer: "custom", wantSess: "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv(t, nil)
			for k, v := range tc.vars {
				env.Vars[k] = strings.ReplaceAll(v, "HOME", env.Home)
			}
			flags := tc.flags
			if tc.file != "" {
				flags.Config = new(writeConfig(t, strings.ReplaceAll(tc.file, "HOME", env.Home)))
			}
			c, err := Load(flags, env)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(env.Home, tc.wantSocket); c.SocketPath() != want {
				t.Errorf("socket = %q, want %q", c.SocketPath(), want)
			}
			if c.Server() != tc.wantServer {
				t.Errorf("server = %q, want %q", c.Server(), tc.wantServer)
			}
			if c.Session() != tc.wantSess {
				t.Errorf("session = %q, want %q", c.Session(), tc.wantSess)
			}
		})
	}
}

func TestServerErrorNamesSocketSource(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		flags Flags
		vars  map[string]string
		want  string
	}{
		{name: "flag", flags: Flags{Socket: new("/tmp/x.sock")}, want: "flag --socket"},
		{name: "file", file: `socket_path = "/tmp/x.sock"`, want: "config.toml"},
		{name: "environment", vars: map[string]string{"HERDR_SOCKET_PATH": "/tmp/x.sock"}, want: "HERDR_SOCKET_PATH"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv(t, tc.vars)
			flags := tc.flags
			if tc.file != "" {
				flags.Config = new(writeConfig(t, tc.file))
			}
			_, err := Load(flags, env)
			wantKeyError(t, err, "server")
			var ce *Error
			errors.As(err, &ce)
			if !strings.Contains(ce.Source, tc.want) {
				t.Fatalf("source = %q, want it to contain %q", ce.Source, tc.want)
			}
		})
	}
}

func TestConfigPathTildeExpanded(t *testing.T) {
	env := testEnv(t, nil)
	if err := os.WriteFile(filepath.Join(env.Home, "x.toml"), []byte("retention_days = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(Flags{Config: new("~/x.toml")}, env)
	if err != nil || c.RetentionDays() != 3 || c.File() != filepath.Join(env.Home, "x.toml") {
		t.Fatalf("file = %q retention = %d err = %v", c.File(), c.RetentionDays(), err)
	}
}

func TestDefaultConfigDanglingSymlink(t *testing.T) {
	env := testEnv(t, nil)
	dir := filepath.Join(env.Home, ".config", "herddash")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.Home, "missing.toml"), filepath.Join(dir, "config.toml")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(Flags{}, env)
	wantKeyError(t, err, "config")
}

func TestBareTildeExpanded(t *testing.T) {
	env := testEnv(t, nil)
	path := writeConfig(t, "data_dir = \"~\"\n[profiles.claude]\nrule = \"env\"\nvariable = \"X\"\nabsent = \"a\"\nvalues = {\"~\" = \"home\"}\n")
	c, err := Load(Flags{Config: new(path)}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir() != env.Home {
		t.Errorf("data_dir = %q, want %q", c.DataDir(), env.Home)
	}
	r, _ := c.Profile("claude")
	if r.Values()[env.Home] != "home" {
		t.Errorf("values = %v, want ~ expanded to %q", r.Values(), env.Home)
	}
}

func TestServerFromPathRejectsNestedSessions(t *testing.T) {
	home := "/h"
	dir := filepath.Join(home, ".config", "herdr")
	for _, p := range []string{
		"/h/.config/herdr/sessions/a/b/herdr.sock",
		"/h/.config/herdr/sessions/herdr.sock",
		"/h/.config/herdr/sessions/a/other.sock",
		"/elsewhere/herdr/sessions/a/herdr.sock",
	} {
		if s, ok := serverFromPath(p, dir); ok {
			t.Errorf("serverFromPath(%q) = %q, want no Server", p, s)
		}
	}
}

func TestHostDefaultsToShortHostname(t *testing.T) {
	env := testEnv(t, nil)
	env.Hostname = "laptop"
	c, err := Load(Flags{}, env)
	if err != nil || c.Host() != "laptop" {
		t.Fatalf("host = %q err = %v", c.Host(), err)
	}
	env.Hostname = ""
	_, err = Load(Flags{}, env)
	wantKeyError(t, err, "host")
}

func TestProfileMap(t *testing.T) {
	env := testEnv(t, nil)
	path := writeConfig(t, `
[profiles.claude]
rule = "env"
variable = "CLAUDE_CONFIG_DIR"
absent = "claude"
values = {"~/.claude-cldw" = "cldw", "/opt/claude/" = "opt"}

[profiles.codex]
rule = "env"
variable = "CODEX_HOME"
absent = "codex"
`)
	c, err := Load(Flags{Config: new(path)}, env)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.AgentPrograms(); len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Fatalf("programs = %v", got)
	}
	r, ok := c.Profile(core.AgentProgram("claude"))
	if !ok {
		t.Fatal("no claude profile")
	}
	if r.Rule() != "env" || r.Variable() != "CLAUDE_CONFIG_DIR" || r.Absent() != "claude" {
		t.Fatalf("rule = %+v", r)
	}
	v := r.Values()
	if v[filepath.Join(env.Home, ".claude-cldw")] != "cldw" || v["/opt/claude"] != "opt" || len(v) != 2 {
		t.Fatalf("values = %v, want ~/ expanded to %s and /opt/claude/ cleaned", v, env.Home)
	}
	// The Config is immutable: mutating a returned map changes nothing.
	v["/evil"] = "x"
	if r2, _ := c.Profile("claude"); len(r2.Values()) != 2 {
		t.Fatal("Values returned the internal map")
	}
	if _, ok := c.Profile("antigravity"); ok {
		t.Fatal("unexpected antigravity profile")
	}

	rules := c.ProfileRules()
	want := map[core.AgentProgram]core.ProfileRule{
		"claude": {Variable: "CLAUDE_CONFIG_DIR", Absent: "claude",
			Values: map[string]string{filepath.Join(env.Home, ".claude-cldw"): "cldw", "/opt/claude": "opt"}},
		"codex": {Variable: "CODEX_HOME", Absent: "codex", Values: map[string]string{}},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("ProfileRules = %+v, want %+v", rules, want)
	}
	rules["claude"].Values["/evil"] = "x"
	if len(c.ProfileRules()["claude"].Values) != 2 {
		t.Fatal("ProfileRules returned the internal map")
	}
	if ProfileUnresolved != core.ProfileUnresolved {
		t.Fatal("ProfileUnresolved is not core's")
	}
}

func TestExampleConfigParses(t *testing.T) {
	env := testEnv(t, nil)
	c, err := Load(Flags{Config: new(filepath.Join("..", "..", "docs", "config.example.toml"))}, env)
	if err != nil {
		t.Fatalf("docs/config.example.toml: %v", err)
	}
	r, ok := c.Profile("claude")
	if !ok || r.Rule() != RuleEnv || r.Variable() != "CLAUDE_CONFIG_DIR" || r.Absent() != "claude" {
		t.Fatalf("example claude profile = %+v, %v", r, ok)
	}
}

func TestMarshalJSON(t *testing.T) {
	env := testEnv(t, nil)
	path := writeConfig(t, "[profiles.claude]\nrule = \"env\"\nvariable = \"CLAUDE_CONFIG_DIR\"\nabsent = \"claude\"\n")
	c, err := Load(Flags{Config: new(path), Session: new("work"), AllowUnsupportedHerdr: new(true)}, env)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"host":"box"`, `"server":"work"`, `"retention_days":90`,
		`"session":"work"`, `"allow_unsupported_herdr":true`, `"claude_recaps":false`,
		`"data_dir":"` + filepath.Join(env.Home, ".local/share/herddash") + `"`,
		`"database":"` + filepath.Join(env.Home, ".local/share/herddash/herddash.db") + `"`,
		`"profiles":{"claude":{"rule":"env","variable":"CLAUDE_CONFIG_DIR","absent":"claude","values":{}}}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON %s lacks %s", b, want)
		}
	}
}

func TestEnsureDataDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "herddash")
	if err := EnsureDataDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("mode = %#o, want 0700", perm)
	}
	// Idempotent on an owner-only directory.
	if err := EnsureDataDir(dir); err != nil {
		t.Fatal(err)
	}

	loose := filepath.Join(root, "loose")
	if err := os.Mkdir(loose, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	wantKeyError(t, EnsureDataDir(loose), "data_dir")

	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wantKeyError(t, EnsureDataDir(file), "data_dir")

	// Owned by another user: "/" belongs to root.
	if os.Getuid() == 0 {
		t.Log("running as root; skipping the foreign-owner case")
	} else if fi, err := os.Stat("/"); err == nil {
		if uid, ok := ownerUID(fi); !ok || uid == os.Getuid() {
			t.Log("owner of / not determinable or is this user; skipping the foreign-owner case")
		} else {
			err := EnsureDataDir("/")
			wantKeyError(t, err, "data_dir")
			if !strings.Contains(err.Error(), "owned by uid") {
				t.Fatalf("error = %v, want it to name the owner", err)
			}
		}
	}
	wantKeyError(t, EnsureDataDir(filepath.Join(file, "sub")), "data_dir")
}

// TestExampleDocumentsEveryKey checks that every key the file accepts appears
// in docs/config.example.toml, set or commented out.
func TestExampleDocumentsEveryKey(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "config.example.toml"))
	if err != nil {
		t.Fatal(err)
	}
	example := string(data)
	for _, typ := range []reflect.Type{reflect.TypeFor[fileConfig](), reflect.TypeFor[fileProfile]()} {
		for f := range typ.Fields() {
			key := f.Tag.Get("toml")
			var re *regexp.Regexp
			switch key {
			case "profiles":
				re = regexp.MustCompile(`(?m)^\[profiles\.`)
			case "values":
				re = regexp.MustCompile(`(?m)^\[profiles\.[^]]+\.values\]`)
			default:
				re = regexp.MustCompile(`(?m)^#?\s*` + regexp.QuoteMeta(key) + `\s*=`)
			}
			if !re.MatchString(example) {
				t.Errorf("docs/config.example.toml does not document %q", key)
			}
		}
	}
}
