package duckdns

import (
	"errors"
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/updater"
	"time"
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
	updater.Register("DuckDNS", func(v *viper.Viper) (updater.Updater, error) {
		cfg := Config{}
		err := v.UnmarshalKey("updaters.duckdns", &cfg)
		if err != nil {
			return nil, err
		}
		if cfg.Domain == "" || cfg.Token == "" {
			return nil, errors.New("[DuckDNS] missing required fields")
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

func (c *DuckDNS) Update(newAddr string) error {
	resp, err := c.httpclient.R().
		SetQueryParams(map[string]string{
			"domains": c.config.Domain,
			"token":   c.config.Token,
			"ip":      newAddr,
		}).Get("/update")

	if err != nil {
		return err
	}

	body := string(resp.Body())

	if body != "OK" {
		return fmt.Errorf("[DuckDNS] failed to updated DNS record: %s", body)
	}

	return nil
}
