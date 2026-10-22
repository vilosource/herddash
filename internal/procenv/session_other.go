//go:build !unix

package procenv

// sessionOpenFlags: no O_NONBLOCK or O_NOFOLLOW here; the Lstat check before
// opening and the Stat check after it still refuse anything but a regular
// file.
const sessionOpenFlags = 0
