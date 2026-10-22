package procenv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnsupported is wrapped by Lookup's error on an OS with no reader.
var ErrUnsupported = errors.New("procenv: reading another process's environment is not supported on this OS")

// ErrNoEnvironment is wrapped by the error for an environment read that
// returned nothing, as for a zombie or exiting process: it cannot show the
// variable is unset.
var ErrNoEnvironment = errors.New("procenv: the process environment read returned nothing")

// ErrMalformed is wrapped by the error for process arguments that do not have
// the shape the kernel documents.
var ErrMalformed = errors.New("procenv: malformed process arguments")

// Lookup reads the environment variable name from the process pid, as the
// process was started: set is false when the process has no such variable.
// It fails when the environment cannot be read, for example because the
// process is gone or belongs to another user, and on an OS with no reader.
//
// A value read here may be secret: never log it.
func Lookup(pid int, name string) (value string, set bool, err error) {
	if pid <= 0 {
		return "", false, fmt.Errorf("procenv: pid %d: not a process id", pid)
	}
	return lookup(pid, name)
}

// parseEnviron finds name in a NUL-separated list of NAME=value strings, as
// /proc/<pid>/environ holds. The first entry for name wins, as getenv's does.
// Empty data is an error wrapping ErrNoEnvironment.
func parseEnviron(data []byte, name string) (string, bool, error) {
	if len(data) == 0 {
		return "", false, ErrNoEnvironment
	}
	v, ok := findEnv(bytes.Split(data, []byte{0}), name)
	return v, ok, nil
}

// parseProcargs2 finds name in darwin's kern.procargs2 for one process: a
// native-endian 32-bit argc, the executable path and its NUL terminator, NUL
// padding, argc NUL-terminated argument strings, then NUL-terminated NAME=value
// strings, ended by an empty string or the end of the buffer. It returns an
// error wrapping ErrNoEnvironment for an empty buffer or one with no
// environment entries, and ErrMalformed when
// the buffer is too short for argc, the path or the arguments.
func parseProcargs2(data []byte, name string) (string, bool, error) {
	if len(data) == 0 {
		return "", false, ErrNoEnvironment
	}
	_, rest, err := splitProcargs2(data)
	if err != nil {
		return "", false, err
	}
	var entries [][]byte
	for len(rest) > 0 {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			end = len(rest)
		}
		if end == 0 {
			break // an empty string ends the environment
		}
		entries = append(entries, rest[:end])
		if end == len(rest) {
			break
		}
		rest = rest[end+1:]
	}
	if len(entries) == 0 {
		// macOS withholds an Apple platform binary's environment from
		// another user process: no entries cannot show the variable is
		// unset.
		return "", false, ErrNoEnvironment
	}
	v, ok := findEnv(entries, name)
	return v, ok, nil
}

// splitProcargs2 splits a kern.procargs2 buffer, laid out as parseProcargs2
// describes, into its argc arguments and the rest, which holds the
// environment. It returns an error wrapping ErrMalformed when the buffer is
// too short for argc, the path or the arguments.
func splitProcargs2(data []byte) (args []string, rest []byte, err error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("%w: %d bytes, too short for argc", ErrMalformed, len(data))
	}
	argc := int(int32(binary.NativeEndian.Uint32(data)))
	if argc < 0 {
		return nil, nil, fmt.Errorf("%w: argc %d", ErrMalformed, argc)
	}
	rest = data[4:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return nil, nil, fmt.Errorf("%w: executable path not terminated", ErrMalformed)
	}
	rest = rest[end:]
	// Skip the path's terminator and the padding after it.
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	args = make([]string, 0, argc)
	for i := range argc {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return nil, nil, fmt.Errorf("%w: %d of %d arguments", ErrMalformed, i, argc)
		}
		args = append(args, string(rest[:end]))
		rest = rest[end+1:]
	}
	return args, rest, nil
}

// findEnv returns the value of the first NAME=value entry for name.
func findEnv(entries [][]byte, name string) (string, bool) {
	prefix := []byte(name + "=")
	for _, e := range entries {
		if v, ok := bytes.CutPrefix(e, prefix); ok {
			return string(v), true
		}
	}
	return "", false
}

// Process is one process as Children reports it: its id and its arguments,
// argv[0] first.
type Process struct {
	PID  int
	Argv []string
}

// Children returns the direct children of the process pid, in pid order, each
// with its arguments: on linux from /proc, on darwin from kern.proc.all and
// kern.procargs2. A child that exits while they are read, or whose arguments
// cannot be read, is left out rather than failing the read. It fails when the
// process table cannot be read, and on an OS with no reader.
//
// The table of parents is read at most once per tableTTL and shared by every
// caller, so watching many Agents costs one table read, not one each; only the
// children's arguments are read per call.
//
// Arguments may hold secrets: never log them.
func Children(pid int) ([]Process, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("procenv: pid %d: not a process id", pid)
	}
	parents, err := cachedTable()
	if err != nil {
		return nil, fmt.Errorf("procenv: children of pid %d: %w", pid, err)
	}
	return argvsOf(parents[pid], argv), nil
}

// tableTTL is how long one read of the process table serves Children.
const tableTTL = time.Second

var tableCache struct {
	sync.Mutex
	at      time.Time
	parents map[int][]int
}

// cachedTable returns the parent-to-children table, read afresh when the last
// read is older than tableTTL.
func cachedTable() (map[int][]int, error) {
	tableCache.Lock()
	defer tableCache.Unlock()
	if tableCache.parents != nil && time.Since(tableCache.at) < tableTTL {
		return tableCache.parents, nil
	}
	parents, err := table()
	if err != nil {
		return nil, err
	}
	tableCache.at, tableCache.parents = time.Now(), parents
	return parents, nil
}

// argvsOf reads each pid's arguments with read, leaving out the pids it cannot
// read (exited since the table was read, or zombies), in pid order.
func argvsOf(pids []int, read func(int) ([]string, error)) []Process {
	var out []Process
	for _, child := range pids {
		args, err := read(child)
		if err != nil {
			continue
		}
		out = append(out, Process{PID: child, Argv: args})
	}
	slices.SortFunc(out, func(a, b Process) int { return a.PID - b.PID })
	return out
}

// tableIn reads a /proc tree at root into a parent-to-children table: each
// numeric entry's stat names its parent, as field 4, the second after the last
// ')' (the command name in parentheses may itself hold ')' or spaces). An
// entry that vanishes or cannot be parsed while it is read is skipped.
func tableIn(root string) (map[int][]int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("procenv: %w", err)
	}
	parents := make(map[int][]int)
	for _, de := range entries {
		pid, err := strconv.Atoi(de.Name())
		if err != nil || pid <= 0 {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(root, de.Name(), "stat"))
		if err != nil {
			continue // gone, or not a process
		}
		if ppid, ok := statParent(stat); ok {
			parents[ppid] = append(parents[ppid], pid)
		}
	}
	return parents, nil
}

// argvIn reads the NUL-terminated arguments of pid from a /proc tree at root.
func argvIn(root string, pid int) ([]string, error) {
	cmdline, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, err
	}
	return splitNUL(cmdline), nil
}

// childrenIn reads the direct children of pid from a /proc tree at root,
// uncached, as Children does on linux.
func childrenIn(root string, pid int) ([]Process, error) {
	parents, err := tableIn(root)
	if err != nil {
		return nil, err
	}
	return argvsOf(parents[pid], func(c int) ([]string, error) { return argvIn(root, c) }), nil
}

// statParent reads the parent pid from the contents of /proc/<pid>/stat.
func statParent(stat []byte) (int, bool) {
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(string(stat[i+1:]))
	if len(f) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(f[1])
	return ppid, err == nil
}

// splitNUL splits NUL-terminated strings, as /proc/<pid>/cmdline holds them;
// a last string without its NUL is kept.
func splitNUL(data []byte) []string {
	data = bytes.TrimSuffix(data, []byte{0})
	if len(data) == 0 {
		return nil
	}
	return strings.Split(string(data), "\x00")
}
