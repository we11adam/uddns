package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

type staticProvider struct {
	result *provider.IpResult
	err    error
	calls  int
}

func (p *staticProvider) GetIPs(_ context.Context) (*provider.IpResult, error) {
	p.calls++
	return p.result, p.err
}

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.now = c.now.Add(duration)
}

type recordingUpdater struct {
	calls int
	last  *provider.IpResult
	err   error
}

func (u *recordingUpdater) Update(_ context.Context, ips *provider.IpResult) error {
	u.calls++
	u.last = ips
	return u.err
}

type recordReadingUpdater struct {
	recordingUpdater
	current *provider.IpResult
	err     error
}

func (u *recordReadingUpdater) Current(_ context.Context) (*provider.IpResult, error) {
	return u.current, u.err
}

type recordingNotifier struct {
	notifications []notifier.Notification
	err           error
	errs          []error
}

func (n *recordingNotifier) Notify(_ context.Context, notification notifier.Notification) error {
	n.notifications = append(n.notifications, notification)
	if len(n.errs) > 0 {
		err := n.errs[0]
		n.errs = n.errs[1:]
		return err
	}
	return n.err
}

type blockingProvider struct {
	started chan struct{}
}

func (p *blockingProvider) GetIPs(ctx context.Context) (*provider.IpResult, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunOnceUpdatesDNSWhenIPChanges(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordingUpdater{}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())

	a.runOnce(context.Background())

	if u.calls != 1 {
		t.Fatalf("expected updater to be called once, got %d", u.calls)
	}
	if u.last == nil || u.last.IPv4 != "192.0.2.10" {
		t.Fatalf("expected updater to receive IPv4 192.0.2.10, got %#v", u.last)
	}
	if a.jobs[0].lastAppliedIPv4 != "192.0.2.10" {
		t.Fatalf("expected lastAppliedIPv4 to be updated, got %q", a.jobs[0].lastAppliedIPv4)
	}
	if len(n.notifications) != 2 {
		t.Fatalf("expected IP change and update success notifications, got %d", len(n.notifications))
	}
	if n.notifications[0].Message != "IPv4 address changed to 192.0.2.10" {
		t.Fatalf("expected IP change notification message, got %q", n.notifications[0].Message)
	}
	if n.notifications[1].Message != "DNS records updated for IPv4 192.0.2.10" {
		t.Fatalf("expected update success notification message, got %q", n.notifications[1].Message)
	}
}

func TestRunOnceSkipsUnchangedIP(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordingUpdater{}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())

	a.runOnce(context.Background())
	a.runOnce(context.Background())

	if u.calls != 1 {
		t.Fatalf("expected unchanged IP to skip second update, got %d calls", u.calls)
	}
	if len(n.notifications) != 2 {
		t.Fatalf("expected no new notifications for unchanged IP, got %d", len(n.notifications))
	}
}

func TestRunOnceDoesNotAdvanceLastIPWhenUpdateFails(t *testing.T) {
	updateErr := errors.New("update failed")
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordingUpdater{err: updateErr}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())

	a.runOnce(context.Background())

	if u.calls != 1 {
		t.Fatalf("expected updater to be called once, got %d", u.calls)
	}
	if a.jobs[0].lastAppliedIPv4 != "" {
		t.Fatalf("expected lastAppliedIPv4 to remain empty after failed update, got %q", a.jobs[0].lastAppliedIPv4)
	}
	if len(n.notifications) != 2 {
		t.Fatalf("expected IP change and update failure notifications, got %d", len(n.notifications))
	}
	if n.notifications[1].Message != "DNS update failed for IPv4 192.0.2.10: update failed" {
		t.Fatalf("expected update failure notification message, got %q", n.notifications[1].Message)
	}
}

func TestRunOnceDeduplicatesRepeatedUpdateFailures(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordingUpdater{err: errors.New("update failed")}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	a.clock = clock
	a.jitter = func() float64 { return 1 }

	for attempt := 0; attempt < 3; attempt++ {
		a.runOnce(context.Background())
		if attempt < 2 {
			clock.now = a.jobs[0].retryAfter
		}
	}

	if u.calls != 3 {
		t.Fatalf("expected three update attempts, got %d", u.calls)
	}
	if len(n.notifications) != 2 {
		t.Fatalf("expected one IP change and one failure notification, got %d attempts", len(n.notifications))
	}
	if n.notifications[0].Message != "IPv4 address changed to 192.0.2.10" {
		t.Fatalf("unexpected IP change notification: %q", n.notifications[0].Message)
	}
	if n.notifications[1].Message != "DNS update failed for IPv4 192.0.2.10: update failed" {
		t.Fatalf("unexpected failure notification: %q", n.notifications[1].Message)
	}
}

func TestRunOnceNotifiesWhenUpdateErrorChanges(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordingUpdater{err: errors.New("first error")}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	a.clock = clock
	a.jitter = func() float64 { return 1 }

	a.runOnce(context.Background())
	clock.now = a.jobs[0].retryAfter
	u.err = errors.New("second error")
	a.runOnce(context.Background())
	clock.now = a.jobs[0].retryAfter
	u.err = errors.New("second error")
	a.runOnce(context.Background())

	if len(n.notifications) != 3 {
		t.Fatalf("expected IP change and two distinct failure notifications, got %d", len(n.notifications))
	}
	if got := n.notifications[2].Message; got != "DNS update failed for IPv4 192.0.2.10: second error" {
		t.Fatalf("unexpected changed-error notification: %q", got)
	}
}

func TestRunOnceSuccessfulUpdateClearsFailureDeduplication(t *testing.T) {
	updateErr := errors.New("update failed")
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordReadingUpdater{current: &provider.IpResult{IPv4: "192.0.2.9"}}
	u.recordingUpdater.err = updateErr
	n := &recordingNotifier{}
	job := NewJob("default", "test-provider", p, "test-updater", u, "", "", AllFamilies(), VerifyAuto)
	a := NewApp([]Job{job}, "test-notifier", n, time.Second)
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	a.clock = clock
	a.jitter = func() float64 { return 1 }

	a.runOnce(context.Background())
	clock.now = a.jobs[0].retryAfter
	u.recordingUpdater.err = nil
	a.runOnce(context.Background())
	u.recordingUpdater.err = updateErr
	a.runOnce(context.Background())

	if u.calls != 3 {
		t.Fatalf("expected failure, success, and repeated failure update attempts, got %d", u.calls)
	}
	if len(n.notifications) != 4 {
		t.Fatalf("expected IP change, failure, success, and post-success failure notifications, got %d", len(n.notifications))
	}
	if got := n.notifications[3].Message; got != "DNS update failed for IPv4 192.0.2.10: update failed" {
		t.Fatalf("unexpected post-success failure notification: %q", got)
	}
}

func TestRunOnceRetriesNotificationsThatWereNotDelivered(t *testing.T) {
	notifyErr := errors.New("notification failed")
	tests := []struct {
		name         string
		notifierErrs []error
		wantRetry    string
	}{
		{
			name:         "IP change",
			notifierErrs: []error{notifyErr, nil},
			wantRetry:    "IPv4 address changed to 192.0.2.10",
		},
		{
			name:         "update failure",
			notifierErrs: []error{nil, notifyErr},
			wantRetry:    "DNS update failed for IPv4 192.0.2.10: update failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
			u := &recordingUpdater{err: errors.New("update failed")}
			n := &recordingNotifier{errs: append([]error(nil), tt.notifierErrs...)}
			a := newTestApp(p, u, n, AllFamilies())
			clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
			a.clock = clock
			a.jitter = func() float64 { return 1 }

			a.runOnce(context.Background())
			clock.now = a.jobs[0].retryAfter
			a.runOnce(context.Background())
			clock.now = a.jobs[0].retryAfter
			a.runOnce(context.Background())

			if len(n.notifications) != 3 {
				t.Fatalf("expected one failed delivery, its retry, and the other notification, got %d attempts", len(n.notifications))
			}
			if got := n.notifications[2].Message; got != tt.wantRetry {
				t.Fatalf("expected retry %q, got %q", tt.wantRetry, got)
			}
		})
	}
}

func TestRunOnceSkipsUpdateWhenProviderFails(t *testing.T) {
	providerErr := errors.New("provider failed")
	p := &staticProvider{err: providerErr}
	u := &recordingUpdater{}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())

	a.runOnce(context.Background())

	if u.calls != 0 {
		t.Fatalf("expected provider failure to skip updater, got %d calls", u.calls)
	}
	if len(n.notifications) != 0 {
		t.Fatalf("expected provider failure to skip notifications, got %d", len(n.notifications))
	}
}

func TestRunOnceSkipsUpdateWhenProviderReturnsInvalidIP(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "not-an-ip"}}
	u := &recordingUpdater{}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())

	a.runOnce(context.Background())

	if u.calls != 0 {
		t.Fatalf("expected invalid provider result to skip updater, got %d calls", u.calls)
	}
	if len(n.notifications) != 0 {
		t.Fatalf("expected invalid provider result to skip notifications, got %d", len(n.notifications))
	}
}

func TestRunReturnsWhenContextIsCanceled(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordingUpdater{}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a.Run(ctx)

	if u.calls != 0 {
		t.Fatalf("expected canceled context to skip updates, got %d calls", u.calls)
	}
}

func TestRunCancelsInFlightProvider(t *testing.T) {
	p := &blockingProvider{started: make(chan struct{})}
	u := &recordingUpdater{}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, AllFamilies())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("provider call did not start")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("app did not return after canceling an in-flight provider call")
	}
	if a.jobs[0].failureCount != 0 {
		t.Fatalf("expected shutdown cancellation not to affect backoff, got %d failures", a.jobs[0].failureCount)
	}
}

func TestRunOnceFiltersRequestedFamilies(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10", IPv6: "2001:db8::1"}}
	u := &recordingUpdater{}
	n := &recordingNotifier{}
	a := newTestApp(p, u, n, Families{IPv4: true})

	a.runOnce(context.Background())

	if u.calls != 1 {
		t.Fatalf("expected updater to be called once, got %d", u.calls)
	}
	if u.last == nil || u.last.IPv4 != "192.0.2.10" || u.last.IPv6 != "" {
		t.Fatalf("expected updater to receive only IPv4, got %#v", u.last)
	}
}

func TestRunOncePrefixesNotificationsForNamedJobs(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordingUpdater{}
	n := &recordingNotifier{}
	job := NewJob("home", "test-provider", p, "test-updater", u, "home.example.com", "example.com", AllFamilies(), VerifyAuto)
	a := NewApp([]Job{job}, "test-notifier", n, time.Second)

	a.runOnce(context.Background())

	if len(n.notifications) != 2 {
		t.Fatalf("expected two notifications, got %d", len(n.notifications))
	}
	if n.notifications[0].Message != "home: IPv4 address changed to 192.0.2.10" {
		t.Fatalf("expected job-prefixed notification, got %q", n.notifications[0].Message)
	}
}

func TestRunOnceUpdatesWhenCurrentRecordDrifts(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordReadingUpdater{current: &provider.IpResult{IPv4: "192.0.2.9"}}
	n := &recordingNotifier{}
	job := NewJob("default", "test-provider", p, "test-updater", u, "home.example.com", "example.com", AllFamilies(), VerifyUpdaterAPI)
	job.lastAppliedIPv4 = "192.0.2.10"
	job.lastNotifiedIPv4 = "192.0.2.10"
	a := NewApp([]Job{job}, "test-notifier", n, time.Second)

	a.runOnce(context.Background())

	if u.calls != 1 {
		t.Fatalf("expected DNS record drift to trigger update, got %d calls", u.calls)
	}
	if u.last == nil || u.last.IPv4 != "192.0.2.10" {
		t.Fatalf("expected updater to receive desired IPv4, got %#v", u.last)
	}
}

func TestRunOnceAutoUpdatesChangedIPWhenRecordReadFails(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordReadingUpdater{err: errors.New("verify failed")}
	n := &recordingNotifier{}
	job := NewJob("default", "test-provider", p, "test-updater", u, "home.example.com", "example.com", AllFamilies(), VerifyAuto)
	job.lastAppliedIPv4 = "192.0.2.9"
	job.lastNotifiedIPv4 = "192.0.2.9"
	a := NewApp([]Job{job}, "test-notifier", n, time.Second)

	a.runOnce(context.Background())

	if u.calls != 1 {
		t.Fatalf("expected changed provider IP to update despite auto verify failure, got %d calls", u.calls)
	}
	if a.jobs[0].lastAppliedIPv4 != "192.0.2.10" {
		t.Fatalf("expected changed IP to be applied, got %q", a.jobs[0].lastAppliedIPv4)
	}
	if a.jobs[0].failureCount != 0 {
		t.Fatalf("expected successful update to avoid verify backoff, got %d failures", a.jobs[0].failureCount)
	}
}

func TestRunOnceAutoSkipsUnchangedIPWhenRecordReadFails(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordReadingUpdater{err: errors.New("verify failed")}
	n := &recordingNotifier{}
	job := NewJob("default", "test-provider", p, "test-updater", u, "home.example.com", "example.com", AllFamilies(), VerifyAuto)
	job.lastAppliedIPv4 = "192.0.2.10"
	job.lastNotifiedIPv4 = "192.0.2.10"
	a := NewApp([]Job{job}, "test-notifier", n, time.Second)

	a.runOnce(context.Background())

	if u.calls != 0 {
		t.Fatalf("expected unchanged provider IP not to update after auto verify failure, got %d calls", u.calls)
	}
	if a.jobs[0].failureCount != 0 {
		t.Fatalf("expected best-effort auto verify failure not to back off, got %d failures", a.jobs[0].failureCount)
	}
	if len(n.notifications) != 0 {
		t.Fatalf("expected no notifications for unchanged IP, got %d", len(n.notifications))
	}
}

func TestRunOnceStrictVerifyBlocksChangedIPWhenRecordReadFails(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordReadingUpdater{err: errors.New("verify failed")}
	n := &recordingNotifier{}
	job := NewJob("default", "test-provider", p, "test-updater", u, "home.example.com", "example.com", AllFamilies(), VerifyUpdaterAPI)
	job.lastAppliedIPv4 = "192.0.2.9"
	job.lastNotifiedIPv4 = "192.0.2.9"
	a := NewApp([]Job{job}, "test-notifier", n, time.Second)

	a.runOnce(context.Background())

	if u.calls != 0 {
		t.Fatalf("expected strict verify failure to block changed IP update, got %d calls", u.calls)
	}
	if a.jobs[0].failureCount != 1 {
		t.Fatalf("expected strict verify failure to back off, got %d failures", a.jobs[0].failureCount)
	}
	if len(n.notifications) != 0 {
		t.Fatalf("expected strict verify failure before notifications, got %d", len(n.notifications))
	}
}

func TestCappedExponentialBackoffGrowsAndCaps(t *testing.T) {
	base := 10 * time.Second
	max := time.Minute
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 1, want: 10 * time.Second},
		{failures: 2, want: 20 * time.Second},
		{failures: 3, want: 40 * time.Second},
		{failures: 4, want: time.Minute},
		{failures: 5, want: time.Minute},
	}

	for _, tt := range tests {
		if got := cappedExponentialBackoff(base, max, tt.failures, 1); got != tt.want {
			t.Fatalf("failure %d: expected %s, got %s", tt.failures, tt.want, got)
		}
	}
}

func TestCappedExponentialBackoffAppliesEqualJitter(t *testing.T) {
	base := 10 * time.Second
	tests := []struct {
		jitter float64
		want   time.Duration
	}{
		{jitter: -1, want: 5 * time.Second},
		{jitter: 0, want: 5 * time.Second},
		{jitter: 0.5, want: 7500 * time.Millisecond},
		{jitter: 1, want: 10 * time.Second},
		{jitter: 2, want: 10 * time.Second},
	}

	for _, tt := range tests {
		if got := cappedExponentialBackoff(base, time.Minute, 1, tt.jitter); got != tt.want {
			t.Fatalf("jitter %.1f: expected %s, got %s", tt.jitter, tt.want, got)
		}
	}
}

func TestRunOnceBacksOffFailureStatuses(t *testing.T) {
	tests := []struct {
		name string
		job  func() Job
	}{
		{
			name: "provider",
			job: func() Job {
				return NewJob("provider", "test-provider", &staticProvider{err: errors.New("provider failed")}, "test-updater", &recordingUpdater{}, "", "", AllFamilies(), VerifyOff)
			},
		},
		{
			name: "verify",
			job: func() Job {
				return NewJob("verify", "test-provider", &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}, "test-updater", &recordReadingUpdater{err: errors.New("verify failed")}, "", "", AllFamilies(), VerifyUpdaterAPI)
			},
		},
		{
			name: "updater",
			job: func() Job {
				return NewJob("updater", "test-provider", &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}, "test-updater", &recordingUpdater{err: errors.New("update failed")}, "", "", AllFamilies(), VerifyOff)
			},
		},
		{
			name: "family unavailable",
			job: func() Job {
				return NewJob("family", "test-provider", &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}, "test-updater", &recordingUpdater{}, "", "", Families{IPv6: true}, VerifyOff)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Unix(1_700_000_000, 0)
			clock := &fakeClock{now: now}
			a := NewApp([]Job{tt.job()}, "test-notifier", &recordingNotifier{}, time.Second)
			a.clock = clock
			a.jitter = func() float64 { return 1 }

			a.runOnce(context.Background())

			if a.jobs[0].failureCount != 1 {
				t.Fatalf("expected one failure, got %d", a.jobs[0].failureCount)
			}
			if want := now.Add(time.Second); !a.jobs[0].retryAfter.Equal(want) {
				t.Fatalf("expected retry at %s, got %s", want, a.jobs[0].retryAfter)
			}
		})
	}
}

func TestRunOnceBackoffDoesNotBlockOtherJobs(t *testing.T) {
	failingProvider := &staticProvider{err: errors.New("provider failed")}
	healthyProvider := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	jobs := []Job{
		NewJob("failing", "test-provider", failingProvider, "test-updater", &recordingUpdater{}, "", "", AllFamilies(), VerifyOff),
		NewJob("healthy", "test-provider", healthyProvider, "test-updater", &recordingUpdater{}, "", "", AllFamilies(), VerifyOff),
	}
	a := NewApp(jobs, "test-notifier", &recordingNotifier{}, time.Second)
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	a.clock = clock
	a.jitter = func() float64 { return 1 }

	a.runOnce(context.Background())
	clock.Advance(500 * time.Millisecond)
	a.runOnce(context.Background())

	if failingProvider.calls != 1 {
		t.Fatalf("expected backed-off provider to be called once, got %d", failingProvider.calls)
	}
	if healthyProvider.calls != 2 {
		t.Fatalf("expected healthy provider to run during another job's backoff, got %d calls", healthyProvider.calls)
	}
}

func TestRunOnceSuccessAndUnchangedResetBackoff(t *testing.T) {
	tests := []struct {
		name     string
		lastIPv4 string
	}{
		{name: "success"},
		{name: "unchanged", lastIPv4: "192.0.2.10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
			job := NewJob(tt.name, "test-provider", &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}, "test-updater", &recordingUpdater{}, "", "", AllFamilies(), VerifyOff)
			job.lastAppliedIPv4 = tt.lastIPv4
			job.lastNotifiedIPv4 = tt.lastIPv4
			job.failureCount = 3
			job.retryAfter = clock.Now()
			a := NewApp([]Job{job}, "test-notifier", &recordingNotifier{}, time.Second)
			a.clock = clock
			a.jitter = func() float64 { return 1 }

			a.runOnce(context.Background())

			if a.jobs[0].failureCount != 0 || !a.jobs[0].retryAfter.IsZero() {
				t.Fatalf("expected backoff reset, got failures=%d retry_after=%s", a.jobs[0].failureCount, a.jobs[0].retryAfter)
			}
		})
	}
}

func newTestApp(p provider.Provider, u updater.Updater, n notifier.Notifier, families Families) *App {
	job := NewJob("default", "test-provider", p, "test-updater", u, "", "", families, VerifyAuto)
	return NewApp([]Job{job}, "test-notifier", n, time.Second)
}
