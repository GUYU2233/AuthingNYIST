//go:build darwin
// +build darwin

package main

import (
	"github.com/Palvef/AuthingNYIST/internal/platform"
	"os"
)

func main() { os.Exit(platform.DarwinMain(os.Args[1:])) }
