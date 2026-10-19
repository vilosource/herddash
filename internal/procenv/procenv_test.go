package procenv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// procargs2 builds a kern.procargs2 buffer: argc, the executable path, NUL
// padding, the arguments, then the environment.
func procargs2(argc int32, path string, pad int, args, env []string) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.NativeEndian, argc)
	b.WriteString(path)
	b.WriteByte(0)
	b.Write(make([]byte, pad))
	for _, s := range append(append([]string{}, args...), env...) {
		b.WriteString(s)
		b.WriteByte(0)
	}
	return b.Bytes()
}

var fixtureEnv = []string{
	"CLAUDE_CONFIG_DIR=/h/.claude-cldw",
	"TOKEN=a=b=c",
	"EMPTY=",
}

func TestParseProcargs2(t *testing.T) {
	data := procargs2(2, "/usr/local/bin/claude", 7, []string{"claude", "CLAUDE_CONFIG_DIR=/not/env"}, fixtureEnv)
	cases := []struct {
		name  string
		value string
		set   bool
	}{
		{"CLAUDE_CONFIG_DIR", "/h/.claude-cldw", true},
		{"TOKEN", "a=b=c", true},
		{"EMPTY", "", true},
		{"MISSING", "", false},
		{"CLAUDE", "", false}, // a prefix of a name is not the name
	}
	for _, c := range cases {
		v, set, err := parseProcargs2(data, c.name)
		if err != nil || v != c.value || set != c.set {
			t.Errorf("%s: got %q, %v, %v; want %q, %v", c.name, v, set, err, c.value, c.set)
		}
	}
}

func TestParseProcargs2Shapes(t *testing.T) {
	// No padding, and an empty string ending the environment early.
	data := procargs2(1, "/bin/x", 0, []string{"x"}, []string{"A=1", "", "B=2"})
	if v, set, err := parseProcargs2(data, "A"); err != nil || !set || v != "1" {
		t.Errorf("A: %q %v %v", v, set, err)
	}
	if _, set, err := parseProcargs2(data, "B"); err != nil || set {
		t.Errorf("B after the empty string: set %v, err %v", set, err)
	}
	// A last entry without its NUL.
	data = append(procargs2(0, "/bin/x", 3, nil, nil), "A=tail"...)
	if v, set, err := parseProcargs2(data, "A"); err != nil || !set || v != "tail" {
		t.Errorf("unterminated last entry: %q %v %v", v, set, err)
	}
	// No environment at all, as macOS shows a platform binary's: not "unset".
	if _, set, err := parseProcargs2(procargs2(1, "/bin/x", 0, []string{"x"}, nil), "A"); set || !errors.Is(err, ErrNoEnvironment) {
		t.Errorf("no environment: set %v, err %v; want ErrNoEnvironment", set, err)
	}
}

func TestParseProcargs2Malformed(t *testing.T) {
	if _, _, err := parseProcargs2(nil, "A"); !errors.Is(err, ErrNoEnvironment) {
		t.Errorf("empty: err = %v, want ErrNoEnvironment", err)
	}
	cases := map[string][]byte{
		"short argc":            {1, 0},
		"negative argc":         procargs2(-1, "/bin/x", 0, nil, nil),
		"path not terminated":   append(procargs2(0, "", 0, nil, nil)[:4], "/bin/x"...),
		"too few arguments":     procargs2(3, "/bin/x", 2, []string{"x"}, nil),
		"argument unterminated": append(procargs2(2, "/bin/x", 0, []string{"x"}, nil), "y"...),
	}
	for name, data := range cases {
		if _, _, err := parseProcargs2(data, "A"); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
	}
}

func TestParseEnviron(t *testing.T) {
	data := []byte(strings.Join(append(fixtureEnv, "CLAUDE_CONFIG_DIR=/second"), "\x00") + "\x00")
	cases := []struct {
		name  string
		value string
		set   bool
	}{
		{"CLAUDE_CONFIG_DIR", "/h/.claude-cldw", true}, // the first entry wins
		{"TOKEN", "a=b=c", true},
		{"EMPTY", "", true},
		{"MISSING", "", false},
		{"CLAUDE", "", false},
	}
	for _, c := range cases {
		v, set, err := parseEnviron(data, c.name)
		if err != nil || v != c.value || set != c.set {
			t.Errorf("%s: got %q, %v, %v; want %q, %v", c.name, v, set, err, c.value, c.set)
		}
	}
	// A zombie's or exiting process's environ reads empty: not "unset".
	if _, set, err := parseEnviron(nil, "A"); set || !errors.Is(err, ErrNoEnvironment) {
		t.Errorf("empty environ: set %v, err %v; want ErrNoEnvironment", set, err)
	}
}

// TestLookupSelf reads this test process's own environment, as it was started.
func TestLookupSelf(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		if _, _, err := Lookup(os.Getpid(), "PATH"); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("err = %v, want ErrUnsupported", err)
		}
		return
	}
	env := os.Environ()
	if len(env) == 0 {
		t.Skip("the test process has no environment")
	}
	name, want, _ := strings.Cut(env[0], "=")
	v, set, err := Lookup(os.Getpid(), name)
	if err != nil || !set || v != want {
		t.Fatalf("Lookup(%s) = set %v, err %v; value matches: %v", name, set, err, v == want)
	}
	if _, set, err := Lookup(os.Getpid(), "HERDDASH_PROCENV_SURELY_UNSET"); err != nil || set {
		t.Fatalf("unset variable: set %v, err %v", set, err)
	}
}

func TestLookupBadPID(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if _, _, err := Lookup(pid, "PATH"); err == nil {
			t.Errorf("pid %d: no error", pid)
		}
	}
}
