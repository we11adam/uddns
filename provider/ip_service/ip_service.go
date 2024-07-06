package ip_service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/viper"
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
	provider.Register("IpService", func(v *viper.Viper) (provider.Provider, error) {
		cfg := ServiceNames{}
		err := v.UnmarshalKey("providers.ip_service", &cfg)
		if err != nil {
			return nil, err
		}
		if len(cfg) == 0 {
			return nil, errors.New("[IpService] no service names provided")
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

	ipv4, err := i.getIP(i.client4)
	if err == nil {
		result.IPv4 = ipv4
	}

	ipv6, err := i.getIP(i.client6)
	if err == nil {
		result.IPv6 = ipv6
	}

	if result.IPv4 == "" && result.IPv6 == "" {
		return nil, errors.New("[IpService] failed to get both IPv4 and IPv6 addresses")
	}

	return result, nil
}

func (i *IpService) getIP(client *resty.Client) (string, error) {
	for _, name := range *i.names {
		resp, err := client.R().Get(SERVICES[name])
		slog.Debug("[IpService] requesting IP address from:", "service", SERVICES[name])
		if err != nil || resp.StatusCode() != 200 {
			continue
		}
		ip := string(resp.Body())
		ip = strings.TrimSpace(ip)
		slog.Debug("[IpService] got IP address:", "ip", ip)
		return ip, nil
	}
	return "", errors.New("[IpService] failed to get IP address")
}
