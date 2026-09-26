package auth

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
