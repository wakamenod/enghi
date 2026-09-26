-- Indexes for the per-day work record (/gtd/day). Also mirrored in
-- docs/schema.sql.
--
-- A day is a range of UTC timestamps, [D 00:00 local, D+1 00:00 local), so
-- both lookups are pure time ranges. idx_task_logs_task leads with task_id and
-- cannot serve one.
CREATE INDEX idx_task_logs_created ON task_logs(created_at);
CREATE INDEX idx_tasks_completed   ON tasks(completed_at) WHERE completed_at IS NOT NULL;
