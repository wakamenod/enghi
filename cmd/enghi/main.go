// Command enghi is a local-only wiki and GTD server.
//
// Usage:
//
//	enghi                 start the resident server (default)
//	enghi serve
//	enghi export [--dir]  export everything as Markdown
//	enghi doctor          consistency checks (DESIGN 2.5)
//	enghi backup          back up the database (also taken daily while resident)
//	enghi rebuild-readings rebuild the title readings for romaji search
//	enghi install-agent   write the service definition (launchd / systemd)
//	enghi install-skill   write the Claude Code skill
//	enghi version         print the version
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/wakamenod/enghi/internal/calendar"
	"github.com/wakamenod/enghi/internal/config"
	"github.com/wakamenod/enghi/internal/export"
	filestore "github.com/wakamenod/enghi/internal/files"
	"github.com/wakamenod/enghi/internal/readings"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/web"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("enghi: ")

	cmd := "serve"
	args := os.Args[1:]
	// Anything not starting with '-' is a subcommand. --help and --version
	// conventionally arrive as flags, so pick those up as subcommands too;
	// otherwise they fall through to serve's flag parsing and print serve's
	// usage alone.
	if len(args) > 0 && (args[0][0] != '-' || isGlobalFlag(args[0])) {
		cmd, args = args[0], args[1:]
	}

	var err error
	switch cmd {
	case "serve":
		err = cmdServe(args)
	case "export":
		err = cmdExport(args)
	case "doctor":
		err = cmdDoctor(args)
	case "backup":
		err = cmdBackup(args)
	case "files":
		err = cmdFiles(args)
	case "rebuild-readings":
		err = cmdRebuildReadings(args)
	case "install-agent":
		err = cmdInstallAgent(args)
	case "install-skill":
		err = cmdInstallSkill(args)
	case "version", "-v", "--version":
		err = cmdVersion(args)
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `enghi - local-only wiki and GTD

  enghi [serve]          start the resident server
  enghi export [--dir D] export everything as Markdown
  enghi doctor [--fix]   consistency checks; --fix repairs text normalization
                         and title readings
  enghi backup [--dir D] back up the database
  enghi files [--prune]  list stored images; --prune removes unreferenced ones
  enghi rebuild-readings rebuild title readings for romaji search (the server
                         does this on its own)
  enghi install-agent    write the service definition (launchd / systemd)
  enghi install-skill    write the Claude Code skill to ~/.claude/skills/enghi
  enghi version          print the version

config: `+config.Path()+`
`)
}

// openDB loads the configuration and opens the database.
func openDB(configPath string) (config.Config, *store.DB, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return cfg, nil, err
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return cfg, nil, err
	}
	return cfg, db, nil
}

// openAll opens both the main database and the file store.
func openAll(configPath string) (config.Config, *store.DB, *filestore.Store, error) {
	cfg, db, err := openDB(configPath)
	if err != nil {
		return cfg, nil, nil, err
	}
	fs, err := filestore.Open(cfg.FilesDBPath)
	if err != nil {
		db.Close()
		return cfg, nil, nil, err
	}
	return cfg, db, fs, nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to the configuration file")
	port := fs.Int("port", 0, "port (overrides the configuration file)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, db, blobs, err := openAll(*configPath)
	if err != nil {
		return err
	}
	defer db.Close()
	defer blobs.Close()
	if *port != 0 {
		cfg.Port = *port
	}

	// **Run doctor at start-up too and warn about anything it finds**
	// (DESIGN 8-10). "Exactly one canonical title" is an invariant the database
	// cannot express, so it needs a path that checks it from outside.
	if problems, err := store.Doctor(context.Background(), db); err != nil {
		log.Printf("warning: consistency check failed: %v", err)
	} else if problems = withoutReadings(problems); len(problems) > 0 {
		log.Printf("warning: %d consistency problem(s); run `enghi doctor` for details", len(problems))
		for i, p := range problems {
			if i >= 5 {
				log.Printf("  ... and %d more", len(problems)-5)
				break
			}
			log.Printf("  %s", p)
		}
	}

	srv, err := web.New(cfg, db, blobs)
	if err != nil {
		return err
	}
	srv.Version = versionString()

	// Title readings for romaji search (DESIGN 3.8): rebuilt once now and a
	// moment after writes, never in the write itself and never blocking the
	// start-up. The rebuild runs in a child process: the dictionary takes about
	// 93 MB that a Go process never gives back, and the child's goes away when
	// it exits.
	//
	// The child is this very binary: the path is resolved now, and the child
	// refuses to run as another version. An upgrade in place would otherwise
	// start a newer enghi, whose migrations would run under this server.
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	readCtx, stopReadings := context.WithCancel(context.Background())
	defer stopReadings()
	worker := readings.NewWorker(db, func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, exe, "rebuild-readings",
			"-db", cfg.DBPath, "-server-version", versionString())
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		return cmd.Run()
	})
	srv.OnWrite = worker.Kick
	go worker.Loop(readCtx)

	// Take a backup once a day while resident.
	// The trigger is "there is no file for today" rather than a fixed time, so a
	// day when the server was down is caught up the next time it runs.
	backupCtx, stopBackup := context.WithCancel(context.Background())
	defer stopBackup()
	if cfg.BackupOn() {
		go store.BackupDaemon(backupCtx, db, blobs, cfg.BackupDir, cfg.BackupKeep,
			func(b *store.Backup, err error) {
				if err != nil {
					log.Printf("warning: backup failed: %v", err)
					return
				}
				msg := fmt.Sprintf("backed up: %s (%.1f MB)", b.Path, float64(b.Bytes)/(1<<20))
				if b.FilesPath != "" && !b.FilesSkip {
					msg += fmt.Sprintf(" / images %.1f MB", float64(b.FilesBytes)/(1<<20))
				}
				log.Print(msg)
			})
	}

	// Calendar events: run the Shortcuts shortcut at start-up and every
	// calendar_sync_interval, while it is on in the settings. macOS only.
	calCtx, stopCal := context.WithCancel(context.Background())
	defer stopCal()
	go srv.UseShortcuts(calendar.ShortcutsRunner{}, runtime.GOOS == "darwin").Daemon(calCtx)

	// **Never bind to 0.0.0.0**, even if the configuration asks for it
	// (DESIGN 4.4). config.validate() rejects it first; this is the second guard.
	switch cfg.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return fmt.Errorf("refusing to bind to host %q: loopback only", cfg.Host)
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// /api/events is SSE, so no WriteTimeout: it would cut the stream off
		IdleTimeout: 120 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", addr, err)
	}
	log.Printf("listening on http://%s/ (db: %s)", addr, cfg.DBPath)

	// Shut down on SIGTERM / SIGINT (including a stop from launchd)
	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
		close(idle)
	}()

	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	<-idle
	log.Print("stopped")
	return nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	configPath := fs.String("config", "", "path to the configuration file")
	// **--dir is a CLI-only option. POST /api/export takes no path**
	// (DESIGN 7 / 4.4).
	dir := fs.String("dir", "", "output directory (defaults to export_dir from the config)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, db, blobs, err := openAll(*configPath)
	if err != nil {
		return err
	}
	defer db.Close()
	defer blobs.Close()

	out := cfg.ExportDir
	if *dir != "" {
		out = *dir
	}
	res, err := export.Run(context.Background(), db, blobs, out)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d article(s) and %d image(s) as %d file(s) to %s\n",
		res.Pages, res.Images, res.Files, res.Dir)
	return nil
}

func cmdBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	configPath := fs.String("config", "", "path to the configuration file")
	// --dir is a CLI-only option. POST /api/backup takes no path.
	dir := fs.String("dir", "", "output directory (defaults to backup_dir from the config)")
	keep := fs.Int("keep", 0, "generations to keep (defaults to backup_keep from the config)")
	list := fs.Bool("list", false, "list existing backups")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, db, blobs, err := openAll(*configPath)
	if err != nil {
		return err
	}
	defer db.Close()
	defer blobs.Close()

	out := cfg.BackupDir
	if *dir != "" {
		out = *dir
	}
	n := cfg.BackupKeep
	if *keep > 0 {
		n = *keep
	}

	if *list {
		backups, err := store.Backups(out)
		if err != nil {
			return err
		}
		if len(backups) == 0 {
			fmt.Printf("no backups in %s yet\n", out)
			return nil
		}
		for _, b := range backups {
			fmt.Printf("%s  %6.1f MB  %s\n",
				b.CreatedAt, float64(b.Bytes)/(1<<20), b.Path)
		}
		return nil
	}

	b, err := store.RunBackup(context.Background(), db, blobs, out, n)
	if err != nil {
		return err
	}
	fmt.Printf("%s (%.1f MB)\n", b.Path, float64(b.Bytes)/(1<<20))
	if b.FilesPath != "" {
		if b.FilesSkip {
			fmt.Printf("%s is unchanged since the last backup, so it was not retaken\n", b.FilesPath)
		} else {
			fmt.Printf("%s (%.1f MB)\n", b.FilesPath, float64(b.FilesBytes)/(1<<20))
		}
	}
	if b.Removed > 0 {
		fmt.Printf("removed %d old backup(s), keeping %d generation(s)\n", b.Removed, n)
	}
	return nil
}

func cmdFiles(args []string) error {
	fs := flag.NewFlagSet("files", flag.ExitOnError)
	configPath := fs.String("config", "", "path to the configuration file")
	prune := fs.Bool("prune", false, "delete files that nothing references")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, db, blobs, err := openAll(*configPath)
	if err != nil {
		return err
	}
	defer db.Close()
	defer blobs.Close()
	ctx := context.Background()

	count, bytes, err := blobs.Stats(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%d file(s) / %.1f MB (%s)\n", count, float64(bytes)/(1<<20), blobs.Path)

	unused, err := blobs.Unused(ctx, db.DB)
	if err != nil {
		return err
	}
	missing, err := blobs.Missing(ctx, db.DB)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		fmt.Printf("referenced from article bodies but missing: %d\n", len(missing))
		for _, h := range missing {
			fmt.Printf("  %s\n", h)
		}
	}
	if len(unused) == 0 {
		fmt.Println("no unreferenced files")
		return nil
	}
	if !*prune {
		fmt.Printf("referenced by nothing: %d (use --prune to delete)\n", len(unused))
		return nil
	}
	n, freed, err := blobs.Prune(ctx, db.DB)
	if err != nil {
		return err
	}
	fmt.Printf("deleted %d file(s) (%.1f MB)\n", n, float64(freed)/(1<<20))
	return nil
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	configPath := fs.String("config", "", "path to the configuration file")
	fix := fs.Bool("fix", false, "repair what can be repaired (text normalization, title readings)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, db, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if *fix {
		n, err := store.FixNormalization(context.Background(), db)
		if err != nil {
			return err
		}
		fmt.Printf("normalized %d row(s)\n", n)
		if n, err = readings.Rebuild(context.Background(), db); err != nil {
			return err
		}
		fmt.Printf("rebuilt %d title reading(s)\n", n)
	}

	problems, err := store.Doctor(context.Background(), db)
	if err != nil {
		return err
	}
	if len(problems) == 0 {
		fmt.Println("no problems found")
		return nil
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	return fmt.Errorf("found %d problem(s)", len(problems))
}

// withoutReadings drops the missing and stale title readings from the start-up
// report: the server rebuilds them itself a moment later, and right after the
// upgrade that adds them every title would be reported.
func withoutReadings(ps []store.Problem) []store.Problem {
	out := ps[:0]
	for _, p := range ps {
		if !strings.HasPrefix(p.Kind, "readings_") {
			out = append(out, p)
		}
	}
	return out
}

// cmdRebuildReadings makes the missing and stale title readings (DESIGN 3.8).
// The server runs it as a child process with -db, so the dictionary's memory
// leaves with the process.
func cmdRebuildReadings(args []string) error {
	fs := flag.NewFlagSet("rebuild-readings", flag.ExitOnError)
	configPath := fs.String("config", "", "path to the configuration file")
	dbPath := fs.String("db", "", "database (overrides db_path from the config)")
	serverVersion := fs.String("server-version", "", "refuse to run unless this is the version (set by the server)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *serverVersion != "" && *serverVersion != versionString() {
		return fmt.Errorf("enghi was replaced by %s while %s is running; restart the server to rebuild title readings",
			versionString(), *serverVersion)
	}
	path := *dbPath
	if path == "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		path = cfg.DBPath
	}
	db, err := store.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	start := time.Now()
	n, err := readings.Rebuild(context.Background(), db)
	if err != nil {
		return err
	}
	log.Printf("title readings: rebuilt %d row(s) in %s", n, time.Since(start).Round(time.Millisecond))
	return nil
}
