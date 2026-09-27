package store

// ReadingTable pairs a table whose titles get a reading with the table holding
// the readings (DESIGN 3.8). The readings are made by internal/readings, not
// here: store must not depend on the dictionary.
type ReadingTable struct {
	Kind     string // page / task / project
	Source   string // pages
	Readings string // page_readings
	Key      string // page_id
}

var ReadingTables = []ReadingTable{
	{"page", "pages", "page_readings", "page_id"},
	{"task", "tasks", "task_readings", "task_id"},
	{"project", "projects", "project_readings", "project_id"},
}

// PendingReadingsSQL selects (id, title) for the rows of t whose reading is
// missing or was made from another title. COLLATE BINARY: pages.title is
// NOCASE, and a change in case still has to refresh source_title.
func (t ReadingTable) PendingReadingsSQL() string {
	return `SELECT s.id, s.title FROM ` + t.Source + ` s
	          LEFT JOIN ` + t.Readings + ` r ON r.` + t.Key + ` = s.id
	         WHERE r.` + t.Key + ` IS NULL OR r.source_title <> s.title COLLATE BINARY`
}
