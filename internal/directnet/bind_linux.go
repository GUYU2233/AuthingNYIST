//go:build linux

package directnet

import (
	"errors"
	"golang.org/x/sys/unix"
	"syscall"
)

// Always source-bind as well. Unprivileged kernels may deny SO_BINDTODEVICE;
// only permission/unsupported failures fall back to that weaker source binding.
func bindDevice(name string) func(string, string, syscall.RawConn) error {
	return func(_, _ string, raw syscall.RawConn) error {
		var bindErr error
		if err := raw.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, name)
		}); err != nil {
			return err
		}
		if errors.Is(bindErr, unix.EPERM) || errors.Is(bindErr, unix.EACCES) || errors.Is(bindErr, unix.ENOPROTOOPT) || errors.Is(bindErr, unix.EOPNOTSUPP) {
			return nil
		}
		return bindErr
	}
}
