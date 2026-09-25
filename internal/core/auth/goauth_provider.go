package auth

import (
	"fmt"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/redis/go-redis/v9"
)

type providerCloser interface {
	Close()
}

// NewGoAuthEngine builds a goAuth engine backed by Redis and SQLC user provider.
//
// Usage:
//
//	engine, shutdown, err := auth.NewGoAuthEngine(redisClient, mode, tenancy, features, userProvider)
//
// Notes:
//   - redisClient must be non-nil
//   - shutdown should be called during application shutdown
//   - AUTH_TEST_* variables are honored for deterministic local perf scenarios
//     only when features.AllowTestOverrides is set (APP_ENV=dev/test)
func NewGoAuthEngine(redisClient redis.UniversalClient, mode Mode, tenancy TenancySettings, features Features, userProvider goauth.UserProvider) (*goauth.Engine, func(), error) {
	if redisClient == nil {
		return nil, nil, fmt.Errorf("goAuth provider requires redis client")
	}

	cfg, err := ProjectGoAuthConfig(mode, tenancy, features)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize goAuth config: %w", err)
	}

	engine, err := goauth.New().
		WithConfig(cfg).
		WithRedis(redisClient).
		WithPermissions(DefaultPermissions).
		WithRoles(DefaultRoles).
		WithUserProvider(userProvider).
		Build()
	if err != nil {
		return nil, nil, fmt.Errorf("build goAuth engine: %w", err)
	}

	shutdown := func() {
		if closer, ok := any(engine).(providerCloser); ok {
			closer.Close()
		}
	}

	return engine, shutdown, nil
}
