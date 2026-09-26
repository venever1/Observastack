package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// echoHandler drains the body and reports how many bytes it received.
func echoHandler(readErr *error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if readErr != nil {
			*readErr = err
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"read":` + itoa(n) + `}`))
	})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestBodyLimit_BelowLimit(t *testing.T) {
	limit := int64(100)
	payload := strings.Repeat("a", 50)

	var readErr error
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(payload))

	BodyLimit(limit)(echoHandler(&readErr)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if readErr != nil {
		t.Fatalf("expected no read error, got %v", readErr)
	}
}

func TestBodyLimit_ExactlyAtLimit(t *testing.T) {
	limit := int64(100)
	payload := strings.Repeat("a", 100)

	var readErr error
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(payload))

	BodyLimit(limit)(echoHandler(&readErr)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for payload exactly at limit, got %d", rec.Code)
	}
	if readErr != nil {
		t.Fatalf("expected no read error at exactly limit, got %v", readErr)
	}
}

func TestBodyLimit_AboveLimit_ContentLengthKnown(t *testing.T) {
	limit := int64(100)
	payload := strings.Repeat("a", 101)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(payload))

	called := false
	BodyLimit(limit)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	})).ServeHTTP(rec, req)

	if called {
		t.Fatal("handler must not be invoked when Content-Length exceeds limit")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}

	assertPayloadTooLargeJSON(t, rec)
}

// Covers chunked / unknown-length bodies, where the limit can only be detected
// while reading, so the handler observes a *http.MaxBytesError.
func TestBodyLimit_AboveLimit_ContentLengthUnknown(t *testing.T) {
	limit := int64(100)
	payload := strings.Repeat("a", 500)

	var readErr error
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(payload))
	req.ContentLength = -1

	BodyLimit(limit)(echoHandler(&readErr)).ServeHTTP(rec, req)

	if readErr == nil {
		t.Fatal("expected read error when body exceeds limit")
	}
	if !IsBodyTooLarge(readErr) {
		t.Fatalf("expected IsBodyTooLarge to be true, got %v", readErr)
	}
}

func TestBodyLimit_DoesNotLimitGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	BodyLimit(1)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET without body, got %d", rec.Code)
	}
}

func TestMaxRequestBodySize_Defaults(t *testing.T) {
	t.Setenv(envMaxRequestBodySize, "")

	if got := MaxRequestBodySize(); got != DefaultMaxRequestBodySize {
		t.Fatalf("expected default %d, got %d", DefaultMaxRequestBodySize, got)
	}
}

func TestMaxRequestBodySize_FromEnv(t *testing.T) {
	t.Setenv(envMaxRequestBodySize, "2048")

	if got := MaxRequestBodySize(); got != 2048 {
		t.Fatalf("expected 2048, got %d", got)
	}
}

func TestMaxRequestBodySize_InvalidFallsBackToDefault(t *testing.T) {
	for _, raw := range []string{"not-a-number", "0", "-1"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv(envMaxRequestBodySize, raw)

			if got := MaxRequestBodySize(); got != DefaultMaxRequestBodySize {
				t.Fatalf("expected fallback %d for %q, got %d",
					DefaultMaxRequestBodySize, raw, got)
			}
		})
	}
}

func TestIsBodyTooLarge_NegativeCase(t *testing.T) {
	if IsBodyTooLarge(nil) {
		t.Fatal("expected false for nil error")
	}
	if IsBodyTooLarge(io.ErrUnexpectedEOF) {
		t.Fatal("expected false for unrelated error")
	}
}

func assertPayloadTooLargeJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("expected application/json content type, got %q", got)
	}

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response body is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	if payload.Error.Code != CodePayloadTooLarge {
		t.Errorf("expected code %q, got %q", CodePayloadTooLarge, payload.Error.Code)
	}
	if payload.Error.Message == "" {
		t.Error("expected a non-empty error message")
	}
}
