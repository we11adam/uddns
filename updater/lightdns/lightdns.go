package lightdns

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/internal/dnsname"
	"github.com/we11adam/uddns/internal/redact"
	"github.com/we11adam/uddns/internal/restyretry"
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
		SetBaseURL("https://api.lightdns.io")
	restyretry.ConfigureTransient(httpclient)
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
