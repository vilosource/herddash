//go:build !linux && !darwin

package procenv

import (
	"fmt"
	"runtime"
)

func lookup(pid int, name string) (string, bool, error) {
	return "", false, fmt.Errorf("procenv: pid %d on %s: %w", pid, runtime.GOOS, ErrUnsupported)
}
