-- The work log on a task: the running record of what was tried, found and
-- decided, as append-mostly timestamped entries. Also mirrored in
-- docs/schema.sql.
--
-- kind:
--   note  ... Markdown, rendered like an article body ([[links]], code, images)
--   start ... began working on the task; body is an optional comment
--   pause ... stopped working on it; body is an optional comment
--
-- **"Working" is derived, not stored.** A task is working when its latest
-- start/pause entry is 'start' and its state is not done/dropped/filed. So
-- completing or dropping a task needs no extra write, and a past day can be
-- reconstructed from the events alone.
--
-- created_at never changes on edit: it is what a per-day view groups by.
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

-- Same pattern as tasks_fts (DESIGN 3.1): external content, trigram,
-- rowid = task_logs.id. Entries can be article-length, so the two-character
-- path is a body LIKE, as for pages.
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
