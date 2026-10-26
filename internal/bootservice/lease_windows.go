//go:build windows

package bootservice

import (
	"errors"
	"golang.org/x/sys/windows"
	"sync"
)

// AcquireAuthentication prevents cooperating GUI processes and the service from
// authenticating concurrently, across Windows sessions. The event is existence-
// based (not thread-affine like a Win32 mutex); closing the last handle releases
// it. Other users can deny availability, but cannot acquire service IPC rights.
func AcquireAuthentication() (func(), error) {
	return acquireAuthenticationNamed(`Global\AuthingNYIST.AuthenticationLease.v1`)
}

func acquireAuthenticationNamed(eventName string) (func(), error) {
	name, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		return nil, err
	}
	sa, e := security("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x00100002;;;AU)")
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateEvent(sa, 0, 0, name)
	if errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return nil, errors.New("authentication is owned by another GUI or the boot service")
	}
	if e != nil {
		return nil, e
	}
	var once sync.Once
	return func() { once.Do(func() { windows.CloseHandle(h) }) }, nil
}
