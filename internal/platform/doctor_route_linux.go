//go:build linux
// +build linux

package platform

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
)

// probeDefaultRoute reads the kernel routing table directly. A TUN adapter is
// often the default route and has no hardware address; the doctor report says
// so instead of treating the route as proof of physical connectivity.
func probeDefaultRoute(ctx context.Context) (doctorRoute, error) {
	if err := ctx.Err(); err != nil {
		return doctorRoute{}, err
	}
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return doctorRoute{}, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&0x1 == 0 { // RTF_UP
			continue
		}
		route := doctorRoute{Interface: fields[0]}
		if iface, err := net.InterfaceByName(fields[0]); err == nil && len(iface.HardwareAddr) > 0 {
			route.HardwareAddr = iface.HardwareAddr.String()
		}
		return route, nil
	}
	return doctorRoute{}, errors.New("未找到默认路由")
}
