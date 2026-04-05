package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/redis/go-redis/v9"

	coreauth "github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

type tokenOutput struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Identifier   string `json:"identifier"`
	Mode         string `json:"mode"`
}

func main() {
	identifier := flag.String("email", "loadtest@example.com", "Login identifier (email)")
	password := flag.String("password", "LoadTest123!", "Login password")
	role := flag.String("role", "user", "Role to assign when creating account")
	modeRaw := flag.String("mode", "", "Auth mode override: jwt_only|hybrid|strict")
	createIfMissing := flag.Bool("create-if-missing", true, "Create account when login fails")
	output := flag.String("output", "text", "Output format: text|json")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}

	if strings.TrimSpace(cfg.Postgres.URL) == "" {
		log.Fatal("POSTGRES_URL is required")
	}
	if strings.TrimSpace(cfg.Redis.Addr) == "" {
		log.Fatal("REDIS_ADDR is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pgPool, err := db.NewPool(ctx, cfg.Postgres)
	if err != nil {
		log.Fatalf("postgres init failed: %v", err)
	}
	defer pgPool.Close()

	redisClient, err := cache.NewRedisClient(ctx, cfg.Redis)
	if err != nil {
		log.Fatalf("redis init failed: %v", err)
	}
	defer func() {
		_ = redisClient.Close()
	}()

	authModeRaw := strings.TrimSpace(*modeRaw)
	if authModeRaw == "" {
		authModeRaw = cfg.Auth.Mode
	}
	parsedMode, err := coreauth.ParseMode(authModeRaw)
	if err != nil {
		log.Fatalf("invalid auth mode: %v", err)
	}

	relStore, err := storage.NewPostgresRelationalStore(pgPool)
	if err != nil {
		log.Fatalf("relational store init failed: %v", err)
	}

	userRepo := coreauth.NewRelationalUserRepository(relStore)
	if userRepo == nil {
		log.Fatal("user repository init failed")
	}

	engine, closeEngine, err := buildEngine(redisClient, parsedMode, coreauth.NewStoreUserProvider(userRepo))
	if err != nil {
		log.Fatalf("build auth engine failed: %v", err)
	}
	defer closeEngine()

	accessToken, refreshToken, err := engine.Login(ctx, strings.TrimSpace(*identifier), *password)
	if err != nil {
		if !*createIfMissing {
			log.Fatalf("login failed: %v", err)
		}

		_, createErr := engine.CreateAccount(ctx, goauth.CreateAccountRequest{
			Identifier: strings.TrimSpace(*identifier),
			Password:   *password,
			Role:       strings.TrimSpace(*role),
		})

		accessToken, refreshToken, err = engine.Login(ctx, strings.TrimSpace(*identifier), *password)
		if err != nil {
			log.Fatalf("login failed after create attempt (createErr=%v): %v", createErr, err)
		}
	}

	result := tokenOutput{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Identifier:   strings.TrimSpace(*identifier),
		Mode:         string(parsedMode),
	}

	switch strings.ToLower(strings.TrimSpace(*output)) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(true)
		if err := enc.Encode(result); err != nil {
			log.Fatalf("encode json output failed: %v", err)
		}
	default:
		fmt.Printf("ACCESS_TOKEN=%s\n", result.AccessToken)
		fmt.Printf("REFRESH_TOKEN=%s\n", result.RefreshToken)
		fmt.Printf("IDENTIFIER=%s\n", result.Identifier)
		fmt.Printf("MODE=%s\n", result.Mode)
	}
}

func buildEngine(redisClient redis.UniversalClient, mode coreauth.Mode, userProvider goauth.UserProvider) (*goauth.Engine, func(), error) {
	cfg := goauth.DefaultConfig()
	cfg.ValidationMode = toGoAuthValidationMode(mode)
	cfg.Result.IncludeRole = true
	cfg.Result.IncludePermissions = true
	// Enable account creation in the helper so create-if-missing can seed load users.
	cfg.Account.Enabled = true
	cfg.Account.DefaultRole = "user"

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
		return nil, nil, fmt.Errorf("build engine: %w", err)
	}

	shutdown := func() {
		if closer, ok := any(engine).(interface{ Close() }); ok {
			closer.Close()
		}
	}

	return engine, shutdown, nil
}

func toGoAuthValidationMode(mode coreauth.Mode) goauth.ValidationMode {
	switch mode {
	case coreauth.ModeJWTOnly:
		return goauth.ModeJWTOnly
	case coreauth.ModeStrict:
		return goauth.ModeStrict
	case coreauth.ModeHybrid:
		return goauth.ModeHybrid
	default:
		return goauth.ModeHybrid
	}
}
