package config

import (
	"fmt"
	"os"
	"syscall"
)

// EnsureDataDir creates the data directory, and any missing parents, with mode
// 0700 (FR-17, AD-9). An existing directory is accepted only if it is a
// directory owned by this process's user that neither group nor others can
// access; it is never chmodded or chowned, because the operator may have
// pointed data_dir somewhere deliberately.
func EnsureDataDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return &Error{Key: "data_dir", Msg: "cannot create the data directory", Err: err}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return &Error{Key: "data_dir", Msg: "cannot stat the data directory", Err: err}
	}
	if !fi.IsDir() {
		return &Error{Key: "data_dir", Msg: fmt.Sprintf("%q is not a directory", path)}
	}
	if uid, ok := ownerUID(fi); ok && uid != os.Getuid() {
		return &Error{Key: "data_dir", Msg: fmt.Sprintf(
			"%q is owned by uid %d, not by this user (uid %d)", path, uid, os.Getuid())}
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return &Error{Key: "data_dir", Msg: fmt.Sprintf(
			"%q has mode %#o; it must be accessible only by its owner (chmod 700)", path, perm)}
	}
	return nil
}

// ownerUID returns the file's owner uid, when the platform reports one.
func ownerUID(fi os.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
