package procenv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
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

func TestSplitProcargs2Argv(t *testing.T) {
	// /bin/zsh's shape on darwin: arguments, and no environment at all.
	data := procargs2(3, "/bin/zsh", 5, []string{"/bin/zsh", "-c", "source /h/.claude/shell-snapshots/snapshot-zsh-1.sh"}, nil)
	args, _, err := splitProcargs2(data)
	want := []string{"/bin/zsh", "-c", "source /h/.claude/shell-snapshots/snapshot-zsh-1.sh"}
	if err != nil || !reflect.DeepEqual(args, want) {
		t.Errorf("args = %q, %v; want %q", args, err, want)
	}
	if args, _, err := splitProcargs2(procargs2(0, "/bin/x", 0, nil, []string{"A=1"})); err != nil || len(args) != 0 {
		t.Errorf("argc 0: %q, %v", args, err)
	}
	if _, _, err := splitProcargs2(procargs2(2, "/bin/x", 0, []string{"x"}, nil)); !errors.Is(err, ErrMalformed) {
		t.Errorf("too few arguments: err = %v, want ErrMalformed", err)
	}
}

// fakeProc writes one /proc/<pid> entry under root: its stat, naming ppid as
// parent after the command name comm, and its cmdline, unless cmdline is nil.
func fakeProc(t *testing.T, root string, pid int, comm string, ppid int, cmdline []byte) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stat := fmt.Sprintf("%d (%s) S %d %d %d 0 -1 4194560 0 0", pid, comm, ppid, pid, pid)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o600); err != nil {
		t.Fatal(err)
	}
	if cmdline != nil {
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), cmdline, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChildrenIn(t *testing.T) {
	root := t.TempDir()
	shell := "/bin/zsh\x00-c\x00source /h/.claude/shell-snapshots/snapshot-zsh-1.sh && sleep 75\x00"
	fakeProc(t, root, 100, "claude", 1, []byte("claude\x00"))
	fakeProc(t, root, 205, "zsh", 100, []byte(shell))
	fakeProc(t, root, 120, "node", 100, []byte("node\x00mcp-server.js\x00"))
	// A command name holding ") " and a digit must not shift the fields.
	fakeProc(t, root, 130, "a) 7 (b", 100, []byte("odd\x00name"))
	fakeProc(t, root, 140, "zsh", 999, []byte("/bin/zsh\x00"))              // another parent
	fakeProc(t, root, 150, "gone", 100, nil)                                // exited before its cmdline was read
	fakeProc(t, root, 160, "zombie", 100, []byte{})                         // a zombie's cmdline reads empty
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o700); err != nil { // not a process entry
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uptime"), []byte("1 2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "170"), 0o700); err != nil { // vanished: no stat
		t.Fatal(err)
	}

	got, err := childrenIn(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []Process{
		{PID: 120, Argv: []string{"node", "mcp-server.js"}},
		{PID: 130, Argv: []string{"odd", "name"}},
		{PID: 160},
		{PID: 205, Argv: []string{"/bin/zsh", "-c", "source /h/.claude/shell-snapshots/snapshot-zsh-1.sh && sleep 75"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("children =\n%+v\nwant\n%+v", got, want)
	}
	if got, err := childrenIn(root, 205); err != nil || len(got) != 0 {
		t.Errorf("a childless process: %+v, %v", got, err)
	}
	if _, err := childrenIn(filepath.Join(root, "missing"), 100); err == nil {
		t.Error("no /proc: no error")
	}
}

func TestStatParent(t *testing.T) {
	cases := []struct {
		stat string
		ppid int
		ok   bool
	}{
		{"42 (zsh) S 7 42 42", 7, true},
		{"42 (a) b) R 9 1", 9, true},
		{"42 (zsh)", 0, false},
		{"42 zsh S 7", 0, false},
		{"42 (zsh) S x", 0, false},
	}
	for _, c := range cases {
		if ppid, ok := statParent([]byte(c.stat)); ppid != c.ppid || ok != c.ok {
			t.Errorf("statParent(%q) = %d, %v; want %d, %v", c.stat, ppid, ok, c.ppid, c.ok)
		}
	}
}

// TestChildrenSelf starts a child of this test process and finds it, with its
// arguments, among the test process's children.
func TestChildrenSelf(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		if _, err := Children(os.Getpid()); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("err = %v, want ErrUnsupported", err)
		}
		return
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep on PATH")
	}
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	got, err := Children(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.PID == cmd.Process.Pid {
			if !reflect.DeepEqual(p.Argv, []string{sleep, "30"}) {
				t.Errorf("argv = %q, want %q", p.Argv, []string{sleep, "30"})
			}
			return
		}
	}
	t.Fatalf("child %d not among %+v", cmd.Process.Pid, got)
}

func TestChildrenBadPID(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if _, err := Children(pid); err == nil {
			t.Errorf("pid %d: no error", pid)
		}
	}
}
