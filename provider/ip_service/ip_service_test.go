package ip_service

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/we11adam/uddns/provider"
)

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
	if !isValidIPFamily("192.0.2.10", "ipv4") {
		t.Fatal("expected IPv4 address to be valid for ipv4")
	}
	if isValidIPFamily("2001:db8::1", "ipv4") {
		t.Fatal("expected IPv6 address to be invalid for ipv4")
	}
	if !isValidIPFamily("2001:db8::1", "ipv6") {
		t.Fatal("expected IPv6 address to be valid for ipv6")
	}
	if isValidIPFamily("not-an-ip", "ipv6") {
		t.Fatal("expected invalid address to be invalid for ipv6")
	}
}
