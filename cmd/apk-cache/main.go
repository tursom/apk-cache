package main

import (
	"context"
	"flag"
	"log/slog"
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
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	app, err := NewApp(cfg)
	if err != nil {
		slog.Error("create app", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		slog.Error("run app", "err", err)
		os.Exit(1)
	}
}
