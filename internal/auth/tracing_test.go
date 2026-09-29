package auth

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// installSpanRecorder routes the auth tracer to an in-memory recorder and
// returns the recorder plus a cleanup that restores the previous provider.
func installSpanRecorder(t *testing.T) (*tracetest.SpanRecorder, func()) {
	t.Helper()

	previous := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)

	return recorder, func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	}
}

func spanNames(spans []sdktrace.ReadOnlySpan) map[string]sdktrace.ReadOnlySpan {
	names := make(map[string]sdktrace.ReadOnlySpan, len(spans))
	for _, span := range spans {
		names[span.Name()] = span
	}
	return names
}

// TestLoginEmitsChildSpans proves the login flow no longer collapses into the
// single request span: password verification, access token generation and
// refresh token issuance each get their own child span.
func TestLoginEmitsChildSpans(t *testing.T) {
	recorder, cleanup := installSpanRecorder(t)
	defer cleanup()

	service, _ := newTestService(t)
	if _, err := service.Register(context.Background(), "user@example.com", "password123"); err != nil {
		t.Fatal(err)
	}
	recorder.Reset()

	if _, err := service.Login(context.Background(), "user@example.com", "password123"); err != nil {
		t.Fatal(err)
	}

	names := spanNames(recorder.Ended())
	for _, want := range []string{"auth.verify_password", "auth.generate_access_token", "auth.issue_refresh_token"} {
		span, ok := names[want]
		if !ok {
			t.Fatalf("expected span %q, got %v", want, recorder.Ended())
		}
		if span.Status().Code == codes.Error {
			t.Fatalf("span %q unexpectedly has error status", want)
		}
	}
}

// TestLoginFailedPasswordMarksSpanError proves failures are recorded on the
// span (RecordError + Error status) instead of ending silently.
func TestLoginFailedPasswordMarksSpanError(t *testing.T) {
	recorder, cleanup := installSpanRecorder(t)
	defer cleanup()

	service, _ := newTestService(t)
	if _, err := service.Register(context.Background(), "user@example.com", "password123"); err != nil {
		t.Fatal(err)
	}
	recorder.Reset()

	if _, err := service.Login(context.Background(), "user@example.com", "wrong-password"); err == nil {
		t.Fatal("expected invalid credentials error")
	}

	span, ok := spanNames(recorder.Ended())["auth.verify_password"]
	if !ok {
		t.Fatalf("expected span %q, got %v", "auth.verify_password", recorder.Ended())
	}
	if span.Status().Code != codes.Error {
		t.Fatalf("expected error status on failed password verification, got %v", span.Status().Code)
	}
	if len(span.Events()) == 0 {
		t.Fatal("expected RecordError event on failed password verification")
	}
}

// TestRegisterEmitsChildSpans proves registration traces hashing apart from
// the account-creation transaction.
func TestRegisterEmitsChildSpans(t *testing.T) {
	recorder, cleanup := installSpanRecorder(t)
	defer cleanup()

	service, _ := newTestService(t)
	if _, err := service.Register(context.Background(), "user@example.com", "password123"); err != nil {
		t.Fatal(err)
	}

	names := spanNames(recorder.Ended())
	for _, want := range []string{"auth.hash_password", "auth.create_account"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("expected span %q, got %v", want, recorder.Ended())
		}
	}
}
