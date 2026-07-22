package config

import (
	"testing"
	"time"
)

// allKeys are every env var Load consults. Each test clears them so ambient
// values in the developer's shell can't leak into a case.
var allKeys = []string{
	"PORT", "DATABASE_URL", "NAGER_BASE_URL", "TIMEZONE",
	"DB_MAX_CONNS", "HTTP_TIMEOUT", "REQUEST_TIMEOUT",
	"DB_CONNECT_RETRIES", "DB_CONNECT_BACKOFF",
}

// setEnv clears all known keys, then applies env for this case.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range allKeys {
		t.Setenv(k, "")
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

const validDSN = "postgres://user:pass@localhost:5432/db?sslmode=disable"

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{
			name:    "defaults with only DATABASE_URL",
			env:     map[string]string{"DATABASE_URL": validDSN},
			wantErr: false,
		},
		{
			name:    "missing DATABASE_URL",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name:    "bad TIMEZONE",
			env:     map[string]string{"DATABASE_URL": validDSN, "TIMEZONE": "Mars/Phobos"},
			wantErr: true,
		},
		{
			name:    "DB_MAX_CONNS zero",
			env:     map[string]string{"DATABASE_URL": validDSN, "DB_MAX_CONNS": "0"},
			wantErr: true,
		},
		{
			name:    "DB_MAX_CONNS negative",
			env:     map[string]string{"DATABASE_URL": validDSN, "DB_MAX_CONNS": "-3"},
			wantErr: true,
		},
		{
			name:    "DB_MAX_CONNS not a number",
			env:     map[string]string{"DATABASE_URL": validDSN, "DB_MAX_CONNS": "lots"},
			wantErr: true,
		},
		{
			name:    "DB_MAX_CONNS overflows int32",
			env:     map[string]string{"DATABASE_URL": validDSN, "DB_MAX_CONNS": "3000000000"},
			wantErr: true,
		},
		{
			name:    "DB_MAX_CONNS large but in range",
			env:     map[string]string{"DATABASE_URL": validDSN, "DB_MAX_CONNS": "2000000000"},
			wantErr: false,
		},
		{
			name:    "HTTP_TIMEOUT zero",
			env:     map[string]string{"DATABASE_URL": validDSN, "HTTP_TIMEOUT": "0s"},
			wantErr: true,
		},
		{
			name:    "HTTP_TIMEOUT malformed",
			env:     map[string]string{"DATABASE_URL": validDSN, "HTTP_TIMEOUT": "5"},
			wantErr: true,
		},
		{
			name:    "REQUEST_TIMEOUT zero",
			env:     map[string]string{"DATABASE_URL": validDSN, "REQUEST_TIMEOUT": "0"},
			wantErr: true,
		},
		{
			name:    "DB_CONNECT_RETRIES zero",
			env:     map[string]string{"DATABASE_URL": validDSN, "DB_CONNECT_RETRIES": "0"},
			wantErr: true,
		},
		{
			name:    "DB_CONNECT_BACKOFF zero",
			env:     map[string]string{"DATABASE_URL": validDSN, "DB_CONNECT_BACKOFF": "0s"},
			wantErr: true,
		},
		{
			name: "valid overrides",
			env: map[string]string{
				"DATABASE_URL":       validDSN,
				"DB_MAX_CONNS":       "20",
				"HTTP_TIMEOUT":       "3s",
				"REQUEST_TIMEOUT":    "8s",
				"DB_CONNECT_RETRIES": "5",
				"DB_CONNECT_BACKOFF": "500ms",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)
			_, err := Load()
			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": validDSN})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Port:             "8080",
		DatabaseURL:      validDSN,
		NagerBaseURL:     "https://date.nager.at",
		DBMaxConns:       10,
		HTTPTimeout:      5 * time.Second,
		RequestTimeout:   10 * time.Second,
		DBConnectRetries: 10,
		DBConnectBackoff: time.Second,
	}
	if cfg.Port != want.Port || cfg.DatabaseURL != want.DatabaseURL ||
		cfg.NagerBaseURL != want.NagerBaseURL || cfg.DBMaxConns != want.DBMaxConns ||
		cfg.HTTPTimeout != want.HTTPTimeout || cfg.RequestTimeout != want.RequestTimeout ||
		cfg.DBConnectRetries != want.DBConnectRetries || cfg.DBConnectBackoff != want.DBConnectBackoff {
		t.Fatalf("defaults mismatch:\n got %+v", cfg)
	}
	if cfg.Location == nil || cfg.Location.String() != "Europe/London" {
		t.Fatalf("Location = %v, want Europe/London", cfg.Location)
	}
}

func TestLoadValidOverridesParsed(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL":       validDSN,
		"HTTP_TIMEOUT":       "3s",
		"DB_CONNECT_RETRIES": "5",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPTimeout != 3*time.Second {
		t.Fatalf("HTTPTimeout = %s, want 3s", cfg.HTTPTimeout)
	}
	if cfg.DBConnectRetries != 5 {
		t.Fatalf("DBConnectRetries = %d, want 5", cfg.DBConnectRetries)
	}
}
