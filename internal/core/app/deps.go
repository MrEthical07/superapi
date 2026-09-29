package app

import (
	"context"
	"errors"
	"fmt"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/metrics"
	"github.com/MrEthical07/superapi/internal/core/notify"
	"github.com/MrEthical07/superapi/internal/core/policy"
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
	// DB is the relational data-access boundary shared with modules. It hands
	// repositories sqlc queries bound to the pool or an active transaction and
	// owns the WithTx write boundary.
	DB *storage.Postgres
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
	CacheMgr *cache.Manager
	// Auth is the resolved auth config snapshot (mode and feature flags).
	Auth config.AuthConfig
	// Notifier delivers password-reset and email-verification secrets
	// asynchronously. Set by App from NOTIFY_* config; nil in bare tests.
	Notifier *notify.Dispatcher
	// AuthUsers is the auth user repository (nil when auth is disabled). The
	// auth module uses it to find the delivery address for reset and
	// verification messages.
	AuthUsers auth.UserRepository
	authClose func()
	// features are the loaded optional features, in registration order.
	features []loadedFeature
}

// DependencyBinder allows modules to receive initialized Dependencies.
type DependencyBinder interface {
	BindDependencies(*Dependencies)
}

// AuthFeatures maps the auth feature flags in config onto goAuth settings.
func AuthFeatures(cfg *config.Config) auth.Features {
	if cfg == nil {
		return auth.Features{}
	}
	return auth.Features{
		RegistrationAutoLogin:     cfg.Auth.RegistrationEnabled && cfg.Auth.RegistrationAutoLogin,
		PasswordReset:             cfg.Auth.PasswordResetEnabled,
		EmailVerification:         cfg.Auth.EmailVerificationEnabled,
		EmailVerificationRequired: cfg.Auth.EmailVerificationRequired,
		TOTP:                      cfg.Auth.TOTPEnabled,
		TOTPIssuer:                cfg.Auth.TOTPIssuer,
		AllowTestOverrides:        cfg.Auth.TestOverridesAllowed,
	}
}

// NewDependencies initializes process dependencies from config. The server
// (App) and command-line tools such as cmd/createuser share it so they build
// the exact same goAuth engine. Call Close when done.
func NewDependencies(ctx context.Context, cfg *config.Config, features ...Feature) (*Dependencies, error) {
	return initDependencies(ctx, cfg, features)
}

// Close releases pooled connections and the auth engine. It does not shut
// down tracing (App does that with its shutdown timeout).
func (d *Dependencies) Close() {
	if d == nil {
		return
	}
	if d.authClose != nil {
		d.authClose()
	}
	if d.Redis != nil {
		_ = d.Redis.Close()
	}
	if d.Postgres != nil {
		d.Postgres.Close()
	}
}

func initDependencies(ctx context.Context, cfg *config.Config, features []Feature) (*Dependencies, error) {
	loaded, err := loadFeatures(cfg, features)
	if err != nil {
		return nil, err
	}
	deps := &Dependencies{
		Readiness: readiness.NewService(),
		RateLimit: cfg.RateLimit,
		Cache:     cfg.Cache,
		Auth:      cfg.Auth,
		features:  loaded,
	}

	// Apply the tenancy decision to the policy engine before any module
	// registers routes or constructs presets, so preset defaults and route
	// validation reflect TENANCY_ENABLED.
	policy.SetTenancyEnabled(cfg.Tenancy.Enabled)

	if cfg.Postgres.Enabled {
		pool, err := db.NewPool(ctx, cfg.Postgres)
		if err != nil {
			return nil, fmt.Errorf("init postgres: %w", err)
		}

		pg, err := storage.NewPostgres(pool)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("init postgres boundary: %w", err)
		}

		deps.Postgres = pool
		deps.DB = pg
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
		if deps.DB == nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init auth provider: relational database unavailable")
		}

		userRepo := auth.NewRelationalUserRepository(deps.DB)
		if userRepo == nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init auth provider: user repository unavailable")
		}

		userProvider := auth.NewStoreUserProvider(userRepo).WithTenancy(cfg.Tenancy.Enabled)

		// template:begin webauthn
		// The provider always carries the WebAuthn credential capability so
		// enabling WebAuthn is a config step. goAuth only exercises it when
		// WEBAUTHN_ENABLED is set.
		userProvider = userProvider.WithWebAuthnRepository(auth.NewWebAuthnCredentialRepository(deps.DB))
		// template:end webauthn

		// TOTP persistence (and the at-rest cipher) is only wired when TOTP
		// is enabled; config lint guarantees the key is present and valid.
		if cfg.Auth.TOTPEnabled {
			cipher, err := newTOTPCipher(cfg.Auth)
			if err != nil {
				if deps.Redis != nil {
					_ = deps.Redis.Close()
				}
				if deps.Postgres != nil {
					deps.Postgres.Close()
				}
				return nil, fmt.Errorf("init auth provider: totp encryption key: %w", err)
			}
			userProvider = userProvider.WithMFA(auth.NewMFARepository(deps.DB), cipher).WithTx(deps.DB)
		}

		var provider goauth.UserProvider = userProvider
		var mutators []auth.ConfigMutator
		var extensions []policy.AuthExtension
		for _, lf := range deps.features {
			if lf.hooks.UserProvider != nil {
				provider = lf.hooks.UserProvider(deps, userProvider)
			}
			if lf.hooks.UserRepository != nil {
				userRepo = lf.hooks.UserRepository(deps, userRepo)
			}
			if lf.hooks.GoAuthConfig != nil {
				mutators = append(mutators, lf.hooks.GoAuthConfig)
			}
			if lf.hooks.AuthExtension != nil {
				extensions = append(extensions, *lf.hooks.AuthExtension)
			}
		}

		engine, closeFn, err := auth.NewGoAuthEngine(deps.Redis, authMode, auth.TenancySettings{
			Enabled: cfg.Tenancy.Enabled,
		}, AuthFeatures(cfg), provider, mutators...)
		if err != nil {
			if deps.Redis != nil {
				_ = deps.Redis.Close()
			}
			if deps.Postgres != nil {
				deps.Postgres.Close()
			}
			return nil, fmt.Errorf("init auth provider: %w", err)
		}
		policy.UseAuthExtensions(engine, extensions...)
		deps.AuthEngine = engine
		deps.AuthUsers = userRepo
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

// drainNotifierWithinShutdown drains the dispatcher for whatever is left of the
// shutdown timeout: the deadline set when shutdown began, or a fresh full
// shutdown timeout on exit paths where graceful shutdown never began.
func (a *App) drainNotifierWithinShutdown() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if a.shutdownDeadline.IsZero() {
		ctx, cancel = context.WithTimeout(context.Background(), a.cfg.HTTP.ShutdownTimeout)
	} else {
		ctx, cancel = context.WithDeadline(context.Background(), a.shutdownDeadline)
	}
	defer cancel()
	a.drainNotifier(ctx)
}

// drainNotifier stops the notification dispatcher and waits for in-flight
// deliveries until ctx is done, logging how many had to be abandoned. It runs at
// most once.
func (a *App) drainNotifier(ctx context.Context) {
	if a == nil || a.deps == nil || a.deps.Notifier == nil || a.notifierDrained {
		return
	}
	a.notifierDrained = true

	if abandoned := a.deps.Notifier.Shutdown(ctx); abandoned > 0 {
		a.log.Warn().Int("abandoned", abandoned).Msg("shutdown: abandoned in-flight notification deliveries at the shutdown deadline")
	}
}

// newTOTPCipher builds the TOTP secret cipher from AUTH_TOTP_ENCRYPTION_KEY /
// AUTH_TOTP_ENCRYPTION_KEYS. The rotatetotpkey command builds the same cipher.
func newTOTPCipher(cfg config.AuthConfig) (auth.SecretCipher, error) {
	ring, err := cfg.TOTPKeyring()
	if err != nil {
		return nil, err
	}
	return auth.NewKeyRingCipher(auth.KeyRing{Keys: ring.Keys, ActiveKID: ring.ActiveKID, LegacyKey: ring.LegacyKey})
}

// NewTOTPCipher is newTOTPCipher for command-line tools that re-encrypt stored
// secrets (cmd/rotatetotpkey).
func NewTOTPCipher(cfg config.AuthConfig) (auth.RotatableCipher, error) {
	c, err := newTOTPCipher(cfg)
	if err != nil {
		return nil, err
	}
	rc, ok := c.(auth.RotatableCipher)
	if !ok {
		return nil, errors.New("totp cipher does not support rotation")
	}
	return rc, nil
}

func (a *App) closeDependencies() {
	if a == nil || a.deps == nil {
		return
	}

	// Pending notifications must finish (or be abandoned) while the resources
	// a notifier may still use are open. Normally Run already drained them
	// right after the HTTP server stopped; this covers every other exit path.
	a.drainNotifierWithinShutdown()

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
