package app

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
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
		slog.Warn("invalid update interval, using default", "env_var", "UDDNS_INTERVAL", "value", interval, "default", "30s", "error", err)
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
		"completed IP check",
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
		a.notify("ip_change", notifier.Notification{Message: fmt.Sprintf("IPv4 address changed to %s", ipResult.IPv4)})
	}

	if ipv6Changed {
		a.notify("ip_change", notifier.Notification{Message: fmt.Sprintf("IPv6 address changed to %s", ipResult.IPv6)})
	}

	slog.Info(
		"updating DNS records",
		"updater", a.updaterName,
		"ipv4", ipResult.IPv4,
		"ipv6", ipResult.IPv6,
	)

	if err := a.updater.Update(ipResult); err != nil {
		status = "updater_error"
		slog.Error(
			"failed to update DNS records",
			"updater", a.updaterName,
			"ipv4", ipResult.IPv4,
			"ipv6", ipResult.IPv6,
			"error", err,
		)
		a.notify("update_failure", notifier.Notification{Message: fmt.Sprintf("DNS update failed for %s: %s", notificationIPSummary(ipResult), err)})
		return
	}

	updated = true
	a.lastIPv4 = ipResult.IPv4
	a.lastIPv6 = ipResult.IPv6

	slog.Info(
		"updated DNS records",
		"updater", a.updaterName,
		"ipv4", ipResult.IPv4,
		"ipv6", ipResult.IPv6,
	)
	a.notify("update_success", notifier.Notification{Message: fmt.Sprintf("DNS records updated for %s", notificationIPSummary(ipResult))})
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

func notificationIPSummary(ipResult *provider.IpResult) string {
	if ipResult == nil {
		return "no IP addresses"
	}

	parts := make([]string, 0, 2)
	if ipResult.IPv4 != "" {
		parts = append(parts, "IPv4 "+ipResult.IPv4)
	}
	if ipResult.IPv6 != "" {
		parts = append(parts, "IPv6 "+ipResult.IPv6)
	}
	if len(parts) == 0 {
		return "no IP addresses"
	}
	return strings.Join(parts, ", ")
}

func (a *App) Run() {
	a.schedule()
}
