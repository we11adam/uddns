package netif

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/we11adam/uddns/provider"
)

type Netif struct {
	iface *net.Interface
}

type Config struct {
	Name string `mapstructure:"name"`
}

func init() {
	provider.Register("NetworkInterface", "providers.netif", func(v provider.ConfigReader) (provider.Provider, error) {
		if !v.IsSet("providers.netif") {
			return nil, provider.ErrNotConfigured
		}

		cfg := Config{}
		err := v.UnmarshalKey("providers.netif", &cfg)
		if err != nil {
			return nil, err
		}
		if cfg.Name == "" {
			return nil, fmt.Errorf("missing network interface name")
		}
		return New(&cfg)
	})
}

func New(cfg *Config) (*Netif, error) {
	if cfg == nil {
		return nil, fmt.Errorf("network interface config is nil")
	}
	iface, err := net.InterfaceByName(cfg.Name)
	if err != nil {
		return nil, err
	}
	return &Netif{
		iface: iface,
	}, nil
}

func (n *Netif) GetIPs(ctx context.Context, families provider.FamilyRequest) (*provider.IpResult, error) {
	if !families.IPv4 && !families.IPv6 {
		return nil, fmt.Errorf("no IP families requested")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	addrs, err := n.iface.Addrs()
	if err != nil {
		return nil, err
	}

	result := selectPublishableIPs(addrs, families)

	if result.IPv4 == "" && result.IPv6 == "" {
		return nil, fmt.Errorf("no IP address found on network interface %s", n.iface.Name)
	}

	return result, nil
}

func selectPublishableIPs(addrs []net.Addr, families provider.FamilyRequest) *provider.IpResult {
	var selected4, selected6 netip.Addr

	for _, addr := range addrs {
		candidate, ok := addressIP(addr)
		if !ok || !candidate.IsGlobalUnicast() {
			continue
		}

		// Interface.Addrs does not expose temporary-address flags. Prefer public
		// addresses, then choose numerically within the same scope so selection is
		// stable across enumeration order changes. Private addresses remain valid
		// fallbacks for internal DNS use.
		if candidate.Is4() && families.IPv4 {
			if preferAddress(candidate, selected4) {
				selected4 = candidate
			}
		} else if candidate.Is6() && families.IPv6 && preferAddress(candidate, selected6) {
			selected6 = candidate
		}
	}

	result := &provider.IpResult{}
	if selected4.IsValid() {
		result.IPv4 = selected4.String()
	}
	if selected6.IsValid() {
		result.IPv6 = selected6.String()
	}
	return result
}

func preferAddress(candidate, current netip.Addr) bool {
	if !current.IsValid() {
		return true
	}
	if candidate.IsPrivate() != current.IsPrivate() {
		return !candidate.IsPrivate()
	}
	return candidate.Less(current)
}

func addressIP(addr net.Addr) (netip.Addr, bool) {
	var ip net.IP
	switch addr := addr.(type) {
	case *net.IPNet:
		if addr != nil {
			ip = addr.IP
		}
	case *net.IPAddr:
		if addr != nil {
			ip = addr.IP
		}
	}

	parsed, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, false
	}
	return parsed.Unmap(), true
}
