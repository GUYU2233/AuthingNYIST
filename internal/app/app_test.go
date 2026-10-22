package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	c := DefaultConfig()
	c.Username = "test-user"
	c.Password = "test-password-not-a-real-secret"
	return c
}
func TestConfigDefaultsValidation(t *testing.T) {
	c, e := Load(filepath.Join(t.TempDir(), "missing"))
	if e != nil || c != DefaultConfig() {
		t.Fatalf("missing config defaults: %v", e)
	}
	for _, alter := range []func(*Config){func(c *Config) { c.Network = "evil" }, func(c *Config) { c.Host = "https://name:secret@example.test" }, func(c *Config) { c.Host = "https://example.test/path" }, func(c *Config) { c.Host = "ftp://example.test" }, func(c *Config) { c.AcID = "0" }, func(c *Config) { c.Username = "a\nb" }, func(c *Config) { c.Username = "a@" }} {
		bad := testConfig()
		alter(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
func TestConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings", "config.json")
	c := testConfig()
	c.AutoStart = true
	c.StartMinimized = true
	c.Background = true
	c.AutoConnect = true
	if e := Save(p, c); e != nil {
		t.Fatal(e)
	}
	got, e := Load(p)
	if e != nil || got != c {
		t.Fatalf("round trip failed: %v", e)
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if runtime.GOOS == "windows" && strings.Contains(string(b), c.Password) {
		t.Fatal("plaintext Windows password")
	}
	if runtime.GOOS != "windows" {
		f, _ := os.Stat(p)
		d, _ := os.Stat(filepath.Dir(p))
		if f.Mode().Perm() != 0600 || d.Mode().Perm() != 0700 {
			t.Fatal("insecure permissions")
		}
	}
	c.Network = "invalid"
	if Save(p, c) == nil {
		t.Fatal("invalid save accepted")
	}
	got, e = Load(p)
	if e != nil || got.Network != "nyist-tec" {
		t.Fatal("failed save corrupted previous config")
	}
}
func TestAccountNormalization(t *testing.T) {
	for _, x := range []struct {
		want, got, domain string
		ok                bool
	}{{"alice", "alice", "nyisttec", true}, {"alice@nyisttec", "alice", "nyisttec", true}, {"alice@nyisttec", "alice@nyisttec", "", true}, {"alice", "bob", "nyisttec", false}, {"alice@nyisttec", "alice", "other", false}, {"", "alice", "nyisttec", false}, {"Alice", "alice", "nyisttec", false}} {
		if sameAccount(x.want, x.got, x.domain) != x.ok {
			t.Fatalf("account normalization wrong for %q/%q", x.want, x.got)
		}
	}
}
func mockPortal(t *testing.T, handler http.HandlerFunc) (Config, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := testConfig()
	c.Host = srv.URL
	return c, srv.Close
}
func TestCheckOnlyReadsAndMatchesAccount(t *testing.T) {
	c, close := mockPortal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cgi-bin/rad_user_info" {
			t.Errorf("unexpected mutation endpoint %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"error": "ok", "user_name": "different-user", "domain": "nyisttec", "online_ip": "192.0.2.1"})
	})
	defer close()
	s, e := Check(context.Background(), c)
	if e != nil || s.Online || s.Username != "different-user" || s.Message == "" {
		t.Fatalf("wrong-account status: %+v %v", s, e)
	}
	if _, e = Connect(context.Background(), c); !errors.Is(e, ErrOtherAccount) {
		t.Fatal("other account was not protected")
	}
	if e = Disconnect(context.Background(), c); !errors.Is(e, ErrOtherAccount) {
		t.Fatal("other account disconnect not rejected")
	}
}
func TestCheckOfflineAndJSONP(t *testing.T) {
	c, close := mockPortal(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("authing_callback({\"error\":\"not_online_error\"});"))
	})
	defer close()
	s, e := Check(context.Background(), c)
	if e != nil || s.Online || s.Message != "Disconnected" {
		t.Fatalf("offline status: %+v %v", s, e)
	}
}
func TestOnlineConnectAndCancelledRunDoNotMutate(t *testing.T) {
	originalCommand := commandOutput
	defer func() { commandOutput = originalCommand }()
	commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "netsh" {
			return []byte("SSID : nyist-tec\n"), nil
		}
		return []byte("yes:nyist-tec\n"), nil
	}
	calls := 0
	c, close := mockPortal(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/cgi-bin/rad_user_info" {
			t.Error("mutation attempted")
		}
		json.NewEncoder(w).Encode(map[string]string{"error": "ok", "user_name": "test-user", "domain": "nyisttec"})
	})
	defer close()
	s, e := Connect(context.Background(), c)
	if e != nil || !s.Online || calls != 2 {
		t.Fatalf("online connect: %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reports := 0
	e = Run(ctx, c, func(s Status) { reports++; cancel() })
	if !errors.Is(e, context.Canceled) || reports != 1 {
		t.Fatalf("run cancellation: %v", e)
	}
}
func TestAuthenticationRejectIsTerminalAndDoesNotExposeSecrets(t *testing.T) {
	mutations := 0
	c, close := mockPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/get_challenge":
			json.NewEncoder(w).Encode(map[string]string{"res": "ok", "challenge": "0123456789abcdef0123456789abcdef", "client_ip": "192.0.2.2"})
		case "/cgi-bin/srun_portal":
			mutations++
			if r.URL.Query().Get("action") != "login" {
				t.Error("wrong action")
			}
			json.NewEncoder(w).Encode(map[string]string{"error": "password-test-password-not-a-real-secret", "ecode": "E2553"})
		default:
			t.Error("unexpected endpoint")
		}
	})
	defer close()
	e := authenticate(context.Background(), c, false)
	if !errors.Is(e, ErrCredentials) || strings.Contains(e.Error(), c.Password) || mutations != 1 {
		t.Fatal("unsafe rejection handling")
	}
}
func TestContextCancellationAndSanitizedTransportErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := testConfig()
	_, e := Check(ctx, c)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel: %v", e)
	}
	c.Host = "http://127.0.0.1:1"
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, e = portalGet(ctx, c, "/cgi-bin/srun_portal", map[string][]string{"password": {c.Password}})
	if e == nil || strings.Contains(e.Error(), c.Password) || strings.Contains(e.Error(), "password=") {
		t.Fatal("transport error leaked request")
	}
}
func TestSSIDParsing(t *testing.T) {
	if !hasSSID("    SSID : nyist-tec\n BSSID : 00:00:00:00:00:00", "nyist-tec") || hasSSID("BSSID : nyist-tec", "nyist-tec") || hasSSID("SSID : nyist-tec-other", "nyist-tec") {
		t.Fatal("SSID parsing")
	}
}
