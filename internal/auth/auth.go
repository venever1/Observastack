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
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrRefreshTokenInvalid = errors.New("refresh token invalid or revoked")
	ErrNotMember           = errors.New("not a member of this tenant")
	ErrInvalidEmail        = errors.New("invalid email format")
	ErrPasswordTooShort    = errors.New("password must be at least 8 characters")
)

type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	Revoked   bool
}

type UserRepository interface {
	EmailExists(ctx context.Context, email string) (bool, error)
	CreateUser(ctx context.Context, id, email, passwordHash string) error
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	SaveRefreshToken(ctx context.Context, rt RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	GetRole(ctx context.Context, tenantID, userID string) (string, error)
}

type Service struct {
	repo       UserRepository
	tokens     *TokenManager
	refreshTTL time.Duration
}

func NewService(repo UserRepository, tokens *TokenManager, refreshTTL time.Duration) *Service {
	return &Service{repo: repo, tokens: tokens, refreshTTL: refreshTTL}
}

func (s *Service) Register(ctx context.Context, email, password string) (*User, error) {
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

	id := uuid.New().String()
	if err := s.repo.CreateUser(ctx, id, email, string(hash)); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return &User{ID: id, Email: email}, nil
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

func hashTokenString(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
