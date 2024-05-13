package app

import (
	"fmt"
	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
	"log/slog"
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
		slog.Warn("[UDDNS] error parsing duration from env: \n. Using default `30s`\n")
		duration = 30 * time.Second
	}

	for {
		func() {
			defer time.Sleep(duration)
			ip, err := a.provider.Ip()
			if err != nil {
				slog.Error("[UDDNS] failed to get IP:", "error", err)
				return
			}

			if ip == a.last {
				slog.Info("[UDDNS] IP has not changed:", "ip", ip)
				return
			} else {
				slog.Info("[UDDNS] new IP obtained:", "ip", ip)
				err = a.notifier.Notify(notifier.Notification{Message: fmt.Sprintf("New IP obtained: %s", ip)})
				if err != nil {
					slog.Error("failed to send notification:", "error", err)
				}
			}

			err = a.updater.Update(ip)
			if err != nil {
				slog.Error("[UDDNS] failed to update DNS record:", "error", err)
				return
			} else {
				slog.Info("IP updated to:", "ip", ip)
				err = a.notifier.Notify(notifier.Notification{Message: fmt.Sprintf("IP updated to: %s\n", ip)})
				if err != nil {
					if err != nil {
						slog.Error("failed to send notification:", "error", err)
					}
				}
			}

			a.last = ip
		}()
	}
}

func (a *App) Run() {
	a.schedule()
}
