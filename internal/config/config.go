// Package config はユーザ設定の読み込みと既定値の解決を行う。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config は ~/.config/enghi/config.toml の内容。
type Config struct {
	Port int    `toml:"port"`
	Host string `toml:"host"` // 127.0.0.1 固定。0.0.0.0 は拒否する(DESIGN 4.4)
	// AllowedHosts: Host ヘッダで追加で許す名前。前段にリバースプロキシを置いて
	// 他の端末から使うときだけ設定する。**空なら従来どおりループバックのみ**。
	//
	// bind は 127.0.0.1 のまま変えない。プロキシが受けて loopback へ渡すので、
	// ここで増えるのは「どの名前で呼ばれたリクエストを受け付けるか」だけである。
	//
	// **ワイルドカードは受け付けない。** Host 検証は DNS rebinding に対する
	// 唯一有効な防御なので(DESIGN 4.4)、緩めるのは書いた名前1つずつに限る。
	//
	// **これを設定した時点で「他マシンから到達できる」状態になるため、
	// 前段での本物の認証が必須になる**(DESIGN 4.4)。enghi は認証を持たない。
	AllowedHosts []string `toml:"allowed_hosts"`
	DBPath       string   `toml:"db_path"`
	// FilesDBPath: 画像などのバイナリ。**本体とは別ファイルにする**(バックアップを軽く保つため)
	FilesDBPath string `toml:"files_db_path"`
	ExportDir   string `toml:"export_dir"`
	// RevisionCompactMinutes: 直前のリビジョンがこの分数以内なら上書きする(DESIGN 4.2)
	RevisionCompactMinutes int `toml:"revision_compact_minutes"`

	// バックアップ。常駐中に1日1回、VACUUM INTO で取る。
	BackupDir     string `toml:"backup_dir"`
	BackupKeep    int    `toml:"backup_keep"`    // 残す世代数
	BackupEnabled *bool  `toml:"backup_enabled"` // 既定は有効
}

// BackupOn はバックアップが有効かを返す(未設定なら有効)。
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

// Path は設定ファイルの既定位置を返す。
func Path() string { return filepath.Join(configHome(), "enghi", "config.toml") }

// filesPathFor は本体 DB のパスから、画像用 DB のパスを決める。
// **db_path を変えたら画像もそれに追従する**(別の場所に取り残さない)。
func filesPathFor(dbPath string) string {
	return strings.TrimSuffix(dbPath, ".db") + "-files.db"
}

// Default は設定ファイルが無いときの既定値。
func Default() Config { return withDefaults(Config{}) }

// withDefaults は未設定の項目を既定値で埋める。
//
// **設定ファイルを読む前に既定値を入れてはいけない。**
// 入れてしまうと「未設定かどうか」が判別できなくなり、
// db_path に追従させたい files_db_path のような項目が既定値のまま固まる。
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

// Load は設定ファイルを読み、既定値で埋めて返す。ファイルが無いのはエラーではない。
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
		return withDefaults(c), fmt.Errorf("設定ファイル %s: %w", path, err)
	}
	c = withDefaults(c)
	return c, c.validate()
}

// validate はローカル専用の前提を守る(DESIGN 4.4)。
func (c Config) validate() error {
	switch c.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return fmt.Errorf("host %q は許可されない。enghi はループバックにのみ bind する(DESIGN 4.4)", c.Host)
	}
	for _, h := range c.AllowedHosts {
		// 手で書く設定なので前後の空白は許す。中の空白は名前ではないので弾く。
		h = strings.TrimSpace(h)
		if h == "" {
			return fmt.Errorf("allowed_hosts に空の項目がある")
		}
		if strings.ContainsAny(h, "*?/ ") {
			return fmt.Errorf("allowed_hosts %q: ワイルドカードや区切りは使えない。名前を1つずつ書くこと(DESIGN 4.4)", h)
		}
	}
	return nil
}

// NormalizedAllowedHosts は allowed_hosts を比較しやすい形にする。
// ホスト名は大小を区別しないので小文字に揃え、ポートが書かれていれば落とす。
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
