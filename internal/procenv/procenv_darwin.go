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
