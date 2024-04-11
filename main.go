package main

import (
	"fmt"
	_ "github.com/joho/godotenv/autoload"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
	_ "github.com/we11adam/uddns/provider/ip_service"
	_ "github.com/we11adam/uddns/provider/routeros"
	"github.com/we11adam/uddns/updater"
	_ "github.com/we11adam/uddns/updater/cloudflare"
	"os"
	"time"
)

func main() {
	v := viper.New()
	v.AddConfigPath(".")
	v.SetConfigName("uddns")
	v.SetConfigType("yaml")
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

	schedule(p, u)
}

func schedule(p provider.Provider, u updater.Updater) {
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
			}

			err = u.Update(ip)
			if err != nil {
				fmt.Printf("Error updating IP: %v\n", err)
				return
			}

			lastIp = ip
		}()
	}
}
