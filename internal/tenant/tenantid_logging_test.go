package tenant_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"observastack/internal/auth"
	"observastack/internal/observability"
	"observastack/internal/tenant"
)

// stubStore reports every caller as an admin of the requested tenant.
type stubStore struct{}

func (stubStore) GetRole(_ context.Context, _, _ string) (string, error) {
	return auth.RoleAdmin, nil
}

// stubParser yields a fixed user ID so auth.Auth populates its context key.
type stubParser struct{}

func (stubParser) Parse(_ string) (*auth.Claims, error) {
	return &auth.Claims{UserID: "user-1"}, nil
}

// This is the test that fails before the fix.
//
// tenant.Resolver writes the tenant with its own typed key, while the logger
// used to read a bare "tenantID" string. Those are different keys, so tenant_id
// was always empty in the log. A test where both sides agree on a hand-made
// string key cannot catch that, because it never touches the real producer and
// consumer. This one drives the real Resolver and the real logger.
func TestResolver_TenantIDReachesAccessLog(t *testing.T) {
	logger := observability.NewLogger()
	var buf strings.Builder
	logger.SetOutput(&buf)

	var tenantSeenByHandler string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantSeenByHandler, _ = tenant.TenantIDFromContext(r.Context())

		observability.LogMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(w, r)
	})

	// auth.Auth supplies the user ID the resolver requires; the resolver then
	// supplies the tenant the logger has to read. Full chain, as in production.
	handler := auth.Auth(stubParser{})(tenant.Resolver(stubStore{})(next))

	req := httptest.NewRequest(http.MethodGet, "/things", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-ID", "tenant-abc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	if tenantSeenByHandler != "tenant-abc" {
		t.Fatalf("resolver did not put the tenant in context: got %q", tenantSeenByHandler)
	}

	var entry observability.LogEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &entry); err != nil {
		t.Fatalf("decode log entry %q: %v", buf.String(), err)
	}

	if entry.TenantID != "tenant-abc" {
		t.Fatalf("log tenant_id = %q, want %q; the logger is not reading the key the resolver writes", entry.TenantID, "tenant-abc")
	}
}
