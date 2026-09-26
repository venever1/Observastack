package middleware

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakeStore emulates the rateLimitScript semantics (INCR, PEXPIRE on first hit,
// PTTL) so limiter logic can be tested without a Redis server.
//
// It records every key it touches and counts Eval calls, which is how the tests
// assert the counter and its expiry are established by one atomic script rather
// than by separate commands.
type fakeStore struct {
	mu      sync.Mutex
	counts  map[string]int64
	expiry  map[string]time.Time
	evals   int
	failAll error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		counts: make(map[string]int64),
		expiry: make(map[string]time.Time),
	}
}

func (f *fakeStore) eval(_ context.Context, keys []string, windowMS int64) (int64, int64, error) {
	if f.failAll != nil {
		return 0, 0, f.failAll
	}
	if len(keys) != 1 {
		return 0, 0, errors.New("fake: expected exactly one key")
	}
	key := keys[0]

	f.mu.Lock()
	defer f.mu.Unlock()

	f.evals++
	f.counts[key]++
	count := f.counts[key]

	if count == 1 {
		f.expiry[key] = time.Now().Add(time.Duration(windowMS) * time.Millisecond)
	}

	var ttl int64
	if exp, ok := f.expiry[key]; ok {
		ttl = int64(exp.Sub(time.Now()) / time.Millisecond)
	}
	return count, ttl, nil
}

func (f *fakeStore) Eval(ctx context.Context, _ string, keys []string, args ...any) *redis.Cmd {
	cmd := redis.NewCmd(ctx)
	windowMS, _ := args[0].(int64)
	count, ttl, err := f.eval(ctx, keys, windowMS)
	if err != nil {
		cmd.SetErr(err)
		return cmd
	}
	cmd.SetVal([]any{count, ttl})
	return cmd
}

func (f *fakeStore) EvalSha(ctx context.Context, _ string, keys []string, args ...any) *redis.Cmd {
	return f.Eval(ctx, "", keys, args...)
}

func (f *fakeStore) EvalRO(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	return f.Eval(ctx, script, keys, args...)
}

func (f *fakeStore) EvalShaRO(ctx context.Context, sha1 string, keys []string, args ...any) *redis.Cmd {
	return f.Eval(ctx, "", keys, args...)
}

func (f *fakeStore) ScriptExists(_ context.Context, _ ...string) *redis.BoolSliceCmd {
	cmd := redis.NewBoolSliceCmd(context.Background())
	cmd.SetVal([]bool{true})
	return cmd
}

func (f *fakeStore) ScriptLoad(_ context.Context, _ string) *redis.StringCmd {
	cmd := redis.NewStringCmd(context.Background())
	cmd.SetVal("sha")
	return cmd
}

func (f *fakeStore) evalCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.evals
}

func (f *fakeStore) expiryFor(key string) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	exp, ok := f.expiry[key]
	return exp, ok
}

func newTestLimiter(store redis.Scripter, limit int, window time.Duration) *RateLimiter {
	return NewRateLimiter(store, "ratelimit:test:", limit, window)
}
