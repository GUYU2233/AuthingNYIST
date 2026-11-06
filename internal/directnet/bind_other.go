//go:build !linux

package directnet

import "syscall"

// Other platforms use the dialer's source address only (best effort).
func bindDevice(string) func(string, string, syscall.RawConn) error { return nil }
