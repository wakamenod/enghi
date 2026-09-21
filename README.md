# enghi

ローカル専用の個人向け Wiki + GTD。常駐サーバとして動き、ブラウザから使う。

設計と判断の根拠は [docs/DESIGN.md](docs/DESIGN.md)、スキーマは [docs/schema.sql](docs/schema.sql)。
**実装は DESIGN.md に従うこと。特に 3 節(検索)は実測に基づく確定仕様である。**

## 状態

**第1段階(Wiki 単体)まで実装済み。** GTD は第2段階。

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

## キーボード操作

`/` 検索 · `g d`/`g w` 移動 · `e` 編集 · `j`/`k` リスト移動 · `Enter` 開く · `Esc` 解除

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

ブラウザを使う検証(スピナー、再接続、スクリーンショット)は Playwright で行う:

```sh
npx playwright install chromium webkit   # ブラウザ本体
```
