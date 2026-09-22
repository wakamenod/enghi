//go:build !darwin && !linux

package main

import "fmt"

// Running as a service differs per OS. We support launchd (macOS) and systemd
// (Linux) only; anywhere else, set it up yourself.
func cmdInstallAgent([]string) error {
	return fmt.Errorf("install-agent supports macOS (launchd) and Linux (systemd) only")
}
