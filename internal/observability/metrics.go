package observability

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)
	requestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "HTTP request duration in seconds.",
		},
		[]string{"method", "path", "status"},
	)
)

func Handler() http.Handler {
	return promhttp.Handler()
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if v := recover(); v != nil {
				// recover() yields any, not error; wrap so the value is logged
				// even when the handler panicked with a non-error value.
				err := fmt.Errorf("panic: %v", v)
				rec.status = http.StatusInternalServerError
				defaultLogger.Error(r.Context(), r.Method, r.URL.Path,
					rec.status, time.Since(start), "panic in handler", err)
				panic(v)
			}
		}()

		next.ServeHTTP(rec, r)
		status := strconv.Itoa(rec.status)
		path := normalizeRoutePath(r.URL.Path)
		requestsTotal.WithLabelValues(r.Method, path, status).Inc()
		requestDuration.WithLabelValues(r.Method, path, status).Observe(time.Since(start).Seconds())
	})
}

func normalizeRoutePath(path string) string {
	// Normalize /tasks/123 → /tasks/:id pattern to avoid high cardinality
	// ponytail: add pattern-based routing when chi/gorilla/mux adopted
	if len(path) > 1 && path[len(path)-1] != '/' {
		// check if last segment is UUID or numeric ID
		parts := strings.Split(path, "/")
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			// if last segment looks like UUID (36 chars with dashes) or all digits, normalize to :id
			if (len(last) == 36 && strings.Count(last, "-") == 4) || isNumeric(last) {
				return strings.Join(parts[:len(parts)-1], "/") + "/:id"
			}
		}
	}
	return path
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
