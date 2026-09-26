package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryRepository struct {
	mu            sync.Mutex
	usersByEmail  map[string]*User
	refreshByHash map[string]*RefreshToken
	roles         map[string]string
	tenants       map[string]string // tenantID -> name
	members       map[string]string // tenantID+":"+userID -> role

	// inTxFailWith, when set, makes InTx abort before running fn, simulating a
	// transaction that fails to start.
	inTxFailWith error
	// inTxCalls counts how many transactions were opened.
	inTxCalls int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		usersByEmail:  make(map[string]*User),
		refreshByHash: make(map[string]*RefreshToken),
		roles:         make(map[string]string),
		tenants:       make(map[string]string),
		members:       make(map[string]string),
	}
}

// InTx runs fn directly. The in-memory store has no transactions, so this
// cannot exercise rollback; it exists to satisfy the interface and to let tests
// assert that Register routes its writes through InTx.
func (r *memoryRepository) InTx(_ context.Context, fn func(UserRepository) error) error {
	r.mu.Lock()
	r.inTxCalls++
	failWith := r.inTxFailWith
	r.mu.Unlock()

	if failWith != nil {
		return failWith
	}
	return fn(r)
}

func (r *memoryRepository) CreateTenant(_ context.Context, id, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tenants[id] = name
	return nil
}

func (r *memoryRepository) AddMember(_ context.Context, tenantID, userID, role string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.members[tenantID+":"+userID] = role
	return nil
}

func (r *memoryRepository) GetRole(_ context.Context, tenantID, userID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	role, ok := r.roles[tenantID+":"+userID]
	if !ok {
		return "", ErrNotMember
	}
	return role, nil
}

func (r *memoryRepository) EmailExists(_ context.Context, email string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exists := r.usersByEmail[email]
	return exists, nil
}

func (r *memoryRepository) CreateUser(_ context.Context, id, email, passwordHash string) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.usersByEmail[email]; exists {
		return time.Time{}, ErrEmailAlreadyExists
	}
	now := time.Now()
	r.usersByEmail[email] = &User{ID: id, Email: email, PasswordHash: passwordHash, CreatedAt: now}
	return now, nil
}

func (r *memoryRepository) GetUserByEmail(_ context.Context, email string) (*User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, exists := r.usersByEmail[email]
	if !exists {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}

func (r *memoryRepository) SaveRefreshToken(_ context.Context, rt RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshByHash[rt.TokenHash] = &rt
	return nil
}

func (r *memoryRepository) GetRefreshToken(_ context.Context, tokenHash string) (*RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt, exists := r.refreshByHash[tokenHash]
	if !exists {
		return nil, ErrRefreshTokenInvalid
	}
	return rt, nil
}

func (r *memoryRepository) RevokeRefreshToken(_ context.Context, tokenHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt, exists := r.refreshByHash[tokenHash]
	if !exists {
		return ErrRefreshTokenInvalid
	}
	rt.Revoked = true
	return nil
}

func newTestService(t *testing.T) (*Service, *memoryRepository) {
	t.Helper()
	tokens, err := NewTokenManager("test-secret", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	repo := newMemoryRepository()
	return NewService(repo, tokens, time.Hour), repo
}

func TestRegister(t *testing.T) {
	service, _ := newTestService(t)
	registration, err := service.Register(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if registration.User.Email != "user@example.com" || registration.User.ID == "" {
		t.Fatalf("unexpected user: %#v", registration.User)
	}

	_, err = service.Register(context.Background(), "user@example.com", "password123")
	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("got %v, want %v", err, ErrEmailAlreadyExists)
	}
}

func TestLogin(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := service.Register(context.Background(), "user@example.com", "password123"); err != nil {
		t.Fatal(err)
	}

	tokens, err := service.Login(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("tokens must not be empty: %#v", tokens)
	}

	if _, err := service.Login(context.Background(), "user@example.com", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v, want %v", err, ErrInvalidCredentials)
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	service, repo := newTestService(t)
	registration, err := service.Register(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	oldToken, err := service.issueRefreshToken(context.Background(), registration.User.ID)
	if err != nil {
		t.Fatal(err)
	}

	tokens, err := service.Refresh(context.Background(), oldToken)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.RefreshToken == oldToken {
		t.Fatalf("unexpected tokens: %#v", tokens)
	}
	if !repo.refreshByHash[hashTokenString(oldToken)].Revoked {
		t.Fatal("old refresh token was not revoked")
	}
	if _, err := service.Refresh(context.Background(), oldToken); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("got %v, want %v", err, ErrRefreshTokenInvalid)
	}
}

func TestTokenManager(t *testing.T) {
	manager, err := NewTokenManager("test-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Generate("user-id")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != "user-id" {
		t.Fatalf("got %q, want user-id", claims.UserID)
	}

	other, err := NewTokenManager("other-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Parse(token); err == nil {
		t.Fatal("expected invalid signature error")
	}
}
