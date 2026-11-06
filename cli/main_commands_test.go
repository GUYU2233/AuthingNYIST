package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	campus "github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/update"
	"github.com/Palvef/AuthingNYIST/libauth"
)

func setupCommands(t *testing.T) *bytes.Buffer {
	t.Helper()
	oldSettings, oldDetect, oldInspect, oldUpdate := settings, detectNetwork, inspectStatus, checkUpdate
	oldOnline, oldLogin := isOnline, loginLogout
	t.Cleanup(func() {
		settings = oldSettings
		detectNetwork = oldDetect
		inspectStatus = oldInspect
		checkUpdate = oldUpdate
		isOnline = oldOnline
		loginLogout = oldLogin
	})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings = Settings{}
	return new(bytes.Buffer)
}

func TestHelpAndUpdateDoNotDetectNetwork(t *testing.T) {
	out := setupCommands(t)
	detectNetwork = func(context.Context) (string, error) { t.Fatal("unexpected network detection"); return "", nil }
	checkUpdate = func(context.Context, string) (*update.Result, error) {
		return &update.Result{CurrentVersion: "dev", Latest: update.Release{TagName: "v3.0.0", HTMLURL: "https://example.com/release"}}, nil
	}
	for _, args := range [][]string{{"auth-nyist", "--help"}, {"auth-nyist", "auth", "--help"}, {"auth-nyist", "check-update"}} {
		app := newCLI()
		app.Writer = out
		if err := app.Run(args); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out.String(), "cannot determine") {
		t.Fatal(out.String())
	}
}

func TestStatusReadOnlyWithoutCredentials(t *testing.T) {
	for _, command := range []string{"status", "online"} {
		t.Run(command, func(t *testing.T) {
			out := setupCommands(t)
			detectNetwork = func(context.Context) (string, error) { return "nyist-stu", nil }
			inspectStatus = func(_ context.Context, cfg campus.Config) (campus.Status, error) {
				if cfg.Username != "" || cfg.Password != "" || cfg.Network != "nyist-stu" || cfg.Host != "portal.example" || cfg.AcID != "7" {
					t.Fatalf("unexpected config: %+v", cfg)
				}
				return campus.Status{Online: true, Network: cfg.Network, Username: "student", IP: "192.0.2.1"}, nil
			}
			loginLogout = func(string, string, *libauth.UrlProvider, bool, string, string) error {
				t.Fatal("unexpected authentication")
				return nil
			}
			app := newCLI()
			app.Writer = out
			if err := app.Run([]string{"auth-nyist", command, "--host", "portal.example", "--ac-id", "7"}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Status: online") {
				t.Fatal(out.String())
			}
		})
	}
}

func TestDefaultAuthAndStatusError(t *testing.T) {
	setupCommands(t)
	detectNetwork = func(context.Context) (string, error) { return "nyist-tec", nil }
	isOnline = func(*libauth.UrlProvider, string) (bool, error, string) {
		return false, errors.New("portal timeout"), ""
	}
	loginLogout = func(string, string, *libauth.UrlProvider, bool, string, string) error {
		t.Fatal("authentication after status failure")
		return nil
	}
	err := newCLI().Run([]string{"auth-nyist", "--username", "student", "--password", "secret"})
	if err == nil || !strings.Contains(err.Error(), "cannot determine portal status") {
		t.Fatalf("got %v", err)
	}
}

func TestExplicitNetworkDoesNotInspectOrSwitch(t *testing.T) {
	setupCommands(t)
	detectNetwork = func(context.Context) (string, error) { t.Fatal("explicit selection should not detect"); return "", nil }
	isOnline = func(*libauth.UrlProvider, string) (bool, error, string) { return true, nil, "student" }
	if err := newCLI().Run([]string{"auth-nyist", "auth", "--network", "nyist-stu"}); err != nil {
		t.Fatal(err)
	}
	if settings.Network != "nyist-stu" {
		t.Fatal(settings.Network)
	}
}

func TestDefaultAuthUsesCredentialsWithoutRealLogin(t *testing.T) {
	setupCommands(t)
	detectNetwork = func(context.Context) (string, error) { return "", nil }
	isOnline = func(*libauth.UrlProvider, string) (bool, error, string) { return false, nil, "" }
	called := false
	loginLogout = func(user, password string, host *libauth.UrlProvider, logout bool, ip, acID string) error {
		called = true
		if user != "student" || password != "secret" || logout || acID != "8" || host.LoginUriBase() != "http://portal.example/cgi-bin/srun_portal" {
			t.Fatal("wrong auth parameters")
		}
		return nil
	}
	if err := newCLI().Run([]string{"auth-nyist", "--username", "student", "--password", "secret", "--host", "http://portal.example", "--ac-id", "8"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("default action did not authenticate")
	}
}

func TestNetworkDetectionFailureStopsAuthentication(t *testing.T) {
	setupCommands(t)
	detectNetwork = func(context.Context) (string, error) { return "", errors.New("unknown Wi-Fi") }
	isOnline = func(*libauth.UrlProvider, string) (bool, error, string) {
		t.Fatal("portal contacted after detection failure")
		return false, nil, ""
	}
	err := newCLI().Run([]string{"auth-nyist"})
	if err == nil || !strings.Contains(err.Error(), "--network nyist-tec") {
		t.Fatalf("got %v", err)
	}
}

func TestAuthRejectsOtherOnlineAccount(t *testing.T) {
	for _, logout := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "logout"}[logout], func(t *testing.T) {
			setupCommands(t)
			settings = Settings{Username: "student@realm", Password: "secret"}
			isOnline = func(*libauth.UrlProvider, string) (bool, error, string) { return true, nil, "student@other" }
			loginLogout = func(string, string, *libauth.UrlProvider, bool, string, string) error {
				t.Fatal("must not modify another session")
				return nil
			}
			if err := authOnce(logout); !errors.Is(err, campus.ErrOtherAccount) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestLogoutDoesNotAdoptOnlineUsername(t *testing.T) {
	setupCommands(t)
	settings = Settings{Daemon: true}
	isOnline = func(*libauth.UrlProvider, string) (bool, error, string) { return true, nil, "other" }
	if err := authOnce(true); err == nil || !strings.Contains(err.Error(), "username can't be empty") {
		t.Fatalf("got %v", err)
	}
	if settings.Username != "" {
		t.Fatal("silently adopted online account")
	}
}

func TestPortalAccountRealmMatching(t *testing.T) {
	for _, tc := range []struct {
		want, got string
		match     bool
	}{
		{"student", "student@realm", true}, {"student@REALM", "student@realm", true}, {"student@realm", "student", false}, {"Student", "student@realm", false}, {"student", "other", false},
	} {
		if samePortalAccount(tc.want, tc.got) != tc.match {
			t.Fatalf("%q vs %q", tc.want, tc.got)
		}
	}
}

func TestStatusUnavailableIsNotOffline(t *testing.T) {
	out := setupCommands(t)
	inspectStatus = func(context.Context, campus.Config) (campus.Status, error) {
		return campus.Status{}, errors.New("unavailable")
	}
	app := newCLI()
	app.Writer = out
	err := app.Run([]string{"auth-nyist", "status", "--network", "nyist-tec"})
	if err == nil || strings.Contains(out.String(), "offline") {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
}
