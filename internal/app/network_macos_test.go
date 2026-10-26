package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMacNetworkSelection(t *testing.T) {
	hardware := "Hardware Port: Wi-Fi\nDevice: en0\nEthernet Address: 00\n\nHardware Port: USB 10/100/1000 LAN\nDevice: en5\n"
	tests := []struct {
		name, current, saved, route string
		fail                        string
		wantErr, selectWiFi         bool
	}{
		{name: "already selected", current: "Current Wi-Fi Network: nyist-tec"},
		{name: "saved selection", current: "Current Wi-Fi Network: home", saved: "Preferred networks on en0:\n\tnyist-tec\n", selectWiFi: true},
		{name: "not associated saved", current: "You are not associated with an AirPort network.", saved: "Preferred networks on en0:\n\tnyist-tec\n", selectWiFi: true},
		{name: "ethernet", route: "interface: en5", fail: "-getairportnetwork"},
		{name: "not saved", current: "Current Wi-Fi Network: home", saved: "Preferred networks on en0:\n\tnyist-tec-evil\n", wantErr: true},
		{name: "redacted", current: "<redacted>", wantErr: true},
		{name: "prefixed redacted", current: "Current Wi-Fi Network: <redacted>", wantErr: true},
		{name: "inspection fails", fail: "-getairportnetwork", wantErr: true},
		{name: "selection fails", current: "Current Wi-Fi Network: home", saved: "Preferred networks on en0:\n\tnyist-tec\n", fail: "-setairportnetwork", wantErr: true, selectWiFi: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := commandOutput
			defer func() { commandOutput = old }()
			selected := false
			calls := 0
			commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				calls++
				for _, a := range args {
					if strings.Contains(a, "secret") {
						t.Fatal("password in argv")
					}
				}
				if args[0] == tt.fail {
					return nil, errors.New("private command output must not leak")
				}
				switch args[0] {
				case "-listallhardwareports":
					return []byte(hardware), nil
				case "-n":
					return []byte(tt.route), nil
				case "-getairportnetwork":
					if selected {
						return []byte("Current Wi-Fi Network: nyist-tec"), nil
					}
					return []byte(tt.current), nil
				case "-listpreferredwirelessnetworks":
					return []byte(tt.saved), nil
				case "-setairportnetwork":
					if !reflect.DeepEqual(args, []string{"-setairportnetwork", "en0", "nyist-tec"}) {
						t.Fatalf("unsafe args %v", args)
					}
					selected = true
					return nil, nil
				default:
					t.Fatalf("unexpected command %s %v", name, args)
					return nil, nil
				}
			}
			cfg := DefaultConfig()
			cfg.Password = "secret"
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := ensureMacNetwork(ctx, cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("command error leaked")
			}
			if tt.route != "" && calls != 2 {
				t.Fatal("Ethernet must not change Wi-Fi")
			}
			if selected && !tt.selectWiFi {
				t.Fatal("unexpected Wi-Fi selection")
			}
		})
	}
}
func TestMacNetworkParsersConservative(t *testing.T) {
	if len(macHardwarePorts("Hardware Port: Wi-Fi\nDevice: en0;bad\n")) != 0 {
		t.Fatal("invalid device accepted")
	}
	if macRouteInterface("interface: utun0") != "" {
		t.Fatal("tunnel must not be considered Ethernet")
	}
	if macCurrentSSID("Current Wi-Fi Network: nyist-tec\nunexpected") != "" {
		t.Fatal("ambiguous current SSID")
	}
	if macSavedSSID("unrecognized\nnyist-tec", "nyist-tec") {
		t.Fatal("unrecognized preferred list")
	}
}
func TestMacNetworkMustVerifySelection(t *testing.T) {
	old := commandOutput
	defer func() { commandOutput = old }()
	commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch args[0] {
		case "-listallhardwareports":
			return []byte("Hardware Port: Wi-Fi\nDevice: en0\n"), nil
		case "-n":
			return nil, errors.New("no route")
		case "-getairportnetwork":
			return []byte("Current Wi-Fi Network: other"), nil
		case "-listpreferredwirelessnetworks":
			return []byte("Preferred networks on en0:\n\tnyist-tec\n"), nil
		case "-setairportnetwork":
			return nil, nil
		}
		t.Fatal(args)
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if ensureMacNetwork(ctx, DefaultConfig()) == nil {
		t.Fatal("unverified selection accepted")
	}
}
