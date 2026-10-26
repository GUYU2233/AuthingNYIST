package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestLibraryProbeResponse(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		location string
		online   bool
	}{
		{"library relative redirect", 302, librarySearchPath, true},
		{"library absolute redirect", 302, "SELF" + librarySearchPath, true},
		{"portal redirect", 302, "/srun_portal_pc?ac_id=1", false},
		{"external redirect with library path", 302, "http://portal.invalid" + librarySearchPath, false},
		{"protocol relative external redirect", 302, "//portal.invalid" + librarySearchPath, false},
		{"HTTPS redirect", 302, "https://portal.invalid" + librarySearchPath, false},
		{"library path with query", 302, librarySearchPath + "?login=1", false},
		{"library path with fragment", 302, librarySearchPath + "#login", false},
		{"missing location", 302, "", false},
		{"malformed location", 302, "://bad", false},
		{"generic success", 200, "", false},
		{"proxy bad gateway", 502, "", false},
		{"temporary redirect", 307, librarySearchPath, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.RequestURI() != "/" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
				}
				location := test.location
				if len(location) >= 4 && location[:4] == "SELF" {
					location = "http://" + r.Host + location[4:]
				}
				if location != "" {
					w.Header().Set("Location", location)
				}
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			probe := newLibraryProbe(server.URL, time.Second)
			defer probe.close()
			if err := probe.check(); (err == nil) != test.online {
				t.Fatalf("check() = %v, online want %v", err, test.online)
			}
			if requests.Load() != 1 {
				t.Fatalf("redirect was followed: requests = %d", requests.Load())
			}
		})
	}
}

func TestLibraryProbeDoesNotUseEnvironmentProxy(t *testing.T) {
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyRequests.Add(1)
		w.Header().Set("Location", librarySearchPath)
		w.WriteHeader(http.StatusFound)
	}))
	defer proxy.Close()
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	probe := newLibraryProbe(server.URL, time.Second)
	defer probe.close()
	// Go bypasses environment proxies for loopback anyway; also assert the
	// transport policy so the test covers the production non-loopback address.
	if probe.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("probe transport must disable proxies explicitly")
	}
	if err := probe.check(); err == nil {
		t.Fatal("origin's bad gateway must not be hidden by proxy success")
	}
	if proxyRequests.Load() != 0 {
		t.Fatal("probe contacted environment proxy")
	}
}

func TestLibraryProbeEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer server.Close()
	probe := newLibraryProbe(server.URL, time.Second)
	defer probe.close()
	if err := probe.check(); !errors.Is(err, io.EOF) {
		t.Fatalf("check() = %v, want EOF", err)
	}
}

func TestLibraryProbeTimeout(t *testing.T) {
	for _, test := range []struct {
		name    string
		partial bool
	}{
		{"no headers after TCP accepted", false},
		{"partial headers", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.partial {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					io.WriteString(conn, "HTTP/1.1 302 Found\r\nLocation: ")
				}
				<-release
			}))
			defer server.Close()
			defer close(release)
			probe := newLibraryProbe(server.URL, 50*time.Millisecond)
			defer probe.close()
			start := time.Now()
			err := probe.check()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("check() = %v, want deadline exceeded", err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("probe exceeded its bounded timeout")
			}
		})
	}
}

func TestLibraryProbeRejectsUnboundedTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		probe := newLibraryProbe("http://unused.invalid/", timeout)
		if err := probe.check(); err == nil {
			t.Fatal("unbounded timeout accepted")
		}
		probe.close()
	}
}

func TestAuthenticateAndKeepAlive(t *testing.T) {
	original := settings
	defer func() { settings = original }()
	authErr := errors.New("login failed")
	for _, test := range []struct {
		name           string
		keepOn, logout bool
		ip             string
		authErr        error
		loops          int
	}{
		{"successful or already online", true, false, "", nil, 1},
		{"keepalive disabled", false, false, "", nil, 0},
		{"logout never starts loop", true, true, "", nil, 0},
		{"other IP never starts loop", true, false, "192.0.2.2", nil, 0},
		{"failed authentication", true, false, "", authErr, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings = Settings{KeepOn: test.keepOn, Ip: test.ip}
			calls, loops := 0, 0
			err := authenticateAndKeepAlive(test.logout, func(logout bool) error {
				calls++
				if logout != test.logout {
					t.Fatal("logout argument changed")
				}
				return test.authErr
			}, func() error { loops++; return nil })
			if !errors.Is(err, test.authErr) || calls != 1 || loops != test.loops {
				t.Fatalf("error=%v calls=%d loops=%d; want error=%v calls=1 loops=%d", err, calls, loops, test.authErr, test.loops)
			}
		})
	}
}

func TestRunKeepAliveRetriesWithoutNesting(t *testing.T) {
	for _, failure := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		checks, authentications := 0, 0
		err := runKeepAlive(ctx, time.Millisecond, func() error {
			checks++
			if checks == 4 {
				cancel()
				return nil
			}
			if failure {
				return errors.New("offline")
			}
			return nil
		}, func() error {
			authentications++
			return errors.New("retryable auth failure")
		})
		cancel()
		want := 0
		if failure {
			want = 3
		}
		if !errors.Is(err, context.Canceled) || checks != 4 || authentications != want {
			t.Fatalf("error=%v checks=%d auth=%d, want canceled/4/%d", err, checks, authentications, want)
		}
	}
}
