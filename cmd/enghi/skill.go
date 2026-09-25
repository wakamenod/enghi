package main

import (
	"flag"
	"fmt"
	"net"
	"path/filepath"

	"github.com/wakamenod/enghi/internal/config"
	"github.com/wakamenod/enghi/internal/skill"
)

// cmdInstallSkill writes the Claude Code skill to ~/.claude/skills/enghi, so
// that Claude Code can capture, write pages and help with the weekly review
// through the API. Running it again overwrites the skill with this binary's
// version.
func cmdInstallSkill(args []string) error {
	fs := flag.NewFlagSet("install-skill", flag.ExitOnError)
	configPath := fs.String("config", "", "path to the configuration file")
	dir := fs.String("dir", "", "skills directory (defaults to skills_dir, ~/.claude/skills)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	root := cfg.SkillsDir
	if *dir != "" {
		root = *dir
	}

	baseURL := "http://" + net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	out, err := skill.Write(filepath.Join(root, "enghi"), baseURL)
	if err != nil {
		return err
	}
	fmt.Printf("wrote: %s\n", out)
	fmt.Printf("  server: %s\n", baseURL)
	fmt.Println("\nClaude Code picks it up in its next session.")
	return nil
}
