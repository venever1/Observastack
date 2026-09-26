package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"observastack/internal/auth"
)

type contextKey string

const tenantIDKey contextKey = "tenantID"

type MemberStore interface {
	GetRole(ctx context.Context, tenantID, userID string) (string, error)
}

func TenantIDFromContext(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantIDKey).(string)
	return tenantID, ok && tenantID != ""
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

// Resolver extracts tenant from X-Tenant-ID (or :tenantId route param in the future)
// and verifies the caller is a member of that tenant, storing tenant id + role in context.
func Resolver(store MemberStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.UserIDFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing auth context")
				return
			}

			tenantID := r.Header.Get("X-Tenant-ID")
			if tenantID == "" {
				tenantID = r.PathValue("tenantId")
			}
			if tenantID == "" {
				writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Missing tenant context")
				return
			}

			role, err := store.GetRole(r.Context(), tenantID, userID)
			if err != nil {
				if errors.Is(err, auth.ErrNotMember) {
					writeError(w, http.StatusForbidden, "TENANT_MISMATCH", "Resource bukan milik tenant ini")
					return
				}
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Error tak terduga")
				return
			}

			ctx := context.WithValue(r.Context(), tenantIDKey, tenantID)
			ctx = auth.WithRole(ctx, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
