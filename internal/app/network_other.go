//go:build !linux
// +build !linux

package app

import (
	"context"
	"errors"
)

func ensureLinuxNetwork(context.Context, Config) error {
	return errors.New("Linux network selection unavailable on this platform")
}
