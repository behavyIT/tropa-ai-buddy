package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"tropa/internal/ratelimit"
)

func check(t *testing.T, l ratelimit.Limiter, k ratelimit.Locker) {
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(ctx, "k", 3, time.Minute); !ok {
			t.Fatalf("allow %d", i)
		}
	}
	if ok, _ := l.Allow(ctx, "k", 3, time.Minute); ok {
		t.Fatal("4th must be limited")
	}
	if ok, _ := k.TryLock(ctx, "lead", "a", time.Minute); !ok {
		t.Fatal("first lock")
	}
	if ok, _ := k.TryLock(ctx, "lead", "b", time.Minute); ok {
		t.Fatal("foreign lock")
	}
	if ok, _ := k.TryLock(ctx, "lead", "a", time.Minute); !ok {
		t.Fatal("renew own lock")
	}
}

func TestSEC07_RateLimitAndLeaderLockMemory(t *testing.T) {
	m := ratelimit.NewMemory()
	check(t, m, m)
}

func TestSEC08_RateLimitAndLeaderLockRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	r := &ratelimit.Redis{C: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	check(t, r, r)
	mr.Close()
	if ok, err := r.Allow(context.Background(), "x", 1, time.Minute); !ok || err == nil {
		t.Fatal("redis down must fail open with error")
	}
	if _, err := r.TryLock(context.Background(), "x", "a", time.Minute); err == nil {
		t.Fatal("lock error")
	}
}
