-- Title readings for romaji search (DESIGN 3.8). Also mirrored in
-- docs/schema.sql.
--
-- One row per page, task and project, keyed by its id as titles_fts is, so a
-- delete cascades. reading comes from the dictionary's reading (キョウハ) and
-- pronunciation from its pronunciation (キョーワ); both are stored as hiragana
-- without 「ー」, and a query hits when either contains it.
--
-- **Nothing here is written in the transaction that writes the title.** The
-- dictionary costs about 93 MB, so the server rebuilds the rows afterwards in a
-- child process (internal/readings): the rows with none, and those whose
-- source_title no longer equals the title. Deleting every row is safe; they
-- come back. No index: a LIKE over short strings is fast enough (measured).
CREATE TABLE page_readings (
  page_id       INTEGER PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
  source_title  TEXT    NOT NULL,
  reading       TEXT    NOT NULL,
  pronunciation TEXT    NOT NULL
);
CREATE TABLE task_readings (
  task_id       INTEGER PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  source_title  TEXT    NOT NULL,
  reading       TEXT    NOT NULL,
  pronunciation TEXT    NOT NULL
);
CREATE TABLE project_readings (
  project_id    INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  source_title  TEXT    NOT NULL,
  reading       TEXT    NOT NULL,
  pronunciation TEXT    NOT NULL
);
