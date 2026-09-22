//go:build darwin

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

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%[1]s</string>

  <key>ProgramArguments</key>
  <array>
    <string>%[2]s</string>
    <string>serve</string>
  </array>

  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>%[3]s</string>
  <key>StandardErrorPath</key>
  <string>%[4]s</string>

  <key>ProcessType</key>
  <string>Background</string>
</dict>
</plist>
`

// cmdInstallAgent writes out a launchd plist (DESIGN 8-11), so that enghi stays
// resident and is always there when the browser asks for it.
func cmdInstallAgent(args []string) error {
	fs := flag.NewFlagSet("install-agent", flag.ExitOnError)
	label := fs.String("label", "dev.enghi.server", "launchd label")
	load := fs.Bool("load", false, "run launchctl bootstrap after writing the plist")
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

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	logDir := filepath.Join(home, "Library", "Logs", "enghi")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}
	outLog := filepath.Join(logDir, "enghi.log")
	errLog := filepath.Join(logDir, "enghi.err.log")

	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o700); err != nil {
		return err
	}
	plistPath := filepath.Join(plistDir, *label+".plist")

	content := fmt.Sprintf(plistTemplate, *label, exe, outLog, errLog)
	if err := os.WriteFile(plistPath, []byte(content), 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote: %s\n", plistPath)
	fmt.Printf("  executable: %s\n", exe)
	fmt.Printf("  log: %s\n", outLog)
	fmt.Printf("  config: %s\n", config.Path())

	if !*load {
		fmt.Printf("\nto enable it:\n  launchctl bootstrap gui/$(id -u) %s\n", plistPath)
		fmt.Printf("to stop it:\n  launchctl bootout gui/$(id -u)/%s\n", *label)
		return nil
	}

	uid := fmt.Sprintf("gui/%d", os.Getuid())
	// Replace it if it is already registered
	_ = exec.Command("launchctl", "bootout", uid+"/"+*label).Run()
	cmd := exec.Command("launchctl", "bootstrap", uid, plistPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Println("registered with launchd")
	return nil
}
