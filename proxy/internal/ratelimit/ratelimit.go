package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is a fixed-window rate limiter backed by Redis INCR+EXPIRE. Fixed
// windows are approximate at boundaries but cheap and cluster-safe, which is
// the right tradeoff here — we're preventing abuse, not metering to the token.
type Limiter struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Limiter { return &Limiter{rdb: rdb} }

// Result reports whether a request is allowed and, if not, how long until the
// current window resets.
type Result struct {
	Allowed    bool
	RetryAfter time.Duration
}

// Allow increments the counter for key within a 1-minute window and returns
// whether it's still under limit. The window is the current UTC minute, so keys
// look like "rl:user:<id>:<unix-minute>".
func (l *Limiter) Allow(ctx context.Context, scope, id string, limitPerMin int) (Result, error) {
	if limitPerMin <= 0 {
		return Result{Allowed: true}, nil
	}
	minute := time.Now().UTC().Unix() / 60
	key := fmt.Sprintf("rl:%s:%s:%d", scope, id, minute)

	n, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		// Fail open: a Redis blip should not take down inference. Abuse
		// protection degrades, availability is preserved.
		return Result{Allowed: true}, err
	}
	if n == 1 {
		// First hit in this window — set the TTL so the key self-expires.
		_ = l.rdb.Expire(ctx, key, 70*time.Second).Err()
	}
	if n > int64(limitPerMin) {
		// Retry-After = seconds until the next minute boundary.
		secsIntoMinute := time.Now().UTC().Unix() % 60
		return Result{Allowed: false, RetryAfter: time.Duration(60-secsIntoMinute) * time.Second}, nil
	}
	return Result{Allowed: true}, nil
}
