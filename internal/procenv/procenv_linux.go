package procenv

import (
	"fmt"
	"os"
	"strconv"
)

// lookup reads /proc/<pid>/environ.
func lookup(pid int, name string) (string, bool, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return "", false, fmt.Errorf("procenv: pid %d: %w", pid, err)
	}
	v, ok, err := parseEnviron(data, name)
	if err != nil {
		return "", false, fmt.Errorf("procenv: pid %d: %w", pid, err)
	}
	return v, ok, nil
}

// table reads the parent of every process in /proc.
func table() (map[int][]int, error) { return tableIn("/proc") }

// argv reads /proc/<pid>/cmdline.
func argv(pid int) ([]string, error) { return argvIn("/proc", pid) }
