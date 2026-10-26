//go:build windows

package bootservice

import (
	"errors"
	"golang.org/x/sys/windows"
)

// Helpers serialize transactions; the object grants no rights to GUI users.
func acquireManagement() (func(), error) {
	p, _ := windows.UTF16PtrFromString(`Global\AuthingNYIST.Management.v1`)
	sa, e := security("D:P(A;;GA;;;SY)(A;;GA;;;BA)")
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateEvent(sa, 0, 0, p)
	if errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return nil, errors.New("another service management operation is in progress")
	}
	if e != nil {
		return nil, e
	}
	return func() { windows.CloseHandle(h) }, nil
}
