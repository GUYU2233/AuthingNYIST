//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runName = "NYISTConnect"

// WindowsAutoStart is scoped to the current user and requires no elevation.
func WindowsAutoStart() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()
	_, _, err = key.GetStringValue(runName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func WindowsStartupCommand(exe, config string) string {
	return syscall.EscapeArg(exe) + " --config " + syscall.EscapeArg(config) + " --startup"
}

func SetWindowsAutoStart(enabled bool, config string) error {
	if !enabled {
		key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer key.Close()
		err = key.DeleteValue(runName)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	config, err = filepath.Abs(config)
	if err != nil {
		return fmt.Errorf("config path: %w", err)
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(runName, WindowsStartupCommand(exe, config))
}
