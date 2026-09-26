package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"observastack/internal/observability"
)

// integrationEnv is the environment variable holding the connection string for
// the integration database. Tests are skipped when it is unset so that
// `go test ./...` stays green without a database.
//
// Run with the project's own stack:
//
//	make migrate
//	INTEGRATION_DATABASE_URL='postgres://observastack:observastack@localhost:5432/observastack?sslmode=disable' go test ./internal/auth/ -run Integration -v
const integrationEnv = "INTEGRATION_DATABASE_URL"

// newIntegrationStack wires HTTP → handler → service → PostgresRepository → DB.
func newIntegrationStack(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()

	url := os.Getenv(integrationEnv)
	if url == "" {
		t.Skipf("set %s to run integration tests", integrationEnv)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect to integration db: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping integration db: %v", err)
	}
	t.Cleanup(pool.Close)

	tokens, err := NewTokenManager("integration-test-secret", 15*time.Minute)
	if err != nil {
		t.Fatalf("token manager: %v", err)
	}

	logger := &observability.Logger{Logger: log.New(&bytes.Buffer{}, "", 0)}
	service := NewService(NewPostgresRepository(pool), tokens, time.Hour)
	handler := NewHandler(service, logger)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/register":
			handler.Register(w, r)
		case "/auth/login":
			handler.Login(w, r)
		case "/auth/refresh":
			handler.Refresh(w, r)
		case "/auth/logout":
			handler.Logout(w, r)
		default:
			http.NotFound(w, r)
		}
	}), pool
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// uniqueEmail keeps runs isolated without truncating shared tables, so the test
// is safe against a database that holds other data.
func uniqueEmail(t *testing.T) string {
	t.Helper()
	return "it-" + uuid.New().String() + "@example.com"
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var parsed struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	return parsed.Data
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("error response is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	return parsed.Error.Code
}

// Registration must persist a bcrypt-hashed password, never the plaintext.
func TestIntegration_RegisterPersistsBcryptHash(t *testing.T) {
	h, pool := newIntegrationStack(t)
	ctx := context.Background()

	email := uniqueEmail(t)
	const password = "integration-password-123"

	rec := postJSON(t, h, "/auth/register",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}

	var stored string
	if err := pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE email = $1", email).Scan(&stored); err != nil {
		t.Fatalf("user row not found: %v", err)
	}

	if stored == password {
		t.Fatal("password was stored in plaintext")
	}
	if strings.Contains(stored, password) {
		t.Fatalf("hash contains the plaintext password: %q", stored)
	}

	cost, err := bcrypt.Cost([]byte(stored))
	if err != nil {
		t.Fatalf("stored value is not a bcrypt hash: %v", err)
	}
	if cost != bcryptCost {
		t.Errorf("stored cost = %d, want %d", cost, bcryptCost)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)); err != nil {
		t.Errorf("stored hash does not verify against the password: %v", err)
	}
}

// Registration must also provision a tenant and an admin membership, otherwise
// tenant.Resolver would reject the new user from every tenant-scoped route.
func TestIntegration_RegisterCreatesTenantAndMembership(t *testing.T) {
	h, pool := newIntegrationStack(t)
	ctx := context.Background()

	email := uniqueEmail(t)
	rec := postJSON(t, h, "/auth/register",
		`{"email":"`+email+`","password":"integration-password-123"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}

	data := decodeData(t, rec)
	tenantID, _ := data["tenantId"].(string)
	userID, _ := data["id"].(string)
	if tenantID == "" || userID == "" {
		t.Fatalf("response must include id and tenantId, got: %v", data)
	}

	var tenantName string
	if err := pool.QueryRow(ctx, "SELECT name FROM tenants WHERE id = $1", tenantID).Scan(&tenantName); err != nil {
		t.Fatalf("tenant row not found: %v", err)
	}
	if !strings.Contains(tenantName, "workspace") {
		t.Errorf("tenant name = %q, want it derived from the email", tenantName)
	}

	var role string
	if err := pool.QueryRow(ctx,
		"SELECT role FROM tenant_members WHERE tenant_id = $1 AND user_id = $2",
		tenantID, userID).Scan(&role); err != nil {
		t.Fatalf("tenant_members row not found: %v", err)
	}
	if role != roleAdmin {
		t.Errorf("role = %q, want %q", role, roleAdmin)
	}
}

func TestIntegration_RegisterRejectsDuplicateEmail(t *testing.T) {
	h, _ := newIntegrationStack(t)

	email := uniqueEmail(t)
	body := `{"email":"` + email + `","password":"integration-password-123"}`

	if rec := postJSON(t, h, "/auth/register", body); rec.Code != http.StatusCreated {
		t.Fatalf("first registration status = %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}

	rec := postJSON(t, h, "/auth/register", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409 (body=%q)", rec.Code, rec.Body.String())
	}
	if code := decodeError(t, rec); code != "EMAIL_EXISTS" {
		t.Errorf("error code = %q, want EMAIL_EXISTS", code)
	}
}

// A duplicate must not leave an orphaned tenant behind.
func TestIntegration_DuplicateRegisterLeavesNoOrphanTenant(t *testing.T) {
	h, pool := newIntegrationStack(t)
	ctx := context.Background()

	email := uniqueEmail(t)
	body := `{"email":"` + email + `","password":"integration-password-123"}`

	postJSON(t, h, "/auth/register", body)
	postJSON(t, h, "/auth/register", body)

	var userCount, tenantCount int
	if err := pool.QueryRow(ctx,
		"SELECT (SELECT count(*) FROM users WHERE email = $1), (SELECT count(*) FROM tenants WHERE name = $2)",
		email, defaultTenantName(email)).Scan(&userCount, &tenantCount); err != nil {
		t.Fatalf("count query: %v", err)
	}

	if userCount != 1 {
		t.Errorf("user rows = %d, want 1", userCount)
	}
	if tenantCount != 1 {
		t.Errorf("tenant rows = %d, want 1", tenantCount)
	}
}

func TestIntegration_LoginReturnsTokens(t *testing.T) {
	h, _ := newIntegrationStack(t)

	email := uniqueEmail(t)
	const password = "integration-password-123"

	postJSON(t, h, "/auth/register", `{"email":"`+email+`","password":"`+password+`"}`)

	rec := postJSON(t, h, "/auth/login", `{"email":"`+email+`","password":"`+password+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}

	data := decodeData(t, rec)
	access, _ := data["accessToken"].(string)
	refresh, _ := data["refreshToken"].(string)
	if access == "" || refresh == "" {
		t.Fatalf("expected both tokens, got: %v", data)
	}
	if access == refresh {
		t.Error("access and refresh tokens must be distinct")
	}
}

func TestIntegration_LoginRejectsWrongPassword(t *testing.T) {
	h, _ := newIntegrationStack(t)

	email := uniqueEmail(t)
	postJSON(t, h, "/auth/register", `{"email":"`+email+`","password":"integration-password-123"}`)

	rec := postJSON(t, h, "/auth/login", `{"email":"`+email+`","password":"wrong-password-xyz"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%q)", rec.Code, rec.Body.String())
	}
	if code := decodeError(t, rec); code != "UNAUTHORIZED" {
		t.Errorf("error code = %q, want UNAUTHORIZED", code)
	}
}

// An unknown email must be indistinguishable from a wrong password, so the
// endpoint cannot be used to enumerate accounts.
func TestIntegration_LoginUnknownEmailIsIndistinguishable(t *testing.T) {
	h, _ := newIntegrationStack(t)

	rec := postJSON(t, h, "/auth/login",
		`{"email":"`+uniqueEmail(t)+`","password":"integration-password-123"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%q)", rec.Code, rec.Body.String())
	}
	if code := decodeError(t, rec); code != "UNAUTHORIZED" {
		t.Errorf("error code = %q, want UNAUTHORIZED", code)
	}
}

// The refresh token must be stored hashed, so a database leak does not hand
// over usable sessions.
func TestIntegration_RefreshTokenStoredHashed(t *testing.T) {
	h, pool := newIntegrationStack(t)
	ctx := context.Background()

	email := uniqueEmail(t)
	const password = "integration-password-123"

	postJSON(t, h, "/auth/register", `{"email":"`+email+`","password":"`+password+`"}`)
	rec := postJSON(t, h, "/auth/login", `{"email":"`+email+`","password":"`+password+`"}`)
	refresh, _ := decodeData(t, rec)["refreshToken"].(string)
	if refresh == "" {
		t.Fatal("expected a refresh token")
	}

	var stored string
	if err := pool.QueryRow(ctx, "SELECT token_hash FROM refresh_tokens WHERE token_hash = $1",
		hashTokenString(refresh)).Scan(&stored); err != nil {
		t.Fatalf("refresh token row not found: %v", err)
	}
	if stored == refresh {
		t.Fatal("refresh token was stored in plaintext")
	}
	if stored != hashTokenString(refresh) {
		t.Errorf("stored hash = %q, want the sha256 of the token", stored)
	}
}

func TestIntegration_RefreshRotatesAndRevokes(t *testing.T) {
	h, _ := newIntegrationStack(t)

	email := uniqueEmail(t)
	const password = "integration-password-123"

	postJSON(t, h, "/auth/register", `{"email":"`+email+`","password":"`+password+`"}`)
	rec := postJSON(t, h, "/auth/login", `{"email":"`+email+`","password":"`+password+`"}`)
	oldRefresh, _ := decodeData(t, rec)["refreshToken"].(string)

	rec = postJSON(t, h, "/auth/refresh", `{"refreshToken":"`+oldRefresh+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	newRefresh, _ := decodeData(t, rec)["refreshToken"].(string)
	if newRefresh == "" || newRefresh == oldRefresh {
		t.Fatalf("expected a rotated refresh token, got %q", newRefresh)
	}

	// The consumed token must be revoked and rejected on reuse.
	rec = postJSON(t, h, "/auth/refresh", `{"refreshToken":"`+oldRefresh+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("reusing a rotated token = %d, want 401 (body=%q)", rec.Code, rec.Body.String())
	}
}

func TestIntegration_LogoutRevokesRefreshToken(t *testing.T) {
	h, _ := newIntegrationStack(t)

	email := uniqueEmail(t)
	const password = "integration-password-123"

	postJSON(t, h, "/auth/register", `{"email":"`+email+`","password":"`+password+`"}`)
	rec := postJSON(t, h, "/auth/login", `{"email":"`+email+`","password":"`+password+`"}`)
	refresh, _ := decodeData(t, rec)["refreshToken"].(string)

	rec = postJSON(t, h, "/auth/logout", `{"refreshToken":"`+refresh+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}

	// The revoked token must no longer mint a new access token.
	rec = postJSON(t, h, "/auth/refresh", `{"refreshToken":"`+refresh+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout = %d, want 401 (body=%q)", rec.Code, rec.Body.String())
	}
}

// Logout is idempotent, so it cannot be used to probe which tokens exist.
func TestIntegration_LogoutIsIdempotent(t *testing.T) {
	h, _ := newIntegrationStack(t)

	rec := postJSON(t, h, "/auth/logout", `{"refreshToken":"never-issued-token"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an unknown token (body=%q)", rec.Code, rec.Body.String())
	}

	rec = postJSON(t, h, "/auth/logout", `{"refreshToken":""}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty token status = %d, want 422", rec.Code)
	}
}
