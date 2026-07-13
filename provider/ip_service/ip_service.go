package ip_service

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/provider"
)

var SERVICES = map[string]string{
	"ip.fm":       "https://ip.fm/myip",
	"ip.sb":       "https://ip.sb",
	"ifconfig.me": "https://ifconfig.me",
	"3322.org":    "https://members.3322.org/dyndns/getip",
}

const maxServiceRedirects = 3

var (
	publicIPv6Prefix  = netip.MustParsePrefix("2000::/3")
	nonPublicPrefixes = []netip.Prefix{
		// IPv4 special-purpose and reserved ranges not suitable for public DNS.
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.31.196.0/24"),
		netip.MustParsePrefix("192.52.193.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("192.175.48.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),

		// IPv6 protocol assignments, documentation, transition, and reserved ranges.
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("2620:4f:8000::/48"),
		netip.MustParsePrefix("3fff::/20"),
	}
)

type ServiceNames []string

type IpService struct {
	client4 *resty.Client
	client6 *resty.Client
	names   *ServiceNames
}

func init() {
	provider.Register("IpService", "providers.ip_service", func(v provider.ConfigReader) (provider.Provider, error) {
		if !v.IsSet("providers.ip_service") {
			return nil, provider.ErrNotConfigured
		}

		cfg := ServiceNames{}
		err := v.UnmarshalKey("providers.ip_service", &cfg)
		if err != nil {
			return nil, err
		}
		if len(cfg) == 0 {
			return nil, fmt.Errorf("no IP service names provided")
		}
		for _, name := range cfg {
			if _, ok := SERVICES[name]; !ok {
				return nil, fmt.Errorf("unsupported IP service %q; supported services: %s", name, strings.Join(supportedServiceNames(), ", "))
			}
		}
		return New(&cfg)
	})
}

func New(names *ServiceNames) (*IpService, error) {
	client4 := createClient("tcp4")
	client6 := createClient("tcp6")

	return &IpService{
		client4: client4,
		client6: client6,
		names:   names,
	}, nil
}

func createClient(network string) *resty.Client {
	httpClient := resty.New()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout: 1 * time.Second,
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	httpClient.SetTransport(transport)
	httpClient.SetTimeout(5 * time.Second)
	httpClient.SetRedirectPolicy(sameOriginHTTPSRedirectPolicy(maxServiceRedirects))
	httpClient.RemoveProxy().SetHeaders(map[string]string{"User-Agent": "curl/8.6.0"})
	return httpClient
}

func sameOriginHTTPSRedirectPolicy(maxRedirects int) resty.RedirectPolicy {
	return resty.RedirectPolicyFunc(func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return fmt.Errorf("redirect rejected: missing original request")
		}
		if len(via) > maxRedirects {
			return fmt.Errorf("redirect rejected: stopped after %d redirects", maxRedirects)
		}

		origin := via[0].URL
		if !strings.EqualFold(origin.Scheme, "https") || !strings.EqualFold(req.URL.Scheme, origin.Scheme) {
			return fmt.Errorf("redirect rejected: scheme must remain HTTPS")
		}
		if !strings.EqualFold(req.URL.Host, origin.Host) {
			return fmt.Errorf("redirect rejected: host must remain %q", origin.Host)
		}
		return nil
	})
}

func (i *IpService) GetIPs(ctx context.Context) (*provider.IpResult, error) {
	result := &provider.IpResult{}

	ipv4, err := i.getIP(ctx, i.client4, "ipv4")
	if err == nil {
		result.IPv4 = ipv4
	} else if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	ipv6, err := i.getIP(ctx, i.client6, "ipv6")
	if err == nil {
		result.IPv6 = ipv6
	} else if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if result.IPv4 == "" && result.IPv6 == "" {
		return nil, fmt.Errorf("failed to get both IPv4 and IPv6 addresses")
	}

	return result, nil
}

func (i *IpService) getIP(ctx context.Context, client *resty.Client, family string) (string, error) {
	for _, name := range *i.names {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		serviceURL, ok := SERVICES[name]
		if !ok {
			continue
		}

		slog.Debug("requesting IP address", "provider", "ip_service", "service", serviceURL, "family", family)
		resp, err := client.R().SetContext(ctx).Get(serviceURL)
		if err != nil {
			slog.Debug("failed to request IP address", "provider", "ip_service", "service", serviceURL, "family", family, "error", err)
			continue
		}
		if resp.StatusCode() != 200 {
			slog.Debug("unexpected IP address response status", "provider", "ip_service", "service", serviceURL, "family", family, "status", resp.StatusCode())
			continue
		}
		ip := string(resp.Body())
		ip = strings.TrimSpace(ip)
		if !isValidIPFamily(ip, family) {
			slog.Debug("ignoring invalid IP address response", "provider", "ip_service", "service", serviceURL, "family", family, "ip", ip)
			continue
		}
		slog.Debug("got IP address", "provider", "ip_service", "family", family, "ip", ip)
		return ip, nil
	}
	return "", fmt.Errorf("failed to get IP address from all services")
}

func isValidIPFamily(ip, family string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || addr.Zone() != "" {
		return false
	}

	switch family {
	case "ipv4":
		if !addr.Is4() {
			return false
		}
	case "ipv6":
		if !addr.Is6() || addr.Is4In6() {
			return false
		}
	default:
		return false
	}
	return isPublicRoutable(addr)
}

func isPublicRoutable(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Is4In6() || addr.Zone() != "" ||
		!addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
		return false
	}
	if addr.Is6() && !publicIPv6Prefix.Contains(addr) {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func supportedServiceNames() []string {
	names := make([]string, 0, len(SERVICES))
	for name := range SERVICES {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
