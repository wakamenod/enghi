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
db_path  = "~/.local/share/enghi/enghi.db"
export_dir = "~/.local/share/enghi/export"
revision_compact_minutes = 10
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

```elisp
(add-to-list 'load-path "~/Projects/SideProjects/enghi/elisp")
(require 'enghi)
(enghi-setup)          ; C-c n にキーマップを置く
```

| キー | コマンド | |
|---|---|---|
| `C-c n s` | `enghi-search-command` | 横断検索(consult があれば打鍵ごと) |
| `C-c n f` | `enghi-find-page` | 記事を選んでバッファで開く |
| `C-c n n` | `enghi-new-page` | 新しい記事を作って開く |
| `C-c n c` | `enghi-capture` | どこからでも1行を Inbox へ |
| `C-c n a` | `enghi-agenda` | GTD の一覧 |
| `C-c n o` | `enghi-focus-page` | **開いているブラウザタブをその記事へ飛ばす** |
| `C-c n b` | `enghi-open-in-browser` | ブラウザで開く |

記事バッファ(`enghi-page-mode`)では `C-c C-c` 保存 / `C-c C-r` 改題 /
`C-c C-t` タグ / `C-c C-l` リンク挿入 / `C-c C-o` ブラウザで開く。

agenda バッファでは `n` 次の行動 / `w` 他者待ち / `s` 日付 / `d` 完了 /
`k` 今回は飛ばす / `f` 資料にする(記事化) / `p` プロジェクト / `C` コンテキスト。

表示関数は差し替えられる:

```elisp
(setq enghi-browse-function #'xwidget-webkit-browse-url)  ; 既定は #'browse-url
```

**保存時の 409 は2種類あり、対応が違う**(DESIGN.md 4.2):

- `version_conflict` … `ediff` でサーバ側と手元の差分を出す。**手元の入力は捨てない**
- `title_conflict` … 衝突相手を示して別のタイトルを聞き直す。**本文は保持したまま**

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

elisp のテストは動いているサーバに対して実行する:

```sh
./bin/enghi serve --config /tmp/enghi-test.toml &   # ポート 7799
make test-elisp
```

内容: 日本語の往復、楽観ロックの競合で**入力が消えないこと**、
`title_conflict` が衝突相手を返すこと、capture、agenda の状態変更、
consult の候補がサーバの順序をそのまま使うこと(Elisp 側で並べ替えない)。
