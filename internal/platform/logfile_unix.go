//go:build linux || darwin
// +build linux darwin

package platform

import (
	"os"
	"syscall"
)

// openAppendNoFollow refuses to open a symlink at the log path. O_NOFOLLOW is
// supported by Linux and Darwin.
func openAppendNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0600)
}

// withLogLock serializes rotation and opening across concurrent daemon starts.
// The lock file's inode is stable; the log file itself is renamed during
// rotation, so it must not be the lock target.
func withLogLock(path string, fn func() error) error {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn()
}
