package routeros

import (
	"crypto/tls"
	"fmt"
	"strings"

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
			return nil, fmt.Errorf("[RouterOS] missing required fields")
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

func (r *RouterOS) GetIPs() (*provider.IpResult, error) {
	var rfaces []rosInterface
	_, err := r.httpClient.R().SetResult(&rfaces).Get("/interface")
	if err != nil {
		return nil, err
	}

	var pppoeIfName string
	for _, rface := range rfaces {
		if rface.Type == "pppoe-out" {
			pppoeIfName = rface.Name
			break
		}
	}

	result := &provider.IpResult{}

	// Get IPv4 address
	var raddrs []rosAddress
	_, err = r.httpClient.R().SetResult(&raddrs).Get("/ip/address")
	if err != nil {
		return nil, err
	}

	for _, raddr := range raddrs {
		if raddr.Interface == pppoeIfName {
			result.IPv4 = strings.Split(raddr.Address, "/")[0]
			break
		}
	}

	// Get IPv6 address
	var raddrs6 []rosAddress
	_, err = r.httpClient.R().SetResult(&raddrs6).Get("/ipv6/address")
	if err != nil {
		return nil, err
	}

	for _, raddr := range raddrs6 {
		if raddr.Interface == pppoeIfName {
			result.IPv6 = strings.Split(raddr.Address, "/")[0]
			break
		}
	}

	if result.IPv4 == "" && result.IPv6 == "" {
		return nil, fmt.Errorf("[RouterOS] no IP address found")
	}

	return result, nil
}
