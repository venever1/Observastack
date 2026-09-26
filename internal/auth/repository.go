package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolationCode is the PostgreSQL SQLSTATE for a unique constraint
// violation (23505).
const uniqueViolationCode = "23505"

// querier is the subset of pgx shared by *pgxpool.Pool and pgx.Tx, so the same
// repository methods work both inside and outside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PostgresRepository struct {
	pool *pgxpool.Pool
	db   querier
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: db, db: db}
}

// isUniqueViolation reports whether err is a PostgreSQL unique constraint
// violation. Callers use it to translate a race on INSERT into a domain error
// rather than surfacing an opaque driver error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode
}

// InTx runs fn inside a database transaction, committing on success and rolling
// back on any error or panic.
//
// Registration creates a user, a tenant and a membership row; without a
// transaction a failure part-way through would leave an orphaned tenant or a
// user with no workspace.
func (r *PostgresRepository) InTx(ctx context.Context, fn func(UserRepository) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Rollback after a successful Commit is a harmless no-op.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(&PostgresRepository{db: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (r *PostgresRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)", email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("query email: %w", err)
	}
	return exists, nil
}

// CreateUser inserts a user and returns the created_at timestamp assigned by
// the database, so the caller does not have to invent one.
func (r *PostgresRepository) CreateUser(ctx context.Context, id, email, passwordHash string) (time.Time, error) {
	var createdAt time.Time
	err := r.db.QueryRow(ctx,
		"INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3) RETURNING created_at",
		id, email, passwordHash,
	).Scan(&createdAt)
	if err != nil {
		// Two concurrent registrations can both pass the EmailExists pre-check;
		// the loser of the INSERT race must surface the same domain error as the
		// sequential path, not an opaque 500.
		if isUniqueViolation(err) {
			return time.Time{}, ErrEmailAlreadyExists
		}
		return time.Time{}, fmt.Errorf("insert user: %w", err)
	}
	return createdAt, nil
}

// CreateTenant creates a workspace for a newly registered user.
func (r *PostgresRepository) CreateTenant(ctx context.Context, id, name string) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO tenants (id, name) VALUES ($1, $2)",
		id, name,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrTenantAlreadyExists
		}
		return fmt.Errorf("insert tenant: %w", err)
	}
	return nil
}

// AddMember grants a user a role within a tenant. It is what makes a
// registration usable: tenant.Resolver rejects any caller who has no
// tenant_members row.
func (r *PostgresRepository) AddMember(ctx context.Context, tenantID, userID, role string) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ($1, $2, $3)",
		tenantID, userID, role,
	)
	if err != nil {
		return fmt.Errorf("insert tenant member: %w", err)
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
