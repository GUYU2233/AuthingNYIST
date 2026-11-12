//go:build !linux

package tray

import "context"

// Run reports ErrUnsupported; the tray is only implemented for Linux.
func Run(ctx context.Context, state func() State, actions Actions, logger func(string, ...any)) error {
	return ErrUnsupported
}

// Refresh is a no-op on platforms without a tray.
func Refresh() {}

// Available reports ErrUnsupported; callers can use it to fall back to another
// UI without special-casing GOOS.
func Available() error {
	return ErrUnsupported
}

// Notify reports ErrUnsupported on platforms without the freedesktop
// notification service.
func Notify(title, body string, urgent bool) error {
	return ErrUnsupported
}
