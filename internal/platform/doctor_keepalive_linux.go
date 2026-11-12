//go:build linux
// +build linux

package platform

import (
	"context"
	"os"
)

// linuxDoctorKeepalive reports only whether the systemd unit is enabled. It
// never enables, disables, starts, or stops anything.
func linuxDoctorKeepalive(ctx context.Context, config string, system bool) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	service, err := NewLinuxService(exe, config, system)
	if err != nil {
		return "", err
	}
	enabled, err := service.Enabled(ctx)
	if err != nil {
		return "", err
	}
	scope := "systemd --user"
	if system {
		scope = "root systemd"
	}
	if enabled {
		return "已启用（" + scope + "）", nil
	}
	return "未启用（" + scope + "）", nil
}
