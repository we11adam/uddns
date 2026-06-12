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
}

func (p *staticProvider) GetIPs() (*provider.IpResult, error) {
	return p.result, p.err
}

type recordingUpdater struct {
	calls int
	last  *provider.IpResult
	err   error
}

func (u *recordingUpdater) Update(ips *provider.IpResult) error {
	u.calls++
	u.last = ips
	return u.err
}

type recordReadingUpdater struct {
	recordingUpdater
	current *provider.IpResult
	err     error
}

func (u *recordReadingUpdater) Current() (*provider.IpResult, error) {
	return u.current, u.err
}

type recordingNotifier struct {
	notifications []notifier.Notification
	err           error
}

func (n *recordingNotifier) Notify(notification notifier.Notification) error {
	n.notifications = append(n.notifications, notification)
	return n.err
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
	if a.jobs[0].lastIPv4 != "192.0.2.10" {
		t.Fatalf("expected lastIPv4 to be updated, got %q", a.jobs[0].lastIPv4)
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
	if a.jobs[0].lastIPv4 != "" {
		t.Fatalf("expected lastIPv4 to remain empty after failed update, got %q", a.jobs[0].lastIPv4)
	}
	if len(n.notifications) != 2 {
		t.Fatalf("expected IP change and update failure notifications, got %d", len(n.notifications))
	}
	if n.notifications[1].Message != "DNS update failed for IPv4 192.0.2.10: update failed" {
		t.Fatalf("expected update failure notification message, got %q", n.notifications[1].Message)
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
	job.lastIPv4 = "192.0.2.10"
	a := NewApp([]Job{job}, "test-notifier", n, time.Second)

	a.runOnce(context.Background())

	if u.calls != 1 {
		t.Fatalf("expected DNS record drift to trigger update, got %d calls", u.calls)
	}
	if u.last == nil || u.last.IPv4 != "192.0.2.10" {
		t.Fatalf("expected updater to receive desired IPv4, got %#v", u.last)
	}
}

func TestRunOnceSkipsUpdateWhenRecordReadFails(t *testing.T) {
	p := &staticProvider{result: &provider.IpResult{IPv4: "192.0.2.10"}}
	u := &recordReadingUpdater{err: errors.New("verify failed")}
	n := &recordingNotifier{}
	job := NewJob("default", "test-provider", p, "test-updater", u, "home.example.com", "example.com", AllFamilies(), VerifyUpdaterAPI)
	job.lastIPv4 = "192.0.2.10"
	a := NewApp([]Job{job}, "test-notifier", n, time.Second)

	a.runOnce(context.Background())

	if u.calls != 0 {
		t.Fatalf("expected verify failure to skip update, got %d calls", u.calls)
	}
}

func newTestApp(p provider.Provider, u updater.Updater, n notifier.Notifier, families Families) *App {
	job := NewJob("default", "test-provider", p, "test-updater", u, "", "", families, VerifyAuto)
	return NewApp([]Job{job}, "test-notifier", n, time.Second)
}
