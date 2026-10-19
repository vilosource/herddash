package procenv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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
	if len(data) < 4 {
		return "", false, fmt.Errorf("%w: %d bytes, too short for argc", ErrMalformed, len(data))
	}
	argc := int(int32(binary.NativeEndian.Uint32(data)))
	if argc < 0 {
		return "", false, fmt.Errorf("%w: argc %d", ErrMalformed, argc)
	}
	rest := data[4:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return "", false, fmt.Errorf("%w: executable path not terminated", ErrMalformed)
	}
	rest = rest[end:]
	// Skip the path's terminator and the padding after it.
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	for i := range argc {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return "", false, fmt.Errorf("%w: %d of %d arguments", ErrMalformed, i, argc)
		}
		rest = rest[end+1:]
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
