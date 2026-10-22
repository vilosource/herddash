//go:build !linux && !darwin

package procenv

import (
	"fmt"
	"runtime"
)

func lookup(pid int, name string) (string, bool, error) {
	return "", false, fmt.Errorf("procenv: pid %d on %s: %w", pid, runtime.GOOS, ErrUnsupported)
}

func table() (map[int][]int, error) {
	return nil, fmt.Errorf("process table on %s: %w", runtime.GOOS, ErrUnsupported)
}

func argv(pid int) ([]string, error) {
	return nil, fmt.Errorf("procenv: arguments of pid %d on %s: %w", pid, runtime.GOOS, ErrUnsupported)
}
