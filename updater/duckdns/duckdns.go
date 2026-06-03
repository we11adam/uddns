package duckdns

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

type Config struct {
	Domain string `mapstructure:"domain"`
	Token  string `mapstructure:"token"`
}

type DuckDNS struct {
	httpclient *resty.Client
	config     *Config
}

func init() {
	updater.Register("DuckDNS", "updaters.duckdns", func(v updater.ConfigReader) (updater.Updater, error) {
		if !v.IsSet("updaters.duckdns") {
			return nil, updater.ErrNotConfigured
		}

		cfg := Config{}
		err := v.UnmarshalKey("updaters.duckdns", &cfg)
		if err != nil {
			return nil, err
		}
		if cfg.Domain == "" || cfg.Token == "" {
			return nil, fmt.Errorf("missing required DuckDNS fields")
		}
		return New(&cfg), nil
	})
}

func New(cfg *Config) *DuckDNS {
	httpclient := resty.New().SetTimeout(10 * time.Second).
		SetBaseURL("https://www.duckdns.org")
	return &DuckDNS{
		httpclient: httpclient,
		config:     cfg,
	}
}

func (c *DuckDNS) Update(ips *provider.IpResult) error {
	if ips.IPv4 != "" {
		err := c.updateIP(ips.IPv4)
		if err != nil {
			return fmt.Errorf("failed to update IPv4: %w", err)
		}
	}

	if ips.IPv6 != "" {
		err := c.updateIP(ips.IPv6)
		if err != nil {
			return fmt.Errorf("failed to update IPv6: %w", err)
		}
	}

	return nil
}

func (c *DuckDNS) updateIP(ip string) error {
	resp, err := c.httpclient.R().
		SetQueryParams(map[string]string{
			"domains": c.config.Domain,
			"token":   c.config.Token,
			"ip":      ip,
		}).Get("/update")

	if err != nil {
		return err
	}

	body := string(resp.Body())

	if body != "OK" {
		return fmt.Errorf("failed to update DuckDNS DNS record: %s", body)
	}

	slog.Info("updated DNS record", "updater", "duckdns", "ip", ip)

	return nil
}
