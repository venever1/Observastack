package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitScript increments a fixed-window counter and arms its expiry in a
// single atomic step, returning {count, pttl_ms}.
//
// The previous implementation issued INCR and then, only when the result was 1,
// a separate EXPIRE. If the process died between the two, or EXPIRE failed, the
// key would exist with no TTL and that subject would be locked out permanently.
// Redis executes a script atomically, so there is no such window.
//
// PTTL is returned so the caller can send a truthful Retry-After instead of
// assuming the full window is still outstanding.
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
return {count, ttl}
`)

// RateLimiter is a fixed-window request counter backed by Redis.
type RateLimiter struct {
	store     redis.Scripter
	keyPrefix string
	limit     int
	window    time.Duration
}

// NewRateLimiter builds a limiter that allows limit hits per window for each
// distinct key produced by keyPrefix+subject.
func NewRateLimiter(store redis.Scripter, keyPrefix string, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		store:     store,
		keyPrefix: keyPrefix,
		limit:     limit,
		window:    window,
	}
}

// Allow records a hit against subject and reports whether it is within the
// limit, along with how long the caller must wait before retrying.
//
// On a denied request the returned duration is the real remaining time on the
// current window. On an allowed request it is the full window.
func (rl *RateLimiter) Allow(ctx context.Context, subject string) (bool, time.Duration, error) {
	key := rl.keyPrefix + subject

	reply, err := rateLimitScript.Run(ctx, rl.store, []string{key}, rl.window.Milliseconds()).Result()
	if err != nil {
		return false, 0, fmt.Errorf("ratelimit check: %w", err)
	}

	values, ok := reply.([]any)
	if !ok || len(values) != 2 {
		return false, 0, fmt.Errorf("ratelimit: unexpected script reply %T", reply)
	}

	count, ok := values[0].(int64)
	if !ok {
		return false, 0, fmt.Errorf("ratelimit: unexpected count type %T", values[0])
	}
	ttlMS, ok := values[1].(int64)
	if !ok {
		return false, 0, fmt.Errorf("ratelimit: unexpected ttl type %T", values[1])
	}

	if count > int64(rl.limit) {
		wait := time.Duration(ttlMS) * time.Millisecond
		// A negative PTTL means the key has no expiry, which should be
		// impossible given the script. Fail closed rather than report a
		// negative Retry-After.
		if wait < 0 {
			wait = rl.window
		}
		return false, wait, nil
	}

	return true, rl.window, nil
}
