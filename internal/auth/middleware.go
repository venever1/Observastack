package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

const (
	headerAuthorization = "Authorization"
	bearerScheme        = "Bearer "
)

type contextKey string

const (
	userIDKey contextKey = "userID"
)

type TokenParser interface {
	Parse(tokenString string) (*Claims, error)
}

// UserIDFromContext returns the authenticated user ID stored by Auth.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey).(string)
	return userID, ok && userID != ""
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

// writeJSON encodes v with encoding/json. Going through the encoder rather than
// string interpolation is what makes quotes, backslashes, newlines and
// non-ASCII characters in user input safe to echo back.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Auth verifies the Bearer JWT in the Authorization header and stores the user ID in context.
func Auth(parser TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString, ok := bearerToken(r.Header.Get(headerAuthorization))
			if !ok {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing bearer token")
				return
			}

			claims, err := parser.Parse(tokenString)
			if err != nil || claims.UserID == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Token invalid/expired")
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(header string) (string, bool) {
	if !strings.HasPrefix(header, bearerScheme) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, bearerScheme))
	if token == "" {
		return "", false
	}
	return token, true
}
