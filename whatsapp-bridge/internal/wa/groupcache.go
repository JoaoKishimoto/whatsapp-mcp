package wa

import (
	"context"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// GroupCache keeps group info so each group message does not cost a call to WhatsApp.
// Entries expire after ttl; Put and Forget keep it current from group events.
type GroupCache struct {
	fetch func(context.Context, types.JID) (*types.GroupInfo, error)
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[types.JID]cached
}

type cached struct {
	group Group
	at    time.Time
}

func NewGroupCache(fetch func(context.Context, types.JID) (*types.GroupInfo, error), ttl time.Duration) *GroupCache {
	return &GroupCache{fetch: fetch, ttl: ttl, now: time.Now, entries: map[types.JID]cached{}}
}

func (c *GroupCache) Get(ctx context.Context, jid types.JID) (Group, error) {
	c.mu.Lock()
	e, ok := c.entries[jid]
	c.mu.Unlock()
	if ok && c.now().Sub(e.at) < c.ttl {
		return e.group, nil
	}
	info, err := c.fetch(ctx, jid)
	if err != nil {
		return Group{}, err
	}
	c.Put(info)
	return GroupFrom(info), nil
}

func (c *GroupCache) Put(info *types.GroupInfo) {
	c.mu.Lock()
	c.entries[info.JID] = cached{group: GroupFrom(info), at: c.now()}
	c.mu.Unlock()
}

func (c *GroupCache) Forget(jid types.JID) {
	c.mu.Lock()
	delete(c.entries, jid)
	c.mu.Unlock()
}
