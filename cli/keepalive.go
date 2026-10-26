package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

const libraryProbeURL = "http://122.207.209.6:8080/"

// The library's root responds with this redirect when reachable. A generic 200
// or an arbitrary redirect could instead be a captive portal or proxy error page.
const librarySearchPath = "/opac/search/simsearch"

type libraryProbe struct {
	endpoint string
	client   *http.Client
}

func newLibraryProbe(endpoint string, timeout time.Duration) *libraryProbe {
	transport := &http.Transport{
		// Do not use ProxyFromEnvironment: a working proxy is not evidence that
		// this machine has a working campus session. TUN is still traversed, so
		// require the actual application response rather than a TCP handshake.
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &libraryProbe{
		endpoint: endpoint,
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (p *libraryProbe) close() {
	p.client.CloseIdleConnections()
}

func (p *libraryProbe) check() error {
	if p.client.Timeout <= 0 {
		return fmt.Errorf("library probe requires a positive timeout")
	}
	response, err := p.client.Get(p.endpoint)
	if err != nil {
		return fmt.Errorf("library probe: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		return fmt.Errorf("library probe: unexpected HTTP status %d", response.StatusCode)
	}
	location, err := response.Location()
	if err != nil {
		return fmt.Errorf("library probe: invalid redirect: %w", err)
	}
	endpoint, err := url.Parse(p.endpoint)
	if err != nil {
		return fmt.Errorf("library probe: invalid endpoint: %w", err)
	}
	if location.Scheme != endpoint.Scheme || location.Host != endpoint.Host ||
		location.User != nil || location.Path != librarySearchPath ||
		location.RawQuery != "" || location.ForceQuery || location.Fragment != "" {
		return fmt.Errorf("library probe: unexpected redirect %q", location.String())
	}
	return nil
}
