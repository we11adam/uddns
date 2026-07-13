package provider

import (
	"fmt"
	"net/netip"
	"strings"
)

func (r *IpResult) Validate() error {
	if r == nil {
		return fmt.Errorf("IP result is nil")
	}

	r.IPv4 = strings.TrimSpace(r.IPv4)
	r.IPv6 = strings.TrimSpace(r.IPv6)

	if r.IPv4 == "" && r.IPv6 == "" {
		return fmt.Errorf("no IP addresses found")
	}
	if r.IPv4 != "" {
		addr, err := netip.ParseAddr(r.IPv4)
		if err != nil || !addr.Is4() || addr.Zone() != "" {
			return fmt.Errorf("invalid IPv4 address: %s", r.IPv4)
		}
		r.IPv4 = addr.String()
	}
	if r.IPv6 != "" {
		addr, err := netip.ParseAddr(r.IPv6)
		if err != nil || !addr.Is6() || addr.Is4In6() || addr.Zone() != "" {
			return fmt.Errorf("invalid IPv6 address: %s", r.IPv6)
		}
		r.IPv6 = addr.String()
	}

	return nil
}

func IsValidIPv4(ip string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	return err == nil && addr.Is4() && addr.Zone() == ""
}

func IsValidIPv6(ip string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	return err == nil && addr.Is6() && !addr.Is4In6() && addr.Zone() == ""
}
