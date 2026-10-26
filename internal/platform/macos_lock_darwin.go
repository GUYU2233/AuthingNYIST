//go:build darwin

package platform

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// Advisory configuration-scoped lock shared by GUI-owned and manual CLI
// processes. Never unlink the file: doing so would allow locking a new inode.
func macAcquireLock(config string) (func(), error) {
	dir := filepath.Dir(config)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, errors.New("cannot create operation lock directory")
	}
	// Resolve symlinked parent directories so equivalent config paths share a lock.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, errors.New("cannot resolve operation lock directory")
	}
	fd, err := unix.Open(filepath.Join(dir, filepath.Base(config)+".operation.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("cannot open operation lock")
	}
	if err = unix.Fchmod(fd, 0600); err != nil {
		unix.Close(fd)
		return nil, errors.New("cannot protect operation lock")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, errors.New("another connect/disconnect/daemon operation owns this configuration; stop it first")
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}
