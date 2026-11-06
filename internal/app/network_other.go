//go:build !linux
// +build !linux

package app

import (
	"context"
	"errors"
)

func detectLinuxNetwork(context.Context) (string, error) {
	return "", errors.New("Linux network detection unavailable on this platform")
}

func ensureLinuxNetwork(context.Context, Config) error {
	return errors.New("Linux network selection unavailable on this platform")
}
