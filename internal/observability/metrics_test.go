package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// metricValue returns the current value of the http_requests_total series for
// the given labels. The counter is process-global (registered with promauto on
// the default registry), so tests must compare a delta rather than an absolute
// value: anything else breaks under -count=2 and under any test that shares the
// same label set.
func metricValue(t *testing.T, labels ...string) float64 {
	t.Helper()
	c, err := requestsTotal.GetMetricWithLabelValues(labels...)
	if err != nil {
		t.Fatalf("get counter: %v", err)
	}
	return testutil.ToFloat64(c)
}

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
	labels := []string{http.MethodGet, "/probe-middleware-count", "418"}
	before := metricValue(t, labels...)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(http.MethodGet, "/probe-middleware-count", nil)
	rec := httptest.NewRecorder()
	Middleware(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("got %d, want 418", rec.Code)
	}

	if got := metricValue(t, labels...) - before; got != 1 {
		t.Fatalf("counter delta = %v, want 1", got)
	}

	mreq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mrec := httptest.NewRecorder()
	Handler().ServeHTTP(mrec, mreq)
	if body := mrec.Body.String(); !strings.Contains(body, `http_request_duration_seconds_count{method="GET",path="/probe-middleware-count",status="418"}`) {
		t.Fatalf("histogram missing:\n%s", body)
	}
}
