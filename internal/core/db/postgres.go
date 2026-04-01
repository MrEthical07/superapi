package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrEthical07/superapi/internal/core/config"
)

// NewPool creates and verifies a pgx connection pool from config.
func NewPool(ctx context.Context, cfg config.PostgresConfig) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("postgres parse config: %w", err)
	}

	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnLifetime = cfg.ConnMaxLifetime
	pcfg.MaxConnIdleTime = cfg.ConnMaxIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}

	if err := pingWithTimeout(ctx, cfg.StartupPingTimeout, pool.Ping); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres startup ping failed: %w", err)
	}

	return pool, nil
}

// CheckHealth performs a bounded ping against the Postgres pool.
func CheckHealth(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	if pool == nil {
		return fmt.Errorf("postgres pool is nil")
	}
	return pingWithTimeout(ctx, timeout, pool.Ping)
}

func pingWithTimeout(ctx context.Context, timeout time.Duration, pingFn func(context.Context) error) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return pingFn(checkCtx)
}
