package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubProvider struct {
	result *IpResult
	err    error
	calls  []FamilyRequest
}

func (s *stubProvider) GetIPs(_ context.Context, families FamilyRequest) (*IpResult, error) {
	s.calls = append(s.calls, families)
	return s.result, s.err
}

func TestChainUsesFirstProviderWhenComplete(t *testing.T) {
	t.Parallel()
	first := &stubProvider{result: &IpResult{IPv4: "203.0.113.10", IPv6: "2001:db8::10"}}
	second := &stubProvider{result: &IpResult{IPv4: "198.51.100.20"}}
	chain := mustChain(t, first, second)

	result, err := chain.GetIPs(context.Background(), FamilyRequest{IPv4: true, IPv6: true})
	if err != nil {
		t.Fatalf("GetIPs returned error: %v", err)
	}
	if result.IPv4 != "203.0.113.10" || result.IPv6 != "2001:db8::10" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(second.calls) != 0 {
		t.Fatalf("expected the second provider to be skipped, got %d calls", len(second.calls))
	}
}

func TestChainFallsBackPerFamily(t *testing.T) {
	t.Parallel()
	first := &stubProvider{result: &IpResult{IPv4: "203.0.113.10"}}
	second := &stubProvider{result: &IpResult{IPv4: "198.51.100.20", IPv6: "2001:db8::20"}}
	chain := mustChain(t, first, second)

	result, err := chain.GetIPs(context.Background(), FamilyRequest{IPv4: true, IPv6: true})
	if err != nil {
		t.Fatalf("GetIPs returned error: %v", err)
	}
	if result.IPv4 != "203.0.113.10" || result.IPv6 != "2001:db8::20" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(second.calls) != 1 || second.calls[0] != (FamilyRequest{IPv6: true}) {
		t.Fatalf("expected the second provider to be asked only for IPv6, got %+v", second.calls)
	}
}

func TestChainSkipsFailedAndInvalidProviders(t *testing.T) {
	t.Parallel()
	failed := &stubProvider{err: errors.New("router unreachable")}
	invalid := &stubProvider{result: &IpResult{IPv4: "not-an-ip"}}
	valid := &stubProvider{result: &IpResult{IPv4: "198.51.100.20"}}
	chain := mustChain(t, failed, invalid, valid)

	result, err := chain.GetIPs(context.Background(), FamilyRequest{IPv4: true})
	if err != nil {
		t.Fatalf("GetIPs returned error: %v", err)
	}
	if result.IPv4 != "198.51.100.20" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestChainReportsAllFailures(t *testing.T) {
	t.Parallel()
	chain := mustChain(t,
		&stubProvider{err: errors.New("router unreachable")},
		&stubProvider{err: errors.New("service unavailable")},
	)

	_, err := chain.GetIPs(context.Background(), FamilyRequest{IPv4: true})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"provider p0: router unreachable", "provider p1: service unavailable"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in error %q", want, err)
		}
	}
}

func TestChainStopsOnCanceledContext(t *testing.T) {
	t.Parallel()
	first := &stubProvider{result: &IpResult{IPv4: "203.0.113.10"}}
	chain := mustChain(t, first)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := chain.GetIPs(ctx, FamilyRequest{IPv4: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if len(first.calls) != 0 {
		t.Fatal("expected no provider calls after cancellation")
	}
}

func TestNewChainRejectsMismatchedInput(t *testing.T) {
	t.Parallel()
	if _, err := NewChain(nil, nil); err == nil {
		t.Fatal("expected empty chain error")
	}
	if _, err := NewChain([]string{"a", "b"}, []Provider{&stubProvider{}}); err == nil {
		t.Fatal("expected mismatched names error")
	}
}

func mustChain(t *testing.T, providers ...Provider) *Chain {
	t.Helper()

	names := make([]string, len(providers))
	for i := range providers {
		names[i] = "p" + string(rune('0'+i))
	}
	chain, err := NewChain(names, providers)
	if err != nil {
		t.Fatalf("NewChain returned error: %v", err)
	}
	return chain
}
