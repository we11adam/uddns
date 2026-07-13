package lightdns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/internal/dnsname"
	"github.com/we11adam/uddns/internal/redact"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

type Config struct {
	Domain string `mapstructure:"domain"`
	Key    string `mapstructure:"key"`
}

type LightDNS struct {
	httpclient *resty.Client
	config     *Config
}

const (
	requestTimeout    = 10 * time.Second
	responseBodyLimit = 64 << 10
	maxUpdateRetries  = 3
	retryWaitTime     = 100 * time.Millisecond
	retryMaxWaitTime  = 2 * time.Second
)

func init() {
	updater.Register("LightDNS", "updaters.lightdns", func(v updater.ConfigReader) (updater.Updater, error) {
		if !v.IsSet("updaters.lightdns") {
			return nil, updater.ErrNotConfigured
		}

		cfg := Config{}
		err := v.UnmarshalKey("updaters.lightdns", &cfg)
		if err != nil {
			return nil, err
		}
		if cfg.Domain == "" || cfg.Key == "" {
			return nil, fmt.Errorf("missing required LightDNS fields")
		}
		return New(&cfg)
	})
}

func New(cfg *Config) (*LightDNS, error) {
	if cfg == nil {
		return nil, fmt.Errorf("LightDNS config is nil")
	}
	domain, err := dnsname.Normalize(cfg.Domain)
	if err != nil {
		return nil, fmt.Errorf("invalid LightDNS domain: %w", err)
	}
	normalizedConfig := *cfg
	normalizedConfig.Domain = domain

	httpclient := resty.New().SetTimeout(requestTimeout).
		SetResponseBodyLimit(responseBodyLimit).
		SetRetryCount(maxUpdateRetries).
		SetRetryWaitTime(retryWaitTime).
		SetRetryMaxWaitTime(retryMaxWaitTime).
		AddRetryCondition(shouldRetryUpdate).
		SetBaseURL("https://api.lightdns.io")
	return &LightDNS{
		httpclient: httpclient,
		config:     &normalizedConfig,
	}, nil
}

func (c *LightDNS) Update(ctx context.Context, ips *provider.IpResult) error {
	if ips.IPv4 != "" {
		err := c.updateIP(ctx, ips.IPv4)
		if err != nil {
			return fmt.Errorf("failed to update IPv4: %w", err)
		}
	}

	if ips.IPv6 != "" {
		err := c.updateIP(ctx, ips.IPv6)
		if err != nil {
			return fmt.Errorf("failed to update IPv6: %w", err)
		}
	}

	return nil
}

func (c *LightDNS) updateIP(ctx context.Context, ip string) error {
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	resp, err := c.httpclient.R().
		SetContext(requestCtx).
		SetQueryParams(map[string]string{
			"domain": c.config.Domain,
			"key":    c.config.Key,
			"myip":   ip,
		}).Get("/update")

	if err != nil {
		return redact.Error(err, c.config.Key)
	}

	body := string(resp.Body())

	if resp.StatusCode() != 200 {
		return fmt.Errorf("failed to update LightDNS DNS record: %s", redact.String(body, c.config.Key))
	}

	slog.Info("updated DNS record", "updater", "lightdns", "record", c.config.Domain, "ip", ip)

	return nil
}

func shouldRetryUpdate(response *resty.Response, err error) bool {
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return true
		}
		var netErr net.Error
		return errors.As(err, &netErr)
	}
	if response == nil {
		return false
	}
	status := response.StatusCode()
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}
