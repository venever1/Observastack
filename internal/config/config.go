package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const (
	envPostgresHost     = "POSTGRES_HOST"
	envPostgresPort     = "POSTGRES_PORT"
	envPostgresUser     = "POSTGRES_USER"
	envPostgresPassword = "POSTGRES_PASSWORD"
	envPostgresDatabase = "POSTGRES_DB"
	envJWTSecret        = "JWT_SECRET"
	envJWTAccessTTL     = "JWT_ACCESS_TTL"
	envJWTRefreshTTL    = "JWT_REFRESH_TTL"
)

type Config struct {
	DatabaseURL   string
	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load environment file: %w", err)
	}

	host, err := required(envPostgresHost)
	if err != nil {
		return Config{}, err
	}
	port, err := requiredInt(envPostgresPort)
	if err != nil {
		return Config{}, err
	}
	user, err := required(envPostgresUser)
	if err != nil {
		return Config{}, err
	}
	password, err := required(envPostgresPassword)
	if err != nil {
		return Config{}, err
	}
	database, err := required(envPostgresDatabase)
	if err != nil {
		return Config{}, err
	}

	jwtSecret, err := required(envJWTSecret)
	if err != nil {
		return Config{}, err
	}
	accessTTL, err := parseDuration(envJWTAccessTTL)
	if err != nil {
		return Config{}, err
	}
	refreshTTL, err := parseDuration(envJWTRefreshTTL)
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:   postgresURL(host, port, user, password, database),
		JWTSecret:     jwtSecret,
		JWTAccessTTL:  accessTTL,
		JWTRefreshTTL: refreshTTL,
	}, nil
}

func parseDuration(name string) (time.Duration, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("environment variable %s must be a positive duration", name)
	}

	return duration, nil
}

func required(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", name)
	}

	return value, nil
}

func requiredInt(name string) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return 0, fmt.Errorf("environment variable %s is required", name)
	}

	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("environment variable %s must be a valid port", name)
	}

	return port, nil
}

func postgresURL(host string, port int, user, password, database string) string {
	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   database,
	}
	query := connectionURL.Query()
	query.Set("sslmode", "disable")
	connectionURL.RawQuery = query.Encode()

	return connectionURL.String()
}
