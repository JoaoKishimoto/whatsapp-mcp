// Package media handles attachments (PLANO.md §7, G9): it remembers how to download a
// received one, builds the message for one being sent, and keeps files inside the
// shared media directory.
//
// The store keeps each attachment's protobuf, not its URL. URLs expire; the old
// URL-based download (the upstream /api/download) got 403s once they did.
package media

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

// Item is a received attachment that can be downloaded.
type Item struct {
	Kind         string // image | video | audio | document | sticker
	MIME         string
	Filename     string // documents only
	Size         uint64
	Downloadable whatsmeow.DownloadableMessage
}

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
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS media (
		chat_jid   TEXT    NOT NULL,
		message_id TEXT    NOT NULL,
		kind       TEXT    NOT NULL,
		proto      BLOB    NOT NULL,
		created_at INTEGER NOT NULL,
		PRIMARY KEY (chat_jid, message_id)
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create media table: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Save keeps the attachment of msg, if it has one; ok is false when it has none.
func (s *Store) Save(chatJID, messageID string, msg *waE2E.Message) (ok bool, err error) {
	kind, part := attachment(msg)
	if part == nil {
		return false, nil
	}
	raw, err := proto.Marshal(part)
	if err != nil {
		return false, err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO media (chat_jid, message_id, kind, proto, created_at) VALUES (?, ?, ?, ?, ?)`,
		chatJID, messageID, kind, raw, s.now().Unix())
	return err == nil, err
}

// Get returns the attachment of a message; ok is false if it is unknown or was cleaned up.
func (s *Store) Get(chatJID, messageID string) (it Item, ok bool, err error) {
	var kind string
	var raw []byte
	err = s.db.QueryRow(`SELECT kind, proto FROM media WHERE chat_jid = ? AND message_id = ?`, chatJID, messageID).Scan(&kind, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, false, nil
	}
	if err != nil {
		return Item{}, false, err
	}
	it, err = decode(kind, raw)
	return it, err == nil, err
}

// Cleanup forgets attachments older than maxAge (MESSAGE_RETENTION_DAYS, G11).
func (s *Store) Cleanup(maxAge time.Duration) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM media WHERE created_at < ?`, s.now().Add(-maxAge).Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func attachment(m *waE2E.Message) (string, proto.Message) {
	switch {
	case m.GetImageMessage() != nil:
		return "image", m.GetImageMessage()
	case m.GetVideoMessage() != nil:
		return "video", m.GetVideoMessage()
	case m.GetAudioMessage() != nil:
		return "audio", m.GetAudioMessage()
	case m.GetDocumentMessage() != nil:
		return "document", m.GetDocumentMessage()
	case m.GetStickerMessage() != nil:
		return "sticker", m.GetStickerMessage()
	}
	return "", nil
}

func decode(kind string, raw []byte) (Item, error) {
	it := Item{Kind: kind}
	var err error
	switch kind {
	case "image":
		x := &waE2E.ImageMessage{}
		err = proto.Unmarshal(raw, x)
		it.MIME, it.Size, it.Downloadable = x.GetMimetype(), x.GetFileLength(), x
	case "video":
		x := &waE2E.VideoMessage{}
		err = proto.Unmarshal(raw, x)
		it.MIME, it.Size, it.Downloadable = x.GetMimetype(), x.GetFileLength(), x
	case "audio":
		x := &waE2E.AudioMessage{}
		err = proto.Unmarshal(raw, x)
		it.MIME, it.Size, it.Downloadable = x.GetMimetype(), x.GetFileLength(), x
	case "document":
		x := &waE2E.DocumentMessage{}
		err = proto.Unmarshal(raw, x)
		it.MIME, it.Size, it.Downloadable, it.Filename = x.GetMimetype(), x.GetFileLength(), x, x.GetFileName()
	case "sticker":
		x := &waE2E.StickerMessage{}
		err = proto.Unmarshal(raw, x)
		it.MIME, it.Size, it.Downloadable = x.GetMimetype(), x.GetFileLength(), x
	default:
		err = fmt.Errorf("unknown media kind %q", kind)
	}
	return it, err
}
