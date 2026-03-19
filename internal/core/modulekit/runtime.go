package modulekit

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

// Runtime gives modules a single injected surface for optional infrastructure
// dependencies used during route registration and handler/service wiring.
type Runtime struct {
	deps *app.Dependencies
}

func New(deps *app.Dependencies) Runtime {
	return Runtime{deps: deps}
}

func (r Runtime) Dependencies() *app.Dependencies {
	return r.deps
}

func (r Runtime) Postgres() *pgxpool.Pool {
	if r.deps == nil {
		return nil
	}
	return r.deps.Postgres
}

func (r Runtime) Redis() *redis.Client {
	if r.deps == nil {
		return nil
	}
	return r.deps.Redis
}

func (r Runtime) RateLimitConfig() config.RateLimitConfig {
	if r.deps == nil {
		return config.RateLimitConfig{}
	}
	return r.deps.RateLimit
}

func (r Runtime) CacheConfig() config.CacheConfig {
	if r.deps == nil {
		return config.CacheConfig{}
	}
	return r.deps.Cache
}

func (r Runtime) AuthProvider() auth.Provider {
	if r.deps == nil || r.deps.Auth == nil {
		return auth.NewDisabledProvider()
	}
	return r.deps.Auth
}

func (r Runtime) AuthMode(overrides ...auth.Mode) auth.Mode {
	if len(overrides) > 0 && overrides[0] != "" {
		return overrides[0]
	}
	if r.deps == nil || r.deps.AuthMode == "" {
		return auth.ModeHybrid
	}
	return r.deps.AuthMode
}

func (r Runtime) Limiter() ratelimit.Limiter {
	if r.deps == nil {
		return nil
	}
	return r.deps.Limiter
}

func (r Runtime) CacheManager() *cache.Manager {
	if r.deps == nil {
		return nil
	}
	return r.deps.CacheMgr
}
