-- The automatic pause. Also mirrored in docs/schema.sql.
--
-- A state change that takes a working task out of Next (to inbox, later,
-- waiting, scheduled or someday) writes a pause mark in the same transaction,
-- with moved_to set to the state it went to. A manual pause leaves it NULL.
-- **It is a mark, not a change to how "working" is derived**, so a past day
-- still reconstructs from the events alone. done/dropped/filed need none: the
-- derivation already ends work there.
ALTER TABLE task_logs ADD COLUMN moved_to TEXT;
