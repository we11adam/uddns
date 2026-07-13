package ip_service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/we11adam/uddns/provider"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestServicesUseHTTPS(t *testing.T) {
	for name, serviceURL := range SERVICES {
		t.Run(name, func(t *testing.T) {
			parsed, err := url.Parse(serviceURL)
			if err != nil {
				t.Fatalf("parse service URL %q: %v", serviceURL, err)
			}
			if parsed.Scheme != "https" {
				t.Fatalf("service URL %q must use HTTPS", serviceURL)
			}
		})
	}
}

func TestServiceRedirectPolicy(t *testing.T) {
	parseURL := func(rawURL string) *url.URL {
		t.Helper()
		parsed, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("parse URL %q: %v", rawURL, err)
		}
		return parsed
	}
	request := func(rawURL string) *http.Request {
		return &http.Request{URL: parseURL(rawURL)}
	}

	checkRedirect := createClient("tcp4").GetClient().CheckRedirect
	if checkRedirect == nil {
		t.Fatal("expected redirect policy to be configured")
	}
	origin := request("https://example.com/start")
	tests := []struct {
		name    string
		target  string
		via     []*http.Request
		wantErr bool
	}{
		{
			name:   "same origin",
			target: "https://example.com/result",
			via:    []*http.Request{origin},
		},
		{
			name:    "scheme downgrade",
			target:  "http://example.com/result",
			via:     []*http.Request{origin},
			wantErr: true,
		},
		{
			name:    "host change",
			target:  "https://127.0.0.1/result",
			via:     []*http.Request{origin},
			wantErr: true,
		},
		{
			name:    "port change",
			target:  "https://example.com:8443/result",
			via:     []*http.Request{origin},
			wantErr: true,
		},
		{
			name:    "redirect limit",
			target:  "https://example.com/result",
			via:     []*http.Request{origin, origin, origin, origin},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkRedirect(request(tt.target), tt.via)
			if (err != nil) != tt.wantErr {
				t.Fatalf("redirect policy error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetIPsCancelsInFlightRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	client := createClient("tcp4")
	client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-request.Context().Done()
		return nil, request.Context().Err()
	}))
	names := ServiceNames{"ip.fm"}
	service := &IpService{client4: client, client6: createClient("tcp6"), names: &names}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.GetIPs(ctx)
		done <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("IP service request did not start")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("IP service request did not return after context cancellation")
	}
}

type testConfig struct {
	services ServiceNames
}

func (c testConfig) GetString(string) string {
	return ""
}

func (c testConfig) IsSet(key string) bool {
	return key == "providers.ip_service"
}

func (c testConfig) UnmarshalKey(_ string, rawVal any) error {
	target := rawVal.(*ServiceNames)
	*target = c.services
	return nil
}

func TestGetProviderRejectsUnsupportedService(t *testing.T) {
	_, _, err := provider.GetProvider(testConfig{services: ServiceNames{"missing"}})
	if err == nil {
		t.Fatal("expected unsupported service error")
	}
	if !strings.Contains(err.Error(), `unsupported IP service "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsValidIPFamily(t *testing.T) {
	tests := []struct {
		name   string
		ip     string
		family string
		want   bool
	}{
		{name: "public IPv4", ip: "8.8.8.8", family: "ipv4", want: true},
		{name: "public IPv6", ip: "2606:4700:4700::1111", family: "ipv6", want: true},
		{name: "private IPv4", ip: "10.0.0.1", family: "ipv4"},
		{name: "private IPv6 ULA", ip: "fd00::1", family: "ipv6"},
		{name: "IPv4 loopback", ip: "127.0.0.1", family: "ipv4"},
		{name: "IPv6 loopback", ip: "::1", family: "ipv6"},
		{name: "IPv4 link local", ip: "169.254.10.20", family: "ipv4"},
		{name: "IPv6 link local", ip: "fe80::1", family: "ipv6"},
		{name: "IPv4 multicast", ip: "224.0.0.1", family: "ipv4"},
		{name: "IPv6 multicast", ip: "ff02::1", family: "ipv6"},
		{name: "IPv4 unspecified", ip: "0.0.0.0", family: "ipv4"},
		{name: "IPv6 unspecified", ip: "::", family: "ipv6"},
		{name: "CGNAT", ip: "100.64.0.1", family: "ipv4"},
		{name: "IPv4 documentation", ip: "192.0.2.10", family: "ipv4"},
		{name: "IPv6 documentation", ip: "2001:db8::1", family: "ipv6"},
		{name: "IPv4 benchmark", ip: "198.18.0.1", family: "ipv4"},
		{name: "IPv6 benchmark", ip: "2001:2::1", family: "ipv6"},
		{name: "IPv4 reserved", ip: "240.0.0.1", family: "ipv4"},
		{name: "IPv6 reserved", ip: "3fff::1", family: "ipv6"},
		{name: "IPv6 NAT64", ip: "64:ff9b::808:808", family: "ipv6"},
		{name: "IPv6 6to4", ip: "2002:0808:0808::1", family: "ipv6"},
		{name: "IPv4-mapped IPv6", ip: "::ffff:8.8.8.8", family: "ipv6"},
		{name: "wrong IPv4 family", ip: "2606:4700:4700::1111", family: "ipv4"},
		{name: "wrong IPv6 family", ip: "8.8.8.8", family: "ipv6"},
		{name: "unknown family", ip: "8.8.8.8", family: "unknown"},
		{name: "invalid address", ip: "not-an-ip", family: "ipv4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidIPFamily(tt.ip, tt.family); got != tt.want {
				t.Fatalf("isValidIPFamily(%q, %q) = %v, want %v", tt.ip, tt.family, got, tt.want)
			}
		})
	}
}
