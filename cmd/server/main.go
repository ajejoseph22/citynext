// Command server runs the CityNext appointment service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embed the timezone database so time.LoadLocation works on minimal images
	// (e.g. distroless/static) that ship no system tzdata.
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"citynext/internal/booking"
	"citynext/internal/config"
	"citynext/internal/httpapi"
	"citynext/internal/nager"
	"citynext/internal/postgres"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// Root context cancelled on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := connectDB(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := postgres.NewRepository(pool)
	if err := repo.Migrate(ctx); err != nil {
		return err
	}

	holidays := nager.New(cfg.NagerBaseURL, nager.WithHTTPClient(&http.Client{Timeout: cfg.HTTPTimeout}))
	service := booking.NewService(repo, holidays, time.Now, uuid.New, cfg.Location)
	handler := httpapi.NewHandler(service, log)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           withTimeout(handler.Routes(), cfg.RequestTimeout),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Serve until the root context is cancelled.
	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// connectDB opens the pool and waits, with bounded backoff, for Postgres to
// accept connections — the app may start before the database is ready.
func connectDB(ctx context.Context, cfg config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = cfg.DBMaxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.DBConnectRetries; attempt++ {
		if err := pool.Ping(ctx); err == nil {
			return pool, nil
		} else {
			lastErr = err
		}
		log.Info("waiting for database", "attempt", attempt, "of", cfg.DBConnectRetries)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(cfg.DBConnectBackoff):
		}
	}
	pool.Close()
	return nil, lastErr
}

// withTimeout bounds each request with a context deadline. It sets the JSON
// content type on the outer ResponseWriter so the timeout body (written directly
// by http.TimeoutHandler, which does not set headers) keeps the uniform
// JSON response contract.
func withTimeout(h http.Handler, d time.Duration) http.Handler {
	timeout := http.TimeoutHandler(h, d, `{"error":{"code":"timeout","message":"request timed out"}}`)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		timeout.ServeHTTP(w, r)
	})
}
