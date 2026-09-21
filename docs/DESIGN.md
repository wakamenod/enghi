# enghi — 設計方針 / 実装指示書

ローカル専用の個人向け Wiki + GTD システム。常駐サーバとして動き、ブラウザから使う。
将来的に Emacs から操作できるようにする。

このドキュメントは実装担当への引き渡し用。**第1段階(Wiki)から順に作ること。**

---

## 0. 前提と原則

- **完全ローカル**。ネットワークには公開しない。`127.0.0.1` のみに bind する。
  - **ただし「localhost に bind したから安全」は誤りである。必ず 4.4 節の保護を実装すること。**
- **常駐**。macOS の `launchd` で自動起動し、常にブラウザから開ける状態を保つ。
- **DB が正本**。記事本文もタスクも SQLite に格納する。ファイルは正本ではない。
  - 代償を消すため、**全件を Markdown にエクスポートする機能を第1段階から実装する**(後述)。
- **検索が最優先の非機能要件**。数万件規模で、打鍵ごとのインクリメンタル検索が体感ゼロ遅延であること。
- **Wiki と GTD は相互に独立**。
  - GTD を一切使わなくても Wiki は完全に機能する(ダッシュボードの GTD 領域は空になる)。
  - Wiki ページは、自分が GTD から参照されていることを知らない。ページ側スキーマに GTD 由来の列を持たせない。
- **UI はシックで落ち着いたトーン**。装飾を足さない。余白と階調で構造を示す。

### なぜ DB が正本か(判断の根拠 / 覆さないこと)

org-roam が遅い原因は SQLite ではなく、(1) Elisp での結果変換、(2) 外部 SQLite プロセスとの IPC、
(3) 保存のたびの全体再クロール、である。「ファイルが正 + DB はキャッシュ」構成は、同期・再インデックス・
競合検出という恒久的な複雑さを背負う。本システムは検索とインデックスを Emacs の外(サーバ)に置くため、
この問題は構造的に発生しない。DB を単一の真実の源とし、トランザクション・履歴・参照整合性を得る。

---

## 1. 技術スタック

| 層 | 選定 | 理由 |
|---|---|---|
| サーバ | **Go**(単一バイナリ) | 常駐・起動高速・配布が1ファイル。ランタイム依存なし |
| DB | **SQLite + FTS5** | 組み込み、十分高速、全文検索が同一トランザクションに乗る |
| ドライバ | `mattn/go-sqlite3`(build tag `sqlite_fts5`)または `modernc.org/sqlite` | **FTS5 と trigram tokenizer が有効なことをビルド時に必ず検証すること** |
| ルーティング | 標準 `net/http` (Go 1.22+ の `ServeMux` パターン) | 依存を増やさない |
| テンプレート | `html/template`(サーバサイドレンダリング) | 初期表示が速い。SPA にしない |
| 対話性 | **htmx** + 少量の vanilla JS | 検索のインクリメンタル更新、タスクの状態変更に十分 |
| Markdown | `yuin/goldmark`(+ GFM, 見出し ID) | 拡張しやすい |
| エディタ | 第1段階は `<textarea>` + プレビュー | 本命の編集環境は Emacs。ブラウザ側に凝らない |

**ビルド時の必須検証**: 起動時に `SELECT sqlite_version()` と
`CREATE VIRTUAL TABLE temp.t USING fts5(x, tokenize='trigram')` を試行し、失敗したら明示的なエラーで落とすこと。
trigram tokenizer は SQLite 3.34 以降が必要。

### ディレクトリ構成

```
enghi/
  cmd/enghi/main.go
  internal/
    store/        # SQLite アクセス、マイグレーション、クエリ
    wiki/         # ページ、タグ、リンク
    gtd/          # area, project, task, context, review
    search/       # 横断検索
    web/          # HTTP ハンドラ、テンプレート、静的ファイル
    export/       # Markdown エクスポート
  migrations/     # 0001_init.sql ...(埋め込み。golang-migrate は使わず自前の単純な版管理で可)
  docs/
  web/static/
  web/templates/
```

データの実体は `~/.local/share/enghi/enghi.db`(`$XDG_DATA_HOME` を尊重、なければ上記)。
設定は `~/.config/enghi/config.toml`(port, db path, export dir)。

---

## 2. データモデル

完全な DDL は `docs/schema.sql` を参照。ここでは意図を述べる。

### 2.1 Wiki 側

- `pages` — 記事。`slug`(URL用、一意)、`title`、`body`(Markdown 原文)、`version`(楽観ロック用の整数)。
  - GTD 由来の列は**持たせない**。
  - **`title` は一意。** `[[...]]` の解決がタイトル照合である以上、同名ページが2件あると
    リンク先が先着順で決まり、しかも誰も気づかない。実効的な一意性は `page_titles` が担保するが、
    `pages.title` にも `UNIQUE` を張って二重に守る。
  - `slug` は**小文字に正規化して保存する**。macOS の APFS は大小を区別しないため、
    `Foo` と `foo` を別ページにするとエクスポート時にファイルが衝突する。UNIQUE は `COLLATE NOCASE`。
- `page_titles` — **ページのタイトル名前空間そのもの。正式名と別名の両方をここに入れる**
  (`title` PRIMARY KEY, `page_id`, `is_canonical`)。
  タイトル変更のたびに旧タイトルが別名として残る。MediaWiki のリダイレクトに相当する。詳細は 2.5 節。
- `page_revisions` — 本文/タイトルが変わったときの全文スナップショット。
  **タグだけの変更では作らない。短時間の連続保存は圧縮する**(規則は 4.2 節)。
- `tags` / `page_tags` — タグ。階層タグは持たない(`親/子` のような命名規約で代用可能にしておく)。
- `links` — **Wiki 内リンクだけでなく、システム全体の汎用リンクテーブル**。
  - `(src_kind, src_id)` → `(dst_kind, dst_id)`。kind は `page` / `project` / `task` / `area`。
  - `[[まだ無い記事]]` を書けるよう、**未解決リンク**を表現できること: `dst_id IS NULL` かつ `dst_title` に生の文字列を保持。
    ページ作成時に、同一タイトルの未解決リンクを解決する。
  - バックリンク表示はこのテーブルの逆引き1本で全種別に対応できる。
  - **`UNIQUE(src_kind, src_id, dst_kind, dst_title)` を張る。** かつ保存時は
    「`(src_kind, src_id)` の行を全削除 → 本文を再パースして再挿入」を**同一トランザクションで**行う。
    これをしないと同じページを保存するたびにリンク行が増殖する。
  - **`dst_id` はポリモーフィックなので外部キーを張れない。ライフサイクルはアプリ側で明示的に管理する:**
    - ページ削除時、**そこを指す行は削除せず `dst_id = NULL` に戻して未解決リンクに落とす。**
      削除したページが「未解決リンク」一覧に現れて書き直しを促す、という Wiki として望ましい挙動になる。
    - ページ削除時、**そのページ発の行(`src`)は削除する。**
    - project / task / area の削除時も同様に扱う。

### 2.2 GTD 側

GTD の Horizons のうち、**20,000ft(Areas of Responsibility)までを実装する**。
30,000ft 以上(Goals / Vision / Purpose)は**今回は作らない**。
将来のための予約列は置かない — SQLite は `ALTER TABLE ADD COLUMN` が安価なので、
使わない列を今から抱える意味がない。必要になってから足す。

- `areas` — 責任範囲。「経理」「健康」「チームの採用」など。**完了しない**。`note_page_id` で Wiki ページを1枚持てる。
- `projects` — **GTD の中核**。「1年以内に完了でき、2つ以上の行動ステップを要する、望ましい結果」。
  - `title` … 短い識別名(例:「オフィス移転」)
  - `outcome` … **完了状態の記述**(例:「新オフィスに移転完了し業務が再開している」)。GTD の作法として必須。任意入力だが UI で促す。
  - `status` … `active` / `someday` / `done` / `dropped`
  - `area_id` … nullable
  - `note_page_id` … nullable。**Project Support Material**(GTD の用語)にあたる Wiki ページへの特権的な1枠。
    それ以外の関連記事は汎用 `links` で繋ぐ。
  - `review_on` … 再検討日('YYYY-MM-DD')。**`status='someday'` にしたプロジェクトが二度と浮上しない**
    のを防ぐための tickler。`tasks.scheduled_on` の project 版。
    **日付が到来したものをダッシュボードに出すこと。** これが無いと Someday は事実上のゴミ箱になる。
- `contexts` — `@電話` `@オフィス` `@自宅` `@メール` など。
- `tasks` — 行動。**GTD の Next Action リストは「今すぐ物理的に実行できる単一行動」だけを載せる**という原則を守る。
  - `state` enum:
    | 値 | 意味 |
    |---|---|
    | `inbox` | 未処理。clarify 前。`project_id`/`context_id` は NULL でよい |
    | `next` | 次の行動。コンテキスト別リストに出る |
    | `later` | プロジェクトに属する後続行動。まだ次ではない。Next リストには出さない |
    | `waiting` | 他者待ち。`waiting_for` と `delegated_at` を伴う |
    | `scheduled` | 特定日付に紐づく。`scheduled_on` 必須。その日まで通常リストに出さない(tickler) |
    | `someday` | いつかやる/たぶんやる |
    | `filed` | **資料化済み。** Inbox の項目が「行動ではなく参照資料」と判断され、Wiki ページになった状態。
      GTD 的には完了でも破棄でもないため、独立した状態として持つ。生成した Wiki ページへは `links` で繋ぐ |
    | `done` | 完了。`completed_at` |
    | `dropped` | 破棄 |
  - その他: `deadline_on`, `energy`(`low`/`mid`/`high`), `time_estimate`(分), `priority`, `note`, `sort_order`,
    `recurrence` / `series_id` / `recurrence_ends_on` … **第1版で自動展開まで実装する**(2.6 節)
  - `area_id` は project を経由せず直接タスクに紐づけることも許す(プロジェクト化するほどでない単発行動のため)。

### 2.3 Weekly Review

- `reviews` — 1回のレビューで1行。`started_at`, `completed_at`, `checklist`(JSON), `note`。
- **ウィザードは作らない**。チェックリスト付きの画面を1枚用意し、**その画面に判断材料をすべて並べる**。
  別画面に移動しないと確認できない項目があると、レビューが続かなくなる。

#### 標準チェックリスト(`checklist` JSON のキーはこれに対応させる)

GTD の標準的な週次レビュー項目に、画面に出すデータを紐づけたもの。**実装者が項目を思いつきで決めないこと。**

| キー | チェック項目 | 同じ画面に出すデータ |
|---|---|---|
| `collect_loose_papers` | 散らばった紙を集める | (物理作業。データなし) |
| `inbox_zero` | Inbox を空にする | Inbox 一覧(その場で clarify できること) |
| `empty_head` | 頭の中を空にする | クイックキャプチャ欄 |
| `review_next_actions` | Next Actions を見直す | コンテキスト別 Next 一覧 |
| `review_past_calendar` | 先週のカレンダーを振り返る | 先週完了したタスク(`completed_at`) |
| `review_upcoming_calendar` | 今後の予定を確認する | 今後2週間の `scheduled_on` / `deadline_on` |
| `review_waiting_for` | Waiting For を見直す | `state='waiting'` 一覧。`delegated_at` からの経過日数付き |
| `review_projects` | プロジェクトリストを見直す | **停滞プロジェクト検出の結果(2.4)** |
| `review_someday` | Someday/Maybe を見直す | **`review_on` が到来した someday プロジェクト** |
| `review_recurring` | 定期タスクを棚卸しする | 定期タスク系列の一覧(2.6) |

`review_projects` と `review_someday` がこのシステムの存在理由に直結する2項目。
**他を削ってもこの2つは削らないこと。**

### 2.4 最重要クエリ

**「Next Action が1つも無いアクティブなプロジェクト」の検出。これがシステムの価値の半分を担う。**

```sql
SELECT p.* FROM projects p
WHERE p.status = 'active'
  AND NOT EXISTS (
    SELECT 1 FROM tasks t
    WHERE t.project_id = p.id AND t.state IN ('next','waiting','scheduled')
  );
```

ダッシュボードと Weekly Review 画面の両方に必ず出すこと。

---

### 2.5 タイトルの名前空間と `[[旧タイトル]]`(方針決定済み)

**本文は書き換えない。`page_titles` で解決する。**

まず、**「本文は書き換えず、表示時に `links.dst_id` を辿って現タイトルへ解決する」方式は成立しない。**
リンクの解決は保存時のパースで行われるため、`[[旧タイトル]]` を含むページが次に保存された時点で、
パーサはタイトルを解決できず `dst_id = NULL` の未解決リンクに落としてしまう。

#### `page_titles` — 名前空間を1つのテーブルで表現する

正式タイトルと別名を**同じテーブルに入れ、`title` を PRIMARY KEY にする**。

```sql
CREATE TABLE page_titles (
  title        TEXT    NOT NULL PRIMARY KEY,   -- 名前空間全体で一意
  page_id      INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  is_canonical INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
) WITHOUT ROWID;
CREATE UNIQUE INDEX idx_page_titles_canonical ON page_titles(page_id) WHERE is_canonical = 1;
```

これにより、**アプリ側で衝突を検査する必要がなくなる**(検査忘れで静かに壊れる経路を作らない):

- 同名ページの作成 → PRIMARY KEY 違反で失敗する
- 既存ページ名と同じ別名の登録 → 同じく失敗する
- 別名と同名のページの作成 → 同じく失敗する
- 1ページに正式名は常に1つ → 部分 UNIQUE インデックスが保証する

**`[[...]]` の解決は1クエリで済み、「解決順」という概念自体が存在しなくなる:**

```sql
SELECT page_id FROM page_titles WHERE title = ?;
```

`pages.title` は FTS の external content と表示のために残し、`page_titles` の
`is_canonical = 1` の行と常に一致させる(同一トランザクション内で更新)。

#### リネームの手順(この順序を守ること)

部分 UNIQUE インデックスが保証するのは**「正式名は高々1つ」であって「ちょうど1つ」ではない**。
「少なくとも1つ」は SQLite では表現できないため、手順とトランザクションで守る。

素朴に「降格 → 挿入」とすると、**降格が成功した後で挿入が失敗する**経路が2つある(いずれも実測確認済み):

- **元のタイトルに戻すとき** — 旧名は自分自身の別名として既に存在するので、素の INSERT は必ず
  `UNIQUE constraint failed: page_titles.title` で失敗する。**リネームを取り消す操作が常に壊れる。**
- **他ページのタイトル/別名と衝突するとき** — `UNIQUE constraint failed: page_titles.page_id` で失敗する。

**正しい手順:**

```sql
BEGIN;
-- 1. 新タイトルが他ページのものでないか、**降格より前に**確認する
--    SELECT page_id FROM page_titles WHERE title = :new;
--    → 行が存在し page_id <> :id なら 409 で中断(降格を実行しない)
-- 2. 現在の正式名を降格
UPDATE page_titles SET is_canonical = 0 WHERE page_id = :id AND is_canonical = 1;
-- 3. 自分の既存別名なら昇格、無ければ挿入(UPSERT)
INSERT INTO page_titles(title, page_id, is_canonical) VALUES (:new, :id, 1)
  ON CONFLICT(title) DO UPDATE SET is_canonical = 1;
-- 4. pages.title を同期
UPDATE pages SET title = :new WHERE id = :id;
-- 5. 事後アサーション: 正式名がちょうど1つであること。1 でなければ ROLLBACK
--    SELECT count(*) FROM page_titles WHERE page_id = :id AND is_canonical = 1;
COMMIT;
```

**3つすべてが必要で、どれも省略できない:**

- **単一トランザクション** — 第一の防御。実測では、トランザクション外で失敗させると
  ページが正式名ゼロのまま残ったが、トランザクション内なら ROLLBACK で復元された。
- **手順1 の事前確認を降格より前に置くこと** — 順序を入れ替えると上記の経路が再現する。
- **UPSERT(`ON CONFLICT DO UPDATE`)** — これが無いと「元の名前に戻す」が必ず失敗する。
  トランザクションがあっても操作自体が完了しないので、これは別途必須。

#### 整合性検査(`enghi doctor` / 起動時)

「正式名がちょうど1つ」は DB で表現できない不変条件なので、**外から検査する経路を必ず用意する。**

```sql
-- 正式名を持たないページ
SELECT id FROM pages WHERE id NOT IN (SELECT page_id FROM page_titles WHERE is_canonical = 1);

-- pages.title と page_titles の不一致
-- 【重要】COLLATE BINARY が必須。両列とも COLLATE NOCASE なので、付けないと比較も NOCASE になり、
--         pages.title='EMACS' / page_titles.title='Emacs' のような大小の食い違いを見逃す(実測確認済み)。
SELECT p.id FROM pages p
  LEFT JOIN page_titles t ON t.page_id = p.id AND t.is_canonical = 1
 WHERE t.title IS NOT p.title COLLATE BINARY;
```

#### その他の運用

1. **他ページの本文は一切書き換えない。** Emacs で開いているバッファが背後で陳腐化しないという実利もある。
2. **表示は `[[...]]` に書かれた文字列をそのまま出す。** 現タイトルに置換しない。
   別名は正当な名前であって誤りではないため、書いた通りに出るほうが驚きが少ない。
3. 誤記を直す目的でリネームした場合のために、**「参照元の本文を一括置換する」操作を
   ユーザが明示的に実行できるコマンドとして用意する**(自動では絶対にやらない)。

#### 大小の扱い — `title` は `COLLATE NOCASE`

`pages.title` と `page_titles.title` は **`COLLATE NOCASE`** とする(`slug` と同じ)。理由:

- **BINARY のままだと `[[emacs]]` が「Emacs」に解決されず、未解決リンクになる**(実測確認済み)。
  Wiki としてこれは明確な不便。
- NOCASE は ASCII にのみ効く照合なので、**日本語は一切影響を受けない**
  (「ハハ」と「パパ」は別物のまま。実測確認済み)。
- `slug` が既に `COLLATE NOCASE` なので、片方だけ BINARY だと
  「Emacs がある状態で emacs を作る」がタイトルではなく **slug の UNIQUE 違反で落ち、
  エラーメッセージが意味不明になる**。

いずれにせよ、タイトル衝突のエラーは
**「同名(大小を区別しない)のページが既に存在します」**という文言で返すこと。

#### 別名の管理 UI(必須)

リネームのたびに別名は増え続けるので、**一覧と削除の導線を必ず設けること。**
`/wiki/:slug/history` に「別名」セクションを置き、`is_canonical = 0` の行を一覧して個別に削除できるようにする。
削除は `DELETE FROM page_titles WHERE title = ? AND is_canonical = 0`
(正式名は削除できないことを部分インデックスと合わせて保証する)。

副次的な効果として、`page_titles` は「GNU Emacs / イーマックス / いーまっくす」のような
**別名の明示登録**にもそのまま使える。個人 Wiki ではこれがよく効く。
(`Emacs` と `emacs` は NOCASE により同一タイトルなので、別名としては登録できない。
 別名にできるのは、大小以外の点で異なる表記である。)

### 2.6 定期タスク(第1版に含める)

「毎週のゴミ出し」「月次の経費精算」が日常的に多いため、**自動展開を第1版で実装する。**
これが無いと Weekly Review のたびに手で足すことになり、GTD システムとして成立しない。

#### 生成方式 — 完了(または skip)を契機に、次の1件だけを生成する

**スケジューラで先回りして複数件を materialize しない。** やると、消化できなかった分が
溜まって Next Actions リストが定期タスクで埋まり、GTD が機能しなくなる。
**同じ系列で開いているインスタンスは常に高々1件**とする。

- タスク完了時、`recurrence` が非 NULL なら:
  1. 現インスタンスを `done` にする
  2. 次回日付を計算する(下記)
  3. `recurrence_ends_on` を過ぎていれば生成しない。そうでなければ**同じ `series_id` で新しい行を1件挿入**する
     (title / note / project_id / context_id / area_id / energy / time_estimate / recurrence を引き継ぐ)
- **生成される次インスタンスの `state` は必ず `scheduled` とし、`scheduled_on` に次回日付を入れる。**
  `next` で作ってはいけない。`weekly:tue,fri` のゴミ出しが常時 Next Actions に居座ることになる。
- **`state='scheduled'` のタスクは、`scheduled_on <= today` になったら Next Actions リストに現れる。**
  これは**ビューの条件で表現し、state を書き換えるバッチ処理は作らない**
  (常駐サーバが落ちていた日にタスクが消える、という壊れ方を避けるため)。
  すなわち Next Actions の抽出条件は:
  `state='next' OR (state='scheduled' AND scheduled_on <= date('now'))`
- 「今回は飛ばす」操作(skip)も同じ経路を通す。専用の state は設けず、
  現インスタンスを `dropped` にして次を生成する。
- **系列そのものを終わらせたい場合**は、先に `recurrence` を NULL にしてから完了/破棄する。
  UI では「この回だけ」「この系列全体」を選ばせること。
- `series_id` は系列の最初のタスクの id。系列の履歴はこれで辿れる。

#### 繰り返し規則の記法

org-mode のリピータ記法を踏襲する(利用者が Emacs ユーザであり、既に知っているため)。
`recurrence` 列にこの文字列をそのまま格納する。

| 記法 | 意味 | 次回日付の基準 |
|---|---|---|
| `+1d` `+2w` `+1m` `+1y` | 固定間隔 | **前回の予定日**(`scheduled_on`)+ 間隔。1回分だけ進める |
| `++1w` | 固定間隔、未来まで送る | 前回の予定日 + 間隔を、**今日より後になるまで繰り返し加算** |
| `.+3d` | 完了日基準 | **完了した日** + 間隔 |
| `weekly:mon,thu` | 毎週の指定曜日(複数可) | 次に来る該当曜日 |
| `monthly:25` | 毎月25日 | 次の25日 |
| `monthly:last` | 毎月末 | 次の月末 |
| `yearly:04-01` | 毎年 | 次の4月1日 |

**`+1w` と `.+1w` の違いは実用上とても重要**なので、両方必ず実装すること。
ゴミ出しは `weekly:tue,fri`(曜日が固定)、シーツの洗濯は `.+2w`(やった日から2週間後)、
経費精算は `monthly:25`。**`++` は「溜めずに次に進む」ためのもので、
長期放置した固定日タスクを現在まで一気に追いつかせるのに使う。**

存在しない日付(31日の無い月、閏日)は**その月の最終日に丸める**。

**【重要】日付計算に SQLite の `date(..., '+1 month')` を使わないこと。**
SQLite の日付演算は月末を丸めず、次の月へ溢れる(実測確認済み):

```
date('2024-01-31','+1 month')  ->  2024-03-02   -- 2024-02-29 ではない
date('2024-03-31','+1 month')  ->  2024-05-01   -- 2024-04-30 ではない
date('2024-02-29','+1 year')   ->  2025-03-01   -- 2025-02-28 ではない
```

すなわち `+1m` と `yearly:02-29` が**上の仕様と逆の結果になる**(`monthly:25` は安全)。
**次回日付の計算はすべて Go 側で行い、月末への丸めを明示的に実装すること。**
(これは DB による挙動差でもある。PostgreSQL の `date + interval '1 month'` は丸めて 2024-02-29 を返す。
将来 11 節 段階4 の移行を行う場合、SQL で日付演算していると結果が静かに変わる。)

#### 表示

- Next Actions リストでは通常のタスクと同じに見える。`recurrence` を持つものには小さな循環アイコンを付す。
- Weekly Review 画面に**「定期タスク系列の一覧」**を出す。
  惰性で回り続けているだけの系列を棚卸しするのは GTD 上重要なので、ここは必ず設けること。

## 3. 検索

**最重要の非機能要件。** 目標: 3万件規模で 1 クエリ 20ms 以内、UI 上は打鍵ごとに再検索して体感遅延ゼロ。

**想定規模は Wiki 記事 1〜3 万件。** この範囲は実測済みで、以下の構成でそのまま足りる。
10 万件を超える見込みが出てきたら 11 節の段階2 へ移行する。

**以下は 3 万件の実データによる計測に基づく確定仕様である。推測で変更しないこと。**

### 3.1 索引の構成

- `pages_fts` を FTS5 の **external content table**(列は `title, body` の2列)として作り、
  `pages` に対する AFTER INSERT/UPDATE/DELETE トリガで同期する(DDL は `schema.sql`)。
- **tokenizer は `trigram`**。日本語は空白で分かち書きされないため `unicode61` は使えない。
- **タグは全文検索に載せない。完全一致フィルタとして扱う**(`tags` / `page_tags` の JOIN)。
  - 理由: trigram は 3 文字未満を索引できず、**日本語のタグは 2 文字が主力である**
    (仕事・健康・読書・技術・家事・経理・趣味)。実測で「仕事」は 0 件、「議事録」は 1 件。
    タグ名を非正規化した列を FTS に載せても、**日本語タグに対して何も機能しない。**
  - タグは閉じた小さな語彙なので、完全一致・前方一致で引けば十分に速く、しかも誤マッチが出ない。
    数万件規模では JOIN のコストは誤差。
  - 検索ボックスでタグを効かせたい場合は、**クエリ文字列を `tags.name` と完全一致/前方一致で別途照合し、
    当たったタグのページ群を結果の先頭に足す。FTS には任せない。**
- `tasks_fts`(`title, note`)、`projects_fts`(`title, outcome`)も同様に作る。
- 横断検索の結果は**種別バッジ付きで1つのリストに混ぜて返す**。

### 3.2 trigram の正しい理解(誤解しやすい点)

**trigram の MATCH は実質的に部分一致である。** クエリが本文中に連続した部分文字列として存在すれば
ヒットする。したがって「オフィスの移転」のような自然文クエリも、本文にその通り書かれていれば
普通にヒットする(実測で確認済み。`snippet()` も正しく効く)。

- **やってはいけないこと: クエリを空白や句読点で分割して AND で結ぶ前処理。**
  日本語クエリには空白がないため空振りし、英数字クエリでは意図せず精度を落とす。
- 0 件になるのは「クエリが本文の literal な部分文字列でないとき」だけ。対策は分割ではなく**切り詰め**(3.4)。

### 3.3 クエリのエスケープ(必須)

`C++` や `a"b` を素で MATCH に渡すと FTS5 の構文エラーになり 500 を返す。
**全クエリを次の手順でフレーズリテラル化してから渡すこと。**

1. クエリ内の `"` を `""` に置換する
2. 全体を `"` で囲む

これで全パターンが通る(実測確認済み)。結果として常にフレーズ検索になるが、
trigram では部分一致と等価なので、これがまさに望ましい挙動である。

### 3.4 クエリ長によるフォールバックの梯子

| クエリ長 | 経路 |
|---|---|
| 3文字以上 | `pages_fts MATCH`(3.5 のランキング付き) |
| 2文字以下 | trigram では引けない。**`titles_fts`(bigram)を引き、併せて本文は `LIKE '%q%'`** |

**日本語は2文字語が主力である**(移転・会議・設計・実装・経理・採用・見積・契約・課題・予算…)。
したがって 2 文字経路は例外ではなく**常用経路**であり、ここの体感がシステムの評価を決める。

- **`titles_fts` を別表として持つ。** `pages.title` だけをアプリ側で bigram 分割し、
  `tokenize='unicode61'` の FTS5 表に入れる。タイトルは短いので索引の増分はほぼ無視できる。
  2 文字クエリの大半はタイトル一致で救われ、即座に返る。
  - **`rowid = pages.id` を規約とし、`page_id` 列は作らないこと。**
    `page_id` を列として持つと rowid が自動採番になり(実測確認済み)、更新/削除のたびに
    `DELETE ... WHERE page_id = ?` が全走査になる。正しくは:
    ```sql
    CREATE VIRTUAL TABLE titles_fts USING fts5(title_bigram, tokenize='unicode61');
    -- 更新: DELETE FROM titles_fts WHERE rowid = ?;
    --       INSERT INTO titles_fts(rowid, title_bigram) VALUES (?, ?);
    ```
- 本文の 2 文字検索は `LIKE '%q%'` に任せる。実測で 3 万件・最悪 20ms(ヒット 0 件やレア語のとき)。
  **現時点では許容範囲**だが、件数に対して線形に伸びる。
- 本文の bigram 化は 11 節の段階2。**規模が増えて `LIKE` が体感に響いてから行う。**

#### ASCII 2 文字クエリの語境界フィルタ(必須)

日本語では問題にならないが、`Go` `UI` `DB` `AI` は技術メモの Wiki で日常的なクエリになる。
`LIKE '%go%'` は `algorithm` や `going` に当たる。**bigram 索引も同じ問題を持つ**
(`algorithm` の bigram には `go` が含まれる)。

> **クエリが ASCII 2 文字の場合に限り、`titles_fts` と本文 `LIKE` の両方の結果を、
> アプリ側で語境界(マッチ位置の前後が英数字でないこと)により再フィルタすること。**

日本語 2 文字クエリにはこのフィルタを適用しない(語境界の概念がないため、適用すると全部落ちる)。

#### 2 つの結果集合のマージ規則

2 文字経路は `titles_fts` と本文 `LIKE` という**索引の異なる2つの結果集合**を返すため、
`bm25` では順位を統一できない。以下の規則で合成すること。

1. `titles_fts` のヒットを `bm25` 昇順で全件、先頭に置く
2. その後ろに本文 `LIKE` のヒットを `updated_at` 降順で置く
3. 両方に現れるページは**タイトル側を採用し、本文側から除く**
4. 合算して `limit` で打ち切る

#### 0 件時のフォールバック

MATCH が 0 件のときは、**クエリを後ろから切り詰めて再試行する**
(「オフィスの移転について」→「オフィスの移転」→「オフィス」)。2 段までとする。

### 3.5 ランキングは SQL 側で行う(アプリ側スコアリングはしない)

「ヒット全件を取得してアプリで並べ替える」は**してはいけない**。実測で「オフィス」は
3 万件中 15,789 件ヒットする。一方 **`ORDER BY bm25()` は 1.6 万件を評価しても 4.5ms で済む**(実測)。

```sql
SELECT p.id, p.slug, p.title, p.updated_at,
       snippet(pages_fts, 1, '<mark>', '</mark>', '…', 12) AS snip,
       bm25(pages_fts, 10.0, 1.0) AS score        -- title, body の列重み
  FROM pages_fts JOIN pages p ON p.id = pages_fts.rowid
 WHERE pages_fts MATCH ?
 ORDER BY score, p.updated_at DESC
 LIMIT 50;
```

- **`bm25()` は負の値を返し、小さいほど良い一致。したがって `ORDER BY score` は昇順のままでよい。**
- **`LIMIT` を ORDER BY なしで発行しないこと。** rowid 順の先頭 50 件が返り、
  タイトル完全一致の記事が結果に入らない。
- `score` は連続値なので、上記の第2キー `updated_at` はほぼ効かない。
  新しさを実際に効かせたい場合は `ORDER BY ROUND(score, 1), p.updated_at DESC` のように
  スコアを丸めて段階化する。

### 3.6 別名は FTS に載らない — 別経路で照合する

`page_titles` の別名(「GNU Emacs / イーマックス」など)は、
**`titles_fts` にも `pages_fts` にも入らない**。どちらも `pages.title` / `pages.body` が元だからである。
放置すると「イーマックス」で検索してもヒットしない。

**採用する方式: `page_titles` に対する直接照合。**

- 検索のたびに以下を別途実行し、当たったページを結果に合流させる。**部分一致とする**
  (本文の trigram 検索が部分一致である以上、別名だけ前方一致にすると挙動が揃わない):
  ```sql
  SELECT page_id, title FROM page_titles
   WHERE is_canonical = 0 AND title LIKE '%' || :q || '%';
  ```
  `LIKE` は ASCII について既定で大小を区別しないため、`Emacs` と `emacs` のどちらでも当たる。
  **`:q` に含まれる `%` と `_` はエスケープすること**(`ESCAPE` 句を使う)。
- **別名の総数は少ない**(リネーム回数 + 明示登録分)ので、全走査で十分に速い。
  `titles_fts` に別名を入れると `rowid = pages.id` の規約が崩れる(1ページに複数行が必要になる)ため、
  そちらは採らない。
- 合流順は 3.4 のマージ規則に準じ、**別名ヒットは正式タイトルのヒットの直後**に置く。

### 3.7 API としての形

検索は UI からも Emacs からも同じエンドポイントを使う(`/api/search`)。
**必ずページングと `limit` を持たせ、既定 50 件。** Emacs の補完 UI は先頭 N 件しか見ないため。

## 4. HTTP インタフェース

### 4.1 画面(ディープリンク)

**すべての画面に安定した URL を割り当てること。** 表示層を差し替え可能にするための前提。

```
/                          ダッシュボード
/wiki                      記事一覧(更新順 / タイトル順 / タグ別)
/wiki/:slug                記事
/wiki/:slug/edit           編集
/wiki/:slug/history        履歴
/tags/:name                タグ別一覧
/search?q=                 横断検索結果
/gtd                       GTD トップ
/gtd/inbox                 Inbox
/gtd/next?context=         Next Actions(コンテキスト絞り込み)
/gtd/waiting               Waiting For
/gtd/scheduled             日付付き
/gtd/someday               Someday/Maybe
/gtd/projects              プロジェクト一覧
/gtd/project/:id           プロジェクト詳細
/gtd/areas                 Area 一覧
/gtd/area/:id              Area 詳細
/gtd/review                Weekly Review
/guide                     使い方ガイド(目次)
/guide/:topic              使い方ガイド(gtd = GTD 入門 / enghi = 操作)
```

**ガイドの本文は `docs/guide/<lang>/<topic>.md` に置き、バイナリに埋め込む。**
Wiki ページとして DB に投入しない(利用者が編集・削除でき、版を上げるたびに衝突するため)。
見出しには `{#id}` で**明示的なアンカーを必ず書く**。自動生成の ID は日本語と英語でずれ、
画面から張ったリンクが言語を切り替えた途端に切れる。この2点は `guide_web_test.go` が検査する。

### 4.2 API(JSON)

Emacs 層と Web UI の両方がこれを使う。Web UI は htmx で HTML 断片を受ける経路も併用してよいが、
**下記 API は JSON で独立に成立させること**(Emacs が依存するため)。

```
GET    /api/search?q=&kind=&limit=      横断検索。kind は page|task|project|area(省略で全部)
GET    /api/pages?limit=&offset=&sort=
POST   /api/pages                       {title, body, tags[]}
GET    /api/pages/:slug                 {id, slug, title, body, tags, version, links, backlinks}
PUT    /api/pages/:slug                 {title, body, tags[], version} → 409 は下記2種
DELETE /api/pages/:slug
GET    /api/pages/:slug/backlinks

GET    /api/tasks?state=&context=&project=&area=&due_before=
POST   /api/tasks                       capture 用。{title} だけで作れること(state=inbox)
PATCH  /api/tasks/:id                   部分更新。状態遷移もここ
DELETE /api/tasks/:id

GET    /api/projects?status=
POST   /api/projects
PATCH  /api/projects/:id
GET    /api/projects/stalled            Next Action の無いアクティブプロジェクト

GET    /api/areas ; POST /api/areas ; PATCH /api/areas/:id
GET    /api/contexts ; POST /api/contexts
GET    /api/dashboard                   ダッシュボードに必要な集計を1発で返す

POST   /api/focus                       {path: "/wiki/foo"} → 開いているブラウザタブを遷移させる
GET    /api/events                      WebSocket(または SSE)。focus 通知と更新通知を配信
POST   /api/export                      Markdown 全件エクスポート
```

**楽観ロック**: `pages` と `tasks` の更新は `version` を必須とし、不一致は `409 Conflict` + 現行データを返す。
Emacs バッファからの書き戻しで競合を検出するために必要。

**409 は2つの異なる意味を持つので、必ず機械可読なコードで区別すること。**
クライアント(特に Emacs 層)は、この2つに対してまったく違う対応をしなければならない:

| `error` | 原因 | クライアントの対応 |
|---|---|---|
| `version_conflict` | 楽観ロックの版不一致(2.5 節とは無関係) | 現行データとの差分を提示してマージさせる。**入力は捨てない** |
| `title_conflict` | 新タイトルが他ページの正式名/別名と衝突(2.5 節の手順1) | 別のタイトルを入力させる。本文は保持したまま |

レスポンスボディの形:

```json
{ "error": "version_conflict", "message": "...", "current": { ... } }
{ "error": "title_conflict",   "message": "同名(大小を区別しない)のページが既に存在します",
  "conflicting_page": { "id": 12, "slug": "emacs", "title": "Emacs" } }
```

`title_conflict` では**衝突相手のページを返すこと。** 「どのページと衝突したのか」が分からないと、
利用者は別名を辿って該当ページへ行けない。

**`version` と `page_revisions` の関係(取り違えやすいので明記):**

| | `version` を上げる | `page_revisions` を作る |
|---|---|---|
| 本文の変更 | ○ | ○ |
| タイトルの変更 | ○ | ○ |
| **タグだけの変更** | **○** | **×** |

タグ変更で `version` を上げないと、Emacs 側の楽観ロックがタグ変更を見逃して黙って上書きする。
一方でタグ変更ごとにリビジョンを作るのは無意味なので、リビジョンは本文/タイトルが変わったときだけ。

**`page_revisions` の圧縮ルール(第1段階から入れること):**
**直前のリビジョンが 10 分以内(設定可)に作られたものであれば、無条件にそれを上書きする。**
差分の大小は判定に使わない — 閾値を設けると実装者が独自に決めることになり、挙動が予測不能になる。
時刻だけを見る決定的な規則にすること。Emacs から `C-x C-s` のたびに PUT する運用になると、
これが無ければリビジョンが爆発する。**後から入れても過去分は救えない。**

### 4.3 focus チャネル(Emacs → ブラウザ)

xwidget-webkit の埋め込みは**既定にはしない**が、閲覧用途に限れば実用になる可能性があるため、
第3段階で `enghi-browse-function` の選択肢として試す(8 節 26)。
既知の弱点は「編集可能テキストエリアとの相互作用」と「Emacs と WebKit のキー入力の取り合い」であり、
本システムでは**編集はネイティブな Emacs バッファで行うので、その経路を踏まない**。
不安定という評価の根拠は Emacs 29 世代の報告であり、**現在の emacs-plus@31 / @32 は
`--with-xwidgets` 付きでビルドされている**(この環境で確認済み)。

いずれにせよ、埋め込みの可否とは無関係に以下の focus チャネルが必要なので、
**これを第1段階で実装する**:

1. Web UI は起動時に `/api/events` へ WebSocket を張る。
2. `POST /api/focus {path}` を受けたサーバが、接続中の全クライアントに `{"type":"navigate","path":"..."}` を配信。
3. ブラウザタブが該当画面へ遷移する。

これにより「Emacs で検索・選択 → 別ディスプレイに開きっぱなしのブラウザが追従する」が実現できる。
**第1段階でこの仕組み(WebSocket + navigate)まで入れておくこと。** 数十行で済み、後の Emacs 層の前提になる。

---

### 4.4 セキュリティ(必須 — 「ローカルだから安全」ではない)

`127.0.0.1` に bind しただけでは守れない。**利用者が普段ブラウザで開いている任意の Web ページの
JavaScript が `http://127.0.0.1:<port>/api/...` を叩ける。**
`Content-Type: application/json` の POST は preflight で弾かれるが、`text/plain` や
`application/x-www-form-urlencoded` の POST は simple request として **preflight なしで通る**。

**ログイン・パスワード・セッションといった意味での認証は実装しない。** 理由は後述。
代わりに以下の3層をすべて実装する。いずれも軽い。

1. **`Host` ヘッダの検証。** `127.0.0.1:<port>` / `localhost:<port>` 以外は 403。
   **これが本丸であり、DNS rebinding に対する唯一有効な防御である**
   (rebinding が成立するとブラウザから見て同一オリジンになるため、`Origin` 検査では防げない)。
2. **`Origin` / `Sec-Fetch-Site` の検証。** ブラウザからのクロスオリジン呼び出しを弾く。
   - **実装上の注意: `Sec-Fetch-Site` は非ブラウザのクライアント(curl、Emacs の `url-retrieve`)では
     送られてこない。** 判定は「**ヘッダが存在する場合に** `same-origin` 以外なら 403」とすること。
     「存在しなければ拒否」にすると Emacs 層が動かなくなる。
3. **`Content-Type` の強制。** 書き込み系 API は `application/json` のみ受け付ける。
   preflight を回避できる simple request(`text/plain` / `form-urlencoded`)経路を塞ぐ。

#### トークンを置かない理由(判断の記録 / 蒸し返さないこと)

起動時に生成したトークンを `~/.config/enghi/token` に置く案は検討したうえで**採用しない**。
増える安全性がほぼゼロで、コストだけが恒久的に残るため。

- **悪意ある Web ページに対して**: 防いでいるのは上記 2 のヘッダ検査であって、トークンではない。
- **DNS rebinding に対して**: rebinding 成立後は攻撃ページが `GET /` を読めるので、
  **HTML に埋め込んだトークンはそのまま抜き取れる**。有効なのは上記 1 だけ。
- **マシン上の他プロセスに対して**: 0600 にしても、**同じ UID で動くプロセスはすべて読める**。
  守れるのは「同じマシンの別 UNIX ユーザ」だけで、個人の Mac では該当しない。
- **ブラウザ拡張に対して**: ページの DOM を読める拡張はトークンも読める。

一方コストは、Emacs 層の設定項目、`curl` でのデバッグの手間、トークン破損時の復旧経路として恒久的に残る。

#### 本物の認証が必要になる唯一の条件

**サーバに他のマシンから到達できるようにしたとき**(Tailscale や VPN に載せる、`0.0.0.0` に bind する、
リバースプロキシを前段に置く)。このとき上記3層はすべて無効化されるため、本物の認証が必須になる。
**逆に言えば、それをしない限り不要である。**

さらに:

- **`POST /api/export` の出力先をリクエストで受けないこと。** 設定ファイルの値に固定する。
  任意パスへの書き出しを外部から起動できる状態は危険。
- サーバは `0.0.0.0` に bind できないようにする(設定で指定されても拒否する)。

## 5. ダッシュボード(`/`)

上段が GTD、下段が Wiki。**GTD を使っていなければ上段は自然に空になる**(空でも崩れないレイアウトにすること。
「まだ何もありません」と小さく出す程度で、登録を強く促す UI にはしない)。

**上段 — GTD の現況**
1. Inbox 件数(0 でないときだけ強調)
2. 今日の Next Actions(`deadline_on <= today` または **`scheduled_on <= today`**)
   - **`=` にしないこと。** 見なかった日に予定されていた `scheduled` タスクが翌日以降に消える。
     2.6 でビュー条件方式を採った理由と同じ。
3. コンテキスト別の Next Action 件数(クリックで `/gtd/next?context=`)
4. **停滞プロジェクト(Next Action が無いもの)** — 件数と一覧の先頭数件
5. Waiting For のうち、委譲から一定日数(既定 7 日)が経過したもの
6. **再検討日(`projects.review_on`)が到来した Someday プロジェクト**

**下段 — Wiki**
7. 最近更新した記事 20 件(タイトル、タグ、更新日時)
8. 最近作成した記事(上と分ける)
9. 未解決リンク(`[[...]]` で参照されているが実体の無いページ)の上位いくつか — 書くべきものの示唆になる

`GET /api/dashboard` が上記をまとめて返すこと。個別に N 本クエリを投げない。

---

## 6. UI デザイン方針

- **シックで落ち着いたトーン。** 彩度の低い配色。アクセント色は1色だけ、状態表示にのみ使う。
- ライト/ダーク両対応。`:root` に CSS 変数でトークンを定義し、`prefers-color-scheme` で切り替える。
- 本文は可読性優先。行長は 70〜80 文字程度で頭打ちにする。
- 等幅と可変幅を明確に使い分ける(本文は可変幅、ID やタグ、コードは等幅)。
- アニメーションは原則使わない。使うなら 120ms 以下の opacity のみ。
- **キーボード操作を第一級に扱う**:
  - `/` … 検索にフォーカス
  - `g d` / `g w` / `g i` / `g n` / `g p` … ダッシュボード / Wiki / Inbox / Next / Projects へ
  - `c` … どこからでもクイックキャプチャ(Inbox へ1行追加するモーダル)
  - `e` … 表示中の記事を編集
  - `j` / `k` … リスト内移動、`Enter` で開く
- 検索は**専用画面に遷移する前に、インクリメンタルにその場で候補を出す**こと(htmx で 100ms デバウンス)。

---

## 7. エクスポート

DB 単一正本の唯一の代償を消すための機能。**第1段階で実装する。**

出力先は**設定ファイル `export_dir` の値に固定**する(4.4 節)。
`POST /api/export` はパスを受け取らない。CLI の `enghi export` のみ `--dir` を許す。

**slug のサニタイズ必須**: `/`、`..`、制御文字、先頭のドットを除去してからファイル名にする。
slug は小文字に正規化して保存しているため(2.1 節)、APFS 上での大小衝突は起きない。

以下を出力:

```
<dir>/wiki/<slug>.md          YAML frontmatter(title, tags, created, updated)+ 本文
<dir>/gtd/projects.md         プロジェクトと配下タスクを Markdown のリストで
<dir>/gtd/tasks.md
<dir>/gtd/areas.md
```

インポートは今回作らない(一方向で十分)。

---

## 8. 実装順序

**各段階の終わりで、実際に日常利用できる状態にすること。**

### 第1段階 — Wiki 単体(ここだけで実用になる)
1. サーバ雛形、設定読み込み、SQLite 初期化とマイグレーション
2. `pages` / `page_revisions` / `tags` / `page_tags` / `links` と CRUD
3. `[[wikilink]]` のパースとリンク解決(未解決リンクの保持を含む)
4. **検索(3 節の全体)** — `pages_fts`(trigram, 2列)、`titles_fts`(bigram, `rowid = pages.id`)、
   クエリのフレーズリテラル化(3.3)、クエリ長による経路分岐と ASCII 語境界フィルタ(3.4)、
   2つの結果集合のマージ規則(3.4)、SQL 側 bm25 ランキング(3.5)、別名の直接照合(3.6)、横断検索。
   **3 節は実測に基づく確定仕様なので、推測で簡略化しないこと**
5. 画面: 記事表示・編集・一覧・タグ別・検索・履歴
6. ダッシュボード下段(最近の記事、未解決リンク)。上段は空の枠として置く
7. 4.4 節のセキュリティ3層(Host / Origin+Sec-Fetch-Site / Content-Type)。**後回しにしないこと**
8. WebSocket `/api/events` と `POST /api/focus`
   - **ブラウザ側に指数バックオフの自動再接続を必ず実装する。**
     サーバ再起動後に張り直されないと、常駐運用で「なぜか focus が飛ばない」状態になる
9. Markdown エクスポート
10. `enghi doctor` — 2.5 節の整合性検査(正式名ゼロのページ、`pages.title` と `page_titles` の不一致)。
    **起動時にも実行し、異常があれば警告を出す**
11. `launchd` plist の生成(`enghi install-agent` サブコマンド)

### 第2段階 — GTD
12. `areas` / `projects` / `contexts` / `tasks` と CRUD
13. 状態遷移(clarify フロー: inbox の1件を開いて、行動か資料かを決める導線)
    - 「資料」と判断した場合: **Wiki ページを生成し、元タスクを `state='filed'` にし、
      `links` で新ページへ繋ぐ**。`done` にも `dropped` にもしないこと(2.2 節)
14. 各リスト画面と絞り込み
15. 停滞プロジェクト検出
16. ダッシュボード上段
17. **定期タスクの自動展開(2.6 節)。** 規則パーサ + 次回日付の計算 + 完了/skip 時の生成。
    日付計算は境界(月末、閏日、`++` の追いつき)が必ず壊れるので、**単体テストを書くこと**。
    **`date(..., '+1 month')` を使わないこと** — SQLite の日付演算は月末を丸めず溢れる
    (`2024-01-31` + 1month = `2024-03-02`、実測確認済み)。計算は Go 側で行う(2.6 節)
18. Weekly Review 画面(チェックリスト + 必要な情報の集約表示 + 定期タスク系列の一覧)
19. `projects.note_page_id` / `areas.note_page_id` による Wiki ページ埋め込み

### 第3段階 — Emacs
20. `enghi.el`: API クライアント(`url-retrieve` + JSON)
21. `enghi-search` … `consult` の非同期ソースとして、打鍵ごとに `/api/search` を叩く
22. `enghi-find-page` / `enghi-open` … 選択したものをバッファで開く、または `POST /api/focus` でブラウザを飛ばす
23. 記事編集: `markdown-mode` バッファに本文を展開、`C-c C-c` で PUT。409 なら差分提示
24. `enghi-capture` … どこからでも1行を Inbox へ
25. `enghi-agenda` … org-agenda 風の一覧バッファ。`n`/`w`/`d` などで状態変更を PATCH にマップ
26. 表示関数は差し替え可能にする:
    ```elisp
    (defvar enghi-browse-function #'browse-url)  ; #'xwidget-webkit-browse-url も可
    ```
    **既定は `browse-url`(外部ブラウザ)。** xwidget-webkit は 4.3 節の方針に従い、
    閲覧用途の選択肢として試す。編集はネイティブな Emacs バッファで行うため、
    xwidget の既知の弱点(編集可能テキストエリア、キー入力の取り合い)は踏まない。

---

## 9. 意図的にやらないこと

- **認証(ログイン/パスワード/セッション)** — 4.4 節に判断の記録あり。必要になる条件も明記してある
- マルチユーザ・共有・公開
- 同期(他デバイス、クラウド)
- リアルタイム共同編集
- Markdown からのインポート
- Goals / Vision / Purpose(30,000ft 以上の Horizons)
- グラフビュー(バックリンク一覧で代替。必要になったら足す)
- ブラウザ側の高機能エディタ(本命は Emacs)

---

## 10. 実装担当への確認事項

判断に迷ったら以下の順で優先すること。

1. **検索の速度** — これを損なう設計は採らない
2. **Wiki と GTD の独立性** — `pages` に GTD 由来の列を足さない
3. **単純さ** — 依存を増やすより、少し書く
4. **常駐の安定性** — クラッシュしない。起動が速い。データを壊さない(すべての書き込みをトランザクションで)

実装上の注意(見落としやすい):

- **`PRAGMA foreign_keys = ON` は接続ごとに必要。** スキーマファイルに書いても、
  コネクションプール内の各接続には効かない。**DSN(`_foreign_keys=on` 等)か接続初期化フックで必ず指定すること。**
  一方 `journal_mode = WAL` は永続設定なので一度でよい。
- すべての書き込みをトランザクションで包むこと。特に
  「ページ保存 + `pages.title` と `page_titles` の更新 + `page_tags` の更新 +
  `links` の全削除・再挿入 + `titles_fts` の更新 + `page_revisions` 追加」は
  **必ず単一トランザクション**。

以下は実装中に判明したら報告すること(仕様を勝手に決めないでよい箇所):
- 3.4 節の 2 文字クエリ経路(`titles_fts` + 本文 `LIKE`)の体感。
  **足りなかった場合の移行先は 11 節に決めてある。**

---

## 11. 日本語検索の強化パス

第1版の確定仕様は 3 節。ここでは、規模が増えて 3 節の構成で足りなくなった場合の移行先を示す。
**先回りして実装しないこと。実データでの計測を根拠に判断する。**

### 段階1(第1版) — trigram + タイトル bigram + 本文 LIKE

3 節のとおり。3 万件規模で実測済み(本文 trigram 索引込みで約 49MB、最悪ケース 20ms)。

### 段階2 — 本文も bigram 化

**移行の引き金**: 2 文字クエリの本文 `LIKE` が体感に響いてきたとき(件数の増加に線形)。

- `pages_fts` の external content をやめ、**アプリ側で bigram 分割した本文を実列として持つ**
  通常の FTS5 表にする(`tokenize='unicode61'`)。
- **検索クエリも同じ bigram 分割を通してから MATCH に渡す。** ここを忘れると全く一致しなくなる。
- external content をやめるとトリガによる自動同期ができなくなる。
  **アプリ側の同一トランザクション内で確実に更新すること。**
- **索引は約 3.3 倍になる**(実測 26MB → 85MB)。増分の主因は索引そのものではなく、
  **分かち書き済みテキストを実列として持つため元テキストの約 3 倍が丸ごと乗ること**。
  絶対値としては小さいので、必要になったら躊躇なく移行してよい。
- 移行時は FTS 表を drop して全件再構築する(数万件なら数十秒)。

### 段階3 — 形態素解析

**移行の引き金**: bigram でも語境界を無視した誤マッチが実用上の問題になったとき。

| 実装言語 | 手段 |
|---|---|
| Rust | **Lindera** を FTS5 のカスタムトークナイザとして登録。FTS5 の枠内で完結し、`tokenize=` の差し替えで済む |
| Go | **kagome**(pure Go、辞書同梱)で、段階2 と同じくアプリ側で分かち書き |

**ただし段階3 は無条件の改善ではない。** 形態素解析は「未知語・固有名詞が正しく切れない」という
別の失敗様式を持ち込む。個人の Wiki は固有名詞と造語の比率が高いため、
**bigram のほうが体感が良い可能性が十分にある。移行は実データでの比較を経てから判断すること。**

### 段階4 — PostgreSQL + PGroonga への移行

**移行の引き金**: 段階2(本文 bigram)でも日本語検索の品質が不足し、かつ記事数が 10 万件を超えたとき。

PostgreSQL の組み込み `to_tsvector` も日本語は分かち書きできないので素では使えないが、拡張が優秀である。

| 拡張 | 内容 |
|---|---|
| **PGroonga** | Groonga ベース。日本語の形態素解析と N-gram の両方に対応し、2 文字語も自然に引ける。ランキングも効き、SQL の枠内で完結する |
| **pg_bigm** | bigram インデックス。段階2 で自作しようとしているものが拡張として用意されている |

**つまり段階2・段階3 でやろうとしていることは、PGroonga なら最初から手に入る。**

代償として、Postgres サーバの常駐・拡張のビルド・メジャー版アップグレード時の移行作業が
**恒久的に**乗る。個人の常駐システムで最も効くコストはここで、
「数年後にマシンを新調したとき何分で復旧できるか」で見ると、SQLite はファイルを1つコピーするだけで済む。

なお**同時書き込み・スケール・信頼性は移行の理由にならない。** 単一ユーザ・単一マシンでは
SQLite の WAL(書き手1・読み手N)で十分であり、この観点で Postgres が有利になる場面は存在しない。

**検索品質が実際に日々の利用を妨げている証拠が出るまで移行しないこと。**
