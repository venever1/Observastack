// Package middleware provides cross-cutting HTTP middleware that is not tied to
// a single business domain.
package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
)

const (
	// DefaultMaxRequestBodySize caps request bodies when MAX_REQUEST_BODY_SIZE is unset.
	DefaultMaxRequestBodySize int64 = 1 << 20 // 1 MiB

	envMaxRequestBodySize = "MAX_REQUEST_BODY_SIZE"

	// CodePayloadTooLarge is the API error code returned when a body exceeds the limit.
	CodePayloadTooLarge = "PAYLOAD_TOO_LARGE"
)

// MaxRequestBodySize resolves the request body limit from MAX_REQUEST_BODY_SIZE,
// falling back to DefaultMaxRequestBodySize when unset or unparsable.
func MaxRequestBodySize() int64 {
	raw := os.Getenv(envMaxRequestBodySize)
	if raw == "" {
		return DefaultMaxRequestBodySize
	}

	size, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || size <= 0 {
		return DefaultMaxRequestBodySize
	}

	return size
}

// BodyLimit returns middleware that caps request bodies at limit bytes.
//
// A declared Content-Length over the limit is rejected with 413 before the body
// is read at all. Otherwise the body is wrapped in http.MaxBytesReader so
// chunked or unknown-length requests stay memory-bounded too.
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				writePayloadTooLarge(w, limit)
				return
			}

			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IsBodyTooLarge reports whether err was caused by the request body exceeding
// the limit enforced by BodyLimit. Handlers that decode the body should map
// this to a 413 instead of a generic decode failure.
func IsBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func writePayloadTooLarge(w http.ResponseWriter, limit int64) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusRequestEntityTooLarge)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code": CodePayloadTooLarge,
			"message": "Request body exceeds maximum allowed size of " +
				strconv.FormatInt(limit, 10) + " bytes",
		},
	})
}
