//go:build linux

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/tray"
)

// trayProcesses selects only this user's tray processes for this configuration.
func trayProcesses(config string) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	target, err := filepath.Abs(config)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		info, e := os.Stat(base)
		if e != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != os.Getuid() {
			continue
		}
		data, e := os.ReadFile(filepath.Join(base, "cmdline"))
		if e != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		if len(args) == 0 {
			continue
		}
		exe, e := os.Readlink(filepath.Join(base, "exe"))
		if e != nil || filepath.Base(strings.TrimSuffix(exe, " (deleted)")) != "nyist-connect" {
			continue
		}
		trayMode := false
		path, e := app.ConfigPath()
		if e != nil {
			continue
		}
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--tray", "-tray":
				trayMode = true
			case "--config", "-config":
				if i+1 < len(args) {
					i++
					path = args[i]
				}
			default:
				for _, prefix := range []string{"--config=", "-config="} {
					if strings.HasPrefix(args[i], prefix) {
						path = strings.TrimPrefix(args[i], prefix)
					}
				}
			}
		}
		if !filepath.IsAbs(path) {
			cwd, e := os.Readlink(filepath.Join(base, "cwd"))
			if e != nil {
				continue
			}
			path = filepath.Join(cwd, path)
		}
		if trayMode && filepath.Clean(path) == target {
			ids = append(ids, pid)
		}
	}
	return ids, nil
}

func linuxSetTray(exe, config string, enabled bool) error {
	ids, err := trayProcesses(config)
	if err != nil {
		return err
	}
	if !enabled {
		for _, pid := range ids {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
				return err
			}
		}
		return nil
	}
	if len(ids) > 0 {
		return nil
	}
	if err := tray.Available(); err != nil {
		return err
	}
	cmd := exec.Command(exe, "--config", config, "--tray")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("开启托盘: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
