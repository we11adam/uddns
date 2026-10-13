package app

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/we11adam/uddns/internal/retry"
	"github.com/we11adam/uddns/notifier"
)

const (
	notificationQueueCapacity = 100
	// notificationMaxAge drops messages that are too stale to be useful.
	notificationMaxAge = 24 * time.Hour
	// Retries after a transient notifier failure back off from
	// notificationRetryBase to notificationRetryMax.
	notificationRetryBase = 5 * time.Second
	notificationRetryMax  = 5 * time.Minute
	// Messages delivered later than notificationLateAfter mention when the
	// event was detected.
	notificationLateAfter    = time.Minute
	notificationFlushTimeout = 5 * time.Second
	notificationTimeFormat   = "2006-01-02 15:04:05 MST"
)

// Notification keys, scoped by job name. A newer message replaces a queued
// message with the same key, so an outage delivers the latest state instead
// of a backlog.
const (
	ipv4NotificationKey   = "ip_change/ipv4"
	ipv6NotificationKey   = "ip_change/ipv6"
	updateNotificationKey = "update"
)

// notificationQueue holds notifications until the notifier accepts them, so a
// notifier outage neither blocks DNS updates nor loses messages.
type notificationQueue struct {
	mu       sync.Mutex
	pending  []queuedNotification
	nextID   uint64
	failures int
	retryAt  time.Time
	wake     chan struct{}
}

type queuedNotification struct {
	id           uint64
	key          string
	notification notifier.Notification
	detectedAt   time.Time
	logAttrs     []any
}

func (n queuedNotification) attrs(args ...any) []any {
	return append(slices.Clip(n.logAttrs), args...)
}

func (a *App) enqueueNotification(job *Job, key string, notification notifier.Notification) {
	q := &a.notifications
	key = job.Name + "/" + key

	q.mu.Lock()
	q.pending = slices.DeleteFunc(q.pending, func(queued queuedNotification) bool {
		return queued.key == key
	})
	if len(q.pending) >= notificationQueueCapacity {
		dropped := q.pending[0]
		q.pending = q.pending[1:]
		slog.Warn("notification queue is full; dropping oldest notification", dropped.attrs("reason", dropped.notification.Reason, "capacity", notificationQueueCapacity)...)
	}
	q.nextID++
	q.pending = append(q.pending, queuedNotification{
		id:           q.nextID,
		key:          key,
		notification: notification,
		detectedAt:   a.clock.Now(),
		logAttrs:     job.logAttrs(),
	})
	q.mu.Unlock()

	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// deliverNotifications sends queued notifications in order until the queue is
// empty or the notifier fails transiently. Permanent failures drop the
// message. While the queue is backing off it sends nothing unless force is set.
func (a *App) deliverNotifications(ctx context.Context, force bool) {
	q := &a.notifications
	for ctx.Err() == nil {
		q.mu.Lock()
		now := a.clock.Now()
		if !force && now.Before(q.retryAt) {
			q.mu.Unlock()
			return
		}
		q.dropExpired(now)
		if len(q.pending) == 0 {
			q.mu.Unlock()
			return
		}
		next := q.pending[0]
		q.mu.Unlock()

		notification := next.notification
		if now.Sub(next.detectedAt) > notificationLateAfter {
			notification.Message += fmt.Sprintf(" (detected %s)", next.detectedAt.Format(notificationTimeFormat))
		}
		err := a.notifier.Notify(ctx, notification)

		q.mu.Lock()
		if err != nil && !retry.IsPermanent(err) {
			if ctx.Err() != nil {
				// Shutdown interrupted the delivery; the message stays queued.
				q.mu.Unlock()
				return
			}
			q.failures++
			delay := cappedExponentialBackoff(notificationRetryBase, notificationRetryMax, q.failures, a.jitter())
			if requested, ok := retry.RetryAfter(err); ok {
				delay = max(delay, requested)
			}
			q.retryAt = a.clock.Now().Add(delay)
			failures, queued := q.failures, len(q.pending)
			q.mu.Unlock()
			slog.Error("failed to send notification; will retry", next.attrs("notifier", a.notifierName, "reason", next.notification.Reason, "failures", failures, "queued", queued, "retry_in", delay, "error", err)...)
			return
		}
		q.remove(next.id)
		q.failures = 0
		q.retryAt = time.Time{}
		q.mu.Unlock()
		if err != nil {
			slog.Error("failed to send notification; dropping it", next.attrs("notifier", a.notifierName, "reason", next.notification.Reason, "error", err)...)
		}
	}
}

// runNotifications delivers queued notifications until ctx ends, then makes a
// final bounded attempt to flush what is left.
func (a *App) runNotifications(ctx context.Context) {
	q := &a.notifications
	for {
		a.deliverNotifications(ctx, false)

		var retryC <-chan time.Time
		stopTimer := func() bool { return false }
		if wait, ok := a.nextNotificationRetry(); ok {
			timer := time.NewTimer(wait)
			retryC, stopTimer = timer.C, timer.Stop
		}

		select {
		case <-ctx.Done():
			stopTimer()
			flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notificationFlushTimeout)
			a.deliverNotifications(flushCtx, true)
			cancel()
			q.mu.Lock()
			remaining := len(q.pending)
			q.mu.Unlock()
			if remaining > 0 {
				slog.Warn("dropping undelivered notifications at shutdown", "notifier", a.notifierName, "count", remaining)
			}
			return
		case <-q.wake:
		case <-retryC:
		}
		stopTimer()
	}
}

func (a *App) nextNotificationRetry() (time.Duration, bool) {
	q := &a.notifications
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return 0, false
	}
	return max(q.retryAt.Sub(a.clock.Now()), 0), true
}

func (q *notificationQueue) dropExpired(now time.Time) {
	q.pending = slices.DeleteFunc(q.pending, func(queued queuedNotification) bool {
		if now.Sub(queued.detectedAt) <= notificationMaxAge {
			return false
		}
		slog.Warn("dropping expired notification", queued.attrs("reason", queued.notification.Reason, "detected_at", queued.detectedAt, "max_age", notificationMaxAge)...)
		return true
	})
}

func (q *notificationQueue) remove(id uint64) {
	q.pending = slices.DeleteFunc(q.pending, func(queued queuedNotification) bool {
		return queued.id == id
	})
}
