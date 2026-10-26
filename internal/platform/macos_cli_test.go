package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Palvef/AuthingNYIST/internal/app"
)

func TestMacConfigJSONRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := app.DefaultConfig()
	cfg.Username = "user"
	cfg.Password = "secret no argv"
	b, _ := json.Marshal(cfg)
	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	input.Write(b)
	input.Seek(0, 0)
	var out, errOut bytes.Buffer
	if err = macMain(context.Background(), []string{"--config", path, "--save-config-stdin"}, input, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatal("save output must remain empty")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("configuration is not private")
		}
	}
	if err = macMain(context.Background(), []string{"--config", path, "--config-json"}, input, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var got app.Config
	if err = json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("config mismatch")
	}
}
func TestMacConfigRejectsBadInputWithoutEcho(t *testing.T) {
	for _, text := range []string{`null`, `[]`, `{"password":"secret","unknown":true}`, `{} {}`, `{"network":"evil","password":"secret"}`, `{"autoStart":true,"password":"secret"}`, strings.Repeat("x", 1024*1024+1)} {
		in, err := os.CreateTemp(t.TempDir(), "stdin")
		if err != nil {
			t.Fatal(err)
		}
		in.WriteString(text)
		in.Seek(0, 0)
		path := filepath.Join(t.TempDir(), "config.json")
		var out, errOut bytes.Buffer
		err = macMain(context.Background(), []string{"--config", path, "--save-config-stdin"}, in, &out, &errOut)
		in.Close()
		if err == nil {
			t.Fatal("invalid input accepted")
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("input echoed")
		}
		if out.Len() != 0 {
			t.Fatal("failure must not emit stdout")
		}
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("bad input wrote config")
		}
	}
}
func TestMacCLIUnsupportedAndExclusive(t *testing.T) {
	for _, args := range [][]string{{"--autostart", "enable"}, {"--system"}, {"--status-json", "--connect"}, {"--config-json", "--save-config-stdin"}, {"--password", "secret"}, {"connect"}} {
		var out, errOut bytes.Buffer
		if err := macMain(context.Background(), args, os.Stdin, &out, &errOut); err == nil {
			t.Fatal("expected error", args)
		}
		if out.Len() != 0 {
			t.Fatal("invalid operation emitted stdout")
		}
	}
}
func TestMacStatusJSONCanceledNoOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	err := macMain(ctx, []string{"--config", filepath.Join(t.TempDir(), "config.json"), "--status-json"}, os.Stdin, &out, &errOut)
	if err == nil || out.Len() != 0 {
		t.Fatal("status failure must leave stdout empty")
	}
}
