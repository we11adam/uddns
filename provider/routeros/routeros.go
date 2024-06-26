package routeros

import (
	"crypto/tls"
	"errors"
	"github.com/go-resty/resty/v2"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
)

type RouterOS struct {
	config     Config
	httpClient *resty.Client
}

type Config struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Endpoint string `mapstructure:"endpoint"`
	Insecure *bool  `yaml:"insecure"`
}

type rosInterface struct {
	ID      string `json:".id"`
	Comment string `json:"comment,omitempty"`
	Name    string `json:"name"`
	Type    string `json:"type"`
}

type rosAddress struct {
	ID              string `json:".id"`
	ActualInterface string `json:"actual-interface"`
	Address         string `json:"address"`
	Comment         string `json:"comment"`
	Disabled        string `json:"disabled"`
	Interface       string `json:"interface"`
	Network         string `json:"network"`
}

func init() {
	provider.Register("RouterOS", func(v *viper.Viper) (provider.Provider, error) {
		cfg := Config{}
		err := v.UnmarshalKey("providers.routeros", &cfg)
		if err != nil {
			return nil, err
		}

		if cfg.Username == "" || cfg.Endpoint == "" {
			return nil, errors.New("[RouterOS] missing required fields")
		}
		return New(&cfg)
	})
}

func New(config *Config) (*RouterOS, error) {
	if config.Insecure == nil {
		insecure := true
		config.Insecure = &insecure
	}
	httpClient := resty.New().SetBasicAuth(config.Username, config.Password).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: *config.Insecure}).
		SetBaseURL(config.Endpoint + "/rest")
	return &RouterOS{
		config:     *config,
		httpClient: httpClient,
	}, nil
}

func (r *RouterOS) Ip() (string, error) {
	var rfaces []rosInterface
	_, err := r.httpClient.R().SetResult(&rfaces).Get("/interface")
	if err != nil {
		return "", err
	}

	var pppoeIfName string
	var ip string
	for _, rface := range rfaces {
		if rface.Type == "pppoe-out" {
			pppoeIfName = rface.Name
			break
		}
	}

	var raddrs []rosAddress
	_, err = r.httpClient.R().SetResult(&raddrs).Get("/ip/address")

	if err != nil {
		return "", err
	}

	for _, raddr := range raddrs {
		if raddr.Interface == pppoeIfName {
			ip = raddr.Address
			break
		}
	}

	if ip == "" {
		return "", errors.New("[RouterOS] no IP address found")
	}

	ip = ip[:len(ip)-3]

	return ip, nil
}
