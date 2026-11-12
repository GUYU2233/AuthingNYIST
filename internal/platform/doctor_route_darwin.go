//go:build darwin
// +build darwin

package platform

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"time"
)

func probeDefaultRoute(ctx context.Context) (doctorRoute, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/sbin/route", "-n", "get", "default").Output()
	if err != nil {
		if ctx.Err() != nil {
			return doctorRoute{}, ctx.Err()
		}
		return doctorRoute{}, errors.New("未找到默认路由")
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "interface" {
			continue
		}
		name := strings.TrimSpace(value)
		if name == "" {
			break
		}
		route := doctorRoute{Interface: name}
		if iface, err := net.InterfaceByName(name); err == nil && len(iface.HardwareAddr) > 0 {
			route.HardwareAddr = iface.HardwareAddr.String()
		}
		return route, nil
	}
	return doctorRoute{}, errors.New("未找到默认路由")
}
