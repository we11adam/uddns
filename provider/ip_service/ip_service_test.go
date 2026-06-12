package ip_service

import (
	"strings"
	"testing"

	"github.com/we11adam/uddns/provider"
)

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
