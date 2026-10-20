package queue

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func ids(evs []Event) []int64 {
	out := []int64{}
	for _, e := range evs {
		out = append(out, e.ID)
	}
	return out
}

func list(t *testing.T, s *Store, after int64, limit int) []int64 {
	t.Helper()
	evs, err := s.List(context.Background(), after, limit, 0)
	if err != nil {
		t.Fatal(err)
	}
	return ids(evs)
}

func appendN(t *testing.T, s *Store, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := s.Append("message", map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCursorAndLimit(t *testing.T) {
	s, _ := open(t)
	appendN(t, s, 5)
	if got := list(t, s, 0, 3); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("first page = %v", got)
	}
	if got := list(t, s, 3, 100); len(got) != 2 || got[0] != 4 {
		t.Errorf("after 3 = %v", got)
	}
	if got := list(t, s, 5, 100); len(got) != 0 {
		t.Errorf("after last = %v", got)
	}
}

func TestPayloadRoundTrip(t *testing.T) {
	s, _ := open(t)
	if _, err := s.Append("poll_vote", map[string]any{"selected": []string{"Sonnet"}}); err != nil {
		t.Fatal(err)
	}
	evs, _ := s.List(context.Background(), 0, 10, 0)
	if len(evs) != 1 || evs[0].Type != "poll_vote" || string(evs[0].Data) != `{"selected":["Sonnet"]}` {
		t.Errorf("got %+v data=%s", evs, evs[0].Data)
	}
}

func TestUnackedSurviveRestart(t *testing.T) {
	s, path := open(t)
	appendN(t, s, 3)
	if err := s.Ack(1); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := list(t, s2, 1, 100); len(got) != 2 || got[0] != 2 {
		t.Errorf("after restart = %v", got)
	}
}

func TestCleanup(t *testing.T) {
	s, _ := open(t)
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return start }
	appendN(t, s, 3) // ids 1..3
	s.Ack(2)         // 1 and 2 acked at start; 3 never acked

	s.now = func() time.Time { return start.Add(23 * time.Hour) }
	if n, _ := s.Cleanup(); n != 0 {
		t.Errorf("deleted %d before 24 h", n)
	}
	s.now = func() time.Time { return start.Add(25 * time.Hour) }
	if n, _ := s.Cleanup(); n != 2 {
		t.Errorf("deleted %d acked events after 24 h, want 2", n)
	}
	if got := list(t, s, 0, 100); len(got) != 1 || got[0] != 3 {
		t.Errorf("left = %v, want [3]", got)
	}
	s.now = func() time.Time { return start.Add(7*24*time.Hour + time.Minute) }
	if n, _ := s.Cleanup(); n != 1 {
		t.Errorf("deleted %d unacked after 7 days, want 1", n)
	}
}

func TestIDsNotReusedAfterCleanup(t *testing.T) {
	s, _ := open(t)
	start := time.Now()
	appendN(t, s, 2)
	s.Ack(2)
	s.now = func() time.Time { return start.Add(48 * time.Hour) }
	s.Cleanup()
	id, err := s.Append("message", nil)
	if err != nil || id != 3 {
		t.Errorf("new id = %d, want 3 (a bridge with cursor 2 would miss a reused id)", id)
	}
}

func TestLongPollWakesOnAppend(t *testing.T) {
	s, _ := open(t)
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.Append("message", nil)
	}()
	began := time.Now()
	evs, err := s.List(context.Background(), 0, 10, 5*time.Second)
	if err != nil || len(evs) != 1 {
		t.Fatalf("got %v, %v", evs, err)
	}
	if waited := time.Since(began); waited > 2*time.Second {
		t.Errorf("took %v, should wake on append", waited)
	}
}

func TestLongPollTimesOut(t *testing.T) {
	s, _ := open(t)
	evs, err := s.List(context.Background(), 0, 10, 100*time.Millisecond)
	if err != nil || evs == nil || len(evs) != 0 {
		t.Errorf("got %v, %v; want empty non-nil slice", evs, err)
	}
}

func TestLongPollStopsWhenClientLeaves(t *testing.T) {
	s, _ := open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.List(ctx, 0, 10, 10*time.Second); err == nil {
		t.Error("want context error")
	}
}
