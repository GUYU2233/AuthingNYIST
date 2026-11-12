//go:build !linux
// +build !linux

package app

import "errors"

var errKeyringUnsupported = errors.New("system keyring is not supported on this platform")

func keyringStore(string, string) error { return errKeyringUnsupported }

func keyringLookup(string) (string, error) { return "", errKeyringUnsupported }
