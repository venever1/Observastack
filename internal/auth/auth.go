package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the work factor used when hashing newly registered passwords.
//
// 12 is the current OWASP recommendation for bcrypt. It is roughly 4x the work
// of bcrypt.DefaultCost (10): about 250ms per hash on commodity server hardware
// versus roughly 60ms. That cost is paid once at registration and once per
// login attempt, both interactive and rate-limited, so the added latency is
// acceptable in exchange for a large increase in resistance to offline cracking
// of a leaked hash dump. Cost 10 was calibrated for hardware from the early
// 2010s and is no longer sufficient.
//
// Raising this later does not invalidate existing hashes: bcrypt embeds its
// cost in every hash it produces, and CompareHashAndPassword reads the cost
// from the stored hash rather than from this constant.
const bcryptCost = 12

var (
	ErrEmailAlreadyExists  = errors.New("email already exists")
	ErrTenantAlreadyExists = errors.New("tenant already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrRefreshTokenInvalid = errors.New("refresh token invalid or revoked")
	ErrNotMember           = errors.New("not a member of this tenant")
	ErrInvalidEmail        = errors.New("invalid email format")
	ErrPasswordTooShort    = errors.New("password must be at least 8 characters")
)

const (
	// roleAdmin is the role granted to the owner of a newly created tenant.
	roleAdmin = "admin"
)

// Registration is the outcome of a successful Register call. The tenant ID is
// returned so the client can use it as X-Tenant-ID on subsequent requests;
// without it a freshly registered user cannot reach any tenant-scoped route.
type Registration struct {
	User     *User
	TenantID string
}

type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	Revoked   bool
}

type UserRepository interface {
	EmailExists(ctx context.Context, email string) (bool, error)
	CreateUser(ctx context.Context, id, email, passwordHash string) (time.Time, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	SaveRefreshToken(ctx context.Context, rt RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	GetRole(ctx context.Context, tenantID, userID string) (string, error)
	CreateTenant(ctx context.Context, id, name string) error
	AddMember(ctx context.Context, tenantID, userID, role string) error
	// InTx runs fn atomically, so multi-step writes cannot leave partial state.
	InTx(ctx context.Context, fn func(UserRepository) error) error
}

type Service struct {
	repo       UserRepository
	tokens     *TokenManager
	refreshTTL time.Duration
}

func NewService(repo UserRepository, tokens *TokenManager, refreshTTL time.Duration) *Service {
	return &Service{repo: repo, tokens: tokens, refreshTTL: refreshTTL}
}

// Register creates a user together with their own tenant, matching the
// contract in docs/API_SPEC.md ("Daftar user baru + buat tenant").
//
// The three inserts run in one transaction: a partial failure would otherwise
// leave either an orphaned tenant or a user with no workspace, which
// tenant.Resolver would then reject from every tenant-scoped route.
func (s *Service) Register(ctx context.Context, email, password string) (*Registration, error) {
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	exists, err := s.repo.EmailExists(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("check email: %w", err)
	}
	if exists {
		return nil, ErrEmailAlreadyExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	userID := uuid.New().String()
	tenantID := uuid.New().String()
	var createdAt time.Time

	err = s.repo.InTx(ctx, func(txRepo UserRepository) error {
		created, err := txRepo.CreateUser(ctx, userID, email, string(hash))
		if err != nil {
			return err
		}
		createdAt = created
		if err := txRepo.CreateTenant(ctx, tenantID, defaultTenantName(email)); err != nil {
			return fmt.Errorf("create tenant: %w", err)
		}
		if err := txRepo.AddMember(ctx, tenantID, userID, roleAdmin); err != nil {
			return fmt.Errorf("add tenant member: %w", err)
		}
		return nil
	})
	if err != nil {
		// Unwrap so the handler can still map sentinel errors to 4xx responses.
		if errors.Is(err, ErrEmailAlreadyExists) {
			return nil, ErrEmailAlreadyExists
		}
		if errors.Is(err, ErrTenantAlreadyExists) {
			return nil, ErrTenantAlreadyExists
		}
		return nil, err
	}

	return &Registration{
		User:     &User{ID: userID, Email: email, CreatedAt: createdAt},
		TenantID: tenantID,
	}, nil
}

// defaultTenantName derives a readable initial workspace name from the email
// local part, e.g. "alice@example.com" becomes "alice's workspace".
func defaultTenantName(email string) string {
	local := email
	if at := strings.IndexByte(email, '@'); at > 0 {
		local = email[:at]
	}
	return local + "'s workspace"
}

func validateEmail(email string) error {
	if email == "" {
		return ErrInvalidEmail
	}
	if !strings.Contains(email, "@") {
		return ErrInvalidEmail
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}
	return nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*Tokens, error) {
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	accessToken, err := s.tokens.Generate(user.ID)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	refreshToken, err := s.issueRefreshToken(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("issue refresh token: %w", err)
	}

	return &Tokens{AccessToken: accessToken, RefreshToken: refreshToken}, nil
}

func (s *Service) Refresh(ctx context.Context, rawToken string) (*Tokens, error) {
	tokenHash := hashTokenString(rawToken)

	rt, err := s.repo.GetRefreshToken(ctx, tokenHash)
	if err != nil {
		return nil, ErrRefreshTokenInvalid
	}

	if rt.Revoked || time.Now().After(rt.ExpiresAt) {
		return nil, ErrRefreshTokenInvalid
	}

	if err := s.repo.RevokeRefreshToken(ctx, tokenHash); err != nil {
		return nil, fmt.Errorf("revoke refresh token: %w", err)
	}

	accessToken, err := s.tokens.Generate(rt.UserID)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	refreshToken, err := s.issueRefreshToken(ctx, rt.UserID)
	if err != nil {
		return nil, fmt.Errorf("issue refresh token: %w", err)
	}

	return &Tokens{AccessToken: accessToken, RefreshToken: refreshToken}, nil
}

func (s *Service) issueRefreshToken(ctx context.Context, userID string) (string, error) {
	raw, err := randomToken()
	if err != nil {
		return "", err
	}

	rt := RefreshToken{
		ID:        uuid.New().String(),
		UserID:    userID,
		TokenHash: hashTokenString(raw),
		ExpiresAt: time.Now().Add(s.refreshTTL),
	}

	if err := s.repo.SaveRefreshToken(ctx, rt); err != nil {
		return "", err
	}

	return raw, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Logout revokes a refresh token.
//
// It is deliberately idempotent: revoking an unknown or already-revoked token
// succeeds, so the endpoint cannot be used to probe whether a given token ever
// existed. Only a genuinely malformed request (empty token) is rejected.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return ErrRefreshTokenInvalid
	}
	if err := s.repo.RevokeRefreshToken(ctx, hashTokenString(rawToken)); err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func hashTokenString(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
