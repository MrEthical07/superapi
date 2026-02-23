package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/modules"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}

	if err := cfg.Lint(); err != nil {
		log.Fatalf("config lint failed: %v", err)
	}

	a, err := app.New(cfg, modules.All())
	if err != nil {
		log.Fatalf("app init failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := a.Run(ctx); err != nil {
		log.Fatalf("app run failed: %v", err)
	}
}
