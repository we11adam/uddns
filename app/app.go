package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

type App struct {
	jobs         []Job
	notifierName string
	notifier     notifier.Notifier
	interval     time.Duration
}

type Job struct {
	Name         string
	ProviderName string
	Provider     provider.Provider
	UpdaterName  string
	Updater      updater.Updater
	Families     Families
	lastIPv4     string
	lastIPv6     string
}

type Families struct {
	IPv4 bool
	IPv6 bool
}

func AllFamilies() Families {
	return Families{IPv4: true, IPv6: true}
}

func NewJob(name, providerName string, p provider.Provider, updaterName string, u updater.Updater, families Families) Job {
	if name == "" {
		name = "default"
	}
	if !families.IPv4 && !families.IPv6 {
		families = AllFamilies()
	}

	return Job{
		Name:         name,
		ProviderName: providerName,
		Provider:     p,
		UpdaterName:  updaterName,
		Updater:      u,
		Families:     families,
	}
}

func NewApp(jobs []Job, notifierName string, n notifier.Notifier, interval time.Duration) *App {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &App{
		jobs:         jobs,
		notifierName: notifierName,
		notifier:     n,
		interval:     interval,
	}
}

func (a *App) schedule(ctx context.Context) {
	duration := a.interval

	slog.Info(
		"starting scheduler",
		"interval", duration,
		"notifier", a.notifierName,
		"jobs", len(a.jobs),
	)

	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			slog.Info("scheduler stopped", "reason", ctx.Err())
			return
		}

		a.runOnce(ctx)
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
		}
	}
}

func (a *App) runOnce(ctx context.Context) {
	for i := range a.jobs {
		if ctx.Err() != nil {
			return
		}
		a.runJob(&a.jobs[i])
	}
}

func (a *App) runJob(job *Job) {
	startedAt := time.Now()
	status := "ok"
	updated := false
	defer func() {
		slog.Debug(
			"update cycle finished",
			"status", status,
			"updated", updated,
			"duration", time.Since(startedAt),
			"job", job.Name,
			"provider", job.ProviderName,
			"updater", job.UpdaterName,
		)
	}()

	slog.Debug(
		"update cycle started",
		"job", job.Name,
		"provider", job.ProviderName,
		"updater", job.UpdaterName,
		"last_ipv4", job.lastIPv4,
		"last_ipv6", job.lastIPv6,
	)

	ipResult, err := job.Provider.GetIPs()
	if err != nil {
		status = "provider_error"
		slog.Error("failed to get IP addresses", "job", job.Name, "provider", job.ProviderName, "error", err)
		return
	}
	if ipResult == nil {
		status = "provider_error"
		slog.Error("provider returned no IP result", "job", job.Name, "provider", job.ProviderName)
		return
	}
	if err := ipResult.Validate(); err != nil {
		status = "provider_error"
		slog.Error("provider returned invalid IP result", "job", job.Name, "provider", job.ProviderName, "error", err)
		return
	}
	ipResult = filterFamilies(ipResult, job.Families)
	if ipResult.IPv4 == "" && ipResult.IPv6 == "" {
		status = "family_unavailable"
		slog.Warn("provider returned no requested IP addresses", "job", job.Name, "provider", job.ProviderName, "families", job.Families.String())
		return
	}

	ipv4Changed := ipResult.IPv4 != "" && ipResult.IPv4 != job.lastIPv4
	ipv6Changed := ipResult.IPv6 != "" && ipResult.IPv6 != job.lastIPv6
	updateNeeded := ipv4Changed || ipv6Changed

	slog.Info(
		"completed IP check",
		"job", job.Name,
		"provider", job.ProviderName,
		"updater", job.UpdaterName,
		"ipv4", ipResult.IPv4,
		"last_ipv4", job.lastIPv4,
		"ipv4_changed", ipv4Changed,
		"ipv6", ipResult.IPv6,
		"last_ipv6", job.lastIPv6,
		"ipv6_changed", ipv6Changed,
		"update_needed", updateNeeded,
	)

	if !updateNeeded {
		status = "unchanged"
		return
	}

	if ipv4Changed {
		a.notify("ip_change", notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("IPv4 address changed to %s", ipResult.IPv4))})
	}

	if ipv6Changed {
		a.notify("ip_change", notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("IPv6 address changed to %s", ipResult.IPv6))})
	}

	slog.Info(
		"updating DNS records",
		"job", job.Name,
		"updater", job.UpdaterName,
		"ipv4", ipResult.IPv4,
		"ipv6", ipResult.IPv6,
	)

	if err := job.Updater.Update(ipResult); err != nil {
		status = "updater_error"
		slog.Error(
			"failed to update DNS records",
			"job", job.Name,
			"updater", job.UpdaterName,
			"ipv4", ipResult.IPv4,
			"ipv6", ipResult.IPv6,
			"error", err,
		)
		a.notify("update_failure", notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("DNS update failed for %s: %s", notificationIPSummary(ipResult), err))})
		return
	}

	updated = true
	job.lastIPv4 = ipResult.IPv4
	job.lastIPv6 = ipResult.IPv6

	slog.Info(
		"updated DNS records",
		"job", job.Name,
		"updater", job.UpdaterName,
		"ipv4", ipResult.IPv4,
		"ipv6", ipResult.IPv6,
	)
	a.notify("update_success", notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("DNS records updated for %s", notificationIPSummary(ipResult)))})
}

func filterFamilies(ipResult *provider.IpResult, families Families) *provider.IpResult {
	filtered := &provider.IpResult{}
	if families.IPv4 {
		filtered.IPv4 = ipResult.IPv4
	}
	if families.IPv6 {
		filtered.IPv6 = ipResult.IPv6
	}
	return filtered
}

func (f Families) String() string {
	parts := make([]string, 0, 2)
	if f.IPv4 {
		parts = append(parts, "ipv4")
	}
	if f.IPv6 {
		parts = append(parts, "ipv6")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

func jobNotificationMessage(job *Job, message string) string {
	if job == nil || job.Name == "" || job.Name == "default" {
		return message
	}
	return fmt.Sprintf("%s: %s", job.Name, message)
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

func (a *App) Run(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.schedule(ctx)
}
