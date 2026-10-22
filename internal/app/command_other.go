//go:build !windows
// +build !windows

package app

import "os/exec"

func configureCommand(cmd *exec.Cmd) {}
