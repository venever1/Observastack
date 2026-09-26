package observability

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

type Logger struct {
	*log.Logger
}

func NewLogger() *Logger {
	return &Logger{
		Logger: log.New(os.Stderr, "", 0),
	}
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	TraceID   string `json:"trace_id"`
	TenantID  string `json:"tenant_id"`
	Level     string `json:"level"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Message   string `json:"message"`
	Error     string `json:"error,omitempty"`
}

func (l *Logger) Info(ctx context.Context, level, method, path string, status int, latency time.Duration, msg string) {
	entry := l.newEntry(ctx, level, method, path, status, latency, msg)
	l.logEntry(entry)
}

func (l *Logger) Error(ctx context.Context, method, path string, status int, latency time.Duration, msg string, err error) {
	entry := l.newEntry(ctx, "ERROR", method, path, status, latency, msg)
	if err != nil {
		entry.Error = err.Error()
	}
	l.logEntry(entry)
}

func (l *Logger) newEntry(ctx context.Context, level, method, path string, status int, latency time.Duration, msg string) LogEntry {
	tenantID := ctx.Value("tenantID")
	tenantIDStr := ""
	if tenantID != nil {
		tenantIDStr = tenantID.(string)
	}

	return LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TraceID:   TraceIDFromContext(ctx),
		TenantID:  tenantIDStr,
		Level:     level,
		Method:    method,
		Path:      path,
		Status:    status,
		LatencyMs: latency.Milliseconds(),
		Message:   msg,
	}
}

func (l *Logger) logEntry(entry LogEntry) {
	b, _ := json.Marshal(entry)
	l.Println(string(b))
}

func LogMiddleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			latency := time.Since(start)

			status := rec.status
			if status >= 500 {
				logger.Error(r.Context(), r.Method, r.URL.Path, status, latency, "internal server error", nil)
			} else {
				logger.Info(r.Context(), "INFO", r.Method, r.URL.Path, status, latency, "request completed")
			}
		})
	}
}
