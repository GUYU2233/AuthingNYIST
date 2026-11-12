package platform

import (
	"context"
	"os"
	"strings"
)

// probeResolver reports only resolver-config presence and the first nameserver.
// It intentionally performs no DNS queries: fake-DNS/TUN behavior cannot be
// diagnosed from a resolver file, so claiming connectivity from it would be
// misleading.
func probeResolver(ctx context.Context) (doctorDNS, error) {
	if err := ctx.Err(); err != nil {
		return doctorDNS{}, err
	}
	const path = "/etc/resolv.conf"
	dns := doctorDNS{Path: path}
	if _, err := os.Stat("/run/systemd/resolve"); err == nil {
		dns.SystemdResolved = true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return dns, nil
		}
		return dns, err
	}
	dns.Exists = true
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			dns.Nameserver = fields[1]
			break
		}
	}
	return dns, nil
}
