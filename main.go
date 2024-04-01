package main

import (
	"fmt"
	_ "github.com/joho/godotenv/autoload"
	"github.com/spf13/viper"
	"time"
	"uddns/provider"
	_ "uddns/provider/routeros"
	"uddns/updater"
	_ "uddns/updater/cloudflare"
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
	for name, constructor := range provider.Providers {
		_p, err := constructor(v)
		if err == nil {
			fmt.Println("Provider registered: ", name)
			p = _p
			break
		}
	}
	if p == nil {
		panic("No provider found")
	}

	var u updater.Updater
	for name, constructor := range updater.Updaters {
		_u, err := constructor(v)
		if err == nil {
			fmt.Println("Updater registered: ", name)
			u = _u
			break
		}
	}
	if u == nil {
		panic("No updater found")
	}

	schedule(p, u)
}

func schedule(p provider.Provider, u updater.Updater) {
	lastIp := ""
	for {
		func() {
			defer time.Sleep(5 * time.Second)
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
