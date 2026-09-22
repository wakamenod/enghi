-- Settings the user toggles from the screen.
--
-- These are not config.toml (ports and paths). That file is operational
-- configuration, read at start-up, edited by hand and applied by restarting.
-- These are preferences that change at run time from the UI, so they live in
-- the same database as everything else and are naturally covered by backups
-- and exports.
--
-- **No row means the default.** Defaults are never written to the database, so
-- changing one is a code change and needs no migration UPDATE.
CREATE TABLE settings (
  key        TEXT NOT NULL PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
