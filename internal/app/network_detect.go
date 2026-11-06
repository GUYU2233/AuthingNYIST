package app

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"
)

// DetectNetwork reads the connected Wi-Fi SSID without scanning or switching
// networks. It returns nyist-tec or nyist-stu for campus Wi-Fi, an empty string
// for no associated Wi-Fi (including an existing wired route), and an actionable
// error for unknown Wi-Fi or an inspection that cannot be verified. It does not
// establish Internet connectivity or inspect the portal session.
func DetectNetwork(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return detectNetwork(ctx, runtime.GOOS)
}

func campusSSID(ssid string) (string, error) {
	switch ssid {
	case "", "nyist-tec", "nyist-stu":
		return ssid, nil
	default:
		return "", errors.New("not connected to campus Wi-Fi; join nyist-tec or nyist-stu manually, or select a campus network explicitly")
	}
}

func detectNetwork(ctx context.Context, platform string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch platform {
	case "linux":
		return detectLinuxNetwork(ctx)
	case "windows":
		b, err := commandOutput(ctx, "netsh", "wlan", "show", "interfaces")
		if err != nil {
			return "", errors.New("cannot inspect Wi-Fi; select the campus network manually")
		}
		return detectedSSIDs(string(b), false)
	case "darwin":
		return detectMacNetwork(ctx)
	default:
		return "", errors.New("network detection supports Windows, Linux and macOS only")
	}
}

// Multiple associated interfaces are accepted only when their SSIDs agree.
func detectedSSIDs(output string, nmcli bool) (string, error) {
	current := ""
	for _, line := range strings.Split(output, "\n") {
		p := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(p) != 2 {
			continue
		}
		key := strings.TrimSpace(p[0])
		if (nmcli && key != "yes") || (!nmcli && !strings.EqualFold(key, "SSID")) {
			continue
		}
		ssid := strings.TrimSpace(p[1])
		if ssid == "" {
			continue
		}
		if current != "" && current != ssid {
			return "", errors.New("multiple connected Wi-Fi networks; select the campus network manually")
		}
		current = ssid
	}
	return campusSSID(current)
}

func detectMacNetwork(ctx context.Context) (string, error) {
	b, err := commandOutput(ctx, "/usr/sbin/networksetup", "-listallhardwareports")
	if err != nil {
		return "", errors.New("cannot inspect macOS network hardware")
	}
	ports := macHardwarePorts(string(b))
	if len(ports) == 0 {
		return "", errors.New("cannot identify macOS network hardware")
	}
	route, err := commandOutput(ctx, "/sbin/route", "-n", "get", "default")
	if err == nil {
		port := ports[macRouteInterface(string(route))]
		if port == "Ethernet" || strings.HasSuffix(port, " Ethernet") || strings.HasSuffix(port, " LAN") {
			return "", nil
		}
	}
	wifi := ""
	for device, port := range ports {
		if port == "Wi-Fi" || port == "AirPort" {
			if wifi != "" {
				return "", errors.New("multiple Wi-Fi adapters; select the campus network manually")
			}
			wifi = device
		}
	}
	if wifi == "" {
		return "", errors.New("no verified Ethernet route or Wi-Fi adapter; connect a network manually")
	}
	b, err = commandOutput(ctx, "/usr/sbin/networksetup", "-getairportnetwork", wifi)
	if err != nil {
		return "", errors.New("cannot inspect current Wi-Fi; select the campus network manually")
	}
	current := macCurrentSSID(string(b))
	if current == "" && strings.TrimSpace(string(b)) != "You are not associated with an AirPort network." && strings.TrimSpace(string(b)) != "You are not associated with a Wi-Fi network." {
		return "", errors.New("cannot verify current Wi-Fi (macOS privacy permission may be required); select it manually")
	}
	return campusSSID(current)
}
