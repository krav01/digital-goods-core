package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid DATABASE_URL")
	} // Do not expose credentials.
	cfg.MaxConns = 10
	cfg.MinConns = 0
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 3 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "10000"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	return pool, nil
}

func Ready(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version != 1 || dirty {
		return errors.New("database migrations are not ready")
	}
	return nil
}

// transact retries only transactions PostgreSQL explicitly rolled back, never ambiguous commits.
// The callback must contain database operations only and be safe to execute again.
func transact(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := pgx.BeginFunc(ctx, pool, fn)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || (pgerr.Code != "40001" && pgerr.Code != "40P01") {
			return err
		}
		if attempt == 2 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
	return nil
}
