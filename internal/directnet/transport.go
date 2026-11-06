// Package directnet provides proxy-free HTTP and best-effort physical-interface
// binding. It never changes system routes or proxy settings. Source binding is
// not a guarantee against policy routing, TUN interception, or DNS interception.
package directnet

import (
	"context"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Interface describes a candidate physical link. Networks contain its assigned
// addresses (IP must be the host address, not just the subnet base).
type Interface struct {
	Name     string
	Networks []*net.IPNet
}

// Options permits injecting interface discovery for tests or explicit selection.
// A nil Interfaces uses active Ethernet/Wi-Fi-like links with hardware addresses.
type Options struct {
	Timeout    time.Duration
	Interfaces func() ([]Interface, error)
}

// NewTransport disables all environment proxies. Each new connection discovers
// links afresh, preferring a directly attached subnet. Existing pooled connections
// may outlive a network switch; callers may CloseIdleConnections on such a switch.
// If no candidate exists, ordinary direct dialing is used (no TUN guarantee).
func NewTransport(opts Options) *http.Transport {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	discover := opts.Interfaces
	if discover == nil {
		discover = interfaces
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		links, _ := discover()
		var last error
		for _, resolved := range ips {
			ip := resolved.IP
			if network == "tcp4" && ip.To4() == nil || network == "tcp6" && ip.To4() != nil {
				continue
			}
			target := net.JoinHostPort(resolved.String(), port)
			candidates := selectInterfaces(links, ip)
			if ip.IsLoopback() || len(candidates) == 0 {
				c, e := (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, target)
				if e == nil {
					return c, nil
				}
				last = e
				continue
			}
			for _, candidate := range candidates {
				d := &net.Dialer{Timeout: timeout, LocalAddr: &net.TCPAddr{IP: candidate.ip}, Control: bindDevice(candidate.name)}
				c, e := d.DialContext(ctx, network, target)
				if e == nil {
					return c, nil
				}
				last = e
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}
		}
		if last == nil {
			last = &net.DNSError{Err: "no usable address", Name: host}
		}
		return nil, last
	}
	return &http.Transport{Proxy: nil, DialContext: dial, ForceAttemptHTTP2: true, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, IdleConnTimeout: 30 * time.Second}
}

type candidate struct {
	name     string
	ip       net.IP
	attached bool
}

func selectInterfaces(links []Interface, destination net.IP) []candidate {
	var out []candidate
	for _, link := range links {
		for _, subnet := range link.Networks {
			if subnet == nil || !subnet.IP.IsGlobalUnicast() || subnet.IP.IsLinkLocalUnicast() || (subnet.IP.To4() != nil) != (destination.To4() != nil) {
				continue
			}
			out = append(out, candidate{link.Name, subnet.IP, subnet.Contains(destination)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].attached && !out[j].attached })
	return out
}
func interfaces() ([]Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Interface
	for _, link := range all {
		if link.Flags&net.FlagUp == 0 || link.Flags&net.FlagLoopback != 0 || link.Flags&net.FlagPointToPoint != 0 || len(link.HardwareAddr) == 0 {
			continue
		}
		name := strings.ToLower(link.Name)
		virtual := false
		for _, prefix := range []string{"tun", "tap", "utun", "wg", "tailscale", "docker", "veth", "virbr", "br-", "vmnet", "vbox"} {
			if strings.HasPrefix(name, prefix) {
				virtual = true
			}
		}
		if virtual {
			continue
		}
		addresses, e := link.Addrs()
		if e != nil {
			continue
		}
		item := Interface{Name: link.Name}
		for _, address := range addresses {
			ip, subnet, e := net.ParseCIDR(address.String())
			if e == nil {
				subnet.IP = ip
				item.Networks = append(item.Networks, subnet)
			}
		}
		out = append(out, item)
	}
	return out, nil
}
