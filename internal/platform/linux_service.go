//go:build linux
// +build linux

package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LinuxRunner executes a program directly, never through a shell.
type LinuxRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type linuxExecRunner struct{}

func (linuxExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("缺少命令 %s: %w", name, err)
	}
	return exec.CommandContext(ctx, p, args...).CombinedOutput()
}

// LinuxService manages a single nyist-auth.service. System=false means systemd --user.
// UnitPath is injectable for tests; production callers should use NewLinuxService.
type LinuxService struct {
	Runner                           LinuxRunner
	UnitPath, Executable, ConfigPath string
	System                           bool
}

func NewLinuxService(executable, config string, system bool) (*LinuxService, error) {
	var dir string
	if system {
		if os.Geteuid() != 0 {
			return nil, fmt.Errorf("--system 需要 root；默认请使用 systemd --user")
		}
		dir = "/etc/systemd/system"
	} else {
		dir = os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			dir = filepath.Join(home, ".config")
		}
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("XDG_CONFIG_HOME 必须是绝对路径")
		}
		dir = filepath.Join(dir, "systemd", "user")
	}
	exe, err := filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	cfg, err := filepath.Abs(config)
	if err != nil {
		return nil, err
	}
	return &LinuxService{Runner: linuxExecRunner{}, UnitPath: filepath.Join(dir, "nyist-auth.service"), Executable: exe, ConfigPath: cfg, System: system}, nil
}

// systemd quoting is not shell quoting. Escape specifiers and environment expansion as well.
func linuxUnitQuote(s string) (string, error) {
	if strings.ContainsAny(s, "\x00\r\n") {
		return "", fmt.Errorf("unit 参数包含不支持的控制字符")
	}
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "$", "$$", "\t", "\\t")
	return "\"" + r.Replace(s) + "\"", nil
}
func (s *LinuxService) RenderUnit() (string, error) {
	if !filepath.IsAbs(s.Executable) || !filepath.IsAbs(s.ConfigPath) {
		return "", fmt.Errorf("二进制和配置路径必须是绝对路径")
	}
	exe, err := linuxUnitQuote(s.Executable)
	if err != nil {
		return "", err
	}
	cfg, err := linuxUnitQuote(s.ConfigPath)
	if err != nil {
		return "", err
	}
	target := "default.target"
	if s.System {
		target = "multi-user.target"
	}
	return "[Unit]\nDescription=NYIST network authentication\nWants=network-online.target\nAfter=network-online.target\n\n[Service]\nType=simple\nExecStart=" + exe + " --config " + cfg + " --daemon\nRestart=no\nUMask=0077\n\n[Install]\nWantedBy=" + target + "\n", nil
}
func (s *LinuxService) ctl(ctx context.Context, args ...string) ([]byte, error) {
	if s.Runner == nil {
		s.Runner = linuxExecRunner{}
	}
	if !s.System {
		args = append([]string{"--user"}, args...)
	}
	out, err := s.Runner.Run(ctx, "systemctl", args...)
	if err != nil {
		return out, fmt.Errorf("systemctl %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func (s *LinuxService) Enabled(ctx context.Context) (bool, error) {
	out, err := s.ctl(ctx, "is-enabled", "nyist-auth.service")
	state := strings.TrimSpace(string(out))
	if err == nil {
		return state == "enabled" || state == "enabled-runtime", nil
	}
	switch state {
	case "disabled", "not-found", "static", "indirect", "masked":
		return false, nil
	}
	return false, err
}
func linuxAtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".nyist-unit-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

// SetEnabled commits configuration only after systemctl succeeds. Any failure restores
// the old unit and enablement; rollback errors are reported, never silently discarded.
// Disabling does not stop a currently running daemon or implicitly log the user out.
func (s *LinuxService) SetEnabled(ctx context.Context, enable bool, commit func(bool) error) (err error) {
	oldEnabled, err := s.Enabled(ctx)
	if err != nil {
		return err
	}
	old, readErr := os.ReadFile(s.UnitPath)
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	mode := os.FileMode(0644)
	if existed {
		info, e := os.Stat(s.UnitPath)
		if e != nil {
			return e
		}
		mode = info.Mode().Perm()
	}
	changed := false
	defer func() {
		if err == nil || !changed {
			return
		}
		var failures []string
		// Rollback must also work after the caller's context was cancelled.
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Remove newly created enablement links while the unit still exists.
		if !oldEnabled {
			if _, e := s.ctl(rollbackCtx, "disable", "nyist-auth.service"); e != nil {
				failures = append(failures, e.Error())
			}
		}
		// Restore the old file before restoring enablement links.
		if existed {
			if e := linuxAtomicWrite(s.UnitPath, old, mode); e != nil {
				failures = append(failures, e.Error())
			}
		} else if e := os.Remove(s.UnitPath); e != nil && !os.IsNotExist(e) {
			failures = append(failures, e.Error())
		}
		if _, e := s.ctl(rollbackCtx, "daemon-reload"); e != nil {
			failures = append(failures, e.Error())
		}
		if oldEnabled {
			if _, e := s.ctl(rollbackCtx, "enable", "nyist-auth.service"); e != nil {
				failures = append(failures, e.Error())
			}
		}
		if len(failures) > 0 {
			err = fmt.Errorf("%w；回滚失败: %s", err, strings.Join(failures, "; "))
		}
	}()
	if enable {
		unit, e := s.RenderUnit()
		if e != nil {
			return e
		}
		if e = linuxAtomicWrite(s.UnitPath, []byte(unit), 0644); e != nil {
			return e
		}
		changed = true
		if _, err = s.ctl(ctx, "daemon-reload"); err != nil {
			return err
		}
	} else {
		changed = true
	}
	verb := "disable"
	if enable {
		verb = "enable"
	}
	if _, err = s.ctl(ctx, verb, "nyist-auth.service"); err != nil {
		return err
	}
	if !enable && existed {
		if err = os.Remove(s.UnitPath); err != nil {
			return err
		}
		if _, err = s.ctl(ctx, "daemon-reload"); err != nil {
			return err
		}
	}
	actual, checkErr := s.Enabled(ctx)
	if checkErr != nil {
		return checkErr
	}
	if actual != enable {
		return fmt.Errorf("systemctl 未达到请求的自启动状态: got %t, want %t", actual, enable)
	}
	if commit != nil {
		err = commit(actual)
	}
	return err
}
