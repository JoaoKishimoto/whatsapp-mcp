package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"
)

func TestValidPhone(t *testing.T) {
	for _, ok := range []string{"5571999999999", "14155550123"} {
		if !validPhone(ok) {
			t.Errorf("%s refused", ok)
		}
	}
	for _, bad := range []string{"", "+5571999999999", "071999999999", "55 71 99999", "5571-99999999", "1234567", "1234567890123456"} {
		if validPhone(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestHealth(t *testing.T) {
	cases := []struct {
		status int
		body   string
		ok     bool
	}{
		{200, `{"connected":true,"logged_in":true}`, true},
		{200, `{"connected":false,"logged_in":true}`, false},
		{200, `not json`, false},
		{500, `{"connected":true,"logged_in":true}`, false},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		err := health([]string{srv.URL})
		srv.Close()
		if (err == nil) != c.ok {
			t.Errorf("%d %s: err = %v", c.status, c.body, err)
		}
	}
	if health([]string{"http://127.0.0.1:1/v1/health"}) == nil {
		t.Error("unreachable gateway reported healthy")
	}
}

func TestRetentionFromEnv(t *testing.T) {
	t.Setenv("MESSAGE_RETENTION_DAYS", "")
	if d, err := retentionFromEnv(); err != nil || d != 7*24*time.Hour {
		t.Errorf("default = %v %v", d, err)
	}
	t.Setenv("MESSAGE_RETENTION_DAYS", "3")
	if d, _ := retentionFromEnv(); d != 3*24*time.Hour {
		t.Errorf("3 days = %v", d)
	}
	for _, bad := range []string{"0", "-1", "7d", "x"} {
		t.Setenv("MESSAGE_RETENTION_DAYS", bad)
		if _, err := retentionFromEnv(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// G11: old content goes, whichever driver wrote the timestamp; chats left empty go too.
func TestMessageRetention(t *testing.T) {
	old := storeDir
	storeDir = t.TempDir()
	t.Cleanup(func() { storeDir = old })
	ms, err := NewMessageStore()
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	now := time.Now()
	ms.StoreChat("a@s.whatsapp.net", "A", now)
	ms.StoreChat("b@s.whatsapp.net", "B", now)
	ms.StoreMessage("new", "a@s.whatsapp.net", "a", "fresh", now.Add(-time.Hour), false, "", "", "", nil, nil, nil, 0)
	ms.StoreMessage("old", "a@s.whatsapp.net", "a", "stale", now.Add(-8*24*time.Hour), false, "", "", "", nil, nil, nil, 0)
	// As mattn/go-sqlite3 wrote it before G12, with a UTC offset.
	_, err = ms.db.Exec(`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me) VALUES (?, ?, ?, ?, ?, ?)`,
		"mattn", "b@s.whatsapp.net", "b", "stale", now.Add(-9*24*time.Hour).Format("2006-01-02 15:04:05.999999999-07:00"), false)
	if err != nil {
		t.Fatal(err)
	}
	n, err := ms.DeleteOlderThan(now.Add(-7 * 24 * time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("deleted %d, %v; want 2", n, err)
	}
	var left, chats int
	ms.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&left)
	ms.db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&chats)
	if left != 1 || chats != 1 {
		t.Errorf("left %d messages and %d chats, want 1 and 1", left, chats)
	}
}

// The pure-Go driver must satisfy whatsmeow (foreign keys on) and the upstream message store.
func TestStoresOpenWithPureGoSQLite(t *testing.T) {
	old := storeDir
	storeDir = filepath.Join(t.TempDir(), "store")
	t.Cleanup(func() { storeDir = old })

	client, err := openClient(waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	if client.Store.ID != nil {
		t.Error("a new store should not be paired")
	}
	if fi, err := os.Stat(storeDir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("store dir: %v %v, want mode 0700 (it holds the session keys)", fi.Mode(), err)
	}
	ms, err := NewMessageStore()
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	if _, err := os.Stat(storePath("messages.db")); err != nil {
		t.Error(err)
	}
}
