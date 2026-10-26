package app

import (
	"context"
	"errors"
	"strings"
)

// macHardwarePorts deliberately parses only networksetup's documented English
// hardware-port blocks. Unknown/ambiguous output is not evidence of a safe link.
func macHardwarePorts(output string) map[string]string {
	ports := make(map[string]string)
	port := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Hardware Port: ") {
			port = strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port: "))
		}
		if strings.HasPrefix(line, "Device: ") {
			device := strings.TrimSpace(strings.TrimPrefix(line, "Device: "))
			if port != "" && validMacDevice(device) {
				ports[device] = port
			}
			port = ""
		}
	}
	return ports
}
func validMacDevice(s string) bool {
	if !strings.HasPrefix(s, "en") || len(s) < 3 {
		return false
	}
	for _, c := range s[2:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func macRouteInterface(output string) string {
	for _, line := range strings.Split(output, "\n") {
		p := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(p) == 2 && p[0] == "interface" && validMacDevice(strings.TrimSpace(p[1])) {
			return strings.TrimSpace(p[1])
		}
	}
	return ""
}
func macCurrentSSID(output string) string {
	const prefix = "Current Wi-Fi Network: "
	s := strings.TrimSpace(output)
	if strings.HasPrefix(s, prefix) && !strings.ContainsAny(s, "\r\n") {
		ssid := strings.TrimPrefix(s, prefix)
		if ssid != "" && !strings.Contains(strings.ToLower(ssid), "redacted") {
			return ssid
		}
	}
	return ""
}
func macSavedSSID(output, ssid string) bool {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "Preferred networks on ") {
		return false
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == ssid {
			return true
		}
	}
	return false
}

// Kept platform-neutral so command transcripts can be unit-tested on Windows.
// No Wi-Fi password is ever accepted or placed in a process argument. A target
// must already be a preferred network and macOS must verify the resulting SSID.
func ensureMacNetwork(ctx context.Context, cfg Config) error {
	b, err := commandOutput(ctx, "/usr/sbin/networksetup", "-listallhardwareports")
	if err != nil {
		return errors.New("cannot inspect macOS network hardware")
	}
	ports := macHardwarePorts(string(b))
	if len(ports) == 0 {
		return errors.New("cannot identify macOS network hardware")
	}
	route, routeErr := commandOutput(ctx, "/sbin/route", "-n", "get", "default")
	if routeErr == nil {
		port := ports[macRouteInterface(string(route))]
		if port == "Ethernet" || strings.HasSuffix(port, " Ethernet") || strings.HasSuffix(port, " LAN") {
			return nil // Use an existing Ethernet default route without switching Wi-Fi.
		}
	}
	wifi := ""
	for device, port := range ports {
		if port == "Wi-Fi" || port == "AirPort" {
			if wifi != "" {
				return errors.New("multiple Wi-Fi adapters; select the campus network manually")
			}
			wifi = device
		}
	}
	if wifi == "" {
		return errors.New("no verified Ethernet route or Wi-Fi adapter; connect a network manually")
	}
	b, err = commandOutput(ctx, "/usr/sbin/networksetup", "-getairportnetwork", wifi)
	if err != nil {
		return errors.New("cannot inspect current Wi-Fi; select the campus network manually")
	}
	if macCurrentSSID(string(b)) == cfg.Network {
		return nil
	}
	// An unrecognized inspection (including privacy-redacted SSIDs on recent
	// macOS releases) must not trigger a blind network switch.
	current := macCurrentSSID(string(b))
	if current == "" && strings.TrimSpace(string(b)) != "You are not associated with an AirPort network." && strings.TrimSpace(string(b)) != "You are not associated with a Wi-Fi network." {
		return errors.New("cannot verify current Wi-Fi (macOS privacy permission may be required); select it manually")
	}
	b, err = commandOutput(ctx, "/usr/sbin/networksetup", "-listpreferredwirelessnetworks", wifi)
	if err != nil || !macSavedSSID(string(b), cfg.Network) {
		return errors.New("campus Wi-Fi is not a saved preferred network; join it in macOS settings first")
	}
	if _, err = commandOutput(ctx, "/usr/sbin/networksetup", "-setairportnetwork", wifi, cfg.Network); err != nil {
		return errors.New("cannot select saved campus Wi-Fi; join it in macOS settings manually")
	}
	for {
		b, err = commandOutput(ctx, "/usr/sbin/networksetup", "-getairportnetwork", wifi)
		if err != nil {
			return errors.New("cannot verify selected campus Wi-Fi")
		}
		if macCurrentSSID(string(b)) == cfg.Network {
			return nil
		}
		if err = waitNetwork(ctx); err != nil {
			return errors.New("campus Wi-Fi could not be verified; check macOS Wi-Fi settings and privacy permissions")
		}
	}
}
