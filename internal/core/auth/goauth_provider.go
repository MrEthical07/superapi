package auth

import (
	"fmt"
	"os"
	"strings"
	"time"

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
//	engine, shutdown, err := auth.NewGoAuthEngine(redisClient, mode, userProvider)
//
// Notes:
// - redisClient must be non-nil
// - shutdown should be called during application shutdown
// - AUTH_TEST_* variables are honored for deterministic local perf scenarios
func NewGoAuthEngine(redisClient redis.UniversalClient, mode Mode, userProvider goauth.UserProvider) (*goauth.Engine, func(), error) {
	if redisClient == nil {
		return nil, nil, fmt.Errorf("goAuth provider requires redis client")
	}

	cfg := goauth.DefaultConfig()
	cfg.ValidationMode = toGoAuthValidationMode(mode)
	cfg.Result.IncludeRole = true
	cfg.Result.IncludePermissions = true
	cfg.Account.Enabled = true
	cfg.Account.DefaultRole = "user"

	// Optional deterministic signer for local perf tests across multiple processes.
	if sharedSecret := strings.TrimSpace(os.Getenv("AUTH_TEST_SHARED_SECRET")); sharedSecret != "" {
		cfg.JWT.SigningMethod = "hs256"
		cfg.JWT.PrivateKey = []byte(sharedSecret)
		cfg.JWT.PublicKey = []byte(sharedSecret)
		cfg.JWT.Issuer = "superapi-perf"
		cfg.JWT.Audience = "superapi-perf"
		cfg.JWT.KeyID = "superapi-perf-key"
	}

	if accessTTLRaw := strings.TrimSpace(os.Getenv("AUTH_TEST_ACCESS_TTL")); accessTTLRaw != "" {
		if d, err := time.ParseDuration(accessTTLRaw); err == nil && d > 0 {
			cfg.JWT.AccessTTL = d
		}
	}
	if refreshTTLRaw := strings.TrimSpace(os.Getenv("AUTH_TEST_REFRESH_TTL")); refreshTTLRaw != "" {
		if d, err := time.ParseDuration(refreshTTLRaw); err == nil && d > 0 {
			cfg.JWT.RefreshTTL = d
		}
	}

	engine, err := goauth.New().
		WithConfig(cfg).
		WithRedis(redisClient).
		WithPermissions([]string{"system.whoami"}).
		WithRoles(map[string][]string{
			"user":  {"system.whoami"},
			"admin": {"system.whoami"},
		}).
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

func toGoAuthValidationMode(mode Mode) goauth.ValidationMode {
	switch mode {
	case ModeJWTOnly:
		return goauth.ModeJWTOnly
	case ModeStrict:
		return goauth.ModeStrict
	case ModeHybrid:
		return goauth.ModeHybrid
	default:
		return goauth.ModeHybrid
	}
}
