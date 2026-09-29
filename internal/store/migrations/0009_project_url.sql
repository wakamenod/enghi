-- A link that goes with the project (a repository, a tracking issue, a shared
-- document). Also mirrored in docs/schema.sql.
--
-- One per project, http(s) only (gtd.normalizeURL), as for tasks (0007). The
-- `o' key on a list opens it.
ALTER TABLE projects ADD COLUMN url TEXT NOT NULL DEFAULT '';
