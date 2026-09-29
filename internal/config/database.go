package config

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenPostgres opens a traced pgx connection pool and verifies it with a ping,
// so a broken database fails startup rather than surfacing as per-request
// errors.
//
// Every query runs as a child span of the incoming request span (the request
// context flows handler -> service -> repository -> pool). Query parameters
// are never recorded: otelpgx only attaches them when WithIncludeQueryParameters
// is set, which is deliberately omitted because auth queries carry emails,
// password hashes and token hashes. The statement text itself is parameterized
// ($1 placeholders, no interpolated values) and stays enabled so slow queries
// can be told apart in Jaeger.
func OpenPostgres(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}

	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
