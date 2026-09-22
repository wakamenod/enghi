package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup is the result of a single backup run.
type Backup struct {
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	CreatedAt string `json:"created_at"`
	Removed   int    `json:"removed"` // how many were deleted beyond the keep limit

	// FilesPath is the copy of the image database. It keeps no generations (below).
	FilesPath  string `json:"files_path,omitempty"`
	FilesBytes int64  `json:"files_bytes,omitempty"`
	FilesSkip  bool   `json:"files_skipped,omitempty"` // unchanged since last time, so not retaken
}

// VacuumIntoer is anything that can write out a consistent snapshot (the image
// database).
type VacuumIntoer interface {
	VacuumInto(ctx context.Context, path string) error
	SourcePath() string
}

// FilesBackupName is the name of the image database copy.
//
// **Images keep no generations.** They are content-addressed and append-only,
// so older generations would buy nothing beyond "restore a deleted file".
// Instead, the copy is not retaken when nothing has changed.
const FilesBackupName = "enghi-files.db"

// backupName is determined by the date, so running twice in a day does not add
// a second file.
func backupName(t time.Time) string {
	return fmt.Sprintf("enghi-%s.db", t.Format("2006-01-02"))
}

// BackupFiles copies the image database, doing nothing when it has not changed
// since the last copy.
func BackupFiles(ctx context.Context, blobs VacuumIntoer, dir string) (path string, size int64, skipped bool, err error) {
	if blobs == nil {
		return "", 0, true, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, false, err
	}
	dst := filepath.Join(dir, FilesBackupName)

	// Skip unless the source is newer than the copy: images are large and we do
	// not want to copy them every day.
	if src, err := os.Stat(blobs.SourcePath()); err == nil {
		if prev, err := os.Stat(dst); err == nil && !src.ModTime().After(prev.ModTime()) {
			return dst, prev.Size(), true, nil
		}
	}
	if err := blobs.VacuumInto(ctx, dst); err != nil {
		return "", 0, false, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return "", 0, false, err
	}
	return dst, info.Size(), false, nil
}

// RunBackup writes a backup into dir, keeping `keep` generations and deleting
// the older ones.
//
// **Use `VACUUM INTO`.** A plain file copy misses the contents of the WAL, and
// catching a write half-way produces a corrupt duplicate. `VACUUM INTO` writes
// a consistent snapshot inside a read transaction, and defragments on the way.
func RunBackup(ctx context.Context, db *DB, blobs VacuumIntoer, dir string, keep int) (*Backup, error) {
	if keep <= 0 {
		keep = 7
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	now := time.Now()
	path := filepath.Join(dir, backupName(now))

	// VACUUM INTO fails when the destination exists. Allow retaking it the same day.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// The path comes from the configuration, not from a request, but strip quotes
	// anyway.
	if strings.ContainsAny(path, "'\x00") {
		return nil, fmt.Errorf("backup path contains characters that cannot be used: %q", path)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", path)); err != nil {
		return nil, fmt.Errorf("backup failed (%s): %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	removed, err := pruneBackups(dir, keep)
	if err != nil {
		return nil, err
	}
	out := &Backup{
		Path:      path,
		Bytes:     info.Size(),
		CreatedAt: now.Format("2006-01-02 15:04:05"),
		Removed:   removed,
	}
	// Copy the image database too.
	// **Having split them, a backup of only one of the two is not a backup.**
	fp, fb, skipped, err := BackupFiles(ctx, blobs, dir)
	if err != nil {
		return nil, fmt.Errorf("image backup failed: %w", err)
	}
	out.FilesPath, out.FilesBytes, out.FilesSkip = fp, fb, skipped
	return out, nil
}

// pruneBackups deletes everything but the newest `keep` files.
func pruneBackups(dir string, keep int) (int, error) {
	names, err := listBackups(dir)
	if err != nil {
		return 0, err
	}
	if len(names) <= keep {
		return 0, nil
	}
	removed := 0
	for _, name := range names[keep:] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// listBackups returns them newest first (the names are dates, so a descending
// string sort is enough).
func listBackups(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, "enghi-") && strings.HasSuffix(n, ".db") {
			names = append(names, n)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// Backups returns the existing backups, newest first.
func Backups(dir string) ([]Backup, error) {
	names, err := listBackups(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Backup, 0, len(names))
	for _, n := range names {
		p := filepath.Join(dir, n)
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, Backup{
			Path:      p,
			Bytes:     info.Size(),
			CreatedAt: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	return out, nil
}

// BackupDaemon takes one backup a day while the server is resident.
//
// **It does not run once at a fixed time.** If the server happens to be down at
// that moment, that day is lost entirely. Following the same idea as the
// recurring-task query, the condition is **"take one if there is no file for
// today"**.
func BackupDaemon(ctx context.Context, db *DB, blobs VacuumIntoer, dir string, keep int, onResult func(*Backup, error)) {
	check := func() {
		path := filepath.Join(dir, backupName(time.Now()))
		if _, err := os.Stat(path); err == nil {
			return // today is already covered
		}
		b, err := RunBackup(ctx, db, blobs, dir, keep)
		if onResult != nil {
			onResult(b, err)
		}
	}
	check() // also check right after start-up: the date may have rolled over while down

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}
