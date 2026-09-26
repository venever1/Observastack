package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuth_ValidToken(t *testing.T) {
	manager, err := NewTokenManager("test-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Generate("user-1")
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID != "user-1" {
			t.Fatalf("user id not in context: %q %v", userID, ok)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	Auth(manager)(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuth_MissingAndInvalidToken(t *testing.T) {
	manager, err := NewTokenManager("test-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	for name, header := range map[string]string{
		"missing": "",
		"bad":     "Token xyz",
		"invalid": "Bearer not-a-token",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()
			Auth(manager)(next).ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401", rec.Code)
			}
		})
	}
}

func TestRequireRole(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithRole(req.Context(), RoleMember))
	rec := httptest.NewRecorder()
	RequireRole(RoleAdmin)(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithRole(req.Context(), RoleAdmin))
	rec = httptest.NewRecorder()
	RequireRole(RoleAdmin)(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
}
