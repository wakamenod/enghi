// Package config loads the user configuration and resolves defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the content of ~/.config/enghi/config.toml.
type Config struct {
	Port int    `toml:"port"`
	Host string `toml:"host"` // fixed to 127.0.0.1; 0.0.0.0 is rejected (DESIGN 4.4)
	// AllowedHosts: extra names accepted in the Host header. Set this only when
	// a reverse proxy in front makes enghi reachable from another device.
	// **Empty means loopback only, as before.**
	//
	// The bind address stays 127.0.0.1. The proxy terminates the connection and
	// forwards to loopback, so all this adds is "which name a request may
	// arrive under".
	//
	// **Wildcards are not accepted.** Host validation is the only effective
	// defense against DNS rebinding (DESIGN 4.4), so it is relaxed one literal
	// name at a time.
	//
	// **Setting this makes enghi reachable from other machines, which means
	// real authentication in front of it is mandatory** (DESIGN 4.4). enghi
	// has no authentication of its own.
	AllowedHosts []string `toml:"allowed_hosts"`
	DBPath       string   `toml:"db_path"`
	// FilesDBPath: binaries such as images. **Kept in a separate file** from the
	// main database, so backups stay small.
	FilesDBPath string `toml:"files_db_path"`
	ExportDir   string `toml:"export_dir"`
	// RevisionCompactMinutes: overwrite the previous revision when it is newer
	// than this many minutes (DESIGN 4.2)
	RevisionCompactMinutes int `toml:"revision_compact_minutes"`

	// Backups. Taken once a day while resident, with VACUUM INTO.
	BackupDir     string `toml:"backup_dir"`
	BackupKeep    int    `toml:"backup_keep"`    // how many generations to keep
	BackupEnabled *bool  `toml:"backup_enabled"` // enabled by default
}

// BackupOn reports whether backups are enabled (unset means enabled).
func (c Config) BackupOn() bool { return c.BackupEnabled == nil || *c.BackupEnabled }

func dataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func configHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// Path returns the default location of the configuration file.
func Path() string { return filepath.Join(configHome(), "enghi", "config.toml") }

// filesPathFor derives the image database path from the main database path.
// **Change db_path and the images follow**, instead of being left behind
// somewhere else.
func filesPathFor(dbPath string) string {
	return strings.TrimSuffix(dbPath, ".db") + "-files.db"
}

// Default is the configuration used when there is no configuration file.
func Default() Config { return withDefaults(Config{}) }

// withDefaults fills in the fields that were not set.
//
// **Do not apply defaults before reading the configuration file.** Doing so
// makes "was this set?" impossible to answer, and fields that should follow
// another one — files_db_path following db_path — freeze at their default.
func withDefaults(c Config) Config {
	if c.Port == 0 {
		c.Port = 7777
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(dataHome(), "enghi", "enghi.db")
	}
	c.DBPath = expand(c.DBPath)

	if c.FilesDBPath == "" {
		c.FilesDBPath = filesPathFor(c.DBPath)
	}
	if c.ExportDir == "" {
		c.ExportDir = filepath.Join(dataHome(), "enghi", "export")
	}
	if c.BackupDir == "" {
		c.BackupDir = filepath.Join(dataHome(), "enghi", "backup")
	}
	if c.RevisionCompactMinutes == 0 {
		c.RevisionCompactMinutes = 10
	}
	if c.BackupKeep == 0 {
		c.BackupKeep = 7
	}
	c.FilesDBPath = expand(c.FilesDBPath)
	c.ExportDir = expand(c.ExportDir)
	c.BackupDir = expand(c.BackupDir)
	return c
}

// Load reads the configuration file and fills in defaults. A missing file is
// not an error.
func Load(path string) (Config, error) {
	if path == "" {
		path = Path()
	}
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			c = withDefaults(c)
			return c, c.validate()
		}
		return withDefaults(c), err
	}
	if err := toml.Unmarshal(b, &c); err != nil {
		return withDefaults(c), fmt.Errorf("config file %s: %w", path, err)
	}
	c = withDefaults(c)
	return c, c.validate()
}

// validate enforces the local-only premise (DESIGN 4.4).
func (c Config) validate() error {
	switch c.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return fmt.Errorf("host %q is not allowed: enghi binds to loopback only (DESIGN 4.4)", c.Host)
	}
	for _, h := range c.AllowedHosts {
		// Hand-written config, so surrounding spaces are fine. A space inside is
		// not part of a host name, so reject it.
		h = strings.TrimSpace(h)
		if h == "" {
			return fmt.Errorf("allowed_hosts contains an empty entry")
		}
		if strings.ContainsAny(h, "*?/ ") {
			return fmt.Errorf("allowed_hosts %q: wildcards and separators are not allowed; list one literal name at a time (DESIGN 4.4)", h)
		}
	}
	return nil
}

// NormalizedAllowedHosts puts allowed_hosts into a comparable form: host names
// are case-insensitive, so lower-case them, and drop a port if one was written.
func (c Config) NormalizedAllowedHosts() []string {
	out := make([]string, 0, len(c.AllowedHosts))
	for _, h := range c.AllowedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
			h = h[:i]
		}
		if h = strings.Trim(h, "[]"); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func expand(p string) string {
	if len(p) > 1 && p[0] == '~' && (p[1] == '/' || p[1] == filepath.Separator) {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
