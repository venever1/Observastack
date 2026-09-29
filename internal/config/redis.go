package config

import (
	"context"
	"fmt"
	"net"
	"strconv"

	redisotel "github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// OpenRedis builds a Redis client and verifies the connection with a PING,
// mirroring OpenPostgres: a broken dependency fails startup rather than
// surfacing later as per-request errors.
//
// Tracing is attached via redisotel so rate-limit calls (which run inside the
// request context) appear as child spans of the request span. The raw command
// statement is disabled: rate-limit keys embed the lookup subject (email or
// IP), which must not land in span attributes.
func OpenRedis(ctx context.Context, host string, port int, password string, db int) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     net.JoinHostPort(host, strconv.Itoa(port)),
		Password: password,
		DB:       db,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	if err := redisotel.InstrumentTracing(client, redisotel.WithDBStatement(false)); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("instrument redis tracing: %w", err)
	}

	return client, nil
}
