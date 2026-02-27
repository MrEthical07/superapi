package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/metrics"
	"github.com/MrEthical07/superapi/internal/core/readiness"
	"github.com/MrEthical07/superapi/internal/core/tracing"
)

type Dependencies struct {
	Postgres  *pgxpool.Pool
	Redis     *redis.Client
	Readiness *readiness.Service
	Metrics   *metrics.Service
	Tracing   *tracing.Service
	Auth      auth.Provider
	AuthMode  auth.Mode
	authClose func()
}

type DependencyBinder interface {
	BindDependencies(*Dependencies)
}

func initDependencies(ctx context.Context, cfg *config.Config) (*Dependencies, error) {
	deps := &Dependencies{
		Readiness: readiness.NewService(),
	}

	if cfg.Postgres.Enabled {
		pool, err := db.NewPool(ctx, cfg.Postgres)
		if err != nil {
			return nil, fmt.Errorf("init postgres: %w", err)
		}
		deps.Postgres = pool
		deps.Readiness.Add("postgres", true, cfg.Postgres.HealthCheckTimeout, func(checkCtx context.Context) error {
			return db.CheckHealth(checkCtx, pool, cfg.Postgres.HealthCheckTimeout)
		})
	} else {
		deps.Readiness.Add("postgres", false, cfg.Postgres.HealthCheckTimeout, nil)
	}

	if cfg.Redis.Enabled {
		client, err := cache.NewRedisClient(ctx, cfg.Redis)
		if err != nil {
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init redis: %w", err)
		}
		deps.Redis = client
		deps.Readiness.Add("redis", true, cfg.Redis.HealthCheckTimeout, func(checkCtx context.Context) error {
			return cache.CheckHealth(checkCtx, client, cfg.Redis.HealthCheckTimeout)
		})
	} else {
		deps.Readiness.Add("redis", false, cfg.Redis.HealthCheckTimeout, nil)
	}

	metricsSvc, err := metrics.New(cfg.Metrics, deps.Postgres)
	if err != nil {
		if deps.Redis != nil {
			_ = deps.Redis.Close()
		}
		if deps.Postgres != nil {
			deps.Postgres.Close()
		}
		return nil, fmt.Errorf("init metrics: %w", err)
	}
	deps.Metrics = metricsSvc

	authMode, err := auth.ParseMode(cfg.Auth.Mode)
	if err != nil {
		if deps.Redis != nil {
			_ = deps.Redis.Close()
		}
		if deps.Postgres != nil {
			deps.Postgres.Close()
		}
		return nil, fmt.Errorf("init auth mode: %w", err)
	}
	deps.AuthMode = authMode
	deps.Auth = auth.NewDisabledProvider()

	if cfg.Auth.Enabled {
		provider, closeFn, err := auth.NewGoAuthEngineProvider(deps.Redis, authMode)
		if err != nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init auth provider: %w", err)
		}
		deps.Auth = provider
		deps.authClose = closeFn
	}

	tracingSvc, err := tracing.New(ctx, cfg.Tracing, cfg.Env)
	if err != nil {
		if deps.Redis != nil {
			_ = deps.Redis.Close()
		}
		if deps.Postgres != nil {
			deps.Postgres.Close()
		}
		return nil, fmt.Errorf("init tracing: %w", err)
	}
	deps.Tracing = tracingSvc

	return deps, nil
}

func (a *App) closeDependencies() {
	if a == nil || a.deps == nil {
		return
	}

	if a.deps.Redis != nil {
		if err := a.deps.Redis.Close(); err != nil {
			a.log.Error().Err(err).Msg("redis close error")
		}
	}
	if a.deps.Postgres != nil {
		a.deps.Postgres.Close()
	}
	if a.deps.Tracing != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := a.deps.Tracing.Shutdown(shutdownCtx); err != nil {
			a.log.Error().Err(err).Msg("tracing shutdown error")
		}
	}
	if a.deps.authClose != nil {
		a.deps.authClose()
	}
}
