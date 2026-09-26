package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"observastack/internal/auth"
	"observastack/internal/config"
	"observastack/internal/middleware"
	"observastack/internal/observability"
)

// Server timeouts bound how long a client may hold a connection.
// ReadHeaderTimeout caps the Slowloris window (headers sent slowly).
// ReadTimeout covers headers plus the whole body read; WriteTimeout bounds the
// response. None of the current endpoints need longer: /healthz and /readyz are
// a status write and a DB ping, /metrics is a Prometheus scrape, and
// /auth/register is a JSON decode plus one bcrypt hash (~250ms at cost 12).
// There is no file-upload route today, so these values are not restrictive.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 120 * time.Second
)

func main() {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	db, err := config.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	log.Printf("✓ postgres connected")

	tokens, err := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTAccessTTL)
	if err != nil {
		log.Fatalf("create token manager: %v", err)
	}
	log.Printf("✓ token manager created")

	_ = tokens
	_ = db

	tracerProvider := observability.InitJaeger("observastack")
	if tracerProvider == nil {
		log.Printf("warning: tracer provider not initialized, tracing disabled")
	} else {
		defer tracerProvider.Shutdown(ctx)
		log.Printf("✓ tracer provider initialized")
	}
	logger := observability.NewLogger()
	log.Printf("✓ structured logger initialized")

	mux := http.NewServeMux()

	// Untraced: health check & readiness probe
	mux.Handle("GET /healthz", observability.LogMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})))

	mux.Handle("GET /readyz", observability.LogMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not ready","error":"database down"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})))

	// Traced critical routes (2): /metrics + /auth/register
	// Order per route: Tracer (outer) -> Log (inner, sees span) -> handler
	mux.Handle("GET /metrics", observability.TracerMiddleware(observability.LogMiddleware(logger)(observability.Handler())))
	mux.Handle("POST /auth/register", observability.TracerMiddleware(observability.LogMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(os.Stderr, "INLINE HANDLER CALLED\n")
		os.Stderr.Sync()
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if middleware.IsBodyTooLarge(err) {
				fmt.Fprintf(os.Stderr, "BODY TOO LARGE: %v\n", err)
				os.Stderr.Sync()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				fmt.Fprintf(w, `{"error":{"code":%q,"message":"Request body exceeds maximum allowed size"}}`,
					middleware.CodePayloadTooLarge)
				return
			}
			fmt.Fprintf(os.Stderr, "DECODE ERROR: %v\n", err)
			os.Stderr.Sync()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":"decode failed: %v"}`, err)
			return
		}
		fmt.Fprintf(os.Stderr, "DECODED: email=%q password=[REDACTED]\n", req.Email)
		os.Stderr.Sync()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"data":{"email":"%s"}}`, req.Email)
	}))))

	bodyLimit, err := middleware.MaxRequestBodySize()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("listening on :%s (max request body %d bytes)", port, bodyLimit)
	handler := middleware.BodyLimit(bodyLimit)(mux)

	server := newServer(":"+port, observability.Middleware(handler))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// newServer builds the HTTP server with defensive timeouts. Exposed as a
// separate constructor so the timeout values stay unit-testable.
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}
