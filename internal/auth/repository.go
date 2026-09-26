package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)", email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("query email: %w", err)
	}
	return exists, nil
}

func (r *PostgresRepository) CreateUser(ctx context.Context, id, email, passwordHash string) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)",
		id, email, passwordHash,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.db.QueryRow(ctx,
		"SELECT id, email, password_hash, created_at FROM users WHERE email = $1",
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	return &u, nil
}

func (r *PostgresRepository) SaveRefreshToken(ctx context.Context, rt RefreshToken) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, revoked) VALUES ($1, $2, $3, $4, $5)",
		rt.ID, rt.UserID, rt.TokenHash, rt.ExpiresAt, rt.Revoked,
	)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var rt RefreshToken
	err := r.db.QueryRow(ctx,
		"SELECT id, user_id, token_hash, expires_at, revoked FROM refresh_tokens WHERE token_hash = $1",
		tokenHash,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.Revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRefreshTokenInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("query refresh token: %w", err)
	}
	return &rt, nil
}

func (r *PostgresRepository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx, "UPDATE refresh_tokens SET revoked = TRUE WHERE token_hash = $1", tokenHash)
	if err != nil {
		return fmt.Errorf("update refresh token: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetRole(ctx context.Context, tenantID, userID string) (string, error) {
	var role string
	err := r.db.QueryRow(ctx,
		"SELECT role FROM tenant_members WHERE tenant_id=$1 AND user_id=$2",
		tenantID, userID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotMember
	}
	if err != nil {
		return "", fmt.Errorf("query tenant role: %w", err)
	}
	return role, nil
}
