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

var linuxWirelessPresent = func() (bool, error) {
	entries, e := os.ReadDir("/sys/class/net")
	if e != nil {
		return false, errors.New("cannot inspect network interfaces")
	}
	wireless := false
	for _, d := range entries {
		if _, e := os.Stat(filepath.Join("/sys/class/net", d.Name(), "wireless")); e == nil {
			wireless = true
			break
		}
	}
	return wireless, nil
}

func detectLinuxNetwork(ctx context.Context) (string, error) {
	wireless, err := linuxWirelessPresent()
	if err != nil {
		return "", err
	}
	if !wireless {
		return "", nil
	}
	b, err := commandOutput(ctx, "nmcli", "-t", "--escape", "no", "-f", "ACTIVE,SSID", "device", "wifi", "list", "--rescan", "no")
	if err != nil {
		return "", errors.New("NetworkManager nmcli is required to inspect campus Wi-Fi; select the campus network manually")
	}
	return detectedSSIDs(string(b), true)
}

func ensureLinuxNetwork(ctx context.Context, cfg Config) error {
	wireless, e := linuxWirelessPresent()
	if e != nil {
		return e
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
