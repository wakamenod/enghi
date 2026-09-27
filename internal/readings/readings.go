// Package readings keeps the title readings for romaji search up to date
// (DESIGN 3.8).
//
// **Readings are not written with the title.** Making them takes the IPA
// dictionary, about 93 MB of heap that the process can never give back
// (ipa.Dict keeps it). So nothing writes readings in the write transaction;
// instead Rebuild finds the rows whose reading is missing or was made from
// another title and redoes them, and the server runs it in a short-lived child
// process, once at start-up and a moment after writes (Worker). A new title
// becomes findable by romaji a few seconds late; that is accepted.
package readings

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/yomi"
)

// Pending counts the rows whose reading is missing or stale. It is cheap and
// needs no dictionary, so the server asks it before starting a rebuild.
func Pending(ctx context.Context, db *store.DB) (int, error) {
	total := 0
	for _, t := range store.ReadingTables {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM (`+t.PendingReadingsSQL()+`)`).Scan(&n); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// Rebuild makes the missing and stale readings and returns how many rows it
// wrote. The dictionary is loaded only when there is something to do. The rows
// are read and written in one transaction, so a title renamed meanwhile is
// either seen or left stale for the next run - never recorded under the wrong
// title.
func Rebuild(ctx context.Context, db *store.DB) (int, error) {
	n, err := Pending(ctx, db)
	if err != nil || n == 0 {
		return 0, err
	}
	r, err := yomi.NewReader()
	if err != nil {
		return 0, err
	}
	written := 0
	err = db.Tx(ctx, func(tx *sql.Tx) error {
		for _, t := range store.ReadingTables {
			rows, err := tx.QueryContext(ctx, t.PendingReadingsSQL())
			if err != nil {
				return err
			}
			type row struct {
				id    int64
				title string
			}
			var todo []row
			for rows.Next() {
				var x row
				if err := rows.Scan(&x.id, &x.title); err != nil {
					rows.Close()
					return err
				}
				todo = append(todo, x)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			stmt, err := tx.PrepareContext(ctx,
				`INSERT INTO `+t.Readings+`(`+t.Key+`, source_title, reading, pronunciation)
				 VALUES (?, ?, ?, ?)
				 ON CONFLICT(`+t.Key+`) DO UPDATE SET source_title = excluded.source_title,
				   reading = excluded.reading, pronunciation = excluded.pronunciation`)
			if err != nil {
				return err
			}
			for _, x := range todo {
				rd, pr := r.Readings(x.title)
				if _, err := stmt.ExecContext(ctx, x.id, x.title, rd, pr); err != nil {
					stmt.Close()
					return err
				}
			}
			stmt.Close()
			written += len(todo)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

// Worker runs rebuilds for the server: once when Loop starts, then Delay after
// the last Kick. Kicks while one is waiting or running fold into one more
// pass, so a burst of writes costs a single rebuild.
type Worker struct {
	db    *store.DB
	run   func(context.Context) error
	Delay time.Duration
	kick  chan struct{}
}

// NewWorker returns a worker that calls run when rows are pending. The server
// points run at a child process (`enghi rebuild-readings`), whose memory goes
// back to the system when it exits.
func NewWorker(db *store.DB, run func(context.Context) error) *Worker {
	return &Worker{db: db, run: run, Delay: 2 * time.Second, kick: make(chan struct{}, 1)}
}

// Kick asks for a pass after Delay. It never blocks; call it after any write.
func (w *Worker) Kick() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// Loop runs until ctx is done. It does not block the start-up of the server:
// start it with go.
func (w *Worker) Loop(ctx context.Context) {
	w.pass(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		}
		// Debounce: wait until the writes stop for Delay
		timer := time.NewTimer(w.Delay)
	wait:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-w.kick:
				timer.Reset(w.Delay)
			case <-timer.C:
				break wait
			}
		}
		w.pass(ctx)
	}
}

func (w *Worker) pass(ctx context.Context) {
	n, err := Pending(ctx, w.db)
	if err == nil && n > 0 {
		err = w.run(ctx)
	}
	if err != nil && ctx.Err() == nil {
		log.Printf("warning: rebuilding title readings failed: %v", err)
	}
}
