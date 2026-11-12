//go:build linux
// +build linux

package platform

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Palvef/AuthingNYIST/internal/app"
)

func TestLinuxInvalidFlags(t *testing.T) {
	for _, args := range [][]string{{"--connect", "--disconnect"}, {"--doctor", "--connect"}, {"--tray", "--daemon"}, {"--autostart", "bad"}, {"unexpected"}} {
		var out bytes.Buffer
		if err := linuxMain(args, os.Stdin, &out, &out); err == nil {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
}
func TestLinuxHelpDoesNotLoadConfig(t *testing.T) {
	var out bytes.Buffer
	if err := linuxMain([]string{"--help", "--config", "/nonexistent"}, os.Stdin, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-daemon") || !strings.Contains(out.String(), "-autostart") || !strings.Contains(out.String(), "-doctor") || !strings.Contains(out.String(), "-tray") {
		t.Fatal(out.String())
	}
}
func TestLinuxCredentials(t *testing.T) {
	cfg := app.DefaultConfig()
	if linuxCredentials(cfg) == nil {
		t.Fatal("accepted empty credentials")
	}
	cfg.Username = "student"
	cfg.Password = "password"
	if err := linuxCredentials(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestTrayRuntimeStopsOnCancel(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.Username, cfg.Password = "student", "password"
	ctx, cancel := context.WithCancel(context.Background())
	rt := newTrayRuntime(ctx, cfg, io.Discard)
	if got := rt.state().StatusText; got != "正在检查网络…" {
		t.Fatalf("initial status = %q", got)
	}
	rt.set("保活中", true, false)
	if s := rt.state(); !s.Connected || s.Busy || s.StatusLabel() != "状态：保活中" {
		t.Fatalf("state = %+v", s)
	}
	cancel()
	if stop := rt.stop(); stop != nil {
		stop()
	}
	rt.mu.Lock()
	running, cancelRef := rt.running, rt.cancel
	rt.mu.Unlock()
	if running || cancelRef != nil {
		t.Fatal("tray keepalive still marked running after shutdown")
	}
}

func TestTruncateStatus(t *testing.T) {
	if got := truncateStatus("  a\nb  "); got != "a b" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("错", 100)
	if got := []rune(truncateStatus(long)); len(got) != 81 || got[80] != '…' {
		t.Fatalf("long status not truncated: %d runes", len(got))
	}
}
