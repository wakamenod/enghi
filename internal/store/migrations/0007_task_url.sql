-- A link that goes with the task (an issue, an article). Also mirrored in
-- docs/schema.sql.
--
-- One per task, http(s) only (gtd.normalizeURL). The `o' key on a list opens
-- it. Not in tasks_fts: searching by URL has not been needed.
ALTER TABLE tasks ADD COLUMN url TEXT NOT NULL DEFAULT '';
