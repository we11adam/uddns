package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/we11adam/uddns/internal/retry"
	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
)

func newQueueTestApp(n notifier.Notifier) (*App, *fakeClock, *Job) {
	a := NewApp(nil, "test-notifier", n, time.Second)
	clock := &fakeClock{now: time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)}
	a.clock = clock
	a.jitter = func() float64 { return 1 }
	job := NewJob("default", "test-provider", nil, "test-updater", nil, "", "", AllFamilies(), VerifyOff)
	return a, clock, &job
}

func message(text string) notifier.Notification {
	return notifier.Notification{Message: text, Reason: notifier.ReasonIPChange}
}

func TestNotificationQueueCoalescesByKeyAndKeepsOrder(t *testing.T) {
	t.Parallel()
	n := &recordingNotifier{}
	a, _, job := newQueueTestApp(n)

	a.enqueueNotification(job, ipv4NotificationKey, message("IPv4 address changed to 192.0.2.10"))
	a.enqueueNotification(job, ipv6NotificationKey, message("IPv6 address changed to 2001:db8::1"))
	a.enqueueNotification(job, ipv4NotificationKey, message("IPv4 address changed to 192.0.2.11"))
	other := NewJob("other", "test-provider", nil, "test-updater", nil, "", "", AllFamilies(), VerifyOff)
	a.enqueueNotification(&other, ipv4NotificationKey, message("other: IPv4 address changed to 192.0.2.12"))
	a.deliverNotifications(context.Background(), false)

	want := []string{
		"IPv6 address changed to 2001:db8::1",
		"IPv4 address changed to 192.0.2.11",
		"other: IPv4 address changed to 192.0.2.12",
	}
	if got := notificationMessages(n); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("notifications = %q, want %q", got, want)
	}
}

func TestNotificationQueueBacksOffAfterTransientFailures(t *testing.T) {
	t.Parallel()
	notifyErr := errors.New("notifier unavailable")
	n := &recordingNotifier{errs: []error{notifyErr, notifyErr}}
	a, clock, job := newQueueTestApp(n)
	a.enqueueNotification(job, updateNotificationKey, message("first"))
	a.enqueueNotification(job, ipv4NotificationKey, message("second"))

	a.deliverNotifications(context.Background(), false)
	if want := clock.Now().Add(notificationRetryBase); !a.notifications.retryAt.Equal(want) {
		t.Fatalf("retryAt = %s, want %s", a.notifications.retryAt, want)
	}
	clock.Advance(notificationRetryBase)
	a.deliverNotifications(context.Background(), false)
	if want := clock.Now().Add(2 * notificationRetryBase); !a.notifications.retryAt.Equal(want) {
		t.Fatalf("retryAt = %s, want doubled backoff %s", a.notifications.retryAt, want)
	}
	clock.Advance(2*notificationRetryBase - time.Millisecond)
	a.deliverNotifications(context.Background(), false)
	if len(n.notifications) != 2 {
		t.Fatalf("expected no delivery before the backoff ends, got %d attempts", len(n.notifications))
	}
	clock.Advance(time.Millisecond)
	a.deliverNotifications(context.Background(), false)

	want := []string{"first", "first", "first", "second"}
	if got := notificationMessages(n); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("notifications = %q, want %q", got, want)
	}
	if a.notifications.failures != 0 || !a.notifications.retryAt.IsZero() {
		t.Fatalf("expected backoff reset after delivery, got failures=%d retryAt=%s", a.notifications.failures, a.notifications.retryAt)
	}
}

func TestNotificationQueueHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	n := &recordingNotifier{errs: []error{retry.After(errors.New("rate limited"), time.Minute)}}
	a, clock, job := newQueueTestApp(n)
	a.enqueueNotification(job, updateNotificationKey, message("update"))

	a.deliverNotifications(context.Background(), false)

	if want := clock.Now().Add(time.Minute); !a.notifications.retryAt.Equal(want) {
		t.Fatalf("retryAt = %s, want %s", a.notifications.retryAt, want)
	}
}

func TestNotificationQueueDropsPermanentFailures(t *testing.T) {
	t.Parallel()
	n := &recordingNotifier{errs: []error{retry.Permanent(errors.New("unknown webhook"))}}
	a, _, job := newQueueTestApp(n)
	a.enqueueNotification(job, updateNotificationKey, message("first"))
	a.enqueueNotification(job, ipv4NotificationKey, message("second"))

	a.deliverNotifications(context.Background(), false)

	if got := notificationMessages(n); fmt.Sprint(got) != fmt.Sprint([]string{"first", "second"}) {
		t.Fatalf("notifications = %q, want the permanent failure dropped and the next sent", got)
	}
	if len(a.notifications.pending) != 0 {
		t.Fatalf("expected empty queue, got %d", len(a.notifications.pending))
	}
}

func TestNotificationQueueAnnotatesLateDeliveries(t *testing.T) {
	t.Parallel()
	n := &recordingNotifier{errs: []error{errors.New("notifier unavailable")}}
	a, clock, job := newQueueTestApp(n)
	a.enqueueNotification(job, ipv4NotificationKey, message("IPv4 address changed to 192.0.2.10"))

	a.deliverNotifications(context.Background(), false)
	clock.Advance(2 * time.Minute)
	a.deliverNotifications(context.Background(), false)

	want := []string{
		"IPv4 address changed to 192.0.2.10",
		"IPv4 address changed to 192.0.2.10 (detected 2026-10-14 08:00:00 UTC)",
	}
	if got := notificationMessages(n); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("notifications = %q, want %q", got, want)
	}
}

func TestNotificationQueueDropsExpiredMessages(t *testing.T) {
	t.Parallel()
	n := &recordingNotifier{}
	a, clock, job := newQueueTestApp(n)
	a.enqueueNotification(job, ipv4NotificationKey, message("stale"))
	clock.Advance(notificationMaxAge + time.Second)
	a.enqueueNotification(job, updateNotificationKey, message("fresh"))

	a.deliverNotifications(context.Background(), false)

	if got := notificationMessages(n); fmt.Sprint(got) != fmt.Sprint([]string{"fresh"}) {
		t.Fatalf("notifications = %q, want only the fresh message", got)
	}
}

func TestNotificationQueueDropsOldestWhenFull(t *testing.T) {
	t.Parallel()
	n := &recordingNotifier{}
	a, _, _ := newQueueTestApp(n)
	for i := range notificationQueueCapacity + 1 {
		job := NewJob(fmt.Sprintf("job-%d", i), "test-provider", nil, "test-updater", nil, "", "", AllFamilies(), VerifyOff)
		a.enqueueNotification(&job, updateNotificationKey, message(job.Name))
	}

	a.deliverNotifications(context.Background(), false)

	got := notificationMessages(n)
	if len(got) != notificationQueueCapacity || got[0] != "job-1" || got[len(got)-1] != fmt.Sprintf("job-%d", notificationQueueCapacity) {
		t.Fatalf("delivered %d notifications from %q to %q, want job-1 through job-%d", len(got), got[0], got[len(got)-1], notificationQueueCapacity)
	}
}

func TestNotificationQueueKeepsMessageInterruptedByShutdown(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	n := notifierFunc(func(ctx context.Context, _ notifier.Notification) error {
		cancel()
		return ctx.Err()
	})
	a, _, job := newQueueTestApp(n)
	a.enqueueNotification(job, updateNotificationKey, message("update"))

	a.deliverNotifications(ctx, false)

	if len(a.notifications.pending) != 1 || a.notifications.failures != 0 {
		t.Fatalf("expected message kept without backoff, got pending=%d failures=%d", len(a.notifications.pending), a.notifications.failures)
	}
}

func TestRunFlushesQueuedNotificationsOnShutdown(t *testing.T) {
	t.Parallel()
	n := &recordingNotifier{}
	a, clock, job := newQueueTestApp(n)
	a.enqueueNotification(job, updateNotificationKey, message("queued"))
	// A pending backoff must not prevent the final flush.
	a.notifications.retryAt = clock.Now().Add(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a.Run(ctx)

	if got := notificationMessages(n); fmt.Sprint(got) != fmt.Sprint([]string{"queued"}) {
		t.Fatalf("notifications = %q, want queued message flushed", got)
	}
}

func TestRunDeliversNotificationsInBackground(t *testing.T) {
	t.Parallel()
	delivered := make(chan notifier.Notification, 2)
	n := notifierFunc(func(_ context.Context, notification notifier.Notification) error {
		delivered <- notification
		return nil
	})
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	job := NewJob("default", "test-provider", p, "test-updater", &recordingUpdater{}, "", "", AllFamilies(), VerifyOff)
	a := NewApp([]Job{job}, "test-notifier", n, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	for range 2 {
		select {
		case <-delivered:
		case <-time.After(time.Second):
			t.Fatal("notification was not delivered while the app was running")
		}
	}
	cancel()
	<-done
}

func TestFormatElapsed(t *testing.T) {
	t.Parallel()
	tests := map[time.Duration]string{
		-time.Second:                    "0s",
		1400 * time.Millisecond:         "1s",
		45 * time.Second:                "45s",
		12*time.Minute + 20*time.Second: "12m",
		2 * time.Hour:                   "2h",
		2*time.Hour + 5*time.Minute:     "2h5m",
	}
	for elapsed, want := range tests {
		if got := formatElapsed(elapsed); got != want {
			t.Errorf("formatElapsed(%s) = %q, want %q", elapsed, got, want)
		}
	}
}

type notifierFunc func(context.Context, notifier.Notification) error

func (f notifierFunc) Notify(ctx context.Context, notification notifier.Notification) error {
	return f(ctx, notification)
}
