package herddash_test

// These tests check the service units under deploy/ against Story 4.2's rules:
// both run `~/.local/bin/herddash serve` and nothing else, set no HERDR_*
// variable, restart only after a failure and never in a tight loop, discard
// standard output, and keep standard error where docs/install.md says it is.
// They never install, load or start anything.

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	plistPath   = "deploy/launchd/io.github.vilosource.herddash.plist"
	unitPath    = "deploy/systemd/herddash.service"
	homeHolder  = "__HOME__"
	minThrottle = 10 // seconds; anything shorter is a tight loop
)

// decodePlist decodes an XML property list into Go values: dict is
// map[string]any, array []any, string string, integer int64, true/false bool.
func decodePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local != "plist" {
				return nil, fmt.Errorf("root element is %q, not plist", se.Name.Local)
			}
			v, end, err := plistValue(d)
			if err != nil {
				return nil, err
			}
			if end {
				return nil, fmt.Errorf("empty plist")
			}
			return v, nil
		}
	}
}

// plistValue reads the next value element. end is true if the enclosing
// element closed instead.
func plistValue(d *xml.Decoder) (v any, end bool, err error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, false, err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil, true, nil
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				m := map[string]any{}
				for {
					tok, err := d.Token()
					if err != nil {
						return nil, false, err
					}
					switch kt := tok.(type) {
					case xml.EndElement:
						return m, false, nil
					case xml.StartElement:
						if kt.Name.Local != "key" {
							return nil, false, fmt.Errorf("dict holds <%s> where a <key> belongs", kt.Name.Local)
						}
						var key string
						if err := d.DecodeElement(&key, &kt); err != nil {
							return nil, false, err
						}
						if _, dup := m[key]; dup {
							return nil, false, fmt.Errorf("duplicate key %q", key)
						}
						val, end, err := plistValue(d)
						if err != nil {
							return nil, false, err
						}
						if end {
							return nil, false, fmt.Errorf("key %q has no value", key)
						}
						m[key] = val
					}
				}
			case "array":
				var a []any
				for {
					val, end, err := plistValue(d)
					if err != nil {
						return nil, false, err
					}
					if end {
						return a, false, nil
					}
					a = append(a, val)
				}
			case "string":
				var s string
				err := d.DecodeElement(&s, &t)
				return s, false, err
			case "integer":
				var s string
				if err := d.DecodeElement(&s, &t); err != nil {
					return nil, false, err
				}
				n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
				return n, false, err
			case "true", "false":
				if err := d.Skip(); err != nil {
					return nil, false, err
				}
				return t.Name.Local == "true", false, nil
			default:
				return nil, false, fmt.Errorf("unsupported plist element <%s>", t.Name.Local)
			}
		}
	}
}

// plistStrings returns every key and string value in a decoded plist.
func plistStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			out = append(out, plistStrings(e)...)
		}
		return out
	case map[string]any:
		var out []string
		for k, e := range t {
			out = append(out, k)
			out = append(out, plistStrings(e)...)
		}
		return out
	}
	return nil
}

func TestLaunchdAgent(t *testing.T) {
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	v, err := decodePlist(data)
	if err != nil {
		t.Fatalf("parse %s: %v", plistPath, err)
	}
	top, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: top-level value is %T, not a dict", plistPath, v)
	}

	if got := top["Label"]; got != "io.github.vilosource.herddash" {
		t.Errorf("Label = %v, want io.github.vilosource.herddash", got)
	}
	if _, ok := top["Program"]; ok {
		t.Error("Program is set; the binary belongs in ProgramArguments[0] only")
	}
	args, ok := top["ProgramArguments"].([]any)
	want := []string{homeHolder + "/.local/bin/herddash", "serve"}
	if !ok || len(args) != len(want) {
		t.Errorf("ProgramArguments = %#v, want exactly %q", top["ProgramArguments"], want)
	} else {
		for i, w := range want {
			if s, _ := args[i].(string); s != w {
				t.Errorf("ProgramArguments[%d] = %#v, want %q", i, args[i], w)
			}
		}
	}
	if top["RunAtLoad"] != true {
		t.Errorf("RunAtLoad = %v, want true", top["RunAtLoad"])
	}
	ka, ok := top["KeepAlive"].(map[string]any)
	if !ok {
		t.Fatalf("KeepAlive = %v, want a dict with SuccessfulExit false", top["KeepAlive"])
	}
	if len(ka) != 1 || ka["SuccessfulExit"] != false {
		t.Errorf("KeepAlive = %v, want exactly {SuccessfulExit: false}", ka)
	}
	if n, _ := top["ThrottleInterval"].(int64); n < minThrottle {
		t.Errorf("ThrottleInterval = %v, want at least %d", top["ThrottleInterval"], minThrottle)
	}
	if env, ok := top["EnvironmentVariables"].(map[string]any); ok {
		for k := range env {
			if strings.HasPrefix(k, "HERDR_") {
				t.Errorf("EnvironmentVariables sets %s; the Daemon must resolve the socket itself", k)
			}
		}
	}
	for _, s := range plistStrings(top) {
		if strings.Contains(s, "HERDR_") {
			t.Errorf("%s holds %q; no herdr variable may be set", plistPath, s)
		}
		// Every path must be absolute once __HOME__ is replaced, because
		// launchd expands neither ~ nor variables.
		if strings.ContainsAny(s, "~$") {
			t.Errorf("%s holds %q; launchd expands neither ~ nor $, use %s", plistPath, s, homeHolder)
		}
	}
	if got := top["StandardOutPath"]; got != "/dev/null" {
		t.Errorf("StandardOutPath = %v, want /dev/null", got)
	}
	if got, want := top["StandardErrorPath"], homeHolder+"/.local/share/herddash/herddash.stderr.log"; got != want {
		t.Errorf("StandardErrorPath = %v, want %s", got, want)
	}
	if got := top["Umask"]; got != int64(0o077) {
		t.Errorf("Umask = %v, want 63 (077)", got)
	}
	if got := top["ProcessType"]; got != "Background" {
		t.Errorf("ProcessType = %v, want Background", got)
	}
	if _, ok := top["UserName"]; ok {
		t.Error("UserName is set; the agent runs as whoever loads it")
	}
}

func TestLaunchdAgentLints(t *testing.T) {
	plutil, err := exec.LookPath("plutil")
	if err != nil {
		t.Skip("plutil not found (not macOS)")
	}
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), filepath.Base(plistPath))
	if err := os.WriteFile(f, bytes.ReplaceAll(data, []byte(homeHolder), []byte("/Users/example")), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(plutil, "-lint", f).CombinedOutput(); err != nil {
		t.Fatalf("plutil -lint: %v\n%s", err, out)
	}
}

// parseUnit reads a systemd unit file into section -> key -> values, in order.
func parseUnit(r io.Reader) (map[string]map[string][]string, error) {
	units := map[string]map[string][]string{}
	section := ""
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";"):
			continue
		case strings.HasSuffix(line, "\\"):
			return nil, fmt.Errorf("line %d: continuation lines are not supported here", n)
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = line[1 : len(line)-1]
			if units[section] == nil {
				units[section] = map[string][]string{}
			}
		default:
			k, v, ok := strings.Cut(line, "=")
			if !ok || section == "" {
				return nil, fmt.Errorf("line %d: %q is not a key=value in a section", n, line)
			}
			k = strings.TrimSpace(k)
			units[section][k] = append(units[section][k], strings.TrimSpace(v))
		}
	}
	return units, sc.Err()
}

func TestSystemdUnit(t *testing.T) {
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	u, err := parseUnit(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse %s: %v", unitPath, err)
	}
	one := func(section, key string) string {
		t.Helper()
		vs := u[section][key]
		if len(vs) != 1 {
			t.Errorf("[%s] %s is set %d times, want once", section, key, len(vs))
			return ""
		}
		return vs[0]
	}
	seconds := func(section, key string) float64 {
		t.Helper()
		s := one(section, key)
		n, err := parseTimespan(s)
		if err != nil {
			t.Errorf("[%s] %s = %q: %v", section, key, s, err)
		}
		return n
	}
	svc := u["Service"]
	if svc == nil {
		t.Fatalf("%s has no [Service] section", unitPath)
	}

	if got, want := one("Service", "ExecStart"), "%h/.local/bin/herddash serve"; got != want {
		t.Errorf("ExecStart = %q, want %q", got, want)
	}
	if got := one("Service", "Restart"); got != "on-failure" {
		t.Errorf("Restart = %q, want on-failure", got)
	}
	restart := seconds("Service", "RestartSec")
	if restart < minThrottle {
		t.Errorf("RestartSec = %vs, want at least %d", restart, minThrottle)
	}
	// A start limit stops a crash loop only if it can trip: Burst starts,
	// RestartSec apart, must fit inside the interval.
	interval := seconds("Unit", "StartLimitIntervalSec")
	burst, err := strconv.Atoi(one("Unit", "StartLimitBurst"))
	if err != nil || burst < 1 {
		t.Errorf("StartLimitBurst = %q, want a positive count", one("Unit", "StartLimitBurst"))
	} else if restart*float64(burst-1) >= interval {
		t.Errorf("RestartSec %vs x (StartLimitBurst %d - 1) >= StartLimitIntervalSec %vs: the limit never trips",
			restart, burst, interval)
	}
	if got := one("Service", "Type"); got != "simple" {
		t.Errorf("Type = %q, want simple", got)
	}
	if got := one("Service", "UMask"); got != "0077" {
		t.Errorf("UMask = %q, want 0077", got)
	}
	// The user manager can carry HERDR_* (imported from a herdr pane); the
	// unit must drop both so the Daemon resolves the socket itself.
	unset := map[string]bool{}
	for _, v := range svc["UnsetEnvironment"] {
		for _, f := range strings.Fields(v) {
			unset[f] = true
		}
	}
	for _, k := range []string{"HERDR_SOCKET_PATH", "HERDR_SESSION"} {
		if !unset[k] {
			t.Errorf("UnsetEnvironment does not name %s", k)
		}
	}
	if got := one("Service", "StandardOutput"); got != "null" {
		t.Errorf("StandardOutput = %q, want null", got)
	}
	if got := one("Service", "StandardError"); got != "journal" {
		t.Errorf("StandardError = %q, want journal", got)
	}
	for _, k := range []string{"User", "Group", "DynamicUser"} {
		if _, ok := svc[k]; ok {
			t.Errorf("[Service] %s is set; a user unit runs as its user", k)
		}
	}
	for section, keys := range u {
		for k, vs := range keys {
			for _, v := range vs {
				if k != "UnsetEnvironment" && strings.Contains(k+"="+v, "HERDR_") {
					t.Errorf("[%s] %s=%s; no herdr variable may be set", section, k, v)
				}
			}
		}
	}
	for _, k := range []string{"Environment", "EnvironmentFile", "PassEnvironment"} {
		if _, ok := svc[k]; ok {
			t.Errorf("[Service] %s is set; the unit sets no environment", k)
		}
	}
	if got := one("Install", "WantedBy"); got != "default.target" {
		t.Errorf("WantedBy = %q, want default.target (user session)", got)
	}
}

func TestSystemdUnitVerifies(t *testing.T) {
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze not found (not Linux with systemd)")
	}
	// verify checks that ExecStart's binary exists, so point it at a stand-in
	// in a temporary home. Nothing is installed or started.
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "herddash"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(home, filepath.Base(unitPath))
	if err := os.WriteFile(f, bytes.ReplaceAll(data, []byte("%h"), []byte(home)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(analyze, "verify", "--man=no", f).CombinedOutput()
	if err != nil {
		t.Fatalf("systemd-analyze verify: %v\n%s", err, out)
	}
}

// parseTimespan parses a systemd time span ("30", "30s", "10min", "5m 30s")
// into seconds. A bare number is seconds.
func parseTimespan(s string) (float64, error) {
	units := map[string]float64{
		"us": 1e-6, "usec": 1e-6, "ms": 1e-3, "msec": 1e-3,
		"": 1, "s": 1, "sec": 1, "second": 1, "seconds": 1,
		"m": 60, "min": 60, "minute": 60, "minutes": 60,
		"h": 3600, "hr": 3600, "hour": 3600, "hours": 3600,
		"d": 86400, "day": 86400, "days": 86400,
		"w": 604800, "week": 604800, "weeks": 604800,
	}
	m := timespanRE.FindAllStringSubmatchIndex(s, -1)
	if len(m) == 0 {
		return 0, fmt.Errorf("not a time span")
	}
	total, end := 0.0, 0
	for _, g := range m {
		if strings.TrimSpace(s[end:g[0]]) != "" {
			return 0, fmt.Errorf("not a time span")
		}
		n, err := strconv.ParseFloat(s[g[2]:g[3]], 64)
		if err != nil {
			return 0, err
		}
		mult, ok := units[s[g[4]:g[5]]]
		if !ok {
			return 0, fmt.Errorf("unknown unit %q", s[g[4]:g[5]])
		}
		total += n * mult
		end = g[1]
	}
	if strings.TrimSpace(s[end:]) != "" {
		return 0, fmt.Errorf("not a time span")
	}
	return total, nil
}

var timespanRE = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([a-z]*)`)

func TestParseTimespan(t *testing.T) {
	for in, want := range map[string]float64{
		"30": 30, "30s": 30, "10min": 600, "5m 30s": 330, "1h": 3600, "500ms": 0.5,
	} {
		if got, err := parseTimespan(in); err != nil || got != want {
			t.Errorf("parseTimespan(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "x", "5 parsecs", "10min x"} {
		if _, err := parseTimespan(in); err == nil {
			t.Errorf("parseTimespan(%q) succeeded, want an error", in)
		}
	}
}

// TestInstallGuideMatchesUnits ties docs/install.md to the values in the unit
// files, so changing one without the other fails.
func TestInstallGuideMatchesUnits(t *testing.T) {
	guide, err := os.ReadFile("docs/install.md")
	if err != nil {
		t.Fatal(err)
	}
	pdata, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	v, err := decodePlist(pdata)
	if err != nil {
		t.Fatal(err)
	}
	top, _ := v.(map[string]any)
	udata, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	u, err := parseUnit(bytes.NewReader(udata))
	if err != nil {
		t.Fatal(err)
	}
	first := func(section, key string) string {
		if vs := u[section][key]; len(vs) > 0 {
			return vs[0]
		}
		return "<unset>"
	}
	label, _ := top["Label"].(string)
	stderr, _ := top["StandardErrorPath"].(string)
	want := []string{
		"gui/$(id -u)/" + label,
		filepath.Base(plistPath),
		filepath.Base(unitPath),
		fmt.Sprintf("`ThrottleInterval` %v", top["ThrottleInterval"]),
		"`RestartSec=" + first("Service", "RestartSec") + "`",
		"`StartLimitBurst=" + first("Unit", "StartLimitBurst") + "`",
		"`StartLimitIntervalSec=" + first("Unit", "StartLimitIntervalSec") + "`",
		strings.Replace(stderr, homeHolder, "~", 1),
	}
	for _, w := range want {
		if !bytes.Contains(guide, []byte(w)) {
			t.Errorf("docs/install.md does not mention %q from the unit files", w)
		}
	}
}
