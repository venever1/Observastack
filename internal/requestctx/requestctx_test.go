package requestctx

import (
	"context"
	"testing"
)

func TestTenantIDFrom_RoundTrips(t *testing.T) {
	ctx := WithTenantID(context.Background(), "tenant-abc")

	got, ok := TenantIDFrom(ctx)
	if !ok {
		t.Fatal("expected ok after WithTenantID")
	}
	if got != "tenant-abc" {
		t.Fatalf("got %q, want %q", got, "tenant-abc")
	}
}

func TestTenantIDFrom_Absent(t *testing.T) {
	got, ok := TenantIDFrom(context.Background())
	if ok {
		t.Fatal("expected ok=false on a background context")
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// An empty string is not a usable tenant, so it reads the same as unset.
func TestTenantIDFrom_EmptyIsNotPresent(t *testing.T) {
	ctx := WithTenantID(context.Background(), "")

	if got, ok := TenantIDFrom(ctx); ok || got != "" {
		t.Fatalf("got (%q, %v), want (\"\", false) for an empty tenant ID", got, ok)
	}
}

// The guard this package exists for: a plain string key that happens to read
// "tenantID" is a different key and must not be readable through the accessor.
//
// The nolint is deliberate: using a bare string key is the subject under test,
// since proving that key is inert is the entire point of the unexported type.
func TestTenantIDFrom_PlainStringKeyIsNotReadable(t *testing.T) {
	//nolint:staticcheck // SA1029: the bare string key is what we are testing against.
	ctx := context.WithValue(context.Background(), "tenantID", "tenant-abc")

	if got, ok := TenantIDFrom(ctx); ok {
		t.Fatalf("read %q from a plain string key; the typed key must not collide with it", got)
	}
}

// A same-named type from another package is a distinct key too, so two packages
// cannot accidentally agree on context state by both spelling the key "tenantID".
func TestTenantIDFrom_ForeignTypedKeyIsNotReadable(t *testing.T) {
	type contextKey string

	ctx := context.WithValue(context.Background(), contextKey("tenantID"), "tenant-abc")

	if got, ok := TenantIDFrom(ctx); ok {
		t.Fatalf("read %q from a foreign typed key; only requestctx's own key is readable", got)
	}
}

// The accessor must not panic on a value stored under the real key that is not
// a string. Only an in-package test can do this, which is exactly why the key
// is unexported: no other package can put a wrong-typed value under it.
func TestTenantIDFrom_NonStringDoesNotPanic(t *testing.T) {
	for name, value := range map[string]any{
		"int":    42,
		"struct": struct{ ID string }{ID: "tenant-abc"},
		"slice":  []string{"tenant-abc"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), tenantIDKey, value)

			if got, ok := TenantIDFrom(ctx); ok || got != "" {
				t.Fatalf("got (%q, %v), want (\"\", false) for a non-string value", got, ok)
			}
		})
	}
}
