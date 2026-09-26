package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
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

	envRedisHost     = "REDIS_HOST"
	envRedisPort     = "REDIS_PORT"
	envRedisPassword = "REDIS_PASSWORD"
	envRedisDB       = "REDIS_DB"

	envTrustedProxy = "TRUSTED_PROXY"

	envRateLimitWindow             = "RATE_LIMIT_WINDOW"
	envRateLimitLoginIPRequests    = "RATE_LIMIT_LOGIN_IP_REQUESTS"
	envRateLimitLoginEmailRequests = "RATE_LIMIT_LOGIN_EMAIL_REQUESTS"
	envRateLimitRegisterIPRequests = "RATE_LIMIT_REGISTER_IP_REQUESTS"
)

// Defaults for rate limiting. Each is used only when its variable is unset; a
// variable that is set but unparsable is an error, so a typo can never silently
// weaken a limit.
const (
	defaultRateLimitWindow             = time.Minute
	defaultRateLimitLoginIPRequests    = 20
	defaultRateLimitLoginEmailRequests = 10
	defaultRateLimitRegisterIPRequests = 10
)

// RateLimitConfig holds the fixed-window rate limit settings.
type RateLimitConfig struct {
	Window time.Duration
	// LoginIPRequests caps login attempts per client IP.
	LoginIPRequests int
	// LoginEmailRequests caps login attempts per target account. Tighter than
	// the IP limit so a botnet cannot grind a single account.
	LoginEmailRequests int
	// RegisterIPRequests caps account creations per client IP.
	RegisterIPRequests int
}

type Config struct {
	DatabaseURL   string
	RedisHost     string
	RedisPort     int
	RedisPassword string
	RedisDB       int
	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
	// TrustedProxy enables honouring X-Forwarded-For for client IP resolution.
	// It must only be true when a proxy this deployment controls sits in front
	// of the app, otherwise clients can spoof the header and bypass per-IP
	// limits.
	TrustedProxy bool
	RateLimit    RateLimitConfig
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

	redisHost, err := required(envRedisHost)
	if err != nil {
		return Config{}, err
	}
	redisPort, err := requiredInt(envRedisPort)
	if err != nil {
		return Config{}, err
	}
	// Optional: an unauthenticated local Redis needs neither.
	redisPassword := os.Getenv(envRedisPassword)
	redisDB, err := optionalInt(envRedisDB, 0)
	if err != nil {
		return Config{}, err
	}

	trustedProxy, err := optionalBool(envTrustedProxy, false)
	if err != nil {
		return Config{}, err
	}

	rateLimit, err := loadRateLimit()
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:   postgresURL(host, port, user, password, database),
		RedisHost:     redisHost,
		RedisPort:     redisPort,
		RedisPassword: redisPassword,
		RedisDB:       redisDB,
		JWTSecret:     jwtSecret,
		JWTAccessTTL:  accessTTL,
		JWTRefreshTTL: refreshTTL,
		TrustedProxy:  trustedProxy,
		RateLimit:     rateLimit,
	}, nil
}

func loadRateLimit() (RateLimitConfig, error) {
	window, err := durationOrDefault(envRateLimitWindow, defaultRateLimitWindow)
	if err != nil {
		return RateLimitConfig{}, err
	}

	loginIP, err := positiveIntOrDefault(envRateLimitLoginIPRequests, defaultRateLimitLoginIPRequests)
	if err != nil {
		return RateLimitConfig{}, err
	}

	loginEmail, err := positiveIntOrDefault(envRateLimitLoginEmailRequests, defaultRateLimitLoginEmailRequests)
	if err != nil {
		return RateLimitConfig{}, err
	}

	registerIP, err := positiveIntOrDefault(envRateLimitRegisterIPRequests, defaultRateLimitRegisterIPRequests)
	if err != nil {
		return RateLimitConfig{}, err
	}

	return RateLimitConfig{
		Window:             window,
		LoginIPRequests:    loginIP,
		LoginEmailRequests: loginEmail,
		RegisterIPRequests: registerIP,
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

// optionalInt returns fallback when the variable is unset, but rejects a value
// that is set and unparsable.
func optionalInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("environment variable %s must be an integer, got %q", name, value)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("environment variable %s must not be negative, got %d", name, parsed)
	}

	return parsed, nil
}

// positiveIntOrDefault is optionalInt with a floor of 1: a limit of zero or less
// would deny every request, which is never what an operator means.
func positiveIntOrDefault(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("environment variable %s must be an integer, got %q", name, value)
	}
	if parsed < 1 {
		return 0, fmt.Errorf("environment variable %s must be at least 1, got %d", name, parsed)
	}

	return parsed, nil
}

func optionalBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}

	switch strings.ToLower(value) {
	case "1", "t", "true", "yes":
		return true, nil
	case "0", "f", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("environment variable %s must be a boolean, got %q", name, value)
	}
}

func durationOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("environment variable %s must be a duration, got %q", name, value)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("environment variable %s must be a positive duration", name)
	}

	return duration, nil
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
