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
	Record       string
	Zone         string
	Families     Families
	Verify       VerifyMode
	lastIPv4     string
	lastIPv6     string
}

type VerifyMode string

const (
	VerifyAuto       VerifyMode = "auto"
	VerifyOff        VerifyMode = "off"
	VerifyUpdaterAPI VerifyMode = "updater_api"
)

type Families struct {
	IPv4 bool
	IPv6 bool
}

func AllFamilies() Families {
	return Families{IPv4: true, IPv6: true}
}

func NewJob(name, providerName string, p provider.Provider, updaterName string, u updater.Updater, record, zone string, families Families, verify VerifyMode) Job {
	if name == "" {
		name = "default"
	}
	if !families.IPv4 && !families.IPv6 {
		families = AllFamilies()
	}
	if verify == "" {
		verify = VerifyAuto
	}

	return Job{
		Name:         name,
		ProviderName: providerName,
		Provider:     p,
		UpdaterName:  updaterName,
		Updater:      u,
		Record:       strings.TrimSpace(record),
		Zone:         strings.TrimSpace(zone),
		Families:     families,
		Verify:       verify,
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
		a.runJob(ctx, &a.jobs[i])
	}
}

func (a *App) runJob(ctx context.Context, job *Job) {
	startedAt := time.Now()
	status := "ok"
	updated := false
	defer func() {
		slog.Debug(
			"update cycle finished",
			job.logAttrs(
				"status", status,
				"updated", updated,
				"duration", time.Since(startedAt),
			)...,
		)
	}()

	slog.Debug(
		"update cycle started",
		job.logAttrs(
			"last_ipv4", job.lastIPv4,
			"last_ipv6", job.lastIPv6,
		)...,
	)

	ipResult, err := job.Provider.GetIPs(ctx)
	if err != nil {
		status = "provider_error"
		slog.Error("failed to get IP addresses", job.logAttrs("error", err)...)
		return
	}
	if ipResult == nil {
		status = "provider_error"
		slog.Error("provider returned no IP result", job.logAttrs()...)
		return
	}
	if err := ipResult.Validate(); err != nil {
		status = "provider_error"
		slog.Error("provider returned invalid IP result", job.logAttrs("error", err)...)
		return
	}
	ipResult = filterFamilies(ipResult, job.Families)
	if ipResult.IPv4 == "" && ipResult.IPv6 == "" {
		status = "family_unavailable"
		slog.Warn("provider returned no requested IP addresses", job.logAttrs("families", job.Families.String())...)
		return
	}

	ipv4Changed := ipResult.IPv4 != "" && ipResult.IPv4 != job.lastIPv4
	ipv6Changed := ipResult.IPv6 != "" && ipResult.IPv6 != job.lastIPv6
	updateNeeded := ipv4Changed || ipv6Changed
	verified := false
	recordChanged := false
	var currentIPResult *provider.IpResult

	if job.shouldReadCurrentRecords() {
		currentIPResult, err = job.currentRecordIPs(ctx)
		if err != nil {
			status = "verify_error"
			slog.Error("failed to verify current DNS records", job.logAttrs("verify", job.Verify, "error", err)...)
			return
		}
		verified = true
		currentIPResult = filterFamilies(currentIPResult, job.Families)
		recordChanged = currentRecordsNeedUpdate(ipResult, currentIPResult)
		updateNeeded = updateNeeded || recordChanged
	}

	logIPCheck := slog.Debug
	if updateNeeded {
		logIPCheck = slog.Info
	}
	logIPCheck(
		"completed IP check",
		job.logAttrs(
			"ipv4", ipResult.IPv4,
			"last_ipv4", job.lastIPv4,
			"ipv4_changed", ipv4Changed,
			"ipv6", ipResult.IPv6,
			"last_ipv6", job.lastIPv6,
			"ipv6_changed", ipv6Changed,
			"verify", job.Verify,
			"verified", verified,
			"current_ipv4", ipResultValue(currentIPResult, "ipv4"),
			"current_ipv6", ipResultValue(currentIPResult, "ipv6"),
			"current_record_changed", recordChanged,
			"update_needed", updateNeeded,
		)...,
	)

	if !updateNeeded {
		status = "unchanged"
		return
	}

	if ipv4Changed {
		a.notify(ctx, "ip_change", job, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("IPv4 address changed to %s", ipResult.IPv4))})
	}

	if ipv6Changed {
		a.notify(ctx, "ip_change", job, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("IPv6 address changed to %s", ipResult.IPv6))})
	}

	slog.Debug(
		"updating DNS records",
		job.logAttrs(
			"ipv4", ipResult.IPv4,
			"ipv6", ipResult.IPv6,
		)...,
	)

	if err := job.Updater.Update(ctx, ipResult); err != nil {
		status = "updater_error"
		slog.Error(
			"failed to update DNS records",
			job.logAttrs(
				"ipv4", ipResult.IPv4,
				"ipv6", ipResult.IPv6,
				"error", err,
			)...,
		)
		a.notify(ctx, "update_failure", job, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("DNS update failed for %s: %s", notificationIPSummary(ipResult), err))})
		return
	}

	updated = true
	job.lastIPv4 = ipResult.IPv4
	job.lastIPv6 = ipResult.IPv6

	slog.Info(
		"updated DNS records",
		job.logAttrs(
			"ipv4", ipResult.IPv4,
			"ipv6", ipResult.IPv6,
		)...,
	)
	a.notify(ctx, "update_success", job, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("DNS records updated for %s", notificationIPSummary(ipResult)))})
}

func filterFamilies(ipResult *provider.IpResult, families Families) *provider.IpResult {
	filtered := &provider.IpResult{}
	if ipResult == nil {
		return filtered
	}
	if families.IPv4 {
		filtered.IPv4 = ipResult.IPv4
	}
	if families.IPv6 {
		filtered.IPv6 = ipResult.IPv6
	}
	return filtered
}

func (job *Job) shouldReadCurrentRecords() bool {
	switch job.Verify {
	case VerifyOff:
		return false
	case VerifyUpdaterAPI:
		return true
	default:
		_, ok := job.Updater.(updater.RecordReader)
		return ok
	}
}

func (job *Job) currentRecordIPs(ctx context.Context) (*provider.IpResult, error) {
	reader, ok := job.Updater.(updater.RecordReader)
	if !ok {
		return nil, fmt.Errorf("updater %s does not support verify mode %s", job.UpdaterName, VerifyUpdaterAPI)
	}
	current, err := reader.Current(ctx)
	if err != nil {
		return nil, err
	}
	if current == nil {
		current = &provider.IpResult{}
	}
	if current.IPv4 != "" && !provider.IsValidIPv4(current.IPv4) {
		return nil, fmt.Errorf("verify returned invalid IPv4 address: %s", current.IPv4)
	}
	if current.IPv6 != "" && !provider.IsValidIPv6(current.IPv6) {
		return nil, fmt.Errorf("verify returned invalid IPv6 address: %s", current.IPv6)
	}
	return current, nil
}

func currentRecordsNeedUpdate(desired, current *provider.IpResult) bool {
	if desired == nil {
		return false
	}
	if current == nil {
		current = &provider.IpResult{}
	}
	if desired.IPv4 != "" && desired.IPv4 != current.IPv4 {
		return true
	}
	if desired.IPv6 != "" && desired.IPv6 != current.IPv6 {
		return true
	}
	return false
}

func ipResultValue(ipResult *provider.IpResult, family string) string {
	if ipResult == nil {
		return ""
	}
	switch family {
	case "ipv4":
		return ipResult.IPv4
	case "ipv6":
		return ipResult.IPv6
	default:
		return ""
	}
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

func (job *Job) logAttrs(args ...any) []any {
	attrs := make([]any, 0, 10+len(args))
	if job != nil {
		attrs = append(attrs,
			"job", job.Name,
			"provider", job.ProviderName,
			"updater", job.UpdaterName,
		)
		if job.Record != "" {
			attrs = append(attrs, "record", job.Record)
		}
		if job.Zone != "" {
			attrs = append(attrs, "zone", job.Zone)
		}
	}
	return append(attrs, args...)
}

func (a *App) notify(ctx context.Context, reason string, job *Job, notification notifier.Notification) {
	if err := a.notifier.Notify(ctx, notification); err != nil {
		slog.Error(
			"failed to send notification",
			job.logAttrs(
				"notifier", a.notifierName,
				"reason", reason,
				"error", err,
			)...,
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
