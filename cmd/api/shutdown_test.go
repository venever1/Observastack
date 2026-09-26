package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// newLoopbackServer binds srv to a free loopback port and begins serving.
// It returns the base URL and a stop func.
func newLoopbackServer(t *testing.T, srv *http.Server, handler http.Handler) (string, net.Listener) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv.Handler = handler
	go func() { _ = srv.Serve(listener) }()

	return "http://" + listener.Addr().String(), listener
}

// muxWithReady returns a handler that answers /ready immediately and delegates
// everything else to slow. Readiness probes must not block on slow.
func muxWithReady(slow http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", slow)
	return mux
}

func waitForServer(t *testing.T, baseURL string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/ready")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
}

func get(t *testing.T, url string) (int, error) {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}

func TestServeUntilShutdown_DrainsInflightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	handler := muxWithReady(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})

	srv := newServer("", handler)
	baseURL, listener := newLoopbackServer(t, srv, handler)
	waitForServer(t, baseURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		status int
		err    error
	}
	reqDone := make(chan result, 1)
	go func() {
		status, err := get(t, baseURL+"/slow")
		reqDone <- result{status, err}
	}()

	<-started

	// Signal shutdown while the request is still in flight.
	cancel()

	// Only now let the handler finish: the shutdown must be waiting on it,
	// not killing it.
	close(release)

	select {
	case res := <-reqDone:
		if res.err != nil {
			t.Fatalf("in-flight request should complete, got error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("in-flight request status = %d, want %d", res.status, http.StatusOK)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight request never completed")
	}

	if err := serveUntilShutdown(ctx, srv, listener, 2*time.Second); err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}
}

func TestServeUntilShutdown_StopsAcceptingNewRequests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := newServer("", handler)
	baseURL, listener := newLoopbackServer(t, srv, handler)
	waitForServer(t, baseURL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already signalled

	if err := serveUntilShutdown(ctx, srv, listener, time.Second); err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}

	// Shutdown closes the listener, so new connections must be refused.
	if _, err := get(t, baseURL+"/ready"); err == nil {
		t.Fatal("expected connection refused after shutdown, but request succeeded")
	}
}

func TestServeUntilShutdown_ForceClosesOnTimeout(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)

	handler := muxWithReady(func(http.ResponseWriter, *http.Request) {
		<-blocked
	})

	srv := newServer("", handler)
	baseURL, listener := newLoopbackServer(t, srv, handler)
	waitForServer(t, baseURL)

	// Start a request that will never finish.
	stuck := make(chan struct{})
	go func() {
		defer close(stuck)
		_, _ = get(t, baseURL+"/stuck")
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel() // already signalled

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- serveUntilShutdown(ctx, srv, listener, 150*time.Millisecond)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("force-close path should not surface an error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serveUntilShutdown hung on a stuck handler")
	}

	// It must bail out near its own deadline, not wait for the handler.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("shutdown took %v, expected it to bail out near the 150ms timeout", elapsed)
	}

	// Force close must have dropped the stuck request.
	select {
	case <-stuck:
	case <-time.After(2 * time.Second):
		t.Error("stuck request was not released by the forced close")
	}
}

func TestServeUntilShutdown_SwallowsErrServerClosed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := newServer("", handler)
	_, listener := newLoopbackServer(t, srv, handler)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := serveUntilShutdown(ctx, srv, listener, time.Second)
	if err != nil {
		t.Fatalf("a deliberate shutdown must not surface http.ErrServerClosed, got %v", err)
	}
}

func TestServeUntilShutdown_ReturnsServeError(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := closed.Addr().String()
	if err := closed.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	srv := newServer(addr, http.NotFoundHandler())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveErr := serveUntilShutdown(ctx, srv, closed, time.Second)
	if serveErr == nil {
		t.Fatal("expected an error from Serve on a closed listener")
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		t.Fatalf("ErrServerClosed should be swallowed, got %v", serveErr)
	}
}

func TestRun_AlwaysCallsCleanupExactlyOnce(t *testing.T) {
	var mu sync.Mutex
	calls := 0

	cleanup := func(ctx context.Context) {
		mu.Lock()
		defer mu.Unlock()
		if ctx == nil {
			t.Error("cleanup must receive a non-nil context")
		}
		calls++
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := newServer("", handler)
	baseURL, listener := newLoopbackServer(t, srv, handler)
	waitForServer(t, baseURL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, srv, listener, cleanup); err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("cleanup called %d times, want exactly 1", calls)
	}
}

// The signal context is already cancelled when cleanup runs, so run must hand
// cleanup a fresh one or the tracer exporter cannot flush its spans.
func TestRun_CleanupReceivesLiveContext(t *testing.T) {
	var sawCancelled bool

	cleanup := func(ctx context.Context) {
		if ctx.Err() != nil {
			sawCancelled = true
		}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := newServer("", handler)
	baseURL, listener := newLoopbackServer(t, srv, handler)
	waitForServer(t, baseURL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, srv, listener, cleanup); err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}

	if sawCancelled {
		t.Fatal("cleanup was handed an already-cancelled context")
	}
}

// Ordering guarantee: resources are released only after the server has stopped
// accepting and in-flight work has drained.
func TestRun_CleanupRunsAfterServerStops(t *testing.T) {
	var mu sync.Mutex
	var order []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := newServer("", handler)
	baseURL, listener := newLoopbackServer(t, srv, handler)
	waitForServer(t, baseURL)

	cleanup := func(context.Context) {
		mu.Lock()
		defer mu.Unlock()
		// The listener must already be closed by the time cleanup runs.
		if _, err := get(t, baseURL+"/ready"); err == nil {
			t.Error("server was still accepting requests when cleanup ran")
		}
		order = append(order, "cleanup")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, srv, listener, cleanup); err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 1 || order[0] != "cleanup" {
		t.Fatalf("unexpected cleanup ordering: %v", order)
	}
}
