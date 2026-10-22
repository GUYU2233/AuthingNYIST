//go:build linux
// +build linux

package platform

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Palvef/AuthingNYIST/internal/app"
)

func TestLinuxInvalidFlags(t *testing.T) {
	for _, args := range [][]string{{"--connect", "--disconnect"}, {"--autostart", "bad"}, {"unexpected"}} {
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
	if !strings.Contains(out.String(), "-daemon") || !strings.Contains(out.String(), "-autostart") {
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
