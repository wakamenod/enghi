-- enghi - schema
-- SQLite 3.34+ (for the trigram tokenizer)
-- See docs/DESIGN.md for the reasoning. Reflects measurements on 30k real rows.
--
-- Principles:
--   * pages knows nothing about GTD. Never add a GTD-derived column to it.
--   * References go one way only, GTD -> wiki (projects.note_page_id and such).
--   * Every cross-kind link is collected in the links table.
--
-- **IMPORTANT** PRAGMA foreign_keys is a per-connection setting. Writing it in
--   this file does not apply it to the connections in the pool. Set it in the
--   DSN (_foreign_keys=on) or in a connection init hook. journal_mode = WAL is
--   persistent, so once is enough.

PRAGMA journal_mode = WAL;      -- persistent
PRAGMA foreign_keys = ON;       -- per connection; see the note above
PRAGMA synchronous = NORMAL;

-- ============================================================
-- Wiki
-- ============================================================

CREATE TABLE pages (
  id          INTEGER PRIMARY KEY,
  slug        TEXT    NOT NULL,               -- for URLs. **Store it lower-cased**
  -- COLLATE NOCASE so that [[emacs]] resolves to "Emacs" (DESIGN.md 2.5).
  -- The collation only affects ASCII, so Japanese is untouched.
  title       TEXT    NOT NULL COLLATE NOCASE,
  body        TEXT    NOT NULL DEFAULT '',    -- raw Markdown
  version     INTEGER NOT NULL DEFAULT 1,     -- optimistic lock; +1 when body, title or tags change
  archived    INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
-- APFS is case-insensitive, so Foo and foo as separate pages collide on export.
CREATE UNIQUE INDEX idx_pages_slug ON pages(slug COLLATE NOCASE);
CREATE INDEX idx_pages_updated ON pages(updated_at DESC);
CREATE INDEX idx_pages_created ON pages(created_at DESC);
-- Titles are unique. Since [[...]] resolves by matching titles, two pages with
-- the same name make link targets non-deterministic. page_titles provides the
-- effective uniqueness; this is the second guard.
CREATE UNIQUE INDEX idx_pages_title ON pages(title);

-- The title namespace itself: canonical titles (is_canonical=1) and aliases (0)
-- live in the same table. Because title is the PRIMARY KEY, **the database
-- constraint alone** rejects all of:
--   * creating a page with an existing name
--   * registering an alias equal to an existing page name
--   * creating a page named like an existing alias
-- So the application never has to check for namespace collisions.
-- See DESIGN.md 2.5.
--
-- Resolving [[...]] is one query: SELECT page_id FROM page_titles WHERE title = ?;
-- pages.title stays for the FTS external content and for display, and must
-- always match the is_canonical=1 row (updated in the same transaction).
CREATE TABLE page_titles (
  title        TEXT    NOT NULL COLLATE NOCASE PRIMARY KEY,
  page_id      INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  is_canonical INTEGER NOT NULL DEFAULT 0 CHECK (is_canonical IN (0,1)),
  created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
) WITHOUT ROWID;
-- Exactly one canonical title per page, always.
CREATE UNIQUE INDEX idx_page_titles_canonical ON page_titles(page_id) WHERE is_canonical = 1;
CREATE INDEX idx_page_titles_page ON page_titles(page_id);

-- Created only when the body or title changes, never for a tag-only edit.
-- If the previous revision is within 10 minutes (configurable) it is simply
-- overwritten; the size of the change is not considered.
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
  name  TEXT NOT NULL UNIQUE        -- hierarchy is a "parent/child" naming convention, not structure
);

CREATE TABLE page_tags (
  page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  tag_id  INTEGER NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
  PRIMARY KEY (page_id, tag_id)
);
CREATE INDEX idx_page_tags_tag ON page_tags(tag_id);

-- Generic links. Not only wiki-internal links: every cross-kind relation is
-- collected here. A row with dst_id NULL is an unresolved link, i.e.
-- [[a page that does not exist yet]].
--
-- **Lifecycle** dst_id is polymorphic, so no foreign key can be declared. The
-- application manages it:
--   * On page save: delete every row for (src_kind, src_id), re-parse the body
--     and re-insert - in the same transaction.
--   * On page create: resolve unresolved rows whose dst_title matches by
--     filling in dst_id.
--   * On page delete: rows pointing at it are **not deleted; dst_id goes back
--     to NULL** (demoted to an unresolved link). Rows originating from that
--     page (the src side) are deleted.
--   * The same applies when a project / task / area is deleted.
CREATE TABLE links (
  id         INTEGER PRIMARY KEY,
  src_kind   TEXT    NOT NULL CHECK (src_kind IN ('page','project','task','area')),
  src_id     INTEGER NOT NULL,
  dst_kind   TEXT    NOT NULL CHECK (dst_kind IN ('page','project','task','area')),
  dst_id     INTEGER,                       -- NULL = unresolved
  dst_title  TEXT    NOT NULL,              -- the raw string written in [[...]]; kept after resolving
  created_at TEXT    NOT NULL DEFAULT (datetime('now')),
  UNIQUE (src_kind, src_id, dst_kind, dst_title)   -- stops rows multiplying on every save
);
CREATE INDEX idx_links_src        ON links(src_kind, src_id);
CREATE INDEX idx_links_dst        ON links(dst_kind, dst_id);       -- for backlinks
CREATE INDEX idx_links_unresolved ON links(dst_title) WHERE dst_id IS NULL;

-- ============================================================
-- GTD
-- ============================================================

-- 20,000ft: areas of responsibility. They never complete.
-- Goals / Vision / Purpose (30,000ft and above) are not implemented, and no
-- columns are reserved for them (ALTER TABLE ADD COLUMN is cheap in SQLite, so
-- they can be added when actually needed).
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

-- A GTD project: a desired outcome that can be finished within a year and takes
-- more than one action step. outcome describes the finished state, kept
-- separately from the title as GTD prescribes.
CREATE TABLE projects (
  id            INTEGER PRIMARY KEY,
  title         TEXT    NOT NULL,
  outcome       TEXT    NOT NULL DEFAULT '',
  status        TEXT    NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','someday','done','dropped')),
  area_id       INTEGER REFERENCES areas(id) ON DELETE SET NULL,
  note_page_id  INTEGER REFERENCES pages(id) ON DELETE SET NULL,  -- Project Support Material
  review_on     TEXT,    -- 'YYYY-MM-DD' review date: the tickler that resurfaces
                         -- something parked in someday. Show the ones that have
                         -- come due on the dashboard; without that, someday is a bin
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
  name       TEXT    NOT NULL UNIQUE,   -- "@phone", "@office", "@home", "@email" and so on
  sort_order INTEGER NOT NULL DEFAULT 0,
  archived   INTEGER NOT NULL DEFAULT 0
);

-- Actions. Only "a single action you can physically do right now" belongs on
-- the next list. The state 'filed' means an inbox item was judged to be
-- reference material and became a wiki page: in GTD terms that is neither done
-- nor dropped, hence a state of its own (the generated page is linked through
-- links).
CREATE TABLE tasks (
  id            INTEGER PRIMARY KEY,
  title         TEXT    NOT NULL,
  note          TEXT    NOT NULL DEFAULT '',
  state         TEXT    NOT NULL DEFAULT 'inbox'
                  CHECK (state IN ('inbox','next','later','waiting','scheduled',
                                   'someday','filed','done','dropped')),
  project_id    INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  context_id    INTEGER REFERENCES contexts(id) ON DELETE SET NULL,
  area_id       INTEGER REFERENCES areas(id)    ON DELETE SET NULL,  -- for a one-off action attached straight to an area

  scheduled_on  TEXT,      -- 'YYYY-MM-DD'; required when state='scheduled'. The tickler uses it too
  deadline_on   TEXT,      -- 'YYYY-MM-DD'; only for a real deadline
  waiting_for   TEXT,      -- who is being waited on when state='waiting'
  delegated_at  TEXT,      -- the day it was delegated; used for the days-elapsed warning

  energy        TEXT CHECK (energy IN ('low','mid','high')),
  time_estimate INTEGER,   -- minutes
  priority      INTEGER NOT NULL DEFAULT 0,
  -- Recurring tasks; see DESIGN.md 2.6. Automatic expansion is in the first
  -- version. The notation follows org-mode repeaters:
  --   +1w (fixed interval) / ++1w (advance into the future) / .+3d (from the
  --   completion date) / weekly:mon,thu / monthly:25 / monthly:last / yearly:04-01
  recurrence         TEXT,
  series_id          INTEGER,  -- id of the first task in the series; used to follow its history
  recurrence_ends_on TEXT,     -- 'YYYY-MM-DD'; past this, no next instance is generated

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
-- Keep at most one open instance per series; never generate ahead (DESIGN.md 2.6).
CREATE INDEX idx_tasks_recurring ON tasks(recurrence)    WHERE recurrence   IS NOT NULL;

-- Weekly Review. No wizard: one screen with a checklist on it.
CREATE TABLE reviews (
  id           INTEGER PRIMARY KEY,
  started_at   TEXT NOT NULL DEFAULT (datetime('now')),
  completed_at TEXT,
  -- JSON. The keys must match the standard checklist in DESIGN.md 2.3:
  --   collect_loose_papers / inbox_zero / empty_head / review_next_actions /
  --   review_past_calendar / review_upcoming_calendar / review_waiting_for /
  --   review_projects / review_someday / review_recurring
  checklist    TEXT NOT NULL DEFAULT '{}',
  note         TEXT NOT NULL DEFAULT ''
);

-- The work log on a task (migration 0003): append-mostly, timestamped entries
-- recording what was tried, found and decided.
--   kind note  ... Markdown, rendered like an article body
--   kind start / pause ... began / stopped working; body is an optional comment
-- **"Working" is derived, not stored**: the latest start/pause entry is 'start'
-- and the task is not done/dropped/filed (see the representative queries).
-- created_at never changes on edit; a per-day view groups by it.
CREATE TABLE task_logs (
  id          INTEGER PRIMARY KEY,
  task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  kind        TEXT    NOT NULL DEFAULT 'note' CHECK (kind IN ('note','start','pause')),
  body        TEXT    NOT NULL DEFAULT '',    -- raw Markdown; may be empty for start/pause
  version     INTEGER NOT NULL DEFAULT 1,     -- optimistic lock, as with pages
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_task_logs_task ON task_logs(task_id, created_at);

-- ============================================================
-- Full-text search
-- ============================================================
-- Details and reasoning are in DESIGN.md section 3 (measured on 30k rows).
-- The essentials:
--   * A trigram MATCH is effectively a substring match, so a natural-language
--     query can be passed through as is. **Never** pre-split the query on
--     spaces or punctuation and AND the pieces together: it misses in Japanese.
--   * Always turn the query into a phrase literal: replace " with "" and wrap
--     the whole thing in ". Without that, C++ or a"b is an FTS5 syntax error.
--   * Queries of two characters or fewer cannot be served by trigram. They go
--     through titles_fts (bigram) plus a body LIKE '%q%'. Japanese is full of
--     two-character words, so that path is in constant use.
--   * Ranking happens in SQL. bm25() is negative and smaller is better, so
--     ORDER BY stays ascending. A LIMIT without ORDER BY falls back to rowid
--     order and drops title matches, so never omit it.

-- Tags are not in the FTS index. trigram cannot index anything shorter than
-- three characters, and Japanese tags are mostly two (measured: 「仕事」 0 hits,
-- 「議事録」 1 hit). Tags are matched exactly, by joining tags / page_tags.
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

-- The two-character-query path. Only titles go in here, split into bigrams by
-- the application (not an external content table).
-- Example: "オフィス移転" -> "オフ フィ ィス ス移 移転"
-- Apply the same split to the query before handing it to MATCH.
-- Titles are short, so the extra index is close to free.
-- **IMPORTANT** Do not add page_id as a column. With a column, rowid becomes
-- auto-assigned and every update/delete turns DELETE ... WHERE page_id = ?
-- into a full scan (measured). The convention is rowid = pages.id:
--   update: DELETE FROM titles_fts WHERE rowid = ?;
--           INSERT INTO titles_fts(rowid, title_bigram) VALUES (?, ?);
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

-- Log entries can be article-length, so the two-character path is a body LIKE,
-- as for pages (not the "small table" shortcut used for tasks and projects).
CREATE VIRTUAL TABLE task_logs_fts USING fts5(
  body,
  content = 'task_logs',
  content_rowid = 'id',
  tokenize = 'trigram'
);
CREATE TRIGGER task_logs_ai AFTER INSERT ON task_logs BEGIN
  INSERT INTO task_logs_fts(rowid, body) VALUES (new.id, new.body);
END;
CREATE TRIGGER task_logs_ad AFTER DELETE ON task_logs BEGIN
  INSERT INTO task_logs_fts(task_logs_fts, rowid, body) VALUES('delete', old.id, old.body);
END;
CREATE TRIGGER task_logs_au AFTER UPDATE ON task_logs BEGIN
  INSERT INTO task_logs_fts(task_logs_fts, rowid, body) VALUES('delete', old.id, old.body);
  INSERT INTO task_logs_fts(rowid, body) VALUES (new.id, new.body);
END;

-- ============================================================
-- Representative queries
-- ============================================================

-- Search of three characters or more, with ranking. Measured: 4.5ms even with
-- 16k hits.
--
--   SELECT p.id, p.slug, p.title, p.updated_at,
--          snippet(pages_fts, 1, '<mark>', '</mark>', '…', 12) AS snip,
--          bm25(pages_fts, 10.0, 1.0) AS score        -- column weights for title, body
--     FROM pages_fts JOIN pages p ON p.id = pages_fts.rowid
--    WHERE pages_fts MATCH ?          -- always a phrase-literalized string
--    ORDER BY score, p.updated_at DESC
--    LIMIT 50;
--
-- Is a task being worked on? The latest start/pause entry decides, via the
-- (task_id, created_at) index; completing or dropping clears it with no write.
--
--   SELECT t.state NOT IN ('done','dropped','filed')
--      AND (SELECT l.kind FROM task_logs l
--            WHERE l.task_id = t.id AND l.kind IN ('start','pause')
--            ORDER BY l.created_at DESC, l.id DESC LIMIT 1) = 'start'
--     FROM tasks t WHERE t.id = ?;
--
-- Stalled projects: active but without a single next action.
-- This must appear on both the dashboard and the Weekly Review.
--
--   SELECT p.* FROM projects p
--   WHERE p.status = 'active'
--     AND NOT EXISTS (
--       SELECT 1 FROM tasks t
--       WHERE t.project_id = p.id AND t.state IN ('next','waiting','scheduled')
--     );
--
-- Someday projects whose review date has come:
--
--   SELECT * FROM projects
--   WHERE status='someday' AND review_on IS NOT NULL AND review_on <= date('now');
--
-- Backlinks to a page, of any kind:
--
--   SELECT src_kind, src_id FROM links WHERE dst_kind='page' AND dst_id=?;
--
-- Unresolved links, i.e. articles worth writing:
--
--   SELECT dst_title, COUNT(*) c FROM links
--   WHERE dst_id IS NULL GROUP BY dst_title ORDER BY c DESC;
--
-- Resolving [[...]] - canonical titles and aliases in one query:
--
--   SELECT page_id FROM page_titles WHERE title = ?;
--
-- Renaming (keep this order; DESIGN.md 2.5), in a single transaction:
--   1. SELECT page_id FROM page_titles WHERE title = :new;  -- another page's? stop with 409
--   2. UPDATE page_titles SET is_canonical = 0 WHERE page_id = :id AND is_canonical = 1;
--   3. INSERT INTO page_titles(title, page_id, is_canonical) VALUES (:new, :id, 1)
--        ON CONFLICT(title) DO UPDATE SET is_canonical = 1;   -- UPSERT; required to rename back
--   4. UPDATE pages SET title = :new WHERE id = :id;
--   5. SELECT count(*) FROM page_titles WHERE page_id=:id AND is_canonical=1;  -- not 1? ROLLBACK
--
-- Consistency checks (enghi doctor, and at start-up):
--   SELECT id FROM pages WHERE id NOT IN (SELECT page_id FROM page_titles WHERE is_canonical=1);
--   -- COLLATE BINARY is required: both columns are NOCASE, so without it a
--   -- difference in case slips through.
--   SELECT p.id FROM pages p LEFT JOIN page_titles t
--     ON t.page_id=p.id AND t.is_canonical=1 WHERE t.title IS NOT p.title COLLATE BINARY;
--
-- The aliases of a page (the "aliases" section of /wiki/:slug/history):
--
--   SELECT title FROM page_titles WHERE page_id = ? AND is_canonical = 0 ORDER BY created_at;
