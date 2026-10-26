//go:build !darwin

package platform

import "errors"

// macOS CLI parsing and JSON tests are portable; mutating CLI operations are
// unavailable elsewhere and cannot accidentally authenticate from host tests.
func macAcquireLock(string) (func(), error) {
	return nil, errors.New("macOS network operations are unavailable on this platform")
}
