package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_ServesPrometheusMetrics(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "go_goroutines") {
		t.Fatal("missing go_goroutines in /metrics output")
	}
}

func TestMiddleware_CountsRequests(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(http.MethodGet, "/probe-middleware-count", nil)
	rec := httptest.NewRecorder()
	Middleware(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("got %d, want 418", rec.Code)
	}

	mreq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mrec := httptest.NewRecorder()
	Handler().ServeHTTP(mrec, mreq)
	body := mrec.Body.String()
	if !strings.Contains(body, `http_requests_total{method="GET",path="/probe-middleware-count",status="418"} 1`) {
		t.Fatalf("counter missing:\n%s", body)
	}
	if !strings.Contains(body, `http_request_duration_seconds_count{method="GET",path="/probe-middleware-count",status="418"} 1`) {
		t.Fatalf("histogram missing:\n%s", body)
	}
}
