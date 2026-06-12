package ip_service

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/provider"
)

var SERVICES = map[string]string{
	"ip.fm":       "https://ip.fm/myip",
	"ip.sb":       "http://ip.sb",
	"ifconfig.me": "http://ifconfig.me",
	"3322.org":    "http://members.3322.org/dyndns/getip",
}

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
	httpClient.RemoveProxy().SetHeaders(map[string]string{"User-Agent": "curl/8.6.0"})
	return httpClient
}

func (i *IpService) GetIPs() (*provider.IpResult, error) {
	result := &provider.IpResult{}

	ipv4, err := i.getIP(i.client4, "ipv4")
	if err == nil {
		result.IPv4 = ipv4
	}

	ipv6, err := i.getIP(i.client6, "ipv6")
	if err == nil {
		result.IPv6 = ipv6
	}

	if result.IPv4 == "" && result.IPv6 == "" {
		return nil, fmt.Errorf("failed to get both IPv4 and IPv6 addresses")
	}

	return result, nil
}

func (i *IpService) getIP(client *resty.Client, family string) (string, error) {
	for _, name := range *i.names {
		serviceURL, ok := SERVICES[name]
		if !ok {
			continue
		}

		slog.Debug("requesting IP address", "provider", "ip_service", "service", serviceURL, "family", family)
		resp, err := client.R().Get(serviceURL)
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
	switch family {
	case "ipv4":
		return provider.IsValidIPv4(ip)
	case "ipv6":
		return provider.IsValidIPv6(ip)
	default:
		return false
	}
}

func supportedServiceNames() []string {
	names := make([]string, 0, len(SERVICES))
	for name := range SERVICES {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
