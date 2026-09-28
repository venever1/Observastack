package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"observastack/internal/requestctx"
)

func TestNewTracerProvider_CreatesProvider(t *testing.T) {
	provider := InitJaeger("test")
	if provider == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	// Shutdown flushes pending spans and closes the exporter. A failure here
	// means the exporter could not drain, which is worth surfacing but must not
	// mask the assertion this test exists to make.
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Logf("tracer shutdown: %v", err)
	}
}

func TestTraceIDFromContext_ExtractsFromSpan(t *testing.T) {
	provider := InitJaeger("test")
	defer func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Logf("tracer shutdown: %v", err)
		}
	}()

	ctx, span := provider.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		t.Fatal("expected non-empty trace_id from span")
	}
	if traceID != span.SpanContext().TraceID().String() {
		t.Fatalf("got %q, want span trace_id", traceID)
	}
}

func TestTraceIDFromContext_EmptyWhenNoSpan(t *testing.T) {
	ctx := context.Background()
	traceID := TraceIDFromContext(ctx)
	if traceID != "" {
		t.Fatalf("expected empty trace_id, got %q", traceID)
	}
}

func TestLogMiddleware_CapturesRequest(t *testing.T) {
	logger := NewLogger()

	var buf strings.Builder
	logger.SetOutput(&buf)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test-path", nil)
	req = req.WithContext(requestctx.WithTenantID(req.Context(), "tenant-abc"))

	LogMiddleware(logger)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}

	output := buf.String()
	if !strings.Contains(output, "request completed") {
		t.Fatalf("middleware should log request, got: %s", output)
	}
	if !strings.Contains(output, "tenant_id") {
		t.Fatalf("log should contain tenant_id, got: %s", output)
	}
}

func TestLogMiddleware_traceIDCorrelation(t *testing.T) {
	provider := InitJaeger("test")
	defer func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Logf("tracer shutdown: %v", err)
		}
	}()

	logger := NewLogger()

	var buf strings.Builder
	logger.SetOutput(&buf)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx, span := provider.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()
	traceID := span.SpanContext().TraceID().String()
	ctx = requestctx.WithTenantID(ctx, "tenant-abc")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test-path", nil)
	req = req.WithContext(ctx)

	LogMiddleware(logger)(next).ServeHTTP(rec, req)

	output := buf.String()
	if !strings.Contains(output, "trace_id") {
		t.Fatalf("log should contain trace_id, got: %s", output)
	}
	if !strings.Contains(output, traceID) {
		t.Fatalf("log trace_id should match span trace_id %s, got: %s", traceID, output)
	}
}
