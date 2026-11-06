package directnet

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnvironmentProxyIgnored(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("environment proxy used"); w.WriteHeader(502) }))
	defer proxy.Close()
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "direct") }))
	defer server.Close()
	tr := NewTransport(Options{Interfaces: func() ([]Interface, error) { return nil, nil }})
	defer tr.CloseIdleConnections()
	if tr.Proxy != nil {
		t.Fatal("proxy function must be nil")
	}
	client := &http.Client{Transport: tr, Timeout: time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "direct" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestSelectInterfaces(t *testing.T) {
	subnet := func(s string) *net.IPNet {
		ip, n, e := net.ParseCIDR(s)
		if e != nil {
			t.Fatal(e)
		}
		n.IP = ip
		return n
	}
	links := []Interface{{"wifi", []*net.IPNet{subnet("192.168.1.10/24"), subnet("2001:db8::1/64")}}, {"ethernet", []*net.IPNet{subnet("10.0.0.2/24")}}}
	candidates := selectInterfaces(links, net.ParseIP("10.0.0.8"))
	if len(candidates) != 2 || candidates[0].name != "ethernet" || !candidates[0].ip.Equal(net.ParseIP("10.0.0.2")) {
		t.Fatalf("bad selection: %+v", candidates)
	}
	candidates = selectInterfaces(links, net.ParseIP("2001:db8::8"))
	if len(candidates) != 1 || candidates[0].name != "wifi" {
		t.Fatalf("bad IPv6 selection: %+v", candidates)
	}
}
