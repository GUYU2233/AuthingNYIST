package app

import (
	"context"
	"errors"
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
