// Package middleware provides cross-cutting HTTP middleware that is not tied to
// a single business domain.
package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	// DefaultMaxRequestBodySize caps request bodies when MAX_REQUEST_BODY_SIZE is unset.
	DefaultMaxRequestBodySize int64 = 1 << 20 // 1 MiB

	envMaxRequestBodySize = "MAX_REQUEST_BODY_SIZE"

	// CodePayloadTooLarge is the API error code returned when a body exceeds the limit.
	CodePayloadTooLarge = "PAYLOAD_TOO_LARGE"
)

// MaxRequestBodySize resolves the request body limit from MAX_REQUEST_BODY_SIZE,
// falling back to DefaultMaxRequestBodySize only when the variable is unset.
//
// A set-but-invalid value is an operator error and returns an error rather than
// silently falling back, so a typo cannot be mistaken for a working config.
func MaxRequestBodySize() (int64, error) {
	raw := os.Getenv(envMaxRequestBodySize)
	if raw == "" {
		return DefaultMaxRequestBodySize, nil
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf(
			"%s is set but empty; expected a plain integer number of bytes (no unit suffixes such as \"10MB\")",
			envMaxRequestBodySize)
	}

	size, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf(
			"%s must be a plain integer number of bytes (no unit suffixes such as \"10MB\"), got %q",
			envMaxRequestBodySize, raw)
	}
	if size <= 0 {
		return 0, fmt.Errorf(
			"%s must be a plain integer number of bytes greater than zero, got %d",
			envMaxRequestBodySize, size)
	}

	return size, nil
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

// writePayloadError writes an error response in the docs/API_SPEC.md envelope.
func writePayloadError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writePayloadTooLarge(w http.ResponseWriter, limit int64) {
	writePayloadError(w, http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
		"Request body exceeds maximum allowed size of "+strconv.FormatInt(limit, 10)+" bytes")
}
