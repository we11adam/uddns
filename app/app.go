package app

import (
	"fmt"
	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
	"os"
	"time"
)

type Config struct {
}

type App struct {
	provider provider.Provider
	updater  updater.Updater
	notifier notifier.Notifier
	config   *Config
	last     string
}

func NewApp(p *provider.Provider, u *updater.Updater, n *notifier.Notifier) *App {
	return &App{
		provider: *p,
		updater:  *u,
		notifier: *n,
	}
}

func (a *App) schedule() {
	interval := os.Getenv("UDDNS_INTERVAL")
	if interval == "" {
		interval = "30s"
	}

	duration, err := time.ParseDuration(interval)
	if err != nil {
		fmt.Printf("Error parsing duration from env: \n. Using default `30s`\n")
		duration = 30 * time.Second
	}

	for {
		func() {
			defer time.Sleep(duration)
			ip, err := a.provider.Ip()
			if err != nil {
				fmt.Printf("Error getting IP: %v\n", err)
				return
			}

			if ip == a.last {
				fmt.Printf("IP has not changed: %s\n", ip)
				return
			} else {
				msg := fmt.Sprintf("New IP obtained: %s\n", ip)
				fmt.Printf(msg)
				a.notifier.Notify(notifier.Notification{Message: msg})
			}

			err = a.updater.Update(ip)
			if err != nil {
				fmt.Printf("Error updating IP: %v\n", err)
				return
			} else {
				message := fmt.Sprintf("IP updated to: %s\n", ip)
				fmt.Printf(message)
				a.notifier.Notify(notifier.Notification{Message: message})
			}

			a.last = ip
		}()
	}
}

func (a *App) Run() {
	a.schedule()
}
