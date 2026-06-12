package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	os.Exit(run(os.Args[1:]))
}

type runtimeConfig struct {
	providerName string
	provider     provider.Provider
	updaterName  string
	updater      updater.Updater
	notifierName string
	notifier     notifier.Notifier
	interval     time.Duration
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "config" {
		return runConfigCommand(args[1:])
	}

	rt, ok := loadRuntimeFromFlags("uddns", args)
	if !ok {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app.NewApp(rt.providerName, rt.provider, rt.updaterName, rt.updater, rt.notifierName, rt.notifier, rt.interval).Run(ctx)
	return 0
}

func runConfigCommand(args []string) int {
	if len(args) == 0 {
		slog.Error("missing config subcommand", "usage", "uddns config check [-c path]")
		return 2
	}

	switch args[0] {
	case "check":
		rt, ok := loadRuntimeFromFlags("uddns config check", args[1:])
		if !ok {
			return 1
		}
		slog.Info(
			"config valid",
			"provider", rt.providerName,
			"updater", rt.updaterName,
			"notifier", rt.notifierName,
			"interval", rt.interval,
		)
		return 0
	default:
		slog.Error("unknown config subcommand", "subcommand", args[0], "usage", "uddns config check [-c path]")
		return 2
	}
}

func loadRuntimeFromFlags(name string, args []string) (*runtimeConfig, bool) {
	var configPath string
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.StringVar(&configPath, "c", "", "Path to the configuration file")
	if err := flags.Parse(args); err != nil {
		return nil, false
	}
	if flags.NArg() > 0 {
		slog.Error("unexpected argument", "argument", flags.Arg(0))
		return nil, false
	}

	rt, err := loadRuntime(configPath)
	if err != nil {
		slog.Error("failed to validate config", "error", err)
		return nil, false
	}

	return rt, true
}

func loadRuntime(configPath string) (*runtimeConfig, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}
	configureLoggerFromConfig(cfg)
	slog.Info("using config file", "config", cfg.Path())

	providerName, p, err := provider.GetProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("no provider found: %w", err)
	} else {
		slog.Info("provider selected", "provider", providerName)
	}

	updaterName, u, err := updater.GetUpdater(cfg)
	if err != nil {
		return nil, fmt.Errorf("no updater found: %w", err)
	} else {
		slog.Info("updater selected", "updater", updaterName)
	}

	notifierName, n, err := notifier.GetNotifier(cfg)
	if err != nil {
		return nil, fmt.Errorf("notifier configuration error: %w", err)
	}
	slog.Info("notifier selected", "notifier", notifierName)

	interval, rawInterval, err := cfg.Interval()
	if err != nil {
		slog.Warn("invalid update interval, using default", "env_var", "UDDNS_INTERVAL", "value", rawInterval, "default", config.DefaultInterval, "error", err)
	}

	return &runtimeConfig{
		providerName: providerName,
		provider:     p,
		updaterName:  updaterName,
		updater:      u,
		notifierName: notifierName,
		notifier:     n,
		interval:     interval,
	}, nil
}
