// Command enghi はローカル専用の Wiki + GTD サーバ。
//
// 使い方:
//
//	enghi                 常駐サーバを起動する(既定)
//	enghi serve
//	enghi export [--dir]  Markdown に全件エクスポート
//	enghi doctor          整合性検査(DESIGN 2.5)
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
  enghi install-agent    launchd の plist を書き出す

設定: `+config.Path()+`
`)
}

// openDB は設定を読み、DB を開き、起動時の整合性検査を行う。
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

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "設定ファイルのパス")
	port := fs.Int("port", 0, "ポート(設定ファイルより優先)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, db, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer db.Close()
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

	srv, err := web.New(cfg, db)
	if err != nil {
		return err
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
	cfg, db, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer db.Close()

	out := cfg.ExportDir
	if *dir != "" {
		out = *dir
	}
	res, err := export.Run(context.Background(), db, out)
	if err != nil {
		return err
	}
	fmt.Printf("%s に %d 件の記事を含む %d ファイルを書き出した\n", res.Dir, res.Pages, res.Files)
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
