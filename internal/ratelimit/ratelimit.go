// Package ratelimit provides fixed-window rate limiting and a leader lock for
// cron, backed by Redis in production and by memory in single-instance mode.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter limits events per key and window.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// Locker provides a TTL lock (leader election for cron across replicas).
type Locker interface {
	TryLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
}

// Redis implements Limiter and Locker on Redis.
type Redis struct{ C *redis.Client }

// Allow increments a fixed-window counter.
func (r *Redis) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	k := "rl:" + key
	n, err := r.C.Incr(ctx, k).Result()
	if err != nil {
		return true, err // fail open: rate limiting must not break the product
	}
	if n == 1 {
		r.C.Expire(ctx, k, window)
	}
	return n <= int64(limit), nil
}

// TryLock acquires or renews a lock owned by owner.
func (r *Redis) TryLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	k := "lock:" + key
	ok, err := r.C.SetNX(ctx, k, owner, ttl).Result()
	if err != nil || ok {
		return ok, err
	}
	cur, err := r.C.Get(ctx, k).Result()
	if err != nil {
		return false, err
	}
	if cur == owner {
		r.C.Expire(ctx, k, ttl)
		return true, nil
	}
	return false, nil
}

// Memory implements Limiter and Locker in-process.
type Memory struct {
	mu    sync.Mutex
	win   map[string]window
	locks map[string]lock
	now   func() time.Time
}

type window struct {
	n     int
	until time.Time
}

type lock struct {
	owner string
	until time.Time
}

// NewMemory creates an in-memory limiter/locker.
func NewMemory() *Memory {
	return &Memory{win: map[string]window{}, locks: map[string]lock{}, now: time.Now}
}

// Allow implements Limiter.
func (m *Memory) Allow(_ context.Context, key string, limit int, d time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	w := m.win[key]
	if now.After(w.until) {
		w = window{until: now.Add(d)}
	}
	w.n++
	m.win[key] = w
	return w.n <= limit, nil
}

// TryLock implements Locker.
func (m *Memory) TryLock(_ context.Context, key, owner string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	l, ok := m.locks[key]
	if ok && now.Before(l.until) && l.owner != owner {
		return false, nil
	}
	m.locks[key] = lock{owner: owner, until: now.Add(ttl)}
	return true, nil
}
