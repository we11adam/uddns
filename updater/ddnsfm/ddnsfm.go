package ddnsfm

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

type Config struct {
	Domain string `mapstructure:"domain"`
	Key    string `mapstructure:"key"`
}

type DDNSFM struct {
	httpclient *resty.Client
	config     *Config
}

func init() {
	updater.Register("ddnsfm", func(v *viper.Viper) (updater.Updater, error) {
		cfg := Config{}
		err := v.UnmarshalKey("updaters.ddnsfm", &cfg)
		if err != nil {
			return nil, err
		}
		if cfg.Domain == "" || cfg.Key == "" {
			return nil, errors.New("[DDNSFM] missing required fields")
		}
		return New(&cfg), nil
	})
}

func New(cfg *Config) *DDNSFM {
	httpclient := resty.New().SetTimeout(10 * time.Second).
		SetBaseURL("https://api.ddns.fm")
	return &DDNSFM{
		httpclient: httpclient,
		config:     cfg,
	}
}

func (c *DDNSFM) Update(ips *provider.IpResult) error {
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

func (c *DDNSFM) updateIP(ip string) error {
	resp, err := c.httpclient.R().
		SetQueryParams(map[string]string{
			"domain": c.config.Domain,
			"key":    c.config.Key,
			"myip":   ip,
		}).Get("/update")

	if err != nil {
		return err
	}

	body := string(resp.Body())

	if resp.StatusCode() != 200 {
		return fmt.Errorf("[DDNSFM] failed to update DNS record: %s", body)
	}
	return nil
}
