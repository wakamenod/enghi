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

// systemd の user unit。macOS の launchd plist(agent_darwin.go)に対応する。
//
// ログは journald が受けるのでファイルには落とさない
// (launchd 側が ~/Library/Logs に書くのは、launchd にその仕組みが無いため)。
const unitTemplate = `[Unit]
Description=enghi — ローカル専用の Wiki + GTD
After=network.target

[Service]
Type=simple
ExecStart=%s serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

// cmdInstallAgent は systemd の user unit を書き出す(DESIGN 8-11)。
// 常駐させ、常にブラウザから開ける状態を保つため。
func cmdInstallAgent(args []string) error {
	fs := flag.NewFlagSet("install-agent", flag.ExitOnError)
	label := fs.String("label", "enghi", "systemd の unit 名(.service は付けない)")
	load := fs.Bool("load", false, "書き出した後に systemctl --user enable --now まで実行する")
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
	fmt.Printf("書き出した: %s\n", unitPath)
	fmt.Printf("  実行ファイル: %s\n", exe)
	fmt.Printf("  ログ: journalctl --user -u %s\n", *label)
	fmt.Printf("  設定: %s\n", config.Path())

	// ログアウト後も常駐させるには linger が要る。
	// これが無いとセッション終了で落ちるので、常駐サーバとしては必ず案内する。
	fmt.Printf("\nログアウト後も常駐させるには(初回のみ):\n  loginctl enable-linger $(whoami)\n")

	if !*load {
		fmt.Printf("\n有効にするには:\n  systemctl --user daemon-reload\n  systemctl --user enable --now %s\n", *label)
		fmt.Printf("止めるには:\n  systemctl --user disable --now %s\n", *label)
		return nil
	}

	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("systemctl", "--user", "enable", "--now", *label).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl enable --now: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Println("systemd に登録した")
	return nil
}

// configHome は unit の置き場を決める。config パッケージと同じ XDG の規則に従う。
func configHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}
