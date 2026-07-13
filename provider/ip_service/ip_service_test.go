package ip_service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we11adam/uddns/internal/restyretry"
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

func TestNewRejectsNilServiceNames(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil service names to be rejected")
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

func TestServiceClientLimitsResponseBodies(t *testing.T) {
	client := createClient("tcp4")
	if client.ResponseBodyLimit != responseBodyLimit {
		t.Fatalf("response body limit = %d, want %d", client.ResponseBodyLimit, responseBodyLimit)
	}
}

func TestServiceClientRetryPolicy(t *testing.T) {
	client := createClient("tcp4")
	if client.RetryCount != restyretry.MaxRetries {
		t.Fatalf("retry count = %d, want %d", client.RetryCount, restyretry.MaxRetries)
	}
	if client.GetClient().Timeout != requestTimeout {
		t.Fatalf("request timeout = %s, want %s", client.GetClient().Timeout, requestTimeout)
	}
}

func TestGetIPsRetriesTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch attempts.Add(1) {
		case 1:
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
		case 2:
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		default:
			_, _ = io.WriteString(w, "8.8.8.8")
		}
	}))
	defer server.Close()

	service := testIPService(t, server.URL)
	service.client4.SetRetryWaitTime(time.Millisecond).SetRetryMaxWaitTime(2 * time.Millisecond)
	result, err := service.GetIPs(context.Background(), provider.FamilyRequest{IPv4: true})
	if err != nil {
		t.Fatalf("get IPs after transient failures: %v", err)
	}
	if result.IPv4 != "8.8.8.8" {
		t.Fatalf("IPv4 = %q, want %q", result.IPv4, "8.8.8.8")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestGetIPsDoesNotRetryPermanentClientError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "invalid request", http.StatusBadRequest)
	}))
	defer server.Close()

	service := testIPService(t, server.URL)
	service.client4.SetRetryWaitTime(time.Millisecond).SetRetryMaxWaitTime(2 * time.Millisecond)
	if _, err := service.GetIPs(context.Background(), provider.FamilyRequest{IPv4: true}); err == nil {
		t.Fatal("expected permanent client error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestGetIPsJoinsRedactedErrorsWhenAllServicesFail(t *testing.T) {
	const (
		firstName    = "first.test"
		secondName   = "second.test"
		secret       = "service-secret"
		responseBody = "sensitive response body"
	)
	firstURL := "https://first.invalid/ip?token=" + secret
	secondURL := "https://second.invalid/ip"
	setTestServices(t, map[string]string{firstName: firstURL, secondName: secondURL})

	client := createClient("tcp4")
	client.SetRetryCount(0)
	client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "first.invalid":
			return nil, errors.New("request failed for " + request.URL.String())
		case "second.invalid":
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(responseBody)),
				Request:    request,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected request host %q", request.URL.Host)
		}
	}))
	names := ServiceNames{firstName, secondName}
	service := &IpService{client4: client, client6: createClient("tcp6"), names: &names}

	_, err := service.GetIPs(context.Background(), provider.FamilyRequest{IPv4: true})
	if err == nil {
		t.Fatal("expected all-service failure")
	}
	message := err.Error()
	for _, expected := range []string{firstName, secondName, "request failed", "HTTP status 502"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("joined error %q does not contain %q", message, expected)
		}
	}
	for _, sensitive := range []string{secret, firstURL, secondURL, responseBody} {
		if strings.Contains(message, sensitive) {
			t.Fatalf("joined error leaks %q: %q", sensitive, message)
		}
	}
}

func TestGetIPsReturnsFirstSuccessAfterEarlierFailure(t *testing.T) {
	const (
		firstName  = "first.test"
		secondName = "second.test"
	)
	setTestServices(t, map[string]string{
		firstName:  "https://first.invalid/ip",
		secondName: "https://second.invalid/ip",
	})

	client := createClient("tcp4")
	client.SetRetryCount(0)
	var calls []string
	client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.URL.Hostname())
		status := http.StatusServiceUnavailable
		body := "temporary failure"
		if request.URL.Hostname() == "second.invalid" {
			status = http.StatusOK
			body = "8.8.8.8"
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	}))
	names := ServiceNames{firstName, secondName}
	service := &IpService{client4: client, client6: createClient("tcp6"), names: &names}

	result, err := service.GetIPs(context.Background(), provider.FamilyRequest{IPv4: true})
	if err != nil {
		t.Fatalf("get IPs after fallback success: %v", err)
	}
	if result.IPv4 != "8.8.8.8" {
		t.Fatalf("IPv4 = %q, want 8.8.8.8", result.IPv4)
	}
	if got, want := strings.Join(calls, ","), "first.invalid,second.invalid"; got != want {
		t.Fatalf("request order = %q, want %q", got, want)
	}
}

func setTestServices(t *testing.T, services map[string]string) {
	t.Helper()
	previous := make(map[string]string, len(services))
	existed := make(map[string]bool, len(services))
	for name, serviceURL := range services {
		previous[name], existed[name] = SERVICES[name]
		SERVICES[name] = serviceURL
	}
	t.Cleanup(func() {
		for name := range services {
			if existed[name] {
				SERVICES[name] = previous[name]
			} else {
				delete(SERVICES, name)
			}
		}
	})
}

func TestGetIPsStopsRetriesWhenContextIsCanceled(t *testing.T) {
	var attempts atomic.Int32
	firstResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "temporary failure")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		if attempt == 1 {
			close(firstResponse)
		}
	}))
	defer server.Close()

	service := testIPService(t, server.URL)
	service.client4.SetRetryWaitTime(500 * time.Millisecond).SetRetryMaxWaitTime(500 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.GetIPs(ctx, provider.FamilyRequest{IPv4: true})
		done <- err
	}()

	select {
	case <-firstResponse:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("first request did not complete")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry loop did not stop after context cancellation")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func testIPService(t *testing.T, serviceURL string) *IpService {
	t.Helper()
	const name = "test.local"
	previous, existed := SERVICES[name]
	SERVICES[name] = serviceURL
	t.Cleanup(func() {
		if existed {
			SERVICES[name] = previous
		} else {
			delete(SERVICES, name)
		}
	})
	names := ServiceNames{name}
	service, err := New(&names)
	if err != nil {
		t.Fatalf("create IP service: %v", err)
	}
	return service
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
		_, err := service.GetIPs(ctx, provider.FamilyRequest{IPv4: true})
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

func TestGetIPsRequestsOnlySelectedFamilies(t *testing.T) {
	tests := []struct {
		name      string
		families  provider.FamilyRequest
		wantIPv4  string
		wantIPv6  string
		wantCalls [2]int
	}{
		{
			name:      "IPv4 only",
			families:  provider.FamilyRequest{IPv4: true},
			wantIPv4:  "8.8.8.8",
			wantCalls: [2]int{1, 0},
		},
		{
			name:      "IPv6 only",
			families:  provider.FamilyRequest{IPv6: true},
			wantIPv6:  "2606:4700:4700::1111",
			wantCalls: [2]int{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := [2]int{}
			client4 := createClient("tcp4")
			client4.SetTransport(ipResponseTransport(&calls[0], "8.8.8.8"))
			client6 := createClient("tcp6")
			client6.SetTransport(ipResponseTransport(&calls[1], "2606:4700:4700::1111"))
			names := ServiceNames{"ip.fm"}
			service := &IpService{client4: client4, client6: client6, names: &names}

			result, err := service.GetIPs(context.Background(), tt.families)
			if err != nil {
				t.Fatalf("get IPs: %v", err)
			}
			if result.IPv4 != tt.wantIPv4 || result.IPv6 != tt.wantIPv6 {
				t.Fatalf("result = %+v, want IPv4=%q IPv6=%q", result, tt.wantIPv4, tt.wantIPv6)
			}
			if calls != tt.wantCalls {
				t.Fatalf("request calls = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func ipResponseTransport(calls *int, body string) http.RoundTripper {
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		*calls = *calls + 1
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})
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
