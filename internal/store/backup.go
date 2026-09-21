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

// Backup は1回のバックアップの結果。
type Backup struct {
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	CreatedAt string `json:"created_at"`
	Removed   int    `json:"removed"` // 世代の上限を超えて削除した数
}

// backupName は日付で1つに決まる名前。同じ日に2回走っても増えない。
func backupName(t time.Time) string {
	return fmt.Sprintf("enghi-%s.db", t.Format("2006-01-02"))
}

// RunBackup は dir へバックアップを取り、keep 世代を残して古いものを消す。
//
// **`VACUUM INTO` を使う。**単なるファイルコピーは WAL の内容を取りこぼし、
// 書き込みの途中を掴むと壊れた複製になる。`VACUUM INTO` は読み取りトランザクション
// の中で一貫したスナップショットを書き出し、同時に断片化も解消する。
func RunBackup(ctx context.Context, db *DB, dir string, keep int) (*Backup, error) {
	if keep <= 0 {
		keep = 7
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	now := time.Now()
	path := filepath.Join(dir, backupName(now))

	// VACUUM INTO は出力先が既に存在すると失敗する。同じ日の取り直しを許す。
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// パスはリクエストから来ない(設定の値)が、念のため引用符を潰しておく
	if strings.ContainsAny(path, "'\x00") {
		return nil, fmt.Errorf("バックアップ先のパスに使えない文字がある: %q", path)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", path)); err != nil {
		return nil, fmt.Errorf("バックアップに失敗した (%s): %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	removed, err := pruneBackups(dir, keep)
	if err != nil {
		return nil, err
	}
	return &Backup{
		Path:      path,
		Bytes:     info.Size(),
		CreatedAt: now.Format("2006-01-02 15:04:05"),
		Removed:   removed,
	}, nil
}

// pruneBackups は新しい keep 個を残して削除する。
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

// listBackups は新しい順に返す(名前が日付なので文字列の降順でよい)。
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

// Backups は現存するバックアップを新しい順に返す。
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

// BackupDaemon は常駐中に1日1回バックアップを取る。
//
// **決まった時刻に1回だけ実行する方式は採らない。**その時刻にサーバが動いていないと
// その日の分が丸ごと飛ぶ。定期タスクの抽出条件と同じ考え方で、
// **「その日のファイルが無ければ取る」**という条件で判断する。
func BackupDaemon(ctx context.Context, db *DB, dir string, keep int, onResult func(*Backup, error)) {
	check := func() {
		path := filepath.Join(dir, backupName(time.Now()))
		if _, err := os.Stat(path); err == nil {
			return // 今日の分はもうある
		}
		b, err := RunBackup(ctx, db, dir, keep)
		if onResult != nil {
			onResult(b, err)
		}
	}
	check() // 起動直後にも確かめる(前回の停止中に日付が変わっていることがある)

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
