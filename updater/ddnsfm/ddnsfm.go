package ddnsfm

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
	Key    string `mapstructure:"key"`
}

type DDNSFM struct {
	httpclient *resty.Client
	config     *Config
}

func init() {
	updater.Register("DDNSFM", func(v *viper.Viper) (updater.Updater, error) {
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

func (c *DDNSFM) Update(newAddr string) error {
	resp, err := c.httpclient.R().
		SetQueryParams(map[string]string{
			"key":    c.config.Key,
			"domain": c.config.Domain,
			"myip":   newAddr,
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
