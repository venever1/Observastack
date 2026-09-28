// Package requestctx holds request-scoped values that more than one package
// needs to read or write.
//
// It exists as a leaf package: it imports nothing from observastack, so
// observability, tenant, and auth can all depend on it. Keeping the key here
// rather than in any one of them is what avoids an import cycle. auth already
// imports observability, and tenant imports auth, so neither of them can be the
// home for a key the other two also need.
//
// The key type is deliberately unexported. The type is the collision guard, so
// callers must go through the accessors below rather than constructing a key
// themselves. A key of type requestctx.contextKey can never equal a plain string
// or a same-named type from another package, which is exactly the bug this
// package exists to prevent.
package requestctx

import "context"

// contextKey is the unexported key type. Its name is never used as a value by
// callers; only the accessors in this file may.
type contextKey string

// tenantIDKey is the single key for the tenant on the current request.
const tenantIDKey contextKey = "tenantID"

// WithTenantID returns a copy of ctx carrying the tenant ID.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// TenantIDFrom returns the tenant ID carried by ctx. ok is false when no tenant
// is set or when the stored value is not a string, which is treated the same as
// absent rather than as an error.
func TenantIDFrom(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantIDKey).(string)
	if !ok || tenantID == "" {
		return "", false
	}
	return tenantID, true
}
