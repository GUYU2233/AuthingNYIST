package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestAutoConfigExplicitAndDefaultCompatible(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Network != "nyist-tec" {
		t.Fatal("GUI default changed")
	}
	cfg.Network = "auto"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDetectReadOnlyWindowsAndMac(t *testing.T) {
	old := commandOutput
	defer func() { commandOutput = old }()
	for _, platform := range []string{"windows", "darwin"} {
		for _, ssid := range []string{"nyist-tec", "nyist-stu", "home", "nyist-tec-evil"} {
			t.Run(platform+"/"+ssid, func(t *testing.T) {
				commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
					joined := strings.Join(args, " ")
					switch joined {
					case "wlan show interfaces":
						return []byte("BSSID : ignored\n SSID : " + ssid + "\n"), nil
					case "-listallhardwareports":
						return []byte("Hardware Port: Wi-Fi\nDevice: en0\n"), nil
					case "-n get default":
						return []byte("interface: en0"), nil
					case "-getairportnetwork en0":
						return []byte("Current Wi-Fi Network: " + ssid), nil
					default:
						t.Fatalf("mutation or unexpected command: %s %s", name, joined)
						return nil, nil
					}
				}
				got, err := detectNetwork(context.Background(), platform)
				campus := ssid == "nyist-tec" || ssid == "nyist-stu"
				if campus && (err != nil || got != ssid) {
					t.Fatalf("got %q %v", got, err)
				}
				if !campus && (err == nil || !strings.Contains(err.Error(), "manually")) {
					t.Fatalf("unknown Wi-Fi accepted: %q %v", got, err)
				}
			})
		}
	}
}

func TestDetectMacWiredAndPrivacy(t *testing.T) {
	old := commandOutput
	defer func() { commandOutput = old }()
	for _, wired := range []bool{true, false} {
		commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			switch args[0] {
			case "-listallhardwareports":
				return []byte("Hardware Port: Wi-Fi\nDevice: en0\nHardware Port: Ethernet\nDevice: en5\n"), nil
			case "-n":
				if wired {
					return []byte("interface: en5"), nil
				}
				return []byte("interface: en0"), nil
			case "-getairportnetwork":
				if wired {
					t.Fatal("wired route inspected Wi-Fi")
				}
				return []byte("Current Wi-Fi Network: <redacted>"), nil
			default:
				t.Fatal("unexpected command", args)
				return nil, nil
			}
		}
		ssid, err := detectNetwork(context.Background(), "darwin")
		if wired && (ssid != "" || err != nil) {
			t.Fatalf("wired: %q %v", ssid, err)
		}
		if !wired && (err == nil || !strings.Contains(err.Error(), "privacy")) {
			t.Fatalf("privacy: %q %v", ssid, err)
		}
	}
}

func TestDetectionParsersAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		output string
		nmcli  bool
		want   string
		fail   bool
	}{
		{"no:nyist-tec\nyes:nyist-stu", true, "nyist-stu", false},
		{"yes:home:guest", true, "", true},
		{"yes:nyist-tec\nyes:nyist-stu", true, "", true},
		{"BSSID : nyist-tec\n", false, "", false},
		{"SSID : nyist-tec\nSSID : nyist-stu", false, "", true},
	} {
		got, err := detectedSSIDs(tc.output, tc.nmcli)
		if got != tc.want || (err != nil) != tc.fail {
			t.Fatalf("%q: %q %v", tc.output, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DetectNetwork(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCredentiallessCheckReportsActualOnlineSession(t *testing.T) {
	cfg, close := mockPortal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cgi-bin/rad_user_info" {
			t.Fatal("status mutated session")
		}
		w.Write([]byte(`{"error":"ok","user_name":"actual-user","domain":"nyiststu","online_ip":"192.0.2.3"}`))
	})
	defer close()
	cfg.Username = ""
	cfg.Password = ""
	cfg.Network = "auto"
	s, err := Check(context.Background(), cfg)
	if err != nil || !s.Online || s.Username != "actual-user" || s.Message != "Connected" {
		t.Fatalf("%+v %v", s, err)
	}
	cfg.Username = "another-user"
	s, err = Check(context.Background(), cfg)
	if err != nil || s.Online {
		t.Fatalf("account protection regressed: %+v %v", s, err)
	}
}
