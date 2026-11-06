//go:build linux

package platform

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Palvef/AuthingNYIST/internal/update"
	"github.com/Palvef/AuthingNYIST/internal/version"
)

func TestUpdateAndVersionFlagsSkipConfig(t *testing.T) {
	original := checkRelease
	t.Cleanup(func() { checkRelease = original })
	config := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(config, []byte("not valid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"linux", "macos"} {
		run := func(args []string, out, stderr *bytes.Buffer) error {
			if platform == "linux" {
				return linuxMain(args, nil, out, stderr)
			}
			return macMain(context.Background(), args, nil, out, stderr)
		}
		for _, flag := range []string{"--version", "--check-update"} {
			t.Run(platform+flag, func(t *testing.T) {
				calls := 0
				checkRelease = func(context.Context, string) (*update.Result, error) {
					calls++
					return &update.Result{Latest: update.Release{TagName: "v9.0.0"}}, nil
				}
				var out, stderr bytes.Buffer
				if err := run([]string{"--config", config, flag}, &out, &stderr); err != nil {
					t.Fatal(err)
				}
				if flag == "--version" {
					if calls != 0 || strings.TrimSpace(out.String()) != version.Version {
						t.Fatalf("version output=%q calls=%d", out.String(), calls)
					}
				} else if calls != 1 || !strings.Contains(out.String(), "v9.0.0") {
					t.Fatalf("update output=%q calls=%d", out.String(), calls)
				}
				if stderr.Len() != 0 {
					t.Fatalf("stderr: %q", stderr.String())
				}
			})
		}
		for _, other := range []string{"--connect", "--status", "--status-json", "--disconnect", "--daemon", "--version"} {
			t.Run(platform+" conflict "+other, func(t *testing.T) {
				checkRelease = func(context.Context, string) (*update.Result, error) {
					t.Fatal("conflicting flags performed update check")
					return nil, nil
				}
				var out, stderr bytes.Buffer
				if err := run([]string{"--config", config, "--check-update", other}, &out, &stderr); err == nil {
					t.Fatal("expected mutually exclusive operation error")
				}
				if out.Len() != 0 {
					t.Fatalf("unexpected output: %q", out.String())
				}
			})
		}
	}
}
