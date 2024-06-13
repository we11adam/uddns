package ip_service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
)

var SERVICES = map[string]string{
	"ifconfig.me": "http://ifconfig.me",
	"ip.sb":       "http://ip.sb",
	"3322.org":    "http://members.3322.org/dyndns/getip",
}

type ServiceNames []string

type IpService struct {
	client *resty.Client
	names  *ServiceNames
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

func New(names *ServiceNames) (provider.Provider, error) {
	httpClient := resty.New()

	// Set transport to use tcp4
	// ip_service use your access ip to get your public ip
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// just use tcp4
			dialer := &net.Dialer{}
			return dialer.DialContext(ctx, "tcp4", addr)
		},
	}
	httpClient.SetTransport(transport)

	// 1. Remove proxy so that the request is sent directly to the service
	// 2. Set user agent to curl so that the service does not return an HTML page
	httpClient.RemoveProxy().SetHeaders(map[string]string{"User-Agent": "curl/8.6.0"})
	return &IpService{
		client: httpClient,
		names:  names,
	}, nil
}

func (i *IpService) Ip() (string, error) {
	for _, name := range *i.names {
		resp, err := i.client.R().Get(SERVICES[name])
		slog.Debug("[IpService] requesting IP address from:", "service", SERVICES[name])
		if err != nil || resp.StatusCode() != 200 {
			continue
		}
		ip := string(resp.Body())
		ip = strings.Trim(ip, "\n")
		slog.Debug("[IpService] got IP address:", "ip", ip)
		return ip, nil
	}
	return "", errors.New("[IpService] failed to get IP address")
}
