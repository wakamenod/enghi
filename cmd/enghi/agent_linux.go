//go:build linux

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wakamenod/enghi/internal/config"
)

// A systemd user unit, the counterpart of the launchd plist on macOS
// (agent_darwin.go).
//
// Logs go to journald rather than to a file. (The launchd side writes to
// ~/Library/Logs only because launchd has no equivalent.)
const unitTemplate = `[Unit]
Description=enghi - local-only wiki and GTD
After=network.target

[Service]
Type=simple
ExecStart=%s serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

// cmdInstallAgent writes out a systemd user unit (DESIGN 8-11), so that enghi
// stays resident and is always there when the browser asks for it.
func cmdInstallAgent(args []string) error {
	fs := flag.NewFlagSet("install-agent", flag.ExitOnError)
	label := fs.String("label", "enghi", "systemd unit name (without .service)")
	load := fs.Bool("load", false, "run systemctl --user enable --now after writing the unit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}

	unitDir := filepath.Join(configHome(), "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o700); err != nil {
		return err
	}
	unitPath := filepath.Join(unitDir, *label+".service")

	if err := os.WriteFile(unitPath, []byte(fmt.Sprintf(unitTemplate, exe)), 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote: %s\n", unitPath)
	fmt.Printf("  executable: %s\n", exe)
	fmt.Printf("  log: journalctl --user -u %s\n", *label)
	fmt.Printf("  config: %s\n", config.Path())

	// Staying resident after logout requires linger. Without it the service dies
	// with the session, so a resident server must always mention this.
	fmt.Printf("\nto keep it running after logout (once):\n  loginctl enable-linger $(whoami)\n")

	if !*load {
		fmt.Printf("\nto enable it:\n  systemctl --user daemon-reload\n  systemctl --user enable --now %s\n", *label)
		fmt.Printf("to stop it:\n  systemctl --user disable --now %s\n", *label)
		return nil
	}

	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("systemctl", "--user", "enable", "--now", *label).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl enable --now: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Println("registered with systemd")
	return nil
}

// configHome decides where the unit goes, following the same XDG rules as the
// config package.
func configHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}
