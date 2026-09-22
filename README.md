# enghi

ローカル専用の個人向け Wiki + GTD。常駐サーバとして動き、ブラウザから使う。

> **enghi** is a local-only personal wiki + GTD server for macOS and Linux.
> It runs as a background service and you use it from your browser — nothing
> leaves your machine. Instant full-text search (SQLite FTS5), `[[wikilinks]]`
> resolved by title, and export to plain Markdown at any time.

設計と判断の根拠は [docs/DESIGN.md](docs/DESIGN.md)、スキーマは [docs/schema.sql](docs/schema.sql)。
**実装は DESIGN.md に従うこと。特に 3 節(検索)は実測に基づく確定仕様である。**

## インストール

```sh
brew install wakamenod/tap/enghi
brew services start enghi     # macOS は launchd、Linux は systemd に登録される
```

http://127.0.0.1:7777/ を開く。使い方は `/guide`(GTD の入門と操作説明。日英)。

brew を使わない場合は [Releases](https://github.com/wakamenod/enghi/releases) の
tarball を展開し、`enghi install-agent -load` で常駐させる。

## 設定

`~/.config/enghi/config.toml`。無ければ既定値で動く。

```toml
port = 7777
db_path    = "~/.local/share/enghi/enghi.db"
export_dir = "~/.local/share/enghi/export"
revision_compact_minutes = 10

files_db_path  = "~/.local/share/enghi/enghi-files.db"   # 省略すると db_path に追従する
backup_dir     = "~/.local/share/enghi/backup"
backup_keep    = 7       # 残す世代数
backup_enabled = true
```

### 画面からの設定(`/settings`)

config.toml が「起動時に読む運用の設定」なのに対し、こちらは実行中に切り替える好み。
保存先は DB で、**行が無い = 既定値**。

| 項目 | 既定 | |
|---|---|---|
| Context | off | 行動を場所や道具で絞り込む(`@電話` `@自宅`)。Next Actions が長くなってきたら |
| Area | off | 完了しない責任範囲(経理・健康)で Project と行動をまとめる |

**on にすると画面に現れ、使い方ガイドの該当する節も現れる。** off に戻してもデータは消えない。
エクスポートとバックアップの実行もこの画面にある。

## コマンド

```
enghi [serve]          常駐サーバを起動する
enghi export [--dir D] Markdown に全件エクスポート(--dir は CLI のみ。API は設定値に固定)
enghi doctor [--fix]   整合性検査。--fix で NFD の混入を直す
enghi backup [--list]  DB のバックアップ(--dir で出力先)。常駐中は1日1回自動で取る
enghi files [--prune]  画像などの一覧。--prune で未参照のものを消す
enghi install-agent    常駐設定を書き出す。-load で登録まで行う
enghi version          バージョンを表示する
```

いずれも `--config` で設定ファイルのパスを指定できる。

## 常駐と復元

`install-agent` は macOS なら launchd の plist を `~/Library/LaunchAgents` に、
Linux なら systemd の user unit を `~/.config/systemd/user` に書く。
**Linux ではログアウト後も動かすために `loginctl enable-linger` が要る。**

バックアップは `VACUUM INTO` で取るので、WAL を取りこぼさない。戻すときは
サーバを止めてファイルを置き換えるだけでよい:

```sh
brew services stop enghi        # または launchctl bootout / systemctl --user stop
cp ~/.local/share/enghi/backup/enghi-2026-09-21.db ~/.local/share/enghi/enghi.db
rm -f ~/.local/share/enghi/enghi.db-wal ~/.local/share/enghi/enghi.db-shm
brew services start enghi
```

## 自宅のスマホから安全に使う

enghi は `127.0.0.1` にしか bind せず、`Host` ヘッダがループバックでなければ 403 を返す。
スマホから使うには、**前段に Caddy を置いて TLS とログインを担当させる**。
enghi はループバックのまま変えない。

```
iPhone ──https──▶ Caddy (LAN:443)  ──http──▶ enghi (127.0.0.1:7777)
                   ├ TLS 終端
                   └ パスワード確認
```

**`0.0.0.0` に bind して済ませないこと。** enghi に認証は無いので、同じ Wi-Fi にいる
全員が読み書きできる状態になる。

### 1. Caddy を入れ、パスワードのハッシュを作る

```sh
brew install caddy
caddy hash-password        # 対話で入力する。平文は設定に書かない
```

### 2. Caddyfile を書く

`$(brew --prefix)/etc/Caddyfile`。名前は `scutil --get LocalHostName` の値 + `.local`
(例: `junnomacbook-pro.local`)。

```
junnomacbook-pro.local {
    tls internal
    basic_auth {
        jun    $2a$14$...(caddy hash-password の出力)...
    }
    reverse_proxy 127.0.0.1:7777
}
```

```sh
brew services start caddy
```

### 3. enghi にその名前を許可させる

`~/.config/enghi/config.toml`:

```toml
allowed_hosts = ["junnomacbook-pro.local"]
```

書いた名前が `Host` ヘッダとして届いたときだけ受け付ける。ワイルドカードは使えない
(`*.local` は起動時にエラー)。省略すればループバックのみ。

書き換えたら enghi を再起動する。

### 4. iPhone に証明書を信頼させる

`tls internal` は Caddy が自作した認証局の証明書なので、そのままでは Safari が警告を出す。
ルート証明書(場所は `caddy trust` の出力か Caddy のデータディレクトリで確認する)を
iPhone に転送し、**設定 → 一般 → 情報 → 証明書信頼設定** で「完全に信頼」を有効にする。

警告を毎回無視しても閲覧はできるが、`wss://` が拒否されて live 更新(Emacs からの
focus 追従)が繋がらなくなることがある。

### 注意

- **`basic_auth` を省かないこと。** 省くと同じ LAN の全員が読み書きできる
- `basic_auth` は以前 `basicauth` という名前だった。入れた版の書式を確認すること
- Basic 認証はブラウザのネイティブなダイアログなので、1Password などからの自動入力は
  効かない。長いランダムなパスワードを初回に貼り付ければ、以後はブラウザが覚える
- **外出先からは使えない**(家の LAN の中だけ)。外からも使うなら Tailscale や
  Cloudflare Tunnel のような別の前段が要る。enghi 側は `allowed_hosts` に
  その名前を足すだけでよい
- Mac の IP が変わっても `.local` の名前は追従する。IP の固定は要らない

## 定期タスクの記法

org-mode のリピータ記法に準拠(`recurrence` 列にそのまま格納):

| 記法 | 意味 | 基準 |
|---|---|---|
| `+1d` `+2w` `+1m` `+1y` | 固定間隔 | 前回の**予定日** + 間隔。1回分だけ進める |
| `++1w` | 固定間隔、未来まで送る | 今日より後になるまで繰り返し加算する |
| `.+3d` | 完了日基準 | **完了した日** + 間隔 |
| `weekly:mon,thu` | 毎週の指定曜日 | 次に来る該当曜日 |
| `monthly:25` / `monthly:last` | 毎月 | 次の該当日 / 月末 |
| `yearly:04-01` | 毎年 | 次の該当日 |

完了(または skip)を契機に**次の1件だけ**を生成するので、同じ系列で開いている
インスタンスは常に高々1件。生成される次インスタンスは必ず `scheduled`。
存在しない日付(31日の無い月、閏日)はその月の最終日に丸める。

## キーボード操作(ブラウザ側)

`/` 検索 · `g d`/`g w`/`g i`/`g n`/`g p` 移動 · `c` クイックキャプチャ ·
`e` 編集 · `j`/`k` リスト移動 · `Enter` 開く · `Esc` 解除

## Emacs から使う

クライアントは別プロジェクト **[`../enghi.el`](../enghi.el)**。サーバ側の設定は要らない。
依存している API は以下だけで、**JSON で独立に成立させてある**:

```
GET    /api/search?q=&kind=&limit=
GET    /api/pages ; POST /api/pages
GET    /api/pages/:slug ; PUT /api/pages/:slug ; DELETE /api/pages/:slug
GET    /api/tasks ; POST /api/tasks ; PATCH /api/tasks/:id
POST   /api/tasks/:id/complete ; POST /api/tasks/:id/file
GET    /api/projects ; GET /api/projects/stalled ; GET /api/contexts
POST   /api/focus              開いているブラウザタブを遷移させる
GET    /api/status             接続確認
```

## 開発

**build tag `sqlite_fts5` と `CGO_ENABLED=1` が必須**(FTS5 と trigram tokenizer)。
どちらも欠けるとコンパイルエラーで止まる(`cmd/enghi/require_*.go`)。
cgo なので **GOOS を跨いだビルドはできない**。配布物は各 OS の runner で作る。

```sh
make build test vet
```

テストは DESIGN.md が「壊れやすい」と名指ししている箇所を中心に書いてある
(リネームの往復、検索のフレーズリテラル化と語境界、セキュリティ3層、
定期タスクの日付計算、全画面が最後まで描画されること)。
ブラウザを使う検証は Playwright: `npx playwright install chromium webkit`。

`docs/guide/` に書き足すときは、**見出しに `{#id}` で明示的なアンカーを必ず書くこと**
(自動生成 ID だと日英で食い違い、画面の「?」リンクが切れる)。

## リリース

タグを push すると各 OS の runner でビルドし、tarball と `SHA256SUMS` を付けて
GitHub Release を作る(`.github/workflows/release.yml`)。

```sh
git tag v0.1.0 && git push origin v0.1.0
```

そのあと `packaging/homebrew/enghi.rb` の `url` と `sha256` を更新し、
`wakamenod/homebrew-tap` の `Formula/enghi.rb` へ反映する。
