package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"observastack/internal/auth"
)

type stubStore struct {
	roles map[string]string
	err   error
}

func (s *stubStore) GetRole(_ context.Context, tenantID, userID string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	role, ok := s.roles[tenantID+":"+userID]
	if !ok {
		return "", auth.ErrNotMember
	}
	return role, nil
}

func TestResolver_OK(t *testing.T) {
	store := &stubStore{roles: map[string]string{"t1:u1": auth.RoleAdmin}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := TenantIDFromContext(r.Context())
		if !ok || tenantID != "t1" {
			t.Fatalf("tenant not in context: %q %v", tenantID, ok)
		}
		role, ok := auth.RoleFromContext(r.Context())
		if !ok || role != auth.RoleAdmin {
			t.Fatalf("role not in context: %q %v", role, ok)
		}
		w.WriteHeader(http.StatusOK)
	})

	manager := fakeParser{userID: "u1"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	auth.Auth(&manager)(Resolver(store)(next)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestResolver_ForbiddenWhenNotMember(t *testing.T) {
	store := &stubStore{}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	manager := fakeParser{userID: "u1"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-ID", "t9")
	rec := httptest.NewRecorder()
	auth.Auth(&manager)(Resolver(store)(next)).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
}

func TestResolver_MissingTenant(t *testing.T) {
	store := &stubStore{roles: map[string]string{"t1:u1": auth.RoleMember}}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	manager := fakeParser{userID: "u1"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	auth.Auth(&manager)(Resolver(store)(next)).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

type fakeParser struct {
	userID string
	err    error
}

func (f *fakeParser) Parse(_ string) (*auth.Claims, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &auth.Claims{UserID: f.userID}, nil
}
