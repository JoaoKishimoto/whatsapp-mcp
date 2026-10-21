package wa

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type fetcher struct {
	calls int
	name  string
	fail  bool
}

func (f *fetcher) fetch(_ context.Context, jid types.JID) (*types.GroupInfo, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("offline")
	}
	return &types.GroupInfo{JID: jid, GroupName: types.GroupName{Name: f.name}}, nil
}

func TestGroupCacheHitsAndExpires(t *testing.T) {
	f := &fetcher{name: "v1"}
	c := NewGroupCache(f.fetch, 10*time.Minute)
	start := time.Now()
	c.now = func() time.Time { return start }

	c.Get(context.Background(), group)
	c.Get(context.Background(), group)
	if f.calls != 1 {
		t.Errorf("fetched %d times, want 1 (second read from cache)", f.calls)
	}

	f.name = "v2"
	c.now = func() time.Time { return start.Add(11 * time.Minute) }
	if g, _ := c.Get(context.Background(), group); g.Name != "v2" || f.calls != 2 {
		t.Errorf("after ttl: name=%q calls=%d", g.Name, f.calls)
	}
}

func TestGroupCacheForgetAndPut(t *testing.T) {
	f := &fetcher{name: "old"}
	c := NewGroupCache(f.fetch, time.Hour)
	c.Get(context.Background(), group)

	c.Put(&types.GroupInfo{JID: group, GroupName: types.GroupName{Name: "renamed"}})
	if g, _ := c.Get(context.Background(), group); g.Name != "renamed" || f.calls != 1 {
		t.Errorf("Put not used: name=%q calls=%d", g.Name, f.calls)
	}
	c.Forget(group)
	c.Get(context.Background(), group)
	if f.calls != 2 {
		t.Errorf("Forget did not force a fetch: calls=%d", f.calls)
	}
}

func TestGroupCacheErrorNotCached(t *testing.T) {
	f := &fetcher{fail: true}
	c := NewGroupCache(f.fetch, time.Hour)
	if _, err := c.Get(context.Background(), group); err == nil {
		t.Fatal("want error")
	}
	f.fail = false
	if _, err := c.Get(context.Background(), group); err != nil || f.calls != 2 {
		t.Errorf("err=%v calls=%d: a failure must not be cached", err, f.calls)
	}
}
