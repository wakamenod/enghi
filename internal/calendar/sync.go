package calendar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wakamenod/enghi/internal/settings"
)

// Why a shortcut: the enghi binary, ad-hoc signed and run by launchd, cannot
// hold the Calendars privacy permission, and neither can Emacs. Shortcuts.app
// is Apple-signed and holds it, so an upgrade of enghi never resets it, and
// `shortcuts run` works the same from a launchd job.

// Runner is the part that touches Shortcuts, behind an interface so tests
// never call /usr/bin/shortcuts.
type Runner interface {
	// Run runs the named shortcut and returns what it printed.
	Run(ctx context.Context, name string) ([]byte, error)
	// Installed reports whether a shortcut with that name exists.
	Installed(ctx context.Context, name string) (bool, error)
	// Open hands a .shortcut file to Shortcuts, which asks whether to add it.
	Open(ctx context.Context, path string) error
}

// RunTimeout bounds one run of the shortcut; it takes well under a second.
const RunTimeout = 30 * time.Second

// ShortcutsRunner is the real Runner, macOS only.
type ShortcutsRunner struct{}

const shortcutsBin = "/usr/bin/shortcuts"

func (ShortcutsRunner) Run(ctx context.Context, name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, shortcutsBin, "run", name)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (ShortcutsRunner) Installed(ctx context.Context, name string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, shortcutsBin, "list").Output()
	if err != nil {
		return false, err
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) == name {
			return true, nil
		}
	}
	return false, nil
}

func (ShortcutsRunner) Open(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "/usr/bin/open", path).Run()
}

// WriteShortcut writes the shortcut file to a fresh temporary directory, for
// Open. **The file name is the name it is added under**, so it is written as
// <name>.shortcut. It is left there: Shortcuts reads it after open returns.
func WriteShortcut(name string, data []byte) (string, error) {
	dir, err := os.MkdirTemp("", "enghi-shortcut-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, filepath.Base(name)+".shortcut")
	return path, os.WriteFile(path, data, 0o600)
}

// ErrNotInstalled is a sync without the shortcut.
var ErrNotInstalled = errors.New("the shortcut is not installed")

// Status is what the settings screen shows about the sync. Times are local
// ISO 8601; empty means never. Kept in memory: a restart syncs again anyway.
type Status struct {
	Supported   bool   `json:"supported"` // macOS
	Enabled     bool   `json:"enabled"`
	Shortcut    string `json:"shortcut"`
	LastRun     string `json:"last_run"`
	LastSuccess string `json:"last_success"`
	Count       int    `json:"count"`    // events stored by the last successful run
	Skipped     int    `json:"skipped"`  // lines of its output that could not be used
	Error       string `json:"error"`    // of the last run; empty when it succeeded
	Missing     bool   `json:"missing"`  // the last run failed because the shortcut is not installed
	Interval    string `json:"interval"` // between runs
}

// Syncer runs the shortcut and stores what it prints.
type Syncer struct {
	svc       *Service
	set       *settings.Service
	runner    Runner
	name      string
	interval  time.Duration
	supported bool
	// OnChange is called after a run that changed the stored events.
	OnChange func()

	run    sync.Mutex // one run at a time
	mu     sync.Mutex // guards status
	status Status
}

// NewSyncer makes a Syncer. Without a runner, or when not supported (not
// macOS), it never runs anything.
func NewSyncer(svc *Service, set *settings.Service, runner Runner, name string, interval time.Duration, supported bool) *Syncer {
	return &Syncer{svc: svc, set: set, runner: runner, name: name, interval: interval,
		supported: supported && runner != nil}
}

// Supported reports whether this machine can run the shortcut.
func (y *Syncer) Supported() bool { return y.supported }

// Name is the shortcut's name.
func (y *Syncer) Name() string { return y.name }

// Status is the current status.
func (y *Syncer) Status(ctx context.Context) Status {
	y.mu.Lock()
	st := y.status
	y.mu.Unlock()
	st.Supported, st.Shortcut, st.Interval = y.supported, y.name, y.interval.String()
	if set, err := y.set.Load(ctx); err == nil {
		st.Enabled = set.Calendar
	}
	return st
}

// Installed reports whether the shortcut is there; false when unsupported.
func (y *Syncer) Installed(ctx context.Context) (bool, error) {
	if !y.supported {
		return false, nil
	}
	return y.runner.Installed(ctx, y.name)
}

// Import hands the shortcut file to Shortcuts.
func (y *Syncer) Import(ctx context.Context, data []byte) error {
	if !y.supported {
		return errors.New("Shortcuts is only available on macOS")
	}
	path, err := WriteShortcut(y.name, data)
	if err != nil {
		return err
	}
	return y.runner.Open(ctx, path)
}

// Sync runs the shortcut once and replaces the window with what it printed.
// It does not look at the on/off setting; Daemon does.
func (y *Syncer) Sync(ctx context.Context) error {
	if !y.supported {
		return errors.New("Shortcuts is only available on macOS")
	}
	y.run.Lock()
	defer y.run.Unlock()

	now := time.Now()
	st := Status{LastRun: now.Format(time.RFC3339)}
	from, to := Window(now)
	out, err := y.runner.Run(ctx, y.name)
	if err != nil {
		// Tell a missing shortcut apart; the rest (a permission prompt that
		// was never answered, say) is Shortcuts' own message
		if ok, lerr := y.runner.Installed(ctx, y.name); lerr == nil && !ok {
			err = fmt.Errorf("%w: %q", ErrNotInstalled, y.name)
			st.Missing = true
		}
	}
	var changed bool
	if err == nil {
		// Empty output is a day with no events, not a failure: the shortcut
		// outputs nothing (and exits 0) when it found nothing
		evs, bad := ParseLines(bytes.NewReader(out))
		for _, b := range bad {
			log.Printf("calendar: skipped %v", b)
		}
		st.Skipped = len(bad)
		if changed, err = y.svc.Replace(ctx, SourceShortcuts, from, to, evs); err == nil {
			st.LastSuccess, st.Count = st.LastRun, len(dedupe(evs))
		}
	}
	y.mu.Lock()
	if err != nil {
		st.Error = err.Error()
		st.LastSuccess, st.Count = y.status.LastSuccess, y.status.Count
	}
	y.status = st
	y.mu.Unlock()
	if changed && y.OnChange != nil {
		y.OnChange()
	}
	return err
}

// dedupe drops repeats of one event, as the stored rows do.
func dedupe(evs []Event) []Event {
	seen := map[string]bool{}
	out := evs[:0:0]
	for _, e := range evs {
		if !seen[e.Key] {
			seen[e.Key] = true
			out = append(out, e)
		}
	}
	return out
}

// Daemon syncs at start-up and then every interval, while the setting is on.
// It returns when ctx is done.
func (y *Syncer) Daemon(ctx context.Context) {
	if !y.supported {
		return
	}
	tick := func() {
		set, err := y.set.Load(ctx)
		if err != nil || !set.Calendar {
			return
		}
		if err := y.Sync(ctx); err != nil && ctx.Err() == nil {
			log.Printf("warning: calendar sync failed: %v", err)
		}
	}
	tick()
	t := time.NewTicker(y.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
