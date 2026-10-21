package procenv

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// lookup reads kern.procargs2 for pid through sysctl.
func lookup(pid int, name string) (string, bool, error) {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", false, fmt.Errorf("procenv: pid %d: kern.procargs2: %w", pid, err)
	}
	v, ok, err := parseProcargs2(data, name)
	if err != nil {
		return "", false, fmt.Errorf("procenv: pid %d: %w", pid, err)
	}
	return v, ok, nil
}

// table reads the parent of every process through kern.proc.all.
func table() (map[int][]int, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("kern.proc.all: %w", err)
	}
	parents := make(map[int][]int)
	for _, kp := range procs {
		ppid := int(kp.Eproc.Ppid)
		parents[ppid] = append(parents[ppid], int(kp.Proc.P_pid))
	}
	return parents, nil
}

// argv reads a process's arguments through kern.procargs2. macOS returns the
// arguments of an Apple platform binary such as /bin/zsh even where it
// withholds its environment.
func argv(pid int) ([]string, error) {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	args, _, err := splitProcargs2(data)
	return args, err
}
