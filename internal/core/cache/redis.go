package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/config"
)

// NewRedisClient builds and validates a Redis client from runtime config.
func NewRedisClient(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
	})

	if err := pingWithTimeout(ctx, cfg.StartupPingTimeout, client); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis startup ping failed: %w", err)
	}

	return client, nil
}

// CheckHealth performs a bounded Redis ping health check.
func CheckHealth(ctx context.Context, client *redis.Client, timeout time.Duration) error {
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return pingWithTimeout(ctx, timeout, client)
}

func pingWithTimeout(ctx context.Context, timeout time.Duration, client *redis.Client) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return client.Ping(checkCtx).Err()
}
