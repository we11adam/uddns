package main

import (
	"fmt"
	_ "github.com/joho/godotenv/autoload"
	"github.com/spf13/viper"
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
	"time"
)

func main() {
	config, err := getConfigFile()
	if err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}
	v := viper.New()
	fmt.Print("Using config file: ", config, "\n")
	v.SetConfigFile(config)
	if err := v.ReadInConfig(); err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}

	var p provider.Provider
	var pn string
	for name, constructor := range provider.Providers {
		_p, err := constructor(v)
		if err == nil {
			fmt.Println("Provider registered: ", name)
			pn = name
			p = _p
			break
		}
	}
	if p == nil {
		panic("No provider found")
	} else {
		fmt.Printf("Provider selected: %s\n", pn)
	}

	var u updater.Updater
	var un string
	for name, constructor := range updater.Updaters {
		_u, err := constructor(v)
		if err == nil {
			fmt.Println("Updater registered: ", name)
			u = _u
			un = name
			break
		}
	}
	if u == nil {
		panic("No updater found")
	} else {
		fmt.Printf("Updater selected: %s\n", un)
	}

	var n notifier.Notifier
	var nn string
	for name, constructor := range notifier.Notifiers {
		_n, err := constructor(v)
		if err == nil {
			fmt.Println("Provider registered: ", name)
			nn = name
			n = _n
			break
		}
	}

	if n != nil {
		fmt.Printf("Notifier selected: %s\n", nn)
	} else {
		n = &notifier.Noop{}
		fmt.Printf("No notifier selected. Using Noop notifier.\n")
	}

	schedule(p, u, n)
}

func schedule(p provider.Provider, u updater.Updater, n notifier.Notifier) {
	lastIp := ""
	interval := os.Getenv("UDDNS_INTERVAL")
	if interval == "" {
		interval = "30s"
	}

	duration, err := time.ParseDuration(interval)
	if err != nil {
		panic("Error parsing duration from env: \n")
	}

	for {
		func() {
			defer time.Sleep(duration)
			ip, err := p.Ip()
			if err != nil {
				fmt.Printf("Error getting IP: %v\n", err)
				return
			}

			if ip == lastIp {
				fmt.Printf("IP has not changed: %s\n", ip)
				return
			} else {
				msg := fmt.Sprintf("New IP obtained: %s\n", ip)
				fmt.Printf(msg)
				n.Notify(notifier.Notification{Message: msg})
			}

			err = u.Update(ip)
			if err != nil {
				fmt.Printf("Error updating IP: %v\n", err)
				return
			} else {
				message := fmt.Sprintf("IP updated to: %s\n", ip)
				fmt.Printf(message)
				n.Notify(notifier.Notification{Message: message})
			}

			lastIp = ip
		}()
	}
}

func getConfigFile() (string, error) {
	pEnv := os.Getenv("UDDNS_CONFIG")
	pHome := os.Getenv("HOME") + "/.config/uddns.yaml"
	pEtc := "/etc/uddns.yaml"
	pCwd := "./uddns.yaml"

	switch true {
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
