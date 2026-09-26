package middleware

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// integrationRedisEnv points at a Redis instance for the atomicity tests.
//
//	INTEGRATION_REDIS_URL='redis://localhost:6379' go test ./internal/middleware/ -run IntegrationRateLimit -v
const integrationRedisEnv = "INTEGRATION_REDIS_URL"

func newIntegrationRedis(t *testing.T) *redis.Client {
	t.Helper()

	raw := os.Getenv(integrationRedisEnv)
	if raw == "" {
		t.Skipf("set %s to run rate limit integration tests", integrationRedisEnv)
	}

	opts, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", integrationRedisEnv, err)
	}

	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("ping redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return client
}

// uniquePrefix keeps concurrent runs and parallel packages from sharing keys.
func uniquePrefix(t *testing.T) string {
	t.Helper()
	return "ratelimit:it:" + uuid.New().String() + ":"
}

// TestIntegrationRateLimit_CounterNeverObservedWithoutTTL asserts a consistency
// property under load: a counter that has been incremented always has an expiry.
//
// Note on what this does and does not prove. It is NOT a reliable detector of the
// original non-atomic INCR-then-EXPIRE bug: the window between those two commands
// is sub-microsecond on loopback, and the PTTL read below is a separate round
// trip, so it almost never samples inside that window (verified empirically: the
// legacy implementation was measured observing it 0 times out of 400 calls).
//
// The deterministic guard against that regression is
// TestRateLimiter_EstablishesTTLInSameOperationAsCounter, which asserts exactly
// one script evaluation per Allow, making a two-command implementation impossible.
// This test remains useful as a load-consistency check.
func TestIntegrationRateLimit_CounterNeverObservedWithoutTTL(t *testing.T) {
	client := newIntegrationRedis(t)
	ctx := context.Background()

	prefix := uniquePrefix(t)
	key := prefix + "consistency"
	rl := NewRateLimiter(client, prefix, 100, 30*time.Second)
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	const goroutines = 16
	const perGoroutine = 25

	var (
		mu         sync.Mutex
		missingTTL int
		wg         sync.WaitGroup
	)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				if _, _, err := rl.Allow(ctx, "consistency"); err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				ttl, err := client.PTTL(ctx, key).Result()
				if err != nil {
					t.Errorf("pttl: %v", err)
					return
				}
				if ttl <= 0 {
					mu.Lock()
					missingTTL++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if missingTTL != 0 {
		t.Errorf("observed the counter %d times without a TTL", missingTTL)
	}

	final, err := client.Get(ctx, key).Int64()
	if err != nil {
		t.Fatalf("read final counter: %v", err)
	}
	if want := int64(goroutines * perGoroutine); final != want {
		t.Errorf("final counter = %d, want %d", final, want)
	}
}

func TestIntegrationRateLimit_ExactlyLimitAdmittedUnderConcurrency(t *testing.T) {
	client := newIntegrationRedis(t)
	ctx := context.Background()

	prefix := uniquePrefix(t)
	key := prefix + "admit"
	rl := NewRateLimiter(client, prefix, 25, 30*time.Second)
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	const callers = 200

	var (
		mu      sync.Mutex
		allowed int
		wg      sync.WaitGroup
	)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _, err := rl.Allow(ctx, "admit")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed != 25 {
		t.Errorf("allowed %d of %d concurrent callers, want exactly the limit of 25", allowed, callers)
	}
}

func TestIntegrationRateLimit_RetryAfterReflectsRealRemainingTTL(t *testing.T) {
	client := newIntegrationRedis(t)
	ctx := context.Background()

	prefix := uniquePrefix(t)
	key := prefix + "retry"
	rl := NewRateLimiter(client, prefix, 1, 20*time.Second)
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	if ok, _, err := rl.Allow(ctx, "retry"); err != nil || !ok {
		t.Fatalf("first allow = %v, err = %v", ok, err)
	}

	ok, retryAfter, err := rl.Allow(ctx, "retry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("second attempt must be denied")
	}
	// The script returns the live PTTL, so the caller learns the real remaining
	// time rather than always being told the full window.
	if retryAfter <= 0 || retryAfter > 20*time.Second {
		t.Errorf("retryAfter = %v, want a positive value within the 20s window", retryAfter)
	}
}

func TestIntegrationRateLimit_IPAndSubjectCountersAreIndependent(t *testing.T) {
	client := newIntegrationRedis(t)
	ctx := context.Background()

	prefix := uniquePrefix(t)
	ipLimiter := NewRateLimiter(client, prefix+"ip:", 2, 30*time.Second)
	subjectLimiter := NewRateLimiter(client, prefix+"email:", 2, 30*time.Second)
	t.Cleanup(func() {
		_ = client.Del(context.Background(),
			prefix+"ip:1.2.3.4", prefix+"email:victim@example.com").Err()
	})

	for i := 0; i < 2; i++ {
		if ok, _, err := ipLimiter.Allow(ctx, "1.2.3.4"); err != nil || !ok {
			t.Fatalf("ip attempt %d = %v, err = %v", i+1, ok, err)
		}
		if ok, _, err := subjectLimiter.Allow(ctx, "victim@example.com"); err != nil || !ok {
			t.Fatalf("subject attempt %d = %v, err = %v", i+1, ok, err)
		}
	}

	if ok, _, _ := ipLimiter.Allow(ctx, "1.2.3.4"); ok {
		t.Error("per-IP limit should be exhausted")
	}
	if ok, _, _ := subjectLimiter.Allow(ctx, "victim@example.com"); ok {
		t.Error("per-subject limit should be exhausted")
	}

	// A different IP is unaffected by the exhausted per-IP counter...
	if ok, _, _ := ipLimiter.Allow(ctx, "5.6.7.8"); !ok {
		t.Error("a different IP must not inherit another IP's exhausted limit")
	}
	// ...and a different account is unaffected by the exhausted per-subject one.
	if ok, _, _ := subjectLimiter.Allow(ctx, "other@example.com"); !ok {
		t.Error("a different subject must not inherit another subject's exhausted limit")
	}
}

func TestIntegrationRateLimit_WindowActuallyExpires(t *testing.T) {
	client := newIntegrationRedis(t)
	ctx := context.Background()

	prefix := uniquePrefix(t)
	key := prefix + "expiry"
	rl := NewRateLimiter(client, prefix, 1, 2*time.Second)
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	if ok, _, err := rl.Allow(ctx, "expiry"); err != nil || !ok {
		t.Fatalf("first allow = %v, err = %v", ok, err)
	}
	if ok, _, _ := rl.Allow(ctx, "expiry"); ok {
		t.Fatal("second attempt must be denied inside the window")
	}

	// The key must actually carry a TTL, so the lockout ends rather than
	// lasting forever.
	ttl, err := client.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("pttl: %v", err)
	}
	if ttl <= 0 || ttl > 2*time.Second {
		t.Errorf("TTL = %v, want a positive value up to the 2s window", ttl)
	}
}
