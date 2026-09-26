package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
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

	// shutdownTimeout bounds how long in-flight requests may keep running after
	// SIGINT/SIGTERM before their connections are force-closed. It is kept above
	// readTimeout so a request already reading a body is not cut off mid-read.
	shutdownTimeout = 15 * time.Second
)

// apiError is the error object defined in docs/API_SPEC.md.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiErrorResponse struct {
	Error apiError `json:"error"`
}

type registerData struct {
	Email string `json:"email"`
}

type registerResponse struct {
	Data registerData `json:"data"`
}

// codeValidationError is the API_SPEC error code for a malformed request body.
const codeValidationError = "VALIDATION_ERROR"

// envAllowedOriginsName is referenced in startup logs so operators know exactly
// which variable to set when cross-origin access is being blocked.
const envAllowedOriginsName = "ALLOWED_ORIGINS"

// writeJSON encodes v with encoding/json. Going through the encoder rather than
// string interpolation is what makes quotes, backslashes, newlines and
// non-ASCII characters in user input safe to echo back.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// newRegisterHandler handles POST /auth/register.
//
// Internal error detail is logged server-side through the structured logger and
// never echoed to the client; the response carries only a generic message inside
// the API_SPEC error envelope.
func newRegisterHandler(logger *observability.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if middleware.IsBodyTooLarge(err) {
				logger.Error(r.Context(), r.Method, r.URL.Path, http.StatusRequestEntityTooLarge,
					time.Since(start), "request body too large", err)
				writeJSON(w, http.StatusRequestEntityTooLarge, apiErrorResponse{
					Error: apiError{
						Code:    middleware.CodePayloadTooLarge,
						Message: "Request body exceeds maximum allowed size",
					},
				})
				return
			}

			logger.Error(r.Context(), r.Method, r.URL.Path, http.StatusBadRequest,
				time.Since(start), "decode request body", err)
			writeJSON(w, http.StatusBadRequest, apiErrorResponse{
				Error: apiError{
					Code:    codeValidationError,
					Message: "Invalid request body",
				},
			})
			return
		}

		// Email is masked and the password is never logged.
		logger.Info(r.Context(), "INFO", r.Method, r.URL.Path, http.StatusOK,
			time.Since(start), "registration received for "+observability.MaskEmail(req.Email))

		writeJSON(w, http.StatusOK, registerResponse{
			Data: registerData{Email: req.Email},
		})
	}
}

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
	mux.Handle("POST /auth/register", observability.TracerMiddleware(observability.LogMiddleware(logger)(newRegisterHandler(logger))))

	bodyLimit, err := middleware.MaxRequestBodySize()
	if err != nil {
		log.Fatal(err)
	}

	allowedOrigins, err := middleware.AllowedOriginsFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if len(allowedOrigins) == 0 {
		log.Printf("warning: %s not set, cross-origin requests will be blocked", envAllowedOriginsName)
	} else {
		log.Printf("allowed CORS origins: %s", strings.Join(allowedOrigins, ", "))
	}

	log.Printf("listening on :%s (max request body %d bytes)", port, bodyLimit)

	// Middleware chain, outermost first:
	//
	//   SecurityHeaders -> CORS -> observability -> BodyLimit -> mux
	//
	// SecurityHeaders is outermost so every response carries the headers,
	// including CORS preflight rejections and the 413 produced by BodyLimit.
	//
	// CORS sits outside observability so a preflight is answered without
	// creating a trace span or a latency sample. The trade-off is that preflights
	// do not appear in Prometheus or the access log; real requests still do.
	//
	// BodyLimit stays innermost, adjacent to the router, so an oversized body is
	// rejected before any handler reads it. Its 413 response still travels back
	// out through observability (so it is counted) and through CORS and
	// SecurityHeaders (so it carries the correct headers).
	handler := middleware.BodyLimit(bodyLimit)(mux)
	handler = observability.Middleware(handler)
	handler = middleware.CORS(allowedOrigins)(handler)
	handler = middleware.SecurityHeaders(handler)

	server := newServer(":"+port, handler)

	// SIGINT/SIGTERM cancel signalCtx, which drains the server before exit.
	// Resources are released explicitly via cleanup rather than with defer,
	// because log.Fatal calls os.Exit and would skip every deferred call.
	signalCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatalf("listen on %s: %v", server.Addr, err)
	}

	cleanup := func(flushCtx context.Context) {
		// Order matters: stop new work at the DB first, then flush spans that
		// were recorded while handling the requests that just drained.
		db.Close()
		log.Printf("✓ postgres pool closed")

		if tracerProvider != nil {
			if err := tracerProvider.Shutdown(flushCtx); err != nil {
				log.Printf("tracer shutdown: %v", err)
			} else {
				log.Printf("✓ tracer provider flushed")
			}
		}
	}

	if err := run(signalCtx, server, listener, cleanup); err != nil {
		log.Fatal(err)
	}
	log.Printf("shutdown complete")
}

// run serves until ctx is cancelled or the server fails, then releases resources
// through cleanup. cleanup is always invoked, including on the serve-error path.
func run(ctx context.Context, srv *http.Server, listener net.Listener, cleanup func(context.Context)) error {
	serveErr := serveUntilShutdown(ctx, srv, listener, shutdownTimeout)

	// cleanup needs a live context: ctx is already cancelled by this point and
	// the tracer exporter cannot flush with a done context.
	flushCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	cleanup(flushCtx)

	return serveErr
}

// serveUntilShutdown runs srv on listener in a goroutine and blocks until ctx is
// cancelled or the server stops on its own.
//
// On cancellation it calls srv.Shutdown, which stops accepting new connections
// and waits for in-flight requests up to timeout. Any connection still hanging
// around when that expires is force-closed so the process can exit.
func serveUntilShutdown(ctx context.Context, srv *http.Server, listener net.Listener, timeout time.Duration) error {
	serveErrCh := make(chan error, 1)
	go func() {
		// http.ErrServerClosed is the expected result of a deliberate shutdown,
		// not a failure.
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
			return
		}
		serveErrCh <- nil
	}()

	select {
	case err := <-serveErrCh:
		return err
	case <-ctx.Done():
		log.Printf("shutdown signal received, draining for up to %s", timeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown incomplete (%v), forcing close", err)
		_ = srv.Close()
	}

	return <-serveErrCh
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
