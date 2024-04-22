package main

import (
	"fmt"
	_ "github.com/joho/godotenv/autoload"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/app"
	"github.com/we11adam/uddns/notifier"
	_ "github.com/we11adam/uddns/notifier/telegram"
	"github.com/we11adam/uddns/provider"
	_ "github.com/we11adam/uddns/provider/ip_service"
	_ "github.com/we11adam/uddns/provider/netif"
	_ "github.com/we11adam/uddns/provider/routeros"
	"github.com/we11adam/uddns/updater"
	_ "github.com/we11adam/uddns/updater/cloudflare"
	_ "github.com/we11adam/uddns/updater/duckdns"
	"os"
)

func main() {
	config, err := getConfigFile()
	if err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}
	v := viper.New()
	fmt.Print("Using config file: ", config, "\n")
	v.SetConfigFile(config)
	if err = v.ReadInConfig(); err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}

	name, p, err := provider.GetProvider(v)
	if err != nil {
		panic("No provider found")
	} else {
		fmt.Printf("Provider selected: %s\n", name)
	}

	name, u, err := updater.GetUpdater(v)
	if err != nil {
		panic("No Updater found")
	} else {
		fmt.Printf("Updater selected: %s\n", name)
	}

	name, n := notifier.GetNotifier(v)
	fmt.Println("Notifier selected: ", name)

	app.NewApp(&p, &u, &n).Run()
}

func getConfigFile() (string, error) {
	pEnv := os.Getenv("UDDNS_CONFIG")
	pHome := os.Getenv("HOME") + "/.config/uddns.yaml"
	pEtc := "/etc/uddns.yaml"
	pCwd := "./uddns.yaml"

	switch {
	case isReadable(pEnv):
		return pEnv, nil
	case isReadable(pCwd):
		return pCwd, nil
	case isReadable(pHome):
		return pHome, nil
	case isReadable(pEtc):
		return pEtc, nil
	default:
		return "", fmt.Errorf("no config file found")
	}
}

func isReadable(p string) bool {
	if _, err := os.Stat(p); err == nil {
		return true
	}
	return false
}
