package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"observastack/internal/auth"
	"observastack/internal/config"
	"observastack/internal/observability"
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

	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, observability.Middleware(mux)); err != nil {
		log.Fatal(err)
	}
}
