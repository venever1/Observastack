package auth

import (
	"context"
	"net/http"
)

type roleContextKey string

const (
	userRoleKey roleContextKey = "role"
)

const (
	RoleAny    = "any"
	RoleMember = "member"
	RoleAdmin  = "admin"
)

// RoleFromContext returns the role stored by RequireRole.
func RoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(userRoleKey).(string)
	return role, ok && role != ""
}

func roleAllowed(have, want string) bool {
	if want == "" || want == RoleAny {
		return true
	}
	if want == RoleAdmin {
		return have == RoleAdmin
	}
	return have == RoleMember || have == RoleAdmin
}

// RequireRole rejects requests whose context role does not satisfy want.
// Use RoleAny for endpoints where any authenticated role may pass.
func RequireRole(want string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := RoleFromContext(r.Context())
			if !ok || !roleAllowed(role, want) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "Role tidak cukup")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithRole stores a role in context. Used by the tenant resolver after loading membership.
func WithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, userRoleKey, role)
}
