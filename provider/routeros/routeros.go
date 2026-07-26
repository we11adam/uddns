package routeros

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/internal/redact"
	"github.com/we11adam/uddns/provider"
)

const errorBodyLimit = 4 << 10

type RouterOS struct {
	config     Config
	httpClient *resty.Client
}

type Config struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Endpoint string `mapstructure:"endpoint"`
	Insecure *bool  `mapstructure:"insecure"`
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
	provider.Register("RouterOS", "providers.routeros", func(v provider.ConfigReader) (provider.Provider, error) {
		if !v.IsSet("providers.routeros") {
			return nil, provider.ErrNotConfigured
		}

		cfg := Config{}
		err := v.UnmarshalKey("providers.routeros", &cfg)
		if err != nil {
			return nil, err
		}

		if cfg.Username == "" || cfg.Endpoint == "" {
			return nil, fmt.Errorf("missing required RouterOS fields")
		}
		return New(&cfg)
	})
}

func New(config *Config) (*RouterOS, error) {
	baseURL, err := routerOSRestURL(config.Endpoint)
	if err != nil {
		return nil, err
	}
	insecure := config.Insecure != nil && *config.Insecure
	normalizedConfig := *config
	normalizedConfig.Insecure = &insecure
	httpClient := resty.New().SetBasicAuth(config.Username, config.Password).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: insecure}).
		SetBaseURL(baseURL).
		SetTimeout(10 * time.Second)
	return &RouterOS{
		config:     normalizedConfig,
		httpClient: httpClient,
	}, nil
}

func routerOSRestURL(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid RouterOS endpoint: %s", endpoint)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("RouterOS endpoint must use http or https: %s", endpoint)
	}

	return strings.TrimRight(endpoint, "/") + "/rest", nil
}

func (r *RouterOS) GetIPs(ctx context.Context, families provider.FamilyRequest) (*provider.IpResult, error) {
	if !families.IPv4 && !families.IPv6 {
		return nil, fmt.Errorf("no IP families requested")
	}
	var rfaces []rosInterface
	if err := r.get(ctx, "/interface", &rfaces); err != nil {
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

	if families.IPv4 {
		var raddrs []rosAddress
		if err := r.get(ctx, "/ip/address", &raddrs); err != nil {
			return nil, err
		}

		for _, raddr := range raddrs {
			if raddr.Interface == pppoeIfName && raddr.Disabled != "true" {
				result.IPv4 = strings.Split(raddr.Address, "/")[0]
				break
			}
		}
	}

	if families.IPv6 {
		var raddrs6 []rosAddress
		if err := r.get(ctx, "/ipv6/address", &raddrs6); err != nil {
			return nil, err
		}

		for _, raddr := range raddrs6 {
			if raddr.Interface == pppoeIfName && raddr.Disabled != "true" {
				result.IPv6 = strings.Split(raddr.Address, "/")[0]
				break
			}
		}
	}

	if result.IPv4 == "" && result.IPv6 == "" {
		return nil, fmt.Errorf("no IP address found")
	}

	return result, nil
}

func (r *RouterOS) get(ctx context.Context, requestPath string, result any) error {
	resp, err := r.httpClient.R().
		SetContext(ctx).
		SetResult(result).
		Get(requestPath)
	if err != nil {
		return err
	}
	if resp.IsSuccess() {
		return nil
	}

	body := redact.String(strings.TrimSpace(string(resp.Body())), r.config.Password)
	if len(body) > errorBodyLimit {
		body = body[:errorBodyLimit] + "..."
	}
	if body == "" {
		return fmt.Errorf(
			"RouterOS REST GET %s failed: HTTP status %d",
			requestPath,
			resp.StatusCode(),
		)
	}
	return fmt.Errorf(
		"RouterOS REST GET %s failed: HTTP status %d: %q",
		requestPath,
		resp.StatusCode(),
		body,
	)
}
