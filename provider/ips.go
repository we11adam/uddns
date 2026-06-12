package provider

import (
	"fmt"
	"net"
	"strings"
)

func (r *IpResult) Validate() error {
	if r == nil {
		return fmt.Errorf("IP result is nil")
	}

	if r.IPv4 == "" && r.IPv6 == "" {
		return fmt.Errorf("no IP addresses found")
	}
	if r.IPv4 != "" && !IsValidIPv4(r.IPv4) {
		return fmt.Errorf("invalid IPv4 address: %s", r.IPv4)
	}
	if r.IPv6 != "" && !IsValidIPv6(r.IPv6) {
		return fmt.Errorf("invalid IPv6 address: %s", r.IPv6)
	}

	return nil
}

func IsValidIPv4(ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	return parsed != nil && parsed.To4() != nil
}

func IsValidIPv6(ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	return parsed != nil && parsed.To4() == nil && parsed.To16() != nil
}
