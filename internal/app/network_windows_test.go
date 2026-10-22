//go:build windows
// +build windows

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestConnectHonorsSelectedSSIDEvenWhenAlreadyOnline(t *testing.T) {
	original := commandOutput
	defer func() { commandOutput = original }()
	selected := "nyist-tec"
	switches := 0
	commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "netsh" {
			t.Fatalf("unexpected executable %s", name)
		}
		if len(args) > 1 && args[1] == "connect" {
			if strings.Join(args, " ") != "wlan connect name=nyist-stu ssid=nyist-stu" {
				t.Fatalf("wrong network command: %v", args)
			}
			switches++
			selected = "nyist-stu"
			return nil, nil
		}
		return []byte("SSID : " + selected + "\n"), nil
	}
	c, close := mockPortal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cgi-bin/rad_user_info" {
			t.Error("authentication must not run for already-online account")
		}
		json.NewEncoder(w).Encode(map[string]string{"error": "ok", "user_name": "test-user", "domain": "nyisttec"})
	})
	defer close()
	c.Network = "nyist-stu"
	s, e := Connect(context.Background(), c)
	if e != nil || !s.Online || switches != 1 {
		t.Fatalf("selected SSID ignored: online=%v switches=%d error=%v", s.Online, switches, e)
	}
	_, e = Connect(context.Background(), c)
	if e != nil || switches != 1 {
		t.Fatal("matching SSID was unnecessarily switched")
	}
}
