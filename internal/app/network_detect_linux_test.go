//go:build linux

package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestCheckReportsDetectedNetwork(t *testing.T) {
	oldCommand, oldWireless := commandOutput, linuxWirelessPresent
	defer func() { commandOutput = oldCommand; linuxWirelessPresent = oldWireless }()
	linuxWirelessPresent = func() (bool, error) { return true, nil }
	commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "nmcli" || strings.Contains(strings.Join(args, " "), "connection up") {
			t.Fatal("status must only inspect Wi-Fi")
		}
		return []byte("yes:nyist-stu"), nil
	}
	cfg, close := mockPortal(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":"not_online_error"}`))
	})
	defer close()
	cfg.Network = "auto"
	s, err := Check(context.Background(), cfg)
	if err != nil || s.Network != "nyist-stu" || s.Online {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestAutoLinuxNeverSwitchesWiFi(t *testing.T) {
	oldCommand, oldWireless := commandOutput, linuxWirelessPresent
	defer func() { commandOutput = oldCommand; linuxWirelessPresent = oldWireless }()
	for _, tc := range []struct {
		name, output                   string
		wireless, commandFail, wantErr bool
	}{
		{name: "teacher", output: "yes:nyist-tec", wireless: true},
		{name: "student", output: "yes:nyist-stu", wireless: true},
		{name: "unknown", output: "yes:home", wireless: true, wantErr: true},
		{name: "disassociated", output: "no:nyist-tec", wireless: true},
		{name: "wired"},
		{name: "missing nmcli", wireless: true, commandFail: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linuxWirelessPresent = func() (bool, error) { return tc.wireless, nil }
			calls := 0
			commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				calls++
				if name != "nmcli" || strings.Join(args, " ") != "-t --escape no -f ACTIVE,SSID device wifi list --rescan no" {
					t.Fatalf("unsafe command %s %v", name, args)
				}
				if tc.commandFail {
					return nil, errors.New("private error")
				}
				return []byte(tc.output), nil
			}
			cfg := DefaultConfig()
			cfg.Network = "auto"
			err := EnsureNetwork(context.Background(), cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if !tc.wireless && calls != 0 {
				t.Fatal("wired fallback invoked nmcli")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("raw error leaked")
			}
		})
	}
}
