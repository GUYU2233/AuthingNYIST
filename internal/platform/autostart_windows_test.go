//go:build windows

package platform

import (
	"strings"
	"testing"
)

func TestWindowsStartupCommand(t *testing.T) {
	got := WindowsStartupCommand(`C:\Program Files\NYIST\nyist.exe`, `C:\Users\Name Here\config.json`)
	want := `"C:\Program Files\NYIST\nyist.exe" --config "C:\Users\Name Here\config.json" --startup`
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, " --startup") {
		t.Fatal("missing startup flag")
	}
}
