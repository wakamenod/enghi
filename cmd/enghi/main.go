// Command enghi はローカル専用の Wiki + GTD サーバ。
//
// 使い方:
//
//	enghi                 常駐サーバを起動する(既定)
//	enghi serve
//	enghi export [--dir]  Markdown に全件エクスポート
//	enghi doctor          整合性検査(DESIGN 2.5)
//	enghi backup          DB のバックアップ(1日1回、常駐中にも自動で取る)
//	enghi install-agent   launchd の plist を書き出す
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wakamenod/enghi/internal/config"
	"github.com/wakamenod/enghi/internal/export"
	filestore "github.com/wakamenod/enghi/internal/files"
	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/web"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("enghi: ")

	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && args[0][0] != '-' {
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
	case "install-agent":
		err = cmdInstallAgent(args)
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
	fmt.Fprint(os.Stderr, `enghi — ローカル専用の Wiki + GTD

  enghi [serve]          常駐サーバを起動する
  enghi export [--dir D] Markdown に全件エクスポート
  enghi doctor           整合性検査
  enghi backup [--dir D] DB のバックアップを取る
  enghi files [--prune]  画像などの一覧。--prune で未参照のものを消す
  enghi install-agent    launchd の plist を書き出す

設定: `+config.Path()+`
`)
}

// openDB は設定を読み、DB を開く。
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

// openAll は本体 DB とファイル保管庫の両方を開く。
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
	configPath := fs.String("config", "", "設定ファイルのパス")
	port := fs.Int("port", 0, "ポート(設定ファイルより優先)")
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

	// **起動時にも doctor を実行し、異常があれば警告を出す**(DESIGN 8-10)。
	// 「正式名がちょうど1つ」は DB で表現できない不変条件なので、外から検査する経路が要る。
	if problems, err := store.Doctor(context.Background(), db); err != nil {
		log.Printf("警告: 整合性検査に失敗した: %v", err)
	} else if len(problems) > 0 {
		log.Printf("警告: 整合性の問題が %d 件ある。`enghi doctor` で詳細を確認すること", len(problems))
		for i, p := range problems {
			if i >= 5 {
				log.Printf("  ... 他 %d 件", len(problems)-5)
				break
			}
			log.Printf("  %s", p)
		}
	}

	srv, err := web.New(cfg, db, blobs)
	if err != nil {
		return err
	}

	// 常駐中は1日1回バックアップを取る。
	// 決まった時刻ではなく「その日のファイルが無ければ取る」で判断するので、
	// サーバが止まっていた日があっても次に起きたときに取り返せる。
	backupCtx, stopBackup := context.WithCancel(context.Background())
	defer stopBackup()
	if cfg.BackupOn() {
		go store.BackupDaemon(backupCtx, db, blobs, cfg.BackupDir, cfg.BackupKeep,
			func(b *store.Backup, err error) {
				if err != nil {
					log.Printf("警告: バックアップに失敗した: %v", err)
					return
				}
				msg := fmt.Sprintf("バックアップを取った: %s (%.1f MB)", b.Path, float64(b.Bytes)/(1<<20))
				if b.FilesPath != "" && !b.FilesSkip {
					msg += fmt.Sprintf(" / 画像 %.1f MB", float64(b.FilesBytes)/(1<<20))
				}
				log.Print(msg)
			})
	}

	// **0.0.0.0 には bind できないようにする**(設定で指定されても拒否する。DESIGN 4.4)。
	// config.validate() が先に弾くが、ここでも二重に守る。
	switch cfg.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return fmt.Errorf("host %q には bind しない。ループバックのみ", cfg.Host)
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// /api/events は SSE なので WriteTimeout は設定しない(切断されてしまう)
		IdleTimeout: 120 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("%s で待ち受けできない: %w", addr, err)
	}
	log.Printf("http://%s/ で待ち受け中 (db: %s)", addr, cfg.DBPath)

	// SIGTERM / SIGINT で落とす(launchd からの停止を含む)
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
	log.Print("停止した")
	return nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	configPath := fs.String("config", "", "設定ファイルのパス")
	// **--dir を許すのは CLI だけ。POST /api/export はパスを受け取らない**(DESIGN 7 / 4.4)。
	dir := fs.String("dir", "", "出力先(既定は設定の export_dir)")
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
	fmt.Printf("%s に記事 %d 件、画像 %d 件、あわせて %d ファイルを書き出した\n",
		res.Dir, res.Pages, res.Images, res.Files)
	return nil
}

func cmdBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	configPath := fs.String("config", "", "設定ファイルのパス")
	// --dir を許すのは CLI だけ。POST /api/backup はパスを受け取らない。
	dir := fs.String("dir", "", "出力先(既定は設定の backup_dir)")
	keep := fs.Int("keep", 0, "残す世代数(既定は設定の backup_keep)")
	list := fs.Bool("list", false, "現存するバックアップを一覧する")
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
			fmt.Printf("%s にバックアップはまだ無い\n", out)
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
			fmt.Printf("%s は前回から変わっていないので取り直していない\n", b.FilesPath)
		} else {
			fmt.Printf("%s (%.1f MB)\n", b.FilesPath, float64(b.FilesBytes)/(1<<20))
		}
	}
	if b.Removed > 0 {
		fmt.Printf("古いバックアップを %d 件消した(%d 世代を残す)\n", b.Removed, n)
	}
	return nil
}

func cmdFiles(args []string) error {
	fs := flag.NewFlagSet("files", flag.ExitOnError)
	configPath := fs.String("config", "", "設定ファイルのパス")
	prune := fs.Bool("prune", false, "どこからも参照されていないファイルを消す")
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
	fmt.Printf("%d 件 / %.1f MB (%s)\n", count, float64(bytes)/(1<<20), blobs.Path)

	unused, err := blobs.Unused(ctx, db.DB)
	if err != nil {
		return err
	}
	missing, err := blobs.Missing(ctx, db.DB)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		fmt.Printf("本文から参照されているが実体が無いもの: %d 件\n", len(missing))
		for _, h := range missing {
			fmt.Printf("  %s\n", h)
		}
	}
	if len(unused) == 0 {
		fmt.Println("未参照のファイルは無い")
		return nil
	}
	if !*prune {
		fmt.Printf("どこからも参照されていないもの: %d 件(--prune で消す)\n", len(unused))
		return nil
	}
	n, freed, err := blobs.Prune(ctx, db.DB)
	if err != nil {
		return err
	}
	fmt.Printf("%d 件消した(%.1f MB)\n", n, float64(freed)/(1<<20))
	return nil
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	configPath := fs.String("config", "", "設定ファイルのパス")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, db, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer db.Close()

	problems, err := store.Doctor(context.Background(), db)
	if err != nil {
		return err
	}
	if len(problems) == 0 {
		fmt.Println("問題なし")
		return nil
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	return fmt.Errorf("%d 件の問題が見つかった", len(problems))
}
