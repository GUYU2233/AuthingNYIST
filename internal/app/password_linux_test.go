//go:build linux
// +build linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestMain disables the real Secret Service for every test in this package.
// Individual tests install their own fake with useKeyring.
func TestMain(m *testing.M) {
	keyringExec = func(context.Context, []byte, ...string) ([]byte, error) {
		return nil, errors.New("keyring disabled in tests")
	}
	os.Exit(m.Run())
}

func useKeyring(t *testing.T, fn func(context.Context, []byte, ...string) ([]byte, error)) {
	t.Helper()
	old := keyringExec
	keyringExec = fn
	t.Cleanup(func() { keyringExec = old })
}

func readStoredPassword(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	return raw.Password
}

func writeConfig(t *testing.T, path string, c Config) {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func containsArg(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestKeyringSaveStoresReferenceOnly(t *testing.T) {
	var stored []byte
	var storeArgs []string
	useKeyring(t, func(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > keyringTimeout {
			t.Errorf("secret-tool context is not bounded: deadline=%v ok=%v", deadline, ok)
		}
		if len(args) == 0 {
			t.Error("missing secret-tool verb")
			return nil, errors.New("bad args")
		}
		switch args[0] {
		case "store":
			stored = append([]byte(nil), stdin...)
			storeArgs = append([]string(nil), args...)
			return nil, nil
		case "lookup":
			if len(stdin) != 0 {
				t.Error("lookup must not receive the secret on stdin")
			}
			return append(append([]byte(nil), stored...), '\n'), nil
		default:
			return nil, errors.New("unexpected secret-tool verb")
		}
	})

	path := filepath.Join(t.TempDir(), "settings", "config.json")
	c := testConfig()
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	if got := PasswordStorage(); got != PasswordBackendKeyring {
		t.Fatalf("PasswordStorage = %q, want %q", got, PasswordBackendKeyring)
	}
	token := readStoredPassword(t, path)
	if !regexp.MustCompile(`^keyring:v1:[0-9a-f]{16}$`).MatchString(token) {
		t.Fatalf("unexpected token format %q", token)
	}
	if want := keyringPrefix + passwordKeyID(path); token != want {
		t.Fatalf("token = %q, want %q", token, want)
	}
	if string(stored) != c.Password {
		t.Fatal("secret was not fed to secret-tool on stdin")
	}
	for _, a := range storeArgs {
		if strings.Contains(a, c.Password) {
			t.Fatal("secret leaked into secret-tool argv")
		}
	}
	if !containsArg(storeArgs, "service", keyringService) || !containsArg(storeArgs, "account", passwordKeyID(path)) {
		t.Fatalf("store attributes missing: %v", storeArgs)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(c.Password)) {
		t.Fatal("plaintext password persisted despite successful keyring store")
	}
	got, err := Load(path)
	if err != nil || got != c {
		t.Fatalf("round trip through keyring: %v", err)
	}
}

func TestSaveFallsBackToPlaintextWhenKeyringFails(t *testing.T) {
	cases := map[string]func(*testing.T){
		"runner error": func(t *testing.T) {
			useKeyring(t, func(context.Context, []byte, ...string) ([]byte, error) {
				return nil, errors.New("no session bus")
			})
		},
		"binary missing": func(t *testing.T) {
			// Exercise the real runner with no secret-tool on PATH.
			useKeyring(t, runSecretTool)
			t.Setenv("PATH", t.TempDir())
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			setup(t)
			path := filepath.Join(t.TempDir(), "config.json")
			c := testConfig()
			if err := Save(path, c); err != nil {
				t.Fatalf("fallback must never break saving: %v", err)
			}
			if got := PasswordStorage(); got != PasswordBackendFile {
				t.Fatalf("PasswordStorage = %q, want %q", got, PasswordBackendFile)
			}
			if got := readStoredPassword(t, path); got != c.Password {
				t.Fatal("expected plaintext fallback password")
			}
			got, err := Load(path)
			if err != nil || got != c {
				t.Fatalf("plaintext fallback round trip: %v", err)
			}
		})
	}
}

func TestLoadResolvesKeyringToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	id := passwordKeyID(path)
	want := "secret with spaces "
	useKeyring(t, func(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
		if len(stdin) != 0 {
			t.Error("lookup must not receive the secret on stdin")
		}
		if len(args) != 5 || args[0] != "lookup" || !containsArg(args, "service", keyringService) || !containsArg(args, "account", id) {
			t.Fatalf("unexpected lookup arguments: %v", args)
		}
		return []byte(want + "\r\n"), nil
	})
	c := testConfig()
	c.Password = keyringPrefix + id
	writeConfig(t, path, c)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Password = want
	if got != c {
		t.Fatalf("resolved config = %#v, want %#v", got, c)
	}
}

func TestLoadUnresolvableKeyringTokenBlanksPassword(t *testing.T) {
	cases := map[string]func(*testing.T){
		"lookup error": func(t *testing.T) {
			useKeyring(t, func(context.Context, []byte, ...string) ([]byte, error) {
				return nil, errors.New("secret service unavailable")
			})
		},
		"empty secret": func(t *testing.T) {
			useKeyring(t, func(context.Context, []byte, ...string) ([]byte, error) {
				return []byte("\n"), nil
			})
		},
		"malformed reference": func(t *testing.T) {
			useKeyring(t, func(context.Context, []byte, ...string) ([]byte, error) {
				t.Error("malformed reference must not reach secret-tool")
				return nil, errors.New("unexpected lookup")
			})
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			setup(t)
			path := filepath.Join(t.TempDir(), "config.json")
			token := keyringPrefix + passwordKeyID(path)
			if name == "malformed reference" {
				token = keyringPrefix + "../../etc/passwd"
			}
			c := testConfig()
			c.Password = token
			writeConfig(t, path, c)
			got, err := Load(path)
			if err == nil {
				t.Fatal("unresolvable keyring reference must fail")
			}
			if got.Password != "" {
				t.Fatalf("password not blanked: %q", got.Password)
			}
			if got.Username != c.Username || got.Network != c.Network {
				t.Fatalf("non-secret settings lost: %#v", got)
			}
			if !strings.Contains(err.Error(), "keyring") {
				t.Fatalf("error should point at the keyring: %v", err)
			}
			if strings.Contains(err.Error(), c.Username) {
				t.Fatalf("error leaks account details: %v", err)
			}
		})
	}
}

func TestWindowsProtectedPasswordStillRejectedOnUnix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := testConfig()
	c.Password = "dpapi:AAAA"
	writeConfig(t, path, c)
	got, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "Windows-protected") {
		t.Fatalf("dpapi reference accepted: %v", err)
	}
	if got.Password != "" {
		t.Fatal("dpapi failure must blank the password")
	}
}

func TestPasswordControlCharactersStillRejected(t *testing.T) {
	useKeyring(t, func(context.Context, []byte, ...string) ([]byte, error) {
		t.Error("invalid password should not reach the keyring")
		return nil, errors.New("unexpected store")
	})
	path := filepath.Join(t.TempDir(), "config.json")
	for _, password := range []string{"line\nbreak", "cr\rreturn", "nul\x00byte"} {
		cfg := testConfig()
		cfg.Password = password
		if err := Save(path, cfg); err == nil {
			t.Fatalf("control characters accepted: %q", password)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid password wrote a configuration file")
		}
	}
}

func TestKeyringOutputIsBounded(t *testing.T) {
	b := &boundedBuffer{limit: 8}
	if _, err := b.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("9")); err == nil {
		t.Fatal("output beyond the limit accepted")
	}
	if !b.overflow {
		t.Fatal("overflow not recorded")
	}
}
