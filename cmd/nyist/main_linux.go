//go:build linux
// +build linux

package main

import (
	"github.com/Palvef/AuthingNYIST/internal/platform"
	"os"
)

func main() { os.Exit(platform.LinuxMain(os.Args[1:])) }
