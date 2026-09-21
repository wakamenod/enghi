# enghi

ローカル専用の個人向け Wiki + GTD。常駐サーバとして動き、ブラウザから使う。

設計と判断の根拠は [docs/DESIGN.md](docs/DESIGN.md)、スキーマは [docs/schema.sql](docs/schema.sql)。
**実装は DESIGN.md に従うこと。特に 3 節(検索)は実測に基づく確定仕様である。**

## 状態

**第3段階(Emacs 層)まで実装済み。**

| # | 項目 | |
|---|---|---|
| 1 | サーバ雛形 / 設定 / SQLite 初期化とマイグレーション | ✅ |
| 2 | pages / page_revisions / tags / page_tags / links と CRUD | ✅ |
| 3 | `[[wikilink]]` のパースと解決(未解決リンクの保持を含む) | ✅ |
| 4 | 検索(3 節の全体) | ✅ |
| 5 | 画面: 表示・編集・一覧・タグ別・検索・履歴 | ✅ |
| 6 | ダッシュボード下段(上段は空の枠) | ✅ |
| 7 | セキュリティ3層(Host / Origin+Sec-Fetch-Site / Content-Type) | ✅ |
| 8 | `/api/events` と `POST /api/focus`(指数バックオフ再接続付き) | ✅ |
| 9 | Markdown エクスポート | ✅ |
| 10 | `enghi doctor`(起動時にも実行) | ✅ |
| 11 | launchd plist の生成 | ✅ |
| 12 | 使い方ガイド(`/guide`。GTD 入門 + 操作説明、日英) | ✅ |

## ビルドと実行

**build tag `sqlite_fts5` は必須。** FTS5 と trigram tokenizer が要る(DESIGN 1)。
付け忘れた場合は起動時の検証が明示的なエラーで落とす。

```sh
make build          # bin/enghi
make test
./bin/enghi         # 既定は serve。http://127.0.0.1:7777/
```

設定は `~/.config/enghi/config.toml`(無ければ既定値で動く):

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

## 使い方ガイド

`/guide` に GTD の入門と enghi の操作説明を置いている(日本語 / 英語)。
本文は `docs/guide/<lang>/<topic>.md` で、バイナリに埋め込まれる。

- `gtd.md` — GTD そのものの入門。enghi を知らなくても読める
- `enghi.md` — 画面ごとの操作、状態の使い分け、定期タスクの記法、用語の対応表

**見出しには `{#id}` で明示的なアンカーを必ず書くこと。** 各画面の「?」リンクがここを指しており、
自動生成 ID に任せると日本語版と英語版で ID がずれて切れる。
アンカーの過不足と、画面から張ったリンクの飛び先は `internal/web/guide_web_test.go` が検査する。

## 文字の正規化

**保存も検索も NFC(合成形)に揃える。**

macOS は日本語を NFD(分解形)で渡してくる経路が多い。「ビ」が「ヒ」+ 濁点の
2文字になっている状態で、見た目は同じだがバイト列としては別物になる。
タイトルで引く Wiki でこれを放置すると、同じ名前のページが見つからなかったり、
同じ名前のページが2つ作れてしまう。

タイトル・別名・slug・タグ・検索クエリ・`[[...]]` の参照先を、すべて入口で NFC に揃える。

既に NFD で入っている行は `enghi doctor` が報告する。直すには:

```sh
./bin/enghi doctor --fix
```

## 画像

記事の編集画面に画像を貼り付ける(ドラッグでも可)と、その場で保管して
Markdown の記法が入る。`enghi files` で一覧と掃除ができる。

**画像は本体とは別の SQLite ファイル(`enghi-files.db`)に置く。**
記事もタスクも DB が正本という原則は変えないが、バイナリを同じファイルに混ぜると
毎日の `VACUUM INTO` が画像ごと全部コピーすることになり、バックアップの費用が
中身の量に比例して増えていく。分けておけば本体は数十 MB のままで済む。

- 内容でアドレスする(SHA-256)。同じ画像を何度貼っても実体は1つ
- 受け付けるのは PNG / JPEG / GIF / WebP / AVIF / PDF。1件 32 MB まで
- **SVG は受け付けない。**スクリプトを含められるうえ同一オリジンで配信するため、
  記事に貼った SVG から Cookie や DOM に触れる経路ができてしまう
- 配信時は `X-Content-Type-Options: nosniff` と `Content-Security-Policy` を付ける
- URL が内容で決まるので恒久的にキャッシュしてよい(`immutable`)

エクスポートすると `files/` に実体を書き出し、本文中の参照を相対パスに書き換える。
書き出したものだけで完結した Markdown になる。

```sh
./bin/enghi files            # 一覧と、未参照・リンク切れの件数
./bin/enghi files --prune    # どこからも参照されていないものを消す
```

## バックアップ

常駐中に1日1回、`VACUUM INTO` で一貫したスナップショットを取る。
**決まった時刻に実行するのではなく「その日のファイルが無ければ取る」で判断する**ので、
サーバが止まっていた日があっても次に起きたときに取り返せる。

単なるファイルコピーでは WAL の内容を取りこぼし、書き込みの途中を掴むと壊れた複製になる。
`VACUUM INTO` は読み取りトランザクションの中で書き出すので、その心配がない。

画像用 DB も一緒に控える。**ただし世代は持たない。**内容でアドレスしていて
追記しかされないため、古い世代を残しても意味が無い。前回から変わっていなければ
取り直さない。

```sh
./bin/enghi backup           # 手動で取る
./bin/enghi backup --list    # 一覧
```

戻すときは、サーバを止めてファイルを置き換えるだけでよい:

```sh
launchctl bootout gui/$(id -u)/dev.enghi.server
cp ~/.local/share/enghi/backup/enghi-2026-09-21.db ~/.local/share/enghi/enghi.db
rm -f ~/.local/share/enghi/enghi.db-wal ~/.local/share/enghi/enghi.db-shm
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.enghi.server.plist
```

常駐させる:

```sh
./bin/enghi install-agent          # plist を書き出す
./bin/enghi install-agent -load    # 書き出して launchctl bootstrap まで行う
```

その他:

```sh
./bin/enghi doctor                 # 整合性検査
./bin/enghi export --dir /path     # --dir を許すのは CLI だけ(API は設定値に固定)
```

## Markdown の扱い

**段落内の改行1つを、そのまま改行として描画します**(goldmark の `html.WithHardWraps`)。
CommonMark の既定では段落内の改行はスペースになり、日本語の本文では文の途中に
見えるスペースが入ってしまうためです。行末にスペース2つを置く記法は目に見えず、
エディタの行末空白削除で消えるので採っていません。

エクスポートした Markdown は原文をそのまま書き出すので、他所のレンダラで開くと
段落内の改行は失われます(1行に繋がります)。

## 実装上の決定

- **`/api/events` は WebSocket**(DESIGN 4.3 が第一に挙げている方式)。
  最初は依存を増やさないために SSE で実装したが、**SSE は「終わらない HTTP リクエスト」なので
  ブラウザが常に読み込み中とみなし、Safari ではタブのスピナーが回り続ける**
  (WebKit で実測: `readyState=complete` でも未完了リクエストが 1 本残る)。
  WebSocket は Upgrade 後に通常のリクエストではなくなるため、この問題が構造的に起きない
  (同じ実測で未完了リクエスト 0 を確認)。`github.com/coder/websocket` を1つだけ足している。
  **ブラウザ側の指数バックオフ再接続は `web/static/app.js` で明示的に実装**(DESIGN 8-8)。
  サーバを落として上げ直すと張り直すことを実ブラウザで確認済み。
- **スニペットの強調は制御文字で持ち回る。** `snippet()` が返すのは本文そのものなので、
  `<mark>` を SQL 側で直接埋めると本文中の HTML がそのまま解釈される経路ができる。
  `search.SnippetHTML` がエスケープ後に強調だけを復元する。JSON API は素のテキストを返す。
- **Content-Type の強制は POST にのみ常時適用する。** simple request になりうるのは
  GET / HEAD / POST だけで、PUT / PATCH / DELETE は必ず preflight を起こす。
  本文の無い DELETE にまで要求すると、防御を足さずに Emacs 層と curl を壊すだけになる
  (PUT / PATCH / DELETE は本文を伴うときだけ検査する)。

## Emacs から使う

クライアントは別プロジェクト **[`../enghi.el`](../enghi.el)** にある。設定方法はそちらの
README を参照すること。サーバ側は何も設定しなくてよい(Emacs 用の設定項目は無い)。

Emacs 層が依存しているのは以下の API だけで、**これらは JSON で独立に成立させてある**:

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

## キーボード操作(ブラウザ側)

`/` 検索 · `g d`/`g w`/`g i`/`g n`/`g p` 移動 · `c` クイックキャプチャ ·
`e` 編集 · `j`/`k` リスト移動 · `Enter` 開く · `Esc` 解除

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

`+1w`(ゴミ出し)と `.+1w`(シーツの洗濯)の違いは実用上とても重要なので両方実装している。
存在しない日付(31日の無い月、閏日)はその月の最終日に丸める。

**完了(または skip)を契機に次の1件だけを生成する。** 先回りして複数件を作らないため、
同じ系列で開いているインスタンスは常に高々1件になる。
生成される次インスタンスは必ず `scheduled` で、`next` では作らない
(`weekly:tue,fri` のゴミ出しが常時 Next Actions に居座るのを避けるため)。

## テスト

DESIGN.md が「壊れやすい」と名指ししている箇所を中心に書いてある:

- リネームの往復(元の名前に戻す = UPSERT が無いと必ず失敗する経路)
- 衝突時に降格が先に走らないこと(正式名ゼロのページを作らない)
- リンクが再保存で増殖しないこと / 削除時に未解決へ降格すること
- タグだけの変更で version は上がりリビジョンは作られないこと / 10 分圧縮
- 検索: フレーズリテラル化(`C++`, `a"b`)、2 文字経路、ASCII 語境界フィルタ、
  日本語 2 文字にフィルタを適用しないこと、別名の部分一致、タグ一致の優先
- セキュリティ3層、409 の2種の区別、`/api/export` が出力先を受け取らないこと
- focus チャネル: navigate の配信、切断時の解放、クロスオリジンからの WebSocket 接続の拒否
- 定期タスクの日付計算: 月末丸め、閏日、`+1w` と `.+1w` の違い、`++` の追いつき
- Next Actions のビュー条件(予定日が到来した `scheduled` が現れ、state は書き換わらない)
- 停滞プロジェクト検出、`filed` 状態、系列の「高々1件」の不変条件
- **全画面が最後まで描画されること** — テンプレートの実行時エラーは HTTP 200 のまま
  途中で切れた HTML を返すので、ステータスコードだけを見るテストでは捕まらない

ブラウザを使う検証(スピナー、再接続、スクリーンショット)は Playwright で行う:

```sh
npx playwright install chromium webkit   # ブラウザ本体
```

Emacs クライアント側のテストは [`../enghi.el`](../enghi.el) にある
(動いているサーバに対して実行する)。
