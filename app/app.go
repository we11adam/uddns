package app

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/we11adam/uddns/internal/retry"
	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

const (
	// maxJobBackoff caps retries of failures that are unlikely to clear soon:
	// provider errors and permanent update or verify errors.
	maxJobBackoff = 30 * time.Minute
	// maxTransientJobBackoff caps retries of transient update or verify errors
	// so a recovered DNS service is used again within a few minutes.
	maxTransientJobBackoff = 5 * time.Minute
	autoVerifyInterval     = 10 * time.Minute
)

type clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

type App struct {
	jobs          []Job
	notifierName  string
	notifier      notifier.Notifier
	interval      time.Duration
	clock         clock
	jitter        func() float64
	notifications notificationQueue
}

type Job struct {
	Name            string
	ProviderName    string
	Provider        provider.Provider
	UpdaterName     string
	Updater         updater.Updater
	Record          string
	Zone            string
	Families        Families
	Verify          VerifyMode
	lastAppliedIPv4 string
	lastAppliedIPv6 string
	// lastNotifiedIPv4 and lastNotifiedIPv6 record the addresses whose change
	// notification was queued, not necessarily delivered.
	lastNotifiedIPv4   string
	lastNotifiedIPv6   string
	lastVerifiedAt     time.Time
	recordDriftPending bool
	failureCount       int
	retryAfter         time.Time
	// updateFailures counts consecutive failed DNS updates since
	// updateFailingSince. Only the first failure of an episode is notified.
	updateFailures     int
	updateFailingSince time.Time
}

type jobStatus string

const (
	jobStatusOK            jobStatus = "ok"
	jobStatusProviderError jobStatus = "provider_error"
	jobStatusUnchanged     jobStatus = "unchanged"
	jobStatusUpdaterError  jobStatus = "updater_error"
	jobStatusVerifyError   jobStatus = "verify_error"
)

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
		notifications: notificationQueue{
			wake: make(chan struct{}, 1),
		},
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
	status := jobStatusOK
	var failure error
	updated := false
	defer func() {
		if isBackoffFailure(status) {
			// Shutdown cancellation is not an operational job failure and should
			// not affect the next run of the same App.
			if ctx.Err() == nil {
				job.recordFailure(a.clock.Now(), a.interval, jobBackoffCap(a.interval, status, failure), a.jitter(), failure)
			}
		} else if status == jobStatusOK || status == jobStatusUnchanged {
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
		status = jobStatusProviderError
		slog.Error("failed to get IP addresses", job.logAttrs("error", err)...)
		return
	}
	if ipResult == nil {
		status = jobStatusProviderError
		slog.Error("provider returned no IP result", job.logAttrs()...)
		return
	}
	ipResult = filterFamilies(ipResult, job.Families)
	if err := ipResult.Validate(); err != nil {
		status = jobStatusProviderError
		slog.Error("provider returned invalid IP result", job.logAttrs("error", err)...)
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
				status = jobStatusVerifyError
				failure = err
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

	if !updateNeeded {
		// The records are correct again, for example because the address
		// changed back, so a failure episode ends without a recovery message.
		job.updateFailures = 0
		a.notifyIPChanges(job, ipResult)
		status = jobStatusUnchanged
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
		status = jobStatusUpdaterError
		failure = err
		slog.Error(
			"failed to update DNS records",
			job.logAttrs(
				"ipv4", ipResult.IPv4,
				"ipv6", ipResult.IPv6,
				"permanent", retry.IsPermanent(err),
				"error", err,
			)...,
		)
		if ctx.Err() != nil {
			return
		}
		job.updateFailures++
		if job.updateFailures == 1 {
			job.updateFailingSince = a.clock.Now()
			a.enqueueNotification(job, updateNotificationKey, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("DNS update failed for %s: %s", notificationIPSummary(ipResult), err)), Reason: notifier.ReasonUpdateFailure})
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
	message := fmt.Sprintf("DNS records updated for %s", notificationIPSummary(ipResult))
	if job.updateFailures > 0 {
		message += fmt.Sprintf(" after %s over %s", pluralize(job.updateFailures, "failed attempt"), formatElapsed(a.clock.Now().Sub(job.updateFailingSince)))
		job.updateFailures = 0
	}

	slog.Info(
		"updated DNS records",
		job.logAttrs(
			"ipv4", ipResult.IPv4,
			"ipv6", ipResult.IPv6,
		)...,
	)
	a.notifyIPChanges(job, ipResult)
	a.enqueueNotification(job, updateNotificationKey, notifier.Notification{Message: jobNotificationMessage(job, message), Reason: notifier.ReasonUpdateSuccess})
}

func (a *App) notifyIPChanges(job *Job, ipResult *provider.IpResult) {
	if ipResult.IPv4 != "" && ipResult.IPv4 != job.lastNotifiedIPv4 {
		a.enqueueNotification(job, ipv4NotificationKey, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("IPv4 address changed to %s", ipResult.IPv4)), Reason: notifier.ReasonIPChange})
		job.lastNotifiedIPv4 = ipResult.IPv4
	}

	if ipResult.IPv6 != "" && ipResult.IPv6 != job.lastNotifiedIPv6 {
		a.enqueueNotification(job, ipv6NotificationKey, notifier.Notification{Message: jobNotificationMessage(job, fmt.Sprintf("IPv6 address changed to %s", ipResult.IPv6)), Reason: notifier.ReasonIPChange})
		job.lastNotifiedIPv6 = ipResult.IPv6
	}
}

func pluralize(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// formatElapsed renders a failure episode length compactly, such as 45s, 12m
// or 2h5m.
func formatElapsed(elapsed time.Duration) string {
	if elapsed < time.Minute {
		return max(elapsed, 0).Round(time.Second).String()
	}
	elapsed = elapsed.Round(time.Minute)
	hours, minutes := int(elapsed.Hours()), int(elapsed.Minutes())%60
	switch {
	case hours == 0:
		return fmt.Sprintf("%dm", minutes)
	case minutes == 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
}

func isBackoffFailure(status jobStatus) bool {
	switch status {
	case jobStatusProviderError, jobStatusVerifyError, jobStatusUpdaterError:
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

// recordFailure schedules the next attempt with capped exponential backoff,
// waiting at least as long as err requests through retry.After.
func (job *Job) recordFailure(now time.Time, base, limit time.Duration, jitter float64, err error) {
	job.failureCount++
	delay := cappedExponentialBackoff(base, limit, job.failureCount, jitter)
	if requested, ok := retry.RetryAfter(err); ok {
		delay = max(delay, requested)
	}
	job.retryAfter = now.Add(delay)
}

func (job *Job) resetBackoff() {
	job.failureCount = 0
	job.retryAfter = time.Time{}
}

func jobBackoffCap(interval time.Duration, status jobStatus, err error) time.Duration {
	limit := maxJobBackoff
	if status != jobStatusProviderError && !retry.IsPermanent(err) {
		limit = maxTransientJobBackoff
	}
	return max(interval, limit)
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
	current, err := reader.Current(ctx, provider.FamilyRequest{
		IPv4: job.Families.IPv4,
		IPv6: job.Families.IPv6,
	})
	if err != nil {
		return nil, err
	}
	if current == nil {
		current = &provider.IpResult{}
	}
	normalized := *current
	if normalized.IPv4 == "" && normalized.IPv6 == "" {
		return &normalized, nil
	}
	if err := normalized.Validate(); err != nil {
		return nil, fmt.Errorf("verify returned invalid IP result: %w", err)
	}
	return &normalized, nil
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

	// The notification queue outlives the scheduler so that messages queued by
	// the final run are still flushed after it returns.
	queueCtx, stopQueue := context.WithCancel(context.WithoutCancel(ctx))
	queueDone := make(chan struct{})
	go func() {
		defer close(queueDone)
		a.runNotifications(queueCtx)
	}()

	a.schedule(ctx)
	stopQueue()
	<-queueDone
}
