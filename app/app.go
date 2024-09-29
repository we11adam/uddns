package app

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

type App struct {
	provider provider.Provider
	updater  updater.Updater
	notifier notifier.Notifier
	lastIPv4 string
	lastIPv6 string
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
		slog.Warn("[UDDNS] error parsing duration from env. Using default `30s`")
		duration = 30 * time.Second
	}

	for {
		func() {
			defer time.Sleep(duration)
			ipResult, err := a.provider.GetIPs()
			if err != nil {
				slog.Error("[UDDNS] failed to get IPs:", "error", err)
				return
			}

			changed := false
			updateNeeded := false

			if ipResult.IPv4 != "" && ipResult.IPv4 != a.lastIPv4 {
				slog.Info("[UDDNS] new IPv4 obtained:", "ip", ipResult.IPv4)
				err = a.notifier.Notify(notifier.Notification{Message: fmt.Sprintf("New IPv4 obtained: %s", ipResult.IPv4)})
				if err != nil {
					slog.Error("failed to send notification:", "error", err)
				}
				changed = true
				updateNeeded = true
			}

			if ipResult.IPv6 != "" && ipResult.IPv6 != a.lastIPv6 {
				slog.Info("[UDDNS] new IPv6 obtained:", "ip", ipResult.IPv6)
				err = a.notifier.Notify(notifier.Notification{Message: fmt.Sprintf("New IPv6 obtained: %s", ipResult.IPv6)})
				if err != nil {
					slog.Error("failed to send notification:", "error", err)
				}
				changed = true
				updateNeeded = true
			}

			if updateNeeded {
				err = a.updater.Update(ipResult)
				if err != nil {
					slog.Error("[UDDNS] failed to update DNS records:", "error", err)
					a.notifier.Notify(notifier.Notification{Message: fmt.Sprintf("Failed to update DNS records: %s", err)})
				} else {
					slog.Info("[UDDNS] DNS records updated:", "ipv4", ipResult.IPv4, "ipv6", ipResult.IPv6)
					err = a.notifier.Notify(notifier.Notification{Message: fmt.Sprintf("DNS records updated: IPv4=%s, IPv6=%s", ipResult.IPv4, ipResult.IPv6)})
					if err != nil {
						slog.Error("failed to send notification:", "error", err)
					}
					a.lastIPv4 = ipResult.IPv4
					a.lastIPv6 = ipResult.IPv6
				}
			}

			if !changed {
				slog.Info("[UDDNS] IPs have not changed:", "ipv4", ipResult.IPv4, "ipv6", ipResult.IPv6)
			}
		}()
	}
}

func (a *App) Run() {
	a.schedule()
}
