//go:build linux
// +build linux

package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type linuxFakeRunner struct {
	calls      [][]string
	enabled    bool
	failEnable bool
	missing    bool
}

func (f *linuxFakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.missing {
		return nil, errors.New("executable file not found")
	}
	if len(args) > 0 && args[0] == "--user" {
		args = args[1:]
	}
	switch args[0] {
	case "is-enabled":
		if f.enabled {
			return []byte("enabled\n"), nil
		}
		return []byte("disabled\n"), errors.New("exit status 1")
	case "enable":
		f.enabled = true
		if f.failEnable {
			f.failEnable = false
			return []byte("failure"), errors.New("exit status 1")
		}
	case "disable":
		f.enabled = false
	}
	return nil, nil
}
func linuxTestService(t *testing.T, f *linuxFakeRunner) *LinuxService {
	t.Helper()
	dir := t.TempDir()
	return &LinuxService{Runner: f, UnitPath: filepath.Join(dir, "nyist-auth.service"), Executable: filepath.Join(dir, "NYIST App", "nyist"), ConfigPath: filepath.Join(dir, "a user", "config.json")}
}
func TestLinuxRenderUnit(t *testing.T) {
	s := linuxTestService(t, &linuxFakeRunner{})
	s.ConfigPath = filepath.Join(filepath.Dir(s.UnitPath), "a", "$X%h", "quo\"te\\config.json")
	unit, err := s.RenderUnit()
	if err != nil {
		t.Fatal(err)
	}
	quotedExe, _ := linuxUnitQuote(s.Executable)
	quotedConfig, _ := linuxUnitQuote(s.ConfigPath)
	if !strings.Contains(quotedConfig, "$$X%%h") || !strings.Contains(quotedConfig, `quo\"te\\config.json`) {
		t.Fatal("unsafe quoting", quotedConfig)
	}
	for _, part := range []string{"ExecStart=" + quotedExe + " --config " + quotedConfig + " --daemon", "WantedBy=default.target", "Restart=no"} {
		if !strings.Contains(unit, part) {
			t.Fatalf("missing %q in %s", part, unit)
		}
	}
	s.System = true
	unit, err = s.RenderUnit()
	if err != nil || !strings.Contains(unit, "WantedBy=multi-user.target") {
		t.Fatal(unit, err)
	}
	s.ConfigPath = "relative.json"
	if _, err = s.RenderUnit(); err == nil {
		t.Fatal("accepted relative config")
	}
	s.ConfigPath = filepath.Join(filepath.Dir(s.UnitPath), "new\nline")
	if _, err = s.RenderUnit(); err == nil {
		t.Fatal("accepted newline")
	}
}
func TestLinuxEnableDisableCommit(t *testing.T) {
	f := &linuxFakeRunner{}
	s := linuxTestService(t, f)
	saved := false
	if err := s.SetEnabled(context.Background(), true, func(v bool) error { saved = v; return nil }); err != nil {
		t.Fatal(err)
	}
	if !saved || !f.enabled {
		t.Fatal("configuration and service mismatch")
	}
	if _, err := os.Stat(s.UnitPath); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled(context.Background(), false, func(v bool) error { saved = v; return nil }); err != nil {
		t.Fatal(err)
	}
	if saved || f.enabled {
		t.Fatal("disable failed")
	}
	if _, err := os.Stat(s.UnitPath); !os.IsNotExist(err) {
		t.Fatal("unit not removed", err)
	}
	if !reflect.DeepEqual(f.calls[0], []string{"systemctl", "--user", "is-enabled", "nyist-auth.service"}) {
		t.Fatal(f.calls)
	}
	for _, call := range f.calls {
		for _, arg := range call {
			if arg == "--now" || arg == "stop" || arg == "start" {
				t.Fatal("unexpected network-affecting lifecycle", call)
			}
		}
	}
}
func TestLinuxEnableRollback(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			f := &linuxFakeRunner{enabled: existing, failEnable: true}
			s := linuxTestService(t, f)
			if existing {
				if err := os.WriteFile(s.UnitPath, []byte("old unit"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			committed := false
			err := s.SetEnabled(context.Background(), true, func(bool) error { committed = true; return nil })
			if err == nil || committed {
				t.Fatal("failure committed", err)
			}
			if f.enabled != existing {
				t.Fatal("enablement not restored")
			}
			data, e := os.ReadFile(s.UnitPath)
			if existing {
				if e != nil || string(data) != "old unit" {
					t.Fatal("old unit lost", e)
				}
			} else if !os.IsNotExist(e) {
				t.Fatal("new unit retained", e)
			}
		})
	}
}
func TestLinuxSaveFailureRollsBack(t *testing.T) {
	f := &linuxFakeRunner{}
	s := linuxTestService(t, f)
	err := s.SetEnabled(context.Background(), true, func(bool) error { return errors.New("save denied") })
	if err == nil || !strings.Contains(err.Error(), "save denied") || f.enabled {
		t.Fatal(err, f.enabled)
	}
	if _, err = os.Stat(s.UnitPath); !os.IsNotExist(err) {
		t.Fatal("unit retained", err)
	}
}
func TestLinuxMissingCommand(t *testing.T) {
	f := &linuxFakeRunner{missing: true}
	s := linuxTestService(t, f)
	if err := s.SetEnabled(context.Background(), true, nil); err == nil || !strings.Contains(err.Error(), "systemctl") {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.UnitPath); !os.IsNotExist(err) {
		t.Fatal("unit written despite unavailable systemctl")
	}
}
func TestLinuxSystemArguments(t *testing.T) {
	f := &linuxFakeRunner{}
	s := linuxTestService(t, f)
	s.System = true
	if _, err := s.Enabled(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls[0], []string{"systemctl", "is-enabled", "nyist-auth.service"}) {
		t.Fatal(f.calls)
	}
}
