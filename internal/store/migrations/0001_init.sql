-- 0001_init.sql — docs/schema.sql より生成。PRAGMA は store 側で接続ごとに設定する。
-- enghi — スキーマ
-- SQLite 3.34+ (trigram tokenizer のため)
-- 方針は docs/DESIGN.md を参照。3 万件の実データによる計測を反映済み。
--
-- 原則:
--   * pages は GTD を一切知らない。GTD 由来の列を足さないこと。
--   * 参照は GTD → Wiki の一方向のみ(projects.note_page_id など)。
--   * 種別をまたぐリンクはすべて links テーブルに集約する。
--
-- 【重要】PRAGMA foreign_keys は接続ごとの設定である。このファイルに書いても
--   コネクションプール内の各接続には効かない。DSN(例: _foreign_keys=on)または
--   接続初期化フックで必ず指定すること。journal_mode = WAL は永続なので一度でよい。


-- ============================================================
-- Wiki
-- ============================================================

CREATE TABLE pages (
  id          INTEGER PRIMARY KEY,
  slug        TEXT    NOT NULL,               -- URL 用。**小文字に正規化して保存すること**
  -- COLLATE NOCASE: [[emacs]] が「Emacs」に解決されるようにするため(DESIGN.md 2.5)。
  -- ASCII にのみ効く照合なので日本語は影響を受けない。
  title       TEXT    NOT NULL COLLATE NOCASE,
  body        TEXT    NOT NULL DEFAULT '',    -- Markdown 原文
  version     INTEGER NOT NULL DEFAULT 1,     -- 楽観ロック。本文/タイトル/タグのいずれかが変われば +1
  archived    INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
-- APFS は大小を区別しないため、Foo と foo を別ページにするとエクスポート時に衝突する。
CREATE UNIQUE INDEX idx_pages_slug ON pages(slug COLLATE NOCASE);
CREATE INDEX idx_pages_updated ON pages(updated_at DESC);
CREATE INDEX idx_pages_created ON pages(created_at DESC);
-- title は一意。[[...]] の解決がタイトル照合である以上、同名ページはリンク先を非決定的にする。
-- 実効的な一意性は page_titles が担保するが、ここでも二重に守る。
CREATE UNIQUE INDEX idx_pages_title ON pages(title);

-- ページのタイトル名前空間そのもの。正式名(is_canonical=1)と別名(0)の両方を入れる。
-- title が PRIMARY KEY なので、以下が **すべて DB 制約で** 弾かれる:
--   * 同名ページの作成
--   * 既存ページ名と同じ別名の登録
--   * 別名と同名のページの作成
-- したがってアプリ側で名前空間の衝突を検査する必要はない。DESIGN.md 2.5 を参照。
--
-- [[...]] の解決は1クエリ:  SELECT page_id FROM page_titles WHERE title = ?;
-- pages.title は FTS の external content と表示のために残し、is_canonical=1 の行と
-- 常に一致させること(同一トランザクション内で更新)。
CREATE TABLE page_titles (
  title        TEXT    NOT NULL COLLATE NOCASE PRIMARY KEY,
  page_id      INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  is_canonical INTEGER NOT NULL DEFAULT 0 CHECK (is_canonical IN (0,1)),
  created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
) WITHOUT ROWID;
-- 正式名は1ページにつき常にちょうど1つ。
CREATE UNIQUE INDEX idx_page_titles_canonical ON page_titles(page_id) WHERE is_canonical = 1;
CREATE INDEX idx_page_titles_page ON page_titles(page_id);

-- 本文/タイトルが変わったときだけ作る(タグだけの変更では作らない)。
-- 直前のリビジョンが 10 分以内(設定可)なら、無条件にそれを上書きする。差分の大小は見ない。
CREATE TABLE page_revisions (
  id          INTEGER PRIMARY KEY,
  page_id     INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  title       TEXT    NOT NULL,
  body        TEXT    NOT NULL,
  version     INTEGER NOT NULL,
  created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_revisions_page ON page_revisions(page_id, version DESC);

CREATE TABLE tags (
  id    INTEGER PRIMARY KEY,
  name  TEXT NOT NULL UNIQUE        -- 階層は "親/子" の命名規約で表現する(構造としては持たない)
);

CREATE TABLE page_tags (
  page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  tag_id  INTEGER NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
  PRIMARY KEY (page_id, tag_id)
);
CREATE INDEX idx_page_tags_tag ON page_tags(tag_id);

-- 汎用リンク。Wiki 内リンクだけでなく、種別をまたぐ関連もここに集約する。
-- dst_id が NULL の行は「未解決リンク」= [[まだ存在しないページ]]。
--
-- 【ライフサイクル】dst_id はポリモーフィックなので外部キーを張れない。アプリ側で管理すること:
--   * ページ保存時: (src_kind, src_id) の行を全削除 → 本文を再パースして再挿入。同一トランザクションで。
--   * ページ作成時: dst_title が一致する未解決行の dst_id を埋めて解決する。
--   * ページ削除時: そこを指す行は **削除せず dst_id = NULL に戻す**(未解決リンクへ降格)。
--                   そのページ発の行(src 側)は削除する。
--   * project / task / area の削除時も同様。
CREATE TABLE links (
  id         INTEGER PRIMARY KEY,
  src_kind   TEXT    NOT NULL CHECK (src_kind IN ('page','project','task','area')),
  src_id     INTEGER NOT NULL,
  dst_kind   TEXT    NOT NULL CHECK (dst_kind IN ('page','project','task','area')),
  dst_id     INTEGER,                       -- NULL = 未解決
  dst_title  TEXT    NOT NULL,              -- [[...]] に書かれた生の文字列。解決後も保持する
  created_at TEXT    NOT NULL DEFAULT (datetime('now')),
  UNIQUE (src_kind, src_id, dst_kind, dst_title)   -- 保存のたびの増殖を防ぐ
);
CREATE INDEX idx_links_src        ON links(src_kind, src_id);
CREATE INDEX idx_links_dst        ON links(dst_kind, dst_id);       -- バックリンク用
CREATE INDEX idx_links_unresolved ON links(dst_title) WHERE dst_id IS NULL;

-- ============================================================
-- GTD
-- ============================================================

-- 20,000ft: 責任範囲。完了しない。
-- Goals / Vision / Purpose(30,000ft 以上)は作らない。予約列も置かない
-- (SQLite は ALTER TABLE ADD COLUMN が安価なので、必要になってから足す)。
CREATE TABLE areas (
  id            INTEGER PRIMARY KEY,
  name          TEXT    NOT NULL UNIQUE,
  description   TEXT    NOT NULL DEFAULT '',
  note_page_id  INTEGER REFERENCES pages(id) ON DELETE SET NULL,
  sort_order    INTEGER NOT NULL DEFAULT 0,
  archived      INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- GTD の Project = 1年以内に完了でき、2つ以上の行動ステップを要する望ましい結果。
-- outcome には「完了した状態」を書く(タイトルとは別に持つのが GTD の作法)。
CREATE TABLE projects (
  id            INTEGER PRIMARY KEY,
  title         TEXT    NOT NULL,
  outcome       TEXT    NOT NULL DEFAULT '',
  status        TEXT    NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','someday','done','dropped')),
  area_id       INTEGER REFERENCES areas(id) ON DELETE SET NULL,
  note_page_id  INTEGER REFERENCES pages(id) ON DELETE SET NULL,  -- Project Support Material
  review_on     TEXT,    -- 'YYYY-MM-DD' 再検討日。someday に落としたものを浮上させる tickler。
                         -- 日付が到来したものをダッシュボードに出すこと。無いと someday はゴミ箱になる
  sort_order    INTEGER NOT NULL DEFAULT 0,
  version       INTEGER NOT NULL DEFAULT 1,
  completed_at  TEXT,
  created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_projects_status ON projects(status, sort_order);
CREATE INDEX idx_projects_area   ON projects(area_id);
CREATE INDEX idx_projects_review ON projects(review_on) WHERE review_on IS NOT NULL;

CREATE TABLE contexts (
  id         INTEGER PRIMARY KEY,
  name       TEXT    NOT NULL UNIQUE,   -- "@電話" "@オフィス" "@自宅" "@メール" など
  sort_order INTEGER NOT NULL DEFAULT 0,
  archived   INTEGER NOT NULL DEFAULT 0
);

-- 行動。Next リストに載るのは「今すぐ物理的に実行できる単一行動」だけ。
-- state の 'filed' は「Inbox の項目が参照資料と判断され Wiki ページになった」状態。
-- GTD 的には完了でも破棄でもないため独立した状態として持つ(生成ページへは links で繋ぐ)。
CREATE TABLE tasks (
  id            INTEGER PRIMARY KEY,
  title         TEXT    NOT NULL,
  note          TEXT    NOT NULL DEFAULT '',
  state         TEXT    NOT NULL DEFAULT 'inbox'
                  CHECK (state IN ('inbox','next','later','waiting','scheduled',
                                   'someday','filed','done','dropped')),
  project_id    INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  context_id    INTEGER REFERENCES contexts(id) ON DELETE SET NULL,
  area_id       INTEGER REFERENCES areas(id)    ON DELETE SET NULL,  -- 単発行動を直接 Area に紐づける場合

  scheduled_on  TEXT,      -- 'YYYY-MM-DD'。state='scheduled' のとき必須。tickler もこれで表現する
  deadline_on   TEXT,      -- 'YYYY-MM-DD'。本当の締切だけに使う
  waiting_for   TEXT,      -- state='waiting' のときの相手
  delegated_at  TEXT,      -- 委譲した日。経過日数の警告に使う

  energy        TEXT CHECK (energy IN ('low','mid','high')),
  time_estimate INTEGER,   -- 分
  priority      INTEGER NOT NULL DEFAULT 0,
  -- 定期タスク。DESIGN.md 2.6 を参照。第1版で自動展開まで実装する。
  -- 記法は org-mode のリピータ準拠: +1w(固定間隔) / ++1w(未来まで送る) / .+3d(完了日基準)
  --                                weekly:mon,thu / monthly:25 / monthly:last / yearly:04-01
  recurrence         TEXT,
  series_id          INTEGER,  -- 系列の最初のタスクの id。系列の履歴を辿るのに使う
  recurrence_ends_on TEXT,     -- 'YYYY-MM-DD'。これを過ぎたら次を生成しない

  sort_order    INTEGER NOT NULL DEFAULT 0,
  version       INTEGER NOT NULL DEFAULT 1,
  completed_at  TEXT,
  created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_tasks_state     ON tasks(state, sort_order);
CREATE INDEX idx_tasks_project   ON tasks(project_id, state);
CREATE INDEX idx_tasks_context   ON tasks(context_id, state);
CREATE INDEX idx_tasks_scheduled ON tasks(scheduled_on) WHERE scheduled_on IS NOT NULL;
CREATE INDEX idx_tasks_deadline  ON tasks(deadline_on)  WHERE deadline_on  IS NOT NULL;
CREATE INDEX idx_tasks_series    ON tasks(series_id)     WHERE series_id    IS NOT NULL;
-- 同じ系列で開いているインスタンスは常に高々1件に保つこと(先回り生成はしない。DESIGN.md 2.6)。
CREATE INDEX idx_tasks_recurring ON tasks(recurrence)    WHERE recurrence   IS NOT NULL;

-- Weekly Review。ウィザードは作らず、チェックリスト付きの画面1枚で運用する。
CREATE TABLE reviews (
  id           INTEGER PRIMARY KEY,
  started_at   TEXT NOT NULL DEFAULT (datetime('now')),
  completed_at TEXT,
  -- JSON。キーは DESIGN.md 2.3 の標準チェックリストに対応させること:
  --   collect_loose_papers / inbox_zero / empty_head / review_next_actions /
  --   review_past_calendar / review_upcoming_calendar / review_waiting_for /
  --   review_projects / review_someday / review_recurring
  checklist    TEXT NOT NULL DEFAULT '{}',
  note         TEXT NOT NULL DEFAULT ''
);

-- ============================================================
-- 全文検索
-- ============================================================
-- 詳細と根拠は DESIGN.md 3 節(3 万件の実測に基づく)。要点:
--   * trigram の MATCH は実質的に部分一致。自然文クエリもそのまま渡してよい。
--     クエリを空白や句読点で分割して AND で結ぶ前処理は **してはいけない**(日本語で空振りする)。
--   * 全クエリは必ずフレーズリテラル化する: " を "" に置換し、全体を " で囲む。
--     これをしないと C++ や a"b で FTS5 構文エラーになる。
--   * 2 文字以下のクエリは trigram では引けない。titles_fts(bigram)+ 本文 LIKE '%q%' で対応する。
--     日本語は 2 文字語が主力なのでこの経路は常用される。
--   * ランキングは SQL 側で行う。bm25() は負値で小さいほど良いので ORDER BY は昇順のまま。
--     ORDER BY なしの LIMIT は rowid 順になりタイトル一致が落ちるので必ず付けること。

-- タグは FTS に載せない。trigram は 3 文字未満を索引できず、日本語のタグは 2 文字が主力のため
-- (実測: 「仕事」0 件 / 「議事録」1 件)。タグは tags / page_tags の完全一致 JOIN で引く。
CREATE VIRTUAL TABLE pages_fts USING fts5(
  title, body,
  content = 'pages',
  content_rowid = 'id',
  tokenize = 'trigram'
);

CREATE TRIGGER pages_ai AFTER INSERT ON pages BEGIN
  INSERT INTO pages_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
CREATE TRIGGER pages_ad AFTER DELETE ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES('delete', old.id, old.title, old.body);
END;
CREATE TRIGGER pages_au AFTER UPDATE ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES('delete', old.id, old.title, old.body);
  INSERT INTO pages_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;

-- 2 文字クエリ対策。title だけをアプリ側で bigram 分割して入れる(外部 content にしない)。
-- 例: "オフィス移転" → "オフ フィ ィス ス移 移転"
-- 検索時も同じ分割をクエリに適用してから MATCH に渡すこと。
-- タイトルは短いので索引の増分はほぼ無視できる。
-- 【重要】page_id を列として持たないこと。列を持つと rowid が自動採番になり、
-- 更新/削除のたびに DELETE ... WHERE page_id = ? が全走査になる(実測確認済み)。
-- rowid = pages.id を規約とする。
--   更新: DELETE FROM titles_fts WHERE rowid = ?;
--         INSERT INTO titles_fts(rowid, title_bigram) VALUES (?, ?);
CREATE VIRTUAL TABLE titles_fts USING fts5(
  title_bigram,
  tokenize = 'unicode61'
);

CREATE VIRTUAL TABLE tasks_fts USING fts5(
  title, note,
  content = 'tasks',
  content_rowid = 'id',
  tokenize = 'trigram'
);
CREATE TRIGGER tasks_ai AFTER INSERT ON tasks BEGIN
  INSERT INTO tasks_fts(rowid, title, note) VALUES (new.id, new.title, new.note);
END;
CREATE TRIGGER tasks_ad AFTER DELETE ON tasks BEGIN
  INSERT INTO tasks_fts(tasks_fts, rowid, title, note) VALUES('delete', old.id, old.title, old.note);
END;
CREATE TRIGGER tasks_au AFTER UPDATE ON tasks BEGIN
  INSERT INTO tasks_fts(tasks_fts, rowid, title, note) VALUES('delete', old.id, old.title, old.note);
  INSERT INTO tasks_fts(rowid, title, note) VALUES (new.id, new.title, new.note);
END;

CREATE VIRTUAL TABLE projects_fts USING fts5(
  title, outcome,
  content = 'projects',
  content_rowid = 'id',
  tokenize = 'trigram'
);
CREATE TRIGGER projects_ai AFTER INSERT ON projects BEGIN
  INSERT INTO projects_fts(rowid, title, outcome) VALUES (new.id, new.title, new.outcome);
END;
CREATE TRIGGER projects_ad AFTER DELETE ON projects BEGIN
  INSERT INTO projects_fts(projects_fts, rowid, title, outcome) VALUES('delete', old.id, old.title, old.outcome);
END;
CREATE TRIGGER projects_au AFTER UPDATE ON projects BEGIN
  INSERT INTO projects_fts(projects_fts, rowid, title, outcome) VALUES('delete', old.id, old.title, old.outcome);
  INSERT INTO projects_fts(rowid, title, outcome) VALUES (new.id, new.title, new.outcome);
END;

-- ============================================================
-- 代表クエリ
-- ============================================================

-- 3 文字以上の検索(ランキング込み)。実測: 1.6 万ヒットでも 4.5ms。
--
--   SELECT p.id, p.slug, p.title, p.updated_at,
--          snippet(pages_fts, 1, '<mark>', '</mark>', '…', 12) AS snip,
--          bm25(pages_fts, 10.0, 1.0) AS score        -- title, body の列重み
--     FROM pages_fts JOIN pages p ON p.id = pages_fts.rowid
--    WHERE pages_fts MATCH ?          -- 必ずフレーズリテラル化した文字列
--    ORDER BY score, p.updated_at DESC
--    LIMIT 50;
--
-- 停滞プロジェクト: アクティブだが Next Action が1つも無い。
-- ダッシュボードと Weekly Review の両方に必ず出すこと。
--
--   SELECT p.* FROM projects p
--   WHERE p.status = 'active'
--     AND NOT EXISTS (
--       SELECT 1 FROM tasks t
--       WHERE t.project_id = p.id AND t.state IN ('next','waiting','scheduled')
--     );
--
-- 再検討日が到来した Someday プロジェクト:
--
--   SELECT * FROM projects
--   WHERE status='someday' AND review_on IS NOT NULL AND review_on <= date('now');
--
-- あるページへのバックリンク(種別を問わない):
--
--   SELECT src_kind, src_id FROM links WHERE dst_kind='page' AND dst_id=?;
--
-- 未解決リンク(書くべき記事の示唆):
--
--   SELECT dst_title, COUNT(*) c FROM links
--   WHERE dst_id IS NULL GROUP BY dst_title ORDER BY c DESC;
--
-- [[...]] の解決(正式名・別名を区別せず1クエリ):
--
--   SELECT page_id FROM page_titles WHERE title = ?;
--
-- リネーム(この順序を守ること。DESIGN.md 2.5)。単一トランザクションで:
--   1. SELECT page_id FROM page_titles WHERE title = :new;  -- 他ページのものなら 409 で中断
--   2. UPDATE page_titles SET is_canonical = 0 WHERE page_id = :id AND is_canonical = 1;
--   3. INSERT INTO page_titles(title, page_id, is_canonical) VALUES (:new, :id, 1)
--        ON CONFLICT(title) DO UPDATE SET is_canonical = 1;   -- UPSERT。元の名前に戻す操作に必須
--   4. UPDATE pages SET title = :new WHERE id = :id;
--   5. SELECT count(*) FROM page_titles WHERE page_id=:id AND is_canonical=1;  -- 1 でなければ ROLLBACK
--
-- 整合性検査(enghi doctor / 起動時):
--   SELECT id FROM pages WHERE id NOT IN (SELECT page_id FROM page_titles WHERE is_canonical=1);
--   -- COLLATE BINARY が必須。両列とも NOCASE なので、付けないと大小の食い違いを見逃す。
--   SELECT p.id FROM pages p LEFT JOIN page_titles t
--     ON t.page_id=p.id AND t.is_canonical=1 WHERE t.title IS NOT p.title COLLATE BINARY;
--
-- あるページの別名一覧(/wiki/:slug/history の「別名」セクション):
--
--   SELECT title FROM page_titles WHERE page_id = ? AND is_canonical = 0 ORDER BY created_at;
