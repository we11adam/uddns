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
	providerName string
	provider     provider.Provider
	updaterName  string
	updater      updater.Updater
	notifierName string
	notifier     notifier.Notifier
	lastIPv4     string
	lastIPv6     string
}

func NewApp(providerName string, p provider.Provider, updaterName string, u updater.Updater, notifierName string, n notifier.Notifier) *App {
	return &App{
		providerName: providerName,
		provider:     p,
		updaterName:  updaterName,
		updater:      u,
		notifierName: notifierName,
		notifier:     n,
	}
}

func intervalFromEnv() time.Duration {
	interval := os.Getenv("UDDNS_INTERVAL")
	if interval == "" {
		interval = "30s"
	}

	duration, err := time.ParseDuration(interval)
	if err != nil {
		slog.Warn("invalid UDDNS_INTERVAL, using default", "value", interval, "default", "30s", "error", err)
		return 30 * time.Second
	}
	return duration
}

func (a *App) schedule() {
	duration := intervalFromEnv()

	slog.Info(
		"starting scheduler",
		"interval", duration,
		"provider", a.providerName,
		"updater", a.updaterName,
		"notifier", a.notifierName,
	)

	for {
		a.runOnce()
		time.Sleep(duration)
	}
}

func (a *App) runOnce() {
	startedAt := time.Now()
	status := "ok"
	updated := false
	defer func() {
		slog.Debug(
			"update cycle finished",
			"status", status,
			"updated", updated,
			"duration", time.Since(startedAt),
			"provider", a.providerName,
			"updater", a.updaterName,
		)
	}()

	slog.Debug(
		"update cycle started",
		"provider", a.providerName,
		"updater", a.updaterName,
		"last_ipv4", a.lastIPv4,
		"last_ipv6", a.lastIPv6,
	)

	ipResult, err := a.provider.GetIPs()
	if err != nil {
		status = "provider_error"
		slog.Error("failed to get IP addresses", "provider", a.providerName, "error", err)
		return
	}
	if ipResult == nil {
		status = "provider_error"
		slog.Error("provider returned no IP result", "provider", a.providerName)
		return
	}

	ipv4Changed := ipResult.IPv4 != "" && ipResult.IPv4 != a.lastIPv4
	ipv6Changed := ipResult.IPv6 != "" && ipResult.IPv6 != a.lastIPv6
	updateNeeded := ipv4Changed || ipv6Changed

	slog.Info(
		"ip check completed",
		"provider", a.providerName,
		"updater", a.updaterName,
		"ipv4", ipResult.IPv4,
		"last_ipv4", a.lastIPv4,
		"ipv4_changed", ipv4Changed,
		"ipv6", ipResult.IPv6,
		"last_ipv6", a.lastIPv6,
		"ipv6_changed", ipv6Changed,
		"update_needed", updateNeeded,
	)

	if !updateNeeded {
		status = "unchanged"
		return
	}

	if ipv4Changed {
		a.notify("ip_change", notifier.Notification{Message: fmt.Sprintf("New IPv4 obtained: %s", ipResult.IPv4)})
	}

	if ipv6Changed {
		a.notify("ip_change", notifier.Notification{Message: fmt.Sprintf("New IPv6 obtained: %s", ipResult.IPv6)})
	}

	slog.Info(
		"updating dns records",
		"updater", a.updaterName,
		"ipv4", ipResult.IPv4,
		"ipv6", ipResult.IPv6,
	)

	if err := a.updater.Update(ipResult); err != nil {
		status = "updater_error"
		slog.Error(
			"failed to update dns records",
			"updater", a.updaterName,
			"ipv4", ipResult.IPv4,
			"ipv6", ipResult.IPv6,
			"error", err,
		)
		a.notify("update_failure", notifier.Notification{Message: fmt.Sprintf("Failed to update DNS records: %s", err)})
		return
	}

	updated = true
	a.lastIPv4 = ipResult.IPv4
	a.lastIPv6 = ipResult.IPv6

	slog.Info(
		"dns records updated",
		"updater", a.updaterName,
		"ipv4", ipResult.IPv4,
		"ipv6", ipResult.IPv6,
	)
	a.notify("update_success", notifier.Notification{Message: fmt.Sprintf("DNS records updated: IPv4=%s, IPv6=%s", ipResult.IPv4, ipResult.IPv6)})
}

func (a *App) notify(reason string, notification notifier.Notification) {
	if err := a.notifier.Notify(notification); err != nil {
		slog.Error(
			"failed to send notification",
			"notifier", a.notifierName,
			"reason", reason,
			"error", err,
		)
	}
}

func (a *App) Run() {
	a.schedule()
}
