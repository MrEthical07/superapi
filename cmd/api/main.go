package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
	"github.com/MrEthical07/superapi/internal/modules"
)

// START HERE:
// - This is the production server bootstrap entrypoint.
// - It loads config, validates config, builds app dependencies, and runs HTTP server.
// - For dependency wiring details, see internal/core/app/deps.go.
// - For module registration, see internal/modules/modules.go.

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}

	if err := cfg.Lint(); err != nil {
		log.Fatalf("config lint failed: %v", err)
	}

	logger, err := logx.New(logx.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})
	if err != nil {
		log.Fatalf("logger init failed: %v", err)
	}

	a, err := app.New(cfg, logger, modules.All())
	if err != nil {
		logger.Fatal().Err(err).Msg("app init failed")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := a.Run(ctx); err != nil {
		logger.Fatal().Err(err).Msg("app run failed")
	}
}
