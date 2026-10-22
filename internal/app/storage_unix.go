//go:build !windows
// +build !windows

package app

import (
	"errors"
	"os"
	"strings"
)

func protectPassword(s string) (string, error) { return s, nil }
func unprotectPassword(s string) (string, error) {
	if strings.HasPrefix(s, "dpapi:") {
		return "", errors.New("Windows-protected password cannot be loaded here; enter it again")
	}
	return s, nil
}
func secureDirectory(path string) error { return os.Chmod(path, 0700) }
func replaceFile(from, to string) error { return os.Rename(from, to) }
