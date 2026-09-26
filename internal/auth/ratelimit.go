package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	redis     redis.Cmdable
	keyPrefix string
	limit     int
	window    time.Duration
}

func NewRateLimiter(redis redis.Cmdable, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{redis: redis, keyPrefix: "ratelimit:login:", limit: limit, window: window}
}

func (rl *RateLimiter) Allow(ctx context.Context, email string) (bool, error) {
	key := rl.keyPrefix + email
	val, err := rl.redis.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("ratelimit check: %w", err)
	}

	if val == 1 {
		if err := rl.redis.Expire(ctx, key, rl.window).Err(); err != nil {
			return false, fmt.Errorf("ratelimit expire: %w", err)
		}
	}

	return val <= int64(rl.limit), nil
}
