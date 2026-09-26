package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_AllowsUpToLimitThenDenies(t *testing.T) {
	store := newFakeStore()
	rl := newTestLimiter(store, 3, time.Minute)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		allowed, _, err := rl.Allow(ctx, "attacker")
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Fatalf("attempt %d should be allowed, limit is 3", i)
		}
	}

	allowed, retryAfter, err := rl.Allow(ctx, "attacker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("4th attempt must be denied at a limit of 3")
	}
	if retryAfter <= 0 || retryAfter > time.Minute {
		t.Errorf("retryAfter = %v, want a positive value up to the window", retryAfter)
	}
}

func TestRateLimiter_SubjectsAreIndependent(t *testing.T) {
	rl := newTestLimiter(newFakeStore(), 2, time.Minute)
	ctx := context.Background()

	// Exhaust one subject; a different subject must be unaffected.
	for i := 0; i < 2; i++ {
		if allowed, _, _ := rl.Allow(ctx, "victim-a@example.com"); !allowed {
			t.Fatalf("victim-a attempt %d should be allowed", i+1)
		}
	}
	if allowed, _, _ := rl.Allow(ctx, "victim-a@example.com"); allowed {
		t.Fatal("victim-a should be denied after exhausting its limit")
	}
	if allowed, _, _ := rl.Allow(ctx, "victim-b@example.com"); !allowed {
		t.Fatal("victim-b must be unaffected by victim-a exhausting its limit")
	}
}

// The counter and its expiry must be established by a single atomic script.
// The old implementation issued INCR and a separate conditional EXPIRE, which
// left a window where the key existed with no TTL.
func TestRateLimiter_EstablishesTTLInSameOperationAsCounter(t *testing.T) {
	store := newFakeStore()
	rl := newTestLimiter(store, 5, 30*time.Second)

	if _, _, err := rl.Allow(context.Background(), "subject"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := store.evalCount(); got != 1 {
		t.Errorf("script evaluations = %d, want exactly 1 per Allow", got)
	}

	exp, ok := store.expiryFor("ratelimit:test:subject")
	if !ok {
		t.Fatal("TTL must be set by the same operation that created the counter")
	}
	if d := time.Until(exp); d <= 0 || d > 30*time.Second {
		t.Errorf("TTL = %v, want a positive value up to the 30s window", d)
	}
}

// Under concurrency the limiter must never observe a partially applied
// increment/expiry pair.
// Counters must not lose updates under concurrency. The stronger property, that
// a counter can never be observed without its expiry armed, needs a real Redis
// and is covered by TestIntegrationRateLimit_* in ratelimit_integration_test.go.
func TestRateLimiter_ConcurrentHitsDoNotLoseUpdates(t *testing.T) {
	store := newFakeStore()
	rl := newTestLimiter(store, 50, time.Minute)
	ctx := context.Background()

	const goroutines = 20
	const perGoroutine = 10

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				if _, _, err := rl.Allow(ctx, "shared"); err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	store.mu.Lock()
	count := store.counts["ratelimit:test:shared"]
	store.mu.Unlock()

	if want := int64(goroutines * perGoroutine); count != want {
		t.Errorf("final count = %d, want %d; updates were lost", count, want)
	}
}

func TestRateLimiter_ExactlyLimitSucceedsUnderConcurrency(t *testing.T) {
	store := newFakeStore()
	rl := newTestLimiter(store, 25, time.Minute)
	ctx := context.Background()

	var (
		mu      sync.Mutex
		allowed int
		wg      sync.WaitGroup
	)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _, err := rl.Allow(ctx, "race")
			if err != nil {
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
		t.Errorf("allowed = %d, want exactly the limit of 25", allowed)
	}
}

func TestRateLimiter_PropagatesStoreError(t *testing.T) {
	store := newFakeStore()
	store.failAll = errors.New("connection refused")
	rl := newTestLimiter(store, 5, time.Minute)

	allowed, _, err := rl.Allow(context.Background(), "subject")
	if err == nil {
		t.Fatal("expected an error when the store is unreachable")
	}
	if allowed {
		t.Error("must not report allowed when the store failed")
	}
}

func TestClientIP_UntrustedProxyIgnoresForwardedHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ClientIP(r, false); got != "10.0.0.5" {
		t.Errorf("ClientIP = %q, want the RemoteAddr 10.0.0.5; a spoofed header must be ignored", got)
	}
}

func TestClientIP_TrustedProxyUsesRightMostEntry(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	// The attacker forged the first entry; the trusted proxy appended the real one.
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")

	if got := ClientIP(r, true); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want the right-most entry 203.0.113.9", got)
	}
}

func TestClientIP_TrustedProxyFallsBackOnGarbageHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "not-an-ip")

	if got := ClientIP(r, true); got != "10.0.0.5" {
		t.Errorf("ClientIP = %q, want fallback to RemoteAddr", got)
	}
}

func TestClientIP_TrustedProxySingleEntry(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")

	if got := ClientIP(r, true); got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, want 203.0.113.9", got)
	}
}

func TestJSONSubject(t *testing.T) {
	extract := JSONSubject("email")

	if got := extract([]byte(`{"email":"a@b.com","password":"x"}`)); got != "a@b.com" {
		t.Errorf("got %q, want a@b.com", got)
	}
	for _, body := range []string{`not json`, `{}`, `{"email":123}`, `{"email":""}`, ``} {
		if got := extract([]byte(body)); got != "" {
			t.Errorf("extract(%q) = %q, want empty", body, got)
		}
	}
}

func TestRateLimit_Returns429WithRetryAfter(t *testing.T) {
	store := newFakeStore()
	rl := newTestLimiter(store, 1, 30*time.Second)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})
	limited := RateLimit(rl, nil, false, nil)(handler)

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"a@b.com"}`))
		req.RemoteAddr = "10.0.0.5:1234"
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
		return rec
	}

	if rec := post(); rec.Code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", rec.Code)
	}
	if !handlerCalled {
		t.Fatal("handler should have been called for the first request")
	}

	rec := post()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("429 must carry a Retry-After header")
	} else if got != "30" {
		t.Errorf("Retry-After = %q, want 30", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), CodeRateLimited) {
		t.Errorf("body = %q, want it to contain %q", rec.Body.String(), CodeRateLimited)
	}
}

func TestRateLimit_FailsClosedWhenStoreUnreachable(t *testing.T) {
	store := newFakeStore()
	store.failAll = errors.New("connection refused")
	rl := newTestLimiter(store, 10, time.Minute)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
	})
	limited := RateLimit(rl, nil, false, nil)(handler)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	rec := httptest.NewRecorder()
	limited.ServeHTTP(rec, req)

	if handlerCalled {
		t.Fatal("a limiter that cannot reach Redis must fail closed, not let the request through")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
}

// The per-subject limiter must see the email, and the handler must still be
// able to decode the full body afterwards.
func TestRateLimit_SubjectLimiterReadsEmailAndPreservesBody(t *testing.T) {
	store := newFakeStore()
	ipLimiter := newTestLimiter(store, 100, time.Minute)
	subjectLimiter := newTestLimiter(store, 100, time.Minute)

	var seenBody string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		seenBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	})

	limited := RateLimit(ipLimiter, subjectLimiter, false, JSONSubject("email"))(handler)

	body := `{"email":"victim@example.com","password":"secret12345"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	req.RemoteAddr = "10.0.0.5:1234"
	rec := httptest.NewRecorder()
	limited.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(seenBody, "victim@example.com") ||
		!strings.Contains(seenBody, "secret12345") {
		t.Errorf("handler saw body %q, want the full original body", seenBody)
	}
}

// A body too large to buffer a subject from must not break the request; it
// simply falls back to IP-only limiting.
func TestRateLimit_LargeBodyFallsBackToIPOnly(t *testing.T) {
	store := newFakeStore()
	ipLimiter := newTestLimiter(store, 100, time.Minute)
	subjectLimiter := newTestLimiter(store, 100, time.Minute)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})
	limited := RateLimit(ipLimiter, subjectLimiter, false, JSONSubject("email"))(handler)

	big := `{"email":"` + strings.Repeat("a", subjectBodyLimit+100) + `@b.com"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(big))
	req.RemoteAddr = "10.0.0.5:1234"
	rec := httptest.NewRecorder()
	limited.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Fatal("handler should still run when the subject cannot be extracted")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRateLimit_NilSubjectLimiterSkipsBodyPeek(t *testing.T) {
	store := newFakeStore()
	ipLimiter := newTestLimiter(store, 100, time.Minute)

	var seenBody string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		seenBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	})
	limited := RateLimit(ipLimiter, nil, false, nil)(handler)

	req := httptest.NewRequest(http.MethodPost, "/auth/register",
		strings.NewReader(`{"email":"a@b.com","password":"pw"}`))
	req.RemoteAddr = "10.0.0.5:1234"
	rec := httptest.NewRecorder()
	limited.ServeHTTP(rec, req)

	if !strings.Contains(seenBody, "a@b.com") {
		t.Errorf("handler saw %q, want the body untouched", seenBody)
	}
}
