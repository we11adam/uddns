package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/we11adam/uddns/app"
	"github.com/we11adam/uddns/internal/config"
	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"

	_ "github.com/joho/godotenv/autoload"

	_ "github.com/we11adam/uddns/notifier/telegram"
	_ "github.com/we11adam/uddns/provider/ip_service"
	_ "github.com/we11adam/uddns/provider/netif"
	_ "github.com/we11adam/uddns/provider/routeros"
	_ "github.com/we11adam/uddns/updater/aliyun"
	_ "github.com/we11adam/uddns/updater/cloudflare"
	_ "github.com/we11adam/uddns/updater/duckdns"
	_ "github.com/we11adam/uddns/updater/lightdns"
)

func init() {
	configureLogger()
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "c", "", "Path to the configuration file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config file", "error", err)
		os.Exit(1)
	}
	configureLoggerFromConfig(cfg)
	slog.Info("using config file", "config", cfg.Path())

	providerName, p, err := provider.GetProvider(cfg)
	if err != nil {
		slog.Error("no provider found", "error", err)
		os.Exit(1)
	} else {
		slog.Info("provider selected", "provider", providerName)
	}

	updaterName, u, err := updater.GetUpdater(cfg)
	if err != nil {
		slog.Error("no updater found", "error", err)
		os.Exit(1)
	} else {
		slog.Info("updater selected", "updater", updaterName)
	}

	notifierName, n, err := notifier.GetNotifier(cfg)
	if err != nil {
		slog.Error("notifier configuration error", "error", err)
		os.Exit(1)
	}
	slog.Info("notifier selected", "notifier", notifierName)

	interval, rawInterval, err := cfg.Interval()
	if err != nil {
		slog.Warn("invalid update interval, using default", "env_var", "UDDNS_INTERVAL", "value", rawInterval, "default", config.DefaultInterval, "error", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app.NewApp(providerName, p, updaterName, u, notifierName, n, interval).Run(ctx)
}
