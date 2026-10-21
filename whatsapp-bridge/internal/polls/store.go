// Package polls remembers the options of each poll the gateway sent, so a vote
// (which only carries SHA-256 hashes of the chosen options) can be turned back into names.
package polls

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const keep = 30 * 24 * time.Hour

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS polls (
		message_id TEXT    PRIMARY KEY,
		chat_jid   TEXT    NOT NULL,
		options    TEXT    NOT NULL,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create polls table: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Save(messageID, chatJID string, options []string) error {
	raw, err := json.Marshal(options)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO polls (message_id, chat_jid, options, created_at) VALUES (?, ?, ?, ?)`,
		messageID, chatJID, raw, s.now().Unix())
	return err
}

// Options returns the poll's options; ok is false for a poll this gateway did not send (or already forgot).
func (s *Store) Options(messageID string) (options []string, ok bool, err error) {
	var raw []byte
	err = s.db.QueryRow(`SELECT options FROM polls WHERE message_id = ?`, messageID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &options)
	}
	if err != nil {
		return nil, false, err
	}
	return options, true, nil
}

// Cleanup forgets polls older than 30 days; votes on them are then ignored.
func (s *Store) Cleanup() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM polls WHERE created_at < ?`, s.now().Add(-keep).Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
