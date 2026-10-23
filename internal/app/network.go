package app

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	configureCommand(cmd)
	return cmd.Output()
}

// EnsureNetwork selects an existing OS Wi-Fi profile; it never creates profiles
// or accepts Wi-Fi passwords. Check and Disconnect never invoke this function.
// Linux with no wireless device (including machines without nmcli) is allowed
// to use its existing wired route. Wireless hosts require NetworkManager.
func EnsureNetwork(ctx context.Context, cfg Config) error {
	if e := cfg.Validate(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "windows":
		b, e := commandOutput(ctx, "netsh", "wlan", "show", "interfaces")
		if e != nil {
			return errors.New("cannot inspect Wi-Fi; configure a saved campus Wi-Fi profile manually")
		}
		if hasSSID(string(b), cfg.Network) {
			return nil
		}
		if _, e = commandOutput(ctx, "netsh", "wlan", "connect", "name="+cfg.Network, "ssid="+cfg.Network); e != nil {
			return errors.New("cannot select campus Wi-Fi; create a saved profile with the same SSID first")
		}
		for {
			b, e = commandOutput(ctx, "netsh", "wlan", "show", "interfaces")
			if e == nil && hasSSID(string(b), cfg.Network) {
				return nil
			}
			if e = waitNetwork(ctx); e != nil {
				return errors.New("campus Wi-Fi did not become connected; check the saved profile")
			}
		}
	case "linux":
		return ensureLinuxNetwork(ctx, cfg)
	default:
		return errors.New("automatic network selection supports Windows and Linux only")
	}
}

// localNetworkStatus is read-only. Interface addresses do not prove Internet
// access; they only distinguish a disconnected link from an unreachable portal.
var localNetworkStatus = inspectLocalNetwork

func inspectLocalNetwork(ctx context.Context, cfg Config) (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", errors.New("cannot inspect local network interfaces")
	}
	connected := false
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() {
				connected = true
			}
		}
	}
	if !connected {
		return "No network connection", nil
	}
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		b, err := commandOutput(ctx, "netsh", "wlan", "show", "interfaces")
		// A wired connection is valid too; don't assume disconnected solely
		// because WLAN is unavailable or has no SSID.
		if err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				p := strings.SplitN(line, ":", 2)
				if len(p) == 2 && strings.EqualFold(strings.TrimSpace(p[0]), "SSID") {
					ssid := strings.TrimSpace(p[1])
					if ssid != "" && ssid != cfg.Network {
						return "Not on selected campus Wi-Fi", nil
					}
				}
			}
		}
	}
	return "", nil
}

func hasSSID(output, ssid string) bool {
	for _, line := range strings.Split(output, "\n") {
		p := strings.SplitN(line, ":", 2)
		if len(p) == 2 && strings.EqualFold(strings.TrimSpace(p[0]), "SSID") && strings.TrimSpace(p[1]) == ssid {
			return true
		}
	}
	return false
}
func waitNetwork(ctx context.Context) error {
	t := time.NewTimer(500 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
