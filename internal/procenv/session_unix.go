//go:build unix

package procenv

import "syscall"

// sessionOpenFlags are added to O_RDONLY when a session record is opened:
// never follow a symbolic link put in its place after the check, and never
// block opening a FIFO.
const sessionOpenFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW
