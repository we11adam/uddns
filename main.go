package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/app"
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
	_ "github.com/we11adam/uddns/updater/ddnsfm"
	_ "github.com/we11adam/uddns/updater/duckdns"
)

func init() {
	slog.SetDefault(slog.New(
		tint.NewHandler(os.Stdout, &tint.Options{
			NoColor:    !isatty.IsTerminal(os.Stdout.Fd()),
			Level:      slog.LevelDebug,
			TimeFormat: time.DateTime,
		}),
	))
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "c", "", "Path to the configuration file")
	flag.Parse()

	config, err := getConfigFile(configPath)
	if err != nil {
		slog.Error("fatal error config file", "error", err)
		os.Exit(1)
	}

	v := viper.New()
	slog.Info("[UDDNS] using config:", "config", config)
	v.SetConfigFile(config)
	if err = v.ReadInConfig(); err != nil {
		slog.Error("[UDDNS] failed to read config file:", "error", err)
		os.Exit(1)
	}

	name, p, err := provider.GetProvider(v)
	if err != nil {
		slog.Error("[UDDNS] no provider found.")
		os.Exit(1)
	} else {
		slog.Info("[UDDNS] provider selected:", "name", name)
	}

	name, u, err := updater.GetUpdater(v)
	if err != nil {
		slog.Error("[UDDNS] no updater found.")
		os.Exit(1)
	} else {
		slog.Info("[UDDNS] updater selected:", "updater", name)
	}

	name, n := notifier.GetNotifier(v)
	slog.Info("[UDDNS] notifier selected:", "notifier", name)

	app.NewApp(&p, &u, &n).Run()
}

func getConfigFile(providedPath string) (string, error) {
	if providedPath != "" && isReadable(providedPath) {
		return providedPath, nil
	}

	locations := []string{
		os.Getenv("UDDNS_CONFIG"),
		"./uddns.yaml",
		os.Getenv("HOME") + "/.config/uddns.yaml",
		"/etc/uddns.yaml",
	}

	for _, p := range locations {
		if isReadable(p) {
			return p, nil
		}
	}

	return "", fmt.Errorf("[UDDNS] no readable config file found in %v", locations)
}

func isReadable(p string) bool {
	if _, err := os.Stat(p); err == nil {
		return true
	}
	return false
}
