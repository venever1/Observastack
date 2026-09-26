package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testOrigin = "https://app.example.com"

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func TestParseAllowedOrigins(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "empty", raw: "", want: nil},
		{name: "whitespace only", raw: "   ", want: nil},
		{name: "single", raw: testOrigin, want: []string{testOrigin}},
		{
			name: "multiple with spaces",
			raw:  "https://a.example.com, https://b.example.com",
			want: []string{"https://a.example.com", "https://b.example.com"},
		},
		{name: "trailing slash trimmed", raw: "https://a.example.com/", want: []string{"https://a.example.com"}},
		{name: "empty entries skipped", raw: "https://a.example.com,,", want: []string{"https://a.example.com"}},
		{name: "wildcard rejected", raw: "*", wantErr: true},
		{name: "wildcard among list rejected", raw: "https://a.example.com,*", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAllowedOrigins(tc.raw)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %v", tc.raw, got)
				}
				if !strings.Contains(err.Error(), envAllowedOrigins) {
					t.Errorf("error must name the env var, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestAllowedOriginsFromEnv_UnsetIsEmptyNotWildcard(t *testing.T) {
	t.Setenv(envAllowedOrigins, "")

	got, err := AllowedOriginsFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unset must yield no origins, got %v", got)
	}
	for _, o := range got {
		if o == "*" {
			t.Fatal("unset must never fall back to a wildcard")
		}
	}
}

func TestAllowedOriginsFromEnv_InvalidIsFatal(t *testing.T) {
	t.Setenv(envAllowedOrigins, "*")

	if _, err := AllowedOriginsFromEnv(); err == nil {
		t.Fatal("a wildcard must be rejected, not accepted")
	}
}

func TestCORS_AllowedOriginGetsGrant(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	req.Header.Set(headerOrigin, testOrigin)

	CORS([]string{testOrigin})(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get(headerACAllowOrigin); got != testOrigin {
		t.Errorf("%s = %q, want %q", headerACAllowOrigin, got, testOrigin)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestCORS_DisallowedOriginGetsNoGrant(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	req.Header.Set(headerOrigin, "https://evil.example.com")

	CORS([]string{testOrigin})(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get(headerACAllowOrigin); got != "" {
		t.Errorf("%s = %q, want empty for a non-allowlisted origin", headerACAllowOrigin, got)
	}
}

func TestCORS_NoOriginHeaderPassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)

	CORS([]string{testOrigin})(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get(headerACAllowOrigin); got != "" {
		t.Errorf("%s = %q, want empty for a same-origin request", headerACAllowOrigin, got)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// An unset allowlist must block cross-origin preflights outright.
func TestCORS_EmptyAllowlistBlocksPreflight(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/auth/register", nil)
	req.Header.Set(headerOrigin, testOrigin)

	CORS(nil)(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if got := rec.Header().Get(headerACAllowOrigin); got != "" {
		t.Errorf("%s = %q, want empty", headerACAllowOrigin, got)
	}
}

func TestCORS_PreflightFromAllowedOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/auth/register", nil)
	req.Header.Set(headerOrigin, testOrigin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	CORS([]string{testOrigin})(handler).ServeHTTP(rec, req)

	if handlerCalled {
		t.Error("preflight must be answered by the middleware, not the router")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get(headerACAllowOrigin); got != testOrigin {
		t.Errorf("%s = %q, want %q", headerACAllowOrigin, got, testOrigin)
	}
	if got := rec.Header().Get(headerACAllowMethods); !strings.Contains(got, http.MethodPost) {
		t.Errorf("%s = %q, must include POST", headerACAllowMethods, got)
	}
	// Authorization must be allowed, this API uses a bearer token.
	if got := rec.Header().Get(headerACAllowHeaders); !strings.Contains(got, "Authorization") {
		t.Errorf("%s = %q, must include Authorization", headerACAllowHeaders, got)
	}
	if rec.Header().Get(headerACMaxAge) == "" {
		t.Error("preflight must advertise Access-Control-Max-Age")
	}
}

func TestCORS_PreflightFromDisallowedOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/auth/register", nil)
	req.Header.Set(headerOrigin, "https://evil.example.com")

	CORS([]string{testOrigin})(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if got := rec.Header().Get(headerACAllowMethods); got != "" {
		t.Errorf("%s = %q, want empty for a rejected preflight", headerACAllowMethods, got)
	}
}

// Shared caches must not serve one origin's response to another.
func TestCORS_SetsVaryOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	req.Header.Set(headerOrigin, testOrigin)

	CORS([]string{testOrigin})(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get(headerVary); !strings.Contains(got, headerOrigin) {
		t.Errorf("%s = %q, must contain %q", headerVary, got, headerOrigin)
	}
}

// Credentials must never be enabled: the API uses bearer tokens, not cookies.
func TestCORS_NeverEnablesCredentials(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/tasks", nil)
	req.Header.Set(headerOrigin, testOrigin)

	CORS([]string{testOrigin})(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get(headerACAllowCredentials); got != "" {
		t.Errorf("%s = %q, want empty", headerACAllowCredentials, got)
	}
}

func TestSecurityHeaders_SetsAllHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)

	SecurityHeaders(okHandler()).ServeHTTP(rec, req)

	want := map[string]string{
		headerXContentTypeOptions: valueNoSniff,
		headerXFrameOptions:       valueDeny,
		headerStrictTransport:     valueHSTS,
		headerContentSecurityPol:  valueCSP,
		headerReferrerPolicy:      valueReferrer,
	}

	for header, expected := range want {
		if got := rec.Header().Get(header); got != expected {
			t.Errorf("%s = %q, want %q", header, got, expected)
		}
	}
}

// Security headers must survive every failure path, not just 200 responses.
func TestSecurityHeaders_PresentOnErrorResponses(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusRequestEntityTooLarge,
		http.StatusInternalServerError,
	}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/tasks", nil)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})

			SecurityHeaders(handler).ServeHTTP(rec, req)

			if rec.Code != status {
				t.Fatalf("status = %d, want %d", rec.Code, status)
			}
			for _, header := range []string{
				headerXContentTypeOptions,
				headerXFrameOptions,
				headerStrictTransport,
				headerContentSecurityPol,
			} {
				if rec.Header().Get(header) == "" {
					t.Errorf("%s missing on a %d response", header, status)
				}
			}
		})
	}
}

// A 413 produced by the body limit must still carry the security headers,
// which requires SecurityHeaders to sit outside BodyLimit.
func TestSecurityHeaders_SurviveBodyLimitRejection(t *testing.T) {
	handler := SecurityHeaders(CORS([]string{testOrigin})(BodyLimit(32)(okHandler())))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/register",
		strings.NewReader(strings.Repeat("a", 200)))
	req.Header.Set(headerOrigin, testOrigin)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if rec.Header().Get(headerXContentTypeOptions) != valueNoSniff {
		t.Error("security headers missing on a 413 response")
	}
	if rec.Header().Get(headerACAllowOrigin) != testOrigin {
		t.Error("CORS grant missing on a 413 response")
	}
}

// A rejected preflight must still carry the security headers, which requires
// SecurityHeaders to sit outside CORS.
func TestSecurityHeaders_SurviveRejectedPreflight(t *testing.T) {
	handler := SecurityHeaders(CORS([]string{testOrigin})(okHandler()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/auth/register", nil)
	req.Header.Set(headerOrigin, "https://evil.example.com")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if rec.Header().Get(headerXFrameOptions) != valueDeny {
		t.Error("security headers missing on a rejected preflight")
	}
}
