package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
