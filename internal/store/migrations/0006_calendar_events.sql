-- Calendar events imported read-only (internal/calendar). Also mirrored in
-- docs/schema.sql.
--
-- A sync replaces one source's events whose start falls in the window it
-- covers; rows outside the window stay, as the history the day page shows. An
-- event still there keeps its row and id.
-- Times are UTC, as everywhere else; an all-day event runs from local midnight
-- to the local midnight after its last day.
CREATE TABLE calendar_events (
  -- never reused, so a page left open cannot act on another event by its id
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  source    TEXT    NOT NULL,              -- 'shortcuts', or the name a PUT gave
  calendar  TEXT    NOT NULL DEFAULT '',
  title     TEXT    NOT NULL DEFAULT '',
  starts_at TEXT    NOT NULL,
  ends_at   TEXT    NOT NULL,
  all_day   INTEGER NOT NULL DEFAULT 0,
  location  TEXT    NOT NULL DEFAULT '',
  -- sha1(calendar | title | starts_at). Shortcuts exposes no stable event ID,
  -- so a moved event is a new event.
  event_key TEXT    NOT NULL,
  synced_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX idx_calendar_events_key ON calendar_events(source, event_key);
CREATE INDEX idx_calendar_events_starts ON calendar_events(starts_at);

-- Events turned into tasks. Keyed by event_key rather than the row, so the
-- link survives the window replace; deleting the task removes it.
CREATE TABLE event_tasks (
  event_key TEXT    NOT NULL PRIMARY KEY,
  task_id   INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX idx_event_tasks_task ON event_tasks(task_id);
