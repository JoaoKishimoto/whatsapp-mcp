package polls

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSaveAndLoadSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "polls.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	opts := []string{"Haiku", "Sonnet", "🥗 Dieta"}
	if err := s.Save("3EB0POLL", "5571999999999@s.whatsapp.net", opts); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.Options("3EB0POLL")
	if err != nil || !ok || !reflect.DeepEqual(got, opts) {
		t.Errorf("got %v ok=%v err=%v", got, ok, err)
	}
}

func TestUnknownPoll(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "polls.db"))
	defer s.Close()
	if _, ok, err := s.Options("nope"); ok || err != nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

func TestCleanupAfter30Days(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "polls.db"))
	defer s.Close()
	start := time.Now()
	s.now = func() time.Time { return start }
	s.Save("old", "chat", []string{"a", "b"})

	s.now = func() time.Time { return start.Add(29 * 24 * time.Hour) }
	if n, _ := s.Cleanup(); n != 0 {
		t.Errorf("deleted %d before 30 days", n)
	}
	s.now = func() time.Time { return start.Add(31 * 24 * time.Hour) }
	if n, _ := s.Cleanup(); n != 1 {
		t.Errorf("deleted %d after 30 days, want 1", n)
	}
	if _, ok, _ := s.Options("old"); ok {
		t.Error("poll still there")
	}
}
