//go:build !linux && !darwin
// +build !linux,!darwin

package platform

import "os"

// O_NOFOLLOW is not available portably on this platform. Windows has no POSIX
// symlink semantics for this log; the CLI daemon path is Linux-only.
func openAppendNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}

func withLogLock(_ string, fn func() error) error { return fn() }
