// Package queue is the gateway's persisted event queue (PLANO.md §8).
// One consumer (the bridge) pulls with a cursor, then acks. Ids only grow
// (AUTOINCREMENT), so a cursor stays valid after old rows are deleted.
package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	keepAcked   = 24 * time.Hour
	keepUnacked = 7 * 24 * time.Hour
)

type Event struct {
	ID   int64           `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type Store struct {
	db  *sql.DB
	now func() time.Time

	mu      sync.Mutex
	changed chan struct{} // closed and replaced on every Append, to wake long-polls
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS events (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		type       TEXT    NOT NULL,
		data       BLOB    NOT NULL,
		created_at INTEGER NOT NULL,
		acked_at   INTEGER
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create events table: %w", err)
	}
	return &Store{db: db, now: time.Now, changed: make(chan struct{})}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Append(typ string, data any) (int64, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO events (type, data, created_at) VALUES (?, ?, ?)`,
		typ, raw, s.now().Unix())
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	return res.LastInsertId()
}

// List returns up to limit events with id > after. If there are none, it
// waits up to wait for one to arrive (long-poll). Acked events not yet
// deleted are returned too: the consumer dedups by id.
func (s *Store) List(ctx context.Context, after int64, limit int, wait time.Duration) ([]Event, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		s.mu.Lock()
		changed := s.changed // taken before the query, so an Append in between still wakes us
		s.mu.Unlock()

		evs, err := s.query(ctx, after, limit)
		if err != nil || len(evs) > 0 || wait <= 0 {
			return evs, err
		}
		select {
		case <-changed:
		case <-timer.C:
			return evs, nil
		case <-ctx.Done():
			return evs, ctx.Err()
		}
	}
}

func (s *Store) query(ctx context.Context, after int64, limit int) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, data FROM events WHERE id > ? ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evs := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Type, &e.Data); err != nil {
			return nil, err
		}
		evs = append(evs, e)
	}
	return evs, rows.Err()
}

func (s *Store) Ack(upTo int64) error {
	_, err := s.db.Exec(`UPDATE events SET acked_at = ? WHERE id <= ? AND acked_at IS NULL`,
		s.now().Unix(), upTo)
	return err
}

// Cleanup deletes acked events after 24 h and any event after 7 days.
func (s *Store) Cleanup() (int64, error) {
	now := s.now()
	res, err := s.db.Exec(`DELETE FROM events WHERE acked_at < ? OR created_at < ?`,
		now.Add(-keepAcked).Unix(), now.Add(-keepUnacked).Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
