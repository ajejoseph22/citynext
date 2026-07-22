// Package config loads service configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration.
type Config struct {
	Port             string
	DatabaseURL      string
	DBMaxConns       int32
	NagerBaseURL     string
	HTTPTimeout      time.Duration // outbound HTTP (Nager) client timeout
	RequestTimeout   time.Duration // per-request server-side deadline
	Location         *time.Location
	DBConnectRetries int
	DBConnectBackoff time.Duration
}

// Load reads configuration from the environment, applying defaults. It returns
// an error only for values that are present but malformed, or for a required
// value (DATABASE_URL) that is missing.
func Load() (Config, error) {
	cfg := Config{
		Port:             getenv("PORT", "8080"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		NagerBaseURL:     getenv("NAGER_BASE_URL", "https://date.nager.at"),
		DBConnectRetries: 10,
		DBConnectBackoff: time.Second,
		HTTPTimeout:      5 * time.Second,
		RequestTimeout:   10 * time.Second,
		DBMaxConns:       10,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	var err error
	if cfg.Location, err = time.LoadLocation(getenv("TIMEZONE", "Europe/London")); err != nil {
		return Config{}, fmt.Errorf("TIMEZONE: %w", err)
	}
	if cfg.DBMaxConns, err = int32Env("DB_MAX_CONNS", cfg.DBMaxConns); err != nil {
		return Config{}, err
	}
	if cfg.HTTPTimeout, err = durationEnv("HTTP_TIMEOUT", cfg.HTTPTimeout); err != nil {
		return Config{}, err
	}
	if cfg.RequestTimeout, err = durationEnv("REQUEST_TIMEOUT", cfg.RequestTimeout); err != nil {
		return Config{}, err
	}
	if cfg.DBConnectRetries, err = intEnv("DB_CONNECT_RETRIES", cfg.DBConnectRetries); err != nil {
		return Config{}, err
	}
	if cfg.DBConnectBackoff, err = durationEnv("DB_CONNECT_BACKOFF", cfg.DBConnectBackoff); err != nil {
		return Config{}, err
	}

	// Reject non-positive values: they silently disable guarantees or break the
	// service. A zero HTTP_TIMEOUT disables the outbound client timeout; a zero
	// REQUEST_TIMEOUT makes http.TimeoutHandler fail every request immediately; a
	// zero DB_CONNECT_RETRIES skips the connect loop and would yield a nil pool;
	// a pool size < 1 is rejected by pgxpool.
	if err := requirePositiveInt32("DB_MAX_CONNS", cfg.DBMaxConns); err != nil {
		return Config{}, err
	}
	if err := requirePositiveDuration("HTTP_TIMEOUT", cfg.HTTPTimeout); err != nil {
		return Config{}, err
	}
	if err := requirePositiveDuration("REQUEST_TIMEOUT", cfg.RequestTimeout); err != nil {
		return Config{}, err
	}
	if err := requirePositiveInt("DB_CONNECT_RETRIES", cfg.DBConnectRetries); err != nil {
		return Config{}, err
	}
	if err := requirePositiveDuration("DB_CONNECT_BACKOFF", cfg.DBConnectBackoff); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func requirePositiveInt(key string, v int) error {
	if v < 1 {
		return fmt.Errorf("%s must be a positive integer, got %d", key, v)
	}
	return nil
}

func requirePositiveInt32(key string, v int32) error {
	if v < 1 {
		return fmt.Errorf("%s must be a positive integer, got %d", key, v)
	}
	return nil
}

func requirePositiveDuration(key string, v time.Duration) error {
	if v <= 0 {
		return fmt.Errorf("%s must be a positive duration, got %s", key, v)
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func int32Env(key string, fallback int32) (int32, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	// Parse with a 32-bit size so an out-of-range value is an error, not a value
	// that wraps into a valid-looking int32.
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return int32(n), nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
