package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Chain asks providers in order and falls back per address family, so a
// provider that only reports IPv4 can be completed by a later provider's IPv6.
type Chain struct {
	names     []string
	providers []Provider
}

func NewChain(names []string, providers []Provider) (*Chain, error) {
	if len(providers) == 0 {
		return nil, errors.New("provider chain is empty")
	}
	if len(names) != len(providers) {
		return nil, errors.New("provider chain names do not match providers")
	}
	return &Chain{names: names, providers: providers}, nil
}

func (c *Chain) GetIPs(ctx context.Context, families FamilyRequest) (*IpResult, error) {
	result := &IpResult{}
	pending := families
	var failures []error
	for i, p := range c.providers {
		if !pending.IPv4 && !pending.IPv6 {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ips, err := p.GetIPs(ctx, pending)
		if err != nil {
			failures = append(failures, fmt.Errorf("provider %s: %w", c.names[i], err))
			if i+1 < len(c.providers) {
				slog.Warn("provider failed; trying next provider", "provider", c.names[i], "next", c.names[i+1], "error", err)
			}
			continue
		}
		if ips == nil {
			continue
		}
		if pending.IPv4 && ips.IPv4 != "" {
			if IsValidIPv4(ips.IPv4) {
				result.IPv4 = strings.TrimSpace(ips.IPv4)
				pending.IPv4 = false
			} else {
				failures = append(failures, fmt.Errorf("provider %s: invalid IPv4 address: %s", c.names[i], ips.IPv4))
			}
		}
		if pending.IPv6 && ips.IPv6 != "" {
			if IsValidIPv6(ips.IPv6) {
				result.IPv6 = strings.TrimSpace(ips.IPv6)
				pending.IPv6 = false
			} else {
				failures = append(failures, fmt.Errorf("provider %s: invalid IPv6 address: %s", c.names[i], ips.IPv6))
			}
		}
	}

	if result.IPv4 == "" && result.IPv6 == "" {
		if len(failures) == 0 {
			failures = append(failures, errors.New("no IP addresses found"))
		}
		return nil, errors.Join(failures...)
	}
	return result, nil
}
