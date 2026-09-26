package auth

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"observastack/internal/middleware"
	"observastack/internal/observability"
)

// newTestHandler builds a Handler whose log output is captured in buf, so tests
// can assert on exactly what would reach stderr.
func newTestHandler(t *testing.T) (*Handler, *bytes.Buffer) {
	t.Helper()

	service, _ := newTestService(t)
	buf := &bytes.Buffer{}
	logger := &observability.Logger{Logger: log.New(buf, "", 0)}

	return NewHandler(service, logger), buf
}

func callRegister(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Register(rec, req)

	return rec
}

// The password must never appear in the logs, on any path.
func TestRegisterHandler_NeverLogsPassword(t *testing.T) {
	const password = "sup3rs3cretp@ss"

	cases := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "successful registration",
			body:       `{"email":"alice@example.com","password":"` + password + `"}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "malformed json decode failure",
			body:       `{"email":"alice@example.com","password":"` + password + `"`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "empty body",
			body:       ``,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "password too short",
			body:       `{"email":"alice@example.com","password":"` + password[:4] + `"}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid email",
			body:       `{"email":"not-an-email","password":"` + password + `"}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "password with json-breaking characters",
			body:       `{"email":"bob@example.com","password":"` + password + `\",\"role\":\"admin"}`,
			wantStatus: http.StatusCreated,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, buf := newTestHandler(t)
			rec := callRegister(t, h, tc.body)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%q)", rec.Code, tc.wantStatus, rec.Body.String())
			}

			logged := buf.String()
			if strings.Contains(logged, password) {
				t.Errorf("password leaked into logs: %q", logged)
			}
			if strings.Contains(logged, password[:4]) && tc.name == "password too short" {
				t.Errorf("partial password leaked into logs: %q", logged)
			}
			// The literal field name must not be logged either.
			if strings.Contains(logged, "password=") {
				t.Errorf("password field logged: %q", logged)
			}
		})
	}
}

// A duplicate registration must not echo the password of either attempt.
func TestRegisterHandler_NeverLogsPasswordOnConflict(t *testing.T) {
	const password = "d0notl3ak3this"

	h, buf := newTestHandler(t)

	first := callRegister(t, h, `{"email":"dup@example.com","password":"`+password+`"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusCreated)
	}

	second := callRegister(t, h, `{"email":"dup@example.com","password":"`+password+`"}`)
	if second.Code != http.StatusConflict {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusConflict)
	}

	if strings.Contains(buf.String(), password) {
		t.Errorf("password leaked into logs: %q", buf.String())
	}
}

// Passwords must not be recoverable from any substring of the log output.
func TestRegisterHandler_NoPasswordSubstringInLogs(t *testing.T) {
	const password = "Tr0ub4dor&3xyz"

	h, buf := newTestHandler(t)
	rec := callRegister(t, h, `{"email":"sub@example.com","password":"`+password+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	logged := buf.String()
	// Check every substring of meaningful length, not just the whole value.
	for i := 0; i+6 <= len(password); i++ {
		fragment := password[i : i+6]
		if strings.Contains(logged, fragment) {
			t.Errorf("6-byte password fragment %q leaked into logs: %q", fragment, logged)
		}
	}
}

// The email is masked, but only on the success path.
func TestRegisterHandler_MasksEmailOnSuccess(t *testing.T) {
	h, buf := newTestHandler(t)

	rec := callRegister(t, h, `{"email":"alice.private@example.com","password":"password123"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	logged := buf.String()
	if strings.Contains(logged, "alice.private@example.com") {
		t.Errorf("full email leaked into logs: %q", logged)
	}
	if !strings.Contains(logged, "a***@example.com") {
		t.Errorf("expected masked email in logs, got %q", logged)
	}
}

// A decode failure logs no email at all, since the field may hold
// attacker-controlled content.
func TestRegisterHandler_NoEmailOnDecodeFailure(t *testing.T) {
	h, buf := newTestHandler(t)

	rec := callRegister(t, h, `{"email":"attacker@evil.example.com","password":"pw123456"`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}

	if strings.Contains(buf.String(), "attacker@evil.example.com") {
		t.Errorf("email leaked into logs on decode failure: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "decode request body") {
		t.Errorf("expected a decode-failure log entry, got %q", buf.String())
	}
}

// MaskEmail itself is covered by internal/observability/redact_test.go. The
// assertions here only verify that this handler actually routes emails through
// it.

// A hostile email must not be able to break out of the JSON string it is placed
// in, whatever characters it contains.
func TestRegisterHandler_EscapesHostileEmail(t *testing.T) {
	hostile := []struct {
		name  string
		email string
	}{
		{name: "double quote", email: `a"b@example.com`},
		{name: "backslash", email: `a\b@example.com`},
		{name: "newline", email: "a\nb@example.com"},
		{name: "carriage return", email: "a\rb@example.com"},
		{name: "injected field", email: `a","role":"admin`},
		{name: "json fragment", email: `"},"data":{"admin":true},"x":"`},
		{name: "unicode", email: `ünïcödé@例え.jp`},
		{name: "control char", email: "a\x01b@example.com"},
		{name: "angle brackets", email: `<script>alert(1)</script>@example.com`},
	}

	for _, tc := range hostile {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTestHandler(t)

			body, err := json.Marshal(map[string]string{
				"email":    tc.email,
				"password": "password123",
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			rec := callRegister(t, h, string(body))

			// 201 Created or a 4xx validation failure are both acceptable; what
			// must never happen is an unparseable body.
			var parsed struct {
				Data struct {
					Email string `json:"email"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				t.Fatalf("response is not valid JSON: %v (body=%q)", err, rec.Body.String())
			}

			if rec.Code == http.StatusCreated && parsed.Data.Email != tc.email {
				t.Errorf("email round-trip mismatch:\n got  %q\n want %q", parsed.Data.Email, tc.email)
			}
		})
	}
}

// A malformed body must not leak the internal decoder error to the client.
func TestRegisterHandler_DoesNotLeakInternalError(t *testing.T) {
	h, logBuf := newTestHandler(t)

	rec := callRegister(t, h, `{"email": not-json,,,}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%q)", rec.Code, rec.Body.String())
	}

	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("error response is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	if parsed.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", parsed.Error.Code)
	}
	if parsed.Error.Message != "Invalid request body" {
		t.Errorf("message = %q, want the generic %q", parsed.Error.Message, "Invalid request body")
	}

	// The decoder detail must not reach the client body.
	if strings.Contains(rec.Body.String(), "invalid character") ||
		strings.Contains(rec.Body.String(), "json:") {
		t.Errorf("internal error detail leaked to client: %q", rec.Body.String())
	}

	// But it must be recorded server-side.
	if !strings.Contains(logBuf.String(), "decode request body") {
		t.Errorf("expected a server-side log of the decode failure, got %q", logBuf.String())
	}
}

// An oversized body must be reported as 413, matching the Content-Length fast
// path in middleware.BodyLimit, even for chunked requests.
func TestRegisterHandler_BodyTooLarge(t *testing.T) {
	h, _ := newTestHandler(t)
	handler := middleware.BodyLimit(64)(http.HandlerFunc(h.Register))

	big := `{"email":"` + strings.Repeat("a", 200) + `@example.com","password":"password123"}`
	rec := postRegister(t, handler, big)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body=%q)", rec.Code, rec.Body.String())
	}

	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("413 response is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	if parsed.Error.Code != middleware.CodePayloadTooLarge {
		t.Errorf("code = %q, want %q", parsed.Error.Code, middleware.CodePayloadTooLarge)
	}
	if strings.Contains(rec.Body.String(), "http: request body too large") {
		t.Errorf("internal error detail leaked to client: %q", rec.Body.String())
	}
}

// postRegister sends a request body through an arbitrary handler.
func postRegister(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}
