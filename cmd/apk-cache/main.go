package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	internalconfig "github.com/tursom/apk-cache/internal/config"
)

func main() {
	configPath := flag.String("config", "config.toml", "Path to the TOML config file")
	flag.Parse()

	cfg, err := internalconfig.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	app, err := NewApp(cfg)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
