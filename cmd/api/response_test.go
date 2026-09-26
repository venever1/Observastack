package main

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

// newCapturingLogger returns a logger writing into buf, so tests can assert on
// exactly what would reach stderr.
func newCapturingLogger(buf *bytes.Buffer) *observability.Logger {
	return &observability.Logger{Logger: log.New(buf, "", 0)}
}

func postRegister(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// A hostile email must not be able to break out of the JSON string it is
// placed in, whatever characters it contains.
func TestRegisterHandler_EscapesHostileEmail(t *testing.T) {
	hostile := []struct {
		name  string
		email string
	}{
		{name: "double quote", email: `a"b@example.com`},
		{name: "backslash", email: `a\b@example.com`},
		{name: "newline", email: "a\nb@example.com"},
		{name: "carriage return", email: "a\rb@example.com"},
		{name: "tab", email: "a\tb@example.com"},
		{name: "injected field", email: `a","role":"admin`},
		{name: "injected object", email: `a","x":{"y":"z`},
		{name: "json fragment", email: `"},"data":{"admin":true},"x":"`},
		{name: "unicode", email: `ünïcödé@例え.jp`},
		{name: "emoji", email: `a@b😀.com`},
		{name: "control char", email: "a\x01b@example.com"},
		{name: "angle brackets", email: `<script>alert(1)</script>@example.com`},
		{name: "sql-ish", email: `a'; DROP TABLE users;--@example.com`},
	}

	for _, tc := range hostile {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			handler := newRegisterHandler(newCapturingLogger(&logBuf))

			body, err := json.Marshal(map[string]string{
				"email":    tc.email,
				"password": "password123",
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			rec := postRegister(t, handler, string(body))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
			}

			// The response must parse as JSON.
			var parsed struct {
				Data struct {
					Email string `json:"email"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				t.Fatalf("response is not valid JSON: %v (body=%q)", err, rec.Body.String())
			}

			// And it must round-trip the email byte-for-byte.
			if parsed.Data.Email != tc.email {
				t.Errorf("email round-trip mismatch:\n got  %q\n want %q", parsed.Data.Email, tc.email)
			}
		})
	}
}

func TestRegisterHandler_ResponseUsesAPIEnvelope(t *testing.T) {
	var logBuf bytes.Buffer
	handler := newRegisterHandler(newCapturingLogger(&logBuf))

	rec := postRegister(t, handler, `{"email":"user@example.com","password":"password123"}`)

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var parsed struct {
		Data struct {
			Email string `json:"email"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	if parsed.Data.Email != "user@example.com" {
		t.Errorf("email = %q, want %q", parsed.Data.Email, "user@example.com")
	}
}

// A malformed body must not leak the internal decoder error to the client.
func TestRegisterHandler_DoesNotLeakInternalError(t *testing.T) {
	var logBuf bytes.Buffer
	handler := newRegisterHandler(newCapturingLogger(&logBuf))

	// Malformed JSON, and a raw value that would previously have been
	// interpolated straight into the response.
	rec := postRegister(t, handler, `{"email": not-json,,,}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	var parsed apiErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("error response is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	if parsed.Error.Code != codeValidationError {
		t.Errorf("code = %q, want %q", parsed.Error.Code, codeValidationError)
	}
	if parsed.Error.Message != "Invalid request body" {
		t.Errorf("message = %q, want generic %q", parsed.Error.Message, "Invalid request body")
	}

	// The decoder detail must not reach the client body.
	if strings.Contains(rec.Body.String(), "invalid character") ||
		strings.Contains(rec.Body.String(), "json") {
		t.Errorf("internal error detail leaked to client: %q", rec.Body.String())
	}

	// But it must be recorded server-side.
	if !strings.Contains(logBuf.String(), "decode request body") {
		t.Errorf("expected server-side log of the decode failure, got %q", logBuf.String())
	}
}

func TestRegisterHandler_NoPIIInLogs(t *testing.T) {
	var logBuf bytes.Buffer
	handler := newRegisterHandler(newCapturingLogger(&logBuf))

	const email = "alice.private@example.com"
	const password = "sup3rs3cret"

	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	rec := postRegister(t, handler, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	logged := logBuf.String()
	if logged == "" {
		t.Fatal("expected the attempt to be logged")
	}

	if strings.Contains(logged, email) {
		t.Errorf("full email leaked into logs: %q", logged)
	}
	if strings.Contains(logged, "alice") {
		t.Errorf("email local part leaked into logs: %q", logged)
	}
	if strings.Contains(logged, password) {
		t.Errorf("password leaked into logs: %q", logged)
	}
	if !strings.Contains(logged, "a***@example.com") {
		t.Errorf("expected masked email in logs, got %q", logged)
	}
}

// The password must not reach the logs on the failure path either.
func TestRegisterHandler_NoPIIInLogsOnDecodeFailure(t *testing.T) {
	var logBuf bytes.Buffer
	handler := newRegisterHandler(newCapturingLogger(&logBuf))

	rec := postRegister(t, handler, `{"email":"bob@example.com","password":"hunter2`,)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	logged := logBuf.String()
	if strings.Contains(logged, "hunter2") {
		t.Errorf("password leaked into logs on failure path: %q", logged)
	}
	if strings.Contains(logged, "bob@example.com") {
		t.Errorf("email leaked into logs on failure path: %q", logged)
	}
}

func TestRegisterHandler_BodyTooLarge(t *testing.T) {
	var logBuf bytes.Buffer
	inner := newRegisterHandler(newCapturingLogger(&logBuf))
	handler := middleware.BodyLimit(64)(inner)

	big := `{"email":"` + strings.Repeat("a", 200) + `@example.com","password":"password123"}`
	rec := postRegister(t, handler, big)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body=%q)", rec.Code, rec.Body.String())
	}

	var parsed apiErrorResponse
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

func TestMaskEmail(t *testing.T) {
	cases := []struct {
		email string
		want  string
	}{
		{email: "alice@example.com", want: "a***@example.com"},
		{email: "a@example.com", want: "a***@example.com"},
		{email: "alice.private@example.com", want: "a***@example.com"},
		{email: "no-at-sign", want: "***"},
		{email: "", want: "***"},
		{email: "@example.com", want: "***"},
		{email: `"quoted"@example.com`, want: `"***@example.com`},
	}

	for _, tc := range cases {
		t.Run(tc.email, func(t *testing.T) {
			if got := maskEmail(tc.email); got != tc.want {
				t.Errorf("maskEmail(%q) = %q, want %q", tc.email, got, tc.want)
			}
		})
	}
}

// maskEmail must never return more of the local part than a single character.
func TestMaskEmail_NeverLeaksLocalPart(t *testing.T) {
	secrets := []string{
		"alice@example.com",
		"alice.private@example.com",
		"averyveryverylongusername@example.com",
	}

	for _, email := range secrets {
		masked := maskEmail(email)
		local := email[:strings.IndexByte(email, '@')]
		if strings.Contains(masked, local) {
			t.Errorf("maskEmail(%q) = %q leaks the local part", email, masked)
		}
	}
}
