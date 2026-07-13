package app

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

const (
	maxJobBackoff      = 30 * time.Minute
	autoVerifyInterval = 10 * time.Minute
)

type clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

type App struct {
	jobs         []Job
	notifierName string
	notifier     notifier.Notifier
	interval     time.Duration
	clock        clock
	jitter       func() float64
}

type Job struct {
	Name                      string
	ProviderName              string
	Provider                  provider.Provider
	UpdaterName               string
	Updater                   updater.Updater
	Record                    string
	Zone                      string
	Families                  Families
	Verify                    VerifyMode
	lastAppliedIPv4           string
	lastAppliedIPv6           string
	lastNotifiedIPv4          string
	lastNotifiedIPv6          string
	lastNotifiedUpdateFailure string
	lastVerifiedAt            time.Time
	recordDriftPending        bool
	failureCount              int
	retryAfter                time.Time
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
	if n == nil {
		n = &notifier.Noop{}
		if notifierName == "" {
			notifierName = "No-op"
		}
	}
	return &App{
		jobs:         jobs,
		notifierName: notifierName,
		notifier:     n,
		interval:     interval,
		clock:        systemClock{},
		jitter:       rand.Float64,
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
		now := a.clock.Now()
		job := &a.jobs[i]
		if now.Before(job.retryAfter) {
			slog.Debug(
				"skipping job during failure backoff",
				job.logAttrs(
					"failure_count", job.failureCount,
					"retry_after", job.retryAfter,
					"remaining", job.retryAfter.Sub(now),
				)...,
			)
			continue
		}
		a.runJob(ctx, job)
	}
}

func (a *App) runJob(ctx context.Context, job *Job) {
	startedAt := time.Now()
	status := "ok"
	updated := false
	defer func() {
		if isBackoffFailure(status) {
			// Shutdown cancellation is not an operational job failure and should
			// not affect the next run of the same App.
			if ctx.Err() == nil {
				job.recordFailure(a.clock.Now(), a.interval, jobBackoffCap(a.interval), a.jitter())
			}
		} else if status == "ok" || status == "unchanged" {
			job.resetBackoff()
		}
		slog.Debug(
			"update cycle finished",
			job.logAttrs(
				"status", status,
				"updated", updated,
				"duration", time.Since(startedAt),
				"failure_count", job.failureCount,
				"retry_after", job.retryAfter,
			)...,
		)
	}()

	slog.Debug(
		"update cycle started",
		job.logAttrs(
			"last_applied_ipv4", job.lastAppliedIPv4,
			"last_applied_ipv6", job.lastAppliedIPv6,
			"last_notified_ipv4", job.lastNotifiedIPv4,
			"last_notified_ipv6", job.lastNotifiedIPv6,
		)...,
	)

	ipResult, err := job.Provider.GetIPs(ctx, provider.FamilyRequest{
		IPv4: job.Families.IPv4,
		IPv6: job.Families.IPv6,
	})
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
	ipResult = filterFamilies(ipResult, job.Families)
	if err := ipResult.Validate(); err != nil {
		status = "provider_error"
		slog.Error("provider returned invalid IP result", job.logAttrs("error", err)...)
		return
	}
	if ipResult.IPv4 == "" && ipResult.IPv6 == "" {
		status = "family_unavailable"
		slog.Warn("provider returned no requested IP addresses", job.logAttrs("families", job.Families.String())...)
		return
	}

	ipv4Changed := ipResult.IPv4 != "" && ipResult.IPv4 != job.lastAppliedIPv4
	ipv6Changed := ipResult.IPv6 != "" && ipResult.IPv6 != job.lastAppliedIPv6
	ipv4NotificationNeeded := ipResult.IPv4 != "" && ipResult.IPv4 != job.lastNotifiedIPv4
	ipv6NotificationNeeded := ipResult.IPv6 != "" && ipResult.IPv6 != job.lastNotifiedIPv6
	providerIPChanged := ipv4Changed || ipv6Changed
	verified := false
	recordChanged := job.recordDriftPending
	updateNeeded := providerIPChanged || recordChanged
	var currentIPResult *provider.IpResult

	if job.shouldReadCurrentRecords(a.clock.Now(), providerIPChanged) {
		currentIPResult, err = job.currentRecordIPs(ctx)
		if err != nil {
			if job.Verify == VerifyUpdaterAPI {
				status = "verify_error"
				slog.Error("failed to verify current DNS records", job.logAttrs("verify", job.Verify, "error", err)...)
				return
			}
			slog.Warn(
				"failed to verify current DNS records; continuing with provider result",
				job.logAttrs(
					"verify", job.Verify,
					"provider_ip_changed", updateNeeded,
					"error", err,
				)...,
			)
			currentIPResult = nil
		} else {
			verified = true
			job.lastVerifiedAt = a.clock.Now()
			currentIPResult = filterFamilies(currentIPResult, job.Families)
			job.initializeAppliedFromCurrent(ipResult, currentIPResult)
			ipv4Changed = ipResult.IPv4 != "" && ipResult.IPv4 != job.lastAppliedIPv4
			ipv6Changed = ipResult.IPv6 != "" && ipResult.IPv6 != job.lastAppliedIPv6
			providerIPChanged = ipv4Changed || ipv6Changed
			recordChanged = currentRecordsNeedUpdate(ipResult, currentIPResult)
			job.recordDriftPending = recordChanged
			updateNeeded = providerIPChanged || recordChanged
		}
	}

	logIPCheck := slog.Debug
	if updateNeeded {
		logIPCheck = slog.Info
	}
	logIPCheck(
		"completed IP check",
		job.logAttrs(
			"ipv4", ipResult.IPv4,
			"last_applied_ipv4", job.lastAppliedIPv4,
			"ipv4_changed", ipv4Changed,
			"last_notified_ipv4", job.lastNotifiedIPv4,
			"ipv4_notification_needed", ipv4NotificationNeeded,
			"ipv6", ipResult.IPv6,
			"last_applied_ipv6", job.lastAppliedIPv6,
			"ipv6_changed", ipv6Changed,
			"last_notified_ipv6", job.lastNotifiedIPv6,
			"ipv6_notification_needed", ipv6NotificationNeeded,
			"verify", job.Verify,
			"verified", verified,
			"last_verified_at", job.lastVerifiedAt,
			"current_ipv4", ipResultValue(currentIPResult, "ipv4"),
			"current_ipv6", ipResultValue(currentIPResult, "ipv6"),
			"current_record_changed", recordChanged,
			"update_needed", updateNeeded,
		)...,
	)

	if ipv4NotificationNeeded {
		if a.notify(ctx, "ip_change", job, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("IPv4 address changed to %s", ipResult.IPv4))}) {
			job.lastNotifiedIPv4 = ipResult.IPv4
		}
	}

	if ipv6NotificationNeeded {
		if a.notify(ctx, "ip_change", job, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("IPv6 address changed to %s", ipResult.IPv6))}) {
			job.lastNotifiedIPv6 = ipResult.IPv6
		}
	}

	if !updateNeeded {
		status = "unchanged"
		return
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
		failureNotification := notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("DNS update failed for %s: %s", notificationIPSummary(ipResult), err))}
		if failureNotification.Message != job.lastNotifiedUpdateFailure && a.notify(ctx, "update_failure", job, failureNotification) {
			job.lastNotifiedUpdateFailure = failureNotification.Message
		}
		return
	}

	updated = true
	if ipResult.IPv4 != "" {
		job.lastAppliedIPv4 = ipResult.IPv4
	}
	if ipResult.IPv6 != "" {
		job.lastAppliedIPv6 = ipResult.IPv6
	}
	job.recordDriftPending = false
	job.lastNotifiedUpdateFailure = ""

	slog.Info(
		"updated DNS records",
		job.logAttrs(
			"ipv4", ipResult.IPv4,
			"ipv6", ipResult.IPv6,
		)...,
	)
	a.notify(ctx, "update_success", job, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("DNS records updated for %s", notificationIPSummary(ipResult)))})
}

func isBackoffFailure(status string) bool {
	switch status {
	case "provider_error", "verify_error", "updater_error", "family_unavailable":
		return true
	default:
		return false
	}
}

func (job *Job) initializeAppliedFromCurrent(desired, current *provider.IpResult) {
	if desired == nil || current == nil {
		return
	}
	if job.lastAppliedIPv4 == "" && desired.IPv4 != "" && desired.IPv4 == current.IPv4 {
		job.lastAppliedIPv4 = desired.IPv4
	}
	if job.lastAppliedIPv6 == "" && desired.IPv6 != "" && desired.IPv6 == current.IPv6 {
		job.lastAppliedIPv6 = desired.IPv6
	}
}

func (job *Job) recordFailure(now time.Time, base, max time.Duration, jitter float64) {
	job.failureCount++
	job.retryAfter = now.Add(cappedExponentialBackoff(base, max, job.failureCount, jitter))
}

func (job *Job) resetBackoff() {
	job.failureCount = 0
	job.retryAfter = time.Time{}
}

func jobBackoffCap(interval time.Duration) time.Duration {
	if interval > maxJobBackoff {
		return interval
	}
	return maxJobBackoff
}

// cappedExponentialBackoff returns an equal-jitter delay in the range
// [exponential/2, exponential], where exponential grows from base and never
// exceeds max. jitter is clamped to [0, 1] to keep the helper deterministic
// and safe for injected test sources.
func cappedExponentialBackoff(base, max time.Duration, failures int, jitter float64) time.Duration {
	if base <= 0 || failures <= 0 {
		return 0
	}
	if max <= 0 {
		max = base
	}

	delay := min(base, max)
	for i := 1; i < failures && delay < max; i++ {
		if delay > max/2 {
			delay = max
		} else {
			delay *= 2
		}
	}
	if jitter < 0 {
		jitter = 0
	} else if jitter > 1 {
		jitter = 1
	}

	half := delay / 2
	return half + time.Duration(float64(delay-half)*jitter)
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

func (job *Job) shouldReadCurrentRecords(now time.Time, providerIPChanged bool) bool {
	switch job.Verify {
	case VerifyOff:
		return false
	case VerifyUpdaterAPI:
		return true
	default:
		_, ok := job.Updater.(updater.RecordReader)
		if !ok {
			return false
		}
		return job.lastVerifiedAt.IsZero() || providerIPChanged || !now.Before(job.lastVerifiedAt.Add(autoVerifyInterval))
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

func (a *App) notify(ctx context.Context, reason string, job *Job, notification notifier.Notification) bool {
	if err := a.notifier.Notify(ctx, notification); err != nil {
		slog.Error(
			"failed to send notification",
			job.logAttrs(
				"notifier", a.notifierName,
				"reason", reason,
				"error", err,
			)...,
		)
		return false
	}
	return true
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
