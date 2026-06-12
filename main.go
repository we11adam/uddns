package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

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

	config, err := getConfigFile(configPath)
	if err != nil {
		slog.Error("failed to find config file", "error", err)
		os.Exit(1)
	}

	v := viper.New()
	v.SetConfigFile(config)
	if err = v.ReadInConfig(); err != nil {
		slog.Error("failed to read config file", "config", config, "error", err)
		os.Exit(1)
	}
	configureLoggerFromConfig(v)
	slog.Info("using config file", "config", config)

	configReader := viperConfigReader{v: v}

	providerName, p, err := provider.GetProvider(configReader)
	if err != nil {
		slog.Error("no provider found", "error", err)
		os.Exit(1)
	} else {
		slog.Info("provider selected", "provider", providerName)
	}

	updaterName, u, err := updater.GetUpdater(configReader)
	if err != nil {
		slog.Error("no updater found", "error", err)
		os.Exit(1)
	} else {
		slog.Info("updater selected", "updater", updaterName)
	}

	notifierName, n, err := notifier.GetNotifier(configReader)
	if err != nil {
		slog.Error("notifier configuration error", "error", err)
		os.Exit(1)
	}
	slog.Info("notifier selected", "notifier", notifierName)

	app.NewApp(providerName, p, updaterName, u, notifierName, n).Run()
}

type viperConfigReader struct {
	v *viper.Viper
}

func (r viperConfigReader) GetString(key string) string {
	return r.v.GetString(key)
}

func (r viperConfigReader) IsSet(key string) bool {
	return r.v.IsSet(key)
}

func (r viperConfigReader) UnmarshalKey(key string, rawVal any) error {
	return r.v.UnmarshalKey(key, rawVal)
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

	return "", fmt.Errorf("no readable config file found in %v", locations)
}

func isReadable(p string) bool {
	if _, err := os.Stat(p); err == nil {
		return true
	}
	return false
}
