//go:build linux
// +build linux

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func ensureLinuxNetwork(ctx context.Context, cfg Config) error {
	entries, e := os.ReadDir("/sys/class/net")
	if e != nil {
		return errors.New("cannot inspect network interfaces")
	}
	wireless := false
	for _, d := range entries {
		if _, e := os.Stat(filepath.Join("/sys/class/net", d.Name(), "wireless")); e == nil {
			wireless = true
			break
		}
	}
	if !wireless {
		return nil
	}
	b, e := commandOutput(ctx, "nmcli", "-t", "--escape", "no", "-f", "ACTIVE,SSID", "device", "wifi", "list", "--rescan", "no")
	if e != nil {
		return errors.New("NetworkManager nmcli is required to select campus Wi-Fi")
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == "yes:"+cfg.Network {
			return nil
		}
	}
	if _, e = commandOutput(ctx, "nmcli", "--wait", "15", "connection", "up", "id", cfg.Network); e != nil {
		return errors.New("cannot select campus Wi-Fi; create a NetworkManager profile named after the SSID first")
	}
	b, e = commandOutput(ctx, "nmcli", "-t", "--escape", "no", "-f", "ACTIVE,SSID", "device", "wifi", "list", "--rescan", "no")
	if e != nil {
		return errors.New("cannot verify campus Wi-Fi")
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == "yes:"+cfg.Network {
			return nil
		}
	}
	return errors.New("selected Wi-Fi profile does not match campus SSID")
}
