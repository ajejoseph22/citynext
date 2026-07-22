// Package pgtest provides an isolated Postgres schema for integration tests.
//
// go test runs different package test binaries in parallel, so any two
// integration packages that shared one table (and truncated it) could clobber
// each other's rows mid-test. Setup gives every test its own uniquely-named
// schema, so tests are fully isolated regardless of -p and need no TRUNCATE.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schemaSeq disambiguates multiple schemas created within one process; the PID
// disambiguates concurrent package test binaries sharing a database.
var schemaSeq atomic.Int64

// Setup returns a pgxpool.Pool scoped to a fresh, uniquely-named schema. It
// registers cleanup that drops the schema and closes connections. The test is
// skipped when TEST_DATABASE_URL is unset, so a local `make test` stays green
// while `make test-integration` (which sets it) exercises the database.
func Setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("cnx_test_%d_%d", os.Getpid(), schemaSeq.Add(1))
	quoted := pgx.Identifier{schema}.Sanitize()

	// Create the schema on a short-lived admin connection.
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create schema: %v", err)
	}

	// Build a pool whose every connection resolves objects in that schema, so
	// Migrate creates the table there and all queries stay isolated.
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("parse config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Logf("dropping schema %s: %v", schema, err)
		}
		_ = admin.Close(context.Background())
	})
	return pool
}
