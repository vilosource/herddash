package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/vilosource/herddash/internal/core"
)

// Defaults (AD-8). The port is distinct from herdr-web-ui's 7317.
const (
	DefaultBind          = "127.0.0.1:7318"
	DefaultRetentionDays = 90
	// MaxRetentionDays bounds retention_days so later day arithmetic cannot overflow.
	MaxRetentionDays = 36500
	// DefaultConfigFile is the configuration file read when --config is not given.
	DefaultConfigFile = "~/.config/herddash/config.toml"
	// DataDirName is the data directory's name under $XDG_DATA_HOME or ~/.local/share.
	DataDirName = "herddash"
	// DatabaseName is the database file's name in the data directory.
	DatabaseName = "herddash.db"
	// ProfileUnresolved is the Profile shown when resolution fails. It is
	// reserved, so the Profile Map cannot name a Profile "unresolved".
	ProfileUnresolved = core.ProfileUnresolved
	// RuleEnv is the only Profile rule type: the Profile is chosen by the value
	// of one environment variable in the Agent's process.
	RuleEnv = "env"
)

// Env is everything Load reads from the process environment, injected so tests
// never touch the real home directory or environment.
type Env struct {
	// Home is the operator's home directory. It must be absolute.
	Home string
	// Hostname is the OS hostname, used for the default Host.
	Hostname string
	// Vars holds environment variables. Only HERDR_SOCKET_PATH, HERDR_SESSION
	// and XDG_DATA_HOME are read. An empty value counts as unset.
	Vars map[string]string
}

// OSEnv returns the real process environment.
func OSEnv() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, fmt.Errorf("config: home directory: %w", err)
	}
	host, err := os.Hostname()
	if err != nil {
		return Env{}, fmt.Errorf("config: hostname: %w", err)
	}
	vars := map[string]string{}
	for _, k := range []string{"HERDR_SOCKET_PATH", "HERDR_SESSION", "XDG_DATA_HOME"} {
		vars[k] = os.Getenv(k)
	}
	return Env{Home: home, Hostname: host, Vars: vars}, nil
}

// Flags are the command-line settings. A nil field was not given and does not
// override the file or the default.
type Flags struct {
	// Config is the --config path. Nil means DefaultConfigFile, which may be
	// absent; an explicit path that is empty or absent is an error.
	Config                *string
	Host                  *string
	Session               *string
	Socket                *string
	Server                *string
	Bind                  *string
	DataDir               *string
	Database              *string
	RetentionDays         *int
	AllowUnsupportedHerdr *bool
}

// Error is a configuration error. Key names the offending setting as it is
// spelled in the file (or the environment variable, for HERDR_*), and Source
// says where its value came from.
type Error struct {
	Key    string
	Source string
	Msg    string
	Err    error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("config: ")
	b.WriteString(e.Key)
	if e.Source != "" {
		fmt.Fprintf(&b, " (from %s)", e.Source)
	}
	b.WriteString(": ")
	b.WriteString(e.Msg)
	if e.Err != nil {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// ErrNonLoopbackBind is wrapped by the Error for a bind address that is not
// loopback (FR-17).
var ErrNonLoopbackBind = errors.New("authentication does not exist yet, so herddash binds only to a loopback address")

// ProfileRule is one Agent Program's Profile rule from the Profile Map.
type ProfileRule struct {
	rule     string
	variable string
	absent   string
	values   map[string]string
}

// Rule returns the rule type; it is always RuleEnv for now.
func (p ProfileRule) Rule() string { return p.rule }

// Variable returns the environment variable the rule reads.
func (p ProfileRule) Variable() string { return p.variable }

// Absent returns the Profile used when the variable is unset.
func (p ProfileRule) Absent() string { return p.absent }

// Values returns a copy of the map from variable value to Profile name, with
// any leading "~/" already expanded to the home directory.
func (p ProfileRule) Values() map[string]string { return maps.Clone(p.values) }

// Config is the effective, validated configuration. It is immutable: fields
// are read through accessors, and accessors return copies.
type Config struct {
	file                  string
	host                  string
	session               string
	socketPath            string
	server                string
	bind                  string
	dataDir               string
	database              string
	retentionDays         int
	allowUnsupportedHerdr bool
	profiles              map[core.AgentProgram]ProfileRule
}

// File returns the configuration file that was read, or "" when none was.
func (c Config) File() string { return c.file }

// Host returns this Host's name (AD-3).
func (c Config) Host() string { return c.host }

// Session returns the herdr session named by --session, the file or
// HERDR_SESSION, or "" when the socket was not chosen by session.
func (c Config) Session() string { return c.session }

// SocketPath returns the herdr socket path.
func (c Config) SocketPath() string { return c.socketPath }

// Server returns the herdr server name (AD-3).
func (c Config) Server() string { return c.server }

// Bind returns the loopback address the HTTP surface listens on.
func (c Config) Bind() string { return c.bind }

// DataDir returns the data directory.
func (c Config) DataDir() string { return c.dataDir }

// Database returns the database path.
func (c Config) Database() string { return c.database }

// RetentionDays returns the Retention period in days.
func (c Config) RetentionDays() int { return c.retentionDays }

// AllowUnsupportedHerdr reports FR-16's dogfooding override.
func (c Config) AllowUnsupportedHerdr() bool { return c.allowUnsupportedHerdr }

// Profile returns the Profile rule for an Agent Program, if the Profile Map has one.
func (c Config) Profile(p core.AgentProgram) (ProfileRule, bool) {
	r, ok := c.profiles[p]
	return r, ok
}

// ProfileRules returns the Profile Map as core's rules, one per Agent Program,
// for the engine. The result is a fresh copy.
func (c Config) ProfileRules() map[core.AgentProgram]core.ProfileRule {
	out := make(map[core.AgentProgram]core.ProfileRule, len(c.profiles))
	for p, r := range c.profiles {
		out[p] = core.ProfileRule{Variable: r.variable, Absent: r.absent, Values: r.Values()}
	}
	return out
}

// AgentPrograms returns the Agent Programs the Profile Map has rules for, sorted.
func (c Config) AgentPrograms() []core.AgentProgram {
	out := make([]core.AgentProgram, 0, len(c.profiles))
	for p := range c.profiles {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

type profileJSON struct {
	Rule     string            `json:"rule"`
	Variable string            `json:"variable"`
	Absent   string            `json:"absent"`
	Values   map[string]string `json:"values"`
}

// MarshalJSON renders the effective configuration with snake_case keys.
func (c Config) MarshalJSON() ([]byte, error) {
	profiles := map[string]profileJSON{}
	for p, r := range c.profiles {
		v := r.Values()
		if v == nil {
			v = map[string]string{}
		}
		profiles[string(p)] = profileJSON{Rule: r.rule, Variable: r.variable, Absent: r.absent, Values: v}
	}
	return json.Marshal(struct {
		File                  string                 `json:"config_file,omitempty"`
		Host                  string                 `json:"host"`
		Server                string                 `json:"server"`
		Session               string                 `json:"session,omitempty"`
		SocketPath            string                 `json:"socket_path"`
		Bind                  string                 `json:"bind"`
		DataDir               string                 `json:"data_dir"`
		Database              string                 `json:"database"`
		RetentionDays         int                    `json:"retention_days"`
		AllowUnsupportedHerdr bool                   `json:"allow_unsupported_herdr"`
		Profiles              map[string]profileJSON `json:"profiles"`
	}{
		File:                  c.file,
		Host:                  c.host,
		Server:                c.server,
		Session:               c.session,
		SocketPath:            c.socketPath,
		Bind:                  c.bind,
		DataDir:               c.dataDir,
		Database:              c.database,
		RetentionDays:         c.retentionDays,
		AllowUnsupportedHerdr: c.allowUnsupportedHerdr,
		Profiles:              profiles,
	})
}

// fileConfig is the TOML file's shape. Pointers distinguish absent from zero.
type fileConfig struct {
	Host                  *string                `toml:"host"`
	Session               *string                `toml:"session"`
	SocketPath            *string                `toml:"socket_path"`
	Server                *string                `toml:"server"`
	Bind                  *string                `toml:"bind"`
	DataDir               *string                `toml:"data_dir"`
	Database              *string                `toml:"database"`
	RetentionDays         *int                   `toml:"retention_days"`
	AllowUnsupportedHerdr *bool                  `toml:"allow_unsupported_herdr"`
	Profiles              map[string]fileProfile `toml:"profiles"`
}

type fileProfile struct {
	Rule     *string           `toml:"rule"`
	Variable *string           `toml:"variable"`
	Absent   *string           `toml:"absent"`
	Values   map[string]string `toml:"values"`
}

// setting is one resolved value and where it came from.
type setting[T any] struct {
	value  T
	source string
	set    bool
}

// pick returns the flag value if given, else the file value if present, else
// an unset setting.
func pick[T any](flag *T, flagName string, file *T, fileName string) setting[T] {
	switch {
	case flag != nil:
		return setting[T]{value: *flag, source: "flag " + flagName, set: true}
	case file != nil:
		return setting[T]{value: *file, source: fileName, set: true}
	}
	return setting[T]{}
}

// Load builds the effective configuration: flags beat the file, the file beats
// the defaults. Every value is validated, and the first problem is returned as
// an *Error naming the key, before anything else happens. Load reads only the
// configuration file; it creates nothing.
func Load(flags Flags, env Env) (Config, error) {
	if !filepath.IsAbs(env.Home) {
		return Config{}, &Error{Key: "home", Msg: fmt.Sprintf("home directory %q is not absolute", env.Home)}
	}

	fc, file, err := readFile(flags.Config, env.Home)
	if err != nil {
		return Config{}, err
	}
	fileName := file
	if fileName == "" {
		fileName = "file"
	}

	c := Config{file: file}

	// A file value overridden by a flag is still validated, so a bad file
	// fails now rather than when the flag is dropped.
	for _, n := range []struct {
		key string
		v   *string
	}{{"host", fc.Host}, {"session", fc.Session}, {"server", fc.Server}} {
		if n.v == nil {
			continue
		}
		if err := validateName(*n.v); err != nil {
			return Config{}, &Error{Key: n.key, Source: fileName, Msg: fmt.Sprintf("%q: %v", *n.v, err)}
		}
	}

	// Host and Server, with the socket path, come from one function (AD-3).
	t, err := resolveTarget(targetInput{
		host:    pick(flags.Host, "--host", fc.Host, fileName),
		session: pick(flags.Session, "--session", fc.Session, fileName),
		socket:  pick(flags.Socket, "--socket", fc.SocketPath, fileName),
		server:  pick(flags.Server, "--server", fc.Server, fileName),
	}, env)
	if err != nil {
		return Config{}, err
	}
	c.host, c.session, c.socketPath, c.server = t.Host, t.Session, t.SocketPath, t.Server

	bind := pick(flags.Bind, "--bind", fc.Bind, fileName)
	if !bind.set {
		bind = setting[string]{value: DefaultBind, source: "default", set: true}
	}
	if err := validateBind(bind.value); err != nil {
		return Config{}, &Error{Key: "bind", Source: bind.source, Msg: fmt.Sprintf("%q", bind.value), Err: err}
	}
	c.bind = bind.value

	dataDir := pick(flags.DataDir, "--data-dir", fc.DataDir, fileName)
	if dataDir.set {
		c.dataDir, err = absPath("data_dir", dataDir, env.Home)
		if err != nil {
			return Config{}, err
		}
	} else if x := env.Vars["XDG_DATA_HOME"]; filepath.IsAbs(x) {
		// The XDG specification says a relative value is invalid and ignored.
		c.dataDir = filepath.Join(x, DataDirName)
	} else {
		c.dataDir = filepath.Join(env.Home, ".local", "share", DataDirName)
	}

	database := pick(flags.Database, "--database", fc.Database, fileName)
	if database.set {
		c.database, err = absPath("database", database, env.Home)
		if err != nil {
			return Config{}, err
		}
	} else {
		c.database = filepath.Join(c.dataDir, DatabaseName)
	}

	retention := pick(flags.RetentionDays, "--retention-days", fc.RetentionDays, fileName)
	c.retentionDays = DefaultRetentionDays
	if retention.set {
		if retention.value < 1 || retention.value > MaxRetentionDays {
			return Config{}, &Error{Key: "retention_days", Source: retention.source,
				Msg: fmt.Sprintf("%d: must be from 1 to %d", retention.value, MaxRetentionDays)}
		}
		c.retentionDays = retention.value
	}

	allow := pick(flags.AllowUnsupportedHerdr, "--allow-unsupported-herdr", fc.AllowUnsupportedHerdr, fileName)
	c.allowUnsupportedHerdr = allow.set && allow.value

	c.profiles, err = buildProfiles(fc.Profiles, fileName, env.Home)
	if err != nil {
		return Config{}, err
	}
	return c, nil
}

// readFile reads and decodes the configuration file. It returns the path that
// was read, or "" when the default path is absent.
func readFile(explicit *string, home string) (fileConfig, string, error) {
	path, isDefault := DefaultConfigFile, true
	if explicit != nil {
		if *explicit == "" {
			return fileConfig{}, "", &Error{Key: "config", Source: "flag --config", Msg: "path is empty"}
		}
		path, isDefault = *explicit, false
	}
	path = expandHome(path, home)
	data, err := os.ReadFile(path)
	if err != nil {
		if isDefault && errors.Is(err, fs.ErrNotExist) {
			// Absent is fine; a dangling symlink means a file was meant to be there.
			if _, lerr := os.Lstat(path); lerr == nil {
				return fileConfig{}, "", &Error{Key: "config", Source: path,
					Msg: "the default configuration file is a symlink whose target is missing", Err: err}
			}
			return fileConfig{}, "", nil
		}
		return fileConfig{}, "", &Error{Key: "config", Msg: "cannot read configuration file", Err: err}
	}
	var fc fileConfig
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&fc)
	if err != nil {
		return fileConfig{}, "", &Error{Key: decodeErrorKey(err), Source: path, Msg: "invalid TOML", Err: err}
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return fileConfig{}, "", &Error{Key: undecoded[0].String(), Source: path, Msg: "unknown key"}
	}
	return fc, path, nil
}

// lastKeyRE matches the key in BurntSushi/toml's type-mismatch errors.
var lastKeyRE = regexp.MustCompile(`\(last key "((?:[^"\\]|\\.)*)"\)`)

// decodeErrorKey extracts the key a TOML decode error is about, when the
// parser reports one, so a type mismatch still names its key.
func decodeErrorKey(err error) string {
	var pe toml.ParseError
	if errors.As(err, &pe) && pe.LastKey != "" {
		return pe.LastKey
	}
	// A type mismatch is a plain error whose text carries the key.
	if m := lastKeyRE.FindStringSubmatch(err.Error()); m != nil {
		if k, err := strconv.Unquote(`"` + m[1] + `"`); err == nil && k != "" {
			return k
		}
	}
	return "config"
}

// absPath expands a leading "~/" and requires the result to be absolute, since
// the Daemon runs under launchd or systemd with no meaningful working directory.
func absPath(key string, s setting[string], home string) (string, error) {
	p := expandHome(s.value, home)
	if !filepath.IsAbs(p) {
		return "", &Error{Key: key, Source: s.source, Msg: fmt.Sprintf("%q: must be an absolute path", s.value)}
	}
	return filepath.Clean(p), nil
}

// expandHome replaces a bare "~" or a leading "~/" with the home directory.
func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return p
}

// validateBind accepts host:port where host is exactly localhost, 127.0.0.1
// or ::1 (written [::1]:port), the hosts the HTTP surface's Host check
// accepts, and port is all digits from 0 to 65535. Port 0 lets the kernel pick
// a free port, which serve prints; tests use it so none binds the default.
// Another loopback address would start a server that refuses every request,
// so it is refused here; a non-loopback one is ErrNonLoopbackBind.
func validateBind(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 || strings.Trim(port, "0123456789") != "" {
		return fmt.Errorf("port %q must be a number from 0 to 65535", port)
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return fmt.Errorf("host %q must be localhost, 127.0.0.1 or ::1, the hosts the API accepts", host)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return fmt.Errorf("host %q must be localhost, 127.0.0.1 or ::1, the hosts the API accepts", host)
	}
	return ErrNonLoopbackBind
}

func buildProfiles(in map[string]fileProfile, source, home string) (map[core.AgentProgram]ProfileRule, error) {
	out := make(map[core.AgentProgram]ProfileRule, len(in))
	programs := make([]string, 0, len(in))
	for p := range in {
		programs = append(programs, p)
	}
	slices.Sort(programs)
	for _, program := range programs {
		fp := in[program]
		key := "profiles." + program
		if strings.TrimSpace(program) == "" {
			return nil, &Error{Key: "profiles", Source: source, Msg: "agent program name must be non-empty"}
		}
		if strings.TrimSpace(program) != program {
			return nil, &Error{Key: fmt.Sprintf("profiles.%q", program), Source: source,
				Msg: "agent program name must not have surrounding whitespace"}
		}
		required := func(field string, v *string) (string, error) {
			if v == nil || strings.TrimSpace(*v) == "" {
				return "", &Error{Key: key + "." + field, Source: source, Msg: "must be set and non-empty"}
			}
			return *v, nil
		}
		rule, err := required("rule", fp.Rule)
		if err != nil {
			return nil, err
		}
		if rule != RuleEnv {
			return nil, &Error{Key: key + ".rule", Source: source,
				Msg: fmt.Sprintf("%q: unknown rule type; the only rule type is %q", rule, RuleEnv)}
		}
		variable, err := required("variable", fp.Variable)
		if err != nil {
			return nil, err
		}
		if strings.IndexFunc(variable, func(r rune) bool { return r == '=' || r == 0 || unicode.IsSpace(r) }) >= 0 {
			return nil, &Error{Key: key + ".variable", Source: source,
				Msg: fmt.Sprintf("%q: an environment variable name cannot contain '=', whitespace or NUL", variable)}
		}
		absent, err := required("absent", fp.Absent)
		if err != nil {
			return nil, err
		}
		if absent == ProfileUnresolved {
			return nil, &Error{Key: key + ".absent", Source: source, Msg: fmt.Sprintf("%q is reserved", ProfileUnresolved)}
		}
		values := make(map[string]string, len(fp.Values))
		raw := make([]string, 0, len(fp.Values))
		for v := range fp.Values {
			raw = append(raw, v)
		}
		slices.Sort(raw)
		for _, v := range raw {
			name := fp.Values[v]
			vkey := fmt.Sprintf("%s.values.%q", key, v)
			if strings.TrimSpace(v) == "" {
				return nil, &Error{Key: key + ".values", Source: source, Msg: "a value must be non-empty"}
			}
			if strings.TrimSpace(name) == "" {
				return nil, &Error{Key: vkey, Source: source, Msg: "Profile name must be non-empty"}
			}
			if name == ProfileUnresolved {
				return nil, &Error{Key: vkey, Source: source, Msg: fmt.Sprintf("Profile name %q is reserved", ProfileUnresolved)}
			}
			expanded := expandHome(v, home)
			if filepath.IsAbs(expanded) {
				expanded = filepath.Clean(expanded)
			}
			if _, dup := values[expanded]; dup {
				return nil, &Error{Key: vkey, Source: source, Msg: fmt.Sprintf("duplicates another value once expanded to %q", expanded)}
			}
			values[expanded] = name
		}
		out[core.AgentProgram(program)] = ProfileRule{rule: rule, variable: variable, absent: absent, values: values}
	}
	return out, nil
}
