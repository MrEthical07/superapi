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

	"github.com/MrEthical07/superapi/internal/core/app"
	coreauth "github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/config"
)

type tokenOutput struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Identifier   string `json:"identifier"`
	Mode         string `json:"mode"`
}

// perftoken mints tokens for load tests using the same goAuth engine the API
// server builds from config (app.NewDependencies), so perf runs exercise the
// real role registry and JWT settings. The password flag is acceptable here
// because it only ever carries throwaway load-test credentials; use
// cmd/createuser (make user) for real accounts.
func main() {
	identifier := flag.String("email", "loadtest@example.com", "Login identifier (email)")
	password := flag.String("password", "LoadTest123!", "Login password (load-test credentials only)")
	role := flag.String("role", "user", "Role to assign when creating the account")
	modeRaw := flag.String("mode", "", "Auth mode override: jwt_only|hybrid|strict")
	createIfMissing := flag.Bool("create-if-missing", false, "Create the account when login fails (load-test seeding)")
	output := flag.String("output", "text", "Output format: text|json")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}
	// perftoken always needs the auth stack, whatever the profile says.
	cfg.Auth.Enabled = true
	cfg.Postgres.Enabled = true
	cfg.Redis.Enabled = true
	if mode := strings.TrimSpace(*modeRaw); mode != "" {
		cfg.Auth.Mode = mode
	}
	if err := cfg.Lint(); err != nil {
		log.Fatalf("config lint failed: %v", err)
	}
	parsedMode, err := coreauth.ParseMode(cfg.Auth.Mode)
	if err != nil {
		log.Fatalf("invalid auth mode: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deps, err := app.NewDependencies(ctx, cfg)
	if err != nil {
		log.Fatalf("init dependencies failed: %v", err)
	}
	defer deps.Close()
	engine := deps.AuthEngine

	email := strings.TrimSpace(*identifier)
	accessToken, refreshToken, err := engine.Login(ctx, email, *password)
	if err != nil {
		if !*createIfMissing {
			log.Fatalf("login failed (pass --create-if-missing to seed the account): %v", err)
		}

		_, createErr := engine.CreateAccount(ctx, goauth.CreateAccountRequest{
			Identifier: email,
			Password:   *password,
			Role:       strings.TrimSpace(*role),
		})

		accessToken, refreshToken, err = engine.Login(ctx, email, *password)
		if err != nil {
			log.Fatalf("login failed after create attempt (createErr=%v): %v", createErr, err)
		}
	}

	result := tokenOutput{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Identifier:   email,
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
