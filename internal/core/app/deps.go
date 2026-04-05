package app

import (
	"context"
	"fmt"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/metrics"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
	"github.com/MrEthical07/superapi/internal/core/readiness"
	"github.com/MrEthical07/superapi/internal/core/storage"
	"github.com/MrEthical07/superapi/internal/core/tracing"
)

// START HERE:
// - This file wires process dependencies (Postgres, Redis, auth, cache, tracing).
// - Module routes should consume these via app.DependencyBinder or modulekit.Runtime.
//
// WARNING:
// This is core infrastructure code.
// Avoid modifying dependency ordering unless you understand startup and readiness behavior.

// Dependencies stores initialized process-level services shared with modules.
type Dependencies struct {
	// Postgres is the optional pgx pool initialized from config.
	Postgres *pgxpool.Pool
	// Store is the primary store surface used by modules.
	Store storage.Store
	// RelationalStore is the relational execution store.
	RelationalStore storage.RelationalStore
	// DocumentStore is the document execution store.
	DocumentStore storage.DocumentStore
	// Redis is the optional Redis client used by auth/cache/ratelimit.
	Redis *redis.Client
	// Readiness aggregates health checks for readiness responses.
	Readiness *readiness.Service
	// Metrics is the Prometheus instrumentation service.
	Metrics *metrics.Service
	// Tracing is the OpenTelemetry lifecycle service.
	Tracing *tracing.Service
	// AuthEngine is the optional goAuth engine.
	AuthEngine *goauth.Engine
	// AuthMode is the normalized auth mode used by auth policies.
	AuthMode auth.Mode
	// RateLimit is the resolved rate-limit config snapshot.
	RateLimit config.RateLimitConfig
	// Cache is the resolved cache config snapshot.
	Cache config.CacheConfig
	// Limiter is the optional route rate limiter.
	Limiter ratelimit.Limiter
	// CacheMgr is the optional response cache manager.
	CacheMgr  *cache.Manager
	authClose func()
}

// DependencyBinder allows modules to receive initialized Dependencies.
type DependencyBinder interface {
	BindDependencies(*Dependencies)
}

func initDependencies(ctx context.Context, cfg *config.Config) (*Dependencies, error) {
	deps := &Dependencies{
		Readiness:     readiness.NewService(),
		RateLimit:     cfg.RateLimit,
		Cache:         cfg.Cache,
		DocumentStore: storage.NoopDocumentStore{},
	}

	if cfg.Postgres.Enabled {
		pool, err := db.NewPool(ctx, cfg.Postgres)
		if err != nil {
			return nil, fmt.Errorf("init postgres: %w", err)
		}

		relStore, err := storage.NewPostgresRelationalStore(pool)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("init relational store: %w", err)
		}

		deps.Postgres = pool
		deps.RelationalStore = relStore
		deps.Store = relStore
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
	deps.AuthEngine = nil

	if cfg.Auth.Enabled {
		if deps.RelationalStore == nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init auth provider: relational store unavailable")
		}

		userRepo := auth.NewRelationalUserRepository(deps.RelationalStore)
		if userRepo == nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init auth provider: user repository unavailable")
		}

		engine, closeFn, err := auth.NewGoAuthEngine(deps.Redis, authMode, auth.NewStoreUserProvider(userRepo))
		if err != nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init auth provider: %w", err)
		}
		deps.AuthEngine = engine
		deps.authClose = closeFn
	}

	if cfg.RateLimit.Enabled {
		limiter, err := ratelimit.NewRedisLimiter(deps.Redis, ratelimit.Config{
			Env:      cfg.Env,
			FailOpen: cfg.RateLimit.FailOpen,
			Observe: func(route string, outcome ratelimit.Outcome) {
				if deps.Metrics == nil {
					return
				}
				deps.Metrics.ObserveRateLimit(route, string(outcome))
			},
		})
		if err != nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init rate limiter: %w", err)
		}
		deps.Limiter = limiter
	}

	if cfg.Cache.Enabled {
		cacheMgr, err := cache.NewManager(deps.Redis, cache.ManagerConfig{
			Env:                cfg.Env,
			FailOpen:           cfg.Cache.FailOpen,
			DefaultMaxBytes:    cfg.Cache.DefaultMaxBytes,
			TagVersionCacheTTL: cfg.Cache.TagVersionCacheTTL,
			Observe: func(route, outcome string) {
				if deps.Metrics == nil {
					return
				}
				deps.Metrics.ObserveCache(route, outcome)
			},
		})
		if err != nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init cache manager: %w", err)
		}
		deps.CacheMgr = cacheMgr
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
